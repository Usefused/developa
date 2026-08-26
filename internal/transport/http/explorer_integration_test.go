package httptransport

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"developa/internal/application"
	"developa/internal/domain"
	source "developa/internal/source/git"
	"developa/internal/store/postgres"
	"github.com/jackc/pgx/v5"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

const integrationTraceID = "4bf92f3577b34da6a3ce929d0e0e4736"
const integrationScanTimeout = 10 * time.Second

type integrationExplorer struct {
	store   *postgres.Store
	server  *httptest.Server
	manager *application.Manager
	worker  *application.AnalysisWorker
	admin   *pgx.Conn
	schema  string
	root    string
}

func TestIntegrationExplorerGitPostgresLifecycle(t *testing.T) {
	exporter := installTraceProvider(t)
	fixture := newIntegrationExplorer(t)
	lock := lockIntegrationAudit(t, fixture)
	fixture.manager.Start(context.Background())
	assertIntegrationAdmission(t, fixture)
	if err := lock.Rollback(context.Background()); err != nil {
		t.Fatal("could not release isolated startup lock")
	}
	initial := awaitIntegrationSnapshot(t, fixture, "")
	original := inspectIntegrationFile(t, fixture, initial.ID)
	modified := modifyIntegrationSource(t, fixture, initial.ID)
	added := addIntegrationSource(t, fixture, modified.ID)
	deleted := deleteIntegrationSource(t, fixture, added.ID)
	assertIntegrationSnapshotIsolation(t, fixture, initial.ID, deleted.ID, original)
	assertIntegrationManualScan(t, fixture, exporter)
}

func newIntegrationExplorer(t *testing.T) *integrationExplorer {
	t.Helper()
	store, admin, schema := integrationStore(t)
	root := integrationGitRepository(t)
	manager, err := application.NewManager(context.Background(), store, application.ManagerConfig{
		RepositoryPath: root, RepositoryName: "HTTP integration fixture", PollInterval: 500 * time.Millisecond, ScanTimeout: integrationScanTimeout,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(manager.Close)
	cfg := testConfig()
	cfg.RequestTimeout = 5 * time.Second
	cfg.Explorer = &Explorer{Catalog: store, Tracker: manager, RepositoryID: manager.Repository().ID, Token: testToken, Knowledge: store}
	server := httptest.NewServer(NewHandler(store, cfg))
	t.Cleanup(server.Close)
	return &integrationExplorer{store: store, server: server, manager: manager, admin: admin, schema: schema, root: root}
}

func integrationStore(t *testing.T) (*postgres.Store, *pgx.Conn, string) {
	t.Helper()
	raw, present := os.LookupEnv("DEVELOPA_TEST_DATABASE_URL")
	if !present {
		t.Skip("set DEVELOPA_TEST_DATABASE_URL to run the real PostgreSQL/Git/HTTP workflow")
	}
	connection := integrationDatabaseURL(t, raw)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	admin, err := pgx.Connect(ctx, connection.String())
	if err != nil {
		t.Fatal("DEVELOPA_TEST_DATABASE_URL could not connect to PostgreSQL")
	}
	t.Cleanup(func() { _ = admin.Close(context.Background()) })
	schema := createIntegrationSchema(t, admin)
	query := connection.Query()
	query.Set("search_path", schema)
	connection.RawQuery = query.Encode()
	store, err := postgres.Open(ctx, postgres.Config{URL: connection.String(), MaxConns: 3, ConnectTimeout: 5 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(store.Close)
	if err := store.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	return store, admin, schema
}

func integrationDatabaseURL(t *testing.T, raw string) *url.URL {
	t.Helper()
	parsed, err := url.Parse(raw)
	if err != nil {
		t.Fatal("DEVELOPA_TEST_DATABASE_URL must be a valid PostgreSQL URL")
	}
	if (parsed.Scheme != "postgres" && parsed.Scheme != "postgresql") || parsed.Hostname() == "" {
		t.Fatal("DEVELOPA_TEST_DATABASE_URL must specify a PostgreSQL host")
	}
	return parsed
}

func createIntegrationSchema(t *testing.T, admin *pgx.Conn) string {
	t.Helper()
	schema := "http_e2e_" + strings.ToLower(rand.Text())
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := admin.Exec(ctx, "CREATE SCHEMA "+pgx.Identifier{schema}.Sanitize()); err != nil {
		t.Fatal("could not create isolated HTTP integration schema")
	}
	t.Cleanup(func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if _, err := admin.Exec(cleanup, "DROP SCHEMA "+pgx.Identifier{schema}.Sanitize()+" CASCADE"); err != nil {
			t.Error("could not drop owned HTTP integration schema")
		}
	})
	return schema
}

func integrationGitRepository(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	integrationGit(t, root, "init", "-q")
	integrationWrite(t, root, "main.go", "package fixture\nfunc Original(name string) string { return name }\n")
	integrationWrite(t, root, "removed.go", "package fixture\nfunc Removed() {}\n")
	integrationGit(t, root, "add", "--", "main.go", "removed.go")
	integrationGit(t, root, "-c", "user.name=Integration Fixture", "-c", "user.email=fixture@example.invalid", "-c", "commit.gpgsign=false", "commit", "-qm", "fixture")
	return root
}

func integrationGit(t *testing.T, root string, args ...string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, "git", args...)
	command.Dir = root
	// Inherited GIT_DIR/GIT_INDEX_FILE settings must never redirect fixture writes
	// into a developer's checkout or index.
	command.Env = []string{"PATH=" + os.Getenv("PATH"), "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=/dev/null", "GIT_TERMINAL_PROMPT=0", "LC_ALL=C"}
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("fixture Git command failed: %v: %s", err, output)
	}
}

func integrationWrite(t *testing.T, root, name, contents string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(root, name), []byte(contents), 0600); err != nil {
		t.Fatal(err)
	}
}

func lockIntegrationAudit(t *testing.T, fixture *integrationExplorer) pgx.Tx {
	t.Helper()
	tx, err := fixture.admin.Begin(context.Background())
	if err != nil {
		t.Fatal("could not open isolated startup lock transaction")
	}
	t.Cleanup(func() { _ = tx.Rollback(context.Background()) })
	// Blocking startup persistence makes busy admission deterministic without
	// replacing real Git/DB work with a mock or relying on a scheduler race.
	query := "LOCK TABLE " + pgx.Identifier{fixture.schema, "developa_audit_events"}.Sanitize() + " IN ACCESS EXCLUSIVE MODE"
	if _, err := tx.Exec(context.Background(), query); err != nil {
		t.Fatal("could not lock isolated startup audit table")
	}
	return tx
}

func assertIntegrationAdmission(t *testing.T, fixture *integrationExplorer) {
	t.Helper()
	status := integrationRequest(t, fixture, http.MethodGet, "/api/project", false, nil)
	if status != http.StatusUnauthorized {
		t.Fatalf("unauthorized project request returned %d", status)
	}
	status = integrationRequest(t, fixture, http.MethodPost, "/api/scan", true, nil)
	if status != http.StatusConflict {
		t.Fatalf("scan while startup is blocked returned %d", status)
	}
}

func integrationRequest(t *testing.T, fixture *integrationExplorer, method, path string, authenticated bool, target any) int {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, method, fixture.server.URL+path, nil)
	if err != nil {
		t.Fatal(err)
	}
	if authenticated {
		request.Header.Set("Authorization", "Bearer "+testToken)
	}
	request.Header.Set("traceparent", "00-"+integrationTraceID+"-00f067aa0ba902b7-01")
	response, err := fixture.server.Client().Do(request)
	if err != nil {
		t.Fatal("HTTP integration request failed")
	}
	defer response.Body.Close()
	if target != nil {
		if err := json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(target); err != nil {
			t.Fatal(err)
		}
	}
	return response.StatusCode
}

func integrationRead(t *testing.T, fixture *integrationExplorer, path string, target any) {
	t.Helper()
	if status := integrationRequest(t, fixture, http.MethodGet, path, true, target); status != http.StatusOK {
		t.Fatalf("authorized integration read returned %d", status)
	}
}

func awaitIntegrationSnapshot(t *testing.T, fixture *integrationExplorer, previous string) domain.Snapshot {
	t.Helper()
	deadline := time.NewTimer(12 * time.Second)
	defer deadline.Stop()
	ticker := time.NewTicker(25 * time.Millisecond)
	defer ticker.Stop()
	for {
		var project domain.Project
		integrationRead(t, fixture, "/api/project", &project)
		if project.Snapshot != nil && project.Snapshot.ID != previous {
			return *project.Snapshot
		}
		select {
		case <-deadline.C:
			t.Fatalf("snapshot publication timed out: status=%s", project.Status)
		case <-ticker.C:
		}
	}
}

func inspectIntegrationFile(t *testing.T, fixture *integrationExplorer, snapshot string) domain.SymbolDetail {
	t.Helper()
	base := "/api/snapshots/" + snapshot
	var files domain.FilePage
	integrationRead(t, fixture, base+"/files", &files)
	if files.Total != 2 || len(files.Items) != 2 {
		t.Fatalf("startup file inventory mismatch: %+v", files)
	}
	var file domain.FileDetail
	integrationRead(t, fixture, base+"/file?path=main.go", &file)
	if file.Path != "main.go" || file.SymbolCount != 1 {
		t.Fatalf("selected file mismatch: %+v", file)
	}
	var symbols domain.SymbolPage
	integrationRead(t, fixture, base+"/symbols?file=main.go&kind=function", &symbols)
	if symbols.Total != 1 || len(symbols.Items) != 1 {
		t.Fatalf("selected file symbols mismatch: %+v", symbols)
	}
	return readIntegrationSymbol(t, fixture, snapshot, symbols.Items[0].Symbol.ID)
}

func readIntegrationSymbol(t *testing.T, fixture *integrationExplorer, snapshot, symbol string) domain.SymbolDetail {
	t.Helper()
	var detail domain.SymbolDetail
	integrationRead(t, fixture, "/api/snapshots/"+snapshot+"/symbols/"+symbol, &detail)
	return detail
}

func modifyIntegrationSource(t *testing.T, fixture *integrationExplorer, previous string) domain.Snapshot {
	t.Helper()
	integrationWrite(t, fixture.root, "main.go", "package fixture\nfunc Original(name string) string { return name + \" changed\" }\n")
	snapshot := awaitIntegrationSnapshot(t, fixture, previous)
	assertIntegrationChange(t, fixture, snapshot.ID, "main.go", source.Modified)
	return snapshot
}

func addIntegrationSource(t *testing.T, fixture *integrationExplorer, previous string) domain.Snapshot {
	t.Helper()
	integrationWrite(t, fixture.root, "added.go", "package fixture\nfunc Added() {}\n")
	snapshot := awaitIntegrationSnapshot(t, fixture, previous)
	assertIntegrationChange(t, fixture, snapshot.ID, "added.go", source.Added)
	if snapshot.FileCount != 3 {
		t.Fatalf("new source file was not indexed: %d", snapshot.FileCount)
	}
	return snapshot
}

func deleteIntegrationSource(t *testing.T, fixture *integrationExplorer, previous string) domain.Snapshot {
	t.Helper()
	if err := os.Remove(filepath.Join(fixture.root, "removed.go")); err != nil {
		t.Fatal(err)
	}
	snapshot := awaitIntegrationSnapshot(t, fixture, previous)
	assertIntegrationChange(t, fixture, snapshot.ID, "removed.go", source.Deleted)
	status := integrationRequest(t, fixture, http.MethodGet, "/api/snapshots/"+snapshot.ID+"/file?path=removed.go", true, nil)
	if status != http.StatusNotFound {
		t.Fatalf("deleted file remained in latest snapshot: %d", status)
	}
	return snapshot
}

func assertIntegrationChange(t *testing.T, fixture *integrationExplorer, snapshot, path string, kind source.ChangeKind) {
	t.Helper()
	var details domain.SnapshotDetails
	integrationRead(t, fixture, "/api/snapshots/"+snapshot+"/details", &details)
	if !details.Snapshot.ChangesKnown {
		t.Fatal("live comparison baseline was lost")
	}
	for _, change := range details.Changes {
		if change.Path == path && change.Kind == kind {
			return
		}
	}
	t.Fatalf("snapshot lacks expected %s change for %s", kind, path)
}

func assertIntegrationSnapshotIsolation(t *testing.T, fixture *integrationExplorer, oldID, newID string, original domain.SymbolDetail) {
	t.Helper()
	old := readIntegrationSymbol(t, fixture, oldID, original.Symbol.ID)
	newer := readIntegrationSymbol(t, fixture, newID, original.Symbol.ID)
	if old.Symbol.ContentHash != original.Symbol.ContentHash || old.Symbol.SourceID != original.Symbol.SourceID {
		t.Fatal("historical symbol changed after a new publication")
	}
	if newer.Symbol.ContentHash == original.Symbol.ContentHash || newer.Symbol.ID != original.Symbol.ID {
		t.Fatal("edited symbol did not preserve logical identity and change its content hash")
	}
	if old.Symbol.Name != "Original" || old.Symbol.Parameters[0].Name != "name" || old.Symbol.Results[0].Type != "string" {
		t.Fatalf("function detail metadata was lost: %+v", old.Symbol)
	}
}

func assertIntegrationManualScan(t *testing.T, fixture *integrationExplorer, exporter *tracetest.InMemoryExporter) {
	t.Helper()
	execution := awaitIntegrationScanAcceptance(t, fixture)
	awaitIntegrationScanCompletion(t, fixture, execution)
	assertIntegrationAudit(t, fixture, execution)
	assertIntegrationScanTrace(t, exporter, execution)
}

func awaitIntegrationScanAcceptance(t *testing.T, fixture *integrationExplorer) domain.Execution {
	t.Helper()
	return awaitIntegrationScanAcceptancePath(t, fixture, "/api/scan")
}

func awaitIntegrationScanAcceptancePath(t *testing.T, fixture *integrationExplorer, path string) domain.Execution {
	t.Helper()
	// An already admitted watch scan may use its full deadline and five-second
	// final audit reserve before the manual request can be accepted.
	deadline := time.NewTimer(integrationScanTimeout + 5*time.Second)
	defer deadline.Stop()
	ticker := time.NewTicker(25 * time.Millisecond)
	defer ticker.Stop()
	for {
		var execution domain.Execution
		status := integrationRequest(t, fixture, http.MethodPost, path, true, &execution)
		if status == http.StatusAccepted {
			return execution
		}
		if status != http.StatusConflict {
			t.Fatalf("manual scan returned unexpected status: %d", status)
		}
		select {
		case <-deadline.C:
			project, _ := fixture.manager.Project(context.Background())
			t.Fatalf("manual scan was never accepted: status=%s last_error=%s", project.Status, project.LastError)
		case <-ticker.C:
		}
	}
}

func awaitIntegrationScanCompletion(t *testing.T, fixture *integrationExplorer, execution domain.Execution) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), integrationScanTimeout+5*time.Second)
	defer cancel()
	ticker := time.NewTicker(25 * time.Millisecond)
	defer ticker.Stop()
	query := "SELECT count(*) FROM " + pgx.Identifier{fixture.schema, "developa_audit_events"}.Sanitize() + " WHERE execution_id=$1 AND outcome='completed'"
	for {
		var count int
		if err := fixture.admin.QueryRow(ctx, query, execution.ID).Scan(&count); err != nil {
			t.Fatal("could not inspect owned execution audit")
		}
		if count > 0 {
			return
		}
		select {
		case <-ctx.Done():
			t.Fatal("manual scan completion audit timed out")
		case <-ticker.C:
		}
	}
}

func assertIntegrationAudit(t *testing.T, fixture *integrationExplorer, execution domain.Execution) {
	t.Helper()
	if execution.Actor != "operator" || execution.TraceID != integrationTraceID {
		t.Fatalf("manual execution lost authenticated actor or parent trace: %+v", execution)
	}
	events := pgx.Identifier{fixture.schema, "developa_audit_events"}.Sanitize()
	outbox := pgx.Identifier{fixture.schema, "developa_audit_outbox"}.Sanitize()
	query := "SELECT count(*), count(o.event_id), bool_and(e.actor='operator' AND e.trace_id=$2) FROM " + events + " e LEFT JOIN " + outbox + " o ON o.event_id=e.id WHERE e.execution_id=$1"
	var count, exports int
	var correlated bool
	err := fixture.admin.QueryRow(context.Background(), query, execution.ID, integrationTraceID).Scan(&count, &exports, &correlated)
	if err != nil || count != 3 || exports != count || !correlated {
		t.Fatalf("queued/running/completed audit and outbox correlation failed: events=%d exports=%d correlated=%t", count, exports, correlated)
	}
}

func assertIntegrationScanTrace(t *testing.T, exporter *tracetest.InMemoryExporter, execution domain.Execution) {
	t.Helper()
	request := awaitIntegrationExecutionSpan(t, exporter, "repository.request_scan", execution.ID)
	scan := awaitIntegrationExecutionSpan(t, exporter, "repository.scan", execution.ID)
	if scan.Parent.SpanID() != request.SpanContext.SpanID() || scan.SpanContext.TraceID().String() != integrationTraceID {
		t.Fatal("background scan lost the accepted HTTP execution trace")
	}
}

func awaitIntegrationExecutionSpan(t *testing.T, exporter *tracetest.InMemoryExporter, name, execution string) tracetest.SpanStub {
	t.Helper()
	deadline := time.NewTimer(integrationScanTimeout)
	defer deadline.Stop()
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		span := integrationExecutionSpan(exporter.GetSpans(), name, execution)
		if span.SpanContext.IsValid() {
			return span
		}
		// PostgreSQL commits the completion audit before the deferred span.End;
		// waiting for export verifies completion without assuming goroutine ordering.
		select {
		case <-deadline.C:
			t.Fatalf("completed execution span was not exported: %s", name)
		case <-ticker.C:
		}
	}
}

func integrationExecutionSpan(spans tracetest.SpanStubs, name, execution string) tracetest.SpanStub {
	for _, span := range spans {
		if span.Name != name {
			continue
		}
		for _, attribute := range span.Attributes {
			if string(attribute.Key) == "execution.id" && attribute.Value.AsString() == execution {
				return span
			}
		}
	}
	return tracetest.SpanStub{}
}
