package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"developa/internal/application"
	"developa/internal/config"
	"developa/internal/domain"
	"developa/internal/store/postgres"
)

func TestRepositoryManagerConfigurationPreservesLegacyAndConfiguredOrder(t *testing.T) {
	cfg := config.Config{RepositoryPath: "legacy", RepositoryName: "Legacy", WatchInterval: time.Second, ScanTimeout: 3 * time.Second,
		SourceMaxFileBytes: 8 << 20, SourceMaxTotalBytes: 256 << 20}
	managers := repositoryManagers(cfg)
	if len(managers) != 1 || managers[0].RepositoryPath != "legacy" || managers[0].PollInterval != time.Second ||
		managers[0].MaxFileBytes != 8<<20 || managers[0].MaxTotalBytes != 256<<20 {
		t.Fatal("legacy tracker configuration was not retained")
	}
	cfg.RepositoryPath = ""
	cfg.Repositories = []config.Repository{{Name: "API", Path: "/api"}, {Name: "Worker", Path: "/worker"}}
	managers = repositoryManagers(cfg)
	if len(managers) != 2 || managers[0].RepositoryName != "API" || managers[1].RepositoryPath != "/worker" {
		t.Fatal("multi-repository default order changed")
	}
}

func TestUnconfiguredWorkspaceCompositionServesInfoWithoutModelOrDatabaseCalls(t *testing.T) {
	store := &postgres.Store{}
	group, err := application.NewWorkspaces(context.Background(), store, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer group.Close()
	cfg := config.Config{RequestTimeout: time.Second, ReadinessTimeout: time.Second, AITimeout: time.Second}
	server, stop, err := managedExplorerServer(context.Background(), store, group, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer stop()
	response := httptest.NewRecorder()
	server.Handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/info", nil))
	if response.Code != http.StatusOK {
		t.Fatal("unconfigured composition did not preserve its disabled runtime")
	}
	var info struct{ Configured bool }
	if err := json.Unmarshal(response.Body.Bytes(), &info); err != nil || info.Configured {
		t.Fatal("unconfigured server advertised a repository")
	}
}

type workerCompositionStore struct{ domain.AnalysisJobStore }
type workerCompositionIntelligence struct{ domain.Intelligence }

func (*workerCompositionIntelligence) Available() bool { return true }

func TestWorkerCompositionRespectsDisabledIndexing(t *testing.T) {
	admission := application.NewAnalysisAdmission()
	store, model := &workerCompositionStore{}, &workerCompositionIntelligence{}
	worker, err := analysisWorker(store, model, "repo", config.Config{}, admission)
	if err != nil {
		t.Fatal(err)
	}
	defer worker.Close()
	if worker.Available() {
		t.Fatal("disabled indexing admitted a background model")
	}
	enabled, err := analysisWorker(store, model, "repo", config.Config{AIIndexEnabled: true}, admission)
	if err != nil {
		t.Fatal(err)
	}
	defer enabled.Close()
	if !enabled.Available() {
		t.Fatal("enabled indexing lost the repository model")
	}
}
