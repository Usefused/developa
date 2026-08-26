package postgres

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"testing"
	"time"

	"developa/internal/domain"
)

func TestIntegrationReviewPagesDeduplicateCalleesAndScopeEvidence(t *testing.T) {
	store, counter := catalogFixture(t)
	report, ids := branchingFlowReport(t)
	snapshot := saveReport(t, store, "repo", report)
	counter.Store(0)
	page, err := store.ReviewPage(context.Background(), "repo", snapshot.ID, domain.ReviewOptions{CalleeOf: ids["Selected"]})
	if err != nil || page.Total != 1 || len(page.Items) != 1 || page.UnresolvedCount != 2 || counter.Load() != 1 {
		t.Fatalf("callee page/query count: %+v %v queries=%d", page, err, counter.Load())
	}
	if page.Items[0].Symbol.Name != "Helper" {
		t.Fatal("non-callee included")
	}
	assertRepeatedCalleeReview(t, store, snapshot.ID, ids["Helper"])
	if _, err := store.ReviewPage(context.Background(), "other", snapshot.ID, domain.ReviewOptions{}); !errors.Is(err, domain.ErrNotFound) {
		t.Fatal("cross-repository read accepted")
	}
	if _, err := store.ReviewPage(context.Background(), "repo", snapshot.ID, domain.ReviewOptions{CalleeOf: strings.Repeat("f", 64)}); !errors.Is(err, domain.ErrNotFound) {
		t.Fatal("missing caller accepted")
	}
}

func assertRepeatedCalleeReview(t *testing.T, store *Store, snapshot, caller string) {
	t.Helper()
	page, err := store.ReviewPage(context.Background(), "repo", snapshot, domain.ReviewOptions{CalleeOf: caller})
	if err != nil || page.Total != 1 || len(page.Items) != 1 || page.Items[0].Symbol.Name != "Shared" {
		t.Fatal("repeated call sites produced repeated review inputs")
	}
}

func TestIntegrationSavedReviewsAreJoinedAndWritesStayConstant(t *testing.T) {
	for _, count := range []int{1, 100} {
		t.Run(strconv.Itoa(count), func(t *testing.T) { assertReviewQueries(t, count) })
	}
}

func assertReviewQueries(t *testing.T, count int) {
	t.Helper()
	store, counter := catalogFixture(t)
	snapshot := saveReport(t, store, "repo", catalogReport(t, count, "reviews"))
	page, err := store.ReviewPage(context.Background(), "repo", snapshot.ID, domain.ReviewOptions{})
	if err != nil {
		t.Fatal(err)
	}
	reviews := testReviews(page.Items)
	execution := testExecution()
	execution.Status = "completed"
	counter.Store(0)
	if err := store.SaveReviews(context.Background(), "repo", snapshot.ID, reviews, nil, execution); err != nil {
		t.Fatal(err)
	}
	if counter.Load() != 6 {
		t.Fatalf("review write queries=%d, expected 6", counter.Load())
	}
	counter.Store(0)
	item, err := store.Symbol(context.Background(), "repo", snapshot.ID, reviews[0].SymbolID)
	if err != nil || item.Review == nil || counter.Load() != 1 {
		t.Fatalf("saved review missing from symbol: %v", err)
	}
	if item.Review.Evidence.Path != item.Path || item.Review.Evidence.Span != item.Symbol.Span {
		t.Fatal("evidence was not rebound in SQL")
	}
	assertReviewPageReads(t, store, counter, snapshot.ID, reviews[0].SymbolID)
}

func assertReviewPageReads(t *testing.T, store *Store, counter *queryCounter, snapshot, id string) {
	t.Helper()
	counter.Store(0)
	page, err := store.Symbols(context.Background(), "repo", snapshot, domain.Filter{Limit: 4, Kind: "function"})
	if err != nil || page.Items[0].Review == nil || counter.Load() != 1 {
		t.Fatal("symbol list omitted saved review or made per-row queries")
	}
	counter.Store(0)
	flow, err := store.Flow(context.Background(), "repo", snapshot, domain.FlowOptions{SymbolID: id})
	if err != nil || flow.Nodes[0].Review == nil || counter.Load() != 1 {
		t.Fatal("flow omitted saved review or made per-node queries")
	}
}

func testReviews(items []domain.SymbolDetail) []domain.FunctionReview {
	reviews := make([]domain.FunctionReview, 0, len(items))
	for _, item := range items {
		reviews = append(reviews, domain.FunctionReview{SymbolID: item.Symbol.ID, SourceID: item.Symbol.SourceID, Summary: "Saved inferred description.", Parameters: []domain.ParameterReview{}, Model: "fixture", PromptVersion: "function-reviews-v1", CreatedAt: time.Now().UTC(), Evidence: domain.Citation{Path: "invented.go"}})
	}
	return reviews
}

func TestIntegrationReviewPublicationRejectsStaleSourceAtomically(t *testing.T) {
	store, _ := catalogFixture(t)
	snapshot := saveReport(t, store, "repo", catalogReport(t, 2, "review-rollback"))
	page, err := store.ReviewPage(context.Background(), "repo", snapshot.ID, domain.ReviewOptions{})
	if err != nil {
		t.Fatal(err)
	}
	reviews := testReviews(page.Items)
	reviews[1].SourceID = strings.Repeat("f", 64)
	execution := testExecution()
	execution.Status = "completed"
	if err := store.SaveReviews(context.Background(), "repo", snapshot.ID, reviews, nil, execution); !errors.Is(err, domain.ErrInvalidModelOutput) {
		t.Fatal(err)
	}
	assertTableCount(t, store, "developa_function_reviews", 0)
	assertTableCount(t, store, "developa_audit_events", 1)
}

func TestIntegrationReviewCacheBatchIsRepositoryScoped(t *testing.T) {
	store, counter := catalogFixture(t)
	snapshot := saveReport(t, store, "repo", catalogReport(t, 1, "review-cache"))
	entry := domain.ReviewCacheEntry{Key: strings.Repeat("a", 64), Model: "fixture", Payload: []byte(`{"summary":"saved"}`)}
	execution := testExecution()
	execution.Status = "completed"
	if err := store.SaveReviews(context.Background(), "repo", snapshot.ID, nil, []domain.ReviewCacheEntry{entry}, execution); err != nil {
		t.Fatal(err)
	}
	counter.Store(0)
	cache, err := store.CachedReviews(context.Background(), "repo", []string{entry.Key})
	if err != nil || len(cache) != 1 || counter.Load() != 1 {
		t.Fatalf("batch cache read: %v queries=%d", err, counter.Load())
	}
	other, err := store.CachedReviews(context.Background(), "other", []string{entry.Key})
	if err != nil || len(other) != 0 {
		t.Fatal("review cache crossed repository scope")
	}
}
