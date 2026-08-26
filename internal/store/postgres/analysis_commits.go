package postgres

import (
	"context"
	"errors"

	"developa/internal/domain"
	"github.com/jackc/pgx/v5"
)

func lockAnalysisSnapshot(ctx context.Context, tx pgx.Tx, repositoryID, snapshotID string) (snapshot domain.Snapshot, err error) {
	var payload []byte
	err = tx.QueryRow(ctx, `SELECT metadata FROM developa_snapshots WHERE repository_id=$1 AND id=$2 FOR NO KEY UPDATE`, repositoryID, snapshotID).Scan(&payload)
	if err != nil {
		return snapshot, err
	}
	err = decodeJSON(payload, &snapshot)
	return snapshot, err
}

func previousCommitAdmission(ctx context.Context, tx pgx.Tx, repositoryID string, snapshot domain.Snapshot) (domain.AnalysisJob, bool, error) {
	if snapshot.Commit == "" || snapshot.Dirty || !snapshot.SourceComplete {
		return domain.AnalysisJob{SnapshotID: snapshot.ID, Commit: snapshot.Commit, Status: "not_queued", TotalSymbols: snapshot.SymbolCount}, true, nil
	}
	job, err := scanAnalysisJob(tx.QueryRow(ctx, `SELECT `+analysisJobJSON+`,j.trace_parent,'' FROM developa_analysis_commits c
		JOIN developa_analysis_jobs j ON j.repository_id=c.repository_id AND j.id=c.job_id WHERE c.repository_id=$1 AND c.commit_id=$2`,
		repositoryID, snapshot.Commit))
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.AnalysisJob{}, false, nil
	}
	return job, err == nil, err
}

func recordCommitAdmission(ctx context.Context, tx pgx.Tx, repositoryID, commit, jobID string) error {
	_, err := tx.Exec(ctx, `INSERT INTO developa_analysis_commits(repository_id,commit_id,job_id) VALUES ($1,$2,$3)
		ON CONFLICT (repository_id,commit_id) DO NOTHING`, repositoryID, commit, jobID)
	return err
}
