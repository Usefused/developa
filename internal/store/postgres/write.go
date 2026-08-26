package postgres

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"path"
	"time"

	"developa/internal/application"
	"developa/internal/domain"
	"github.com/jackc/pgx/v5"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

func (s *Store) EnsureRepository(ctx context.Context, repository domain.Repository) (err error) {
	ctx, done := operation(ctx, "postgres.ensure_repository")
	defer func() { done(err) }()
	if repository.ID == "" || repository.Name == "" {
		return ErrInvalidInput
	}
	_, err = s.pool.Exec(ctx, `INSERT INTO developa_repositories (id,name) VALUES ($1,$2)
		ON CONFLICT (id) DO UPDATE SET name=EXCLUDED.name`, repository.ID, repository.Name)
	return databaseError(err)
}

func (s *Store) SaveSnapshot(ctx context.Context, repositoryID string, report application.Report, execution domain.Execution) (snapshot domain.Snapshot, err error) {
	ctx, done := operation(ctx, "postgres.save_snapshot")
	defer func() { done(err) }()
	if report.Snapshot.Fingerprint == "" || !validExecution(execution, "completed") {
		return snapshot, ErrInvalidInput
	}
	traceExecution(ctx, repositoryID, execution)
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return snapshot, databaseError(err)
	}
	defer rollback(tx)
	if err := lockRepository(ctx, tx, repositoryID); err != nil {
		return snapshot, databaseError(err)
	}
	snapshot, err = saveImmutableSnapshot(ctx, tx, repositoryID, report, publicationID(report.Snapshot.Fingerprint, execution.ID))
	if err != nil {
		return snapshot, databaseError(err)
	}
	if err := s.publishSnapshot(ctx, tx, repositoryID, snapshot, execution); err != nil {
		return domain.Snapshot{}, databaseError(err)
	}
	err = databaseError(tx.Commit(ctx))
	if err == nil {
		trace.SpanFromContext(ctx).AddEvent("snapshot.published", trace.WithAttributes(
			attribute.String("snapshot.id", snapshot.ID), attribute.Int("file.count", snapshot.FileCount),
			attribute.Int("symbol.count", snapshot.SymbolCount), attribute.Int("change.count", snapshot.ChangeCount)))
	}
	return snapshot, err
}

func (s *Store) publishSnapshot(ctx context.Context, tx pgx.Tx, repositoryID string, snapshot domain.Snapshot, execution domain.Execution) error {
	if err := publishSnapshot(ctx, tx, repositoryID, snapshot, execution); err != nil {
		return err
	}
	if !s.analysisEnabled {
		return nil
	}
	execution.Trigger = "feature_auto"
	_, err := enqueueAnalysis(ctx, tx, repositoryID, snapshot.ID, execution)
	return err
}

func lockRepository(ctx context.Context, tx pgx.Tx, repositoryID string) error {
	var id string
	// Pointer updates never change the primary key. A weaker writer lock permits
	// concurrent job/audit foreign-key checks without reversing the lock order.
	return tx.QueryRow(ctx, `SELECT id FROM developa_repositories WHERE id=$1 FOR NO KEY UPDATE`, repositoryID).Scan(&id)
}

func saveImmutableSnapshot(ctx context.Context, tx pgx.Tx, repositoryID string, report application.Report, snapshotID string) (domain.Snapshot, error) {
	var payload []byte
	err := tx.QueryRow(ctx, `SELECT metadata FROM developa_snapshots WHERE repository_id=$1 AND id=$2`, repositoryID, snapshotID).Scan(&payload)
	if err == nil {
		var snapshot domain.Snapshot
		err := json.Unmarshal(payload, &snapshot)
		return snapshot, err
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return domain.Snapshot{}, err
	}
	snapshot := snapshotMetadata(report, snapshotID)
	if err := insertSnapshot(ctx, tx, repositoryID, snapshot, report); err != nil {
		return domain.Snapshot{}, err
	}
	if err := copyCatalog(ctx, tx, repositoryID, snapshot.ID, report.Index.Files); err != nil {
		return domain.Snapshot{}, err
	}
	if err := copyCalls(ctx, tx, repositoryID, snapshot.ID, report.Index.Calls); err != nil {
		return domain.Snapshot{}, err
	}
	if err := copyImplementations(ctx, tx, repositoryID, snapshot.ID, report.Index.Implementations); err != nil {
		return domain.Snapshot{}, err
	}
	return snapshot, nil
}

func snapshotMetadata(report application.Report, snapshotID string) domain.Snapshot {
	packages := make(map[string]struct{})
	symbols := 0
	for _, file := range report.Index.Files {
		packages[path.Dir(file.Path)+"\x00"+file.Package] = struct{}{}
		symbols += len(file.Symbols)
	}
	return domain.Snapshot{
		ID: snapshotID, Fingerprint: report.Snapshot.Fingerprint, IndexVersion: domain.IndexVersion,
		Commit: report.Snapshot.Commit, Branch: report.Snapshot.Branch, Dirty: report.Snapshot.Dirty,
		SourceComplete: report.Snapshot.Complete, Analysis: report.Index.Analysis,
		Completeness: string(report.Index.Completeness), FileCount: len(report.Index.Files),
		SymbolCount: symbols, PackageCount: len(packages), DiagnosticCount: len(report.Index.Diagnostics),
		ExclusionCount: len(report.Snapshot.Exclusions), ChangeCount: len(report.Changes),
		ChangesKnown: report.ChangesKnown,
		Tags:         nonNil(report.Snapshot.Tags), IndexedAt: time.Now().UTC(),
	}
}

func insertSnapshot(ctx context.Context, tx pgx.Tx, repositoryID string, snapshot domain.Snapshot, report application.Report) error {
	metadata, err := json.Marshal(snapshot)
	if err != nil {
		return err
	}
	details, err := json.Marshal(map[string]any{
		"limitations": nonNil(report.Index.Limitations), "diagnostics": nonNil(report.Index.Diagnostics),
		"exclusions": nonNil(report.Snapshot.Exclusions), "changes": nonNil(report.Changes), "skipped": nonNil(report.Index.Skipped),
		"call_analysis":           report.Index.CallAnalysis,
		"implementation_analysis": report.Index.ImplementationAnalysis,
	})
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `INSERT INTO developa_snapshots
		(repository_id,id,fingerprint,metadata,details,indexed_at) VALUES ($1,$2,$3,$4,$5,$6)`,
		repositoryID, snapshot.ID, snapshot.Fingerprint, metadata, details, snapshot.IndexedAt)
	return err
}

func publishSnapshot(ctx context.Context, tx pgx.Tx, repositoryID string, snapshot domain.Snapshot, execution domain.Execution) error {
	// Only a retry of the same execution reuses publication metadata; later visits
	// to the same source fingerprint retain their own change evidence and timestamp.
	if _, err := tx.Exec(ctx, `UPDATE developa_repositories SET latest_snapshot_id=$2 WHERE id=$1`, repositoryID, snapshot.ID); err != nil {
		return err
	}
	counts := map[string]int{"files": snapshot.FileCount, "symbols": snapshot.SymbolCount, "changes": snapshot.ChangeCount}
	return appendAudit(ctx, tx, repositoryID, &snapshot.ID, execution, "completed", counts)
}

func publicationID(fingerprint, executionID string) string {
	hash := sha256.Sum256([]byte(fingerprint + "\x00" + executionID))
	return hex.EncodeToString(hash[:])
}

func nonNil[T any](items []T) []T {
	if items == nil {
		return []T{}
	}
	return items
}
