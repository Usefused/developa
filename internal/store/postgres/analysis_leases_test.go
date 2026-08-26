package postgres

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"developa/internal/domain"
)

func TestIntegrationAnalysisFailureAndCancellationRetry(t *testing.T) {
	store, _ := catalogFixture(t)
	snapshot := saveReport(t, store, "repo", committedReport(t, 1, "job-failures"))
	enqueueManual(t, store, snapshot.ID)
	job := claimJob(t, store, "first-worker")
	if job.Attempts != 0 {
		t.Fatal("claim counted a failure before work ran")
	}
	updateJob(t, store, job, domain.AnalysisJobUpdate{Status: "queued"})
	assertJobStatus(t, store, snapshot.ID, "queued")
	for attempt := 1; attempt <= 3; attempt++ {
		job = claimJob(t, store, fmt.Sprintf("worker-%d", attempt))
		if job.Attempts != attempt-1 {
			t.Fatalf("wrong consecutive failure count: %d", job.Attempts)
		}
		updateJob(t, store, job, domain.AnalysisJobUpdate{Status: "queued", Failed: true, ErrorCode: "model_unavailable"})
	}
	failed := assertJobStatus(t, store, snapshot.ID, "failed")
	if failed.Attempts != 3 || failed.ErrorCode != "model_unavailable" {
		t.Fatal("failed work did not stop at its retry bound")
	}
	_, err := store.ClaimAnalysis(context.Background(), "repo", "fourth-worker", time.Minute)
	if !errors.Is(err, domain.ErrNotFound) {
		t.Fatal("terminal failed job was claimed")
	}
	reset := enqueueManual(t, store, snapshot.ID)
	if reset.Attempts != 0 || reset.ErrorCode != "" {
		t.Fatal("manual retry did not reset failure state")
	}
}

func TestIntegrationAnalysisExpiredLeaseIsFencedAndBounded(t *testing.T) {
	store, _ := catalogFixture(t)
	snapshot := saveReport(t, store, "repo", committedReport(t, 1, "expired-lease"))
	enqueueManual(t, store, snapshot.ID)
	first := claimJob(t, store, "first-owner")
	expireJob(t, store, first)
	second := claimJob(t, store, "second-owner")
	if second.Attempts != 1 || second.ID != first.ID {
		t.Fatal("expired lease was not recovered with one failure")
	}
	assertUpdateLeaseLost(t, store, "repo", first)
	assertUpdateLeaseLost(t, store, "other", second)
	expireJob(t, store, second)
	third := claimJob(t, store, "third-owner")
	expireJob(t, store, third)
	_, err := store.ClaimAnalysis(context.Background(), "repo", "fourth-owner", time.Minute)
	if !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("three expired leases should be terminal: %v", err)
	}
	failed := assertJobStatus(t, store, snapshot.ID, "failed")
	if failed.Attempts != 3 || failed.ErrorCode != "lease_expired" {
		t.Fatal("abandoned attempts were not counted")
	}
}

func TestIntegrationAnalysisClaimSkipsLockedRows(t *testing.T) {
	store, _ := catalogFixture(t)
	first := saveReport(t, store, "repo", committedReport(t, 1, "locked-first"))
	second := saveReport(t, store, "repo", committedReport(t, 1, "locked-second"))
	enqueueManual(t, store, first.ID)
	enqueueManual(t, store, second.ID)
	tx, err := store.pool.Begin(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer rollback(tx)
	_, err = tx.Exec(context.Background(), `SELECT id FROM developa_analysis_jobs WHERE repository_id='repo' AND snapshot_id=$1 FOR UPDATE`, first.ID)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	job, err := store.ClaimAnalysis(ctx, "repo", "unblocked-worker", time.Minute)
	if err != nil || job.SnapshotID != second.ID {
		t.Fatalf("claim blocked behind another worker: %+v, %v", job, err)
	}
}

func TestIntegrationAnalysisClaimAuditDoesNotDeadlockPublicationLocks(t *testing.T) {
	store, _ := catalogFixture(t)
	snapshot := saveReport(t, store, "repo", committedReport(t, 1, "audit-lock-order"))
	enqueueManual(t, store, snapshot.ID)
	tx, err := store.pool.Begin(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer rollback(tx)
	if err := lockRepository(context.Background(), tx, "repo"); err != nil {
		t.Fatal(err)
	}
	if _, err := lockAnalysisSnapshot(context.Background(), tx, "repo", snapshot.ID); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	job, err := store.ClaimAnalysis(ctx, "repo", "audit-lock-worker", time.Minute)
	if err != nil || job.Status != "running" {
		t.Fatalf("queue audit FK checks blocked behind pointer-only publication locks: %v", err)
	}
}

func TestIntegrationAnalysisBackoffAndCanceledUpdate(t *testing.T) {
	store, _ := catalogFixture(t)
	snapshot := saveReport(t, store, "repo", committedReport(t, 1, "backoff"))
	enqueueManual(t, store, snapshot.ID)
	job := claimJob(t, store, "backoff-worker")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := store.UpdateAnalysis(ctx, "repo", job.ID, job.LeaseToken, domain.AnalysisJobUpdate{Status: "queued"})
	if !errors.Is(err, context.Canceled) {
		t.Fatal("canceled update did not fail")
	}
	assertJobStatus(t, store, snapshot.ID, "running")
	updateJob(t, store, job, domain.AnalysisJobUpdate{Status: "queued", AvailableAt: time.Now().Add(time.Hour)})
	_, err = store.ClaimAnalysis(context.Background(), "repo", "early-worker", time.Minute)
	if !errors.Is(err, domain.ErrNotFound) {
		t.Fatal("backoff was not respected")
	}
}

func TestIntegrationAnalysisManualPromotionSurvivesSourceAdvance(t *testing.T) {
	store, _ := catalogFixture(t)
	store.analysisEnabled = true
	snapshot := saveReport(t, store, "repo", committedReport(t, 1, "promoted-old"))
	job := claimJob(t, store, "automatic-worker")
	promoted := enqueueManual(t, store, snapshot.ID)
	if promoted.Automatic || promoted.Status != "running" || promoted.ID != job.ID {
		t.Fatal("manual admission did not preserve and promote active work")
	}
	saveReport(t, store, "repo", committedReport(t, 1, "promoted-new"))
	updateJob(t, store, job, domain.AnalysisJobUpdate{Status: "superseded"})
	queued := assertJobStatus(t, store, snapshot.ID, "queued")
	if queued.Automatic {
		t.Fatal("cached automatic flag canceled explicit manual work")
	}
}

func TestIntegrationAnalysisOutdatedAutomaticRequeueIsSuperseded(t *testing.T) {
	store, _ := catalogFixture(t)
	store.analysisEnabled = true
	snapshot := saveReport(t, store, "repo", committedReport(t, 1, "running-old"))
	job := claimJob(t, store, "old-auto-worker")
	saveReport(t, store, "repo", committedReport(t, 1, "running-new"))
	assertJobStatus(t, store, snapshot.ID, "running")
	updateJob(t, store, job, domain.AnalysisJobUpdate{Status: "queued"})
	assertJobStatus(t, store, snapshot.ID, "superseded")
}

func expireJob(t *testing.T, store *Store, job domain.AnalysisJob) {
	t.Helper()
	_, err := store.pool.Exec(context.Background(), `UPDATE developa_analysis_jobs SET lease_until=clock_timestamp()-interval '1 second'
		WHERE repository_id='repo' AND id=$1`, job.ID)
	if err != nil {
		t.Fatal(err)
	}
}

func assertUpdateLeaseLost(t *testing.T, store *Store, repositoryID string, job domain.AnalysisJob) {
	t.Helper()
	err := store.UpdateAnalysis(context.Background(), repositoryID, job.ID, job.LeaseToken, domain.AnalysisJobUpdate{Status: "completed"})
	if !errors.Is(err, domain.ErrLeaseLost) {
		t.Fatalf("unowned lease update was not fenced: %v", err)
	}
}
