package application

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"developa/internal/domain"
)

func TestAnalysisShutdownCancelsModelAndReleasesWithoutFailure(t *testing.T) {
	worker, store, intelligence := analysisFixture(t, 1)
	entered := make(chan struct{})
	intelligence.discover = func(ctx context.Context, _ string) (domain.FeatureRun, error) {
		close(entered)
		<-ctx.Done()
		return domain.FeatureRun{}, ctx.Err()
	}
	queuedAnalysis(t, worker)
	worker.Start(context.Background())
	awaitAnalysisSignal(t, entered)
	closed := make(chan struct{})
	go func() { worker.Close(); close(closed) }()
	awaitAnalysisSignal(t, closed)
	job := analysisStatus(t, worker)
	if job.Status != "queued" || job.Attempts != 0 || job.ErrorCode != "" || job.LeaseUntil != nil {
		t.Fatal("shutdown orphaned a lease or consumed a failure attempt")
	}
	if store.leaseTTL != worker.cfg.ExecutionTimeout+30*time.Second {
		t.Fatal("lease does not reserve time for publication and shutdown")
	}
}

func TestAnalysisDeadlineRetriesWhileShutdownDoesNot(t *testing.T) {
	worker, _, intelligence := analysisFixture(t, 1)
	worker.cfg.ExecutionTimeout = 20 * time.Millisecond
	intelligence.discover = func(ctx context.Context, _ string) (domain.FeatureRun, error) {
		<-ctx.Done()
		return domain.FeatureRun{}, ctx.Err()
	}
	queuedAnalysis(t, worker)
	processAnalysis(t, worker)
	job := analysisStatus(t, worker)
	if job.Status != "queued" || job.Attempts != 1 || job.ErrorCode != "model_timeout" {
		t.Fatal("model deadline was mistaken for server shutdown")
	}
}

func TestAnalysisStartupReconciliationRetriesBeforeClaiming(t *testing.T) {
	worker, store, intelligence := analysisFixture(t, 1)
	store.failedEnsure = 1
	worker.Start(context.Background())
	awaitAnalysisStatus(t, worker, "completed")
	worker.Close()
	if store.ensureCalls != 2 || intelligence.calls.Load() != 1 {
		t.Fatal("startup reconciliation failure lost work or ran duplicate analysis")
	}
}

func TestAnalysisStartAndCloseAreRaceSafe(t *testing.T) {
	worker, _, _ := analysisFixture(t, 1)
	var group sync.WaitGroup
	for range 20 {
		group.Add(1)
		go func() {
			defer group.Done()
			worker.Start(context.Background())
			worker.Close()
		}()
	}
	group.Wait()
	worker.Close()
}

func TestAnalysisDisabledWorkerStillReadsStoredStatus(t *testing.T) {
	store := &analysisTestStore{job: domain.AnalysisJob{Status: "completed"}}
	worker := newAnalysisFixtureWorker(t, store, nil)
	if worker.Available() {
		t.Fatal("disabled worker reported configured inference")
	}
	worker.Start(context.Background())
	worker.Close()
	if job := analysisStatus(t, worker); job.Status != "completed" {
		t.Fatal("disabling inference hid durable prior status")
	}
	if _, err := worker.Queue(context.Background(), "snapshot"); !errors.Is(err, domain.ErrModelUnavailable) {
		t.Fatal("disabled worker admitted a model job")
	}
	worker.cfg.RepositoryID = ""
	if _, err := worker.Status(context.Background(), "snapshot"); !errors.Is(err, domain.ErrNotConfigured) {
		t.Fatal("unconfigured repository was not explicit")
	}
}

func awaitAnalysisSignal(t *testing.T, signal <-chan struct{}) {
	t.Helper()
	select {
	case <-signal:
	case <-time.After(3 * time.Second):
		t.Fatal("analysis lifecycle did not settle")
	}
}

func awaitAnalysisStatus(t *testing.T, worker *AnalysisWorker, status string) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if analysisStatus(t, worker).Status == status {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("analysis did not reach expected status")
}
