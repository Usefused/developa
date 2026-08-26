package server

import (
	"context"
	"net/http"
	"sync"

	"developa/internal/application"
	"developa/internal/config"
	"developa/internal/domain"
	"developa/internal/store/postgres"
	httptransport "developa/internal/transport/http"
	"developa/internal/webui"
)

type workspaceRuntime struct {
	mu         sync.Mutex
	ctx        context.Context
	cancel     context.CancelFunc
	store      *postgres.Store
	group      *application.Workspaces
	cfg        config.Config
	admission  *application.AnalysisAdmission
	explorers  map[string]*httptransport.Explorer
	workers    []*application.AnalysisWorker
	management bool
}

func managedExplorerServer(ctx context.Context, store *postgres.Store, group *application.Workspaces, cfg config.Config) (*http.Server, func(), error) {
	paths := workspaceRootPaths(cfg)
	service, err := application.NewWorkspaceService(group, managerDefaults(cfg), paths)
	if err != nil {
		return nil, nil, err
	}
	runCtx, cancel := context.WithCancel(ctx)
	runtime := &workspaceRuntime{ctx: runCtx, cancel: cancel, store: store, group: group, cfg: cfg, admission: application.NewAnalysisAdmission(), explorers: map[string]*httptransport.Explorer{}, management: len(paths) > 0}
	first, err := runtime.initialize()
	if err != nil {
		runtime.Close()
		return nil, nil, err
	}
	options := httptransport.Config{Address: cfg.HTTPAddr, ReadinessTimeout: cfg.ReadinessTimeout, RequestTimeout: cfg.RequestTimeout,
		AITimeout: cfg.AITimeout, UI: webui.Handler(), RepositoryCatalog: store, Explorer: first, WorkspaceRuntime: runtime, WorkspaceManagement: service}
	return httptransport.NewServer(store, options), runtime.Close, nil
}

func (r *workspaceRuntime) initialize() (*httptransport.Explorer, error) {
	for _, id := range r.RepositoryIDs() {
		if _, err := r.Explorer(id); err != nil {
			return nil, err
		}
	}
	return r.Explorer("")
}

func (r *workspaceRuntime) Explorer(id string) (*httptransport.Explorer, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := r.ctx.Err(); err != nil {
		return nil, err
	}
	manager := r.group.Default()
	if id != "" {
		manager = r.group.Find(id)
	}
	if manager == nil {
		return nil, domain.ErrNotFound
	}
	id = manager.Repository().ID
	if explorer := r.explorers[id]; explorer != nil {
		return explorer, nil
	}
	explorer, worker, err := repositoryExplorer(r.store, manager, r.cfg, r.admission)
	if err != nil {
		return nil, err
	}
	explorer.WorkspaceManagement = r.management
	r.explorers[id] = explorer
	r.workers = append(r.workers, worker)
	worker.Start(r.ctx)
	return explorer, nil
}

func (r *workspaceRuntime) RepositoryIDs() []string {
	ids := []string{}
	for _, manager := range r.group.Managers() {
		ids = append(ids, manager.Repository().ID)
	}
	return ids
}

func (r *workspaceRuntime) Close() {
	r.mu.Lock()
	r.cancel()
	workers := append([]*application.AnalysisWorker{}, r.workers...)
	r.mu.Unlock()
	closeAnalysisWorkers(workers)
}

func workspaceRootPaths(cfg config.Config) []string {
	if len(cfg.WorkspaceRoots) > 0 {
		return cfg.WorkspaceRoots
	}
	paths := []string{}
	// Existing checkouts are the narrow default; broader browsing needs explicit operator consent.
	for _, manager := range repositoryManagers(cfg) {
		paths = append(paths, manager.RepositoryPath)
	}
	return paths
}

func managerDefaults(cfg config.Config) application.ManagerConfig {
	return application.ManagerConfig{PollInterval: cfg.WatchInterval, ScanTimeout: cfg.ScanTimeout,
		MaxFileBytes: cfg.SourceMaxFileBytes, MaxTotalBytes: cfg.SourceMaxTotalBytes}
}
