package httptransport

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"developa/internal/domain"
	"github.com/go-chi/chi/v5"
)

type observedEvent struct {
	Name string
	Data json.RawMessage
}

func readObservedEvent(reader *bufio.Reader) (observedEvent, error) {
	var event observedEvent
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			return event, err
		}
		if line == "\n" && event.Name != "" {
			return event, nil
		}
		key, value, _ := strings.Cut(line, ":")
		switch key {
		case "event":
			event.Name = strings.TrimSpace(value)
		case "data":
			event.Data = append(event.Data, strings.TrimSpace(value)...)
		}
	}
}

func observedEvents(t *testing.T, body string) []observedEvent {
	t.Helper()
	reader := bufio.NewReader(strings.NewReader(body))
	var events []observedEvent
	for {
		event, err := readObservedEvent(reader)
		if errors.Is(err, io.EOF) {
			return events
		}
		if err != nil {
			t.Fatal(err)
		}
		events = append(events, event)
	}
}

func TestAnswerStreamReturnsValidatedServiceResult(t *testing.T) {
	handler, _, intelligence := intelligenceFixture()
	response := authorizedRequest(handler, http.MethodPost, "/api/snapshots/"+snapshotID+"/answers/stream", `{"question":"secret-question","symbol_id":"`+symbolID+`"}`)
	if response.Code != http.StatusOK || !strings.HasPrefix(response.Header().Get("Content-Type"), "text/event-stream") {
		t.Fatalf("stream response: %d %s", response.Code, response.Body)
	}
	events := observedEvents(t, response.Body.String())
	if len(events) != 2 || events[0].Name != "started" || events[1].Name != "answer" {
		t.Fatalf("unexpected answer stream: %+v", events)
	}
	var answer domain.Answer
	if err := json.Unmarshal(events[1].Data, &answer); err != nil {
		t.Fatal(err)
	}
	if answer.Text != "Evidence-backed answer" || intelligence.request.SymbolID != symbolID || strings.Contains(response.Body.String(), "secret-question") {
		t.Fatal("stream lost its target or exposed request contents")
	}
}

func TestAnswerStreamErrorsAreSafeAndNeverEmitAnswers(t *testing.T) {
	cases := []struct {
		err  error
		code int
	}{{errors.New("secret SQL password source"), 503}, {domain.ErrInvalidModelOutput, 502}, {domain.ErrBusy, 409}, {context.DeadlineExceeded, 504}}
	for _, tc := range cases {
		handler, _, intelligence := intelligenceFixture()
		intelligence.err = tc.err
		response := authorizedRequest(handler, http.MethodPost, "/api/snapshots/"+snapshotID+"/answers/stream", `{"question":"why"}`)
		assertSafeAnswerErrorEvent(t, response, tc.code)
	}
}

func assertSafeAnswerErrorEvent(t *testing.T, response *httptest.ResponseRecorder, code int) {
	t.Helper()
	events := observedEvents(t, response.Body.String())
	if response.Code != 200 || len(events) != 2 || events[1].Name != "error" || strings.Contains(response.Body.String(), "secret") {
		t.Fatalf("unsafe error stream: %d %s", response.Code, response.Body)
	}
	var data struct {
		Status  int    `json:"status"`
		TraceID string `json:"trace_id"`
	}
	if err := json.Unmarshal(events[1].Data, &data); err != nil {
		t.Fatal(err)
	}
	if data.Status != code {
		t.Fatalf("error event status=%d want=%d", data.Status, code)
	}
}

func TestStreamPreflightKeepsOrdinaryHTTPErrors(t *testing.T) {
	cases := []struct {
		method, path, body string
		code               int
	}{
		{http.MethodPost, "/api/snapshots/" + snapshotID + "/answers/stream", `{}`, 400},
		{http.MethodPost, "/api/snapshots/" + snapshotID + "/answers/stream", `{"question":"why","feature_id":"invalid"}`, 400},
		{http.MethodPost, "/api/snapshots/invalid/answers/stream", `{"question":"why"}`, 400},
		{http.MethodGet, "/api/snapshots/invalid/events", "", 400},
		{http.MethodGet, "/api/snapshots/" + snapshotID + "/events", "", 503},
	}
	for _, tc := range cases {
		handler, _, intelligence := intelligenceFixture()
		response := authorizedRequest(handler, tc.method, tc.path, tc.body)
		if response.Code != tc.code || strings.HasPrefix(response.Header().Get("Content-Type"), "text/event-stream") || intelligence.method != "" {
			t.Fatalf("invalid preflight committed SSE headers: %d %s", response.Code, response.Body)
		}
	}
}

func TestStreamPreflightRejectsMissingFeatureBeforeInference(t *testing.T) {
	handler, knowledge, intelligence := intelligenceFixture()
	knowledge.err = domain.ErrNotFound
	response := authorizedRequest(handler, http.MethodPost, "/api/snapshots/"+snapshotID+"/answers/stream", `{"question":"why","feature_id":"`+symbolID+`"}`)
	if response.Code != 404 || intelligence.method != "" || knowledge.snapshot != snapshotID {
		t.Fatal("missing scoped feature reached inference or stream headers")
	}
}

func TestStreamsRequireHeaderAuthenticationAndSameOriginMutation(t *testing.T) {
	handler, _, intelligence := intelligenceFixture()
	for _, method := range []string{http.MethodGet, http.MethodPost} {
		path := "/api/snapshots/" + snapshotID + "/events?token=" + testToken
		if method == http.MethodPost {
			path = "/api/snapshots/" + snapshotID + "/answers/stream?token=" + testToken
		}
		request := httptest.NewRequest(method, path, strings.NewReader(`{"question":"why"}`))
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Code != 401 {
			t.Fatal("URL token authenticated stream")
		}
	}
	request := httptest.NewRequest(http.MethodPost, "/api/snapshots/"+snapshotID+"/answers/stream", strings.NewReader(`{"question":"why"}`))
	request.Header.Set("Authorization", "Bearer "+testToken)
	request.Header.Set("Origin", "https://attacker.invalid")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != 403 || intelligence.method != "" {
		t.Fatal("cross-origin stream invoked inference")
	}
}

type canceledAnswerService struct {
	domain.Intelligence
	entered, exited chan struct{}
	release         <-chan struct{}
}

func (*canceledAnswerService) Available() bool { return true }
func (s *canceledAnswerService) Answer(ctx context.Context, _ string, _ domain.AnswerRequest) (domain.Answer, error) {
	close(s.entered)
	defer close(s.exited)
	select {
	case <-ctx.Done():
		return domain.Answer{}, ctx.Err()
	case <-s.release:
		return domain.Answer{Text: "Persisted response"}, nil
	}
}

func TestAnswerStreamDisconnectCancelsModelWork(t *testing.T) {
	service := &canceledAnswerService{entered: make(chan struct{}), exited: make(chan struct{})}
	cfg, _ := analysisQueueFixture(&analysisQueueStub{})
	cfg.Explorer.Intelligence, cfg.Explorer.Knowledge = service, &knowledgeStub{}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	request := httptest.NewRequest(http.MethodPost, "/api/snapshots/"+snapshotID+"/answers/stream", strings.NewReader(`{"question":"why"}`)).WithContext(ctx)
	request.Header.Set("Authorization", "Bearer "+testToken)
	done := make(chan struct{})
	go func() { NewHandler(nil, cfg).ServeHTTP(httptest.NewRecorder(), request); close(done) }()
	awaitEventSignal(t, service.entered)
	cancel()
	awaitEventSignal(t, service.exited)
	awaitEventSignal(t, done)
}

func awaitEventSignal(t *testing.T, signal <-chan struct{}) {
	t.Helper()
	timer := time.NewTimer(2 * time.Second)
	defer timer.Stop()
	select {
	case <-signal:
	case <-timer.C:
		t.Fatal("stream operation did not finish within its cancellation bound")
	}
}

func TestAnalysisEventsDeduplicateAndHideLeaseData(t *testing.T) {
	previous := domain.AnalysisJob{Status: "queued", UpdatedAt: time.Unix(1, 0)}
	queue := &analysisQueueStub{job: previous}
	explorer := &Explorer{Jobs: queue}
	response := httptest.NewRecorder()
	writer := newEventWriter(response)
	request := httptest.NewRequest(http.MethodGet, "/", nil)
	route := chi.NewRouteContext()
	route.URLParams.Add("snapshot", snapshotID)
	request = request.WithContext(context.WithValue(request.Context(), chi.RouteCtxKey, route))
	if !explorer.nextAnalysisEvent(writer, request, &previous) || response.Body.Len() != 0 {
		t.Fatal("unchanged job was emitted")
	}
	queue.job.Status, queue.job.LeaseToken, queue.job.TraceParent = "running", "secret-lease", "secret-parent"
	if !explorer.nextAnalysisEvent(writer, request, &previous) {
		t.Fatal("changed job was not emitted")
	}
	if queue.snapshot != snapshotID || strings.Contains(response.Body.String(), "secret") {
		t.Fatal("analysis event leaked or lost scope")
	}
	if events := observedEvents(t, response.Body.String()); len(events) != 1 || events[0].Name != "analysis" {
		t.Fatal("wrong analysis event")
	}
}

func TestEventHeartbeatContainsNoRepositoryData(t *testing.T) {
	response := httptest.NewRecorder()
	if err := newEventWriter(response).heartbeat(); err != nil {
		t.Fatal(err)
	}
	if response.Body.String() != ": keepalive\n\n" || response.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("heartbeat contained unexpected data or was cacheable")
	}
}

func TestAnalysisStreamPollingTracksActiveVersusIdleStatus(t *testing.T) {
	for _, status := range []string{"not_queued", "completed", "failed", "superseded", ""} {
		if analysisPollInterval(status) != 2*time.Minute {
			t.Fatalf("idle status %q polls too often", status)
		}
	}
	for _, status := range []string{"queued", "running"} {
		if analysisPollInterval(status) != time.Second {
			t.Fatalf("active status %q lost progress polling", status)
		}
	}
}

func TestOversizedAnswerEventEmitsOnlySafeFailure(t *testing.T) {
	response := httptest.NewRecorder()
	finishSavedEvent(newEventWriter(response), context.Background(), "answer", savedEvent{value: domain.Answer{Text: strings.Repeat("secret", maxEventBytes)}})
	events := observedEvents(t, response.Body.String())
	if len(events) != 1 || events[0].Name != "error" || strings.Contains(response.Body.String(), "secret") || response.Body.Len() > maxEventBytes {
		t.Fatal("oversized answer escaped the bounded event contract")
	}
}

type countedHeaders struct {
	*httptest.ResponseRecorder
	count int
}

func (w *countedHeaders) WriteHeader(code int) { w.count++; w.ResponseRecorder.WriteHeader(code) }

func TestStreamTimeoutDoesNotWriteAfterCommittedHeaders(t *testing.T) {
	response := &countedHeaders{ResponseRecorder: httptest.NewRecorder()}
	handler := streamContextTimeout(5 * time.Millisecond)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200); <-r.Context().Done() }))
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/", nil))
	if response.count != 1 || response.Code != 200 {
		t.Fatal("stream timeout attempted to replace committed HTTP headers")
	}
}

func TestExactStreamRoutesReceiveBoundedDedicatedContexts(t *testing.T) {
	cfg := testConfig()
	cfg.RequestTimeout, cfg.AITimeout = time.Second, 3*time.Second
	cases := []struct {
		method, path string
		duration     time.Duration
	}{
		{http.MethodGet, "/api/snapshots/" + snapshotID + "/events", eventStreamLifetime},
		{http.MethodPost, "/api/snapshots/" + snapshotID + "/answers/stream", cfg.AITimeout},
		{http.MethodPost, "/api/snapshots/" + snapshotID + "/function-reviews/stream", cfg.AITimeout},
		{http.MethodPost, "/api/snapshots/" + snapshotID + "/function-reviews", cfg.AITimeout},
		{http.MethodGet, "/api/snapshots/" + snapshotID + "/function-reviews", cfg.RequestTimeout},
		{http.MethodPost, "/api/snapshots/" + snapshotID + "/events", cfg.RequestTimeout},
		{http.MethodGet, "/api/snapshots/" + snapshotID + "/events/extra", cfg.RequestTimeout},
		{http.MethodGet, "/api/snapshots/invalid/events", cfg.RequestTimeout},
	}
	for _, tc := range cases {
		for _, path := range []string{tc.path, strings.Replace(tc.path, "/api/", "/api/repositories/"+strings.Repeat("1", 64)+"/", 1)} {
			assertRequestTimeout(t, cfg, tc.method, path, tc.duration)
		}
	}
}

func assertRequestTimeout(t *testing.T, cfg Config, method, path string, duration time.Duration) {
	t.Helper()
	var remaining time.Duration
	handler := executionTimeout(cfg)(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		deadline, _ := r.Context().Deadline()
		remaining = time.Until(deadline)
	}))
	handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(method, path, nil))
	if remaining <= duration/2 || remaining > duration {
		t.Fatalf("incorrect stream lifetime for %s %s: %s", method, path, remaining)
	}
}

func TestMalformedRepositoryScopeDoesNotExtendRequestTimeout(t *testing.T) {
	cfg := testConfig()
	cfg.AITimeout = 3 * time.Second
	for _, id := range []string{"invalid", strings.Repeat("A", 64), strings.Repeat("1", 63), ""} {
		path := "/api/repositories/" + id + "/snapshots/" + snapshotID
		assertRequestTimeout(t, cfg, http.MethodGet, path+"/events", cfg.RequestTimeout)
		assertRequestTimeout(t, cfg, http.MethodPost, path+"/answers/stream", cfg.RequestTimeout)
	}
}

func TestHTTP2AnswerStreamSurvivesIdleWriteDeadline(t *testing.T) {
	release := make(chan struct{})
	service := &canceledAnswerService{entered: make(chan struct{}), exited: make(chan struct{}), release: release}
	cfg, _ := analysisQueueFixture(&analysisQueueStub{})
	cfg.AITimeout = 12 * time.Second
	cfg.Explorer.Intelligence, cfg.Explorer.Knowledge = service, &knowledgeStub{}
	server := httptest.NewUnstartedServer(NewHandler(nil, cfg))
	server.EnableHTTP2 = true
	server.StartTLS()
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
	t.Cleanup(func() { cancel(); server.Close() })
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, server.URL+"/api/snapshots/"+snapshotID+"/answers/stream", strings.NewReader(`{"question":"why"}`))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Authorization", "Bearer "+testToken)
	response, err := server.Client().Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.ProtoMajor != 2 {
		t.Fatal("regression requires a real HTTP/2 connection")
	}
	reader := bufio.NewReader(response.Body)
	assertObservedEvent(t, reader, "started")
	awaitIdleEventDeadline(t, ctx)
	close(release)
	assertObservedEvent(t, reader, "answer")
}

func assertObservedEvent(t *testing.T, reader *bufio.Reader, name string) observedEvent {
	t.Helper()
	event, err := readObservedEvent(reader)
	if err != nil || event.Name != name {
		t.Fatalf("expected %s event, got %s: %v", name, event.Name, err)
	}
	return event
}

func awaitIdleEventDeadline(t *testing.T, ctx context.Context) {
	t.Helper()
	// Crossing the actual configured IO bound catches HTTP/2's idle reset; this
	// delay is the failure condition, not synchronization with model scheduling.
	timer := time.NewTimer(eventIOTimeout + 100*time.Millisecond)
	defer timer.Stop()
	select {
	case <-timer.C:
	case <-ctx.Done():
		t.Fatal("stream deadline elapsed before the idle-write regression ran")
	}
}
