package application

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"developa/internal/domain"
)

func TestManagerRestartSkipsUnchangedPersistedSnapshot(t *testing.T) {
	store := newManagerStore()
	root := fixtureRepository(t, "package fixture\nfunc First() {}\n")
	first := fixtureManager(t, root, store, time.Hour)
	awaitReady(t, first, "1")
	first.Close()
	second := fixtureManager(t, root, store, time.Hour)
	awaitReady(t, second, "1")
	if store.count() != 1 {
		t.Fatal("unchanged startup reparsed and republished")
	}
	writeFixture(t, root, "fixture.go", "package fixture\nfunc Second() {}\n")
	requestManagerScan(t, second)
	awaitReady(t, second, "2")
	if !store.report(t, 1).ChangesKnown {
		t.Fatal("unchanged startup did not recover in-memory baseline")
	}
}

func TestManagerUpgradeReindexesUnchangedSourceOnce(t *testing.T) {
	store := newManagerStore()
	root := fixtureRepository(t, "package fixture\nfunc First() {}\n")
	first := fixtureManager(t, root, store, time.Hour)
	initial := awaitReady(t, first, "1")
	first.Close()
	store.mu.Lock()
	store.latest.IndexVersion = "1"
	store.mu.Unlock()
	second := fixtureManager(t, root, store, time.Hour)
	upgraded := awaitReady(t, second, "2")
	if upgraded.Snapshot.Fingerprint != initial.Snapshot.Fingerprint || upgraded.Snapshot.IndexVersion != domain.IndexVersion {
		t.Fatal("analysis upgrade did not retain source identity and advance index version")
	}
	requestManagerScan(t, second)
	awaitReady(t, second, "2")
	if store.count() != 2 {
		t.Fatal("upgraded index repeatedly republishes unchanged source")
	}
}

func TestManagerRestartChangedSourceMarksUnknownBaseline(t *testing.T) {
	store := newManagerStore()
	root := fixtureRepository(t, "package fixture\nfunc First() {}\n")
	first := fixtureManager(t, root, store, time.Hour)
	awaitReady(t, first, "1")
	first.Close()
	writeFixture(t, root, "fixture.go", "package fixture\nfunc Second() {}\n")
	second := fixtureManager(t, root, store, time.Hour)
	awaitReady(t, second, "2")
	report := store.report(t, 1)
	if report.ChangesKnown || len(report.Changes) != 0 {
		t.Fatal("restart fabricated source changes")
	}
}

func TestManagerCaptureFailureRetriesWithoutLosingSnapshot(t *testing.T) {
	store := newManagerStore()
	root := fixtureRepository(t, "package fixture\nfunc First() {}\n")
	manager := fixtureManager(t, root, store, time.Hour)
	awaitReady(t, manager, "1")
	away := filepath.Join(filepath.Dir(root), "temporarily-away")
	if err := os.Rename(root, away); err != nil {
		t.Fatal(err)
	}
	requestManagerScan(t, manager)
	project := awaitManager(t, manager, func(p domain.Project) bool { return p.Status == "error" })
	if project.Snapshot.ID != "1" {
		t.Fatal("capture error discarded saved snapshot")
	}
	if err := os.Rename(away, root); err != nil {
		t.Fatal(err)
	}
	requestManagerScan(t, manager)
	awaitReady(t, manager, "1")
}

func TestManagerStartAndCloseAreIdempotent(t *testing.T) {
	store := newManagerStore()
	manager := fixtureManager(t, fixtureRepository(t, "package fixture\n"), store, time.Hour)
	manager.Start(context.Background())
	awaitReady(t, manager, "1")
	manager.Close()
	manager.Close()
	manager.Start(context.Background())
	project, err := manager.Project(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if project.Watching || store.count() != 1 {
		t.Fatal("closed manager restarted")
	}
}
