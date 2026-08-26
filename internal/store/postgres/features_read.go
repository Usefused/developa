package postgres

import (
	"context"

	"developa/internal/domain"
)

func (s *Store) Features(ctx context.Context, repositoryID, snapshotID string, filter domain.Filter) (page domain.FeaturePage, err error) {
	ctx, done := operation(ctx, "postgres.features")
	defer func() { done(err) }()
	if filter.Kind != "" || filter.File != "" || !validSearchQuery(filter.Query, 200) {
		return page, domain.ErrInvalidInput
	}
	if filter.Limit < 0 || filter.Limit > 100 || filter.Offset < 0 {
		return page, domain.ErrInvalidInput
	}
	filter = boundedFilter(filter)
	page.Limit, page.Offset = filter.Limit, filter.Offset
	var exists bool
	var run, savedSnapshot, payload []byte
	err = s.pool.QueryRow(ctx, featuresSQL, repositoryID, snapshotID, filter.Query, filter.Limit, filter.Offset).Scan(&exists, &run, &page.Total, &savedSnapshot, &payload)
	if err := pageError(err, exists); err != nil {
		return page, err
	}
	if err := decodeJSON(run, &page.Run); err != nil {
		return page, err
	}
	if err := decodeJSON(savedSnapshot, &page.SavedSnapshot); err != nil {
		return page, err
	}
	err = decodeJSON(payload, &page.Items)
	return page, err
}

func (s *Store) Feature(ctx context.Context, repositoryID, snapshotID, featureID string) (feature domain.Feature, err error) {
	ctx, done := operation(ctx, "postgres.feature")
	defer func() { done(err) }()
	var payload []byte
	err = s.pool.QueryRow(ctx, featureSQL, repositoryID, snapshotID, featureID).Scan(&payload)
	if err != nil {
		return feature, databaseError(err)
	}
	err = decodeJSON(payload, &feature)
	return feature, err
}
