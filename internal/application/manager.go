package application

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"path/filepath"
	"sync"
	"time"

	"developa/internal/domain"
	source "developa/internal/source/git"
)

type CatalogStore interface {
	domain.CatalogReader
	EnsureRepository(context.Context, domain.Repository) error
	SaveSnapshot(context.Context, string, Report, domain.Execution) (domain.Snapshot, error)
	RecordExecution(context.Context, string, domain.Execution, string) error
}

type ManagerConfig struct {
	RepositoryPath string
	RepositoryName string
	PollInterval   time.Duration
	ScanTimeout    time.Duration
}

// Manager serializes automatic and requested scans for one operator-owned checkout.
// Only successfully committed snapshots advance the source comparison baseline.
type Manager struct {
	store      CatalogStore
	cfg        ManagerConfig
	repository domain.Repository
	source     *source.Repository
	requests   chan scanRequest
	previous   *source.Snapshot
	mu         sync.Mutex
	latest     *domain.Snapshot
	status     string
	lastError  string
	busy       bool
	started    bool
	closed     bool
	watching   bool
	cancel     context.CancelFunc
	lifecycle  context.Context
	wg         sync.WaitGroup
	requestWG  sync.WaitGroup
}

type scanRequest struct {
	ctx       context.Context
	execution domain.Execution
}

func NewManager(ctx context.Context, store CatalogStore, cfg ManagerConfig) (*Manager, error) {
	if store == nil {
		return nil, errors.New("catalog store is required")
	}
	if cfg.PollInterval <= 0 {
		cfg.PollInterval = 2 * time.Second
	}
	if cfg.ScanTimeout <= 0 {
		cfg.ScanTimeout = 30 * time.Second
	}
	m := &Manager{store: store, cfg: cfg, requests: make(chan scanRequest, 1), status: "unconfigured"}
	if cfg.RepositoryPath == "" {
		return m, nil
	}
	if err := m.initialize(ctx); err != nil {
		return nil, err
	}
	return m, nil
}

func (m *Manager) initialize(ctx context.Context) error {
	repo, err := source.Open(ctx, m.cfg.RepositoryPath, source.Options{})
	if err != nil {
		return errors.New("configured repository is unavailable")
	}
	m.source = repo
	m.cfg.RepositoryPath = repo.Root()
	name := m.cfg.RepositoryName
	if name == "" {
		name = filepath.Base(repo.Root())
	}
	m.repository = domain.Repository{ID: repositoryID(repo.Root()), Name: name}
	if err := m.store.EnsureRepository(ctx, m.repository); err != nil {
		return errors.New("repository registration failed")
	}
	latest, err := m.store.Latest(ctx, m.repository.ID)
	if err != nil && !errors.Is(err, domain.ErrNotFound) {
		return errors.New("repository state is unavailable")
	}
	if err == nil {
		m.latest = &latest
	}
	m.status = "ready"
	return nil
}

func repositoryID(root string) string {
	hash := sha256.Sum256([]byte(root))
	return hex.EncodeToString(hash[:])
}

func (m *Manager) Repository() domain.Repository { return m.repository }

func (m *Manager) Project(ctx context.Context) (domain.Project, error) {
	if err := ctx.Err(); err != nil {
		return domain.Project{}, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	project := domain.Project{Configured: m.source != nil, Repository: m.repository,
		Status: m.status, Watching: m.watching, PollIntervalMS: m.cfg.PollInterval.Milliseconds(), LastError: m.lastError}
	if m.latest != nil {
		latest := *m.latest
		latest.Tags = append([]string(nil), latest.Tags...)
		project.Snapshot = &latest
	}
	return project, nil
}

func (m *Manager) Start(ctx context.Context) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.started || m.closed || m.source == nil {
		return
	}
	runCtx, cancel := context.WithCancel(ctx)
	m.lifecycle = runCtx
	m.cancel, m.started, m.watching, m.busy = cancel, true, true, true
	m.status = "scanning"
	m.wg.Add(1)
	go m.run(runCtx)
}

func (m *Manager) Close() {
	m.mu.Lock()
	m.closed = true
	if m.cancel != nil {
		m.cancel()
	}
	m.mu.Unlock()
	m.wg.Wait()
	m.requestWG.Wait()
}

func (m *Manager) reserve() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.reserveLocked()
}

func (m *Manager) reserveManual() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.reserveLocked(); err != nil {
		return err
	}
	// Admission and shutdown share this lock, so Close cannot miss an in-flight request.
	m.requestWG.Add(1)
	return nil
}

func (m *Manager) reserveLocked() error {
	if m.source == nil {
		return domain.ErrNotConfigured
	}
	if m.closed || !m.started {
		return context.Canceled
	}
	if m.busy {
		return domain.ErrBusy
	}
	m.busy, m.status = true, "scanning"
	return nil
}

func (m *Manager) finish(err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.busy = false
	m.status, m.lastError = "ready", ""
	if err != nil && !errors.Is(err, context.Canceled) {
		m.status, m.lastError = "error", "Repository indexing failed; the previous snapshot remains available."
	}
}
