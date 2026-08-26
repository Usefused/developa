package httptransport

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"developa/internal/application"
	"developa/internal/domain"
	"developa/internal/store/postgres"
	"github.com/jackc/pgx/v5"
)

type integrationWorkspaceRuntime struct {
	group *application.Workspaces
	store *postgres.Store
}

type resolutionRuntime struct{ explorer *Explorer }

func (r resolutionRuntime) Explorer(string) (*Explorer, error) { return r.explorer, nil }
func (r resolutionRuntime) RepositoryIDs() []string            { return []string{r.explorer.RepositoryID} }

type resolutionManagement struct {
	request domain.ResolveRepositoryRequest
	result  domain.RepositorySummary
	calls   int
}

func (*resolutionManagement) FolderRoots(context.Context) ([]domain.FolderRoot, error) {
	return nil, nil
}
func (*resolutionManagement) Folders(context.Context, string, string, int) (domain.FolderPage, error) {
	return domain.FolderPage{}, nil
}
func (*resolutionManagement) AddWorkspace(context.Context, domain.AddWorkspaceRequest) (domain.AddedWorkspace, error) {
	return domain.AddedWorkspace{}, nil
}
func (m *resolutionManagement) ResolveRepository(_ context.Context, request domain.ResolveRepositoryRequest) (domain.RepositorySummary, error) {
	m.request, m.calls = request, m.calls+1
	return m.result, nil
}

func (runtime integrationWorkspaceRuntime) Explorer(id string) (*Explorer, error) {
	manager := runtime.group.Default()
	if id != "" {
		manager = runtime.group.Find(id)
	}
	if manager == nil {
		return nil, domain.ErrNotFound
	}
	return &Explorer{Catalog: runtime.store, Tracker: manager, RepositoryID: manager.Repository().ID, Token: testToken, WorkspaceManagement: true}, nil
}

func (runtime integrationWorkspaceRuntime) RepositoryIDs() []string {
	ids := []string{}
	for _, manager := range runtime.group.Managers() {
		ids = append(ids, manager.Repository().ID)
	}
	return ids
}

func managedFixture(t *testing.T, store *postgres.Store, paths []string) (*application.Workspaces, *integrationExplorer, []domain.FolderRoot) {
	t.Helper()
	defaults := application.ManagerConfig{PollInterval: 500 * time.Millisecond, ScanTimeout: integrationScanTimeout}
	group, err := application.NewPersistentWorkspaces(context.Background(), store, nil, defaults)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(group.Close)
	service, err := application.NewWorkspaceService(group, defaults, paths)
	if err != nil {
		t.Fatal(err)
	}
	runtime := integrationWorkspaceRuntime{group: group, store: store}
	first, _ := runtime.Explorer("")
	cfg := testConfig()
	cfg.Explorer, cfg.RepositoryCatalog, cfg.WorkspaceRuntime, cfg.WorkspaceManagement = first, store, runtime, service
	server := httptest.NewServer(NewHandler(store, cfg))
	t.Cleanup(server.Close)
	group.Start(context.Background())
	roots, _ := service.FolderRoots(context.Background())
	return group, &integrationExplorer{store: store, manager: group.Default(), server: server}, roots
}

func TestIntegrationWorkspaceAdditionPersistsAndRestoresWithoutEnvironmentEntries(t *testing.T) {
	installTraceProvider(t)
	store, admin, schema := integrationStore(t)
	path := integrationGitRepository(t)
	group, fixture, roots := managedFixture(t, store, []string{path, t.TempDir()})
	assertWorkspaceAccessAndValidation(t, fixture, roots)
	request := domain.AddWorkspaceRequest{RootID: roots[0].ID, Path: ".", Name: "Added from browser"}
	added := postWorkspace(t, fixture, request, 201)
	if duplicate := postWorkspace(t, fixture, request, 200); !duplicate.AlreadyAdded || duplicate.ID != added.ID {
		t.Fatal("duplicate did not reuse the workspace")
	}
	resolved := resolveWorkspace(t, fixture, path, http.StatusOK)
	if resolved.ID != added.ID || resolved.Name != added.Name {
		t.Fatal("path resolution returned the wrong workspace", resolved)
	}
	fixture.manager = group.Find(added.ID)
	first := awaitRepositorySnapshot(t, fixture, "")
	var project domain.Project
	integrationRead(t, fixture, "/api/project", &project)
	if project.Repository.ID != added.ID {
		t.Fatal("default route did not switch from empty engine to added workspace")
	}
	assertAddedWorkspaceAudit(t, admin, schema, added.ID)
	group.Close()
	fixture.server.Close()
	restored, next, _ := managedFixture(t, store, []string{path})
	if len(restored.Managers()) != 1 {
		t.Fatal("database registration was not restored")
	}
	assertUnchangedRepository(t, next, first.ID)
	integrationWrite(t, path, "main.go", "package fixture\nfunc AfterRestart() {}\n")
	updated := awaitRepositorySnapshot(t, next, first.ID)
	assertUnavailableSavedWorkspace(t, store, restored, next, path, updated.ID)
}

func assertUnavailableSavedWorkspace(t *testing.T, store *postgres.Store, group *application.Workspaces, fixture *integrationExplorer, path, snapshot string) {
	t.Helper()
	group.Close()
	fixture.server.Close()
	if err := os.Rename(path, filepath.Join(t.TempDir(), "unavailable")); err != nil {
		t.Fatal(err)
	}
	_, restored, _ := managedFixture(t, store, nil)
	var project domain.Project
	integrationRead(t, restored, repositoryPrefix(restored)+"/project", &project)
	if project.Status != "error" || project.Snapshot == nil || project.Snapshot.ID != snapshot {
		t.Fatal("unavailable folder discarded saved workspace context")
	}
	var symbols domain.SymbolPage
	integrationRead(t, restored, repositoryPrefix(restored)+"/snapshots/"+snapshot+"/symbols?q=AfterRestart", &symbols)
	if symbols.Total != 1 {
		t.Fatal("saved source became unreadable while its checkout was unavailable")
	}
}

func postWorkspace(t *testing.T, fixture *integrationExplorer, value domain.AddWorkspaceRequest, status int) domain.AddedWorkspace {
	t.Helper()
	data, _ := json.Marshal(value)
	request, err := http.NewRequest(http.MethodPost, fixture.server.URL+"/api/repositories", strings.NewReader(string(data)))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Authorization", "Bearer "+testToken)
	request.Header.Set("Content-Type", "application/json")
	response, err := fixture.server.Client().Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != status {
		var payload any
		_ = json.NewDecoder(response.Body).Decode(&payload)
		t.Fatalf("add workspace status=%d want=%d: %v", response.StatusCode, status, payload)
	}
	var result domain.AddedWorkspace
	if err := json.NewDecoder(response.Body).Decode(&result); err != nil {
		t.Fatal(err)
	}
	return result
}

func resolveWorkspace(t *testing.T, fixture *integrationExplorer, path string, status int) domain.RepositorySummary {
	t.Helper()
	data, _ := json.Marshal(domain.ResolveRepositoryRequest{Path: path})
	request, err := http.NewRequest(http.MethodPost, fixture.server.URL+"/api/repositories/resolve", strings.NewReader(string(data)))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Authorization", "Bearer "+testToken)
	request.Header.Set("Content-Type", "application/json")
	response, err := fixture.server.Client().Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != status {
		t.Fatalf("resolve workspace status=%d want=%d", response.StatusCode, status)
	}
	var result domain.RepositorySummary
	if status == http.StatusOK {
		if err := json.NewDecoder(response.Body).Decode(&result); err != nil {
			t.Fatal(err)
		}
	}
	return result
}

func assertWorkspaceAccessAndValidation(t *testing.T, fixture *integrationExplorer, roots []domain.FolderRoot) {
	t.Helper()
	for _, path := range []string{"/api/workspace-roots", "/api/workspace-folders?root_id=" + roots[0].ID + "&path=.", "/api/project"} {
		if status := integrationRequest(t, fixture, http.MethodGet, path, false, nil); status != 401 {
			t.Fatal("filesystem management escaped root authentication", status)
		}
	}
	postWorkspace(t, fixture, domain.AddWorkspaceRequest{RootID: roots[1].ID, Path: "."}, 422)
	postWorkspace(t, fixture, domain.AddWorkspaceRequest{RootID: roots[0].ID, Path: "../"}, 400)
	postWorkspace(t, fixture, domain.AddWorkspaceRequest{RootID: "unknown", Path: "."}, 403)
	resolveWorkspace(t, fixture, "relative/repository", http.StatusBadRequest)
	resolveWorkspace(t, fixture, filepath.Join(t.TempDir(), "unknown"), http.StatusNotFound)
	var page domain.RepositoryPage
	integrationRead(t, fixture, "/api/repositories", &page)
	if page.Total != 0 {
		t.Fatal("failed validation persisted a workspace")
	}
}

func assertAddedWorkspaceAudit(t *testing.T, admin *pgx.Conn, schema, id string) {
	t.Helper()
	var valid bool
	query := `SELECT count(*)=1 AND bool_and(actor='operator' AND trace_id<>'' AND outcome='completed') FROM ` + pgx.Identifier{schema, "developa_audit_events"}.Sanitize() + ` WHERE repository_id=$1 AND trigger='workspace.add'`
	if err := admin.QueryRow(context.Background(), query, id).Scan(&valid); err != nil || !valid {
		t.Fatal("workspace mutation missing correlated durable audit", err)
	}
}

func TestWorkspaceRequestRejectsUnknownFieldsAndCrossOriginMutation(t *testing.T) {
	for _, body := range []string{`{"root_id":"x","path":".","token":"override"}`, `{} {}`, `null`, strings.Repeat("x", 8193)} {
		request := httptest.NewRequest(http.MethodPost, "/api/repositories", strings.NewReader(body))
		_, err := workspaceBody(httptest.NewRecorder(), request)
		if err == nil {
			t.Fatal("invalid registration body accepted")
		}
	}
	routes := managedWorkspaces{}
	request := httptest.NewRequest(http.MethodPost, "http://localhost/api/repositories", strings.NewReader(`{}`))
	request.Header.Set("Origin", "http://attacker.invalid")
	response := httptest.NewRecorder()
	routes.add(response, request)
	if response.Code != 403 {
		t.Fatal("cross origin registration reached the service")
	}
	resolve := httptest.NewRequest(http.MethodPost, "/api/repositories/resolve", strings.NewReader(`{"path":"/repository","extra":true}`))
	resolve.Header.Set("Content-Type", "application/json")
	if _, err := repositoryPathBody(httptest.NewRecorder(), resolve); !errors.Is(err, domain.ErrInvalidInput) {
		t.Fatal("repository resolution accepted unknown input")
	}
}

func TestRepositoryPathResolutionIsAuthenticatedAndReturnsNoPath(t *testing.T) {
	id := strings.Repeat("a", 64)
	explorer := &Explorer{RepositoryID: id, Token: testToken, WorkspaceManagement: true}
	management := &resolutionManagement{result: domain.RepositorySummary{Repository: domain.Repository{ID: id, Name: "API"}}}
	cfg := testConfig()
	cfg.Explorer, cfg.WorkspaceRuntime, cfg.WorkspaceManagement = explorer, resolutionRuntime{explorer: explorer}, management
	handler := NewHandler(nil, cfg)
	body := `{"path":"/repositories/api"}`
	unauthorized := httptest.NewRecorder()
	handler.ServeHTTP(unauthorized, httptest.NewRequest(http.MethodPost, "/api/repositories/resolve", strings.NewReader(body)))
	if unauthorized.Code != http.StatusUnauthorized || management.calls != 0 {
		t.Fatal("repository path resolved before authentication")
	}
	response := authorizedRequest(handler, http.MethodPost, "/api/repositories/resolve", body)
	if response.Code != http.StatusOK || management.calls != 1 || management.request.Path != "/repositories/api" {
		t.Fatal("repository path resolution did not reach its service", response.Code, response.Body)
	}
	if strings.Contains(response.Body.String(), "/repositories/api") {
		t.Fatal("repository resolution echoed a server checkout path")
	}
}
