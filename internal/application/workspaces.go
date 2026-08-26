package application

import (
	"context"
	"errors"
	"path/filepath"
	"slices"
	"sync"
)

// Workspaces owns independent trackers for a bounded operator-selected repository set.
type Workspaces struct {
	managers  []*Manager
	fallback  *Manager
	mu        sync.Mutex
	started   bool
	closed    bool
	cancel    context.CancelFunc
	lifecycle context.Context
	store     CatalogStore
}

func NewWorkspaces(ctx context.Context, store CatalogStore, configs []ManagerConfig) (*Workspaces, error) {
	if err := validateWorkspacePaths(configs); err != nil {
		return nil, err
	}
	group := &Workspaces{store: store}
	if err := group.initialize(ctx, store, configs); err != nil {
		group.Close()
		return nil, err
	}
	return group, nil
}

func validateWorkspacePaths(configs []ManagerConfig) error {
	if len(configs) > 32 {
		return errors.New("at most 32 repositories may be configured")
	}
	seen := map[string]bool{}
	for _, cfg := range configs {
		path, err := canonicalWorkspacePath(cfg.RepositoryPath)
		if err != nil {
			return err
		}
		if seen[path] {
			return errors.New("duplicate configured repository")
		}
		seen[path] = true
	}
	return nil
}

func canonicalWorkspacePath(path string) (string, error) {
	if path == "" {
		return "", errors.New("configured repository path is required")
	}
	absolute, err := filepath.Abs(path)
	if err != nil {
		return "", errors.New("configured repository is unavailable")
	}
	canonical, err := filepath.EvalSymlinks(absolute)
	if err != nil {
		return "", errors.New("configured repository is unavailable")
	}
	return canonical, nil
}

func (g *Workspaces) initialize(ctx context.Context, store CatalogStore, configs []ManagerConfig) error {
	ids := map[string]bool{}
	for _, cfg := range configs {
		manager, err := NewManager(ctx, store, cfg)
		if err != nil {
			return err
		}
		g.managers = append(g.managers, manager)
		if ids[manager.Repository().ID] {
			return errors.New("duplicate configured repository")
		}
		ids[manager.Repository().ID] = true
	}
	if len(g.managers) == 0 {
		manager, err := NewManager(ctx, store, ManagerConfig{})
		g.fallback = manager
		return err
	}
	g.fallback = g.managers[0]
	return nil
}

func (g *Workspaces) Managers() []*Manager {
	g.mu.Lock()
	defer g.mu.Unlock()
	return slices.Clone(g.managers)
}
func (g *Workspaces) Default() *Manager { g.mu.Lock(); defer g.mu.Unlock(); return g.fallback }

func (g *Workspaces) Start(ctx context.Context) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.started || g.closed {
		return
	}
	runCtx, cancel := context.WithCancel(ctx)
	g.cancel, g.started = cancel, true
	g.lifecycle = runCtx
	for _, manager := range g.managers {
		manager.Start(runCtx)
	}
}

func (g *Workspaces) Close() {
	g.mu.Lock()
	g.closed = true
	if g.cancel != nil {
		g.cancel()
	}
	managers, fallback := slices.Clone(g.managers), g.fallback
	g.mu.Unlock()
	// Cancel every tracker before waiting, so one blocked scan cannot delay cancellation of others.
	for _, manager := range managers {
		manager.Close()
	}
	if len(managers) == 0 && fallback != nil {
		fallback.Close()
	}
}
