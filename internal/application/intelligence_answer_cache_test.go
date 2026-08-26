package application

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"testing"

	"developa/internal/domain"
)

func answerCacheFixture(t *testing.T, count int, cfg IntelligenceConfig) (*IntelligenceService, *cacheTestStore, *resolvingTestModel) {
	t.Helper()
	_, store, model := cacheFixture(t, count, cfg)
	id := "unused"
	if count > 0 {
		id = store.symbols[0].Symbol.ID
	}
	model.intelligenceTestModel = answerTestModel(id)
	return cacheService(t, store, model, cfg), store, model
}

func cachedAnswer(t *testing.T, service *IntelligenceService, snapshot string, request domain.AnswerRequest) domain.Answer {
	t.Helper()
	answer, err := service.Answer(context.Background(), snapshot, request)
	if err != nil {
		t.Fatal(err)
	}
	return answer
}

func TestAnswerCacheSkipsInferenceAndRebindsCurrentCitations(t *testing.T) {
	service, store, model := answerCacheFixture(t, 1, IntelligenceConfig{})
	request := domain.AnswerRequest{Question: "Explain this function", SymbolID: store.symbols[0].Symbol.ID}
	first := cachedAnswer(t, service, "first-snapshot", request)
	store.symbols[0].Symbol.Span.Start.Line = 42
	store.symbols[0].Symbol.Span.End.Line = 44
	second := cachedAnswer(t, service, "second-snapshot", request)
	if first.Cached || !second.Cached || model.calls.Load() != 1 || model.resolves != 2 {
		t.Fatal("repeated explanation consumed inference or hid cache reuse")
	}
	if first.ID == second.ID || second.Evidence[0].Span.Start.Line != 42 || store.answer.ID != second.ID {
		t.Fatal("cached explanation reused old identity or source positions")
	}
	assertCachedAnswerPublication(t, store)
}

func assertCachedAnswerPublication(t *testing.T, store *cacheTestStore) {
	t.Helper()
	if store.reads != 2 || store.writes != 1 || len(store.outcomes) != 4 || !store.answer.Cached {
		t.Fatal("cached request did not retain bounded cache access and durable publication")
	}
}

func TestAnswerCacheInvalidatesQuestionSourceModelPolicyAndRepository(t *testing.T) {
	service, store, model := answerCacheFixture(t, 1, IntelligenceConfig{})
	request := domain.AnswerRequest{Question: "Explain"}
	cachedAnswer(t, service, "snapshot", request)
	request.Question = "Explain the return value"
	cachedAnswer(t, service, "snapshot", request)
	store.symbols[0].Symbol.Source = "func Run() { changed() }"
	cachedAnswer(t, service, "snapshot", request)
	model.resolved = "fixture@sha256:" + strings.Repeat("b", 64)
	cachedAnswer(t, service, "snapshot", request)
	model.policy = "fixture-v2"
	cachedAnswer(t, service, "snapshot", request)
	otherRepo := cacheService(t, store, model, IntelligenceConfig{RepositoryID: "other-repository"})
	cachedAnswer(t, otherRepo, "snapshot", request)
	if model.calls.Load() != 6 || store.writes != 6 {
		t.Fatal("changed question, evidence, model, or authorization scope reused stale answers")
	}
}

func TestAnswerCacheInvalidatesChangesBeyondCapturedExcerpt(t *testing.T) {
	service, store, model := answerCacheFixture(t, 1, IntelligenceConfig{})
	store.symbols[0].Symbol.SourceTruncated = true
	store.symbols[0].Symbol.ContentHash = strings.Repeat("a", 64)
	request := domain.AnswerRequest{Question: "Explain", SymbolID: store.symbols[0].Symbol.ID}
	cachedAnswer(t, service, "old", request)
	store.symbols[0].Symbol.ContentHash = strings.Repeat("b", 64)
	answer := cachedAnswer(t, service, "new", request)
	if answer.Cached || !answer.ContextTruncated || model.calls.Load() != 2 {
		t.Fatal("a changed declaration reused stale inference from an unchanged prefix")
	}
}

func TestAnswerCacheRejectsInvalidEvidenceWithoutGeneratingOrPublishing(t *testing.T) {
	service, store, model := answerCacheFixture(t, 1, IntelligenceConfig{})
	request := domain.AnswerRequest{Question: "Explain"}
	previous := cachedAnswer(t, service, "snapshot", request)
	for key := range store.entries {
		store.entries[key] = json.RawMessage(`{"text":"Unsupported","symbol_ids":["absent"],"insufficient_evidence":false}`)
	}
	_, err := service.Answer(context.Background(), "snapshot", request)
	if !errors.Is(err, domain.ErrInvalidModelOutput) || model.calls.Load() != 1 || store.answer.ID != previous.ID {
		t.Fatal("invalid cached answer was repaired, accepted, or replaced valid output")
	}
}

func TestAnswerCacheNeverStoresInvalidModelOutput(t *testing.T) {
	service, store, model := answerCacheFixture(t, 1, IntelligenceConfig{})
	model.generate = func(context.Context, string, string, json.RawMessage) (json.RawMessage, error) {
		return json.RawMessage(`{"text":"Unsupported","symbol_ids":["absent"],"insufficient_evidence":false}`), nil
	}
	_, err := service.Answer(context.Background(), "snapshot", domain.AnswerRequest{Question: "Explain"})
	if !errors.Is(err, domain.ErrInvalidModelOutput) || store.writes != 0 || store.answer.ID != "" {
		t.Fatal("invalid generated explanation entered the cache or answer history")
	}
}

func TestAnswerCacheSurvivesPublicationFailureWithoutRepeatingInference(t *testing.T) {
	service, store, model := answerCacheFixture(t, 1, IntelligenceConfig{})
	store.saveError = errors.New("answer publication failed")
	request := domain.AnswerRequest{Question: "Explain"}
	if _, err := service.Answer(context.Background(), "snapshot", request); err == nil {
		t.Fatal("failed answer publication reported success")
	}
	store.saveError = nil
	answer := cachedAnswer(t, service, "snapshot", request)
	if !answer.Cached || model.calls.Load() != 1 || store.answer.ID != answer.ID {
		t.Fatal("publication retry spent another model call")
	}
}

func TestCachedCloudAnswerDisclosesReuseWithoutFreshSourceTransfer(t *testing.T) {
	service, _, model := answerCacheFixture(t, 1, IntelligenceConfig{})
	model.resolved = "fixture@cloud:aabbccddeeff"
	request := domain.AnswerRequest{Question: "Explain"}
	first := cachedAnswer(t, service, "snapshot", request)
	second := cachedAnswer(t, service, "snapshot", request)
	assertCloudProvenance(t, first.Limitations)
	if !second.Cached || !slices.Contains(second.Limitations, cloudAnswerCachedLimitation) || slices.Contains(second.Limitations, cloudTransferLimitation) {
		t.Fatal("cached cloud answer claimed new source transfer")
	}
}

func TestAnswerCacheResolverFailureDoesNotInvokeInference(t *testing.T) {
	service, store, model := answerCacheFixture(t, 1, IntelligenceConfig{})
	request := domain.AnswerRequest{Question: "Explain"}
	previous := cachedAnswer(t, service, "snapshot", request)
	model.resolveErr = context.DeadlineExceeded
	_, err := service.Answer(context.Background(), "snapshot", request)
	if !errors.Is(err, context.DeadlineExceeded) || model.calls.Load() != 1 || store.answer.ID != previous.ID {
		t.Fatal("metadata failure retried inference or replaced previous output")
	}
}

func TestAnswerAndFeatureCachePurposesRemainSeparate(t *testing.T) {
	input := json.RawMessage(`{"same":"input"}`)
	featureKey := inferenceCacheKey("repo", "model", "policy", "features-v1", "task", answerSchema, input)
	answerKey := inferenceCacheKey("repo", "model", "policy", "answers-v1", "task", answerSchema, input)
	if featureKey == answerKey {
		t.Fatal("answer and feature purposes shared cache entries")
	}
}

func TestAnswerOversizedCanonicalMetadataRefusedBeforeInference(t *testing.T) {
	for _, name := range []string{strings.Repeat("Name", 2200), strings.Repeat("<", 1500)} {
		service, store, model := answerCacheFixture(t, 1, IntelligenceConfig{})
		store.symbols[0].Symbol.Name = name
		_, err := service.Answer(context.Background(), "snapshot", domain.AnswerRequest{Question: "Explain"})
		if !errors.Is(err, domain.ErrInvalidInput) || model.calls.Load() != 0 || model.resolves != 0 {
			t.Fatal("unrepresentable canonical metadata reached the model")
		}
		if store.answer.ID != "" || store.writes != 0 {
			t.Fatal("unrepresentable canonical metadata entered answer history or cache")
		}
	}
}

func TestCanonicalEvidenceRejectsOversizedMetadataOnCachedValidation(t *testing.T) {
	facts := intelligenceFacts(1)
	facts[0].Path = strings.Repeat("directory/", 900) + "file.go"
	if _, err := canonicalEvidence([]string{facts[0].Symbol.ID}, facts, true); !errors.Is(err, domain.ErrInvalidInput) {
		t.Fatal("cached output bypassed canonical citation bounds")
	}
}

type savedAnswerTestStore struct {
	*cacheTestStore
	answers map[string]domain.Answer
}

func (s *savedAnswerTestStore) SaveAnswer(ctx context.Context, repository string, answer domain.Answer, execution domain.Execution) error {
	if err := s.cacheTestStore.SaveAnswer(ctx, repository, answer, execution); err != nil {
		return err
	}
	s.answers[repository+":"+answer.ContextKey] = answer
	return nil
}

func (s *savedAnswerTestStore) SavedAnswer(_ context.Context, repository, snapshot, key string) (domain.Answer, error) {
	answer, ok := s.answers[repository+":"+key]
	if !ok {
		return domain.Answer{}, domain.ErrNotFound
	}
	answer.GeneratedSnapshotID, answer.SnapshotID = answer.SnapshotID, snapshot
	return answer, nil
}

func savedAnswerFixture(t *testing.T) (*IntelligenceService, *savedAnswerTestStore, *resolvingTestModel, domain.AnswerRequest) {
	t.Helper()
	_, original, model := answerCacheFixture(t, 1, IntelligenceConfig{})
	store := &savedAnswerTestStore{cacheTestStore: original, answers: map[string]domain.Answer{}}
	store.symbols[0].Symbol.ContentHash = strings.Repeat("a", 64)
	request := domain.AnswerRequest{Question: "Explain this function", SymbolID: store.symbols[0].Symbol.ID}
	return cacheService(t, store, model, IntelligenceConfig{}), store, model, request
}

func TestSavedAnswerReadSurvivesRestartWithoutModelOrPublication(t *testing.T) {
	service, store, model, request := savedAnswerFixture(t)
	first := cachedAnswer(t, service, "first", request)
	outcomes := len(store.outcomes)
	store.symbols[0].Symbol.Span.Start.Line = 42
	restarted := cacheService(t, store, nil, IntelligenceConfig{})
	answer, err := restarted.SavedAnswer(context.Background(), "next", request)
	if err != nil || answer == nil {
		t.Fatalf("saved answer unavailable without model: %v", err)
	}
	assertSavedReadProvenance(t, answer, first)
	if answer.Evidence[0].Span.Start.Line != 42 || len(store.outcomes) != outcomes || model.calls.Load() != 1 || model.resolves != 1 {
		t.Fatal("read used old positions, published new work, or contacted model metadata/inference")
	}
}

func assertSavedReadProvenance(t *testing.T, answer *domain.Answer, first domain.Answer) {
	t.Helper()
	if answer.ID != first.ID || answer.SnapshotID != "next" || answer.GeneratedSnapshotID != "first" || !answer.Cached {
		t.Fatal("saved answer lost publication provenance or selected snapshot")
	}
}

func TestSavedAnswerInvalidatesFullFunctionHashQuestionAndRepository(t *testing.T) {
	service, store, model, request := savedAnswerFixture(t)
	store.symbols[0].Symbol.SourceTruncated = true
	cachedAnswer(t, service, "first", request)
	store.symbols[0].Symbol.ContentHash = strings.Repeat("b", 64)
	assertNoSavedAnswer(t, service, request)
	store.symbols[0].Symbol.ContentHash = strings.Repeat("a", 64)
	other := cacheService(t, store, nil, IntelligenceConfig{RepositoryID: "other"})
	assertNoSavedAnswer(t, other, request)
	request.Question = "A different question"
	assertNoSavedAnswer(t, service, request)
	if model.calls.Load() != 1 || model.resolves != 1 {
		t.Fatal("lookup misses performed model work")
	}
}

func assertNoSavedAnswer(t *testing.T, service *IntelligenceService, request domain.AnswerRequest) {
	t.Helper()
	answer, err := service.SavedAnswer(context.Background(), "next", request)
	if err != nil || answer != nil {
		t.Fatalf("stale explanation was returned: %v", err)
	}
}

func TestSavedAnswerRejectsMissingTargetAndUnsupportedEvidence(t *testing.T) {
	service, store, _, request := savedAnswerFixture(t)
	first := cachedAnswer(t, service, "first", request)
	stored := store.answers["repository:"+first.ContextKey]
	stored.Evidence = []domain.Citation{{SymbolID: "not-in-context"}}
	store.answers["repository:"+first.ContextKey] = stored
	if _, err := service.SavedAnswer(context.Background(), "first", request); !errors.Is(err, domain.ErrInvalidModelOutput) {
		t.Fatal("saved answer bypassed canonical evidence validation")
	}
	request.SymbolID = strings.Repeat("f", 64)
	if _, err := service.SavedAnswer(context.Background(), "first", request); !errors.Is(err, domain.ErrNotFound) {
		t.Fatal("missing target returned a cache miss instead of not found")
	}
}
