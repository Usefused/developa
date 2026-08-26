package postgres

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"developa/internal/domain"
)

func TestIntegrationRepositoriesAreConfiguredScopedWithLatestSnapshots(t *testing.T) {
	store, counter := catalogFixture(t)
	ensureListedRepository(t, store, "second", "Alpha")
	ensureListedRepository(t, store, "hidden", "Hidden retained index")
	first := saveReport(t, store, "repo", catalogReport(t, 1, "first"))
	latest := saveReport(t, store, "repo", catalogReport(t, 2, "latest"))
	saveReport(t, store, "hidden", catalogReport(t, 3, "hidden"))
	counter.Store(0)
	page, err := store.Repositories(context.Background(), []string{"repo", "second", "repo", "absent"}, domain.Filter{Limit: 10})
	if err != nil || counter.Load() != 1 || page.Total != 2 || len(page.Items) != 2 {
		t.Fatalf("repository page/query budget mismatch: %+v %v queries=%d", page, err, counter.Load())
	}
	if page.Items[0].ID != "second" || page.Items[0].Snapshot != nil {
		t.Fatal("unindexed configured repository must remain visible with a null snapshot")
	}
	assertListedSnapshot(t, page.Items[1], latest, first.ID)
}

func assertListedSnapshot(t *testing.T, item domain.RepositorySummary, latest domain.Snapshot, old string) {
	t.Helper()
	if item.ID != "repo" || item.Snapshot == nil {
		t.Fatal("indexed repository lost its latest snapshot")
	}
	if item.Snapshot.ID != latest.ID || item.Snapshot.ID == old || item.Snapshot.FileCount != 2 {
		t.Fatal("repository listing mixed publication metadata")
	}
}

func TestIntegrationRepositoriesEmptyAllowlistNeverListsRetainedIndexes(t *testing.T) {
	store, counter := catalogFixture(t)
	for _, ids := range [][]string{nil, {}, {"unknown"}} {
		counter.Store(0)
		page, err := store.Repositories(context.Background(), ids, domain.Filter{})
		if err != nil || counter.Load() != 1 || page.Total != 0 || page.Items == nil || len(page.Items) != 0 {
			t.Fatalf("empty authorization scope exposed a repository: %+v %v", page, err)
		}
	}
}

func TestIntegrationRepositoriesQueryBudgetAndEmptyPageTotal(t *testing.T) {
	store, counter := catalogFixture(t)
	ids := listedRepositorySet(t, store, 100)
	for _, size := range []int{1, 100} {
		counter.Store(0)
		page, err := store.Repositories(context.Background(), ids[:size], domain.Filter{Limit: 1, Offset: size + 1})
		if err != nil || counter.Load() != 1 || page.Total != size || len(page.Items) != 0 {
			t.Fatalf("empty page lost total or grew queries at %d repositories: %+v %v", size, page, err)
		}
	}
	page, err := store.Repositories(context.Background(), ids, domain.Filter{Query: "REPOSITORY 09", Limit: 3, Offset: 7})
	if err != nil || page.Total != 10 || len(page.Items) != 3 || page.Items[0].Name != "Repository 097" {
		t.Fatalf("SQL filtering/pagination mismatch: %+v %v", page, err)
	}
}

func listedRepositorySet(t *testing.T, store *Store, count int) []string {
	t.Helper()
	_, err := store.pool.Exec(context.Background(), `INSERT INTO developa_repositories(id,name)
		SELECT 'listed-'||n,'Repository '||lpad(n::text,3,'0') FROM generate_series(0,$1::int-1) n`, count)
	if err != nil {
		t.Fatal(err)
	}
	ids := make([]string, count)
	for i := range ids {
		ids[i] = fmt.Sprintf("listed-%d", i)
	}
	return ids
}

func TestIntegrationRepositoriesSearchUsesLiteralSubstring(t *testing.T) {
	store, _ := catalogFixture(t)
	ensureListedRepository(t, store, "literal", "100%_coverage")
	page, err := store.Repositories(context.Background(), []string{"repo", "literal"}, domain.Filter{Query: "%_"})
	if err != nil || page.Total != 1 || page.Items[0].ID != "literal" {
		t.Fatal("repository search treated literal punctuation as a wildcard")
	}
}

func TestIntegrationRepositoriesJoinSnapshotIdentityWithinRepository(t *testing.T) {
	store, counter := catalogFixture(t)
	firstReport := catalogReport(t, 1, "shared-publication")
	firstReport.Snapshot.Branch = "first-branch"
	first := saveReport(t, store, "repo", firstReport)
	secondReport := firstReport
	secondReport.Snapshot.Branch = "second-branch"
	second := saveReport(t, store, "second", secondReport)
	if first.ID != second.ID {
		t.Fatal("fixture must exercise matching snapshot IDs in different repositories")
	}
	counter.Store(0)
	page, err := store.Repositories(context.Background(), []string{"repo", "second"}, domain.Filter{})
	if err != nil || page.Total != 2 || len(page.Items) != 2 || counter.Load() != 1 {
		t.Fatal("snapshot join duplicated rows or grew the query budget")
	}
	expected := map[string]string{"repo": "first-branch", "second": "second-branch"}
	for _, item := range page.Items {
		if item.Snapshot == nil || item.Snapshot.Branch != expected[item.ID] {
			t.Fatal("snapshot metadata crossed a repository boundary with a matching snapshot ID")
		}
	}
}

func TestIntegrationRepositoriesRejectInvalidFiltersBeforeQueries(t *testing.T) {
	store, counter := catalogFixture(t)
	cases := []domain.Filter{{Limit: 101}, {Limit: -1}, {Offset: -1}, {Offset: 100001}, {Kind: "function"}, {File: "main.go"}, {Query: strings.Repeat("x", 201)}, {Query: "\x00"}, {Query: "\xff"}}
	for _, filter := range cases {
		counter.Store(0)
		_, err := store.Repositories(context.Background(), []string{"repo"}, filter)
		if !errors.Is(err, domain.ErrInvalidInput) || counter.Load() != 0 {
			t.Fatalf("invalid repository filter reached SQL: %+v %v", filter, err)
		}
	}
}

func ensureListedRepository(t *testing.T, store *Store, id, name string) {
	t.Helper()
	if err := store.EnsureRepository(context.Background(), domain.Repository{ID: id, Name: name}); err != nil {
		t.Fatal(err)
	}
}
