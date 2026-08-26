package application

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"developa/internal/domain"
	goparser "developa/internal/indexer/golang"
)

type reviewTestStore struct {
	*intelligenceTestStore
	cache      map[string]json.RawMessage
	saved      []domain.FunctionReview
	cacheReads int
}

func (s *reviewTestStore) ReviewPage(_ context.Context, _ string, snapshot string, options domain.ReviewOptions) (domain.ReviewPage, error) {
	end := min(len(s.symbols), options.Offset+options.Limit)
	items := append([]domain.SymbolDetail(nil), s.symbols[options.Offset:end]...)
	return domain.ReviewPage{SnapshotID: snapshot, Options: options, Items: items, Total: len(s.symbols)}, nil
}

func (s *reviewTestStore) CachedReviews(_ context.Context, _ string, keys []string) (map[string]json.RawMessage, error) {
	s.cacheReads++
	result := make(map[string]json.RawMessage)
	for _, key := range keys {
		if data, ok := s.cache[key]; ok {
			result[key] = data
		}
	}
	return result, nil
}

func (s *reviewTestStore) SaveReviews(_ context.Context, _ string, _ string, reviews []domain.FunctionReview, entries []domain.ReviewCacheEntry, execution domain.Execution) error {
	if s.saveError != nil {
		return s.saveError
	}
	s.saved = reviews
	for _, entry := range entries {
		s.cache[entry.Key] = entry.Payload
	}
	s.executions = append(s.executions, execution)
	return nil
}

func reviewFixture(t *testing.T, count int) (*IntelligenceService, *reviewTestStore, *intelligenceTestModel) {
	t.Helper()
	facts := intelligenceFacts(count)
	for i := range facts {
		facts[i].Symbol.SourceID = strings.Repeat("a", 64)
		facts[i].Symbol.Parameters = []goparser.Parameter{{Position: 0, Name: "value", Type: "string"}}
	}
	store := &reviewTestStore{intelligenceTestStore: &intelligenceTestStore{symbols: facts}, cache: make(map[string]json.RawMessage)}
	model := &intelligenceTestModel{identity: "fixture@sha256:" + strings.Repeat("c", 64), generate: reviewTestOutput}
	service, err := NewIntelligence(store, model, IntelligenceConfig{RepositoryID: "repository", Timeout: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	return service, store, model
}

func reviewTestOutput(_ context.Context, _ string, prompt string, _ json.RawMessage) (json.RawMessage, error) {
	_, data, _ := strings.Cut(prompt, "DATA:\n")
	var inputs []reviewInput
	if err := json.Unmarshal([]byte(data), &inputs); err != nil {
		return nil, err
	}
	outputs := make([]json.RawMessage, 0, len(inputs))
	for _, input := range inputs {
		notes := []domain.ParameterReview{}
		if len(input.Parameters) > 0 {
			notes = append(notes, domain.ParameterReview{Position: input.Parameters[0].Position, Description: "Value used by this function."})
		}
		outputs = append(outputs, reviewCachePayload(domain.FunctionReview{SymbolID: input.ID, Summary: "Performs the captured operation.", Parameters: notes}))
	}
	return json.Marshal(map[string]any{"reviews": outputs})
}

func TestFunctionReviewBatchesAndReusesEachFunctionIndependently(t *testing.T) {
	service, store, model := reviewFixture(t, 4)
	page, err := service.Review(context.Background(), "snapshot", domain.ReviewOptions{})
	if err != nil || page.ModelCalls != 1 || len(page.Items) != 4 || model.calls.Load() != 1 {
		t.Fatalf("batch failed: %+v %v", page, err)
	}
	if len(store.saved) != 4 || len(store.cache) != 4 || store.cacheReads != 1 {
		t.Fatal("reviews were not saved/cache-read as a batch")
	}
	assertReviewNotes(t, page)
	assertReviewRebinding(t, service, store, model)
	assertChangedReview(t, service, store, model)
}

func TestReviewPromptIncludesCompiledCommentsAndParameterContext(t *testing.T) {
	service, store, model := reviewFixture(t, 1)
	store.symbols[0].Symbol.Documentation = &goparser.Documentation{Summary: "Run accepts a value.\n\nReject blank input before sending."}
	model.generate = func(ctx context.Context, system, prompt string, schema json.RawMessage) (json.RawMessage, error) {
		_, data, _ := strings.Cut(prompt, "DATA:\n")
		var inputs []reviewInput
		if err := json.Unmarshal([]byte(data), &inputs); err != nil {
			t.Fatal(err)
		}
		if len(inputs) != 1 || inputs[0].Source == "" || inputs[0].Signature == "" || inputs[0].Parameters[0].Name != "value" {
			t.Fatal("review lost captured implementation or parameter context")
		}
		if !strings.Contains(inputs[0].Doc, "Reject blank input") || !strings.Contains(system, "comments may be stale") {
			t.Fatal("compiled comments absent or incorrectly treated as verified behavior")
		}
		return reviewTestOutput(ctx, system, prompt, schema)
	}
	if _, err := service.Review(context.Background(), "snapshot", domain.ReviewOptions{}); err != nil {
		t.Fatal(err)
	}
}

func assertReviewNotes(t *testing.T, page domain.ReviewPage) {
	t.Helper()
	for _, item := range page.Items {
		if item.Review == nil || len(item.Review.Parameters) != 1 {
			t.Fatal("missing description or supported parameter note")
		}
	}
}

func assertChangedReview(t *testing.T, service *IntelligenceService, store *reviewTestStore, model *intelligenceTestModel) {
	t.Helper()
	store.symbols[2].Symbol.Source = "func Changed() { return }"
	page, err := service.Review(context.Background(), "snapshot-new", domain.ReviewOptions{})
	if err != nil || page.CachedCount != 3 || page.ModelCalls != 1 || model.calls.Load() != 2 {
		t.Fatalf("unchanged functions were reanalyzed: %+v %v", page, err)
	}
}

func assertReviewRebinding(t *testing.T, service *IntelligenceService, store *reviewTestStore, model *intelligenceTestModel) {
	t.Helper()
	store.symbols[0].Symbol.Span.Start.Line = 87
	store.symbols[0].Symbol.SourceID = strings.Repeat("b", 64)
	page, err := service.Review(context.Background(), "later-snapshot", domain.ReviewOptions{Limit: 2})
	if err != nil || page.CachedCount != 2 || page.ModelCalls != 0 || model.calls.Load() != 1 {
		t.Fatalf("cross-page reuse failed: %+v %v", page, err)
	}
	review := page.Items[0].Review
	if review.Evidence.Span.Start.Line != 87 || review.SourceID != store.symbols[0].Symbol.SourceID {
		t.Fatal("cache returned stale snapshot positions")
	}
}

func TestFunctionReviewCacheInvalidatesCommentsAndModel(t *testing.T) {
	service, store, model := reviewFixture(t, 1)
	if _, err := service.Review(context.Background(), "snapshot", domain.ReviewOptions{}); err != nil {
		t.Fatal(err)
	}
	store.symbols[0].Symbol.Comment = "Updated evidence"
	if _, err := service.Review(context.Background(), "snapshot", domain.ReviewOptions{}); err != nil {
		t.Fatal(err)
	}
	model.identity = "fixture@sha256:" + strings.Repeat("d", 64)
	if _, err := service.Review(context.Background(), "snapshot", domain.ReviewOptions{}); err != nil {
		t.Fatal(err)
	}
	if model.calls.Load() != 3 {
		t.Fatal("changed evidence/model reused stale reviews")
	}
}

func TestFunctionReviewBudgetHasOneCallAndProgressCursor(t *testing.T) {
	service, store, model := reviewFixture(t, 8)
	service.cfg.MaxContextBytes = 1024
	for i := range store.symbols {
		store.symbols[i].Symbol.Source = strings.Repeat("large source\n", 700)
	}
	page, err := service.Review(context.Background(), "snapshot", domain.ReviewOptions{Limit: 8})
	if err != nil || len(page.Items) != 1 || model.calls.Load() != 1 {
		t.Fatalf("unbounded batch: %+v %v", page, err)
	}
	if page.NextOffset == nil || *page.NextOffset != 1 || !page.Items[0].Review.ContextTruncated {
		t.Fatal("clipped batch lost its continuation/truncation")
	}
}

func TestFunctionReviewRejectsMissingInventedAndDuplicateEvidence(t *testing.T) {
	inputs, err := reviewInputs(intelligenceFacts(1), 16384)
	if err != nil {
		t.Fatal(err)
	}
	inputs[0].input.Parameters = []reviewParameter{{Position: 0, Name: "value"}}
	id := inputs[0].fact.Symbol.ID
	valid := `{"symbol_id":"` + id + `","summary":"Uses value.","parameters":[],"insufficient_evidence":false}`
	cases := []string{
		`{"reviews":[]}`, `{"reviews":[` + valid + `,` + valid + `]}`,
		strings.Replace(`{"reviews":[`+valid+`]}`, id, "unknown", 1),
		strings.Replace(`{"reviews":[`+valid+`]}`, `"parameters":[]`, `"parameters":[{"description":"missing position"}]`, 1),
		strings.Replace(`{"reviews":[`+valid+`]}`, `"parameters":[]`, `"parameters":[{"position":4,"description":"invented"}]`, 1),
		strings.Replace(`{"reviews":[`+valid+`]}`, `"parameters":[]`, `"parameters":[{"position":0,"description":"a"},{"position":0,"description":"b"}]`, 1),
		`{"reviews":[{"symbol_id":"` + id + `","summary":"missing required fields"}]}`,
	}
	for _, data := range cases {
		if _, err := validateReviewBatch([]byte(data), inputs, "fixture"); !errors.Is(err, domain.ErrInvalidModelOutput) {
			t.Fatalf("accepted invalid output: %s %v", data, err)
		}
	}
}

func TestFunctionReviewFailureNeverPublishesAndRemainsAudited(t *testing.T) {
	service, store, model := reviewFixture(t, 2)
	model.generate = func(context.Context, string, string, json.RawMessage) (json.RawMessage, error) {
		return []byte(`{"reviews":[]}`), nil
	}
	if _, err := service.Review(context.Background(), "snapshot", domain.ReviewOptions{}); !errors.Is(err, domain.ErrInvalidModelOutput) {
		t.Fatal(err)
	}
	if len(store.saved) != 0 || len(store.cache) != 0 || store.outcomes[len(store.outcomes)-1] != "error" {
		t.Fatal("invalid result published or failure not audited")
	}
}

func TestFunctionReviewCancellationAndPersistenceFailure(t *testing.T) {
	service, store, model := reviewFixture(t, 1)
	model.generate = func(ctx context.Context, _ string, _ string, _ json.RawMessage) (json.RawMessage, error) {
		<-ctx.Done()
		return nil, ctx.Err()
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := service.Review(ctx, "snapshot", domain.ReviewOptions{}); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	model.generate = reviewTestOutput
	store.saveError = errors.New("publication failed")
	if _, err := service.Review(context.Background(), "snapshot", domain.ReviewOptions{}); err == nil {
		t.Fatal("ignored persistence failure")
	}
	if len(store.saved) != 0 || len(store.cache) != 0 {
		t.Fatal("failed request published reviews")
	}
}

func TestEmptyFunctionReviewPageSkipsModel(t *testing.T) {
	service, _, model := reviewFixture(t, 0)
	page, err := service.Review(context.Background(), "snapshot", domain.ReviewOptions{})
	if err != nil || page.ModelCalls != 0 || model.calls.Load() != 0 || page.NextOffset != nil {
		t.Fatalf("empty page invoked model: %+v %v", page, err)
	}
}
