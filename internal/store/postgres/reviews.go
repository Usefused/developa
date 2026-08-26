package postgres

import (
	"context"
	"encoding/json"

	"developa/internal/domain"
	"github.com/jackc/pgx/v5"
)

var _ domain.ReviewStore = (*Store)(nil)

func (s *Store) ReviewPage(ctx context.Context, repositoryID, snapshotID string, options domain.ReviewOptions) (page domain.ReviewPage, err error) {
	ctx, done := operation(ctx, "postgres.review_page")
	defer func() { done(err) }()
	options, err = domain.NormalizeReviewOptions(options)
	if err != nil {
		return page, err
	}
	page = domain.ReviewPage{SnapshotID: snapshotID, Options: options, Limitations: []string{
		"AI reviews are inferred from captured source, not proof of runtime behavior. Source comments remain separate.",
		"Only resolved local direct callees can be summarized. External and unresolved call sites are counted, not invented.",
	}}
	var payload []byte
	var exists bool
	err = s.pool.QueryRow(ctx, reviewPageSQL, repositoryID, snapshotID, options.SymbolID, options.CalleeOf, options.Limit, options.Offset).Scan(&exists, &page.Total, &payload, &page.UnresolvedCount)
	if err := pageError(err, exists); err != nil {
		return page, err
	}
	if err = decodeSymbols(payload, &page.Items); err != nil {
		return page, err
	}
	page.Advance(len(page.Items))
	return page, nil
}

func (s *Store) CachedReviews(ctx context.Context, repositoryID string, keys []string) (result map[string]json.RawMessage, err error) {
	ctx, done := operation(ctx, "postgres.cached_reviews")
	defer func() { done(err) }()
	if len(keys) > 8 {
		return nil, domain.ErrInvalidInput
	}
	var payload []byte
	err = s.pool.QueryRow(ctx, `SELECT COALESCE(jsonb_object_agg(cache_key,payload),'{}'::jsonb)
		FROM developa_function_review_cache WHERE repository_id=$1 AND cache_key=ANY($2::text[])`, repositoryID, keys).Scan(&payload)
	if err != nil {
		return nil, databaseError(err)
	}
	err = decodeJSON(payload, &result)
	return result, err
}

func (s *Store) SaveReviews(ctx context.Context, repositoryID, snapshotID string, reviews []domain.FunctionReview, cache []domain.ReviewCacheEntry, execution domain.Execution) error {
	if err := validateReviewWrite(reviews, cache); err != nil {
		return err
	}
	return s.intelligenceWrite(ctx, repositoryID, snapshotID, "postgres.save_reviews", execution, map[string]int{"reviews": len(reviews), "cache_entries": len(cache)},
		func(ctx context.Context, tx pgx.Tx, _ int) error {
			return insertReviews(ctx, tx, repositoryID, snapshotID, reviews, cache)
		})
}

func validateReviewWrite(reviews []domain.FunctionReview, cache []domain.ReviewCacheEntry) error {
	if len(reviews) > 8 || len(cache) > 8 {
		return domain.ErrInvalidInput
	}
	seen := make(map[string]bool)
	for _, review := range reviews {
		if seen[review.SymbolID] || !validReviewRecord(review) {
			return domain.ErrInvalidModelOutput
		}
		seen[review.SymbolID] = true
	}
	for _, entry := range cache {
		if !hexID.MatchString(entry.Key) || !validModel(entry.Model) || !validCachePayload(entry.Payload) {
			return domain.ErrInvalidInput
		}
	}
	return nil
}

func validReviewRecord(review domain.FunctionReview) bool {
	if !hexID.MatchString(review.SymbolID) || !hexID.MatchString(review.SourceID) || !validModel(review.Model) {
		return false
	}
	if !boundedText(review.Summary, 1200) || !boundedText(review.PromptVersion, 80) || len(review.Parameters) > 16 {
		return false
	}
	for _, parameter := range review.Parameters {
		if parameter.Position < 0 || !boundedText(parameter.Description, 400) {
			return false
		}
	}
	return true
}

func insertReviews(ctx context.Context, tx pgx.Tx, repositoryID, snapshotID string, reviews []domain.FunctionReview, cache []domain.ReviewCacheEntry) error {
	payload, err := json.Marshal(nonNil(reviews))
	if err != nil {
		return err
	}
	command, err := tx.Exec(ctx, insertReviewsSQL, repositoryID, snapshotID, payload)
	if err != nil {
		return err
	}
	// Reject the whole batch if even one source identity is stale or outside scope.
	if command.RowsAffected() != int64(len(reviews)) {
		return domain.ErrInvalidModelOutput
	}
	entries, err := json.Marshal(nonNil(cache))
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `INSERT INTO developa_function_review_cache(repository_id,cache_key,model,payload)
		SELECT $1,e.key,e.model,e.payload FROM jsonb_to_recordset($2::jsonb) AS e(key text,model text,payload jsonb)
		ON CONFLICT(repository_id,cache_key) DO NOTHING`, repositoryID, entries)
	return err
}
