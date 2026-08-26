package application

import (
	"context"
	"encoding/json"

	"developa/internal/domain"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

var _ domain.FunctionReviewer = (*IntelligenceService)(nil)

func (s *IntelligenceService) Review(ctx context.Context, snapshotID string, options domain.ReviewOptions) (page domain.ReviewPage, err error) {
	options, err = domain.NormalizeReviewOptions(options)
	if err != nil {
		return page, err
	}
	store, ok := s.store.(domain.ReviewStore)
	if !ok {
		return page, domain.ErrNotConfigured
	}
	job, err := s.begin(ctx, "review_functions")
	if err != nil {
		return page, err
	}
	defer func() { s.end(job, err) }()
	page, err = store.ReviewPage(job.ctx, s.cfg.RepositoryID, snapshotID, options)
	if err != nil {
		return page, err
	}
	reviews, cache, err := s.reviewPage(job.ctx, store, &page)
	if err != nil {
		return domain.ReviewPage{}, err
	}
	execution := job.execution
	execution.Status = "completed"
	if err = store.SaveReviews(job.ctx, s.cfg.RepositoryID, snapshotID, reviews, cache, execution); err != nil {
		return domain.ReviewPage{}, err
	}
	attachReviews(&page, reviews)
	job.span.SetAttributes(attribute.Int("reviews.count", len(reviews)), attribute.Int("reviews.model_calls", page.ModelCalls), attribute.Int("reviews.cached", page.CachedCount))
	return page, nil
}

func (s *IntelligenceService) reviewPage(ctx context.Context, store domain.ReviewStore, page *domain.ReviewPage) ([]domain.FunctionReview, []domain.ReviewCacheEntry, error) {
	inputs, err := reviewInputs(page.Items, s.cfg.MaxContextBytes)
	if err != nil {
		return nil, nil, err
	}
	page.Items = page.Items[:len(inputs)]
	page.Advance(len(inputs))
	if len(inputs) == 0 {
		return []domain.FunctionReview{}, nil, nil
	}
	model, err := s.reviewModel(ctx)
	if err != nil {
		return nil, nil, err
	}
	reviews, pending, err := s.cachedReviewInputs(ctx, store, inputs, model)
	if err != nil {
		return nil, nil, err
	}
	page.CachedCount = len(reviews)
	if len(pending) == 0 {
		return reviews, nil, nil
	}
	page.ModelCalls = 1
	generated, err := s.generateReviews(ctx, pending, model)
	if err != nil {
		return nil, nil, err
	}
	cache := reviewCacheEntries(generated, pending)
	page.Limitations = append(page.Limitations, modelLimitations(s.model.Model(), true)...)
	return append(reviews, generated...), cache, nil
}

func (s *IntelligenceService) reviewModel(ctx context.Context) (string, error) {
	if resolver, ok := s.model.(modelResolver); ok {
		return resolver.ResolveModel(ctx)
	}
	return s.model.Model(), nil
}

func (s *IntelligenceService) cachedReviewInputs(ctx context.Context, store domain.ReviewStore, inputs []reviewEvidence, model string) ([]domain.FunctionReview, []reviewEvidence, error) {
	keys := make([]string, 0, len(inputs))
	for index := range inputs {
		if !cacheableModel(model) {
			break
		}
		inputs[index].key = inferenceCacheKey(s.cfg.RepositoryID, model, s.inferencePolicy(), reviewVersion, reviewTask, reviewSchema, inputs[index].data)
		keys = append(keys, inputs[index].key)
	}
	cached, err := store.CachedReviews(ctx, s.cfg.RepositoryID, keys)
	if err != nil {
		return nil, nil, err
	}
	return readCachedReviews(ctx, inputs, cached, model)
}

func readCachedReviews(ctx context.Context, inputs []reviewEvidence, cached map[string]json.RawMessage, model string) ([]domain.FunctionReview, []reviewEvidence, error) {
	reviews := []domain.FunctionReview{}
	pending := []reviewEvidence{}
	for _, input := range inputs {
		data, ok := cached[input.key]
		if !ok {
			pending = append(pending, input)
			continue
		}
		var generated generatedReview
		if err := decodeModel(data, &generated); err != nil {
			return nil, nil, err
		}
		review, err := validateReview(generated, input, model)
		if err != nil {
			return nil, nil, err
		}
		review.Cached = true
		reviews = append(reviews, review)
	}
	if len(reviews) > 0 {
		trace.SpanFromContext(ctx).AddEvent("reviews.cache_hit")
	}
	return reviews, pending, nil
}

func (s *IntelligenceService) generateReviews(ctx context.Context, inputs []reviewEvidence, model string) ([]domain.FunctionReview, error) {
	data := make([]json.RawMessage, 0, len(inputs))
	for _, input := range inputs {
		data = append(data, input.data)
	}
	encoded, err := json.Marshal(data)
	if err != nil {
		return nil, err
	}
	batch := modelResponse{identity: model}
	if err = batch.generate(ctx, s.model, reviewTask, reviewSchema, encoded); err != nil {
		return nil, err
	}
	return validateReviewBatch(batch.data, inputs, batch.identity)
}

func reviewCacheEntries(reviews []domain.FunctionReview, inputs []reviewEvidence) []domain.ReviewCacheEntry {
	keys := make(map[string]string, len(inputs))
	for _, input := range inputs {
		keys[input.fact.Symbol.ID] = input.key
	}
	entries := []domain.ReviewCacheEntry{}
	for _, review := range reviews {
		if key := keys[review.SymbolID]; key != "" {
			entries = append(entries, domain.ReviewCacheEntry{Key: key, Model: review.Model, Payload: reviewCachePayload(review)})
		}
	}
	return entries
}

func attachReviews(page *domain.ReviewPage, reviews []domain.FunctionReview) {
	byID := make(map[string]domain.FunctionReview, len(reviews))
	for _, review := range reviews {
		byID[review.SymbolID] = review
	}
	for index := range page.Items {
		review := byID[page.Items[index].Symbol.ID]
		page.Items[index].Review = &review
	}
}
