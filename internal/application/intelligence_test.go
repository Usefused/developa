package application

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"developa/internal/domain"
	goparser "developa/internal/indexer/golang"
)

type intelligenceTestStore struct {
	domain.IntelligenceStore
	symbols      []domain.SymbolDetail
	pages        []domain.Filter
	run          domain.FeatureRun
	features     []domain.Feature
	answer       domain.Answer
	outcomes     []string
	executions   []domain.Execution
	symbolReads  int
	contextReads int
	saveError    error
	auditError   error
	auditBlock   bool
}

func (s *intelligenceTestStore) Features(context.Context, string, string, domain.Filter) (domain.FeaturePage, error) {
	if s.run.ID == "" {
		return domain.FeaturePage{}, nil
	}
	run := s.run
	return domain.FeaturePage{Run: &run}, nil
}

func intelligenceFacts(count int) []domain.SymbolDetail {
	facts := make([]domain.SymbolDetail, 0, count)
	for index := 0; index < count; index++ {
		facts = append(facts, domain.SymbolDetail{Path: fmt.Sprintf("file%d.go", index), Symbol: goparser.Symbol{ID: fmt.Sprintf("%064x", index+1), Name: fmt.Sprintf("Run%d", index), Kind: goparser.Function, Signature: "func Run()", Source: "func Run() {}", Span: goparser.Span{Start: goparser.Position{Line: index + 2}, End: goparser.Position{Line: index + 3}}}})
	}
	return facts
}

func (s *intelligenceTestStore) Symbols(_ context.Context, _ string, _ string, filter domain.Filter) (domain.SymbolPage, error) {
	s.pages = append(s.pages, filter)
	end := min(filter.Offset+filter.Limit, len(s.symbols))
	return domain.SymbolPage{Items: s.symbols[filter.Offset:end], Total: len(s.symbols)}, nil
}

func (s *intelligenceTestStore) Symbol(_ context.Context, _ string, _ string, id string) (domain.SymbolDetail, error) {
	s.symbolReads++
	for _, symbol := range s.symbols {
		if symbol.Symbol.ID == id {
			return symbol, nil
		}
	}
	return domain.SymbolDetail{}, domain.ErrNotFound
}

func (s *intelligenceTestStore) Context(_ context.Context, repo, snapshot, query string, limit int) (domain.ContextPack, error) {
	s.contextReads++
	count := min(limit, len(s.symbols))
	return domain.ContextPack{RepositoryID: repo, SnapshotID: snapshot, Query: query, Items: s.symbols[:count], Total: len(s.symbols), Truncated: count < len(s.symbols)}, nil
}

func (s *intelligenceTestStore) SaveFeatures(_ context.Context, _ string, run domain.FeatureRun, features []domain.Feature, execution domain.Execution) error {
	if s.saveError != nil {
		return s.saveError
	}
	if run.ParentRunID != "" {
		features = append(append([]domain.Feature(nil), s.features...), features...)
	}
	s.run, s.features = run, features
	s.outcomes = append(s.outcomes, "completed")
	s.executions = append(s.executions, execution)
	return nil
}

func (s *intelligenceTestStore) SaveAnswer(_ context.Context, _ string, answer domain.Answer, execution domain.Execution) error {
	if s.saveError != nil {
		return s.saveError
	}
	s.answer = answer
	s.outcomes = append(s.outcomes, "completed")
	s.executions = append(s.executions, execution)
	return nil
}

func (s *intelligenceTestStore) RecordExecution(ctx context.Context, _ string, execution domain.Execution, outcome string) error {
	if s.auditBlock && outcome == "running" {
		<-ctx.Done()
		return ctx.Err()
	}
	if s.auditError != nil {
		return s.auditError
	}
	s.outcomes = append(s.outcomes, outcome)
	s.executions = append(s.executions, execution)
	return nil
}

type intelligenceTestModel struct {
	generate func(context.Context, string, string, json.RawMessage) (json.RawMessage, error)
	calls    atomic.Int32
	identity string
}

func (m *intelligenceTestModel) Model() string {
	if m.identity != "" {
		return m.identity
	}
	return "local-fixture"
}
func (m *intelligenceTestModel) Generate(ctx context.Context, system, prompt string, schema json.RawMessage) (json.RawMessage, error) {
	m.calls.Add(1)
	return m.generate(ctx, system, prompt, schema)
}

func featureTestModel() *intelligenceTestModel {
	return &intelligenceTestModel{generate: func(_ context.Context, _ string, prompt string, _ json.RawMessage) (json.RawMessage, error) {
		var items []promptSymbol
		if err := json.Unmarshal([]byte(strings.SplitN(prompt, "DATA:\n", 2)[1]), &items); err != nil {
			return nil, err
		}
		return json.Marshal(map[string]any{"features": []any{map[string]any{"title": "Capability " + clipText(items[0].Name, 100), "summary": "Implements the supplied function.", "symbol_ids": []string{items[0].ID}}}})
	}}
}

func fixtureIntelligence(t *testing.T, store *intelligenceTestStore, model StructuredModel, cfg IntelligenceConfig) *IntelligenceService {
	t.Helper()
	cfg.RepositoryID = "repository"
	service, err := NewIntelligence(store, model, cfg)
	if err != nil {
		t.Fatal(err)
	}
	return service
}

func TestDiscoverBatchesAllSymbolsAndCanonicalizesEvidence(t *testing.T) {
	store := &intelligenceTestStore{symbols: intelligenceFacts(5)}
	model := featureTestModel()
	service := fixtureIntelligence(t, store, model, IntelligenceConfig{BatchSize: 2})
	run, err := service.Discover(context.Background(), "snapshot")
	if err != nil {
		t.Fatal(err)
	}
	if run.Status != "completed" || run.AnalyzedSymbols != 5 || run.TotalSymbols != 5 || model.calls.Load() != 3 {
		t.Fatalf("coverage: %+v", run)
	}
	for index, page := range store.pages {
		if page.Limit != 2 || page.Offset != index*2 {
			t.Fatal("unbounded or overlapping page")
		}
	}
	assertCanonicalFeature(t, store.features[0])
	assertIntelligenceAudit(t, store, "completed")
}

func assertCanonicalFeature(t *testing.T, feature domain.Feature) {
	t.Helper()
	if feature.Status != "inferred" || len(feature.ID) != 64 || feature.Evidence[0].Path != "file0.go" || feature.Evidence[0].Span.Start.Line != 2 {
		t.Fatalf("evidence not canonical: %+v", feature)
	}
}

func TestDiscoverBudgetsAndTruncatedSourceAreExplicit(t *testing.T) {
	store := &intelligenceTestStore{symbols: intelligenceFacts(5)}
	service := fixtureIntelligence(t, store, featureTestModel(), IntelligenceConfig{BatchSize: 2, MaxModelCalls: 1})
	run, err := service.Discover(context.Background(), "snapshot")
	if err != nil {
		t.Fatal(err)
	}
	if run.Status != "partial" || run.AnalyzedSymbols != 2 || run.TotalSymbols != 5 {
		t.Fatal("call cap misrepresented coverage")
	}
	store.symbols = store.symbols[:1]
	store.run = domain.FeatureRun{}
	store.symbols[0].Symbol.SourceTruncated = true
	run, err = service.Discover(context.Background(), "snapshot")
	if err != nil || run.Status != "partial" {
		t.Fatalf("source truncation concealed: %+v %v", run, err)
	}
}

func TestDiscoverContextBudgetDoesNotPretendToCoverSymbols(t *testing.T) {
	store := &intelligenceTestStore{symbols: intelligenceFacts(1)}
	store.symbols[0].Symbol.Source = strings.Repeat("x", 2048)
	model := featureTestModel()
	service := fixtureIntelligence(t, store, model, IntelligenceConfig{MaxContextBytes: 1024})
	run, err := service.Discover(context.Background(), "snapshot")
	if err != nil || run.Status != "partial" || run.AnalyzedSymbols != 1 || model.calls.Load() != 1 {
		t.Fatalf("byte budget violated: %+v %v", run, err)
	}
}

func TestDiscoverFailureRetainsPriorRunAndAudits(t *testing.T) {
	store := &intelligenceTestStore{symbols: intelligenceFacts(1), run: domain.FeatureRun{ID: "prior"}}
	model := &intelligenceTestModel{generate: func(context.Context, string, string, json.RawMessage) (json.RawMessage, error) {
		return json.RawMessage(`{"features":[{"title":"Invented","summary":"No proof","symbol_ids":["hallucinated"]}]}`), nil
	}}
	service := fixtureIntelligence(t, store, model, IntelligenceConfig{})
	if _, err := service.Discover(context.Background(), "snapshot"); !errors.Is(err, domain.ErrInvalidModelOutput) {
		t.Fatal(err)
	}
	if store.run.ID != "prior" {
		t.Fatal("failed generation replaced existing feature run")
	}
	assertIntelligenceAudit(t, store, "error")
}

func assertIntelligenceAudit(t *testing.T, store *intelligenceTestStore, outcome string) {
	t.Helper()
	if len(store.outcomes) != 2 || store.outcomes[0] != "running" || store.outcomes[1] != outcome {
		t.Fatalf("audit lifecycle: %v", store.outcomes)
	}
	if store.executions[0].Actor != "operator" || store.executions[0].ID != store.executions[1].ID {
		t.Fatal("audit correlation missing")
	}
}

func TestIntelligenceDeadlineAndBusyGate(t *testing.T) {
	store := &intelligenceTestStore{symbols: intelligenceFacts(1)}
	entered := make(chan struct{})
	model := &intelligenceTestModel{generate: func(ctx context.Context, _ string, _ string, _ json.RawMessage) (json.RawMessage, error) {
		close(entered)
		<-ctx.Done()
		return nil, ctx.Err()
	}}
	service := fixtureIntelligence(t, store, model, IntelligenceConfig{Timeout: 100 * time.Millisecond})
	done := make(chan error, 1)
	go func() { _, err := service.Discover(context.Background(), "snapshot"); done <- err }()
	<-entered
	if _, err := service.Discover(context.Background(), "snapshot"); !errors.Is(err, domain.ErrBusy) {
		t.Fatalf("concurrency cap failed: %v", err)
	}
	if err := <-done; !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal(err)
	}
	assertIntelligenceAudit(t, store, "error")
}

func TestIntelligenceRequiresDurableStartBeforeModel(t *testing.T) {
	store := &intelligenceTestStore{symbols: intelligenceFacts(1), auditError: errors.New("unavailable")}
	model := featureTestModel()
	service := fixtureIntelligence(t, store, model, IntelligenceConfig{})
	if _, err := service.Discover(context.Background(), "snapshot"); err == nil || model.calls.Load() != 0 {
		t.Fatal("model ran before durable execution start")
	}
	disabled := fixtureIntelligence(t, store, nil, IntelligenceConfig{})
	if disabled.Available() {
		t.Fatal("unconfigured service reported ready")
	}
	if _, err := disabled.Discover(context.Background(), "snapshot"); !errors.Is(err, domain.ErrModelUnavailable) {
		t.Fatal(err)
	}
}
