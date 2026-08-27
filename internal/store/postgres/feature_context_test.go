package postgres

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"developa/internal/application"
	"developa/internal/domain"
)

func TestIntegrationFeatureContextBundleHasConstantQueryBudget(t *testing.T) {
	store, counter := catalogFixture(t)
	report := catalogReport(t, 100, "feature-context-bundle")
	snapshot := saveReport(t, store, "repo", report)
	for _, count := range []int{1, 10, 16} {
		feature := featureFixture(1, report.Index.Files[0].Symbols[0].ID)[0]
		feature.Evidence = nil
		for _, file := range report.Index.Files[:count] {
			feature.Evidence = append(feature.Evidence, domain.Citation{SymbolID: file.Symbols[0].ID})
		}
		saveFeatureFixture(t, store, snapshot, fmt.Sprintf("bundle-run-%d", count), []domain.Feature{feature})
		counter.Store(0)
		bundle, err := application.NewFeatureContexts(store, "repo").FeatureContext(context.Background(), snapshot.ID, feature.ID, domain.FeatureContextOptions{SourceLimit: 5})
		if err != nil || counter.Load() != 3 {
			t.Fatalf("feature bundle query budget changed with %d evidence records: queries=%d err=%v", count, counter.Load(), err)
		}
		if bundle.Source.Total != count || len(bundle.Source.Items) != min(5, count) || bundle.Flow.Options.FeatureID != feature.ID {
			t.Fatal("feature bundle lost canonical evidence or feature flow scope")
		}
	}
}

func TestIntegrationFeatureContextCanonicalAndConstantQueryBudget(t *testing.T) {
	store, counter := catalogFixture(t)
	report := catalogReport(t, 100, "feature-context")
	snapshot := saveReport(t, store, "repo", report)
	for _, count := range []int{1, 10, 16} {
		feature := featureFixture(1, report.Index.Files[0].Symbols[0].ID)[0]
		feature.Evidence = nil
		for _, file := range report.Index.Files[:count] {
			feature.Evidence = append(feature.Evidence, domain.Citation{SymbolID: file.Symbols[0].ID, Name: "spoofed", Path: "spoofed"})
		}
		saveFeatureFixture(t, store, snapshot, fmt.Sprintf("context-run-%d", count), []domain.Feature{feature})
		counter.Store(0)
		pack, err := store.FeatureContext(context.Background(), "repo", snapshot.ID, feature.ID, 5)
		if err != nil || counter.Load() != 1 {
			t.Fatalf("feature context query budget failed: %d, %v", counter.Load(), err)
		}
		assertFeatureContextEvidence(t, pack, count)
	}
}

func assertFeatureContextEvidence(t *testing.T, pack domain.ContextPack, count int) {
	t.Helper()
	if pack.Total != count || len(pack.Items) != min(5, count) || pack.Truncated != (count > 5) {
		t.Fatalf("feature context count or truncation wrong: %+v", pack)
	}
	first := pack.Items[0]
	if first.Path != "file000.go" || first.Symbol.Name != "Run000" || !strings.Contains(first.Symbol.Source, "func Run000") {
		t.Fatal("feature context did not use canonical indexed source")
	}
}

func TestIntegrationFeatureContextIsPinnedToSnapshotAndGeneration(t *testing.T) {
	store, _ := catalogFixture(t)
	report := catalogReport(t, 1, "feature-context-old")
	report.Index.Files[0].Symbols[0].Source = "old-source"
	old := saveReport(t, store, "repo", report)
	features := featureFixture(1, report.Index.Files[0].Symbols[0].ID)
	saveFeatureFixture(t, store, old, "old-generation", features)
	report.Snapshot.Fingerprint = fingerprint("feature-context-new")
	report.Index.Files[0].Symbols[0].Source = "new-source"
	current := saveReport(t, store, "repo", report)
	saveFeatureFixture(t, store, current, "current-generation", features)
	pack, err := store.FeatureContext(context.Background(), "repo", old.ID, features[0].ID, 20)
	if err != nil || len(pack.Items) != 1 || pack.Items[0].Symbol.Source != "old-source" {
		t.Fatal("feature context crossed the pinned snapshot")
	}
	assertMissingFeatureContext(t, store, "other", old.ID, features[0].ID)
	assertMissingFeatureContext(t, store, "repo", fingerprint("unknown-snapshot"), features[0].ID)
	previousID := features[0].ID
	features[0].ID = fingerprint("replacement-feature")
	saveFeatureFixture(t, store, old, "replacement-generation", features)
	assertMissingFeatureContext(t, store, "repo", old.ID, previousID)
}

func TestIntegrationFeatureContextDistinguishesEmptyEvidenceFromMissing(t *testing.T) {
	store, _ := catalogFixture(t)
	report := catalogReport(t, 1, "empty-evidence-context")
	snapshot := saveReport(t, store, "repo", report)
	features := featureFixture(1, report.Index.Files[0].Symbols[0].ID)
	saveFeatureFixture(t, store, snapshot, "empty-evidence-run", features)
	_, err := store.pool.Exec(context.Background(), `DELETE FROM developa_feature_evidence
		WHERE repository_id='repo' AND snapshot_id=$1 AND feature_id=$2`, snapshot.ID, features[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	pack, err := store.FeatureContext(context.Background(), "repo", snapshot.ID, features[0].ID, 20)
	if err != nil || pack.Total != 0 || pack.Items == nil || pack.Truncated {
		t.Fatal("existing feature without evidence was confused with missing feature")
	}
	assertMissingFeatureContext(t, store, "repo", snapshot.ID, fingerprint("missing-feature"))
}

func TestIntegrationFeatureContextRejectsUnboundedLimits(t *testing.T) {
	store, counter := catalogFixture(t)
	counter.Store(0)
	for _, limit := range []int{-1, 0, 21} {
		_, err := store.FeatureContext(context.Background(), "repo", fingerprint("snapshot"), fingerprint("feature"), limit)
		if !errors.Is(err, domain.ErrInvalidInput) {
			t.Fatal("unbounded feature evidence was accepted")
		}
	}
	if counter.Load() != 0 {
		t.Fatal("invalid limits reached the database")
	}
}

func assertMissingFeatureContext(t *testing.T, store *Store, repositoryID, snapshotID, featureID string) {
	t.Helper()
	_, err := store.FeatureContext(context.Background(), repositoryID, snapshotID, featureID, 20)
	if !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("missing scoped feature context returned %v", err)
	}
}
