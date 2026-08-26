package httptransport

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"developa/internal/domain"
	"github.com/jackc/pgx/v5"
)

func integrationStream(t *testing.T, fixture *integrationExplorer, method, path, body string) *http.Response {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	t.Cleanup(cancel)
	request, err := http.NewRequestWithContext(ctx, method, fixture.server.URL+path, strings.NewReader(body))
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
	t.Cleanup(func() { _ = response.Body.Close() })
	if response.StatusCode != 200 || !strings.HasPrefix(response.Header.Get("Content-Type"), "text/event-stream") {
		t.Fatalf("stream preflight failed: %d", response.StatusCode)
	}
	return response
}

func integrationStreamAnswer(t *testing.T, fixture *integrationExplorer, snapshot string, request domain.AnswerRequest) domain.Answer {
	t.Helper()
	body, err := json.Marshal(request)
	if err != nil {
		t.Fatal(err)
	}
	response := integrationStream(t, fixture, http.MethodPost, "/api/snapshots/"+snapshot+"/answers/stream", string(body))
	reader := bufio.NewReader(response.Body)
	assertObservedEvent(t, reader, "started")
	event := assertObservedEvent(t, reader, "answer")
	var answer domain.Answer
	if err := json.Unmarshal(event.Data, &answer); err != nil {
		t.Fatal(err)
	}
	if _, err := readObservedEvent(reader); !errors.Is(err, io.EOF) {
		t.Fatalf("answer stream did not end: %v", err)
	}
	assertStreamAnswerPersisted(t, fixture, answer)
	return answer
}

func assertStreamAnswerPersisted(t *testing.T, fixture *integrationExplorer, answer domain.Answer) {
	t.Helper()
	var count int
	query := "SELECT count(*) FROM " + pgx.Identifier{fixture.schema, "developa_answers"}.Sanitize() + " WHERE id=$1 AND snapshot_id=$2"
	if err := fixture.admin.QueryRow(context.Background(), query, answer.ID, answer.SnapshotID).Scan(&count); err != nil || count != 1 {
		t.Fatalf("SSE answer was emitted before persistence: count=%d err=%v", count, err)
	}
	if len(answer.Evidence) != 1 {
		t.Fatal("streamed answer lacks canonical evidence")
	}
	detail := readIntegrationSymbol(t, fixture, answer.SnapshotID, answer.Evidence[0].SymbolID)
	if answer.Evidence[0].Span != detail.Symbol.Span || answer.Evidence[0].Path != detail.Path {
		t.Fatal("streamed answer has stale evidence")
	}
}

func TestIntegrationAnswerStreamsPersistAndCacheExplicitTargets(t *testing.T) {
	fixture, model := newIntelligenceIntegration(t)
	fixture.manager.Start(context.Background())
	snapshot := awaitIntegrationSnapshot(t, fixture, "")
	page := runIntegratedFeaturePage(t, fixture, snapshot.ID)
	feature := page.Items[0]
	assertExplicitAnswerStreamCache(t, fixture, model, snapshot.ID, domain.AnswerRequest{Question: "Explain this function from the captured source.", SymbolID: feature.Evidence[0].SymbolID})
	assertExplicitAnswerStreamCache(t, fixture, model, snapshot.ID, domain.AnswerRequest{Question: "Explain this feature using its cited code.", FeatureID: feature.ID})
	assertMissingStreamFeature(t, fixture, model, snapshot.ID)
}

func assertExplicitAnswerStreamCache(t *testing.T, fixture *integrationExplorer, model *protocolModel, snapshot string, request domain.AnswerRequest) {
	t.Helper()
	before := model.calls.Load()
	first := integrationStreamAnswer(t, fixture, snapshot, request)
	if first.Cached || model.calls.Load() != before+1 {
		t.Fatal("first explicit explanation did not invoke exactly one model call")
	}
	second := integrationStreamAnswer(t, fixture, snapshot, request)
	if !second.Cached || model.calls.Load() != before+1 || second.ID == first.ID || second.Evidence[0] != first.Evidence[0] {
		t.Fatal("cached explanation invoked inference, lost audit identity, or changed evidence")
	}
}

func assertMissingStreamFeature(t *testing.T, fixture *integrationExplorer, model *protocolModel, snapshot string) {
	t.Helper()
	before := model.calls.Load()
	body := `{"question":"Explain this feature","feature_id":"` + strings.Repeat("f", 64) + `"}`
	status := integrationPostJSON(t, fixture, "/api/snapshots/"+snapshot+"/answers/stream", body, nil)
	if status != 404 || model.calls.Load() != before {
		t.Fatal("missing feature was not rejected before stream/inference")
	}
}

func TestIntegrationAnalysisEventsTrackDurableJobAndSurviveDisconnect(t *testing.T) {
	fixture, model := newIntelligenceIntegration(t)
	model.pauseAt.Store(1)
	fixture.manager.Start(context.Background())
	snapshot := awaitIntegrationSnapshot(t, fixture, "")
	response := integrationStream(t, fixture, http.MethodGet, "/api/snapshots/"+snapshot.ID+"/events", "")
	reader := bufio.NewReader(response.Body)
	initial := decodeAnalysisEvent(t, assertObservedEvent(t, reader, "analysis"))
	if initial.Status != "not_queued" || initial.SnapshotID != snapshot.ID {
		t.Fatal("analysis stream did not start with its scoped durable status")
	}
	if err := response.Body.Close(); err != nil {
		t.Fatal(err)
	}
	queueIntegratedAnalysis(t, fixture, snapshot.ID)
	// Manual refresh reconnects immediately; idle subscriptions intentionally
	// poll less often, while a queued subscription receives active progress.
	response = integrationStream(t, fixture, http.MethodGet, "/api/snapshots/"+snapshot.ID+"/events", "")
	reader = bufio.NewReader(response.Body)
	if queued := decodeAnalysisEvent(t, assertObservedEvent(t, reader, "analysis")); queued.Status != "queued" {
		t.Fatal("refreshed subscription did not read queued status immediately")
	}
	fixture.worker.Start(context.Background())
	awaitProtocolPause(t, model)
	running := decodeAnalysisEvent(t, assertObservedEvent(t, reader, "analysis"))
	if running.Status != "running" || running.SnapshotID != snapshot.ID {
		t.Fatalf("job state was not streamed: %+v", running)
	}
	if err := response.Body.Close(); err != nil {
		t.Fatal(err)
	}
	close(model.resume)
	awaitIntegratedAnalysis(t, fixture, snapshot.ID, "completed")
}

func decodeAnalysisEvent(t *testing.T, event observedEvent) domain.AnalysisJob {
	t.Helper()
	var job domain.AnalysisJob
	if err := json.Unmarshal(event.Data, &job); err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{`"lease_token"`, `"trace_parent"`, `"source"`, `"prompt"`} {
		if strings.Contains(string(event.Data), field) {
			t.Fatalf("analysis event exposed internal data: %s", field)
		}
	}
	return job
}
