package postgres

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"developa/internal/domain"
)

func TestIntegrationAnalysisFeatureCheckpointsAdvanceBaseline(t *testing.T) {
	store, _ := catalogFixture(t)
	report := committedReport(t, 1, "checkpoints")
	snapshot := saveReport(t, store, "repo", report)
	enqueueManual(t, store, snapshot.ID)
	failed := claimJob(t, store, "failed-worker")
	updateJob(t, store, failed, domain.AnalysisJobUpdate{Status: "queued", Failed: true, ErrorCode: "model_unavailable"})
	job := claimJob(t, store, "checkpoint-worker")
	run := continuationRun(snapshot, "chunk-one", "", 1)
	features := featureFixture(1, report.Index.Files[0].Symbols[0].ID)
	saveLeasedFeatures(t, store, run, features, job)
	updateJob(t, store, job, progressUpdate(run, "queued", 1))
	checkpoint := assertJobStatus(t, store, snapshot.ID, "queued")
	if checkpoint.BaseRunID != run.ID || checkpoint.Chunks != 1 || checkpoint.Attempts != 0 || checkpoint.AnalyzedSymbols != 1 {
		t.Fatalf("checkpoint acknowledgement lost durable progress: %+v", checkpoint)
	}
	assertFinalCheckpoint(t, store, snapshot, features[0].Evidence[0].SymbolID, run.ID)
}

func assertFinalCheckpoint(t *testing.T, store *Store, snapshot domain.Snapshot, symbolID, parentID string) {
	t.Helper()
	job := claimJob(t, store, "final-worker")
	run := runFixture(snapshot, "chunk-two")
	run.ParentRunID = parentID
	features := featureFixture(1, symbolID)
	features[0].ID = fingerprint("second-chunk-feature")
	saveLeasedFeatures(t, store, run, features, job)
	updateJob(t, store, job, progressUpdate(run, "completed", 2))
	completed := assertJobStatus(t, store, snapshot.ID, "completed")
	if completed.BaseRunID != run.ID || completed.Chunks != 2 || completed.FeatureCount != 2 {
		t.Fatal("final checkpoint did not accumulate feature progress")
	}
	regenerated := enqueueManual(t, store, snapshot.ID)
	if regenerated.BaseRunID != run.ID || regenerated.Chunks != 0 || regenerated.FeatureCount != 2 {
		t.Fatal("manual regeneration lost its prior-generation baseline")
	}
}

func TestIntegrationAnalysisStaleFeatureLeaseCannotPublish(t *testing.T) {
	store, _ := catalogFixture(t)
	report := committedReport(t, 1, "fenced-feature")
	snapshot := saveReport(t, store, "repo", report)
	features := featureFixture(1, report.Index.Files[0].Symbols[0].ID)
	saveFeatureFixture(t, store, snapshot, "previous-run", features)
	enqueueManual(t, store, snapshot.ID)
	stale := claimJob(t, store, "stale-worker")
	expireJob(t, store, stale)
	current := claimJob(t, store, "current-worker")
	execution := leasedExecution(stale)
	err := store.SaveFeatures(context.Background(), "repo", runFixture(snapshot, "stale-run"), features, execution)
	if !errors.Is(err, domain.ErrLeaseLost) {
		t.Fatalf("stale feature write was not fenced: %v", err)
	}
	assertCurrentFeatureRun(t, store, snapshot.ID, "previous-run")
	assertTableCount(t, store, "developa_feature_runs", 1)
	saveLeasedFeatures(t, store, runFixture(snapshot, "current-run"), features, current)
	assertCurrentFeatureRun(t, store, snapshot.ID, "current-run")
}

func TestIntegrationAnalysisLeaseExpiryDuringPublicationRollsBack(t *testing.T) {
	store, _ := catalogFixture(t)
	report := committedReport(t, 1, "mid-publication-expiry")
	snapshot := saveReport(t, store, "repo", report)
	features := featureFixture(1, report.Index.Files[0].Symbols[0].ID)
	saveFeatureFixture(t, store, snapshot, "before-expiry", features)
	enqueueManual(t, store, snapshot.ID)
	_, err := store.pool.Exec(context.Background(), `CREATE FUNCTION slow_feature_write() RETURNS trigger LANGUAGE plpgsql AS
		$$ BEGIN PERFORM pg_sleep(0.3); RETURN NEW; END $$;
		CREATE TRIGGER slow_feature_write BEFORE INSERT ON developa_feature_runs FOR EACH ROW EXECUTE FUNCTION slow_feature_write()`)
	if err != nil {
		t.Fatal(err)
	}
	job, err := store.ClaimAnalysis(context.Background(), "repo", "expires-mid-write", 150*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	err = store.SaveFeatures(context.Background(), "repo", runFixture(snapshot, "expired-during-write"), features, leasedExecution(job))
	if !errors.Is(err, domain.ErrLeaseLost) {
		t.Fatalf("lease expiry during mutation did not abort publication: %v", err)
	}
	assertCurrentFeatureRun(t, store, snapshot.ID, "before-expiry")
	assertTableCount(t, store, "developa_feature_runs", 1)
	assertTableCount(t, store, "developa_audit_events", 4)
	assertTableCount(t, store, "developa_audit_outbox", 4)
}

func TestIntegrationAnalysisPublicationCrashRetainsBaseline(t *testing.T) {
	store, _ := catalogFixture(t)
	report := committedReport(t, 1, "publication-crash")
	snapshot := saveReport(t, store, "repo", report)
	features := featureFixture(1, report.Index.Files[0].Symbols[0].ID)
	saveFeatureFixture(t, store, snapshot, "old-complete", features)
	enqueueManual(t, store, snapshot.ID)
	job := claimJob(t, store, "crashing-worker")
	saveLeasedFeatures(t, store, runFixture(snapshot, "published-before-crash"), features, job)
	expireJob(t, store, job)
	recovered := claimJob(t, store, "recovered-worker")
	if recovered.BaseRunID != "old-complete" {
		t.Fatal("publication prematurely acknowledged its own job baseline")
	}
	assertCurrentFeatureRun(t, store, snapshot.ID, "published-before-crash")
	updateJob(t, store, recovered, progressUpdate(runFixture(snapshot, "published-before-crash"), "completed", 1))
	assertJobStatus(t, store, snapshot.ID, "completed")
}

func TestIntegrationAnalysisFencedWritesKeepConstantQueryBudget(t *testing.T) {
	store, counter := catalogFixture(t)
	report := committedReport(t, 1, "leased-budget")
	snapshot := saveReport(t, store, "repo", report)
	enqueueManual(t, store, snapshot.ID)
	job := claimJob(t, store, "leased-budget-worker")
	var previous int64
	for _, count := range []int{1, 100} {
		features := featureFixture(count, report.Index.Files[0].Symbols[0].ID)
		run := runFixture(snapshot, fmt.Sprintf("fenced-budget-%d", count))
		counter.Store(0)
		saveLeasedFeatures(t, store, run, features, job)
		queries := counter.Load()
		if queries > 12 || previous != 0 && queries != previous {
			t.Fatalf("fenced write query budget grew: %d -> %d", previous, queries)
		}
		previous = queries
	}
}

func TestIntegrationAnalysisUpdateAuditFailureRollsBack(t *testing.T) {
	store, _ := catalogFixture(t)
	snapshot := saveReport(t, store, "repo", committedReport(t, 1, "update-audit-failure"))
	enqueueManual(t, store, snapshot.ID)
	job := claimJob(t, store, "audited-worker")
	_, err := store.pool.Exec(context.Background(), `ALTER TABLE developa_audit_events ADD CONSTRAINT reject_completion
		CHECK (job_id IS NULL OR outcome<>'completed')`)
	if err != nil {
		t.Fatal(err)
	}
	err = store.UpdateAnalysis(context.Background(), "repo", job.ID, job.LeaseToken, domain.AnalysisJobUpdate{Status: "completed"})
	if !errors.Is(err, ErrUnavailable) {
		t.Fatal("audit failure did not abort job completion")
	}
	assertJobStatus(t, store, snapshot.ID, "running")
	assertTableCount(t, store, "developa_audit_events", 3)
	assertTableCount(t, store, "developa_audit_outbox", 3)
}

func leasedExecution(job domain.AnalysisJob) domain.Execution {
	execution := testExecution()
	execution.JobID, execution.LeaseToken = job.ID, job.LeaseToken
	return execution
}

func progressUpdate(run domain.FeatureRun, status string, features int) domain.AnalysisJobUpdate {
	return domain.AnalysisJobUpdate{Status: status, Progress: true, AnalyzedSymbols: run.AnalyzedSymbols,
		TotalSymbols: run.TotalSymbols, FeatureCount: features}
}

func saveLeasedFeatures(t *testing.T, store *Store, run domain.FeatureRun, features []domain.Feature, job domain.AnalysisJob) {
	t.Helper()
	if err := store.SaveFeatures(context.Background(), "repo", run, features, leasedExecution(job)); err != nil {
		t.Fatal(err)
	}
}

func assertCurrentFeatureRun(t *testing.T, store *Store, snapshotID, runID string) {
	t.Helper()
	page, err := store.Features(context.Background(), "repo", snapshotID, domain.Filter{Limit: 1})
	if err != nil || page.Run == nil || page.Run.ID != runID {
		t.Fatalf("expected current run %s: %+v, %v", runID, page, err)
	}
}
