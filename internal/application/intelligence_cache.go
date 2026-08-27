package application

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"

	"developa/internal/domain"
	"go.opentelemetry.io/otel/trace"
)

const featureTask = `Identify product capabilities evidenced by this code batch. A capability is behavior that a user, operator, API client, or coding agent can rely on; it is not merely a function name or an implementation detail. Consolidate related declarations into the smallest useful set of capabilities and cite every supporting symbol needed for the claim. Summaries must explain what the capability enables, the implementation path visible in the evidence, and material boundaries or failure behavior. Return zero features when the batch contains only generic helpers or insufficient evidence. Never promote names or comments over contradictory implementation. DATA:
`

type modelResolver interface {
	ResolveModel(context.Context) (string, error)
}

type modelCachePolicy interface {
	CachePolicy() string
}

type modelResponse struct {
	data     json.RawMessage
	identity string
	cache    domain.AnalysisCacheStore
	key      string
	cached   bool
}

func (s *IntelligenceService) featureBatchData(ctx context.Context, state *featureState, facts json.RawMessage) (modelResponse, error) {
	batch, err := s.inferenceCache(ctx, "features-v2", featureTask, featureSchema, facts)
	if err != nil {
		return batch, err
	}
	if batch.identity != "" && state.expectedModel != "" && batch.identity != state.expectedModel {
		return batch, domain.ErrInvalidInput
	}
	if err := batch.lookup(ctx, s.cfg.RepositoryID); err != nil || batch.cached {
		return batch, err
	}
	state.modelCalls++
	state.run.ModelCalls++
	err = batch.generate(ctx, s.model, featureTask, featureSchema, facts)
	return batch, err
}

func (s *IntelligenceService) inferenceCache(ctx context.Context, purpose, task string, schema, input json.RawMessage) (modelResponse, error) {
	cache, stored := s.store.(domain.AnalysisCacheStore)
	resolver, resolvable := s.model.(modelResolver)
	if !stored || !resolvable {
		return modelResponse{}, nil
	}
	identity, err := resolver.ResolveModel(ctx)
	if err != nil {
		return modelResponse{}, err
	}
	if !cacheableModel(identity) {
		return modelResponse{}, nil
	}
	key := inferenceCacheKey(s.cfg.RepositoryID, identity, s.inferencePolicy(), purpose, task, schema, input)
	return modelResponse{identity: identity, cache: cache, key: key}, nil
}

func (batch *modelResponse) lookup(ctx context.Context, repositoryID string) error {
	if batch.cache == nil {
		return nil
	}
	data, err := batch.cache.CachedAnalysis(ctx, repositoryID, batch.key)
	if errors.Is(err, domain.ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	batch.data, batch.cached = data, true
	trace.SpanFromContext(ctx).AddEvent("analysis.cache_hit")
	return nil
}

func (batch modelResponse) save(ctx context.Context, repositoryID string) error {
	if batch.cache == nil || batch.cached {
		return nil
	}
	// Cache writes do not replace each caller's audited, snapshot-pinned result publication.
	return batch.cache.CacheAnalysis(ctx, repositoryID, batch.key, batch.identity, batch.data)
}

func inferenceCacheKey(repositoryID, model, policy, purpose, task string, schema, data json.RawMessage) string {
	// Framed strings preserve exact prompt bytes without ambiguous concatenation. Line spans are
	// deliberately absent from model facts and are rebound from the current snapshot on every hit.
	input, _ := json.Marshal([]string{purpose, repositoryID, model, policy, groundingInstructions, string(schema), task, string(data)})
	hash := sha256.Sum256(input)
	return hex.EncodeToString(hash[:])
}

func (batch *modelResponse) generate(ctx context.Context, model StructuredModel, task string, schema, input json.RawMessage) error {
	data, err := model.Generate(ctx, groundingInstructions, task+string(input), schema)
	if err != nil {
		return err
	}
	identity := model.Model()
	if batch.identity != "" && batch.identity != identity {
		return domain.ErrInvalidInput
	}
	batch.data, batch.identity = data, identity
	return nil
}

func (s *IntelligenceService) inferencePolicy() string {
	if policy, ok := s.model.(modelCachePolicy); ok {
		return policy.CachePolicy()
	}
	return "structured-model-v1"
}

func cacheableModel(model string) bool {
	identity := parseModelIdentity(model)
	if identity.name == "" || len(model) > 200 || !identity.hasRevision() {
		return false
	}
	revision, err := hex.DecodeString(identity.revision)
	if err != nil {
		return false
	}
	if identity.backend == "cloud" {
		return len(revision) >= 6 && len(revision) <= 32
	}
	return len(revision) == 32
}
