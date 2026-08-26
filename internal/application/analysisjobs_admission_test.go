package application

import (
	"context"
	"errors"
	"testing"
	"time"

	"developa/internal/domain"
)

func TestAnalysisAdmissionWaitsBeforeClaimAndSerializesRepositoryInference(t *testing.T) {
	first, _, model := analysisFixture(t, 1)
	second, secondStore, secondModel := analysisFixture(t, 1)
	first.cfg.RepositoryID, second.cfg.RepositoryID = "first", "second"
	first.cfg.Admission = NewAnalysisAdmission()
	second.cfg.Admission = first.cfg.Admission
	queuedAnalysis(t, first)
	queuedAnalysis(t, second)
	entered, release := make(chan struct{}), make(chan struct{})
	discover := model.discover
	model.discover = func(ctx context.Context, snapshot string) (domain.FeatureRun, error) {
		close(entered)
		if err := waitSaveGate(ctx, release); err != nil {
			return domain.FeatureRun{}, err
		}
		return discover(ctx, snapshot)
	}
	completed := make(chan error, 1)
	go func() { completed <- first.processNext(context.Background()) }()
	awaitAnalysisSignal(t, entered)
	assertWaitingAnalysisHasNoLease(t, second, secondStore)
	close(release)
	if err := <-completed; err != nil {
		t.Fatal(err)
	}
	processAnalysis(t, second)
	if model.calls.Load() != 1 || secondModel.calls.Load() != 1 || analysisStatus(t, second).Status != "completed" {
		t.Fatal("shared admission lost or repeated repository work")
	}
}

func assertWaitingAnalysisHasNoLease(t *testing.T, worker *AnalysisWorker, store *analysisTestStore) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Millisecond)
	defer cancel()
	if err := worker.processNext(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal("waiting analysis ignored cancellation or passed occupied admission")
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	if store.claimCalls != 0 || store.job.LeaseUntil != nil || store.job.Attempts != 0 {
		t.Fatal("waiting for inference capacity claimed or consumed a durable lease")
	}
}

func TestAnalysisAdmissionCancellationClosesWorkerWithoutClaiming(t *testing.T) {
	worker, store, model := analysisFixture(t, 1)
	worker.cfg.Admission = NewAnalysisAdmission()
	release, err := worker.cfg.Admission.acquire(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	queuedAnalysis(t, worker)
	worker.Start(context.Background())
	closed := make(chan struct{})
	go func() { worker.Close(); close(closed) }()
	awaitAnalysisSignal(t, closed)
	if store.claimCalls != 0 || model.calls.Load() != 0 || analysisStatus(t, worker).Status != "queued" {
		t.Fatal("closing an admission waiter claimed work or called inference")
	}
}

func TestAnalysisAdmissionReleasesSlotWhenNoJobCanBeClaimed(t *testing.T) {
	worker, _, model := analysisFixture(t, 1)
	worker.cfg.Admission = NewAnalysisAdmission()
	processAnalysis(t, worker)
	queuedAnalysis(t, worker)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := worker.processNext(ctx); err != nil {
		t.Fatal(err)
	}
	if model.calls.Load() != 1 {
		t.Fatal("an empty queue retained the shared admission slot")
	}
}

func TestAnalysisAdmissionRefusesAlreadyCanceledCallWithoutCapacityUse(t *testing.T) {
	admission := NewAnalysisAdmission()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := admission.acquire(ctx); !errors.Is(err, context.Canceled) {
		t.Fatal("canceled admission obtained capacity")
	}
	active, stop := context.WithTimeout(context.Background(), time.Second)
	defer stop()
	release, err := admission.acquire(active)
	if err != nil {
		t.Fatal(err)
	}
	release()
}
