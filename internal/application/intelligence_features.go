package application

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"

	"developa/internal/domain"
)

var featureSchema = json.RawMessage(`{"type":"object","additionalProperties":false,"required":["features"],"properties":{"features":{"type":"array","maxItems":8,"items":{"type":"object","additionalProperties":false,"required":["title","summary","symbol_ids"],"properties":{"title":{"type":"string","maxLength":160},"summary":{"type":"string","maxLength":2000},"symbol_ids":{"type":"array","minItems":1,"maxItems":16,"items":{"type":"string"}}}}}}}`)

type generatedFeature struct {
	Title     string   `json:"title"`
	Summary   string   `json:"summary"`
	SymbolIDs []string `json:"symbol_ids"`
}
type featureState struct {
	run               domain.FeatureRun
	features          []domain.Feature
	sourceTruncated   bool
	startAnalyzed     int
	inheritedFeatures int
	expectedModel     string
	budgetLimited     bool
	modelCalls        int
	cachedBatches     int
}

func (s *IntelligenceService) Discover(ctx context.Context, snapshotID string) (run domain.FeatureRun, err error) {
	job, err := s.begin(ctx, "discover_features")
	if err != nil {
		return run, err
	}
	defer func() { s.end(job, err) }()
	state, err := s.featureState(job.ctx, snapshotID)
	if err != nil {
		return run, err
	}
	if err = s.discoverBudgeted(job.ctx, &state); err != nil {
		return run, err
	}
	finishFeatureRun(&state)
	execution := job.execution
	execution.Status = "completed"
	if err = s.store.SaveFeatures(job.ctx, s.cfg.RepositoryID, state.run, state.features, execution); err != nil {
		return run, err
	}
	return state.run, nil
}

func (s *IntelligenceService) discoverBatches(ctx context.Context, state *featureState) error {
	for call := 0; call < s.cfg.MaxModelCalls; call++ {
		page, err := s.analysisPage(ctx, state.run.SnapshotID, state.run.AnalyzedSymbols)
		if err != nil {
			return err
		}
		state.run.TotalSymbols = page.Total
		if len(page.Items) == 0 {
			return nil
		}
		used, err := s.discoverBatch(ctx, state, page.Items)
		if err != nil {
			return err
		}
		if used == 0 {
			return nil
		}
		state.run.AnalyzedSymbols += used
		if state.run.AnalyzedSymbols >= page.Total {
			return nil
		}
	}
	return nil
}

func (s *IntelligenceService) analysisPage(ctx context.Context, snapshotID string, offset int) (domain.SymbolPage, error) {
	if reader, ok := s.store.(domain.AnalysisPageReader); ok {
		return reader.AnalysisPage(ctx, s.cfg.RepositoryID, snapshotID, s.cfg.BatchSize, offset)
	}
	return s.store.Symbols(ctx, s.cfg.RepositoryID, snapshotID, domain.Filter{Limit: s.cfg.BatchSize, Offset: offset})
}

func (s *IntelligenceService) discoverBatch(ctx context.Context, state *featureState, items []domain.SymbolDetail) (int, error) {
	context, err := boundedEvidence(items, s.cfg.MaxContextBytes)
	if err != nil {
		return 0, err
	}
	if len(context.Facts) == 0 {
		return 0, nil
	}
	batch, err := s.featureBatchData(ctx, state, context.JSON)
	if err != nil {
		return 0, err
	}
	features, err := validateFeatures(batch.data, context.Facts, state.run.ID, len(state.features))
	if err != nil {
		return 0, err
	}
	identity := batch.identity
	if state.expectedModel != "" && state.expectedModel != identity {
		return 0, domain.ErrInvalidInput
	}
	if err := batch.save(ctx, s.cfg.RepositoryID); err != nil {
		return 0, err
	}
	if batch.cached {
		state.cachedBatches++
		state.run.CachedBatches++
	}
	state.features = append(state.features, features...)
	state.run.Model = identity
	// A fresh run must pin its first successful batch too; otherwise a model
	// change between batches could mix provenance even without a continuation.
	state.expectedModel = state.run.Model
	state.sourceTruncated = state.sourceTruncated || context.Truncated
	return len(context.Facts), nil
}

func validateFeatures(data json.RawMessage, facts []domain.SymbolDetail, runID string, offset int) ([]domain.Feature, error) {
	var response struct {
		Features *[]generatedFeature `json:"features"`
	}
	if err := decodeModel(data, &response); err != nil {
		return nil, err
	}
	if response.Features == nil || len(*response.Features) > 8 {
		return nil, domain.ErrInvalidModelOutput
	}
	features := make([]domain.Feature, 0, len(*response.Features))
	for index, generated := range *response.Features {
		if !boundedText(generated.Title, 160) || !boundedText(generated.Summary, 2000) {
			return nil, domain.ErrInvalidModelOutput
		}
		evidence, err := canonicalEvidence(generated.SymbolIDs, facts, true)
		if err != nil {
			return nil, err
		}
		hash := sha256.Sum256([]byte(fmt.Sprintf("%s:%d:%s", runID, offset+index, generated.Title)))
		features = append(features, domain.Feature{ID: hex.EncodeToString(hash[:]), Title: generated.Title, Summary: generated.Summary, Status: "inferred", Evidence: evidence})
	}
	return features, nil
}

func finishFeatureRun(state *featureState) {
	state.run.FeatureCount = state.inheritedFeatures + len(state.features)
	state.run.Limitations = append(state.run.Limitations, featureModelLimitations(state)...)
	if state.run.AnalyzedSymbols < state.run.TotalSymbols {
		state.run.Status = "partial"
		state.run.Limitations = append(state.run.Limitations, "Symbol coverage is partial; continue generation to examine remaining symbol records.")
	}
	if state.budgetLimited {
		state.run.Limitations = append(state.run.Limitations, "The inference time budget was reached; completed batches were saved and generation can continue.")
	}
	if state.sourceTruncated {
		state.run.Status = "partial"
		state.run.Limitations = append(state.run.Limitations, featureTruncationLimitation)
	}
}
