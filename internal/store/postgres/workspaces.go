package postgres

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"

	"developa/internal/domain"
	"github.com/jackc/pgx/v5"
)

func (s *Store) SavedWorkspaces(ctx context.Context, excluded []string) (records []domain.WorkspaceRegistration, err error) {
	ctx, done := operation(ctx, "postgres.saved_workspaces")
	defer func() { done(err) }()
	var payload []byte
	err = s.pool.QueryRow(ctx, `SELECT COALESCE(jsonb_agg(jsonb_build_object(
		'id',r.id,'name',r.name,'root',w.root,'snapshot',s.metadata) ORDER BY w.added_at,w.repository_id),'[]'::jsonb)
		FROM developa_workspaces w JOIN developa_repositories r ON r.id=w.repository_id
		LEFT JOIN developa_snapshots s ON s.repository_id=r.id AND s.id=r.latest_snapshot_id
		WHERE NOT (w.repository_id=ANY(COALESCE($1::text[],'{}'::text[])))`, excluded).Scan(&payload)
	if err != nil {
		return nil, databaseError(err)
	}
	err = decodeJSON(payload, &records)
	return records, err
}

func (s *Store) SaveWorkspaces(ctx context.Context, records []domain.WorkspaceRegistration, execution domain.Execution) (err error) {
	ctx, done := operation(ctx, "postgres.save_workspaces")
	defer func() { done(err) }()
	if !validWorkspaceRecords(records) || !validExecution(execution, "completed") {
		return domain.ErrInvalidInput
	}
	if len(records) == 0 {
		return nil
	}
	payload, err := json.Marshal(records)
	if err != nil {
		return domain.ErrInvalidInput
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return databaseError(err)
	}
	defer rollback(tx)
	if err := saveWorkspaceRecords(ctx, tx, payload, execution); err != nil {
		return err
	}
	return databaseError(tx.Commit(ctx))
}

func validWorkspaceRecords(records []domain.WorkspaceRegistration) bool {
	if len(records) > 32 {
		return false
	}
	for _, record := range records {
		if record.ID == "" || !filepath.IsAbs(record.Root) || len(record.Root) > 4096 || strings.ContainsRune(record.Root, 0) {
			return false
		}
	}
	return true
}

func saveWorkspaceRecords(ctx context.Context, tx pgx.Tx, payload []byte, execution domain.Execution) error {
	// Capacity and durable registration share a lock across concurrent API instances.
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(84679927940322)`); err != nil {
		return databaseError(err)
	}
	var count int
	err := tx.QueryRow(ctx, `SELECT count(*) FROM (
		SELECT repository_id FROM developa_workspaces UNION SELECT id FROM jsonb_to_recordset($1::jsonb) AS x(id text)
	) all_workspaces`, payload).Scan(&count)
	if err != nil {
		return databaseError(err)
	}
	if count > 32 {
		return domain.ErrWorkspaceLimit
	}
	_, err = tx.Exec(ctx, `INSERT INTO developa_workspaces(repository_id,root)
		SELECT id,root FROM jsonb_to_recordset($1::jsonb) AS x(id text,root text)
		ON CONFLICT (repository_id) DO UPDATE SET root=EXCLUDED.root`, payload)
	if err != nil {
		return databaseError(err)
	}
	return auditWorkspaceRegistration(ctx, tx, payload, execution)
}

func auditWorkspaceRegistration(ctx context.Context, tx pgx.Tx, payload []byte, execution domain.Execution) error {
	_, err := tx.Exec(ctx, `WITH events AS (
		INSERT INTO developa_audit_events(repository_id,execution_id,actor,trigger,trace_id,outcome)
		SELECT id,$2,$3,$4,$5,'completed' FROM jsonb_to_recordset($1::jsonb) AS x(id text)
		RETURNING id
	) INSERT INTO developa_audit_outbox(event_id) SELECT id FROM events`, payload, execution.ID, execution.Actor, execution.Trigger, execution.TraceID)
	return databaseError(err)
}
