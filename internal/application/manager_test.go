package application

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"developa/internal/domain"
	source "developa/internal/source/git"
)

func fixtureManager(t *testing.T, root string, store *managerStore, interval time.Duration) *Manager {
	t.Helper()
	manager, err := NewManager(context.Background(), store, ManagerConfig{RepositoryPath: root, RepositoryName: "Fixture", PollInterval: interval, ScanTimeout: 10 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	manager.Start(context.Background())
	t.Cleanup(manager.Close)
	return manager
}

func awaitManager(t *testing.T, manager *Manager, predicate func(domain.Project) bool) domain.Project {
	t.Helper()
	deadline := time.NewTimer(15 * time.Second)
	defer deadline.Stop()
	ticker := time.NewTicker(20 * time.Millisecond)
	defer ticker.Stop()
	for {
		project, err := manager.Project(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		if predicate(project) {
			return project
		}
		select {
		case <-deadline.C:
			t.Fatalf("manager condition timed out: %+v", project)
		case <-ticker.C:
		}
	}
}

func awaitReady(t *testing.T, manager *Manager, id string) domain.Project {
	t.Helper()
	return awaitManager(t, manager, func(project domain.Project) bool {
		return project.Status == "ready" && project.Snapshot != nil && project.Snapshot.ID == id
	})
}

func requestManagerScan(t *testing.T, manager *Manager) domain.Execution {
	t.Helper()
	execution, err := manager.RequestScan(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return execution
}

func TestManagerUnconfiguredLifecycle(t *testing.T) {
	store := newManagerStore()
	manager := fixtureManager(t, "", store, time.Second)
	project, err := manager.Project(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if project.Configured || project.Watching || project.Status != "unconfigured" {
		t.Fatalf("unexpected project: %+v", project)
	}
	if _, err := manager.RequestScan(context.Background()); !errors.Is(err, domain.ErrNotConfigured) {
		t.Fatalf("request: %v", err)
	}
	manager.Close()
	manager.Close()
}

func TestManagerStartupPersistsUnknownChangeBaseline(t *testing.T) {
	store := newManagerStore()
	root := fixtureRepository(t, "package fixture\nfunc First() {}\n")
	manager := fixtureManager(t, root, store, time.Hour)
	project := awaitReady(t, manager, "1")
	report := store.report(t, 0)
	if report.ChangesKnown || len(report.Changes) != 0 {
		t.Fatal("startup invented an earlier manifest")
	}
	if !project.Watching || project.Repository.Name != "Fixture" {
		t.Fatal("project state missing")
	}
	if strings.Contains(project.Repository.ID, root) || len(project.Repository.ID) != 64 {
		t.Fatal("repository ID exposes root")
	}
	reportSymbol(t, report, "First")
}

func TestManagerLiveEditsAndDeletion(t *testing.T) {
	store := newManagerStore()
	root := fixtureRepository(t, "package fixture\nfunc First() {}\n")
	manager := fixtureManager(t, root, store, 40*time.Millisecond)
	awaitManager(t, manager, func(p domain.Project) bool { return p.Snapshot != nil })
	writeFixture(t, root, "extra.go", "package fixture\nfunc Extra() {}\n")
	awaitManager(t, manager, func(p domain.Project) bool { return p.Snapshot != nil && p.Snapshot.ID == "2" })
	assertReportChange(t, store.report(t, 1), "extra.go", source.Added)
	if err := os.Remove(filepath.Join(root, "extra.go")); err != nil {
		t.Fatal(err)
	}
	awaitManager(t, manager, func(p domain.Project) bool { return p.Snapshot != nil && p.Snapshot.ID == "3" })
	assertReportChange(t, store.report(t, 2), "extra.go", source.Deleted)
}

func assertReportChange(t *testing.T, report Report, path string, kind source.ChangeKind) {
	t.Helper()
	if !report.ChangesKnown {
		t.Fatal("comparison baseline was lost")
	}
	for _, change := range report.Changes {
		if change.Path == path && change.Kind == kind {
			return
		}
	}
	t.Fatalf("missing %s change for %q", kind, path)
}

func TestManagerFailedSavePreservesSnapshotAndBaseline(t *testing.T) {
	store := newManagerStore()
	root := fixtureRepository(t, "package fixture\nfunc First() {}\n")
	manager := fixtureManager(t, root, store, time.Hour)
	first := awaitReady(t, manager, "1")
	store.setFailure(true, false)
	writeFixture(t, root, "fixture.go", "package fixture\nfunc Second() {}\n")
	execution := requestManagerScan(t, manager)
	failed := awaitManager(t, manager, func(p domain.Project) bool { return p.Status == "error" })
	if failed.Snapshot.ID != first.Snapshot.ID || strings.Contains(failed.LastError, "PRIVATE") {
		t.Fatal("failed save lost previous snapshot or exposed error")
	}
	if !store.hasOutcome(execution.ID, "error") {
		t.Fatal("failure audit missing")
	}
	store.setFailure(false, false)
	requestManagerScan(t, manager)
	awaitReady(t, manager, "2")
	assertReportChange(t, store.report(t, 1), "fixture.go", source.Modified)
}

func TestManagerPartialSyntaxIsPublishable(t *testing.T) {
	store := newManagerStore()
	root := fixtureRepository(t, "package fixture\nfunc Broken(\n")
	manager := fixtureManager(t, root, store, time.Hour)
	project := awaitReady(t, manager, "1")
	if project.Snapshot.Completeness != "partial" {
		t.Fatal("partial syntax was concealed")
	}
}

func TestManagerUnchangedManualScanDoesNotRepublish(t *testing.T) {
	store := newManagerStore()
	manager := fixtureManager(t, fixtureRepository(t, "package fixture\n"), store, time.Hour)
	awaitReady(t, manager, "1")
	execution := requestManagerScan(t, manager)
	awaitReady(t, manager, "1")
	if store.count() != 1 || !store.hasOutcome(execution.ID, "completed") {
		t.Fatal("unchanged scan republished or lost audit")
	}
}
