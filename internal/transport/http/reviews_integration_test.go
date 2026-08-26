package httptransport

import (
	"bufio"
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"developa/internal/domain"
	"github.com/jackc/pgx/v5"
)

func protocolReviewOutput(data string, invalid bool) string {
	var inputs []struct {
		ID         string `json:"id"`
		Parameters []struct {
			Position int `json:"position"`
		} `json:"parameters"`
	}
	_ = json.Unmarshal([]byte(data), &inputs)
	reviews := []any{}
	for _, input := range inputs {
		id := input.ID
		if invalid {
			id = strings.Repeat("f", 64)
		}
		parameters := []domain.ParameterReview{}
		if len(input.Parameters) > 0 {
			parameters = append(parameters, domain.ParameterReview{Position: input.Parameters[0].Position, Description: "The value passed through the captured function."})
		}
		reviews = append(reviews, map[string]any{"symbol_id": id, "summary": "Returns the value from the captured implementation.", "parameters": parameters, "insufficient_evidence": false})
	}
	encoded, _ := json.Marshal(map[string]any{"reviews": reviews})
	return string(encoded)
}

func TestIntegrationFunctionReviewStreamsPersistAndReuseBatches(t *testing.T) {
	fixture, model := newIntelligenceIntegration(t)
	integrationWrite(t, fixture.root, "main.go", integratedFlowSource)
	fixture.manager.Start(context.Background())
	snapshot := awaitIntegrationSnapshot(t, fixture, "")
	var initial domain.ReviewPage
	integrationRead(t, fixture, "/api/snapshots/"+snapshot.ID+"/function-reviews", &initial)
	if initial.Total != 4 || model.calls.Load() != 0 {
		t.Fatal("GET reviewed code or omitted functions")
	}
	page := integrationStreamReviews(t, fixture, snapshot.ID, domain.ReviewOptions{})
	if len(page.Items) != 4 || page.ModelCalls != 1 || model.calls.Load() != 1 {
		t.Fatal("functions were not generated in one batch")
	}
	assertReviewPublished(t, fixture, page)
	root := page.Items[0].Symbol.ID
	callees := integrationStreamReviews(t, fixture, snapshot.ID, domain.ReviewOptions{CalleeOf: root})
	if callees.Total != 1 || callees.CachedCount != 1 || model.calls.Load() != 1 {
		t.Fatalf("shared callee review was regenerated: %+v", callees)
	}
	assertReviewSnapshotRebinding(t, fixture, model, page)
}

func integrationStreamReviews(t *testing.T, fixture *integrationExplorer, snapshot string, options domain.ReviewOptions) domain.ReviewPage {
	t.Helper()
	body, err := json.Marshal(options)
	if err != nil {
		t.Fatal(err)
	}
	response := integrationStream(t, fixture, http.MethodPost, "/api/snapshots/"+snapshot+"/function-reviews/stream", string(body))
	reader := bufio.NewReader(response.Body)
	assertObservedEvent(t, reader, "started")
	event := assertObservedEvent(t, reader, "reviews")
	var page domain.ReviewPage
	if err := json.Unmarshal(event.Data, &page); err != nil {
		t.Fatal(err)
	}
	return page
}

func assertReviewPublished(t *testing.T, fixture *integrationExplorer, page domain.ReviewPage) {
	t.Helper()
	for _, item := range page.Items {
		stored := readIntegrationSymbol(t, fixture, page.SnapshotID, item.Symbol.ID)
		if stored.Review == nil || len(stored.Review.Parameters) != min(1, len(stored.Symbol.Parameters)) || stored.Review.Summary != item.Review.Summary {
			t.Fatal("SSE preceded durable review publication")
		}
	}
	var events int
	query := "SELECT count(*) FROM " + pgx.Identifier{fixture.schema, "developa_audit_events"}.Sanitize() + " WHERE trigger='review_functions' AND outcome='completed'"
	if err := fixture.admin.QueryRow(context.Background(), query).Scan(&events); err != nil || events != 1 {
		t.Fatal("review publication not audited")
	}
}

func assertReviewSnapshotRebinding(t *testing.T, fixture *integrationExplorer, model *protocolModel, previous domain.ReviewPage) {
	t.Helper()
	integrationWrite(t, fixture.root, "main.go", "\n"+integratedFlowSource)
	snapshot := awaitIntegrationSnapshot(t, fixture, previous.SnapshotID)
	page := integrationStreamReviews(t, fixture, snapshot.ID, domain.ReviewOptions{})
	if page.CachedCount != 4 || model.calls.Load() != 1 {
		t.Fatal("line shifts invalidated per-function cache")
	}
	if page.Items[0].Review.Evidence.Span.Start.Line != previous.Items[0].Review.Evidence.Span.Start.Line+1 {
		t.Fatal("stale cache citation")
	}
	old := readIntegrationSymbol(t, fixture, previous.SnapshotID, previous.Items[0].Symbol.ID)
	if old.Review.Evidence.Span != previous.Items[0].Review.Evidence.Span {
		t.Fatal("old snapshot review followed current source")
	}
}

func TestIntegrationReviewValidationAndMissingScopeNeverPublish(t *testing.T) {
	fixture, model := newIntelligenceIntegration(t)
	fixture.manager.Start(context.Background())
	snapshot := awaitIntegrationSnapshot(t, fixture, "")
	base := "/api/snapshots/" + snapshot.ID + "/function-reviews"
	for _, suffix := range []string{"", "/stream"} {
		status := integrationPostJSON(t, fixture, base+suffix, `{"symbol_id":"`+strings.Repeat("f", 64)+`"}`, nil)
		if status != http.StatusNotFound {
			t.Fatalf("missing scope returned %d", status)
		}
	}
	if model.calls.Load() != 0 {
		t.Fatal("missing scope reached inference")
	}
	model.invalid.Store(true)
	status := integrationPostJSON(t, fixture, base, `{}`, nil)
	if status != http.StatusBadGateway {
		t.Fatalf("invalid output returned %d", status)
	}
	var page domain.ReviewPage
	integrationRead(t, fixture, base, &page)
	for _, item := range page.Items {
		if item.Review != nil {
			t.Fatal("invalid batch partially published")
		}
	}
}
