package application

import (
	"context"
	"errors"
	"testing"

	"developa/internal/domain"
)

type featureContextStoreStub struct {
	calls   []string
	feature domain.Feature
	source  domain.ContextPack
	flow    domain.CodeFlow
	errAt   string
}

func (s *featureContextStoreStub) Feature(_ context.Context, repository, snapshot, feature string) (domain.Feature, error) {
	s.calls = append(s.calls, "feature:"+repository+":"+snapshot+":"+feature)
	if s.errAt == "feature" {
		return domain.Feature{}, domain.ErrNotFound
	}
	return s.feature, nil
}

func (s *featureContextStoreStub) FeatureContext(_ context.Context, repository, snapshot, feature string, limit int) (domain.ContextPack, error) {
	s.calls = append(s.calls, "source:"+repository+":"+snapshot+":"+feature)
	if s.errAt == "source" {
		return domain.ContextPack{}, domain.ErrNotFound
	}
	result := s.source
	result.Total = limit
	return result, nil
}

func (s *featureContextStoreStub) Flow(_ context.Context, repository, snapshot string, options domain.FlowOptions) (domain.CodeFlow, error) {
	s.calls = append(s.calls, "flow:"+repository+":"+snapshot+":"+options.FeatureID)
	if s.errAt == "flow" {
		return domain.CodeFlow{}, domain.ErrNotFound
	}
	result := s.flow
	result.Options = options
	return result, nil
}

func TestFeatureContextComposesBoundedClaimsSourceAndFlow(t *testing.T) {
	id, snapshot := testHash("feature"), testHash("snapshot")
	store := &featureContextStoreStub{feature: domain.Feature{ID: id}, source: domain.ContextPack{Truncated: true}, flow: domain.CodeFlow{Truncated: true}}
	bundle, err := NewFeatureContexts(store, "repo").FeatureContext(context.Background(), snapshot, id, domain.FeatureContextOptions{})
	if err != nil || len(store.calls) != 3 {
		t.Fatalf("feature context failed: %v calls=%v", err, store.calls)
	}
	if bundle.RepositoryID != "repo" || bundle.SnapshotID != snapshot || bundle.Feature.ID != id || bundle.Source.Total != 20 {
		t.Fatalf("feature context lost scope or default bounds: %+v", bundle)
	}
	if bundle.Options != (domain.FeatureContextOptions{SourceLimit: 20, Depth: 6, FlowLimit: 80}) || bundle.Flow.Options.FeatureID != id {
		t.Fatalf("feature context options were not normalized: %+v", bundle)
	}
	if len(bundle.Limitations) != 3 {
		t.Fatal("truncated evidence was not disclosed")
	}
}

func TestFeatureContextRejectsInvalidBoundsBeforeStorage(t *testing.T) {
	store := &featureContextStoreStub{}
	_, err := NewFeatureContexts(store, "repo").FeatureContext(context.Background(), testHash("snapshot"), testHash("feature"), domain.FeatureContextOptions{SourceLimit: 21})
	if !errors.Is(err, domain.ErrInvalidInput) || len(store.calls) != 0 {
		t.Fatal("invalid feature context bounds reached storage")
	}
}

func TestFeatureContextStopsAfterFirstFailedStage(t *testing.T) {
	for _, stage := range []string{"feature", "source", "flow"} {
		store := &featureContextStoreStub{errAt: stage}
		_, err := NewFeatureContexts(store, "repo").FeatureContext(context.Background(), testHash("snapshot"), testHash("feature"), domain.FeatureContextOptions{})
		if !errors.Is(err, domain.ErrNotFound) {
			t.Fatalf("%s error was lost: %v", stage, err)
		}
		want := map[string]int{"feature": 1, "source": 2, "flow": 3}[stage]
		if len(store.calls) != want {
			t.Fatalf("%s failure ran later stages: %v", stage, store.calls)
		}
	}
}

func testHash(seed string) string {
	value := []byte(seed)
	result := make([]byte, 64)
	for i := range result {
		result[i] = "abcdef0123456789"[value[i%len(value)]%16]
	}
	return string(result)
}
