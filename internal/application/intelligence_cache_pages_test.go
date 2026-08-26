package application

import (
	"context"
	"errors"
	"strings"
	"testing"

	"developa/internal/domain"
)

// Production selects these file boundaries in SQL; this fixture exposes that reader contract.
type boundaryCacheStore struct {
	*cacheTestStore
	pageReads int
}

func (s *boundaryCacheStore) AnalysisPage(_ context.Context, _, _ string, limit, offset int) (domain.SymbolPage, error) {
	s.pageReads++
	page := domain.SymbolPage{Total: len(s.symbols), Limit: limit, Offset: offset}
	if offset >= len(s.symbols) {
		return page, nil
	}
	end := offset
	for end < len(s.symbols) && end < offset+limit && s.symbols[end].Path == s.symbols[offset].Path {
		end++
	}
	page.Items = s.symbols[offset:end]
	return page, nil
}

func (s *boundaryCacheStore) Symbols(context.Context, string, string, domain.Filter) (domain.SymbolPage, error) {
	return domain.SymbolPage{}, errors.New("analysis bypassed the SQL file page contract")
}

func TestFeatureCacheFilePagesSurviveInsertionsInEarlierFiles(t *testing.T) {
	_, base, model := cacheFixture(t, 3, IntelligenceConfig{})
	base.symbols[0].Path = "a.go"
	base.symbols[1].Path, base.symbols[2].Path = "b.go", "b.go"
	store := &boundaryCacheStore{cacheTestStore: base}
	service := cacheService(t, store, model, IntelligenceConfig{})
	first := discoverCached(t, service, "first")
	addition := intelligenceFacts(4)[3]
	addition.Path = "a.go"
	base.symbols = append([]domain.SymbolDetail{base.symbols[0], addition}, base.symbols[1:]...)
	second := discoverCached(t, service, "second")
	if first.ModelCalls != 2 || second.ModelCalls != 1 || second.CachedBatches != 1 || model.calls.Load() != 3 {
		t.Fatal("an earlier file insertion invalidated unchanged later file evidence")
	}
	if store.pageReads != 4 || second.AnalyzedSymbols != 4 {
		t.Fatal("file page traversal lost coverage or made per-symbol reads")
	}
}

func TestFeatureCacheResolverDetectsContinuationMismatchBeforeInference(t *testing.T) {
	service, store, model := cacheFixture(t, 2, IntelligenceConfig{BatchSize: 1, MaxModelCalls: 1})
	previous := discoverCached(t, service, "snapshot")
	model.resolved = "fixture@sha256:" + strings.Repeat("b", 64)
	_, err := service.Discover(context.Background(), "snapshot")
	if !errors.Is(err, domain.ErrInvalidInput) || model.calls.Load() != 1 || store.run.ID != previous.ID {
		t.Fatal("changed verified revision consumed inference or mixed continuation provenance")
	}
}
