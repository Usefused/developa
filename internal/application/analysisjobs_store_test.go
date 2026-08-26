package application

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"developa/internal/domain"
)

type analysisTestStore struct {
	domain.AnalysisJobStore
	mu           sync.Mutex
	job          domain.AnalysisJob
	run          *domain.FeatureRun
	latest       string
	latestCommit string
	updates      []domain.AnalysisJobUpdate
	executions   []domain.Execution
	ensureCalls  int
	claimCalls   int
	leaseTTL     time.Duration
	failedWrites int
	failedEnsure int
}

func (s *analysisTestStore) EnqueueAnalysis(ctx context.Context, _, snapshot string, execution domain.Execution) (domain.AnalysisJob, error) {
	if err := ctx.Err(); err != nil {
		return domain.AnalysisJob{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.executions = append(s.executions, execution)
	if s.job.Status == "queued" || s.job.Status == "running" {
		return s.job, nil
	}
	s.resetJob(snapshot, false)
	return s.job, nil
}

func (s *analysisTestStore) resetJob(snapshot string, automatic bool) {
	s.job = domain.AnalysisJob{ID: newExecutionID(), SnapshotID: snapshot, Status: "queued", Automatic: automatic}
	if s.run != nil {
		s.job.BaseRunID = s.run.ID
	}
}

func (s *analysisTestStore) EnsureAnalysis(_ context.Context, _ string, execution domain.Execution) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ensureCalls++
	if s.failedEnsure > 0 {
		s.failedEnsure--
		return errors.New("fixture reconciliation failure")
	}
	if s.job.ID == "" {
		s.resetJob(s.latest, true)
		s.executions = append(s.executions, execution)
	}
	return nil
}

func (s *analysisTestStore) AnalysisStatus(ctx context.Context, _, _ string) (domain.AnalysisJob, error) {
	if err := ctx.Err(); err != nil {
		return domain.AnalysisJob{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.job, nil
}

func (s *analysisTestStore) Latest(context.Context, string) (domain.Snapshot, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return domain.Snapshot{ID: s.latest, Commit: s.latestCommit}, nil
}

func (s *analysisTestStore) ClaimAnalysis(ctx context.Context, _, token string, ttl time.Duration) (domain.AnalysisJob, error) {
	if err := ctx.Err(); err != nil {
		return domain.AnalysisJob{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.claimCalls++
	if !s.claimable() {
		return domain.AnalysisJob{}, domain.ErrNotFound
	}
	until := time.Now().Add(ttl)
	s.job.Status, s.job.LeaseToken, s.job.LeaseUntil = "running", token, &until
	s.leaseTTL = ttl
	return s.job, nil
}

func (s *analysisTestStore) claimable() bool {
	if s.job.Status == "queued" {
		return !time.Now().Before(s.job.AvailableAt)
	}
	if s.job.Status == "running" && s.job.LeaseUntil != nil && time.Now().After(*s.job.LeaseUntil) {
		s.job.Attempts++
		return true
	}
	return false
}

func (s *analysisTestStore) UpdateAnalysis(ctx context.Context, _, id, token string, update domain.AnalysisJobUpdate) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.failedWrites > 0 {
		s.failedWrites--
		return errors.New("fixture update failed after model publication")
	}
	if s.job.ID != id || s.job.LeaseToken != token {
		return domain.ErrLeaseLost
	}
	s.applyUpdate(update)
	return nil
}

func (s *analysisTestStore) applyUpdate(update domain.AnalysisJobUpdate) {
	s.updates = append(s.updates, update)
	s.job.Status, s.job.ErrorCode, s.job.AvailableAt = update.Status, update.ErrorCode, update.AvailableAt
	s.job.AnalyzedSymbols, s.job.TotalSymbols, s.job.FeatureCount = update.AnalyzedSymbols, update.TotalSymbols, update.FeatureCount
	s.job.LeaseToken, s.job.LeaseUntil = "", nil
	if update.Failed {
		s.job.Attempts++
	}
	if update.Progress {
		s.job.Chunks++
		s.job.Attempts = 0
		if s.run != nil {
			s.job.BaseRunID = s.run.ID
		}
	}
}

func (s *analysisTestStore) Features(_ context.Context, _, _ string, filter domain.Filter) (domain.FeaturePage, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if filter.Limit != 1 || filter.Offset != 0 {
		return domain.FeaturePage{}, errors.New("worker must fetch bounded run metadata")
	}
	if s.run == nil {
		return domain.FeaturePage{}, nil
	}
	run := *s.run
	return domain.FeaturePage{Run: &run}, nil
}

func (s *analysisTestStore) advance(ctx context.Context, snapshot string, total int) (domain.FeatureRun, error) {
	lease, ok := ctx.Value(analysisLeaseKey{}).(analysisLease)
	if !ok {
		return domain.FeatureRun{}, errors.New("worker lost lease context")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if lease.jobID != s.job.ID || lease.token != s.job.LeaseToken {
		return domain.FeatureRun{}, domain.ErrLeaseLost
	}
	run := domain.FeatureRun{ID: newExecutionID(), SnapshotID: snapshot, Model: "fixture", TotalSymbols: total, AnalyzedSymbols: 1, FeatureCount: 1, Status: "partial"}
	if s.run != nil && s.run.AnalyzedSymbols < total {
		run.ParentRunID = s.run.ID
		run.AnalyzedSymbols, run.FeatureCount = s.run.AnalyzedSymbols+1, s.run.FeatureCount+1
	}
	if run.AnalyzedSymbols >= total {
		run.Status = "completed"
	}
	s.run = &run
	return run, nil
}

func (s *analysisTestStore) ready() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.job.AvailableAt = time.Now().Add(-time.Second)
	if s.job.LeaseUntil != nil {
		past := time.Now().Add(-time.Second)
		s.job.LeaseUntil = &past
	}
}

type analysisTestIntelligence struct {
	domain.Intelligence
	disabled bool
	calls    atomic.Int32
	discover func(context.Context, string) (domain.FeatureRun, error)
}

func (s *analysisTestIntelligence) Available() bool { return !s.disabled }
func (s *analysisTestIntelligence) Discover(ctx context.Context, snapshot string) (domain.FeatureRun, error) {
	s.calls.Add(1)
	return s.discover(ctx, snapshot)
}

func analysisFixture(t *testing.T, total int) (*AnalysisWorker, *analysisTestStore, *analysisTestIntelligence) {
	t.Helper()
	store := &analysisTestStore{latest: "snapshot"}
	intelligence := &analysisTestIntelligence{discover: func(ctx context.Context, snapshot string) (domain.FeatureRun, error) {
		return store.advance(ctx, snapshot, total)
	}}
	worker := newAnalysisFixtureWorker(t, store, intelligence)
	return worker, store, intelligence
}

func newAnalysisFixtureWorker(t *testing.T, store *analysisTestStore, intelligence domain.Intelligence) *AnalysisWorker {
	t.Helper()
	worker, err := NewAnalysisWorker(store, intelligence, AnalysisWorkerConfig{RepositoryID: "repository", PollInterval: 10 * time.Millisecond,
		ExecutionTimeout: time.Second, RetryInterval: 10 * time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(worker.Close)
	return worker
}

func queuedAnalysis(t *testing.T, worker *AnalysisWorker) domain.AnalysisJob {
	t.Helper()
	job, err := worker.Queue(context.Background(), "snapshot")
	if err != nil {
		t.Fatal(err)
	}
	return job
}

func processAnalysis(t *testing.T, worker *AnalysisWorker) {
	t.Helper()
	if err := worker.processNext(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func analysisStatus(t *testing.T, worker *AnalysisWorker) domain.AnalysisJob {
	t.Helper()
	job, err := worker.Status(context.Background(), "snapshot")
	if err != nil {
		t.Fatal(err)
	}
	return job
}
