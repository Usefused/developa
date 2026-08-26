package postgres

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"

	"developa/internal/application"
	"developa/internal/domain"
	goparser "developa/internal/indexer/golang"
	source "developa/internal/source/git"
	"github.com/jackc/pgx/v5"
)

func TestIntegrationCatalogRoundTrip(t *testing.T) {
	store, _ := catalogFixture(t)
	report := catalogReport(t, 3, "first")
	report.ChangesKnown = true
	report.Changes = []source.Change{{Kind: source.Modified, Path: "file000.go"}}
	snapshot := saveReport(t, store, "repo", report)
	latest, err := store.Latest(context.Background(), "repo")
	if err != nil || latest.ID != snapshot.ID || latest.FileCount != 3 || latest.SymbolCount != 6 {
		t.Fatalf("incorrect latest snapshot: %+v, %v", latest, err)
	}
	if !latest.ChangesKnown || latest.ChangeCount != 1 || latest.PackageCount != 1 {
		t.Fatalf("incorrect metadata counts: %+v", latest)
	}
	assertCatalogDetails(t, store, snapshot)
	assertCatalogFile(t, store, report, snapshot)
}

func TestIntegrationDocumentationPersistsAndLegacyReadsStayPinned(t *testing.T) {
	store, counter := catalogFixture(t)
	report, ids := flowReport(t, "package sample\n// Run sends a value.\nfunc Run() {\n// Reject empty input.\n}\n", nil, "documentation")
	snapshot := saveReport(t, store, "repo", report)
	symbol, err := store.Symbol(context.Background(), "repo", snapshot.ID, ids["Run"])
	if err != nil || symbol.Symbol.Documentation.Summary != "Run sends a value.\n\nReject empty input." || symbol.Symbol.Documentation.Origin != "indexed_source" {
		t.Fatalf("compiled documentation did not round trip: %+v %v", symbol, err)
	}
	// Simulate a pre-documentation catalog record without touching the current source.
	_, err = store.pool.Exec(context.Background(), `UPDATE developa_symbols SET payload=payload-'documentation' WHERE repository_id='repo' AND snapshot_id=$1`, snapshot.ID)
	if err != nil {
		t.Fatal(err)
	}
	counter.Store(0)
	page, err := store.Symbols(context.Background(), "repo", snapshot.ID, domain.Filter{})
	if err != nil || counter.Load() != 1 || page.Items[0].Symbol.Documentation.Origin != "captured_excerpt" {
		t.Fatal("legacy enrichment exceeded the page query budget")
	}
	if page.Items[0].Symbol.Documentation.Summary != symbol.Symbol.Documentation.Summary {
		t.Fatal("legacy summary changed saved evidence")
	}
	assertLegacyFlowDocumentation(t, store, snapshot.ID, ids["Run"])
}

func assertLegacyFlowDocumentation(t *testing.T, store *Store, snapshot, id string) {
	t.Helper()
	flow := readFlow(t, store, snapshot, domain.FlowOptions{SymbolID: id})
	if flow.Nodes[0].DescriptionSource != "source_comments" || !strings.Contains(flow.Nodes[0].Description, "Reject empty input.") {
		t.Fatal("flow API omitted compiled comments")
	}
	var exists bool
	if err := store.pool.QueryRow(context.Background(), `SELECT payload ? 'documentation' FROM developa_symbols WHERE repository_id='repo' AND snapshot_id=$1 AND id=$2`, snapshot, id).Scan(&exists); err != nil {
		t.Fatal(err)
	}
	if exists {
		t.Fatal("read enrichment mutated immutable historical records")
	}
}

func assertCatalogDetails(t *testing.T, store *Store, snapshot domain.Snapshot) {
	t.Helper()
	details, err := store.Details(context.Background(), "repo", snapshot.ID)
	if err != nil || details.Snapshot.ID != snapshot.ID || len(details.Changes) != 1 {
		t.Fatalf("details did not round trip: %+v, %v", details, err)
	}
	if details.Diagnostics == nil || details.Exclusions == nil || details.Skipped == nil {
		t.Fatal("empty detail collections must be JSON arrays")
	}
}

func assertCatalogFile(t *testing.T, store *Store, report application.Report, snapshot domain.Snapshot) {
	t.Helper()
	file, err := store.File(context.Background(), "repo", snapshot.ID, "file000.go")
	if err != nil || file.SymbolCount != 2 || file.Kinds["function"] != 1 || file.Kinds["struct"] != 1 {
		t.Fatalf("file did not round trip: %+v, %v", file, err)
	}
	symbolID := report.Index.Files[0].Symbols[0].ID
	symbol, err := store.Symbol(context.Background(), "repo", snapshot.ID, symbolID)
	if err != nil || symbol.Symbol.ID != symbolID || symbol.Path != file.Path {
		t.Fatalf("symbol did not round trip: %+v, %v", symbol, err)
	}
}

func TestIntegrationCatalogPaginationFiltersAndTotals(t *testing.T) {
	store, _ := catalogFixture(t)
	snapshot := saveReport(t, store, "repo", catalogReport(t, 100, "page"))
	page, err := store.Files(context.Background(), "repo", snapshot.ID, domain.Filter{Limit: 3, Offset: 97})
	if err != nil || page.Total != 100 || len(page.Items) != 3 || page.Items[0].Path != "file097.go" {
		t.Fatalf("unexpected ordered page: %+v, %v", page, err)
	}
	page, err = store.Files(context.Background(), "repo", snapshot.ID, domain.Filter{Limit: 3, Offset: 1000})
	if err != nil || page.Total != 100 || len(page.Items) != 0 {
		t.Fatalf("empty page lost its total: %+v, %v", page, err)
	}
	assertFilteredFiles(t, store, snapshot)
	assertFilteredSymbols(t, store, snapshot)
}

func assertFilteredFiles(t *testing.T, store *Store, snapshot domain.Snapshot) {
	t.Helper()
	page, err := store.Files(context.Background(), "repo", snapshot.ID, domain.Filter{Query: "FILE009", Kind: "struct", Limit: 999, Offset: -1})
	if err != nil || page.Total != 1 || page.Items[0].Path != "file009.go" || page.Limit != 100 || page.Offset != 0 {
		t.Fatalf("file filters/bounds failed: %+v, %v", page, err)
	}
	page, err = store.Files(context.Background(), "repo", snapshot.ID, domain.Filter{Kind: "interface"})
	if err != nil || page.Total != 0 || len(page.Items) != 0 {
		t.Fatalf("kind filter was not applied: %+v, %v", page, err)
	}
}

func assertFilteredSymbols(t *testing.T, store *Store, snapshot domain.Snapshot) {
	t.Helper()
	page, err := store.Symbols(context.Background(), "repo", snapshot.ID, domain.Filter{Kind: "function", File: "file009.go", Query: "RUN"})
	if err != nil || page.Total != 1 || len(page.Items) != 1 || page.Items[0].Symbol.Name != "Run009" {
		t.Fatalf("symbol filters failed: %+v, %v", page, err)
	}
	page, err = store.Symbols(context.Background(), "repo", snapshot.ID, domain.Filter{Kind: "function", Offset: 1000})
	if err != nil || page.Total != 100 || len(page.Items) != 0 {
		t.Fatalf("symbol empty page lost its total: %+v, %v", page, err)
	}
}

func TestIntegrationFileSearchFindsContainedSymbols(t *testing.T) {
	store, counter := catalogFixture(t)
	snapshot := saveReport(t, store, "repo", catalogReport(t, 100, "symbol-search"))
	cases := []domain.Filter{
		{Query: "Run009", Kind: "function"},
		{Query: "value string", Kind: "function", File: "file009.go"},
	}
	for _, filter := range cases {
		counter.Store(0)
		page, err := store.Files(context.Background(), "repo", snapshot.ID, filter)
		if err != nil || page.Total != 1 || len(page.Items) != 1 || page.Items[0].Path != "file009.go" {
			t.Fatalf("contained symbol search failed: %+v, %v", page, err)
		}
		if counter.Load() != 1 {
			t.Fatalf("contained symbol search used %d queries", counter.Load())
		}
	}
	assertSymbolFileSearchKindAndPagination(t, store, snapshot)
}

func assertSymbolFileSearchKindAndPagination(t *testing.T, store *Store, snapshot domain.Snapshot) {
	t.Helper()
	page, err := store.Files(context.Background(), "repo", snapshot.ID, domain.Filter{Query: "Run009", Kind: "struct"})
	if err != nil || page.Total != 0 || len(page.Items) != 0 {
		t.Fatal("symbol search ignored the requested symbol kind")
	}
	page, err = store.Files(context.Background(), "repo", snapshot.ID, domain.Filter{Query: "Run", Kind: "function", Limit: 2, Offset: 9})
	if err != nil || page.Total != 100 || len(page.Items) != 2 || page.Items[0].Path != "file009.go" {
		t.Fatalf("contained symbol search lost pagination: %+v, %v", page, err)
	}
}

func TestIntegrationFileSymbolSearchDoesNotCrossScope(t *testing.T) {
	store, _ := catalogFixture(t)
	report := catalogReport(t, 1, "search-scope")
	original := saveReport(t, store, "repo", report)
	report.Index.Files[0].Symbols[0].Name = "OtherRepositoryOnly"
	report.Index.Files[0].Symbols[0].Signature = "func OtherRepositoryOnly()"
	other := saveReport(t, store, "other", report)
	report.Snapshot.Fingerprint = fingerprint("newer-search-scope")
	report.Index.Files[0].Symbols[0].Name = "NewerSnapshotOnly"
	report.Index.Files[0].Symbols[0].Signature = "func NewerSnapshotOnly()"
	newer := saveReport(t, store, "repo", report)
	cases := []struct {
		repository, snapshotID, query string
		total                         int
	}{
		{"repo", original.ID, "OtherRepositoryOnly", 0},
		{"repo", original.ID, "NewerSnapshotOnly", 0},
		{"other", other.ID, "OtherRepositoryOnly", 1},
		{"repo", newer.ID, "NewerSnapshotOnly", 1},
	}
	for _, tc := range cases {
		page, err := store.Files(context.Background(), tc.repository, tc.snapshotID, domain.Filter{Query: tc.query})
		if err != nil || page.Total != tc.total || len(page.Items) != tc.total {
			t.Fatalf("symbol search crossed repository/snapshot scope: %+v, %v", page, err)
		}
	}
}

func TestIntegrationCatalogRepositoryAndSnapshotIsolation(t *testing.T) {
	store, _ := catalogFixture(t)
	report := catalogReport(t, 1, "same-fingerprint")
	a := saveReport(t, store, "repo", report)
	report.Index.Files[0].Symbols[0].Doc = "other-repository-doc"
	saveReport(t, store, "other", report)
	symbol, err := store.Symbol(context.Background(), "repo", a.ID, report.Index.Files[0].Symbols[0].ID)
	if err != nil || symbol.Symbol.Doc == "other-repository-doc" {
		t.Fatal("read crossed a repository boundary")
	}
	assertMissingCatalog(t, store, a.ID)
	older, err := store.Files(context.Background(), "repo", fingerprint("missing"), domain.Filter{})
	if !errors.Is(err, domain.ErrNotFound) || len(older.Items) != 0 {
		t.Fatal("unknown snapshot was not isolated")
	}
}

func assertMissingCatalog(t *testing.T, store *Store, snapshotID string) {
	t.Helper()
	_, fileErr := store.File(context.Background(), "absent", snapshotID, "file000.go")
	_, symbolErr := store.Symbol(context.Background(), "absent", snapshotID, "symbol")
	_, detailsErr := store.Details(context.Background(), "absent", snapshotID)
	_, filesErr := store.Files(context.Background(), "absent", snapshotID, domain.Filter{})
	_, symbolsErr := store.Symbols(context.Background(), "absent", snapshotID, domain.Filter{})
	_, latestErr := store.Latest(context.Background(), "absent")
	for _, err := range []error{fileErr, symbolErr, detailsErr, filesErr, symbolsErr, latestErr} {
		if !errors.Is(err, domain.ErrNotFound) {
			t.Fatalf("cross-repository read returned %v", err)
		}
	}
}

func TestIntegrationCatalogRetriesPreserveImmutableSnapshot(t *testing.T) {
	store, _ := catalogFixture(t)
	first := catalogReport(t, 1, "old")
	old := saveReport(t, store, "repo", first)
	newer := saveReport(t, store, "repo", catalogReport(t, 2, "new"))
	assertLatestID(t, store, newer.ID)
	first.Index.Files[0].Symbols[0].Doc = "must-not-overwrite"
	republished := saveReport(t, store, "repo", first)
	if republished.ID != old.ID || !republished.IndexedAt.Equal(old.IndexedAt) {
		t.Fatal("republish changed immutable snapshot metadata")
	}
	assertLatestID(t, store, old.ID)
	symbol, err := store.Symbol(context.Background(), "repo", old.ID, first.Index.Files[0].Symbols[0].ID)
	if err != nil || symbol.Symbol.Doc == "must-not-overwrite" {
		t.Fatal("republish overwrote immutable symbol data")
	}
}

func TestIntegrationCatalogRollbackPreservesPublicationAndAudit(t *testing.T) {
	store, _ := catalogFixture(t)
	old := saveReport(t, store, "repo", catalogReport(t, 1, "valid"))
	broken := catalogReport(t, 2, "broken")
	broken.Index.Files[1].Symbols[0].ID = broken.Index.Files[0].Symbols[0].ID
	_, err := store.SaveSnapshot(context.Background(), "repo", broken, testExecution())
	if !errors.Is(err, ErrUnavailable) {
		t.Fatalf("duplicate symbol should abort transaction, got %v", err)
	}
	assertLatestID(t, store, old.ID)
	assertTableCount(t, store, "developa_snapshots", 1)
	assertTableCount(t, store, "developa_files", 1)
	assertTableCount(t, store, "developa_symbols", 2)
	assertTableCount(t, store, "developa_audit_events", 1)
	assertTableCount(t, store, "developa_audit_outbox", 1)
}

func TestIntegrationCatalogQueryCountDoesNotGrowPerFile(t *testing.T) {
	store, counter := catalogFixture(t)
	var previousWrites int64
	for _, count := range []int{1, 10, 100} {
		report := catalogReport(t, count, fmt.Sprint(count))
		counter.Store(0)
		snapshot, err := store.SaveSnapshot(context.Background(), "repo", report, testExecution())
		writes := counter.Load()
		// Implementation links add one bulk COPY, independent of candidate/file count.
		if err != nil || writes > 11 || (previousWrites > 0 && writes != previousWrites) {
			t.Fatalf("write query budget changed: size=%d queries=%d previous=%d err=%v", count, writes, previousWrites, err)
		}
		previousWrites = writes
		assertReadQueryBudgets(t, store, counter, snapshot)
	}
}

func assertReadQueryBudgets(t *testing.T, store *Store, counter *queryCounter, snapshot domain.Snapshot) {
	t.Helper()
	counter.Store(0)
	_, err := store.Files(context.Background(), "repo", snapshot.ID, domain.Filter{Limit: 100})
	if err != nil || counter.Load() != 1 {
		t.Fatalf("file page requires exactly one SQL query: %d, %v", counter.Load(), err)
	}
	counter.Store(0)
	_, err = store.Symbols(context.Background(), "repo", snapshot.ID, domain.Filter{Limit: 100})
	if err != nil || counter.Load() != 1 {
		t.Fatalf("symbol page requires exactly one SQL query: %d, %v", counter.Load(), err)
	}
}

func TestIntegrationAuditIsSanitizedAndOutboxAtomic(t *testing.T) {
	store, _ := catalogFixture(t)
	execution := testExecution()
	if err := store.RecordExecution(context.Background(), "repo", execution, "queued"); err != nil {
		t.Fatal(err)
	}
	report := catalogReport(t, 1, "audit")
	report.Index.Files[0].Symbols[0].Doc = "secret-source-code"
	saveReport(t, store, "repo", report)
	var audit string
	err := store.pool.QueryRow(context.Background(), `SELECT jsonb_agg(to_jsonb(e))::text FROM developa_audit_events e`).Scan(&audit)
	if err != nil || strings.Contains(audit, "secret-source-code") || !strings.Contains(audit, execution.TraceID) {
		t.Fatal("audit record missing correlation or leaking source")
	}
	assertTableCount(t, store, "developa_audit_events", 2)
	assertTableCount(t, store, "developa_audit_outbox", 2)
}

func TestIntegrationMigrationIsIdempotent(t *testing.T) {
	store, _ := catalogFixture(t)
	snapshot := saveReport(t, store, "repo", catalogReport(t, 1, "retained"))
	if err := store.Migrate(context.Background()); err != nil {
		t.Fatal(err)
	}
	assertTableCount(t, store, "developa_schema_migrations", len(catalogMigrations))
	assertLatestID(t, store, snapshot.ID)
}

func TestIntegrationEmptySnapshotHasEmptyPages(t *testing.T) {
	store, _ := catalogFixture(t)
	snapshot := saveReport(t, store, "repo", catalogReport(t, 0, "empty"))
	files, err := store.Files(context.Background(), "repo", snapshot.ID, domain.Filter{})
	if err != nil || files.Total != 0 || files.Items == nil {
		t.Fatalf("empty file page is invalid: %+v, %v", files, err)
	}
	symbols, err := store.Symbols(context.Background(), "repo", snapshot.ID, domain.Filter{})
	if err != nil || symbols.Total != 0 || symbols.Items == nil {
		t.Fatalf("empty symbol page is invalid: %+v, %v", symbols, err)
	}
}

func TestAuditValidationRejectsUntrustedPayloadFields(t *testing.T) {
	cases := []domain.Execution{
		{Actor: "system", Trigger: "watch"},
		{ID: "execution", Actor: "spoofed", Trigger: "watch"},
		{ID: "execution", Actor: "system", Trigger: "source\ncontents"},
		{ID: "execution", Actor: "system", Trigger: "watch", TraceID: strings.Repeat("a", 129)},
	}
	for _, execution := range cases {
		if validExecution(execution, "completed") {
			t.Fatal("invalid execution was accepted for auditing")
		}
	}
	if validExecution(testExecution(), "raw-error-contents") {
		t.Fatal("arbitrary error message accepted as audit outcome")
	}
}

func TestIntegrationAuditFailureRollsBackPublication(t *testing.T) {
	store, _ := catalogFixture(t)
	old := saveReport(t, store, "repo", catalogReport(t, 1, "audit-valid"))
	_, err := store.pool.Exec(context.Background(), `ALTER TABLE developa_audit_events
		ADD CONSTRAINT test_audit_failure CHECK (execution_id <> 'rejected')`)
	if err != nil {
		t.Fatal(err)
	}
	execution := testExecution()
	execution.ID = "rejected"
	_, err = store.SaveSnapshot(context.Background(), "repo", catalogReport(t, 2, "audit-rejected"), execution)
	if !errors.Is(err, ErrUnavailable) {
		t.Fatalf("audit failure should abort publication, got %v", err)
	}
	assertLatestID(t, store, old.ID)
	assertTableCount(t, store, "developa_snapshots", 1)
	assertTableCount(t, store, "developa_audit_events", 1)
	assertTableCount(t, store, "developa_audit_outbox", 1)
}

func TestIntegrationCanceledSaveDoesNotPublish(t *testing.T) {
	store, _ := catalogFixture(t)
	old := saveReport(t, store, "repo", catalogReport(t, 1, "before-cancel"))
	report := catalogReport(t, 2, "canceled")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := store.SaveSnapshot(ctx, "repo", report, testExecution())
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected cancellation, got %v", err)
	}
	assertLatestID(t, store, old.ID)
	assertTableCount(t, store, "developa_audit_events", 1)
}

func assertLatestID(t *testing.T, store *Store, expected string) {
	t.Helper()
	latest, err := store.Latest(context.Background(), "repo")
	if err != nil || latest.ID != expected {
		t.Fatalf("latest snapshot = %s, expected %s, error=%v", latest.ID, expected, err)
	}
}

func assertTableCount(t *testing.T, store *Store, table string, expected int) {
	t.Helper()
	var actual int
	err := store.pool.QueryRow(context.Background(), "SELECT count(*) FROM "+pgx.Identifier{table}.Sanitize()).Scan(&actual)
	if err != nil || actual != expected {
		t.Fatalf("%s has %d rows, expected %d: %v", table, actual, expected, err)
	}
}

func catalogReport(t *testing.T, count int, seed string) application.Report {
	t.Helper()
	files := make([]goparser.SourceFile, count)
	for i := range files {
		files[i] = goparser.SourceFile{Path: fmt.Sprintf("file%03d.go", i), Content: []byte(fmt.Sprintf("package example\n// Run works.\nfunc Run%03d(value string) error { return nil }\ntype Record%03d struct{}\n", i, i))}
	}
	index, err := goparser.Parse(context.Background(), files)
	if err != nil {
		t.Fatal(err)
	}
	return application.Report{SchemaVersion: "0.1", Snapshot: application.SnapshotInfo{
		Fingerprint: fingerprint(seed), Commit: "commit", Branch: "main", Complete: true, Files: count,
	}, Index: index}
}

func fingerprint(seed string) string {
	hash := sha256.Sum256([]byte(seed))
	return hex.EncodeToString(hash[:])
}

func testExecution() domain.Execution {
	return domain.Execution{ID: "execution-id", Actor: "operator", Trigger: "manual", TraceID: "4bf92f3577b34da6a3ce929d0e0e4736"}
}

func saveReport(t *testing.T, store *Store, repositoryID string, report application.Report) domain.Snapshot {
	t.Helper()
	if err := store.EnsureRepository(context.Background(), domain.Repository{ID: repositoryID, Name: "Test"}); err != nil {
		t.Fatal(err)
	}
	snapshot, err := store.SaveSnapshot(context.Background(), repositoryID, report, testExecution())
	if err != nil {
		t.Fatal(err)
	}
	return snapshot
}

type queryCounter struct{ atomic.Int64 }

func (c *queryCounter) TraceQueryStart(ctx context.Context, _ *pgx.Conn, _ pgx.TraceQueryStartData) context.Context {
	c.Add(1)
	return ctx
}

func (c *queryCounter) TraceQueryEnd(context.Context, *pgx.Conn, pgx.TraceQueryEndData) {}

func (c *queryCounter) TraceCopyFromStart(ctx context.Context, _ *pgx.Conn, _ pgx.TraceCopyFromStartData) context.Context {
	c.Add(1)
	return ctx
}

func (c *queryCounter) TraceCopyFromEnd(context.Context, *pgx.Conn, pgx.TraceCopyFromEndData) {}
