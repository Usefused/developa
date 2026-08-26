package httptransport

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"developa/internal/application"
	"developa/internal/domain"
	"developa/internal/model/ollama"
	"github.com/jackc/pgx/v5"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

func TestIntegrationCallsFeaturesAndAnswers(t *testing.T) {
	exporter := installTraceProvider(t)
	fixture, model := intelligenceIntegrationWithLimits(t, 8, true)
	integrationWrite(t, fixture.root, "main.go", "package fixture\n// Original returns a greeting through Helper.\nfunc Original(name string) string { return Helper(name) }\nfunc Helper(name string) string { return name }\n")
	fixture.manager.Start(context.Background())
	snapshot := awaitIntegrationSnapshot(t, fixture, "")
	assertIntegratedCallChain(t, fixture, snapshot.ID)
	assertIntegratedFeatures(t, fixture, snapshot.ID)
	assertIntegratedAnswer(t, fixture, snapshot.ID)
	assertRejectedGenerationPreservesFeatures(t, fixture, model, snapshot.ID)
	assertIntegratedIntelligenceAudit(t, fixture)
	assertIntegratedAnalysisTraceLink(t, exporter.GetSpans())
}

type protocolModel struct {
	invalid         atomic.Bool
	changedRevision atomic.Bool
	calls           atomic.Int32
	pauseAt         atomic.Int32
	paused          chan struct{}
	resume          chan struct{}
}

func newIntelligenceIntegration(t *testing.T) (*integrationExplorer, *protocolModel) {
	t.Helper()
	return intelligenceIntegrationWithLimits(t, 8, false)
}

type inferenceOnlyStore struct{ domain.IntelligenceStore }

func intelligenceIntegrationWithLimits(t *testing.T, batch int, forceInference bool) (*integrationExplorer, *protocolModel) {
	t.Helper()
	fixture := newIntegrationExplorer(t)
	fixture.server.Close()
	model := &protocolModel{paused: make(chan struct{}), resume: make(chan struct{})}
	provider := httptest.NewServer(http.HandlerFunc(model.serve))
	t.Cleanup(provider.Close)
	client, err := ollama.New(ollama.Config{BaseURL: provider.URL, Model: "fixture:latest", Timeout: 3 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	var store domain.IntelligenceStore = fixture.store
	if forceInference {
		// Invalid-output fixtures must reach inference instead of reusing a prior valid
		// cached page; the worker still uses the real store for leases and audit.
		store = inferenceOnlyStore{IntelligenceStore: store}
	}
	service, err := application.NewIntelligence(store, client, application.IntelligenceConfig{RepositoryID: fixture.manager.Repository().ID, Timeout: 3 * time.Second, BatchSize: batch, MaxModelCalls: 1})
	if err != nil {
		t.Fatal(err)
	}
	fixture.worker = integrationAnalysisWorker(t, fixture, service)
	cfg := testConfig()
	cfg.AITimeout = 4 * time.Second
	cfg.Explorer = &Explorer{Catalog: fixture.store, Tracker: fixture.manager, RepositoryID: fixture.manager.Repository().ID, Token: testToken, Knowledge: fixture.store, Intelligence: service, Reviewer: service, Jobs: fixture.worker}
	fixture.server = httptest.NewServer(NewHandler(fixture.store, cfg))
	t.Cleanup(fixture.server.Close)
	return fixture, model
}

func integrationAnalysisWorker(t *testing.T, fixture *integrationExplorer, service domain.Intelligence) *application.AnalysisWorker {
	t.Helper()
	worker, err := application.NewAnalysisWorker(fixture.store, service, application.AnalysisWorkerConfig{
		RepositoryID: fixture.manager.Repository().ID, PollInterval: 25 * time.Millisecond,
		ExecutionTimeout: 3 * time.Second, RetryInterval: 25 * time.Millisecond,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(worker.Close)
	return worker
}

func queueIntegratedAnalysis(t *testing.T, fixture *integrationExplorer, snapshot string) domain.AnalysisJob {
	t.Helper()
	var job domain.AnalysisJob
	status := integrationPostJSON(t, fixture, "/api/snapshots/"+snapshot+"/features/generate", "{}", &job)
	if status != http.StatusAccepted || job.ID == "" || job.SnapshotID != snapshot {
		t.Fatalf("analysis admission: %d %+v", status, job)
	}
	return job
}

func awaitIntegratedAnalysis(t *testing.T, fixture *integrationExplorer, snapshot, status string) domain.AnalysisJob {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
	defer cancel()
	ticker := time.NewTicker(25 * time.Millisecond)
	defer ticker.Stop()
	for {
		var job domain.AnalysisJob
		integrationRead(t, fixture, "/api/snapshots/"+snapshot+"/analysis-job", &job)
		if job.Status == status {
			return job
		}
		if job.Status == "failed" || job.Status == "completed" || job.Status == "superseded" {
			t.Fatalf("analysis reached unexpected terminal state: %+v", job)
		}
		select {
		case <-ctx.Done():
			t.Fatalf("analysis did not reach %s: %+v", status, job)
		case <-ticker.C:
		}
	}
}

func TestIntegrationFeatureContinuationRetainsEarlierEvidence(t *testing.T) {
	fixture, model := intelligenceIntegrationWithLimits(t, 1, false)
	model.pauseAt.Store(2)
	fixture.manager.Start(context.Background())
	snapshot := awaitIntegrationSnapshot(t, fixture, "")
	base := "/api/snapshots/" + snapshot.ID
	queueIntegratedAnalysis(t, fixture, snapshot.ID)
	fixture.worker.Start(context.Background())
	awaitProtocolPause(t, model)
	var page domain.FeaturePage
	integrationRead(t, fixture, base+"/features", &page)
	if page.Run == nil || page.Run.AnalyzedSymbols != 1 || page.Run.Status != "partial" || len(page.Items) != 1 {
		t.Fatalf("first chunk coverage: %+v", page)
	}
	firstID := page.Run.ID
	original := page.Items[0]
	// The JSON decoder may reuse page slice storage on the next read; retain the evidence value.
	expectedEvidence := original.Evidence[0]
	close(model.resume)
	job := awaitIntegratedAnalysis(t, fixture, snapshot.ID, "completed")
	integrationRead(t, fixture, base+"/features", &page)
	if page.Run.ParentRunID != firstID || job.AnalyzedSymbols != 2 || job.Chunks != 2 || page.Total != 2 {
		t.Fatalf("continuation lost progress: %+v total=%d", job, page.Total)
	}
	var preserved domain.Feature
	integrationRead(t, fixture, base+"/features/"+original.ID, &preserved)
	if preserved.Evidence[0] != expectedEvidence {
		t.Fatal("resumed feature evidence changed")
	}
}

func TestIntegrationAnalysisJobsRemainSnapshotScoped(t *testing.T) {
	fixture, model := newIntelligenceIntegration(t)
	fixture.manager.Start(context.Background())
	first := awaitIntegrationSnapshot(t, fixture, "")
	assertIntegratedJobStatus(t, fixture, first.ID, "not_queued")
	job := queueIntegratedAnalysis(t, fixture, first.ID)
	repeated := queueIntegratedAnalysis(t, fixture, first.ID)
	if job.ID != repeated.ID || model.calls.Load() != 0 {
		t.Fatal("queue admission must deduplicate without running inference in the request")
	}
	integrationWrite(t, fixture.root, "main.go", "package fixture\nfunc Updated() {}\n")
	second := awaitIntegrationSnapshot(t, fixture, first.ID)
	assertIntegratedJobStatus(t, fixture, first.ID, "queued")
	assertIntegratedJobStatus(t, fixture, second.ID, "not_queued")
	assertIntegratedJobAccess(t, fixture, second.ID)
}

func TestIntegrationSavedFeaturesRemainDiscoverableAfterSourceChanges(t *testing.T) {
	fixture, model := newIntelligenceIntegration(t)
	fixture.manager.Start(context.Background())
	first := awaitIntegrationSnapshot(t, fixture, "")
	queueIntegratedAnalysis(t, fixture, first.ID)
	fixture.worker.Start(context.Background())
	awaitIntegratedAnalysis(t, fixture, first.ID, "completed")
	calls := model.calls.Load()
	integrationWrite(t, fixture.root, "main.go", "package fixture\nfunc Changed() {}\n")
	current := awaitIntegrationSnapshot(t, fixture, first.ID)
	var page domain.FeaturePage
	integrationRead(t, fixture, "/api/snapshots/"+current.ID+"/features", &page)
	if page.SavedSnapshot == nil || page.SavedSnapshot.ID != first.ID || page.Run != nil || page.Total != 0 {
		t.Fatalf("new source did not identify prior saved analysis: %+v", page)
	}
	var saved domain.FeaturePage
	integrationRead(t, fixture, "/api/snapshots/"+page.SavedSnapshot.ID+"/features", &saved)
	if saved.Run == nil || saved.Run.SnapshotID != first.ID || saved.Total == 0 || saved.SavedSnapshot != nil {
		t.Fatalf("saved results lost source provenance: %+v", saved)
	}
	if model.calls.Load() != calls {
		t.Fatal("reading saved analysis triggered inference")
	}
}

func assertIntegratedJobStatus(t *testing.T, fixture *integrationExplorer, snapshot, status string) {
	t.Helper()
	var job domain.AnalysisJob
	integrationRead(t, fixture, "/api/snapshots/"+snapshot+"/analysis-job", &job)
	if job.SnapshotID != snapshot || job.Status != status {
		t.Fatalf("wrong snapshot job state: %+v", job)
	}
}

func assertIntegratedJobAccess(t *testing.T, fixture *integrationExplorer, snapshot string) {
	t.Helper()
	for _, method := range []string{http.MethodPost, http.MethodGet} {
		status := integrationRequest(t, fixture, method, analysisRoute(method, snapshot), false, nil)
		if status != http.StatusUnauthorized {
			t.Fatalf("anonymous analysis request returned %d", status)
		}
		status = integrationRequest(t, fixture, method, analysisRoute(method, strings.Repeat("f", 64)), true, nil)
		if status != http.StatusNotFound {
			t.Fatalf("unknown snapshot returned %d", status)
		}
	}
}

func awaitProtocolPause(t *testing.T, model *protocolModel) {
	t.Helper()
	timer := time.NewTimer(5 * time.Second)
	defer timer.Stop()
	select {
	case <-model.paused:
	case <-timer.C:
		t.Fatal("worker did not continue to the second model batch")
	}
}

func (m *protocolModel) awaitRelease(ctx context.Context, call int32) bool {
	if call != m.pauseAt.Load() {
		return true
	}
	close(m.paused)
	select {
	case <-ctx.Done():
		return false
	case <-m.resume:
		return true
	}
}

func (m *protocolModel) serve(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == "/api/tags" {
		writeJSON(w, http.StatusOK, map[string]any{"models": []any{map[string]string{"name": "fixture:latest", "model": "fixture:latest", "digest": m.providerDigest()}}})
		return
	}
	if !m.awaitRelease(r.Context(), m.calls.Add(1)) {
		return
	}
	var input struct {
		Messages []struct {
			Content string `json:"content"`
		} `json:"messages"`
		Format json.RawMessage `json:"format"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&input); err != nil || len(input.Messages) != 2 {
		writeStatus(w, 400, "bad_protocol")
		return
	}
	_, data, ok := strings.Cut(input.Messages[1].Content, "DATA:\n")
	if !ok {
		writeStatus(w, 400, "missing_evidence")
		return
	}
	feature := bytes.Contains(input.Format, []byte(`"features"`))
	id := protocolEvidenceID(data, feature)
	if m.invalid.Load() {
		id = strings.Repeat("f", 64)
	}
	content := protocolOutput(id, feature)
	if bytes.Contains(input.Format, []byte(`"reviews"`)) {
		content = protocolReviewOutput(data, m.invalid.Load())
	}
	writeJSON(w, http.StatusOK, map[string]any{"model": "fixture:latest", "done": true, "done_reason": "stop", "message": map[string]string{"role": "assistant", "content": content}})
}

func (m *protocolModel) providerDigest() string {
	if m.changedRevision.Load() {
		return strings.Repeat("d", 64)
	}
	return strings.Repeat("c", 64)
}

func protocolEvidenceID(data string, feature bool) string {
	var facts []struct {
		ID string `json:"id"`
	}
	if feature {
		_ = json.Unmarshal([]byte(data), &facts)
	} else {
		var input struct {
			Symbols json.RawMessage `json:"symbols"`
		}
		_ = json.Unmarshal([]byte(data), &input)
		_ = json.Unmarshal(input.Symbols, &facts)
	}
	if len(facts) == 0 {
		return ""
	}
	return facts[0].ID
}

func protocolOutput(id string, feature bool) string {
	var output any = map[string]any{"text": "The function delegates to Helper and returns its string result.", "symbol_ids": []string{id}, "insufficient_evidence": false}
	if feature {
		output = map[string]any{"features": []any{map[string]any{"title": "Greeting delegation", "summary": "Delegates greeting handling through the captured helper.", "symbol_ids": []string{id}}}}
	}
	data, _ := json.Marshal(output)
	return string(data)
}

func assertIntegratedCallChain(t *testing.T, fixture *integrationExplorer, snapshot string) {
	t.Helper()
	var symbols domain.SymbolPage
	base := "/api/snapshots/" + snapshot
	integrationRead(t, fixture, base+"/symbols?q=Original&kind=function", &symbols)
	if len(symbols.Items) != 1 {
		t.Fatalf("root lookup: %+v", symbols)
	}
	var chain domain.CallChain
	integrationRead(t, fixture, base+"/symbols/"+symbols.Items[0].Symbol.ID+"/chain?direction=out&depth=2", &chain)
	if len(chain.Nodes) != 2 || len(chain.Edges) != 1 || chain.Edges[0].Resolution != "resolved" {
		t.Fatalf("resolved chain lost: %+v", chain)
	}
	var pack domain.ContextPack
	integrationRead(t, fixture, base+"/context?q=Original&limit=2", &pack)
	if len(pack.Items) == 0 || !strings.Contains(pack.Items[0].Symbol.Source, "Helper") {
		t.Fatalf("source context missing: %+v", pack)
	}
}

func assertIntegratedFeatures(t *testing.T, fixture *integrationExplorer, snapshot string) {
	t.Helper()
	base := "/api/snapshots/" + snapshot
	queueIntegratedAnalysis(t, fixture, snapshot)
	fixture.worker.Start(context.Background())
	job := awaitIntegratedAnalysis(t, fixture, snapshot, "completed")
	var page domain.FeaturePage
	integrationRead(t, fixture, base+"/features", &page)
	if page.Run == nil || page.Run.AnalyzedSymbols != job.AnalyzedSymbols || page.Total != 1 {
		t.Fatalf("feature persistence lost: %+v", page)
	}
	var feature domain.Feature
	integrationRead(t, fixture, base+"/features/"+page.Items[0].ID, &feature)
	if feature.Status != "inferred" || len(feature.Evidence) != 1 {
		t.Fatalf("feature evidence lost: %+v", feature)
	}
	citation := feature.Evidence[0]
	detail := readIntegrationSymbol(t, fixture, snapshot, citation.SymbolID)
	if citation.Path != detail.Path || citation.Span != detail.Symbol.Span {
		t.Fatal("citation did not retain canonical source position")
	}
}

func assertIntegratedAnswer(t *testing.T, fixture *integrationExplorer, snapshot string) {
	t.Helper()
	var answer domain.Answer
	status := integrationPostJSON(t, fixture, "/api/snapshots/"+snapshot+"/answers", `{"question":"What does Original do?"}`, &answer)
	if status != http.StatusOK || len(answer.Evidence) != 1 || answer.InsufficientEvidence {
		t.Fatalf("answer failed: %d %+v", status, answer)
	}
	if !strings.Contains(answer.Model, "@sha256:") {
		t.Fatal("model digest provenance missing")
	}
	var total int
	query := "SELECT count(*) FROM " + pgx.Identifier{fixture.schema, "developa_answers"}.Sanitize() + " WHERE id=$1"
	if err := fixture.admin.QueryRow(context.Background(), query, answer.ID).Scan(&total); err != nil || total != 1 {
		t.Fatalf("answer was not persisted: %v", err)
	}
}

func assertRejectedGenerationPreservesFeatures(t *testing.T, fixture *integrationExplorer, model *protocolModel, snapshot string) {
	t.Helper()
	base := "/api/snapshots/" + snapshot
	var before, after domain.FeaturePage
	integrationRead(t, fixture, base+"/features", &before)
	model.invalid.Store(true)
	queueIntegratedAnalysis(t, fixture, snapshot)
	job := awaitIntegratedAnalysis(t, fixture, snapshot, "failed")
	if job.ErrorCode != "invalid_model_output" || job.Attempts != 3 {
		t.Fatalf("invented citation accepted or failure not bounded: %+v", job)
	}
	integrationRead(t, fixture, base+"/features", &after)
	if after.Run.ID != before.Run.ID || after.Total != before.Total {
		t.Fatal("failed generation replaced published features")
	}
	if model.calls.Load() < 3 {
		t.Fatal("protocol fixture was not exercised")
	}
}

func assertIntegratedIntelligenceAudit(t *testing.T, fixture *integrationExplorer) {
	t.Helper()
	query := "SELECT count(*) FROM " + pgx.Identifier{fixture.schema, "developa_audit_events"}.Sanitize() + " WHERE trigger='answer_question' AND outcome='completed' AND trace_id=$1"
	var total int
	if err := fixture.admin.QueryRow(context.Background(), query, integrationTraceID).Scan(&total); err != nil || total != 1 {
		t.Fatalf("intelligence audit missing: %d %v", total, err)
	}
	assertIntegratedQueueAudit(t, fixture)
}

func assertIntegratedQueueAudit(t *testing.T, fixture *integrationExplorer) {
	t.Helper()
	query := "SELECT count(DISTINCT execution_id) FROM " + pgx.Identifier{fixture.schema, "developa_audit_events"}.Sanitize() + " WHERE trigger='feature_manual' AND actor='operator' AND outcome='queued' AND trace_id=$1 AND job_id IS NOT NULL"
	var queued int
	if err := fixture.admin.QueryRow(context.Background(), query, integrationTraceID).Scan(&queued); err != nil || queued != 2 {
		t.Fatalf("operator job admission audit missing: %d %v", queued, err)
	}
	query = "SELECT count(*) FROM " + pgx.Identifier{fixture.schema, "developa_audit_events"}.Sanitize() + " WHERE trigger='discover_features' AND actor='system' AND outcome='completed' AND job_id IS NOT NULL"
	var completed int
	if err := fixture.admin.QueryRow(context.Background(), query).Scan(&completed); err != nil || completed != 1 {
		t.Fatalf("system job execution audit missing: %d %v", completed, err)
	}
}

func assertIntegratedAnalysisTraceLink(t *testing.T, spans []tracetest.SpanStub) {
	t.Helper()
	for _, span := range spans {
		if span.Name != "analysis.chunk" {
			continue
		}
		for _, link := range span.Links {
			if link.SpanContext.TraceID().String() == integrationTraceID {
				return
			}
		}
	}
	t.Fatal("background chunk trace was not linked to the admitting request")
}

func integrationPostJSON(t *testing.T, fixture *integrationExplorer, path, body string, target any) int {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, fixture.server.URL+path, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Authorization", "Bearer "+testToken)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("traceparent", "00-"+integrationTraceID+"-00f067aa0ba902b7-01")
	response, err := fixture.server.Client().Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if target != nil {
		if err := json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(target); err != nil {
			t.Fatal(err)
		}
	}
	return response.StatusCode
}
