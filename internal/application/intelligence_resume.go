package application

import (
	"context"
	"errors"
	"slices"
	"time"

	"developa/internal/domain"
)

const featureTruncationLimitation = "Some symbol source, signatures, or documentation was unavailable or truncated."

func (s *IntelligenceService) featureState(ctx context.Context, snapshotID string) (featureState, error) {
	state := featureState{run: domain.FeatureRun{ID: newExecutionID(), SnapshotID: snapshotID, Model: s.model.Model(), Status: "completed", CreatedAt: time.Now().UTC(),
		Limitations: []string{"Features are inferred from code evidence, not proof of runtime behavior.", "Only indexed symbols are analyzed; excluded files and unavailable source are not covered.", "Bounded batches do not establish cross-batch behavior; overlapping feature descriptions may remain."}}}
	page, err := s.store.Features(ctx, s.cfg.RepositoryID, snapshotID, domain.Filter{Limit: 1})
	if err != nil {
		return state, err
	}
	if !canResumeFeatures(page.Run, s.model.Model()) {
		return state, nil
	}
	previous := page.Run
	state.run.ParentRunID = previous.ID
	state.run.AnalyzedSymbols, state.run.TotalSymbols = previous.AnalyzedSymbols, previous.TotalSymbols
	state.startAnalyzed, state.inheritedFeatures = previous.AnalyzedSymbols, previous.FeatureCount
	state.expectedModel, state.run.Model = previous.Model, previous.Model
	state.run.CachedBatches, state.run.ModelCalls = previous.CachedBatches, previous.ModelCalls
	state.sourceTruncated = slices.Contains(previous.Limitations, featureTruncationLimitation)
	return state, nil
}

func canResumeFeatures(previous *domain.FeatureRun, model string) bool {
	if previous == nil || previous.AnalyzedSymbols <= 0 || previous.AnalyzedSymbols >= previous.TotalSymbols {
		return false
	}
	previousIdentity, currentIdentity := parseModelIdentity(previous.Model), parseModelIdentity(model)
	if previousIdentity.backend == "cloud" && !previousIdentity.hasRevision() {
		return false
	}
	return previousIdentity.name == currentIdentity.name && previousIdentity.backend == currentIdentity.backend
}

func (s *IntelligenceService) discoverBudgeted(ctx context.Context, state *featureState) error {
	deadline, _ := ctx.Deadline()
	remaining := time.Until(deadline)
	reserve := min(5*time.Second, remaining/10)
	// Stop inference before the request deadline so completed batches can publish atomically.
	budgetCtx, cancel := context.WithDeadline(ctx, deadline.Add(-reserve))
	defer cancel()
	err := s.discoverBatches(budgetCtx, state)
	if err == nil {
		return ctx.Err()
	}
	if canPublishFeatureProgress(ctx, budgetCtx, state, err) {
		state.budgetLimited = true
		return nil
	}
	return err
}

func canPublishFeatureProgress(ctx, budgetCtx context.Context, state *featureState, err error) bool {
	return errors.Is(err, context.DeadlineExceeded) && errors.Is(budgetCtx.Err(), context.DeadlineExceeded) &&
		ctx.Err() == nil && state.run.AnalyzedSymbols > state.startAnalyzed
}
