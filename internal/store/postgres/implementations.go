package postgres

import (
	"context"
	"encoding/json"

	"developa/internal/domain"
	goparser "developa/internal/indexer/golang"
	"github.com/jackc/pgx/v5"
)

var _ domain.ImplementationReader = (*Store)(nil)

func (s *Store) Implementations(ctx context.Context, repositoryID, snapshotID, symbolID string, options domain.ImplementationOptions) (page domain.ImplementationPage, err error) {
	ctx, done := operation(ctx, "postgres.implementations")
	defer func() { done(err) }()
	options, err = normalizeImplementations(options)
	if err != nil {
		return page, err
	}
	page = domain.ImplementationPage{RepositoryID: repositoryID, SnapshotID: snapshotID, SymbolID: symbolID, Limit: options.Limit, Offset: options.Offset}
	var exists bool
	var payload, analysis []byte
	err = s.pool.QueryRow(ctx, implementationsSQL, repositoryID, snapshotID, symbolID, options.Limit, options.Offset).Scan(&exists, &page.Total, &payload, &analysis)
	if err := pageError(err, exists); err != nil {
		return page, err
	}
	if err := decodeJSON(payload, &page.Items); err != nil {
		return page, err
	}
	err = decodeJSON(analysis, &page.Analysis)
	return page, err
}

func normalizeImplementations(options domain.ImplementationOptions) (domain.ImplementationOptions, error) {
	if options.Limit == 0 {
		options.Limit = 20
	}
	if options.Limit < 1 || options.Limit > 100 || options.Offset < 0 || options.Offset > 100000 {
		return options, domain.ErrInvalidInput
	}
	return options, nil
}

func copyImplementations(ctx context.Context, tx pgx.Tx, repositoryID, snapshotID string, candidates []goparser.Implementation) error {
	_, err := tx.CopyFrom(ctx, pgx.Identifier{"developa_implementations"},
		[]string{"repository_id", "snapshot_id", "interface_id", "method_id", "receiver_id", "target_id", "payload"},
		pgx.CopyFromSlice(len(candidates), func(i int) ([]any, error) {
			candidate := candidates[i]
			payload, err := json.Marshal(candidate)
			if err != nil {
				return nil, err
			}
			return []any{repositoryID, snapshotID, candidate.Interface.SymbolID, candidate.Method.SymbolID, candidate.Receiver.SymbolID, candidate.Target.SymbolID, payload}, nil
		}))
	return err
}

// The page, filtered total and analysis status share one snapshot-scoped query.
// Even an empty page retains limitations; zero rows must not imply no runtime implementation.
const implementationsSQL = `WITH filtered AS MATERIALIZED (
	SELECT interface_id,method_id,receiver_id,target_id,payload FROM developa_implementations
	WHERE repository_id=$1 AND snapshot_id=$2 AND (interface_id=$3 OR method_id=$3)
), page AS (
	SELECT * FROM filtered ORDER BY interface_id,method_id,receiver_id,target_id LIMIT $4 OFFSET $5
)
SELECT EXISTS(SELECT 1 FROM developa_symbols WHERE repository_id=$1 AND snapshot_id=$2 AND id=$3),
	(SELECT count(*) FROM filtered),
	COALESCE((SELECT jsonb_agg(payload ORDER BY interface_id,method_id,receiver_id,target_id) FROM page),'[]'::jsonb),
	COALESCE((SELECT CASE WHEN details->'implementation_analysis'->>'status' IN ('complete','partial')
		THEN details->'implementation_analysis' END FROM developa_snapshots WHERE repository_id=$1 AND id=$2),
		'{"status":"unavailable","limitations":["Implementation analysis was not retained for this snapshot; index a new snapshot. Never infer absence from an empty page."]}'::jsonb)`
