package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"developa/internal/application"
	"developa/internal/domain"
	"go.opentelemetry.io/otel/propagation"
)

func TestIntegrationAnalysisPublicationAndSupersessionAreAtomic(t *testing.T) {
	store, _ := catalogFixture(t)
	store.analysisEnabled = true
	first := saveReport(t, store, "repo", committedReport(t, 1, "auto-first"))
	assertJobStatus(t, store, first.ID, "queued")
	second := saveReport(t, store, "repo", committedReport(t, 1, "auto-second"))
	assertJobStatus(t, store, first.ID, "superseded")
	assertJobStatus(t, store, second.ID, "queued")
	assertTableCount(t, store, "developa_analysis_jobs", 2)
	assertTableCount(t, store, "developa_audit_events", 5)
	assertTableCount(t, store, "developa_audit_outbox", 5)
	assertQueuePublicationRollback(t, store, second)
}

func assertQueuePublicationRollback(t *testing.T, store *Store, previous domain.Snapshot) {
	t.Helper()
	_, err := store.pool.Exec(context.Background(), `ALTER TABLE developa_analysis_jobs ADD CONSTRAINT reject_enqueue CHECK (execution_id<>'rejected')`)
	if err != nil {
		t.Fatal(err)
	}
	execution := testExecution()
	execution.ID = "rejected"
	_, err = store.SaveSnapshot(context.Background(), "repo", committedReport(t, 1, "auto-rejected"), execution)
	if !errors.Is(err, ErrUnavailable) {
		t.Fatalf("queue failure did not abort publication: %v", err)
	}
	assertLatestID(t, store, previous.ID)
	assertJobStatus(t, store, previous.ID, "queued")
	assertTableCount(t, store, "developa_snapshots", 2)
	assertTableCount(t, store, "developa_audit_events", 5)
}

func TestIntegrationAnalysisStartupAndManualDedup(t *testing.T) {
	store, _ := catalogFixture(t)
	snapshot := saveReport(t, store, "repo", committedReport(t, 1, "existing-index"))
	assertJobStatus(t, store, snapshot.ID, "not_queued")
	store.analysisEnabled = true
	if err := store.EnsureAnalysis(context.Background(), "repo", testExecution()); err != nil {
		t.Fatal(err)
	}
	queued := assertJobStatus(t, store, snapshot.ID, "queued")
	manual := enqueueManual(t, store, snapshot.ID)
	if manual.ID != queued.ID || manual.Automatic {
		t.Fatal("active job was duplicated or manual intent was lost")
	}
	claimed := claimJob(t, store, "worker-one")
	dedup := enqueueManual(t, store, snapshot.ID)
	if dedup.Status != "running" || dedup.LeaseToken != "" {
		t.Fatal("active lease was reset or exposed")
	}
	updateJob(t, store, claimed, domain.AnalysisJobUpdate{Status: "completed"})
	if err := store.EnsureAnalysis(context.Background(), "repo", testExecution()); err != nil {
		t.Fatal(err)
	}
	assertJobStatus(t, store, snapshot.ID, "completed")
	regenerated := enqueueManual(t, store, snapshot.ID)
	if regenerated.Automatic || regenerated.Attempts != 0 || regenerated.Status != "queued" {
		t.Fatalf("manual regeneration was not reset: %+v", regenerated)
	}
}

func TestIntegrationAnalysisManualKeepsOlderSnapshotRunnable(t *testing.T) {
	store, _ := catalogFixture(t)
	first := saveReport(t, store, "repo", committedReport(t, 1, "manual-old"))
	enqueueManual(t, store, first.ID)
	store.analysisEnabled = true
	second := saveReport(t, store, "repo", committedReport(t, 1, "manual-new"))
	assertJobStatus(t, store, first.ID, "queued")
	assertJobStatus(t, store, second.ID, "queued")
	job := claimJob(t, store, "old-worker")
	if job.SnapshotID != first.ID || job.Automatic {
		t.Fatal("manual old snapshot lost its request scope")
	}
}

func TestIntegrationAnalysisDisabledKeepsAutomaticJobsAndRunsManualWork(t *testing.T) {
	store, counter := catalogFixture(t)
	store.analysisEnabled = true
	automatic := saveReport(t, store, "repo", committedReport(t, 1, "auto-before-opt-out"))
	store.analysisEnabled = false
	manual := saveReport(t, store, "repo", committedReport(t, 1, "manual-after-opt-out"))
	counter.Store(0)
	if err := store.EnsureAnalysis(context.Background(), "repo", testExecution()); err != nil || counter.Load() != 0 {
		t.Fatal("disabled startup reconciliation performed database work")
	}
	assertJobStatus(t, store, manual.ID, "not_queued")
	_, err := store.ClaimAnalysis(context.Background(), "repo", "disabled-auto-worker", time.Minute)
	if !errors.Is(err, domain.ErrNotFound) {
		t.Fatal("disabled automatic work was claimed")
	}
	assertJobStatus(t, store, automatic.ID, "queued")
	enqueueManual(t, store, manual.ID)
	job := claimJob(t, store, "manual-only-worker")
	if job.Automatic || job.SnapshotID != manual.ID {
		t.Fatal("explicit manual work was blocked by disabled automation")
	}
	updateJob(t, store, job, domain.AnalysisJobUpdate{Status: "completed"})
	assertJobStatus(t, store, automatic.ID, "queued")
	assertTableCount(t, store, "developa_analysis_jobs", 2)
}

func TestIntegrationAnalysisDisabledDoesNotExpireAutomaticJobs(t *testing.T) {
	store, _ := catalogFixture(t)
	store.analysisEnabled = true
	snapshot := saveReport(t, store, "repo", committedReport(t, 1, "paused-expired-automatic"))
	job := claimJob(t, store, "paused-owner")
	_, err := store.pool.Exec(context.Background(), `UPDATE developa_analysis_jobs SET attempts=2,
		lease_until=clock_timestamp()-interval '1 second' WHERE repository_id='repo' AND id=$1`, job.ID)
	if err != nil {
		t.Fatal(err)
	}
	store.analysisEnabled = false
	_, err = store.ClaimAnalysis(context.Background(), "repo", "manual-only-owner", time.Minute)
	if !errors.Is(err, domain.ErrNotFound) {
		t.Fatal("disabled automatic lease was recovered")
	}
	paused := assertJobStatus(t, store, snapshot.ID, "running")
	if paused.Attempts != 2 {
		t.Fatal("disabled automatic work consumed its retry budget")
	}
	assertTableCount(t, store, "developa_audit_events", 3)
}

func TestIntegrationAnalysisScopeAndTracePrivacy(t *testing.T) {
	store, _ := catalogFixture(t)
	snapshot := saveReport(t, store, "repo", committedReport(t, 1, "queue-trace"))
	origin := "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01"
	ctx := propagation.TraceContext{}.Extract(context.Background(), propagation.MapCarrier{"traceparent": origin})
	execution := manualExecution()
	job, err := store.EnqueueAnalysis(ctx, "repo", snapshot.ID, execution)
	if err != nil || job.TraceParent != origin {
		t.Fatalf("queue origin trace was lost: %v", err)
	}
	assertQueueScope(t, store, snapshot.ID)
	claimed := claimJob(t, store, "private-lease-secret")
	payload, err := json.Marshal(claimed)
	if err != nil || strings.Contains(string(payload), "private-lease-secret") || strings.Contains(string(payload), origin) {
		t.Fatal("queue API JSON exposed internal trace or lease token")
	}
	var audit string
	err = store.pool.QueryRow(ctx, `SELECT jsonb_agg(to_jsonb(e))::text FROM developa_audit_events e WHERE job_id=$1`, job.ID).Scan(&audit)
	if err != nil || !strings.Contains(audit, execution.TraceID) || strings.Contains(audit, "private-lease-secret") {
		t.Fatal("queue audit lost origin identity or exposed lease token")
	}
}

func assertQueueScope(t *testing.T, store *Store, snapshotID string) {
	t.Helper()
	_, statusErr := store.AnalysisStatus(context.Background(), "other", snapshotID)
	_, enqueueErr := store.EnqueueAnalysis(context.Background(), "other", snapshotID, manualExecution())
	_, claimErr := store.ClaimAnalysis(context.Background(), "other", "worker", time.Minute)
	for _, err := range []error{statusErr, enqueueErr, claimErr} {
		if !errors.Is(err, domain.ErrNotFound) {
			t.Fatalf("queue scope failure: %v", err)
		}
	}
}

func TestIntegrationAnalysisFixedQueryBudget(t *testing.T) {
	store, counter := catalogFixture(t)
	store.analysisEnabled = true
	var previous int64
	for _, count := range []int{1, 100} {
		report := committedReport(t, count, fmt.Sprintf("queue-budget-%d", count))
		counter.Store(0)
		snapshot, err := store.SaveSnapshot(context.Background(), "repo", report, testExecution())
		queries := counter.Load()
		if err != nil || queries > 19 || previous != 0 && queries != previous {
			t.Fatalf("auto publication query budget changed: %d -> %d, %v", previous, queries, err)
		}
		previous = queries
		assertQueueQueryBudget(t, store, counter, snapshot.ID)
	}
}

func assertQueueQueryBudget(t *testing.T, store *Store, counter *queryCounter, snapshotID string) {
	t.Helper()
	counter.Store(0)
	assertJobStatus(t, store, snapshotID, "queued")
	if counter.Load() != 1 {
		t.Fatal("status exceeded one query")
	}
	counter.Store(0)
	job := claimJob(t, store, "budget-worker")
	if counter.Load() != 2 {
		t.Fatal("claim exceeded two bounded queries")
	}
	counter.Store(0)
	updateJob(t, store, job, domain.AnalysisJobUpdate{Status: "completed"})
	if counter.Load() != 1 {
		t.Fatal("update exceeded one query")
	}
}

func manualExecution() domain.Execution {
	execution := testExecution()
	execution.Trigger = "feature_manual"
	return execution
}

func committedReport(t *testing.T, count int, seed string) application.Report {
	t.Helper()
	report := catalogReport(t, count, seed)
	report.Snapshot.Commit = fingerprint(seed)
	return report
}

func enqueueManual(t *testing.T, store *Store, snapshotID string) domain.AnalysisJob {
	t.Helper()
	job, err := store.EnqueueAnalysis(context.Background(), "repo", snapshotID, manualExecution())
	if err != nil {
		t.Fatal(err)
	}
	return job
}

func claimJob(t *testing.T, store *Store, token string) domain.AnalysisJob {
	t.Helper()
	job, err := store.ClaimAnalysis(context.Background(), "repo", token, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	return job
}

func updateJob(t *testing.T, store *Store, job domain.AnalysisJob, update domain.AnalysisJobUpdate) {
	t.Helper()
	if err := store.UpdateAnalysis(context.Background(), "repo", job.ID, job.LeaseToken, update); err != nil {
		t.Fatal(err)
	}
}

func assertJobStatus(t *testing.T, store *Store, snapshotID, status string) domain.AnalysisJob {
	t.Helper()
	job, err := store.AnalysisStatus(context.Background(), "repo", snapshotID)
	if err != nil || job.Status != status {
		t.Fatalf("job status expected %s, got %+v: %v", status, job, err)
	}
	return job
}
