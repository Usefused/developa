package httptransport

import (
	"context"
	"strings"
	"testing"

	"developa/internal/domain"
)

func TestIntegrationFeatureCacheRebuildsSnapshotsWithoutChat(t *testing.T) {
	fixture, model := newIntelligenceIntegration(t)
	fixture.manager.Start(context.Background())
	first := awaitIntegrationSnapshot(t, fixture, "")
	initial := runIntegratedFeaturePage(t, fixture, first.ID)
	assertIntegratedBatchCounts(t, model, initial, 2, 2, 0)
	before := assertIntegratedFeatureCitation(t, fixture, first.ID, initial)

	rebuilt := assertIntegratedCachedRebuild(t, fixture, model, first.ID, initial.Run.ID)
	if assertIntegratedFeatureCitation(t, fixture, first.ID, rebuilt) != before {
		t.Fatal("same-snapshot cache rebuild changed canonical evidence")
	}
	second := commitUnchangedIntegrationSource(t, fixture, first)
	newRevision := assertIntegratedCachedRebuild(t, fixture, model, second.ID, rebuilt.Run.ID)
	assertIntegratedFeatureCitation(t, fixture, second.ID, newRevision)
	assertIntegratedCacheRepositionsEvidence(t, fixture, model, second, before)
}

func runIntegratedFeaturePage(t *testing.T, fixture *integrationExplorer, snapshot string) domain.FeaturePage {
	t.Helper()
	queueIntegratedAnalysis(t, fixture, snapshot)
	fixture.worker.Start(context.Background())
	awaitIntegratedAnalysis(t, fixture, snapshot, "completed")
	var page domain.FeaturePage
	integrationRead(t, fixture, "/api/snapshots/"+snapshot+"/features", &page)
	if page.Run == nil || page.Run.SnapshotID != snapshot || len(page.Items) != 2 {
		t.Fatalf("feature run was not published for its requested snapshot: %+v", page)
	}
	return page
}

func assertIntegratedBatchCounts(t *testing.T, model *protocolModel, page domain.FeaturePage, totalCalls int32, fresh, cached int) {
	t.Helper()
	if model.calls.Load() != totalCalls || page.Run.ModelCalls != fresh || page.Run.CachedBatches != cached {
		t.Fatalf("incorrect inference/cache batch counts: calls=%d run=%+v", model.calls.Load(), page.Run)
	}
	if page.Run.Model != "fixture:latest@sha256:"+strings.Repeat("c", 64) {
		t.Fatal("cache provenance did not retain the verified model revision")
	}
}

func assertIntegratedCachedRebuild(t *testing.T, fixture *integrationExplorer, model *protocolModel, snapshot, previousRun string) domain.FeaturePage {
	t.Helper()
	calls := model.calls.Load()
	page := runIntegratedFeaturePage(t, fixture, snapshot)
	if page.Run.ID == previousRun {
		t.Fatal("manual rebuild reused a prior run instead of publishing a fresh validated run")
	}
	if model.calls.Load() != calls || page.Run.ModelCalls != 0 || page.Run.CachedBatches != 2 {
		t.Fatalf("unchanged bounded input invoked chat: calls before=%d after=%d run=%+v", calls, model.calls.Load(), page.Run)
	}
	return page
}

func assertIntegratedFeatureCitation(t *testing.T, fixture *integrationExplorer, snapshot string, page domain.FeaturePage) domain.Citation {
	t.Helper()
	var main domain.Citation
	for _, feature := range page.Items {
		citation := assertIntegratedCanonicalCitation(t, fixture, snapshot, feature)
		if citation.Path == "main.go" {
			main = citation
		}
	}
	if main.SymbolID == "" {
		t.Fatal("feature generation lost the main file's evidence")
	}
	return main
}

func assertIntegratedCanonicalCitation(t *testing.T, fixture *integrationExplorer, snapshot string, feature domain.Feature) domain.Citation {
	t.Helper()
	if feature.Status != "inferred" || len(feature.Evidence) != 1 {
		t.Fatalf("cached output lost its grounded claim: %+v", feature)
	}
	citation := feature.Evidence[0]
	detail := readIntegrationSymbol(t, fixture, snapshot, citation.SymbolID)
	if citation.Path != detail.Path || citation.Span != detail.Symbol.Span || citation.Name != detail.Symbol.Name {
		t.Fatal("cached evidence was not canonicalized against the current snapshot")
	}
	return citation
}

func commitUnchangedIntegrationSource(t *testing.T, fixture *integrationExplorer, previous domain.Snapshot) domain.Snapshot {
	t.Helper()
	// A distinct commit exercises cross-snapshot reuse without changing any captured source bytes.
	integrationGit(t, fixture.root, "-c", "user.name=Integration Fixture", "-c", "user.email=fixture@example.invalid", "-c", "commit.gpgsign=false", "commit", "--allow-empty", "-qm", "unchanged source revision")
	next := awaitIntegrationSnapshot(t, fixture, previous.ID)
	if next.Commit == previous.Commit {
		t.Fatal("empty fixture commit did not produce a distinct source revision")
	}
	return next
}

func assertIntegratedCacheRepositionsEvidence(t *testing.T, fixture *integrationExplorer, model *protocolModel, previous domain.Snapshot, before domain.Citation) {
	t.Helper()
	// Leading whitespace moves physical lines without changing the bounded function facts.
	integrationWrite(t, fixture.root, "main.go", "\n\npackage fixture\nfunc Original(name string) string { return name }\n")
	shifted := awaitIntegrationSnapshot(t, fixture, previous.ID)
	page := assertIntegratedCachedRebuild(t, fixture, model, shifted.ID, "")
	after := assertIntegratedFeatureCitation(t, fixture, shifted.ID, page)
	if after.SymbolID != before.SymbolID || after.Span.Start.Line != before.Span.Start.Line+2 {
		t.Fatalf("cached evidence retained stale physical positions: before=%+v after=%+v", before, after)
	}
	old := readIntegrationSymbol(t, fixture, previous.ID, before.SymbolID)
	if old.Symbol.Span != before.Span {
		t.Fatal("cache publication changed evidence in an earlier snapshot")
	}
}

func TestIntegrationFeatureCacheDoesNotReuseChangedSource(t *testing.T) {
	fixture, model := newIntelligenceIntegration(t)
	fixture.manager.Start(context.Background())
	first := awaitIntegrationSnapshot(t, fixture, "")
	initial := runIntegratedFeaturePage(t, fixture, first.ID)
	assertIntegratedBatchCounts(t, model, initial, 2, 2, 0)
	integrationWrite(t, fixture.root, "main.go", "package fixture\nfunc Original(name string) string { return name + \" updated\" }\n")
	changed := awaitIntegrationSnapshot(t, fixture, first.ID)
	page := runIntegratedFeaturePage(t, fixture, changed.ID)
	assertIntegratedBatchCounts(t, model, page, 3, 1, 1)
	assertIntegratedFeatureCitation(t, fixture, changed.ID, page)
}

func TestIntegrationFeatureCacheRequiresVerifiedProviderRevision(t *testing.T) {
	fixture, model := newIntelligenceIntegration(t)
	fixture.manager.Start(context.Background())
	snapshot := awaitIntegrationSnapshot(t, fixture, "")
	initial := runIntegratedFeaturePage(t, fixture, snapshot.ID)
	assertIntegratedBatchCounts(t, model, initial, 2, 2, 0)
	model.changedRevision.Store(true)
	queueIntegratedAnalysis(t, fixture, snapshot.ID)
	job := awaitIntegratedAnalysis(t, fixture, snapshot.ID, "failed")
	if job.ErrorCode != "model_unavailable" || model.calls.Load() != 2 {
		t.Fatalf("changed pinned revision was not rejected before cache reuse or inference: %+v", job)
	}
	var page domain.FeaturePage
	integrationRead(t, fixture, "/api/snapshots/"+snapshot.ID+"/features", &page)
	if page.Run == nil || page.Run.ID != initial.Run.ID {
		t.Fatal("unverified provider revision replaced previously published evidence")
	}
}
