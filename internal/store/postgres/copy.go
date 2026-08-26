package postgres

import (
	"context"
	"encoding/json"

	goparser "developa/internal/indexer/golang"
	"github.com/jackc/pgx/v5"
)

func copyCatalog(ctx context.Context, tx pgx.Tx, repositoryID, snapshotID string, files []goparser.FileBlock) error {
	_, err := tx.CopyFrom(ctx, pgx.Identifier{"developa_files"},
		[]string{"repository_id", "snapshot_id", "path", "package", "overview", "completeness", "symbol_count", "kinds", "doc", "imports", "source"},
		pgx.CopyFromSlice(len(files), func(i int) ([]any, error) {
			return fileValues(repositoryID, snapshotID, files[i])
		}))
	if err != nil {
		return err
	}
	_, err = tx.CopyFrom(ctx, pgx.Identifier{"developa_symbols"},
		[]string{"repository_id", "snapshot_id", "id", "file_path", "name", "kind", "source_line", "payload"},
		&symbolRows{repositoryID: repositoryID, snapshotID: snapshotID, files: files})
	return err
}

func fileValues(repositoryID, snapshotID string, file goparser.FileBlock) ([]any, error) {
	kinds := make(map[string]int)
	for _, symbol := range file.Symbols {
		kinds[string(symbol.Kind)]++
	}
	kindJSON, err := json.Marshal(kinds)
	if err != nil {
		return nil, err
	}
	imports, err := json.Marshal(nonNil(file.Imports))
	if err != nil {
		return nil, err
	}
	return []any{repositoryID, snapshotID, file.Path, file.Package, file.Overview,
		string(file.Completeness), len(file.Symbols), kindJSON, file.Doc, imports, file.Source}, nil
}

type symbolRows struct {
	repositoryID string
	snapshotID   string
	files        []goparser.FileBlock
	file         int
	symbol       int
}

func (r *symbolRows) Next() bool {
	for r.file < len(r.files) {
		if r.symbol < len(r.files[r.file].Symbols) {
			r.symbol++
			return true
		}
		r.file++
		r.symbol = 0
	}
	return false
}

func (r *symbolRows) Values() ([]any, error) {
	file := r.files[r.file]
	symbol := file.Symbols[r.symbol-1]
	payload, err := json.Marshal(symbol)
	if err != nil {
		return nil, err
	}
	return []any{r.repositoryID, r.snapshotID, symbol.ID, file.Path, symbol.Name,
		string(symbol.Kind), symbol.Span.Start.Line, payload}, nil
}

func (r *symbolRows) Err() error { return nil }

func copyCalls(ctx context.Context, tx pgx.Tx, repositoryID, snapshotID string, calls []goparser.Call) error {
	_, err := tx.CopyFrom(ctx, pgx.Identifier{"developa_calls"},
		[]string{"repository_id", "snapshot_id", "id", "caller_id", "target_id", "resolution", "path", "source_line", "payload"},
		pgx.CopyFromSlice(len(calls), func(i int) ([]any, error) {
			call := calls[i]
			payload, err := json.Marshal(call)
			if err != nil {
				return nil, err
			}
			var target any
			if call.TargetID != "" {
				target = call.TargetID
			}
			return []any{repositoryID, snapshotID, call.ID, call.CallerID, target, call.Resolution, call.Path, call.Span.Start.Line, payload}, nil
		}))
	return err
}
