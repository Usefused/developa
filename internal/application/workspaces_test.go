package application

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"developa/internal/domain"
)

type workspaceStore struct {
	domain.CatalogReader
	mu     sync.Mutex
	stores map[string]*managerStore
	gate   <-chan struct{}
}

func (s *workspaceStore) EnsureRepository(ctx context.Context, repo domain.Repository) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	store := newManagerStore()
	store.gate = s.gate
	s.stores[repo.ID] = store
	return store.EnsureRepository(ctx, repo)
}

func (s *workspaceStore) forRepository(id string) *managerStore {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.stores[id]
}

func (s *workspaceStore) Latest(ctx context.Context, id string) (domain.Snapshot, error) {
	return s.forRepository(id).Latest(ctx, id)
}

func (s *workspaceStore) SaveSnapshot(ctx context.Context, id string, report Report, execution domain.Execution) (domain.Snapshot, error) {
	return s.forRepository(id).SaveSnapshot(ctx, id, report, execution)
}

func (s *workspaceStore) RecordExecution(ctx context.Context, id string, execution domain.Execution, outcome string) error {
	return s.forRepository(id).RecordExecution(ctx, id, execution, outcome)
}

func workspaceFixture(t *testing.T, gate <-chan struct{}) (*Workspaces, *workspaceStore, []string) {
	t.Helper()
	roots := []string{fixtureRepository(t, "package first\nfunc First() {}\n"), fixtureRepository(t, "package second\nfunc Second() {}\n")}
	store := &workspaceStore{stores: map[string]*managerStore{}, gate: gate}
	configs := []ManagerConfig{{RepositoryPath: roots[0], RepositoryName: "First", PollInterval: time.Hour},
		{RepositoryPath: roots[1], RepositoryName: "Second", PollInterval: time.Hour}}
	group, err := NewWorkspaces(context.Background(), store, configs)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(group.Close)
	return group, store, roots
}

func TestWorkspacesKeepTrackersSnapshotsAndManualScansIndependent(t *testing.T) {
	group, store, roots := workspaceFixture(t, nil)
	group.Start(context.Background())
	managers := group.Managers()
	first, second := awaitReady(t, managers[0], "1"), awaitReady(t, managers[1], "1")
	if group.Default() != managers[0] || first.Repository.ID == second.Repository.ID {
		t.Fatal("configured default order or independent repository identity was lost")
	}
	writeFixture(t, roots[0], "extra.go", "package first\nfunc Extra() {}\n")
	requestManagerScan(t, managers[0])
	awaitReady(t, managers[0], "2")
	awaitReady(t, managers[1], "1")
	reportSymbol(t, store.forRepository(first.Repository.ID).report(t, 1), "Extra")
	reportSymbol(t, store.forRepository(second.Repository.ID).report(t, 0), "Second")
	managers[0] = nil
	if group.Managers()[0] == nil {
		t.Fatal("caller mutated group ownership through a returned slice")
	}
}

func TestWorkspacesRejectCanonicalAliasesBeforeRegistration(t *testing.T) {
	root := fixtureRepository(t, "package fixture\n")
	alias := filepath.Join(t.TempDir(), "alias")
	if err := os.Symlink(root, alias); err != nil {
		t.Fatal(err)
	}
	store := &workspaceStore{stores: map[string]*managerStore{}}
	_, err := NewWorkspaces(context.Background(), store, []ManagerConfig{{RepositoryPath: root}, {RepositoryPath: alias}})
	if err == nil || len(store.stores) != 0 || strings.Contains(err.Error(), root) {
		t.Fatal("canonical duplicate was registered or exposed its path")
	}
}

func TestWorkspacesCancelAllActiveScansBeforeWaitingForClose(t *testing.T) {
	group, store, _ := workspaceFixture(t, make(chan struct{}))
	group.Start(context.Background())
	for _, manager := range group.Managers() {
		select {
		case <-store.forRepository(manager.Repository().ID).entered:
		case <-time.After(15 * time.Second):
			t.Fatal("tracker did not enter blocked persistence")
		}
	}
	closed := make(chan struct{})
	go func() { group.Close(); close(closed) }()
	select {
	case <-closed:
	case <-time.After(2 * time.Second):
		t.Fatal("workspace shutdown did not cancel every active scan")
	}
}

func TestWorkspacesEmptyAndConcurrentLifecycleRemainSafe(t *testing.T) {
	group, err := NewWorkspaces(context.Background(), newManagerStore(), nil)
	if err != nil {
		t.Fatal(err)
	}
	var workers sync.WaitGroup
	for range 8 {
		workers.Add(1)
		go func() { defer workers.Done(); group.Start(context.Background()); group.Close() }()
	}
	workers.Wait()
	project, err := group.Default().Project(context.Background())
	if err != nil || project.Configured || len(group.Managers()) != 0 {
		t.Fatal("empty workspace did not retain an unconfigured default")
	}
	if _, err := group.Default().RequestScan(context.Background()); !errors.Is(err, domain.ErrNotConfigured) {
		t.Fatal("unconfigured workspace accepted a source scan")
	}
}

func TestWorkspacesRejectUnavailableRootsWithoutStartingEarlierManagers(t *testing.T) {
	root := fixtureRepository(t, "package fixture\n")
	store := &workspaceStore{stores: map[string]*managerStore{}}
	_, err := NewWorkspaces(context.Background(), store, []ManagerConfig{{RepositoryPath: root}, {RepositoryPath: filepath.Join(root, "private-missing")}})
	if err == nil || len(store.stores) != 0 || strings.Contains(err.Error(), "private-missing") {
		t.Fatal("invalid group partially started or exposed a filesystem path")
	}
}
