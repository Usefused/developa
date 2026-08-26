package postgres

import (
	"context"
	"testing"

	"developa/internal/application"
	"developa/internal/domain"
)

func TestIntegrationAnalysisAutomaticRequiresCleanCompleteCommit(t *testing.T) {
	store, _ := catalogFixture(t)
	store.analysisEnabled = true
	for _, state := range []string{"dirty", "no-head", "incomplete"} {
		report := committedReport(t, 1, state)
		report.Snapshot.Dirty = state == "dirty"
		report.Snapshot.Complete = state != "incomplete"
		if state == "no-head" {
			report.Snapshot.Commit = ""
		}
		snapshot := saveReport(t, store, "repo", report)
		assertJobStatus(t, store, snapshot.ID, "not_queued")
		if err := store.EnsureAnalysis(context.Background(), "repo", testExecution()); err != nil {
			t.Fatal(err)
		}
		assertJobStatus(t, store, snapshot.ID, "not_queued")
		enqueueManual(t, store, snapshot.ID)
		assertJobStatus(t, store, snapshot.ID, "queued")
	}
	assertTableCount(t, store, "developa_analysis_commits", 0)
}

func TestIntegrationAnalysisCommitAdmissionSurvivesPromotionAndRestart(t *testing.T) {
	store, _ := catalogFixture(t)
	store.analysisEnabled = true
	report := committedReport(t, 1, "one-commit")
	original := saveExecution(t, store, report, "original-publication")
	enqueueManual(t, store, original.ID)
	job := claimJob(t, store, "original-worker")
	updateJob(t, store, job, domain.AnalysisJobUpdate{Status: "completed"})
	revisited := saveExecution(t, store, report, "revisited-publication")
	assertJobStatus(t, store, revisited.ID, "not_queued")
	if err := store.EnsureAnalysis(context.Background(), "repo", testExecution()); err != nil {
		t.Fatal(err)
	}
	assertJobStatus(t, store, revisited.ID, "not_queued")
	assertTableCount(t, store, "developa_analysis_commits", 1)
	assertTableCount(t, store, "developa_analysis_jobs", 1)
	manual := enqueueManual(t, store, revisited.ID)
	if manual.Commit != report.Snapshot.Commit || manual.Automatic {
		t.Fatal("manual revisit did not retain explicit commit scope")
	}
}

func TestIntegrationAnalysisDirtySameHeadKeepsCommittedWork(t *testing.T) {
	store, _ := catalogFixture(t)
	store.analysisEnabled = true
	report := committedReport(t, 1, "committed-head")
	clean := saveReport(t, store, "repo", report)
	job := claimJob(t, store, "committed-worker")
	dirty := committedReport(t, 2, "working-tree-edit")
	dirty.Snapshot.Commit, dirty.Snapshot.Dirty = report.Snapshot.Commit, true
	changed := saveReport(t, store, "repo", dirty)
	assertJobStatus(t, store, changed.ID, "not_queued")
	updateJob(t, store, job, domain.AnalysisJobUpdate{Status: "queued"})
	queued := assertJobStatus(t, store, clean.ID, "queued")
	if queued.Commit != report.Snapshot.Commit || !queued.Automatic {
		t.Fatal("dirty source canceled analysis of its committed HEAD")
	}
	assertTableCount(t, store, "developa_analysis_commits", 1)
}

func TestIntegrationAnalysisCommitAdmissionIsRepositoryScoped(t *testing.T) {
	store, _ := catalogFixture(t)
	store.analysisEnabled = true
	report := committedReport(t, 1, "shared-commit")
	first := saveReport(t, store, "repo", report)
	second := saveReport(t, store, "other", report)
	job, err := store.AnalysisStatus(context.Background(), "other", second.ID)
	if err != nil || job.Status != "queued" || job.Commit != report.Snapshot.Commit {
		t.Fatalf("commit admission leaked across repositories: %+v, %v", job, err)
	}
	assertJobStatus(t, store, first.ID, "queued")
	assertTableCount(t, store, "developa_analysis_commits", 2)
}

func TestIntegrationAnalysisCommitMigrationSupersedesUnsafeAutomaticWork(t *testing.T) {
	store, _ := unmigratedFixture(t)
	installLegacyJobs(t, store)
	clean := saveLegacyJobSnapshot(t, store, committedReport(t, 1, "legacy-clean-commit"))
	report := committedReport(t, 1, "legacy-dirty-commit")
	report.Snapshot.Dirty = true
	dirty := saveLegacyJobSnapshot(t, store, report)
	_, err := store.pool.Exec(context.Background(), `INSERT INTO developa_analysis_jobs
		(repository_id,snapshot_id,id,status,automatic,total_symbols,execution_id,actor,trigger,trace_id,trace_parent)
		SELECT repository_id,id,id,'queued',true,(metadata->>'symbol_count')::integer,'legacy-execution','system','feature_auto','',''
		FROM developa_snapshots WHERE repository_id='repo'`)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Migrate(context.Background()); err != nil {
		t.Fatal(err)
	}
	assertJobStatus(t, store, clean.ID, "queued")
	assertJobStatus(t, store, dirty.ID, "superseded")
	assertTableCount(t, store, "developa_analysis_commits", 1)
	assertTableCount(t, store, "developa_audit_events", 3)
	assertTableCount(t, store, "developa_audit_outbox", 3)
}

func saveLegacyJobSnapshot(t *testing.T, store *Store, report application.Report) domain.Snapshot {
	t.Helper()
	snapshot := saveLegacyReport(t, store, report)
	counts := map[string]int{"files": snapshot.FileCount, "symbols": snapshot.SymbolCount, "changes": snapshot.ChangeCount}
	if err := appendAudit(context.Background(), store.pool, "repo", &snapshot.ID, testExecution(), "completed", counts); err != nil {
		t.Fatal(err)
	}
	return snapshot
}

func installLegacyJobs(t *testing.T, store *Store) {
	t.Helper()
	installLegacyCatalog(t, store)
	tx, err := store.pool.Begin(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer rollback(tx)
	for _, migration := range catalogMigrations[1:4] {
		if err := applyMigration(context.Background(), tx, migration); err != nil {
			t.Fatal(err)
		}
	}
	if err := tx.Commit(context.Background()); err != nil {
		t.Fatal(err)
	}
}
