package application

import (
	"context"
	"errors"
	"testing"
	"time"

	"developa/internal/domain"
)

func TestAnalysisQueueIsDurableAdmissionIndependentOfRequestCancellation(t *testing.T) {
	worker, _, intelligence := analysisFixture(t, 1)
	ctx, cancel := context.WithCancel(context.Background())
	first, err := worker.Queue(ctx, "snapshot")
	if err != nil {
		t.Fatal(err)
	}
	cancel()
	second := queuedAnalysis(t, worker)
	if first.ID != second.ID || intelligence.calls.Load() != 0 {
		t.Fatal("admission ran a model or discarded store deduplication")
	}
	processAnalysis(t, worker)
	if job := analysisStatus(t, worker); job.Status != "completed" || intelligence.calls.Load() != 1 {
		t.Fatal("request cancellation canceled accepted work")
	}
}

func TestAnalysisContinuesPersistedCoverageAfterWorkerRestart(t *testing.T) {
	worker, store, intelligence := analysisFixture(t, 3)
	queuedAnalysis(t, worker)
	processAnalysis(t, worker)
	first := analysisStatus(t, worker)
	if first.Status != "queued" || first.AnalyzedSymbols != 1 || first.Chunks != 1 {
		t.Fatal("first chunk not durably checkpointed")
	}
	worker.Close()
	restarted := newAnalysisFixtureWorker(t, store, intelligence)
	for range 2 {
		store.ready()
		processAnalysis(t, restarted)
	}
	final := analysisStatus(t, restarted)
	if final.Status != "completed" || final.AnalyzedSymbols != 3 || final.FeatureCount != 3 || intelligence.calls.Load() != 3 {
		t.Fatal("restart repeated or lost completed batches")
	}
}

func TestAnalysisRecoversFinalPublicationBeforeQueueAcknowledgement(t *testing.T) {
	worker, store, intelligence := analysisFixture(t, 1)
	store.failedWrites = 1
	queuedAnalysis(t, worker)
	if err := worker.processNext(context.Background()); err == nil {
		t.Fatal("fixture failed to interrupt acknowledgement")
	}
	old := analysisStatus(t, worker)
	worker.Close()
	restarted := newAnalysisFixtureWorker(t, store, intelligence)
	store.ready()
	processAnalysis(t, restarted)
	if final := analysisStatus(t, restarted); final.Status != "completed" || intelligence.calls.Load() != 1 {
		t.Fatal("recovery regenerated an already published complete run")
	}
	err := store.UpdateAnalysis(context.Background(), "repository", old.ID, old.LeaseToken, domain.AnalysisJobUpdate{Status: "completed"})
	if !errors.Is(err, domain.ErrLeaseLost) {
		t.Fatal("old owner acknowledged a reclaimed job")
	}
}

func TestAnalysisRecoversUnacknowledgedPartialPublication(t *testing.T) {
	worker, store, intelligence := analysisFixture(t, 2)
	store.failedWrites = 1
	queuedAnalysis(t, worker)
	if err := worker.processNext(context.Background()); err == nil {
		t.Fatal("fixture failed to interrupt acknowledgement")
	}
	worker.Close()
	restarted := newAnalysisFixtureWorker(t, store, intelligence)
	store.ready()
	processAnalysis(t, restarted)
	if job := analysisStatus(t, restarted); job.AnalyzedSymbols != 1 || job.Chunks != 1 || intelligence.calls.Load() != 1 {
		t.Fatal("partial crash recovery did not acknowledge the committed page before inference")
	}
	store.ready()
	processAnalysis(t, restarted)
	if job := analysisStatus(t, restarted); job.AnalyzedSymbols != 2 || job.FeatureCount != 2 || intelligence.calls.Load() != 2 {
		t.Fatal("recovery lost or repeated the committed partial run")
	}
}

func TestAnalysisSupersedesAutomaticOldSnapshotButProcessesManualRequest(t *testing.T) {
	worker, store, intelligence := analysisFixture(t, 1)
	queuedAnalysis(t, worker)
	store.job.Automatic, store.latest = true, "new-snapshot"
	processAnalysis(t, worker)
	if analysisStatus(t, worker).Status != "superseded" || intelligence.calls.Load() != 0 {
		t.Fatal("automatic obsolete snapshot consumed a model request")
	}
	queuedAnalysis(t, worker)
	processAnalysis(t, worker)
	if analysisStatus(t, worker).Status != "completed" || intelligence.calls.Load() != 1 {
		t.Fatal("explicit snapshot-pinned work was superseded")
	}
}

func TestAnalysisAutomaticCompleteRunIsReusedButManualRegenerationRuns(t *testing.T) {
	worker, store, intelligence := analysisFixture(t, 1)
	store.run = &domain.FeatureRun{ID: "prior", SnapshotID: "snapshot", AnalyzedSymbols: 1, TotalSymbols: 1}
	queuedAnalysis(t, worker)
	store.job.Automatic = true
	processAnalysis(t, worker)
	if intelligence.calls.Load() != 0 {
		t.Fatal("automatic reconciliation regenerated completed coverage")
	}
	queuedAnalysis(t, worker)
	processAnalysis(t, worker)
	if intelligence.calls.Load() != 1 || store.run.ID == "prior" {
		t.Fatal("manual regeneration was mistaken for crash recovery")
	}
}

func TestAnalysisRetriesAreBoundedAndDoNotHotLoop(t *testing.T) {
	worker, store, intelligence := analysisFixture(t, 1)
	worker.cfg.RetryInterval = time.Second
	intelligence.discover = func(context.Context, string) (domain.FeatureRun, error) {
		return domain.FeatureRun{}, domain.ErrInvalidModelOutput
	}
	queuedAnalysis(t, worker)
	processAnalysis(t, worker)
	processAnalysis(t, worker)
	if intelligence.calls.Load() != 1 || !analysisStatus(t, worker).AvailableAt.After(time.Now()) {
		t.Fatal("failed work retried immediately")
	}
	for range 2 {
		store.ready()
		processAnalysis(t, worker)
	}
	job := analysisStatus(t, worker)
	if job.Status != "failed" || job.Attempts != 3 || job.ErrorCode != "invalid_model_output" || intelligence.calls.Load() != 3 {
		t.Fatal("failure retry limit or safe error code missing")
	}
}

func TestAnalysisGateContentionDoesNotBurnFailureAllowance(t *testing.T) {
	worker, _, intelligence := analysisFixture(t, 1)
	intelligence.discover = func(context.Context, string) (domain.FeatureRun, error) { return domain.FeatureRun{}, domain.ErrBusy }
	queuedAnalysis(t, worker)
	processAnalysis(t, worker)
	job := analysisStatus(t, worker)
	if job.Status != "queued" || job.Attempts != 0 || job.ErrorCode != "analysis_busy" {
		t.Fatal("answer contention exhausted analysis retries")
	}
}

func TestAnalysisProgressResetsConsecutiveFailures(t *testing.T) {
	worker, store, intelligence := analysisFixture(t, 2)
	queuedAnalysis(t, worker)
	store.job.Attempts = 2
	processAnalysis(t, worker)
	if job := analysisStatus(t, worker); job.Attempts != 0 || job.AnalyzedSymbols != 1 || job.Chunks != 1 {
		t.Fatal("successful checkpoint retained old consecutive failures")
	}
	intelligence.discover = func(context.Context, string) (domain.FeatureRun, error) {
		return domain.FeatureRun{}, errors.New("PRIVATE failure details")
	}
	store.ready()
	processAnalysis(t, worker)
	if job := analysisStatus(t, worker); job.ErrorCode != "analysis_failed" || job.Attempts != 1 {
		t.Fatal("queue stored raw errors or retained old failures")
	}
}

func TestAnalysisNoProgressAndLostLeaseCannotLoopOrAcknowledge(t *testing.T) {
	worker, store, intelligence := analysisFixture(t, 1)
	queuedAnalysis(t, worker)
	intelligence.discover = func(context.Context, string) (domain.FeatureRun, error) {
		return domain.FeatureRun{TotalSymbols: 5}, nil
	}
	processAnalysis(t, worker)
	if analysisStatus(t, worker).ErrorCode != "no_progress" {
		t.Fatal("empty incomplete result concealed lack of progress")
	}
	store.ready()
	intelligence.discover = func(context.Context, string) (domain.FeatureRun, error) {
		return domain.FeatureRun{}, domain.ErrLeaseLost
	}
	processAnalysis(t, worker)
	if len(store.updates) != 1 {
		t.Fatal("worker acknowledged a lease it no longer owned")
	}
}

func TestAnalysisLostLeaseDuringAcknowledgementLeavesNewOwnerAlone(t *testing.T) {
	worker, store, intelligence := analysisFixture(t, 1)
	queuedAnalysis(t, worker)
	intelligence.discover = func(ctx context.Context, snapshot string) (domain.FeatureRun, error) {
		run, err := store.advance(ctx, snapshot, 1)
		store.mu.Lock()
		store.job.LeaseToken = "another-owner"
		store.mu.Unlock()
		return run, err
	}
	processAnalysis(t, worker)
	if len(store.updates) != 0 || analysisStatus(t, worker).LeaseToken != "another-owner" {
		t.Fatal("expired owner modified the reclaimed job")
	}
}

func TestAnalysisCommittedJobSurvivesDirtySnapshotAtSameCommit(t *testing.T) {
	worker, store, intelligence := analysisFixture(t, 1)
	queuedAnalysis(t, worker)
	store.job.Automatic, store.job.Commit = true, "commit-one"
	store.latest, store.latestCommit = "dirty-snapshot", "commit-one"
	processAnalysis(t, worker)
	if analysisStatus(t, worker).Status != "completed" || intelligence.calls.Load() != 1 {
		t.Fatal("uncommitted changes superseded already queued committed evidence")
	}
}

func TestAnalysisCommittedJobIsSupersededByDifferentCommit(t *testing.T) {
	worker, store, intelligence := analysisFixture(t, 1)
	queuedAnalysis(t, worker)
	store.job.Automatic, store.job.Commit = true, "commit-one"
	store.latest, store.latestCommit = "new-snapshot", "commit-two"
	processAnalysis(t, worker)
	if analysisStatus(t, worker).Status != "superseded" || intelligence.calls.Load() != 0 {
		t.Fatal("obsolete committed evidence consumed inference")
	}
}
