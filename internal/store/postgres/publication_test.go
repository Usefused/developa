package postgres

import (
	"context"
	"testing"

	"developa/internal/application"
	"developa/internal/domain"
	goparser "developa/internal/indexer/golang"
	source "developa/internal/source/git"
	"github.com/jackc/pgx/v5"
)

func TestIntegrationRevisitedContentRetainsNewTransition(t *testing.T) {
	store, _ := catalogFixture(t)
	first := catalogReport(t, 1, "content-a")
	original := saveExecution(t, store, first, "execution-a")
	second := catalogReport(t, 2, "content-b")
	saveExecution(t, store, second, "execution-b")
	first.ChangesKnown = true
	first.Changes = []source.Change{{Kind: source.Deleted, Path: "file001.go"}}
	revisited := saveExecution(t, store, first, "execution-c")
	assertNewPublication(t, original, revisited)
	assertLatestID(t, store, revisited.ID)
	assertTransitionEvidence(t, store, original, revisited)
	assertTableCount(t, store, "developa_snapshots", 3)
}

func assertNewPublication(t *testing.T, original, revisited domain.Snapshot) {
	t.Helper()
	if original.ID == revisited.ID || original.Fingerprint != revisited.Fingerprint {
		t.Fatal("content identity and publication identity were conflated")
	}
	if !revisited.IndexedAt.After(original.IndexedAt) {
		t.Fatal("new publication did not retain its new timestamp")
	}
	if !revisited.ChangesKnown || revisited.ChangeCount != 1 {
		t.Fatal("revisited content lost its current transition metadata")
	}
}

func assertTransitionEvidence(t *testing.T, store *Store, original, revisited domain.Snapshot) {
	t.Helper()
	current, err := store.Details(context.Background(), "repo", revisited.ID)
	if err != nil || len(current.Changes) != 1 || current.Changes[0].Kind != source.Deleted {
		t.Fatalf("new transition evidence was not persisted: %+v, %v", current, err)
	}
	old, err := store.Details(context.Background(), "repo", original.ID)
	if err != nil || old.Snapshot.ChangesKnown || len(old.Changes) != 0 {
		t.Fatalf("original snapshot was mutated: %+v, %v", old, err)
	}
}

func TestIntegrationSameExecutionRetryKeepsPublicationIdentity(t *testing.T) {
	store, _ := catalogFixture(t)
	report := catalogReport(t, 1, "retry")
	original := saveExecution(t, store, report, "retry-execution")
	report.ChangesKnown = true
	report.Changes = []source.Change{{Kind: source.Added, Path: "not-original.go"}}
	retry := saveExecution(t, store, report, "retry-execution")
	if retry.ID != original.ID || !retry.IndexedAt.Equal(original.IndexedAt) || retry.ChangesKnown {
		t.Fatal("same execution retry changed immutable publication metadata")
	}
	assertTableCount(t, store, "developa_snapshots", 1)
	assertTableCount(t, store, "developa_files", 1)
}

func TestIntegrationPublicationMigrationUpgradesExistingCatalog(t *testing.T) {
	store, _ := unmigratedFixture(t)
	installLegacyCatalog(t, store)
	report := catalogReport(t, 1, "legacy")
	original := saveLegacyReport(t, store, report)
	if err := store.Migrate(context.Background()); err != nil {
		t.Fatal(err)
	}
	revisited := saveExecution(t, store, report, "after-upgrade")
	if revisited.ID == original.ID {
		t.Fatal("upgraded catalog did not permit a new publication")
	}
	if _, err := store.File(context.Background(), "repo", original.ID, "file000.go"); err != nil {
		t.Fatal("migration did not preserve existing data")
	}
	assertTableCount(t, store, "developa_schema_migrations", len(catalogMigrations))
	assertTableCount(t, store, "developa_snapshots", 2)
}

func installLegacyCatalog(t *testing.T, store *Store) {
	t.Helper()
	tx, err := store.pool.Begin(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer rollback(tx)
	_, err = tx.Exec(context.Background(), `CREATE TABLE developa_schema_migrations
		(version integer PRIMARY KEY, applied_at timestamptz NOT NULL DEFAULT now())`)
	if err != nil {
		t.Fatal(err)
	}
	if err := applyMigration(context.Background(), tx, catalogMigrations[0]); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func saveExecution(t *testing.T, store *Store, report application.Report, executionID string) domain.Snapshot {
	t.Helper()
	execution := testExecution()
	execution.ID = executionID
	snapshot, err := store.SaveSnapshot(context.Background(), "repo", report, execution)
	if err != nil {
		t.Fatal(err)
	}
	return snapshot
}

func saveLegacyReport(t *testing.T, store *Store, report application.Report) domain.Snapshot {
	t.Helper()
	if err := store.EnsureRepository(context.Background(), domain.Repository{ID: "repo", Name: "Legacy"}); err != nil {
		t.Fatal(err)
	}
	tx, err := store.pool.Begin(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer rollback(tx)
	snapshot := snapshotMetadata(report, report.Snapshot.Fingerprint)
	snapshot.IndexVersion = ""
	if err := insertSnapshot(context.Background(), tx, "repo", snapshot, report); err != nil {
		t.Fatal(err)
	}
	if err := copyLegacyCatalog(context.Background(), tx, snapshot.ID, report.Index.Files); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(context.Background(), `UPDATE developa_repositories SET latest_snapshot_id=$2 WHERE id=$1`, "repo", snapshot.ID); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(context.Background()); err != nil {
		t.Fatal(err)
	}
	return snapshot
}

func copyLegacyCatalog(ctx context.Context, tx pgx.Tx, snapshotID string, files []goparser.FileBlock) error {
	// Migration fixtures must populate the old schema without teaching production
	// writes to silently omit source or newer relationship evidence.
	columns := []string{"repository_id", "snapshot_id", "path", "package", "overview", "completeness", "symbol_count", "kinds", "doc", "imports"}
	_, err := tx.CopyFrom(ctx, pgx.Identifier{"developa_files"}, columns,
		pgx.CopyFromSlice(len(files), func(i int) ([]any, error) {
			values, err := fileValues("repo", snapshotID, files[i])
			if err != nil {
				return nil, err
			}
			// The initial ten columns predate captured file bytes.
			return values[:len(columns)], nil
		}))
	if err != nil {
		return err
	}
	_, err = tx.CopyFrom(ctx, pgx.Identifier{"developa_symbols"},
		[]string{"repository_id", "snapshot_id", "id", "file_path", "name", "kind", "source_line", "payload"},
		&symbolRows{repositoryID: "repo", snapshotID: snapshotID, files: files})
	return err
}
