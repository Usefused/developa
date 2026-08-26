package postgres

import (
	"context"
	"errors"
	"time"

	"developa/internal/domain"
	"github.com/jackc/pgx/v5"
	"go.opentelemetry.io/otel/propagation"
)

var _ domain.AnalysisJobStore = (*Store)(nil)

func (s *Store) EnqueueAnalysis(ctx context.Context, repositoryID, snapshotID string, execution domain.Execution) (job domain.AnalysisJob, err error) {
	ctx, done := operation(ctx, "postgres.enqueue_analysis")
	defer func() { done(err) }()
	if !validExecution(execution, "queued") || !hexID.MatchString(snapshotID) {
		return job, domain.ErrInvalidInput
	}
	traceExecution(ctx, repositoryID, execution)
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return job, databaseError(err)
	}
	defer rollback(tx)
	if err := lockRepository(ctx, tx, repositoryID); err != nil {
		return job, databaseError(err)
	}
	job, err = enqueueAnalysis(ctx, tx, repositoryID, snapshotID, execution)
	if err != nil {
		return job, databaseError(err)
	}
	return job, databaseError(tx.Commit(ctx))
}

func (s *Store) EnsureAnalysis(ctx context.Context, repositoryID string, execution domain.Execution) (err error) {
	ctx, done := operation(ctx, "postgres.ensure_analysis")
	defer func() { done(err) }()
	if !s.analysisEnabled {
		return nil
	}
	if !validExecution(execution, "queued") {
		return domain.ErrInvalidInput
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return databaseError(err)
	}
	defer rollback(tx)
	if err := ensureLatestAnalysis(ctx, tx, repositoryID, execution); err != nil {
		return databaseError(err)
	}
	return databaseError(tx.Commit(ctx))
}

func ensureLatestAnalysis(ctx context.Context, tx pgx.Tx, repositoryID string, execution domain.Execution) error {
	// Match publication's lock order so startup cannot enqueue an obsolete latest
	// pointer while another transaction publishes and supersedes queued work.
	var snapshotID *string
	err := tx.QueryRow(ctx, `SELECT latest_snapshot_id FROM developa_repositories WHERE id=$1 FOR NO KEY UPDATE`, repositoryID).Scan(&snapshotID)
	if err != nil || snapshotID == nil {
		return err
	}
	execution.Trigger = "feature_auto"
	_, err = enqueueAnalysis(ctx, tx, repositoryID, *snapshotID, execution)
	return err
}

func enqueueAnalysis(ctx context.Context, tx pgx.Tx, repositoryID, snapshotID string, execution domain.Execution) (domain.AnalysisJob, error) {
	snapshot, err := lockAnalysisSnapshot(ctx, tx, repositoryID, snapshotID)
	if err != nil {
		return domain.AnalysisJob{}, err
	}
	automatic := execution.Trigger != "feature_manual"
	if automatic {
		job, admitted, err := previousCommitAdmission(ctx, tx, repositoryID, snapshot)
		if admitted || err != nil {
			return job, err
		}
	}
	return admitAnalysis(ctx, tx, repositoryID, snapshot, execution, automatic)
}

func admitAnalysis(ctx context.Context, tx pgx.Tx, repositoryID string, snapshot domain.Snapshot, execution domain.Execution, automatic bool) (domain.AnalysisJob, error) {
	snapshotID := snapshot.ID
	if err := prepareAnalysisAdmission(ctx, tx, repositoryID, snapshotID, automatic); err != nil {
		return domain.AnalysisJob{}, err
	}
	carrier := propagation.MapCarrier{}
	propagation.TraceContext{}.Inject(ctx, carrier)
	row := tx.QueryRow(ctx, enqueueAnalysisSQL, repositoryID, snapshotID,
		publicationID("analysis-job\x00"+repositoryID, snapshotID), automatic,
		execution.ID, execution.Actor, execution.Trigger, execution.TraceID, carrier.Get("traceparent"))
	var changed bool
	job, err := scanEnqueuedJob(row, &changed)
	if err != nil {
		return job, err
	}
	if automatic {
		if err := recordCommitAdmission(ctx, tx, repositoryID, snapshot.Commit, job.ID); err != nil {
			return job, err
		}
	}
	if changed || !automatic {
		execution.JobID = job.ID
		err = appendAudit(ctx, tx, repositoryID, &snapshotID, execution, "queued", map[string]int{"jobs": 1})
	}
	return job, err
}

func prepareAnalysisAdmission(ctx context.Context, tx pgx.Tx, repositoryID, snapshotID string, automatic bool) error {
	if automatic {
		_, err := tx.Exec(ctx, supersedeAnalysisSQL, repositoryID, snapshotID)
		return err
	}
	// An explicit request keeps its historical snapshot eligible even if the
	// deduplicated work began automatically and the source later advances.
	_, err := tx.Exec(ctx, `UPDATE developa_analysis_jobs SET automatic=false,updated_at=clock_timestamp()
		WHERE repository_id=$1 AND snapshot_id=$2 AND automatic AND status IN ('queued','running')`, repositoryID, snapshotID)
	return err
}

func (s *Store) AnalysisStatus(ctx context.Context, repositoryID, snapshotID string) (job domain.AnalysisJob, err error) {
	ctx, done := operation(ctx, "postgres.analysis_status")
	defer func() { done(err) }()
	row := s.pool.QueryRow(ctx, analysisStatusSQL, repositoryID, snapshotID)
	job, err = scanAnalysisJob(row)
	return job, databaseError(err)
}

func scanAnalysisJob(row pgx.Row) (job domain.AnalysisJob, err error) {
	var payload []byte
	if err := row.Scan(&payload, &job.TraceParent, &job.LeaseToken); err != nil {
		return job, err
	}
	err = decodeJSON(payload, &job)
	return job, err
}

func scanEnqueuedJob(row pgx.Row, changed *bool) (job domain.AnalysisJob, err error) {
	var payload []byte
	if err := row.Scan(&payload, &job.TraceParent, changed); err != nil {
		return job, err
	}
	err = decodeJSON(payload, &job)
	return job, err
}

func validateAnalysisLease(ctx context.Context, tx pgx.Tx, repositoryID, snapshotID string, execution domain.Execution) error {
	if execution.JobID == "" && execution.LeaseToken == "" {
		return nil
	}
	if !hexID.MatchString(execution.JobID) || !auditToken.MatchString(execution.LeaseToken) {
		return domain.ErrLeaseLost
	}
	var id string
	err := tx.QueryRow(ctx, `SELECT id FROM developa_analysis_jobs WHERE repository_id=$1 AND snapshot_id=$2
		AND id=$3 AND status='running' AND lease_token=$4 AND lease_until>clock_timestamp() FOR UPDATE`,
		repositoryID, snapshotID, execution.JobID, execution.LeaseToken).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.ErrLeaseLost
	}
	return databaseError(err)
}

func (s *Store) ClaimAnalysis(ctx context.Context, repositoryID, leaseToken string, duration time.Duration) (job domain.AnalysisJob, err error) {
	ctx, done := operation(ctx, "postgres.claim_analysis")
	defer func() { done(err) }()
	if !auditToken.MatchString(leaseToken) || duration <= 0 || duration > 15*time.Minute {
		return job, domain.ErrInvalidInput
	}
	// Expired attempts are failures even if a process died before it could report
	// one. A bounded cleanup prevents crashed jobs from retrying indefinitely.
	if _, err := s.pool.Exec(ctx, expireAnalysisSQL, repositoryID, s.analysisEnabled); err != nil {
		return job, databaseError(err)
	}
	job, err = scanAnalysisJob(s.pool.QueryRow(ctx, claimAnalysisSQL, repositoryID, leaseToken, duration.Seconds(), s.analysisEnabled))
	return job, databaseError(err)
}
