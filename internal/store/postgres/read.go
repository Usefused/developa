package postgres

import (
	"context"
	"encoding/json"

	"developa/internal/domain"
	goparser "developa/internal/indexer/golang"
)

var _ domain.CatalogReader = (*Store)(nil)

func (s *Store) Latest(ctx context.Context, repositoryID string) (snapshot domain.Snapshot, err error) {
	ctx, done := operation(ctx, "postgres.latest")
	defer func() { done(err) }()
	var payload []byte
	err = s.pool.QueryRow(ctx, `SELECT s.metadata FROM developa_snapshots s
		JOIN developa_repositories r ON r.id=s.repository_id AND r.latest_snapshot_id=s.id
		WHERE r.id=$1`, repositoryID).Scan(&payload)
	if err != nil {
		return snapshot, databaseError(err)
	}
	err = decodeJSON(payload, &snapshot)
	return snapshot, err
}

func (s *Store) File(ctx context.Context, repositoryID, snapshotID, filePath string) (file domain.FileDetail, err error) {
	ctx, done := operation(ctx, "postgres.file")
	defer func() { done(err) }()
	var payload []byte
	err = s.pool.QueryRow(ctx, `SELECT jsonb_build_object(
		'path',path,'package',package,'overview',overview,'completeness',completeness,
		'symbol_count',symbol_count,'kinds',kinds,'doc',doc,'imports',imports)
		FROM developa_files WHERE repository_id=$1 AND snapshot_id=$2 AND path=$3`, repositoryID, snapshotID, filePath).Scan(&payload)
	if err != nil {
		return file, databaseError(err)
	}
	err = decodeJSON(payload, &file)
	return file, err
}

func (s *Store) Symbol(ctx context.Context, repositoryID, snapshotID, symbolID string) (symbol domain.SymbolDetail, err error) {
	ctx, done := operation(ctx, "postgres.symbol")
	defer func() { done(err) }()
	var payload []byte
	err = s.pool.QueryRow(ctx, `SELECT jsonb_build_object('path',s.file_path,'symbol',s.payload,'review',r.payload)
		FROM developa_symbols s LEFT JOIN developa_function_reviews r
		ON r.repository_id=s.repository_id AND r.snapshot_id=s.snapshot_id AND r.symbol_id=s.id
		WHERE s.repository_id=$1 AND s.snapshot_id=$2 AND s.id=$3`, repositoryID, snapshotID, symbolID).Scan(&payload)
	if err != nil {
		return symbol, databaseError(err)
	}
	err = decodeJSON(payload, &symbol)
	symbol.Symbol.Documentation = goparser.DocumentationFor(symbol.Symbol)
	return symbol, err
}

func (s *Store) Details(ctx context.Context, repositoryID, snapshotID string) (details domain.SnapshotDetails, err error) {
	ctx, done := operation(ctx, "postgres.details")
	defer func() { done(err) }()
	var payload []byte
	err = s.pool.QueryRow(ctx, `SELECT details || jsonb_build_object('snapshot',metadata)
		FROM developa_snapshots WHERE repository_id=$1 AND id=$2`, repositoryID, snapshotID).Scan(&payload)
	if err != nil {
		return details, databaseError(err)
	}
	err = decodeJSON(payload, &details)
	return details, err
}

func (s *Store) Files(ctx context.Context, repositoryID, snapshotID string, filter domain.Filter) (page domain.FilePage, err error) {
	ctx, done := operation(ctx, "postgres.files")
	defer func() { done(err) }()
	filter = boundedFilter(filter)
	page.Limit, page.Offset = filter.Limit, filter.Offset
	var payload []byte
	var exists bool
	err = s.pool.QueryRow(ctx, filesSQL, repositoryID, snapshotID, filter.Query, filter.Kind, filter.File, filter.Limit, filter.Offset).Scan(&exists, &page.Total, &payload)
	if err := pageError(err, exists); err != nil {
		return page, err
	}
	err = decodeJSON(payload, &page.Items)
	return page, err
}

func (s *Store) Symbols(ctx context.Context, repositoryID, snapshotID string, filter domain.Filter) (page domain.SymbolPage, err error) {
	ctx, done := operation(ctx, "postgres.symbols")
	defer func() { done(err) }()
	filter = boundedFilter(filter)
	page.Limit, page.Offset = filter.Limit, filter.Offset
	var payload []byte
	var exists bool
	err = s.pool.QueryRow(ctx, symbolsSQL, repositoryID, snapshotID, filter.Query, filter.Kind, filter.File, filter.Limit, filter.Offset).Scan(&exists, &page.Total, &payload)
	if err := pageError(err, exists); err != nil {
		return page, err
	}
	err = decodeSymbols(payload, &page.Items)
	return page, err
}

func decodeJSON(payload []byte, target any) error {
	if err := json.Unmarshal(payload, target); err != nil {
		return ErrUnavailable
	}
	return nil
}

func decodeSymbols(payload []byte, target *[]domain.SymbolDetail) error {
	if err := decodeJSON(payload, target); err != nil {
		return err
	}
	// Enrich only the already SQL-scoped page; this never reads another symbol
	// or substitutes current working-tree source for historical evidence.
	for i := range *target {
		item := &(*target)[i]
		item.Symbol.Documentation = goparser.DocumentationFor(item.Symbol)
	}
	return nil
}

func pageError(err error, exists bool) error {
	if err != nil {
		return databaseError(err)
	}
	if !exists {
		return domain.ErrNotFound
	}
	return nil
}
