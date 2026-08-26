package postgres

import (
	"context"

	"developa/internal/domain"
)

var _ domain.AnalysisPageReader = (*Store)(nil)

func (s *Store) AnalysisPage(ctx context.Context, repositoryID, snapshotID string, limit, offset int) (page domain.SymbolPage, err error) {
	ctx, done := operation(ctx, "postgres.analysis_page")
	defer func() { done(err) }()
	if limit < 1 || limit > 100 || offset < 0 {
		return page, domain.ErrInvalidInput
	}
	page.Limit, page.Offset = limit, offset
	var exists bool
	var payload []byte
	err = s.pool.QueryRow(ctx, analysisPageSQL, repositoryID, snapshotID, limit, offset).Scan(&exists, &page.Total, &payload)
	if err := pageError(err, exists); err != nil {
		return page, err
	}
	err = decodeSymbols(payload, &page.Items)
	return page, err
}

// File-local batches stop a declaration inserted in one file from shifting the
// cached inputs of every subsequent file. Only the bounded candidate page is read.
const analysisPageSQL = `WITH candidates AS MATERIALIZED (
	SELECT id,file_path,source_line,payload FROM developa_symbols WHERE repository_id=$1 AND snapshot_id=$2
	ORDER BY file_path,source_line,id LIMIT $3 OFFSET $4
), page AS (
	SELECT id,file_path,source_line,jsonb_build_object('path',file_path,'symbol',payload) AS item FROM candidates
	WHERE file_path=(SELECT file_path FROM candidates ORDER BY file_path,source_line,id LIMIT 1)
)
SELECT EXISTS(SELECT 1 FROM developa_snapshots WHERE repository_id=$1 AND id=$2),
	COALESCE((SELECT (metadata->>'symbol_count')::integer FROM developa_snapshots WHERE repository_id=$1 AND id=$2),0),
	COALESCE(jsonb_agg(item ORDER BY file_path,source_line,id),'[]'::jsonb) FROM page`
