package postgres

import (
	"context"
	"errors"
	"time"

	"developa/internal/domain"
	"github.com/jackc/pgx/v5"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

func (s *Store) UpdateAnalysis(ctx context.Context, repositoryID, jobID, leaseToken string, update domain.AnalysisJobUpdate) (err error) {
	ctx, done := operation(ctx, "postgres.update_analysis")
	defer func() { done(err) }()
	if !hexID.MatchString(jobID) || !auditToken.MatchString(leaseToken) || !validAnalysisUpdate(update) {
		return domain.ErrInvalidInput
	}
	if update.AvailableAt.IsZero() {
		update.AvailableAt = time.Now().UTC()
	}
	span := trace.SpanFromContext(ctx)
	span.SetAttributes(attribute.String("job.id", jobID), attribute.String("repository.id", repositoryID))
	var status string
	err = s.pool.QueryRow(ctx, updateAnalysisSQL, repositoryID, jobID, leaseToken, update.Status,
		update.ErrorCode, update.AvailableAt, update.Failed, update.Progress,
		update.AnalyzedSymbols, update.TotalSymbols, update.FeatureCount).Scan(&status)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.ErrLeaseLost
	}
	if err == nil {
		span.AddEvent("analysis."+status, trace.WithAttributes(attribute.Bool("analysis.progress", update.Progress)))
	}
	return databaseError(err)
}

func validAnalysisUpdate(update domain.AnalysisJobUpdate) bool {
	if !validAnalysisStatus(update.Status) || !validAnalysisErrorCode(update.ErrorCode) {
		return false
	}
	if update.Failed && update.Progress {
		return false
	}
	return update.AnalyzedSymbols >= 0 && update.TotalSymbols >= update.AnalyzedSymbols && update.FeatureCount >= 0
}

func validAnalysisStatus(status string) bool {
	switch status {
	case "queued", "completed", "failed", "superseded":
		return true
	default:
		return false
	}
}

func validAnalysisErrorCode(code string) bool {
	switch code {
	case "", "invalid_model_output", "model_unavailable", "model_timeout", "invalid_analysis_input",
		"analysis_busy", "no_progress", "analysis_failed":
		return true
	default:
		return false
	}
}

const updateAnalysisSQL = `WITH owned AS (
	SELECT j.repository_id,j.snapshot_id,s.latest_feature_run_id,l.metadata->>'commit' AS latest_commit
	FROM developa_analysis_jobs j JOIN developa_snapshots s ON s.repository_id=j.repository_id AND s.id=j.snapshot_id
	JOIN developa_repositories r ON r.id=j.repository_id
	JOIN developa_snapshots l ON l.repository_id=r.id AND l.id=r.latest_snapshot_id
	LEFT JOIN developa_feature_runs f ON f.repository_id=s.repository_id AND f.snapshot_id=s.id AND f.id=s.latest_feature_run_id
	WHERE j.repository_id=$1 AND j.id=$2 AND j.status='running' AND j.lease_token=$3 AND j.lease_until>clock_timestamp()
	AND (NOT $8 OR ((f.metadata->>'analyzed_symbols')::integer=$9 AND (f.metadata->>'total_symbols')::integer=$10
		AND (f.metadata->>'feature_count')::integer=$11)) FOR UPDATE OF j
), changed AS (
	UPDATE developa_analysis_jobs j SET
		status=CASE WHEN $7 AND j.attempts>=2 THEN 'failed'
			WHEN $4='superseded' AND NOT j.automatic THEN 'queued'
			WHEN $4='queued' AND j.automatic AND o.latest_commit IS DISTINCT FROM j.commit_id THEN 'superseded' ELSE $4 END,
		attempts=CASE WHEN $8 THEN 0 WHEN $7 THEN LEAST(3,j.attempts+1) ELSE j.attempts END,
		chunks=j.chunks+CASE WHEN $8 THEN 1 ELSE 0 END,
		base_run_id=CASE WHEN $8 THEN o.latest_feature_run_id ELSE j.base_run_id END,
		analyzed_symbols=CASE WHEN $8 THEN $9 ELSE j.analyzed_symbols END,
		total_symbols=CASE WHEN $8 THEN $10 ELSE j.total_symbols END,
		feature_count=CASE WHEN $8 THEN $11 ELSE j.feature_count END,
		error_code=$5,available_at=$6,lease_token=NULL,lease_until=NULL,updated_at=clock_timestamp()
	FROM owned o WHERE j.repository_id=o.repository_id AND j.snapshot_id=o.snapshot_id RETURNING j.*
)` + jobAuditCTE + `SELECT status FROM changed`
