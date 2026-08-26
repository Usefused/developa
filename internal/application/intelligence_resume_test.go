package application

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"developa/internal/domain"
)

func TestDiscoverContinuesWithoutRepeatingPrefix(t *testing.T) {
	store := &intelligenceTestStore{symbols: intelligenceFacts(5)}
	service := fixtureIntelligence(t, store, featureTestModel(), IntelligenceConfig{BatchSize: 2, MaxModelCalls: 1})
	first, err := service.Discover(context.Background(), "snapshot")
	if err != nil {
		t.Fatal(err)
	}
	second, err := service.Discover(context.Background(), "snapshot")
	if err != nil {
		t.Fatal(err)
	}
	if second.ParentRunID != first.ID || second.AnalyzedSymbols != 4 || second.FeatureCount != 2 {
		t.Fatalf("continuation lost progress: %+v", second)
	}
	third, err := service.Discover(context.Background(), "snapshot")
	if err != nil {
		t.Fatal(err)
	}
	assertCompletedContinuation(t, store, third, second.ID)
}

func assertCompletedContinuation(t *testing.T, store *intelligenceTestStore, run domain.FeatureRun, parent string) {
	t.Helper()
	if run.ParentRunID != parent || run.Status != "completed" || run.AnalyzedSymbols != 5 || run.FeatureCount != 3 {
		t.Fatalf("coverage: %+v", run)
	}
	if len(store.features) != 3 {
		t.Fatal("prior feature evidence was discarded")
	}
	for index, page := range store.pages {
		if page.Offset != index*2 {
			t.Fatal("continuation repeated prefix")
		}
	}
}

func TestDiscoveryPublishesCompletedBatchesBeforeTimeBudget(t *testing.T) {
	store := &intelligenceTestStore{symbols: intelligenceFacts(5)}
	fast := featureTestModel()
	model := &intelligenceTestModel{}
	model.generate = func(ctx context.Context, system, prompt string, schema json.RawMessage) (json.RawMessage, error) {
		if model.calls.Load() == 1 {
			return fast.generate(ctx, system, prompt, schema)
		}
		<-ctx.Done()
		return nil, ctx.Err()
	}
	service := fixtureIntelligence(t, store, model, IntelligenceConfig{BatchSize: 2, Timeout: time.Second})
	run, err := service.Discover(context.Background(), "snapshot")
	if err != nil {
		t.Fatal(err)
	}
	if run.Status != "partial" || run.AnalyzedSymbols != 2 || run.FeatureCount != 1 {
		t.Fatalf("completed batch was lost: %+v", run)
	}
	if !strings.Contains(strings.Join(run.Limitations, " "), "time budget") {
		t.Fatal("time-budget limit not disclosed")
	}
	assertIntelligenceAudit(t, store, "completed")
}

func TestCanceledDiscoveryDoesNotPublishCompletedBatches(t *testing.T) {
	store := &intelligenceTestStore{symbols: intelligenceFacts(5)}
	fast := featureTestModel()
	model := &intelligenceTestModel{}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	model.generate = func(ctx context.Context, system, prompt string, schema json.RawMessage) (json.RawMessage, error) {
		if model.calls.Load() == 1 {
			return fast.generate(ctx, system, prompt, schema)
		}
		cancel()
		<-ctx.Done()
		return nil, ctx.Err()
	}
	service := fixtureIntelligence(t, store, model, IntelligenceConfig{BatchSize: 2})
	if _, err := service.Discover(ctx, "snapshot"); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation was converted to partial success: %v", err)
	}
	if store.run.ID != "" {
		t.Fatal("caller cancellation published partial state")
	}
	assertIntelligenceAudit(t, store, "canceled")
}

func TestContinuationRejectsChangedDigest(t *testing.T) {
	store := &intelligenceTestStore{symbols: intelligenceFacts(3)}
	firstModel := featureTestModel()
	firstModel.identity = "local-fixture@sha256:" + strings.Repeat("a", 64)
	service := fixtureIntelligence(t, store, firstModel, IntelligenceConfig{BatchSize: 1, MaxModelCalls: 1})
	first, err := service.Discover(context.Background(), "snapshot")
	if err != nil {
		t.Fatal(err)
	}
	secondModel := featureTestModel()
	generate := secondModel.generate
	secondModel.generate = func(ctx context.Context, system, prompt string, schema json.RawMessage) (json.RawMessage, error) {
		secondModel.identity = "local-fixture@sha256:" + strings.Repeat("b", 64)
		return generate(ctx, system, prompt, schema)
	}
	secondService := fixtureIntelligence(t, store, secondModel, IntelligenceConfig{BatchSize: 1, MaxModelCalls: 1})
	if _, err := secondService.Discover(context.Background(), "snapshot"); !errors.Is(err, domain.ErrInvalidInput) {
		t.Fatalf("model versions were mixed: %v", err)
	}
	if store.run.ID != first.ID {
		t.Fatal("model mismatch replaced prior run")
	}
}

func TestDifferentConfiguredModelStartsFresh(t *testing.T) {
	store := &intelligenceTestStore{symbols: intelligenceFacts(3)}
	first := fixtureIntelligence(t, store, featureTestModel(), IntelligenceConfig{BatchSize: 1, MaxModelCalls: 1})
	if _, err := first.Discover(context.Background(), "snapshot"); err != nil {
		t.Fatal(err)
	}
	model := featureTestModel()
	model.identity = "other-local-model"
	second := fixtureIntelligence(t, store, model, IntelligenceConfig{BatchSize: 1, MaxModelCalls: 1})
	run, err := second.Discover(context.Background(), "snapshot")
	if err != nil {
		t.Fatal(err)
	}
	if run.ParentRunID != "" || run.AnalyzedSymbols != 1 || len(store.features) != 1 {
		t.Fatal("different model inherited previous feature claims")
	}
}

func TestContinuationPreservesEarlierTruncationLimitations(t *testing.T) {
	store := &intelligenceTestStore{symbols: intelligenceFacts(2)}
	store.symbols[0].Symbol.SourceTruncated = true
	service := fixtureIntelligence(t, store, featureTestModel(), IntelligenceConfig{BatchSize: 1, MaxModelCalls: 1})
	if _, err := service.Discover(context.Background(), "snapshot"); err != nil {
		t.Fatal(err)
	}
	run, err := service.Discover(context.Background(), "snapshot")
	if err != nil {
		t.Fatal(err)
	}
	if run.AnalyzedSymbols != 2 || run.Status != "partial" {
		t.Fatal("continuation erased earlier incomplete source evidence")
	}
	if !strings.Contains(strings.Join(run.Limitations, " "), featureTruncationLimitation) {
		t.Fatal("prior source limitation was lost")
	}
}

func TestAuditAdmissionPreservesDeadline(t *testing.T) {
	store := &intelligenceTestStore{symbols: intelligenceFacts(1), auditBlock: true}
	service := fixtureIntelligence(t, store, featureTestModel(), IntelligenceConfig{Timeout: 20 * time.Millisecond})
	if _, err := service.Discover(context.Background(), "snapshot"); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("audit deadline was masked: %v", err)
	}
}

func TestOversizedPromptRecordStillAdvancesCoverage(t *testing.T) {
	store := &intelligenceTestStore{symbols: intelligenceFacts(2)}
	store.symbols[0].Symbol.Name = strings.Repeat("LongIdentifier", 100)
	store.symbols[0].Path = strings.Repeat("long-directory/", 100) + "file.go"
	store.symbols[0].Symbol.Source = strings.Repeat("\n\"\t", 2730)
	evidence, err := boundedEvidence(store.symbols, 1024)
	if err != nil {
		t.Fatal(err)
	}
	if len(evidence.Facts) == 0 || !evidence.Truncated || len(evidence.JSON) > 1024 {
		t.Fatal("oversized record blocked progress or escaped budget")
	}
	service := fixtureIntelligence(t, store, featureTestModel(), IntelligenceConfig{MaxContextBytes: 1024})
	run, err := service.Discover(context.Background(), "snapshot")
	if err != nil {
		t.Fatal(err)
	}
	if run.AnalyzedSymbols != 2 || run.Status != "partial" {
		t.Fatalf("later records never analyzed: %+v", run)
	}
	if store.features[0].Evidence[0].Path != store.symbols[0].Path {
		t.Fatal("prompt clipping altered canonical citation")
	}
}

func cloudFeatureTestModel(revision string) *intelligenceTestModel {
	model := featureTestModel()
	model.identity = "shared-model@cloud:unverified"
	generate := model.generate
	model.generate = func(ctx context.Context, system, prompt string, schema json.RawMessage) (json.RawMessage, error) {
		model.identity = "shared-model@cloud:" + revision
		return generate(ctx, system, prompt, schema)
	}
	return model
}

func TestCloudContinuationRetainsProviderRevisionAcrossClients(t *testing.T) {
	store := &intelligenceTestStore{symbols: intelligenceFacts(3)}
	cfg := IntelligenceConfig{BatchSize: 1, MaxModelCalls: 1}
	first := fixtureIntelligence(t, store, cloudFeatureTestModel("aabbccddeeff"), cfg)
	previous, err := first.Discover(context.Background(), "snapshot")
	if err != nil {
		t.Fatal(err)
	}
	resumed := fixtureIntelligence(t, store, cloudFeatureTestModel("aabbccddeeff"), cfg)
	run, err := resumed.Discover(context.Background(), "snapshot")
	if err != nil {
		t.Fatal(err)
	}
	if run.ParentRunID != previous.ID || run.AnalyzedSymbols != 2 || run.Model != "shared-model@cloud:aabbccddeeff" {
		t.Fatalf("cloud continuation lost its provider identity or progress: %+v", run)
	}
	assertCloudProvenance(t, run.Limitations)
}

func TestCloudContinuationRejectsChangedProviderRevision(t *testing.T) {
	store := &intelligenceTestStore{symbols: intelligenceFacts(3)}
	cfg := IntelligenceConfig{BatchSize: 1, MaxModelCalls: 1}
	first := fixtureIntelligence(t, store, cloudFeatureTestModel("aabbccddeeff"), cfg)
	previous, err := first.Discover(context.Background(), "snapshot")
	if err != nil {
		t.Fatal(err)
	}
	changed := fixtureIntelligence(t, store, cloudFeatureTestModel("112233445566"), cfg)
	if _, err := changed.Discover(context.Background(), "snapshot"); !errors.Is(err, domain.ErrInvalidInput) {
		t.Fatalf("changed cloud revision continued prior claims: %v", err)
	}
	if store.run.ID != previous.ID || len(store.features) != 1 {
		t.Fatal("revision mismatch replaced the prior feature run")
	}
}

func TestContinuationNeverCrossesLocalAndCloudBackends(t *testing.T) {
	local := "shared-model@sha256:" + strings.Repeat("a", 64)
	cloud := "shared-model@cloud:aabbccddeeff"
	cases := []struct{ previous, configured, resolved string }{
		{local, "shared-model@cloud:unverified", cloud},
		{cloud, "shared-model", local},
	}
	for _, tc := range cases {
		t.Run(tc.configured, func(t *testing.T) {
			assertBackendStartsFresh(t, tc.previous, tc.configured, tc.resolved)
		})
	}
}

func assertBackendStartsFresh(t *testing.T, previous, configured, resolved string) {
	t.Helper()
	store := &intelligenceTestStore{symbols: intelligenceFacts(3)}
	firstModel := featureTestModel()
	firstModel.identity = previous
	cfg := IntelligenceConfig{BatchSize: 1, MaxModelCalls: 1}
	first := fixtureIntelligence(t, store, firstModel, cfg)
	if _, err := first.Discover(context.Background(), "snapshot"); err != nil {
		t.Fatal(err)
	}
	model := changingIdentityModel(configured, resolved)
	second := fixtureIntelligence(t, store, model, cfg)
	run, err := second.Discover(context.Background(), "snapshot")
	if err != nil {
		t.Fatal(err)
	}
	if run.ParentRunID != "" || run.AnalyzedSymbols != 1 || len(store.features) != 1 || run.Model != resolved {
		t.Fatalf("different inference backends inherited claims: %+v", run)
	}
}

func changingIdentityModel(configured, resolved string) *intelligenceTestModel {
	model := featureTestModel()
	model.identity = configured
	generate := model.generate
	model.generate = func(ctx context.Context, system, prompt string, schema json.RawMessage) (json.RawMessage, error) {
		model.identity = resolved
		return generate(ctx, system, prompt, schema)
	}
	return model
}

func TestFreshFeatureRunPinsItsFirstGeneratedModel(t *testing.T) {
	for _, prefix := range []string{"shared-model@sha256:", "shared-model@cloud:"} {
		t.Run(prefix, func(t *testing.T) {
			assertFreshRunRejectsModelChange(t, prefix)
		})
	}
}

func assertFreshRunRejectsModelChange(t *testing.T, prefix string) {
	t.Helper()
	store := &intelligenceTestStore{symbols: intelligenceFacts(2)}
	model := featureTestModel()
	model.identity = prefix + strings.Repeat("a", 64)
	generate := model.generate
	model.generate = func(ctx context.Context, system, prompt string, schema json.RawMessage) (json.RawMessage, error) {
		if model.calls.Load() == 2 {
			model.identity = prefix + strings.Repeat("b", 64)
		}
		return generate(ctx, system, prompt, schema)
	}
	service := fixtureIntelligence(t, store, model, IntelligenceConfig{BatchSize: 1})
	if _, err := service.Discover(context.Background(), "snapshot"); !errors.Is(err, domain.ErrInvalidInput) {
		t.Fatalf("fresh run mixed model revisions: %v", err)
	}
	if store.run.ID != "" {
		t.Fatal("mixed-revision features were published")
	}
}

func TestUnverifiedCloudIdentityCannotResumePriorClaims(t *testing.T) {
	previous := &domain.FeatureRun{Model: "shared-model@cloud:unverified", AnalyzedSymbols: 1, TotalSymbols: 2}
	if canResumeFeatures(previous, "shared-model@cloud:unverified") {
		t.Fatal("unverified cloud revision was treated as stable provenance")
	}
	limits := modelLimitations(previous.Model, true)
	if !slices.Contains(limits, cloudUnverifiedLimitation) || slices.Contains(limits, cloudRevisionLimitation) {
		t.Fatal("unverified cloud identity claimed a provider revision")
	}
}

func TestCloudDiscoveryWithoutEvidenceDoesNotClaimTransfer(t *testing.T) {
	model := cloudFeatureTestModel("aabbccddeeff")
	store := &intelligenceTestStore{}
	service := fixtureIntelligence(t, store, model, IntelligenceConfig{})
	run, err := service.Discover(context.Background(), "snapshot")
	if err != nil {
		t.Fatal(err)
	}
	if model.calls.Load() != 0 || !slices.Contains(run.Limitations, cloudNoTransferLimitation) || slices.Contains(run.Limitations, cloudTransferLimitation) {
		t.Fatal("empty feature discovery claimed to transfer source")
	}
}

func assertCloudProvenance(t *testing.T, limitations []string) {
	t.Helper()
	if !slices.Contains(limitations, cloudTransferLimitation) || !slices.Contains(limitations, cloudRevisionLimitation) {
		t.Fatalf("cloud provenance was not disclosed: %+v", limitations)
	}
}
