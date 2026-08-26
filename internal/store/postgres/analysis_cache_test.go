package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"developa/internal/domain"
)

func TestIntegrationAnalysisCacheScopedImmutableAndAudited(t *testing.T) {
	store, counter := catalogFixture(t)
	key := fingerprint("scoped-cache")
	payload := json.RawMessage(`{"features":[{"title":"private-output-title"}]}`)
	counter.Store(0)
	if err := store.CacheAnalysis(context.Background(), "repo", key, "model@cloud:123456789abc", payload); err != nil {
		t.Fatal(err)
	}
	if counter.Load() != 1 {
		t.Fatal("cache write exceeded one query")
	}
	counter.Store(0)
	cached, err := store.CachedAnalysis(context.Background(), "repo", key)
	if err != nil || !strings.Contains(string(cached), "private-output-title") || counter.Load() != 1 {
		t.Fatalf("cache read or budget failed: %v, %d", err, counter.Load())
	}
	assertCacheIsolationAndImmutability(t, store, key)
	assertTableCount(t, store, "developa_audit_events", 1)
	assertTableCount(t, store, "developa_audit_outbox", 1)
	var audit string
	err = store.pool.QueryRow(context.Background(), `SELECT to_jsonb(e)::text FROM developa_audit_events e`).Scan(&audit)
	if err != nil || strings.Contains(audit, "private-output-title") || strings.Contains(audit, "model@cloud") {
		t.Fatal("cache payload or model identity leaked into audit")
	}
}

func assertCacheIsolationAndImmutability(t *testing.T, store *Store, key string) {
	t.Helper()
	_, err := store.CachedAnalysis(context.Background(), "other", key)
	if !errors.Is(err, domain.ErrNotFound) {
		t.Fatal("cache hit crossed repository scope")
	}
	err = store.CacheAnalysis(context.Background(), "repo", key, "other-model", json.RawMessage(`{"features":[]}`))
	if err != nil {
		t.Fatal(err)
	}
	cached, err := store.CachedAnalysis(context.Background(), "repo", key)
	if err != nil || !strings.Contains(string(cached), "private-output-title") {
		t.Fatal("duplicate cache key overwrote validated evidence")
	}
}

func TestIntegrationAnalysisCacheAuditFailureRollsBack(t *testing.T) {
	store, _ := catalogFixture(t)
	_, err := store.pool.Exec(context.Background(), `ALTER TABLE developa_audit_events ADD CONSTRAINT reject_cache CHECK(trigger<>'analysis_cache')`)
	if err != nil {
		t.Fatal(err)
	}
	err = store.CacheAnalysis(context.Background(), "repo", fingerprint("cache-rollback"), "model", json.RawMessage(`{"features":[]}`))
	if !errors.Is(err, ErrUnavailable) {
		t.Fatal("cache audit failure did not abort its write")
	}
	assertTableCount(t, store, "developa_analysis_cache", 0)
	assertTableCount(t, store, "developa_audit_events", 0)
}

func TestIntegrationAnalysisCacheRejectsMalformedAndOversizedInputs(t *testing.T) {
	store, _ := catalogFixture(t)
	cases := []struct{ key, model, payload string }{
		{"invalid", "model", `{}`},
		{fingerprint("large-model"), strings.Repeat("m", 201), `{}`},
		{fingerprint("malformed"), "model", `{`},
		{fingerprint("oversized"), "model", `{"data":"` + strings.Repeat("x", 256*1024) + `"}`},
	}
	for _, tc := range cases {
		err := store.CacheAnalysis(context.Background(), "repo", tc.key, tc.model, json.RawMessage(tc.payload))
		if !errors.Is(err, domain.ErrInvalidInput) {
			t.Fatalf("invalid cache input was accepted: %v", err)
		}
	}
	assertTableCount(t, store, "developa_analysis_cache", 0)
}

func TestIntegrationAnalysisPageStopsAtFileBoundary(t *testing.T) {
	store, counter := catalogFixture(t)
	snapshot := saveReport(t, store, "repo", catalogReport(t, 3, "file-pages"))
	for _, offset := range []int{0, 1, 2, 4, 6, 1000000} {
		counter.Store(0)
		page, err := store.AnalysisPage(context.Background(), "repo", snapshot.ID, 8, offset)
		if err != nil || page.Total != 6 || counter.Load() != 1 {
			t.Fatalf("file-bounded page budget or total failed: %+v, %v", page, err)
		}
		assertAnalysisPageBoundary(t, page, offset)
	}
	_, err := store.AnalysisPage(context.Background(), "other", snapshot.ID, 8, 0)
	if !errors.Is(err, domain.ErrNotFound) {
		t.Fatal("analysis page leaked across repositories")
	}
}

func assertAnalysisPageBoundary(t *testing.T, page domain.SymbolPage, offset int) {
	t.Helper()
	expected := 0
	if offset < 6 {
		expected = 2 - offset%2
	}
	if len(page.Items) != expected {
		t.Fatalf("page offset=%d returned %d symbols, expected %d", offset, len(page.Items), expected)
	}
	for _, item := range page.Items {
		if item.Path != page.Items[0].Path {
			t.Fatal("analysis batch crossed a file boundary")
		}
	}
}
