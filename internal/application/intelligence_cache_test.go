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

type cacheTestStore struct {
	*intelligenceTestStore
	entries  map[string]json.RawMessage
	reads    int
	writes   int
	readErr  error
	writeErr error
}

func (s *cacheTestStore) CachedAnalysis(_ context.Context, repo, key string) (json.RawMessage, error) {
	s.reads++
	if s.readErr != nil {
		return nil, s.readErr
	}
	data, ok := s.entries[repo+":"+key]
	if !ok {
		return nil, domain.ErrNotFound
	}
	return append(json.RawMessage(nil), data...), nil
}

func (s *cacheTestStore) CacheAnalysis(_ context.Context, repo, key, _ string, data json.RawMessage) error {
	s.writes++
	if s.writeErr != nil {
		return s.writeErr
	}
	s.entries[repo+":"+key] = append(json.RawMessage(nil), data...)
	return nil
}

type resolvingTestModel struct {
	*intelligenceTestModel
	resolved   string
	resolveErr error
	resolves   int
	policy     string
}

func (m *resolvingTestModel) ResolveModel(context.Context) (string, error) {
	m.resolves++
	if m.resolveErr != nil {
		return "", m.resolveErr
	}
	m.identity = m.resolved
	return m.resolved, nil
}

func (m *resolvingTestModel) CachePolicy() string { return m.policy }

func cacheFixture(t *testing.T, count int, cfg IntelligenceConfig) (*IntelligenceService, *cacheTestStore, *resolvingTestModel) {
	t.Helper()
	store := &cacheTestStore{intelligenceTestStore: &intelligenceTestStore{symbols: intelligenceFacts(count)}, entries: make(map[string]json.RawMessage)}
	model := &resolvingTestModel{intelligenceTestModel: featureTestModel(), resolved: "fixture@sha256:" + strings.Repeat("a", 64), policy: "fixture-v1"}
	return cacheService(t, store, model, cfg), store, model
}

func cacheService(t *testing.T, store domain.IntelligenceStore, model StructuredModel, cfg IntelligenceConfig) *IntelligenceService {
	t.Helper()
	if cfg.RepositoryID == "" {
		cfg.RepositoryID = "repository"
	}
	service, err := NewIntelligence(store, model, cfg)
	if err != nil {
		t.Fatal(err)
	}
	return service
}

func discoverCached(t *testing.T, service *IntelligenceService, snapshot string) domain.FeatureRun {
	t.Helper()
	run, err := service.Discover(context.Background(), snapshot)
	if err != nil {
		t.Fatal(err)
	}
	return run
}

func TestFeatureCacheRebindsCurrentPositionsAndFeatureIDsWithoutInference(t *testing.T) {
	service, store, model := cacheFixture(t, 1, IntelligenceConfig{})
	first := discoverCached(t, service, "snapshot-one")
	firstID := store.features[0].ID
	store.symbols[0].Symbol.Span.Start.Line = 99
	store.symbols[0].Symbol.Span.End.Line = 102
	second := discoverCached(t, service, "snapshot-two")
	if first.ModelCalls != 1 || second.ModelCalls != 0 || second.CachedBatches != 1 || model.calls.Load() != 1 {
		t.Fatal("unchanged evidence consumed inference or counters hid reuse")
	}
	if store.features[0].ID == firstID || store.features[0].Evidence[0].Span.Start.Line != 99 {
		t.Fatal("cached output reused stale feature identity or citation positions")
	}
	if store.reads != 2 || store.writes != 1 || model.resolves != 2 {
		t.Fatal("cache did not use one bounded lookup and metadata preflight per batch")
	}
}

func TestFeatureCacheInvalidatesSourceModelPolicyAndRepositoryChanges(t *testing.T) {
	service, store, model := cacheFixture(t, 1, IntelligenceConfig{})
	discoverCached(t, service, "snapshot")
	store.symbols[0].Symbol.Source = "func Run() { changed() }"
	discoverCached(t, service, "snapshot")
	model.resolved = "fixture@sha256:" + strings.Repeat("b", 64)
	discoverCached(t, service, "snapshot")
	model.policy = "fixture-v2"
	discoverCached(t, service, "snapshot")
	otherRepo := cacheService(t, store, model, IntelligenceConfig{RepositoryID: "other-repository"})
	discoverCached(t, otherRepo, "snapshot")
	if model.calls.Load() != 5 || store.writes != 5 {
		t.Fatal("changed inference inputs or repository reused stale output")
	}
}

func TestFeatureCacheKeyIncludesExactFactsAndSchemaScope(t *testing.T) {
	facts := json.RawMessage(`[{"id":"one","source":"first"}]`)
	key := inferenceCacheKey("repo", "model", "policy", "features-v2", featureTask, featureSchema, facts)
	cases := []struct {
		name          string
		schema, facts json.RawMessage
	}{
		{"symbol scope", featureSchema, json.RawMessage(`[{"id":"two","source":"first"}]`)},
		{"exact prompt bytes", featureSchema, append(facts, ' ')},
		{"output contract", json.RawMessage(`{"type":"object"}`), facts},
	}
	for _, tc := range cases {
		if key == inferenceCacheKey("repo", "model", "policy", "features-v2", featureTask, tc.schema, tc.facts) {
			t.Fatalf("changed %s shared a cache key", tc.name)
		}
	}
}

func TestFeatureCacheRejectsInvalidCachedEvidenceWithoutFallback(t *testing.T) {
	service, store, model := cacheFixture(t, 1, IntelligenceConfig{})
	previous := discoverCached(t, service, "snapshot")
	for key := range store.entries {
		store.entries[key] = json.RawMessage(`{"features":[{"title":"Invented","summary":"No evidence","symbol_ids":["absent"]}]}`)
	}
	_, err := service.Discover(context.Background(), "snapshot")
	if !errors.Is(err, domain.ErrInvalidModelOutput) || model.calls.Load() != 1 || store.run.ID != previous.ID {
		t.Fatal("corrupt cached evidence was accepted, repaired, or replaced valid results")
	}
}

func TestFeatureCacheNeverStoresInvalidModelOutput(t *testing.T) {
	service, store, model := cacheFixture(t, 1, IntelligenceConfig{})
	model.generate = func(context.Context, string, string, json.RawMessage) (json.RawMessage, error) {
		return json.RawMessage(`{"features":[{"title":"Invented","summary":"No evidence","symbol_ids":["absent"]}]}`), nil
	}
	if _, err := service.Discover(context.Background(), "snapshot"); !errors.Is(err, domain.ErrInvalidModelOutput) {
		t.Fatal(err)
	}
	if len(store.entries) != 0 || store.writes != 0 {
		t.Fatal("invalid feature output entered the shared cache")
	}
}

func TestFeatureCacheRequiresResolverAndVerifiedRevision(t *testing.T) {
	service, store, model := cacheFixture(t, 1, IntelligenceConfig{})
	model.resolved = "fixture@cloud:unverified"
	discoverCached(t, service, "snapshot")
	discoverCached(t, service, "snapshot")
	legacy := cacheService(t, store, featureTestModel(), IntelligenceConfig{})
	discoverCached(t, legacy, "snapshot")
	if store.reads != 0 || store.writes != 0 || model.calls.Load() != 2 {
		t.Fatal("unverified or unresolved model used a cache")
	}
}

func TestFeatureCacheResolverFailureKeepsPriorResultsWithoutInference(t *testing.T) {
	service, store, model := cacheFixture(t, 1, IntelligenceConfig{})
	previous := discoverCached(t, service, "snapshot")
	model.resolveErr = context.DeadlineExceeded
	_, err := service.Discover(context.Background(), "snapshot")
	if !errors.Is(err, context.DeadlineExceeded) || model.calls.Load() != 1 || store.run.ID != previous.ID {
		t.Fatal("metadata failure retried inference or replaced prior results")
	}
}

func TestFeatureCacheNeverStoresAChangedPostInferenceIdentity(t *testing.T) {
	service, store, model := cacheFixture(t, 1, IntelligenceConfig{})
	generate := model.generate
	model.generate = func(ctx context.Context, system, prompt string, schema json.RawMessage) (json.RawMessage, error) {
		model.identity = "fixture@sha256:" + strings.Repeat("b", 64)
		return generate(ctx, system, prompt, schema)
	}
	_, err := service.Discover(context.Background(), "snapshot")
	if !errors.Is(err, domain.ErrInvalidInput) || len(store.entries) != 0 || store.run.ID != "" {
		t.Fatal("changed model output was stored under the preflight identity")
	}
}

func TestFeatureCacheStorageFailuresRetainPriorRun(t *testing.T) {
	for _, operation := range []string{"read", "write"} {
		t.Run(operation, func(t *testing.T) {
			service, store, _ := cacheFixture(t, 1, IntelligenceConfig{})
			previous := discoverCached(t, service, "snapshot")
			store.symbols[0].Symbol.Source = "changed source"
			if operation == "read" {
				store.readErr = errors.New("cache unavailable")
			} else {
				store.writeErr = errors.New("cache unavailable")
			}
			if _, err := service.Discover(context.Background(), "snapshot"); err == nil || store.run.ID != previous.ID {
				t.Fatal("failed cache access replaced valid prior results")
			}
		})
	}
}

func TestFeatureCacheContinuationCountersAndCloudDisclosureAreHonest(t *testing.T) {
	service, store, model := cacheFixture(t, 2, IntelligenceConfig{BatchSize: 1, MaxModelCalls: 1})
	model.resolved = "fixture@cloud:aabbccddeeff"
	discoverCached(t, service, "snapshot-one")
	first := discoverCached(t, service, "snapshot-one")
	if first.ModelCalls != 2 || first.CachedBatches != 0 {
		t.Fatal("model call counters did not accumulate across continuations")
	}
	store.run = domain.FeatureRun{}
	discoverCached(t, service, "snapshot-two")
	second := discoverCached(t, service, "snapshot-two")
	if second.ModelCalls != 0 || second.CachedBatches != 2 || model.calls.Load() != 2 {
		t.Fatal("cached continuation counters were not cumulative")
	}
	if !slices.Contains(second.Limitations, cloudCachedLimitation) || slices.Contains(second.Limitations, cloudTransferLimitation) {
		t.Fatal("cache-only execution claimed fresh cloud source transfer")
	}
}
