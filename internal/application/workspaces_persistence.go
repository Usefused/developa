package application

import (
	"context"
	"errors"

	"developa/internal/domain"
	source "developa/internal/source/git"
)

func NewPersistentWorkspaces(ctx context.Context, store CatalogStore, configs []ManagerConfig, defaults ManagerConfig) (*Workspaces, error) {
	registry, ok := store.(domain.WorkspaceStore)
	if !ok {
		return nil, errors.New("workspace persistence is required")
	}
	group, err := NewWorkspaces(ctx, store, configs)
	if err != nil {
		return nil, err
	}
	if err := group.restore(ctx, registry, defaults); err != nil {
		group.Close()
		return nil, err
	}
	return group, nil
}

func (g *Workspaces) restore(ctx context.Context, store domain.WorkspaceStore, defaults ManagerConfig) error {
	registrations, ids := []domain.WorkspaceRegistration{}, []string{}
	for _, manager := range g.managers {
		registrations = append(registrations, domain.WorkspaceRegistration{Repository: manager.Repository(), Root: manager.source.Root()})
		ids = append(ids, manager.Repository().ID)
	}
	execution := newScanRequest(ctx, "system", "workspace.configure").execution
	if err := store.SaveWorkspaces(ctx, registrations, execution); err != nil {
		return err
	}
	records, err := store.SavedWorkspaces(ctx, ids)
	if err != nil {
		return err
	}
	for _, record := range records {
		g.managers = append(g.managers, restoredManager(ctx, g.store, record, defaults))
	}
	if len(g.managers) > 0 {
		g.fallback = g.managers[0]
	}
	return nil
}

func restoredManager(ctx context.Context, store CatalogStore, record domain.WorkspaceRegistration, defaults ManagerConfig) *Manager {
	defaults.RepositoryPath = ""
	manager, _ := NewManager(ctx, store, defaults)
	manager.cfg.RepositoryPath, manager.cfg.RepositoryName = record.Root, record.Name
	manager.repository, manager.latest = record.Repository, record.Snapshot
	repository, err := source.Open(ctx, record.Root, source.Options{
		MaxFileBytes: defaults.MaxFileBytes, MaxTotalBytes: defaults.MaxTotalBytes,
	})
	if err != nil || repository.Root() != record.Root {
		manager.status, manager.lastError = "error", "Workspace folder is unavailable or is no longer a Git repository. Saved snapshots remain available."
		return manager
	}
	manager.source, manager.status = repository, "ready"
	return manager
}

func (g *Workspaces) Find(id string) *Manager {
	g.mu.Lock()
	defer g.mu.Unlock()
	for _, manager := range g.managers {
		if manager.Repository().ID == id {
			return manager
		}
	}
	return nil
}

func (g *Workspaces) Add(ctx context.Context, cfg ManagerConfig) (*Manager, bool, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.closed {
		return nil, false, context.Canceled
	}
	if manager := g.existingPath(cfg.RepositoryPath); manager != nil {
		return manager, true, nil
	}
	if len(g.managers) >= 32 {
		return nil, false, domain.ErrWorkspaceLimit
	}
	registry, ok := g.store.(domain.WorkspaceStore)
	if !ok {
		return nil, false, domain.ErrNotConfigured
	}
	manager, err := NewManager(ctx, g.store, cfg)
	if err != nil {
		return nil, false, err
	}
	if err := persistAddedWorkspace(ctx, registry, manager, cfg.RepositoryPath); err != nil {
		manager.Close()
		return nil, false, err
	}
	g.managers = append(g.managers, manager)
	if len(g.managers) == 1 {
		g.fallback = manager
	}
	if g.started {
		manager.Start(g.lifecycle)
	}
	return manager, false, nil
}

// The caller holds g.mu, so concurrent additions cannot create duplicate trackers.
func (g *Workspaces) existingPath(path string) *Manager {
	for _, manager := range g.managers {
		if manager.cfg.RepositoryPath == path {
			return manager
		}
	}
	return nil
}

func persistAddedWorkspace(ctx context.Context, store domain.WorkspaceStore, manager *Manager, expectedRoot string) error {
	// Revalidate the actual Git root before publication in case the selected path changed during discovery.
	if manager.source == nil || manager.source.Root() != expectedRoot {
		return domain.ErrFolderForbidden
	}
	execution := newScanRequest(ctx, "operator", "workspace.add").execution
	return store.SaveWorkspaces(ctx, []domain.WorkspaceRegistration{{Repository: manager.Repository(), Root: manager.source.Root()}}, execution)
}
