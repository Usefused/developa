package postgres

import (
	"context"

	"developa/internal/domain"
)

var _ domain.FeatureContextReader = (*Store)(nil)

func (s *Store) FeatureContext(ctx context.Context, repositoryID, snapshotID, featureID string, limit int) (pack domain.ContextPack, err error) {
	ctx, done := operation(ctx, "postgres.feature_context")
	defer func() { done(err) }()
	if limit < 1 || limit > 20 || !hexID.MatchString(featureID) {
		return pack, domain.ErrInvalidInput
	}
	pack.RepositoryID, pack.SnapshotID = repositoryID, snapshotID
	var exists bool
	var payload []byte
	err = s.pool.QueryRow(ctx, featureContextSQL, repositoryID, snapshotID, featureID, limit).Scan(&exists, &pack.Total, &payload)
	if err := pageError(err, exists); err != nil {
		return pack, err
	}
	pack.Truncated = pack.Total > limit
	err = decodeSymbols(payload, &pack.Items)
	return pack, err
}

// Explanations are grounded in canonical symbol records for the feature's
// current generation, never in model-supplied citation labels or other runs.
const featureContextSQL = `WITH selected AS MATERIALIZED (
	SELECT f.repository_id,f.snapshot_id,f.run_id,f.id FROM developa_features f
	JOIN developa_snapshots s ON s.repository_id=f.repository_id AND s.id=f.snapshot_id AND s.latest_feature_run_id=f.run_id
	WHERE f.repository_id=$1 AND f.snapshot_id=$2 AND f.id=$3
), evidence AS MATERIALIZED (
	SELECT y.id,y.file_path,y.source_line,y.payload FROM developa_feature_evidence e
	JOIN selected f ON f.repository_id=e.repository_id AND f.snapshot_id=e.snapshot_id AND f.run_id=e.run_id AND f.id=e.feature_id
	JOIN developa_symbols y ON y.repository_id=e.repository_id AND y.snapshot_id=e.snapshot_id AND y.id=e.symbol_id
	WHERE e.repository_id=$1 AND e.snapshot_id=$2
), page AS (
	SELECT id,file_path,source_line,jsonb_build_object('path',file_path,'symbol',payload) AS item FROM evidence
	ORDER BY file_path,source_line,id LIMIT $4
)
SELECT EXISTS(SELECT 1 FROM selected),(SELECT count(*) FROM evidence),
	COALESCE(jsonb_agg(item ORDER BY file_path,source_line,id),'[]'::jsonb) FROM page`
