package httptransport

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"developa/internal/domain"
	"go.opentelemetry.io/otel/trace"
)

type analysisQueueStub struct {
	available        bool
	method, snapshot string
	job              domain.AnalysisJob
	err              error
	wait             bool
	deadline         time.Duration
	traceID          string
}

func (s *analysisQueueStub) Available() bool { return s.available }

func (s *analysisQueueStub) Queue(ctx context.Context, snapshot string) (domain.AnalysisJob, error) {
	s.method, s.snapshot = "queue", snapshot
	s.traceID = trace.SpanContextFromContext(ctx).TraceID().String()
	if s.wait {
		deadline, _ := ctx.Deadline()
		s.deadline = time.Until(deadline)
		<-ctx.Done()
		return domain.AnalysisJob{}, ctx.Err()
	}
	return s.job, s.err
}

func (s *analysisQueueStub) Status(_ context.Context, snapshot string) (domain.AnalysisJob, error) {
	s.method, s.snapshot = "status", snapshot
	return s.job, s.err
}

func analysisQueueFixture(queue *analysisQueueStub) (Config, *intelligenceStub) {
	intelligence := &intelligenceStub{}
	cfg := testConfig()
	cfg.Explorer = &Explorer{Catalog: &catalogStub{}, Tracker: &trackerStub{}, RepositoryID: "repo", Token: testToken, Intelligence: intelligence, Jobs: queue}
	return cfg, intelligence
}

func TestFeatureGenerationOnlyEnqueuesSnapshot(t *testing.T) {
	queue := &analysisQueueStub{available: true, job: domain.AnalysisJob{ID: symbolID, SnapshotID: snapshotID, Status: "queued", TraceParent: "private-parent", LeaseToken: "private-lease"}}
	cfg, intelligence := analysisQueueFixture(queue)
	response := authorizedRequest(NewHandler(nil, cfg), http.MethodPost, "/api/snapshots/"+snapshotID+"/features/generate", "{}")
	var job domain.AnalysisJob
	if err := json.Unmarshal(response.Body.Bytes(), &job); err != nil {
		t.Fatal(err)
	}
	if response.Code != http.StatusAccepted || job.ID != symbolID || job.Status != "queued" {
		t.Fatalf("job admission: %d %+v", response.Code, job)
	}
	if queue.method != "queue" || queue.snapshot != snapshotID || intelligence.method != "" {
		t.Fatal("generation must enqueue the pinned snapshot without synchronous inference")
	}
	if strings.Contains(response.Body.String(), "private-") {
		t.Fatal("job response leaked internal lease or trace parent")
	}
}

func TestFeatureGenerationNeverFallsBackWhenQueueUnavailable(t *testing.T) {
	for _, queue := range []*analysisQueueStub{nil, {available: false}} {
		cfg, intelligence := analysisQueueFixture(queue)
		if queue == nil {
			cfg.Explorer.Jobs = nil
		}
		response := authorizedRequest(NewHandler(nil, cfg), http.MethodPost, "/api/snapshots/"+snapshotID+"/features/generate", "")
		if response.Code != http.StatusServiceUnavailable || intelligence.method != "" {
			t.Fatalf("unavailable queue attempted inference: %d", response.Code)
		}
	}
}

func TestAnalysisStatusReadsDurableStateWhileWorkerDisabled(t *testing.T) {
	for _, status := range []string{"not_queued", "queued", "running", "completed", "failed", "superseded"} {
		queue := &analysisQueueStub{job: domain.AnalysisJob{SnapshotID: snapshotID, Status: status}}
		cfg, _ := analysisQueueFixture(queue)
		response := authorizedRequest(NewHandler(nil, cfg), http.MethodGet, "/api/snapshots/"+snapshotID+"/analysis-job", "")
		var job domain.AnalysisJob
		if err := json.Unmarshal(response.Body.Bytes(), &job); err != nil {
			t.Fatal(err)
		}
		if response.Code != http.StatusOK || job.Status != status || queue.snapshot != snapshotID || queue.method != "status" {
			t.Fatalf("status unavailable or unscoped: %d %+v", response.Code, job)
		}
	}
}

func TestAnalysisQueueErrorsAreSanitized(t *testing.T) {
	cases := []struct {
		err  error
		code int
	}{{domain.ErrNotFound, 404}, {domain.ErrBusy, 409}, {domain.ErrModelUnavailable, 503}, {errors.New("postgres credential secret"), 503}}
	for _, tc := range cases {
		queue := &analysisQueueStub{available: true, err: tc.err}
		cfg, _ := analysisQueueFixture(queue)
		for _, method := range []string{http.MethodPost, http.MethodGet} {
			response := authorizedRequest(NewHandler(nil, cfg), method, analysisRoute(method, snapshotID), "{}")
			if response.Code != tc.code || strings.Contains(response.Body.String(), "secret") {
				t.Fatalf("unsafe %s queue failure: %d %s", method, response.Code, response.Body)
			}
		}
	}
}

func analysisRoute(method, snapshot string) string {
	if method == http.MethodPost {
		return "/api/snapshots/" + snapshot + "/features/generate"
	}
	return "/api/snapshots/" + snapshot + "/analysis-job"
}

func TestAnalysisJobRequiresAuthenticationAndValidSnapshot(t *testing.T) {
	for _, method := range []string{http.MethodPost, http.MethodGet} {
		queue := &analysisQueueStub{available: true}
		cfg, _ := analysisQueueFixture(queue)
		handler := NewHandler(nil, cfg)
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest(method, analysisRoute(method, snapshotID), nil))
		if response.Code != http.StatusUnauthorized || queue.method != "" {
			t.Fatal("anonymous request reached queue")
		}
		response = authorizedRequest(handler, method, analysisRoute(method, "bad"), "")
		if response.Code != http.StatusBadRequest || queue.method != "" {
			t.Fatal("invalid snapshot reached queue")
		}
	}
}

func TestFeatureQueueRejectsUntrustedOriginAndBody(t *testing.T) {
	cases := []struct{ body, header, value string }{
		{"{}", "Origin", "https://attacker.invalid"}, {"{}", "Sec-Fetch-Site", "cross-site"},
		{`{"model":"untrusted"}`, "", ""}, {"{}{}", "", ""}, {strings.Repeat(" ", 1025), "", ""},
	}
	for _, tc := range cases {
		queue := &analysisQueueStub{available: true}
		cfg, _ := analysisQueueFixture(queue)
		request := httptest.NewRequest(http.MethodPost, analysisRoute(http.MethodPost, snapshotID), strings.NewReader(tc.body))
		request.Header.Set("Authorization", "Bearer "+testToken)
		request.Header.Set(tc.header, tc.value)
		response := httptest.NewRecorder()
		NewHandler(nil, cfg).ServeHTTP(response, request)
		if response.Code < 400 || queue.method != "" {
			t.Fatalf("untrusted queue request reached service: %d", response.Code)
		}
	}
}

func TestAnalysisQueueUsesOrdinaryDeadline(t *testing.T) {
	queue := &analysisQueueStub{available: true, wait: true}
	cfg, _ := analysisQueueFixture(queue)
	cfg.RequestTimeout, cfg.AITimeout = 10*time.Millisecond, time.Second
	response := authorizedRequest(NewHandler(nil, cfg), http.MethodPost, analysisRoute(http.MethodPost, snapshotID), "{}")
	if response.Code != http.StatusGatewayTimeout || queue.deadline > cfg.RequestTimeout {
		t.Fatalf("queue inherited model deadline: %d %s", response.Code, queue.deadline)
	}
}

func TestOnlyKnownAnswerRouteGetsModelDeadline(t *testing.T) {
	cases := []struct {
		method, path string
		model        bool
	}{
		{http.MethodPost, "/api/snapshots/" + snapshotID + "/answers", true},
		{http.MethodPost, "/api/snapshots/" + snapshotID + "/answers/stream", true},
		{http.MethodGet, "/api/snapshots/" + snapshotID + "/answers", false},
		{http.MethodPost, analysisRoute(http.MethodPost, snapshotID), false},
		{http.MethodPost, "/api/snapshots/invalid/answers", false},
		{http.MethodPost, "/api/snapshots/" + snapshotID + "/unknown/answers", false},
		{http.MethodPost, "/api/snapshots/" + snapshotID + "/answers/stream/extra", false},
	}
	for _, tc := range cases {
		if isModelRequest(httptest.NewRequest(tc.method, tc.path, nil)) != tc.model {
			t.Fatalf("incorrect timeout class for %s %s", tc.method, tc.path)
		}
	}
}

func TestAnalysisJobsCapabilityRequiresAvailableWorker(t *testing.T) {
	for _, available := range []bool{true, false} {
		cfg, _ := analysisQueueFixture(&analysisQueueStub{available: available})
		response := authorizedRequest(NewHandler(nil, cfg), http.MethodGet, "/api/capabilities", "")
		var capabilities map[string]bool
		if err := json.Unmarshal(response.Body.Bytes(), &capabilities); err != nil {
			t.Fatal(err)
		}
		if response.Code != http.StatusOK || capabilities["analysis_jobs"] != available {
			t.Fatal("capability disagrees with worker availability")
		}
	}
}

func TestAnalysisOnlyConfigurationDoesNotEnableAnswers(t *testing.T) {
	queue := &analysisQueueStub{available: true}
	cfg, _ := analysisQueueFixture(queue)
	cfg.Explorer.Intelligence = nil
	cfg.Explorer.Knowledge = &knowledgeStub{}
	handler := NewHandler(nil, cfg)
	response := authorizedRequest(handler, http.MethodGet, "/api/capabilities", "")
	var capabilities map[string]bool
	if err := json.Unmarshal(response.Body.Bytes(), &capabilities); err != nil {
		t.Fatal(err)
	}
	if response.Code != http.StatusOK || !capabilities["ollama_configured"] || !capabilities["analysis_jobs"] || capabilities["answers"] {
		t.Fatal("analysis-only model configuration was hidden")
	}
	response = authorizedRequest(handler, http.MethodPost, "/api/snapshots/"+snapshotID+"/answers", `{"question":"What does this do?"}`)
	if response.Code != http.StatusServiceUnavailable || !strings.Contains(response.Body.String(), "model_unavailable") {
		t.Fatal("analysis configuration unexpectedly enabled synchronous answers")
	}
}

func TestCapabilitiesReportExplicitAutomaticFeaturePolicy(t *testing.T) {
	for _, automatic := range []bool{false, true} {
		cfg, _ := analysisQueueFixture(&analysisQueueStub{available: true})
		cfg.Explorer.AutomaticFeatures = automatic
		response := authorizedRequest(NewHandler(nil, cfg), http.MethodGet, "/api/capabilities", "")
		var capabilities map[string]bool
		if err := json.Unmarshal(response.Body.Bytes(), &capabilities); err != nil {
			t.Fatal(err)
		}
		if capabilities["automatic_features"] != automatic || !capabilities["answers"] {
			t.Fatal("capabilities confused explicit answer availability with automatic analysis policy")
		}
	}
}

func TestAutomaticFeaturesCapabilityRequiresAvailableWorker(t *testing.T) {
	cfg, _ := analysisQueueFixture(&analysisQueueStub{})
	cfg.Explorer.AutomaticFeatures = true
	response := authorizedRequest(NewHandler(nil, cfg), http.MethodGet, "/api/capabilities", "")
	var capabilities map[string]bool
	if err := json.Unmarshal(response.Body.Bytes(), &capabilities); err != nil {
		t.Fatal(err)
	}
	if capabilities["automatic_features"] {
		t.Fatal("disabled worker advertised automatic analysis")
	}
}

func TestAnalysisAdmissionPropagatesSafeOperatorTrace(t *testing.T) {
	exporter := installTraceProvider(t)
	queue := &analysisQueueStub{available: true}
	cfg, _ := analysisQueueFixture(queue)
	request := httptest.NewRequest(http.MethodPost, analysisRoute(http.MethodPost, snapshotID)+"?prompt=secret-source&actor=system", nil)
	request.Header.Set("Authorization", "Bearer "+testToken)
	request.Header.Set("traceparent", "00-"+integrationTraceID+"-00f067aa0ba902b7-01")
	response := httptest.NewRecorder()
	NewHandler(nil, cfg).ServeHTTP(response, request)
	if response.Code != http.StatusAccepted || queue.traceID != integrationTraceID {
		t.Fatal("admission lost its request trace")
	}
	spans := exporter.GetSpans()
	if len(spans) != 1 || spans[0].Name != "HTTP /api/snapshots/{snapshot}/features/generate" {
		t.Fatal("admission trace did not use the safe route template")
	}
	assertSafeRequestSpan(t, spans[0])
	actor := ""
	for _, attribute := range spans[0].Attributes {
		if string(attribute.Key) == "actor.type" {
			actor = attribute.Value.AsString()
		}
	}
	if actor != "operator" {
		t.Fatal("query parameters changed the trusted queue actor")
	}
}
