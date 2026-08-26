package application

import (
	"context"
	"errors"
	"time"

	"developa/internal/domain"
)

var errAnalysisNoProgress = errors.New("analysis made no symbol progress")

func (w *AnalysisWorker) analyze(ctx context.Context, job domain.AnalysisJob) (domain.AnalysisJobUpdate, error) {
	current, err := w.isCurrentJob(ctx, job)
	if err != nil {
		return domain.AnalysisJobUpdate{}, err
	}
	if !current {
		return domain.AnalysisJobUpdate{Status: "superseded"}, nil
	}
	page, err := w.store.Features(ctx, w.cfg.RepositoryID, job.SnapshotID, domain.Filter{Limit: 1})
	if err != nil {
		return domain.AnalysisJobUpdate{}, err
	}
	if analysisAlreadyPublished(job, page.Run) {
		update := w.featureJobUpdate(*page.Run)
		update.Progress = page.Run.ID != job.BaseRunID
		return update, nil
	}
	run, err := w.intelligence.Discover(ctx, job.SnapshotID)
	if err != nil {
		return domain.AnalysisJobUpdate{}, err
	}
	if !analysisAdvanced(run, page.Run) {
		return domain.AnalysisJobUpdate{}, errAnalysisNoProgress
	}
	return w.featureJobUpdate(run), nil
}

func (w *AnalysisWorker) isCurrentJob(ctx context.Context, job domain.AnalysisJob) (bool, error) {
	if !job.Automatic {
		return true, nil
	}
	latest, err := w.store.Latest(ctx, w.cfg.RepositoryID)
	if job.Commit != "" {
		return latest.Commit == job.Commit, err
	}
	return latest.ID == job.SnapshotID, err
}

func analysisAlreadyPublished(job domain.AnalysisJob, run *domain.FeatureRun) bool {
	return run != nil && (run.ID != job.BaseRunID || (job.Automatic && run.AnalyzedSymbols >= run.TotalSymbols))
}

func analysisAdvanced(run domain.FeatureRun, previous *domain.FeatureRun) bool {
	if run.AnalyzedSymbols >= run.TotalSymbols {
		return true
	}
	if run.AnalyzedSymbols <= 0 {
		return false
	}
	if run.ParentRunID != "" && previous != nil {
		return run.AnalyzedSymbols > previous.AnalyzedSymbols
	}
	return true
}

func (w *AnalysisWorker) featureJobUpdate(run domain.FeatureRun) domain.AnalysisJobUpdate {
	status := "queued"
	if run.AnalyzedSymbols >= run.TotalSymbols {
		status = "completed"
	}
	return domain.AnalysisJobUpdate{Status: status, Progress: true, AnalyzedSymbols: run.AnalyzedSymbols, TotalSymbols: run.TotalSymbols,
		FeatureCount: run.FeatureCount, AvailableAt: time.Now().UTC().Add(w.cfg.PollInterval)}
}

func (w *AnalysisWorker) failureUpdate(ctx context.Context, job domain.AnalysisJob, err error) domain.AnalysisJobUpdate {
	update := domain.AnalysisJobUpdate{Status: "queued", AvailableAt: time.Now().UTC().Add(w.cfg.RetryInterval), AnalyzedSymbols: job.AnalyzedSymbols,
		TotalSymbols: job.TotalSymbols, FeatureCount: job.FeatureCount}
	if ctx.Err() != nil {
		return update
	}
	update.ErrorCode = analysisErrorCode(err)
	if errors.Is(err, domain.ErrBusy) {
		return update
	}
	update.Failed = true
	update.AvailableAt = time.Now().UTC().Add(w.cfg.RetryInterval * time.Duration(1<<max(0, min(job.Attempts, 2))))
	if job.Attempts+1 >= 3 {
		update.Status = "failed"
	}
	return update
}

func analysisErrorCode(err error) string {
	switch {
	case errors.Is(err, domain.ErrInvalidModelOutput):
		return "invalid_model_output"
	case errors.Is(err, domain.ErrModelUnavailable):
		return "model_unavailable"
	case errors.Is(err, context.DeadlineExceeded):
		return "model_timeout"
	case errors.Is(err, domain.ErrInvalidInput):
		return "invalid_analysis_input"
	case errors.Is(err, domain.ErrBusy):
		return "analysis_busy"
	case errors.Is(err, errAnalysisNoProgress):
		return "no_progress"
	default:
		return "analysis_failed"
	}
}
