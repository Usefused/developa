package postgres

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"developa/internal/domain"
)

func TestIntegrationFeatureEvidenceIsCanonicalAndScoped(t *testing.T) {
	store, _ := catalogFixture(t)
	report := catalogReport(t, 1, "features")
	snapshot := saveReport(t, store, "repo", report)
	features := featureFixture(1, report.Index.Files[0].Symbols[0].ID)
	saveFeatureFixture(t, store, snapshot, "run-one", features)
	feature, err := store.Feature(context.Background(), "repo", snapshot.ID, features[0].ID)
	if err != nil || len(feature.Evidence) != 1 || feature.Evidence[0].Path != "file000.go" || feature.Evidence[0].Name != "Run000" {
		t.Fatalf("feature citation was not canonicalized: %+v, %v", feature, err)
	}
	_, err = store.Feature(context.Background(), "other", snapshot.ID, features[0].ID)
	if !errors.Is(err, domain.ErrNotFound) {
		t.Fatal("feature detail leaked across repository scope")
	}
	assertFeatureLatestRun(t, store, snapshot, features)
}

func assertFeatureLatestRun(t *testing.T, store *Store, snapshot domain.Snapshot, previous []domain.Feature) {
	t.Helper()
	features := featureFixture(1, previous[0].Evidence[0].SymbolID)
	features[0].ID = fingerprint("new-feature")
	saveFeatureFixture(t, store, snapshot, "run-two", features)
	page, err := store.Features(context.Background(), "repo", snapshot.ID, domain.Filter{})
	if err != nil || page.Run == nil || page.Run.ID != "run-two" || page.Total != 1 {
		t.Fatalf("latest generation was not published: %+v, %v", page, err)
	}
	_, err = store.Feature(context.Background(), "repo", snapshot.ID, previous[0].ID)
	if !errors.Is(err, domain.ErrNotFound) {
		t.Fatal("feature detail did not stay pinned to latest generation")
	}
}

func TestIntegrationFeaturesKeepFixedQueryBudget(t *testing.T) {
	store, counter := catalogFixture(t)
	report := catalogReport(t, 1, "feature-budget")
	snapshot := saveReport(t, store, "repo", report)
	var previous int64
	for _, count := range []int{1, 10, 100} {
		features := featureFixture(count, report.Index.Files[0].Symbols[0].ID)
		counter.Store(0)
		saveFeatureFixture(t, store, snapshot, fmt.Sprintf("run-%d", count), features)
		queries := counter.Load()
		if queries > 10 || (previous > 0 && previous != queries) {
			t.Fatalf("feature write query count grew: %d -> %d", previous, queries)
		}
		previous = queries
		assertFeaturePageBudget(t, store, counter, snapshot, count)
	}
}

func assertFeaturePageBudget(t *testing.T, store *Store, counter *queryCounter, snapshot domain.Snapshot, total int) {
	t.Helper()
	counter.Store(0)
	page, err := store.Features(context.Background(), "repo", snapshot.ID, domain.Filter{Query: "feature", Limit: 3, Offset: 1000})
	if err != nil || page.Total != total || len(page.Items) != 0 || counter.Load() != 1 {
		t.Fatalf("feature page/budget failed: %+v, %v, queries=%d", page, err, counter.Load())
	}
	page, err = store.Features(context.Background(), "repo", snapshot.ID, domain.Filter{Query: "000", Limit: 3})
	if err != nil || page.Total != 1 || len(page.Items) != 1 {
		t.Fatalf("feature SQL query filtering failed: %+v, %v", page, err)
	}
}

func TestIntegrationInvalidFeatureEvidencePreservesPreviousRun(t *testing.T) {
	store, _ := catalogFixture(t)
	report := catalogReport(t, 1, "valid-evidence")
	snapshot := saveReport(t, store, "repo", report)
	features := featureFixture(1, report.Index.Files[0].Symbols[0].ID)
	saveFeatureFixture(t, store, snapshot, "valid-run", features)
	features[0].Evidence[0].SymbolID = fingerprint("missing")
	err := store.SaveFeatures(context.Background(), "repo", runFixture(snapshot, "invalid-run"), features, testExecution())
	if !errors.Is(err, domain.ErrInvalidModelOutput) {
		t.Fatalf("missing evidence was not rejected: %v", err)
	}
	page, err := store.Features(context.Background(), "repo", snapshot.ID, domain.Filter{})
	if err != nil || page.Run == nil || page.Run.ID != "valid-run" {
		t.Fatal("failed feature generation replaced the previous run")
	}
	assertTableCount(t, store, "developa_feature_runs", 1)
	assertTableCount(t, store, "developa_features", 1)
	assertTableCount(t, store, "developa_audit_events", 2)
}

func TestIntegrationFeatureEvidenceCannotCrossSnapshots(t *testing.T) {
	store, _ := catalogFixture(t)
	old := saveReport(t, store, "repo", catalogReport(t, 1, "evidence-old"))
	report := catalogReport(t, 2, "evidence-new")
	saveReport(t, store, "repo", report)
	features := featureFixture(1, report.Index.Files[1].Symbols[0].ID)
	err := store.SaveFeatures(context.Background(), "repo", runFixture(old, "cross-snapshot"), features, testExecution())
	if !errors.Is(err, domain.ErrInvalidModelOutput) {
		t.Fatal("feature citation crossed snapshot scope")
	}
	assertTableCount(t, store, "developa_feature_runs", 0)
}

func TestIntegrationIntelligenceAuditFailureRollsBackWrites(t *testing.T) {
	store, _ := catalogFixture(t)
	report := catalogReport(t, 1, "audit-rollback")
	snapshot := saveReport(t, store, "repo", report)
	_, err := store.pool.Exec(context.Background(), `ALTER TABLE developa_audit_events ADD CONSTRAINT reject_intelligence CHECK (execution_id<>'rejected')`)
	if err != nil {
		t.Fatal(err)
	}
	execution := testExecution()
	execution.ID = "rejected"
	features := featureFixture(1, report.Index.Files[0].Symbols[0].ID)
	err = store.SaveFeatures(context.Background(), "repo", runFixture(snapshot, "rejected-run"), features, execution)
	if !errors.Is(err, ErrUnavailable) {
		t.Fatal("feature audit failure did not abort mutation")
	}
	answer := answerFixture(snapshot.ID, report.Index.Files[0].Symbols[0].ID)
	if err := store.SaveAnswer(context.Background(), "repo", answer, execution); !errors.Is(err, ErrUnavailable) {
		t.Fatal("answer audit failure did not abort mutation")
	}
	assertTableCount(t, store, "developa_feature_runs", 0)
	assertTableCount(t, store, "developa_answers", 0)
	assertTableCount(t, store, "developa_audit_events", 1)
	assertTableCount(t, store, "developa_audit_outbox", 1)
}

func TestIntegrationAnswersValidateEvidenceAndDoNotAuditText(t *testing.T) {
	store, _ := catalogFixture(t)
	report := catalogReport(t, 1, "answers")
	snapshot := saveReport(t, store, "repo", report)
	answer := answerFixture(snapshot.ID, report.Index.Files[0].Symbols[0].ID)
	if err := store.SaveAnswer(context.Background(), "repo", answer, testExecution()); err != nil {
		t.Fatal(err)
	}
	assertAnswerPrivacy(t, store)
	answer.ID = "invalid-answer"
	answer.Evidence[0].SymbolID = fingerprint("no-evidence")
	if err := store.SaveAnswer(context.Background(), "repo", answer, testExecution()); !errors.Is(err, domain.ErrInvalidModelOutput) {
		t.Fatal("invalid answer citation was accepted")
	}
	assertTableCount(t, store, "developa_answers", 1)
	assertTableCount(t, store, "developa_answer_evidence", 1)
}

func assertAnswerPrivacy(t *testing.T, store *Store) {
	t.Helper()
	var audit, answer string
	if err := store.pool.QueryRow(context.Background(), `SELECT jsonb_agg(to_jsonb(e))::text FROM developa_audit_events e`).Scan(&audit); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(audit, "private-answer-text") || strings.Contains(audit, "spoofed") {
		t.Fatal("answer text or model citation labels leaked into audit")
	}
	if err := store.pool.QueryRow(context.Background(), `SELECT metadata::text FROM developa_answers`).Scan(&answer); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(answer, "spoofed") {
		t.Fatal("untrusted citation labels persisted in answer metadata")
	}
}

func TestIntegrationSavedAnswerLookupIsOneQueryAndRepositoryScoped(t *testing.T) {
	for _, count := range []int{1, 16} {
		t.Run(fmt.Sprint(count), func(t *testing.T) { assertSavedAnswerLookup(t, count) })
	}
}

func assertSavedAnswerLookup(t *testing.T, count int) {
	t.Helper()
	store, queries := catalogFixture(t)
	report := catalogReport(t, count, "saved-answer")
	snapshot := saveReport(t, store, "repo", report)
	answer := answerFixture(snapshot.ID, report.Index.Files[0].Symbols[0].ID)
	answer.ContextKey = fingerprint("answer-context")
	answer.Evidence = nil
	for _, file := range report.Index.Files {
		answer.Evidence = append(answer.Evidence, domain.Citation{SymbolID: file.Symbols[0].ID})
	}
	if err := store.SaveAnswer(context.Background(), "repo", answer, testExecution()); err != nil {
		t.Fatal(err)
	}
	queries.Store(0)
	saved, err := store.SavedAnswer(context.Background(), "repo", snapshot.ID, answer.ContextKey)
	if err != nil || saved.ID != answer.ID || len(saved.Evidence) != count || queries.Load() != 1 {
		t.Fatalf("saved lookup did not use one complete scoped query: %v", err)
	}
	if saved.GeneratedSnapshotID != snapshot.ID || saved.SnapshotID != snapshot.ID || !saved.Cached {
		t.Fatal("saved lookup lost snapshot provenance")
	}
	assertSavedAnswerMisses(t, store, snapshot.ID, answer.ContextKey)
}

func assertSavedAnswerMisses(t *testing.T, store *Store, snapshot, key string) {
	t.Helper()
	for _, scope := range [][3]string{{"other", snapshot, key}, {"repo", fingerprint("missing"), key}, {"repo", snapshot, fingerprint("changed")}} {
		if _, err := store.SavedAnswer(context.Background(), scope[0], scope[1], scope[2]); !errors.Is(err, domain.ErrNotFound) {
			t.Fatal("unknown source or repository returned an answer")
		}
	}
}

func TestIntegrationFeaturesWithoutGenerationAreEmpty(t *testing.T) {
	store, _ := catalogFixture(t)
	snapshot := saveReport(t, store, "repo", catalogReport(t, 1, "no-features"))
	page, err := store.Features(context.Background(), "repo", snapshot.ID, domain.Filter{})
	if err != nil || page.Run != nil || page.Items == nil || page.Total != 0 {
		t.Fatalf("ungenerated features response invalid: %+v, %v", page, err)
	}
	_, err = store.Features(context.Background(), "repo", fingerprint("missing"), domain.Filter{})
	if !errors.Is(err, domain.ErrNotFound) {
		t.Fatal("unknown snapshot must not look like an empty generation")
	}
}

func TestIntegrationFeaturePageFindsSavedSnapshotWithoutMixingEvidence(t *testing.T) {
	store, counter := catalogFixture(t)
	report := catalogReport(t, 1, "saved-feature-source")
	saved := saveReport(t, store, "repo", report)
	saveFeatureFixture(t, store, saved, "saved-feature-run", featureFixture(1, report.Index.Files[0].Symbols[0].ID))
	current := saveReport(t, store, "repo", catalogReport(t, 1, "new-unreviewed-source"))
	counter.Store(0)
	page, err := store.Features(context.Background(), "repo", current.ID, domain.Filter{})
	if err != nil || counter.Load() != 1 {
		t.Fatalf("saved snapshot discovery must use one SQL read: %v, queries=%d", err, counter.Load())
	}
	if page.SavedSnapshot == nil || page.SavedSnapshot.ID != saved.ID {
		t.Fatalf("saved analysis was not discoverable: %+v", page)
	}
	if page.Run != nil || page.Total != 0 || len(page.Items) != 0 {
		t.Fatal("older analysis was presented as evidence for newer source")
	}
	assertSavedSnapshotHintScope(t, store, saved)
}

func assertSavedSnapshotHintScope(t *testing.T, store *Store, saved domain.Snapshot) {
	t.Helper()
	page, err := store.Features(context.Background(), "repo", saved.ID, domain.Filter{Query: "no-match"})
	if err != nil || page.SavedSnapshot != nil || page.Run == nil || page.Total != 0 {
		t.Fatalf("existing analysis must not suggest fallback for empty search results: %+v, %v", page, err)
	}
	if err := store.EnsureRepository(context.Background(), domain.Repository{ID: "other", Name: "Other"}); err != nil {
		t.Fatal(err)
	}
	other := saveReport(t, store, "other", catalogReport(t, 1, "other-repository"))
	page, err = store.Features(context.Background(), "other", other.ID, domain.Filter{})
	if err != nil || page.SavedSnapshot != nil {
		t.Fatalf("saved analysis hint crossed repository scope: %+v, %v", page, err)
	}
}

func TestIntegrationSavedFeatureHintSelectsNewestAnalyzedSource(t *testing.T) {
	store, counter := catalogFixture(t)
	var latest domain.Snapshot
	for _, name := range []string{"older", "newer"} {
		report := catalogReport(t, 1, name)
		latest = saveReport(t, store, "repo", report)
		saveFeatureFixture(t, store, latest, name, featureFixture(1, report.Index.Files[0].Symbols[0].ID))
	}
	current := saveReport(t, store, "repo", catalogReport(t, 1, "unreviewed"))
	counter.Store(0)
	page, err := store.Features(context.Background(), "repo", current.ID, domain.Filter{Limit: 1, Offset: 100})
	if err != nil || page.SavedSnapshot == nil || page.SavedSnapshot.ID != latest.ID || counter.Load() != 1 {
		t.Fatalf("latest saved source was not selected independently of pagination: %+v, %v", page, err)
	}
}

func featureFixture(count int, symbolID string) []domain.Feature {
	features := make([]domain.Feature, count)
	for i := range features {
		features[i] = domain.Feature{ID: fingerprint(fmt.Sprintf("feature-%d", i)), Title: fmt.Sprintf("Feature %03d", i),
			Summary: "An inferred capability", Status: "inferred", Evidence: []domain.Citation{{SymbolID: symbolID, Path: "spoofed", Name: "spoofed"}}}
	}
	return features
}

func runFixture(snapshot domain.Snapshot, id string) domain.FeatureRun {
	return domain.FeatureRun{ID: id, SnapshotID: snapshot.ID, Model: "local-model", Status: "completed",
		AnalyzedSymbols: snapshot.SymbolCount, TotalSymbols: snapshot.SymbolCount, CreatedAt: time.Now().UTC()}
}

func saveFeatureFixture(t *testing.T, store *Store, snapshot domain.Snapshot, id string, features []domain.Feature) {
	t.Helper()
	if err := store.SaveFeatures(context.Background(), "repo", runFixture(snapshot, id), features, testExecution()); err != nil {
		t.Fatal(err)
	}
}

func answerFixture(snapshotID, symbolID string) domain.Answer {
	return domain.Answer{ID: "answer-one", SnapshotID: snapshotID, Model: "local-model", Text: "private-answer-text",
		Evidence: []domain.Citation{{SymbolID: symbolID, Path: "spoofed", Name: "spoofed"}}, CreatedAt: time.Now().UTC()}
}

func TestIntegrationIntelligenceWritesRejectUnknownSnapshot(t *testing.T) {
	store, _ := catalogFixture(t)
	snapshot := domain.Snapshot{ID: fingerprint("absent")}
	featuresErr := store.SaveFeatures(context.Background(), "repo", runFixture(snapshot, "absent-run"), nil, testExecution())
	answer := answerFixture(snapshot.ID, fingerprint("absent-symbol"))
	answerErr := store.SaveAnswer(context.Background(), "repo", answer, testExecution())
	for _, err := range []error{featuresErr, answerErr} {
		if !errors.Is(err, domain.ErrNotFound) {
			t.Fatalf("unknown snapshot returned %v", err)
		}
	}
	assertTableCount(t, store, "developa_audit_events", 0)
}

func TestIntegrationFeatureResumeAccumulatesImmutableGenerations(t *testing.T) {
	store, _ := catalogFixture(t)
	report := catalogReport(t, 2, "feature-resume")
	snapshot := saveReport(t, store, "repo", report)
	first := continuationRun(snapshot, "first-chunk", "", 2)
	features := featureFixture(2, report.Index.Files[0].Symbols[0].ID)
	assertSaveFeatureRun(t, store, first, features)
	second := continuationRun(snapshot, "second-chunk", first.ID, 4)
	second.FeatureCount = 999
	assertSaveFeatureRun(t, store, second, continuedFeatures(report.Index.Files[1].Symbols[0].ID))
	page, err := store.Features(context.Background(), "repo", snapshot.ID, domain.Filter{})
	if err != nil || page.Run == nil || page.Total != 3 || page.Run.FeatureCount != 3 {
		t.Fatalf("resumed feature counts were not accumulated: %+v, %v", page, err)
	}
	assertResumedFeatureMetadata(t, page.Run, second)
	assertInheritedFeatureAccessible(t, store, snapshot.ID, features[0].ID)
	assertOriginalFeatureRunUnchanged(t, store, snapshot.ID, first.ID)
	assertTableCount(t, store, "developa_features", 5)
}

func assertResumedFeatureMetadata(t *testing.T, actual *domain.FeatureRun, expected domain.FeatureRun) {
	t.Helper()
	if actual.ParentRunID != expected.ParentRunID || actual.AnalyzedSymbols != 4 || actual.Status != "completed" {
		t.Fatalf("resumed coverage/provenance incorrect: %+v", actual)
	}
}

func assertInheritedFeatureAccessible(t *testing.T, store *Store, snapshotID, featureID string) {
	t.Helper()
	feature, err := store.Feature(context.Background(), "repo", snapshotID, featureID)
	if err != nil || len(feature.Evidence) != 1 || feature.Evidence[0].Path != "file000.go" {
		t.Fatalf("inherited feature lost its ID or canonical evidence: %+v, %v", feature, err)
	}
}

func assertOriginalFeatureRunUnchanged(t *testing.T, store *Store, snapshotID, runID string) {
	t.Helper()
	var payload []byte
	err := store.pool.QueryRow(context.Background(), `SELECT metadata FROM developa_feature_runs WHERE repository_id=$1 AND snapshot_id=$2 AND id=$3`, "repo", snapshotID, runID).Scan(&payload)
	if err != nil {
		t.Fatal(err)
	}
	var original domain.FeatureRun
	if err := decodeJSON(payload, &original); err != nil {
		t.Fatal(err)
	}
	if original.FeatureCount != 2 || original.AnalyzedSymbols != 2 || original.Status != "partial" || original.ParentRunID != "" {
		t.Fatal("resuming mutated the parent generation")
	}
}

func TestIntegrationFeatureResumeRejectsStaleParentAndModelChanges(t *testing.T) {
	store, _ := catalogFixture(t)
	report := catalogReport(t, 2, "resume-conflicts")
	snapshot := saveReport(t, store, "repo", report)
	first := continuationRun(snapshot, "resume-parent", "", 2)
	assertSaveFeatureRun(t, store, first, featureFixture(2, report.Index.Files[0].Symbols[0].ID))
	assertInvalidFeatureContinuations(t, store, snapshot, first)
	second := continuationRun(snapshot, "resume-latest", first.ID, 3)
	assertSaveFeatureRun(t, store, second, continuedFeatures(report.Index.Files[1].Symbols[0].ID))
	stale := continuationRun(snapshot, "resume-stale", first.ID, 4)
	err := store.SaveFeatures(context.Background(), "repo", stale, nil, testExecution())
	if !errors.Is(err, domain.ErrBusy) {
		t.Fatalf("stale parent should report a publication conflict, got %v", err)
	}
	page, err := store.Features(context.Background(), "repo", snapshot.ID, domain.Filter{})
	if err != nil || page.Run == nil || page.Run.ID != second.ID {
		t.Fatal("stale continuation changed the latest generation")
	}
	assertTableCount(t, store, "developa_feature_runs", 2)
	assertTableCount(t, store, "developa_features", 5)
}

func assertInvalidFeatureContinuations(t *testing.T, store *Store, snapshot domain.Snapshot, parent domain.FeatureRun) {
	t.Helper()
	changedModel := continuationRun(snapshot, "changed-model", parent.ID, 3)
	changedModel.Model = "different-model-digest"
	rewind := continuationRun(snapshot, "rewind", parent.ID, 1)
	for _, run := range []domain.FeatureRun{changedModel, rewind} {
		if err := store.SaveFeatures(context.Background(), "repo", run, nil, testExecution()); !errors.Is(err, domain.ErrInvalidInput) {
			t.Fatalf("invalid continuation accepted: %v", err)
		}
	}
	invalidEvidence := continuationRun(snapshot, "invalid-continuation-evidence", parent.ID, 3)
	features := continuedFeatures(fingerprint("missing-evidence"))
	if err := store.SaveFeatures(context.Background(), "repo", invalidEvidence, features, testExecution()); !errors.Is(err, domain.ErrInvalidModelOutput) {
		t.Fatal("invalid continuation evidence did not roll back inherited feature copies")
	}
	assertTableCount(t, store, "developa_feature_runs", 1)
	assertTableCount(t, store, "developa_features", 2)
}

func TestIntegrationFeatureResumeRetainsInferenceCounters(t *testing.T) {
	store, _ := catalogFixture(t)
	snapshot := saveReport(t, store, "repo", catalogReport(t, 2, "cache-counters"))
	first := continuationRun(snapshot, "counter-parent", "", 2)
	first.ModelCalls, first.CachedBatches = 1, 1
	assertSaveFeatureRun(t, store, first, nil)
	rewind := continuationRun(snapshot, "counter-rewind", first.ID, 3)
	if err := store.SaveFeatures(context.Background(), "repo", rewind, nil, testExecution()); !errors.Is(err, domain.ErrInvalidInput) {
		t.Fatal("continuation reset cumulative inference counters")
	}
	resume := continuationRun(snapshot, "counter-complete", first.ID, 4)
	resume.ModelCalls, resume.CachedBatches = 1, 2
	assertSaveFeatureRun(t, store, resume, nil)
	page, err := store.Features(context.Background(), "repo", snapshot.ID, domain.Filter{})
	if err != nil || page.Run == nil || page.Run.ModelCalls != 1 || page.Run.CachedBatches != 2 {
		t.Fatal("inference counters did not round trip")
	}
}

func TestIntegrationFeatureResumeQueryBudgetDoesNotGrowWithParent(t *testing.T) {
	store, counter := catalogFixture(t)
	var previous int64
	for _, count := range []int{1, 100, 512} {
		report := catalogReport(t, 1, fmt.Sprintf("resume-budget-%d", count))
		snapshot := saveReport(t, store, "repo", report)
		parent := continuationRun(snapshot, fmt.Sprintf("parent-%d", count), "", 1)
		assertSaveFeatureRun(t, store, parent, featureFixture(count, report.Index.Files[0].Symbols[0].ID))
		resume := continuationRun(snapshot, fmt.Sprintf("resume-%d", count), parent.ID, 2)
		counter.Store(0)
		assertSaveFeatureRun(t, store, resume, continuedFeatures(report.Index.Files[0].Symbols[0].ID))
		queries := counter.Load()
		if queries > 10 || (previous > 0 && queries != previous) {
			t.Fatalf("resume write budget grew with parent size: %d -> %d", previous, queries)
		}
		previous = queries
		assertAccumulatedFeatureCount(t, store, snapshot.ID, count+1)
	}
}

func assertAccumulatedFeatureCount(t *testing.T, store *Store, snapshotID string, count int) {
	t.Helper()
	page, err := store.Features(context.Background(), "repo", snapshotID, domain.Filter{Limit: 1})
	if err != nil || page.Run == nil || page.Run.FeatureCount != count || page.Total != count || len(page.Items) != 1 {
		t.Fatalf("accumulated generation did not exceed chunk cap safely: %+v, %v", page, err)
	}
}

func TestIntegrationFeatureResumeRejectsAlreadyCoveredParent(t *testing.T) {
	store, _ := catalogFixture(t)
	report := catalogReport(t, 1, "already-covered")
	snapshot := saveReport(t, store, "repo", report)
	parent := continuationRun(snapshot, "covered-parent", "", 2)
	assertSaveFeatureRun(t, store, parent, featureFixture(1, report.Index.Files[0].Symbols[0].ID))
	resume := continuationRun(snapshot, "unneeded-resume", parent.ID, 2)
	if err := store.SaveFeatures(context.Background(), "repo", resume, nil, testExecution()); !errors.Is(err, domain.ErrInvalidInput) {
		t.Fatalf("already covered parent must not continue, got %v", err)
	}
	assertTableCount(t, store, "developa_feature_runs", 1)
}

func continuationRun(snapshot domain.Snapshot, id, parent string, analyzed int) domain.FeatureRun {
	run := runFixture(snapshot, id)
	run.ParentRunID, run.AnalyzedSymbols = parent, analyzed
	if analyzed < snapshot.SymbolCount {
		run.Status = "partial"
	}
	return run
}

func continuedFeatures(symbolID string) []domain.Feature {
	features := featureFixture(1, symbolID)
	features[0].ID = fingerprint("continued-feature")
	return features
}

func assertSaveFeatureRun(t *testing.T, store *Store, run domain.FeatureRun, features []domain.Feature) {
	t.Helper()
	if err := store.SaveFeatures(context.Background(), "repo", run, features, testExecution()); err != nil {
		t.Fatal(err)
	}
}

func TestIntegrationCloudAnswerPreservesIdentityAndLimitations(t *testing.T) {
	store, _ := catalogFixture(t)
	report := catalogReport(t, 1, "cloud-answer")
	snapshot := saveReport(t, store, "repo", report)
	answer := answerFixture(snapshot.ID, report.Index.Files[0].Symbols[0].ID)
	answer.Model = strings.Repeat("m", 128) + "@cloud:" + strings.Repeat("a", 64)
	answer.Cached = true
	answer.Limitations = []string{"Code excerpts were sent to the explicitly configured cloud provider.", "Provider identity does not pin model weights."}
	if err := store.SaveAnswer(context.Background(), "repo", answer, testExecution()); err != nil {
		t.Fatal(err)
	}
	var payload []byte
	err := store.pool.QueryRow(context.Background(), `SELECT metadata FROM developa_answers
		WHERE repository_id=$1 AND snapshot_id=$2 AND id=$3`, "repo", snapshot.ID, answer.ID).Scan(&payload)
	if err != nil {
		t.Fatal(err)
	}
	assertCloudAnswerMetadata(t, payload, answer)
}

func assertCloudAnswerMetadata(t *testing.T, payload []byte, expected domain.Answer) {
	t.Helper()
	var actual domain.Answer
	if err := decodeJSON(payload, &actual); err != nil {
		t.Fatal(err)
	}
	if actual.Model != expected.Model || len(actual.Limitations) != len(expected.Limitations) {
		t.Fatal("cloud answer identity or provenance limitations did not persist")
	}
	if actual.Cached != expected.Cached {
		t.Fatal("answer cache provenance did not persist")
	}
	for i, limitation := range expected.Limitations {
		if actual.Limitations[i] != limitation {
			t.Fatal("cloud answer provenance limitation was modified")
		}
	}
}

func TestCloudFeatureParentIdentityMustMatchExactly(t *testing.T) {
	identity := strings.Repeat("m", 128) + "@cloud:" + strings.Repeat("a", 64)
	parent := domain.FeatureRun{Model: identity, TotalSymbols: 2, AnalyzedSymbols: 1}
	next := domain.FeatureRun{Model: identity, TotalSymbols: 2, AnalyzedSymbols: 2}
	if !validModel(identity) || validateFeatureParent(parent, next) != nil {
		t.Fatal("maximum cloud model identity rejected")
	}
	next.Model = strings.Repeat("m", 128) + "@cloud:" + strings.Repeat("b", 64)
	if !errors.Is(validateFeatureParent(parent, next), domain.ErrInvalidInput) {
		t.Fatal("feature continuation silently mixed cloud provider identities")
	}
}
