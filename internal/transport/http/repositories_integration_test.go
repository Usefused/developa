package httptransport

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"developa/internal/application"
	"developa/internal/domain"
	"github.com/jackc/pgx/v5"
)

func TestIntegrationRepositoriesHaveIndependentGitWatchersAndScopedAPIs(t *testing.T) {
	installTraceProvider(t)
	group, fixtures := integrationRepositories(t)
	group.Start(context.Background())
	first := awaitRepositorySnapshot(t, fixtures[0], "")
	second := awaitRepositorySnapshot(t, fixtures[1], "")
	assertRepositoryList(t, fixtures[0], first.ID, second.ID)
	assertRepositorySnapshotIsolation(t, fixtures[1], first.ID)
	assertRepositoryEventScope(t, fixtures[1], second.ID)
	assertRepositoryWatchers(t, fixtures, first, second)
	assertScopedManualScan(t, fixtures[1])
	assertRepositoryRestart(t, group, fixtures)
}

func integrationRepositories(t *testing.T) (*application.Workspaces, []*integrationExplorer) {
	t.Helper()
	store, admin, schema := integrationStore(t)
	configs := []application.ManagerConfig{}
	for _, name := range []string{"Alpha", "Beta"} {
		root := integrationGitRepository(t)
		integrationWrite(t, root, "main.go", fmt.Sprintf("package fixture\nfunc %s() {}\n", name))
		configs = append(configs, application.ManagerConfig{RepositoryPath: root, RepositoryName: name, PollInterval: 500 * time.Millisecond, ScanTimeout: integrationScanTimeout})
	}
	group, err := application.NewWorkspaces(context.Background(), store, configs)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(group.Close)
	cfg := testConfig()
	cfg.RequestTimeout, cfg.RepositoryCatalog = 5*time.Second, store
	fixtures := []*integrationExplorer{}
	for i, manager := range group.Managers() {
		worker, err := application.NewAnalysisWorker(store, nil, application.AnalysisWorkerConfig{RepositoryID: manager.Repository().ID})
		if err != nil {
			t.Fatal(err)
		}
		cfg.Explorers = append(cfg.Explorers, &Explorer{Catalog: store, Tracker: manager, RepositoryID: manager.Repository().ID, Token: testToken, Knowledge: store, Jobs: worker})
		fixtures = append(fixtures, &integrationExplorer{store: store, manager: manager, worker: worker, admin: admin, schema: schema, root: configs[i].RepositoryPath})
	}
	server := httptest.NewServer(NewHandler(store, cfg))
	t.Cleanup(server.Close)
	for _, fixture := range fixtures {
		fixture.server = server
	}
	return group, fixtures
}

func repositoryPrefix(fixture *integrationExplorer) string {
	return "/api/repositories/" + fixture.manager.Repository().ID
}

func awaitRepositorySnapshot(t *testing.T, fixture *integrationExplorer, previous string) domain.Snapshot {
	t.Helper()
	deadline := time.NewTimer(12 * time.Second)
	defer deadline.Stop()
	ticker := time.NewTicker(25 * time.Millisecond)
	defer ticker.Stop()
	for {
		var project domain.Project
		integrationRead(t, fixture, repositoryPrefix(fixture)+"/project", &project)
		if project.Repository.ID != fixture.manager.Repository().ID {
			t.Fatal("scoped project selected another repository's tracker")
		}
		if project.Snapshot != nil && project.Snapshot.ID != previous {
			return *project.Snapshot
		}
		select {
		case <-deadline.C:
			t.Fatalf("repository snapshot timed out: %s", project.Status)
		case <-ticker.C:
		}
	}
}

func assertRepositoryList(t *testing.T, fixture *integrationExplorer, first, second string) {
	t.Helper()
	if err := fixture.store.EnsureRepository(context.Background(), domain.Repository{ID: strings.Repeat("e", 64), Name: "Retained but not configured"}); err != nil {
		t.Fatal(err)
	}
	var page domain.RepositoryPage
	integrationRead(t, fixture, "/api/repositories", &page)
	if page.Total != 2 || len(page.Items) != 2 || page.DefaultRepositoryID != fixture.manager.Repository().ID {
		t.Fatalf("list exposed unconfigured repositories or lost default: %+v", page)
	}
	assertRepositoryListSnapshots(t, page, first, second)
	var project domain.Project
	integrationRead(t, fixture, "/api/project", &project)
	if project.Repository.ID != page.DefaultRepositoryID || project.Snapshot.ID != first {
		t.Fatal("legacy project alias stopped targeting the first configured repository")
	}
}

func assertRepositoryListSnapshots(t *testing.T, page domain.RepositoryPage, first, second string) {
	t.Helper()
	if page.Items[0].Snapshot == nil || page.Items[1].Snapshot == nil {
		t.Fatal("configured indexed repository omitted its latest publication")
	}
	if page.Items[0].Snapshot.ID != first || page.Items[1].Snapshot.ID != second || page.Items[0].Name != "Alpha" || page.Items[1].Name != "Beta" {
		t.Fatal("list mixed latest snapshot identities")
	}
}

func assertRepositorySnapshotIsolation(t *testing.T, fixture *integrationExplorer, otherSnapshot string) {
	t.Helper()
	paths := []string{"/files", "/file?path=main.go", "/symbols", "/details", "/calls", "/flow", "/context", "/features", "/function-reviews", "/analysis-job", "/events"}
	for _, path := range paths {
		status := integrationRequest(t, fixture, http.MethodGet, repositoryPrefix(fixture)+"/snapshots/"+otherSnapshot+path, true, nil)
		if status != http.StatusNotFound {
			t.Fatalf("cross-repository snapshot reached %s: status=%d", path, status)
		}
	}
	path := "/api/repositories/" + strings.Repeat("e", 64) + "/project"
	if status := integrationRequest(t, fixture, http.MethodGet, path, true, nil); status != 404 {
		t.Fatal("retained unconfigured repository was addressable")
	}
}

func assertRepositoryWatchers(t *testing.T, fixtures []*integrationExplorer, first, second domain.Snapshot) {
	t.Helper()
	integrationWrite(t, fixtures[0].root, "main.go", "package fixture\nfunc AlphaChanged() {}\n")
	changedFirst := awaitRepositorySnapshot(t, fixtures[0], first.ID)
	assertUnchangedRepository(t, fixtures[1], second.ID)
	integrationWrite(t, fixtures[1].root, "main.go", "package fixture\nfunc BetaChanged() {}\n")
	changedSecond := awaitRepositorySnapshot(t, fixtures[1], second.ID)
	assertUnchangedRepository(t, fixtures[0], changedFirst.ID)
	var symbols domain.SymbolPage
	integrationRead(t, fixtures[1], repositoryPrefix(fixtures[1])+"/snapshots/"+changedSecond.ID+"/symbols?q=BetaChanged", &symbols)
	if symbols.Total != 1 || len(symbols.Items) != 1 || symbols.Items[0].Symbol.Name != "BetaChanged" {
		t.Fatal("second repository watcher did not persist independently parsed source")
	}
}

func assertUnchangedRepository(t *testing.T, fixture *integrationExplorer, snapshot string) {
	t.Helper()
	var project domain.Project
	integrationRead(t, fixture, repositoryPrefix(fixture)+"/project", &project)
	if project.Snapshot == nil || project.Snapshot.ID != snapshot || !project.Watching {
		t.Fatal("another repository's edit changed this repository or stopped its watcher")
	}
}

func assertRepositoryRestart(t *testing.T, group *application.Workspaces, fixtures []*integrationExplorer) {
	t.Helper()
	configs, previous := []application.ManagerConfig{}, []domain.Snapshot{}
	for _, fixture := range fixtures {
		previous = append(previous, awaitRepositorySnapshot(t, fixture, ""))
		configs = append(configs, application.ManagerConfig{RepositoryPath: fixture.root, RepositoryName: fixture.manager.Repository().Name,
			PollInterval: 500 * time.Millisecond, ScanTimeout: integrationScanTimeout})
	}
	group.Close()
	restored, err := application.NewWorkspaces(context.Background(), fixtures[0].store, configs)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(restored.Close)
	cfg := testConfig()
	cfg.RepositoryCatalog = fixtures[0].store
	for _, manager := range restored.Managers() {
		cfg.Explorers = append(cfg.Explorers, &Explorer{Catalog: fixtures[0].store, Tracker: manager, RepositoryID: manager.Repository().ID, Token: testToken})
	}
	server := httptest.NewServer(NewHandler(fixtures[0].store, cfg))
	t.Cleanup(server.Close)
	restored.Start(context.Background())
	for i, manager := range restored.Managers() {
		fixture := *fixtures[i]
		fixture.manager, fixture.server = manager, server
		assertUnchangedRepository(t, &fixture, previous[i].ID)
	}
}

func assertScopedManualScan(t *testing.T, fixture *integrationExplorer) {
	t.Helper()
	execution := awaitIntegrationScanAcceptancePath(t, fixture, repositoryPrefix(fixture)+"/scan")
	awaitIntegrationScanCompletion(t, fixture, execution)
	assertIntegrationAudit(t, fixture, execution)
	query := "SELECT bool_and(repository_id=$2) FROM " + pgx.Identifier{fixture.schema, "developa_audit_events"}.Sanitize() + " WHERE execution_id=$1"
	var scoped bool
	if err := fixture.admin.QueryRow(context.Background(), query, execution.ID, fixture.manager.Repository().ID).Scan(&scoped); err != nil || !scoped {
		t.Fatal("manual scan audit crossed its configured repository boundary")
	}
}

func assertRepositoryEventScope(t *testing.T, fixture *integrationExplorer, snapshot string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, fixture.server.URL+repositoryPrefix(fixture)+"/snapshots/"+snapshot+"/events", nil)
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Authorization", "Bearer "+testToken)
	response, err := fixture.server.Client().Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	event := assertObservedEvent(t, bufio.NewReader(response.Body), "analysis")
	var job domain.AnalysisJob
	if err := json.Unmarshal(event.Data, &job); err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != 200 || job.SnapshotID != snapshot || job.Status != "not_queued" {
		t.Fatal("repository event stream lost its source scope or started inference")
	}
}
