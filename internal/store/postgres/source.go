package postgres

import (
	"context"
	"unicode/utf8"

	"developa/internal/domain"
)

var _ domain.SymbolSourceReader = (*Store)(nil)

// Only the requested declaration bytes cross the database boundary. Full file
// bytes stay in PostgreSQL; the snapshot/file join never consults a checkout.
const symbolSourceSQL = `WITH selected AS (
 SELECT s.file_path, s.id, s.payload->'span' AS span,
 s.payload->>'source_id' AS source_id, s.payload->>'content_hash' AS content_hash,
 (s.payload#>>'{span,start,offset}')::bigint AS start_byte,
 (s.payload#>>'{span,end,offset}')::bigint AS end_byte,
 f.source, convert_to(COALESCE(s.payload->>'source',''),'UTF8') AS preview,
 COALESCE((s.payload->>'source_truncated')::boolean,true) AS truncated
 FROM developa_symbols s JOIN developa_files f
 ON f.repository_id=s.repository_id AND f.snapshot_id=s.snapshot_id AND f.path=s.file_path
 WHERE s.repository_id=$1 AND s.snapshot_id=$2 AND s.id=$3
), captured AS (
 SELECT *, end_byte-start_byte AS total_bytes,
 CASE WHEN source IS NOT NULL THEN start_byte ELSE 0 END AS base_byte,
 COALESCE(source,preview) AS retained,
 start_byte >= 0 AND end_byte >= start_byte AND
 CASE WHEN source IS NOT NULL THEN end_byte <= octet_length(source)
 ELSE NOT truncated AND octet_length(preview)=end_byte-start_byte END AS available
 FROM selected
)
SELECT jsonb_build_object('snapshot_id',$2::text,'path',file_path,'symbol_id',id,
 'span',span,'source_id',source_id,'content_hash',content_hash,'total_bytes',total_bytes),
 source IS NOT NULL, available,
 CASE WHEN available AND $4::bigint <= total_bytes THEN
 substring(retained FROM (base_byte+$4::bigint+1)::integer
 FOR LEAST($5::bigint,total_bytes-$4::bigint)::integer)
 ELSE ''::bytea END
FROM captured`

func (s *Store) Source(ctx context.Context, repositoryID, snapshotID, symbolID string, options domain.SourceOptions) (source domain.SymbolSource, err error) {
	ctx, done := operation(ctx, "postgres.symbol_source")
	defer func() { done(err) }()
	options, err = options.Validate()
	if err != nil {
		return source, err
	}
	var metadata, chunk []byte
	var fullFile, available bool
	err = s.pool.QueryRow(ctx, symbolSourceSQL, repositoryID, snapshotID, symbolID, options.Offset, options.Limit).
		Scan(&metadata, &fullFile, &available, &chunk)
	if err != nil {
		return source, databaseError(err)
	}
	if err = decodeJSON(metadata, &source); err != nil {
		return source, err
	}
	return completeSource(source, chunk, options.Offset, available, fullFile)
}

func completeSource(source domain.SymbolSource, chunk []byte, offset int, available, fullFile bool) (domain.SymbolSource, error) {
	if !available {
		return source, domain.ErrSourceUnavailable
	}
	if offset > source.TotalBytes {
		return source, domain.ErrInvalidInput
	}
	text, err := sourceText(chunk, offset+len(chunk) < source.TotalBytes)
	if err != nil {
		return source, err
	}
	source.Source, source.Offset, source.Complete = text, offset, true
	source.Limitations = []string{}
	if !fullFile {
		source.Limitations = append(source.Limitations, "Full file bytes were not retained; this declaration is available from its complete indexed excerpt.")
	}
	if next := offset + len(text); next < source.TotalBytes {
		source.NextOffset = &next
	}
	return source, nil
}

func sourceText(chunk []byte, more bool) (string, error) {
	if len(chunk) > 0 && !utf8.RuneStart(chunk[0]) {
		return "", domain.ErrInvalidInput
	}
	for offset := 0; offset < len(chunk); {
		r, size := utf8.DecodeRune(chunk[offset:])
		if r == utf8.RuneError && size == 1 {
			return sourceTextTail(chunk, offset, more)
		}
		offset += size
	}
	return string(chunk), nil
}

func sourceTextTail(chunk []byte, offset int, more bool) (string, error) {
	// A requested byte budget may split the last rune. Invalid stored UTF-8 must
	// not be replaced silently, because that would change source evidence.
	if more && !utf8.FullRune(chunk[offset:]) {
		return string(chunk[:offset]), nil
	}
	return "", domain.ErrSourceUnavailable
}
