package postgres

// Count and page use one MVCC statement, retaining the total even past the last page.
// JSON assembly follows SQL filtering/pagination; no stored rows are filtered in Go.
const filesSQL = `WITH symbol_matches AS MATERIALIZED (
	SELECT DISTINCT file_path FROM developa_symbols
	WHERE repository_id=$1 AND snapshot_id=$2 AND $3<>''
	AND ($4='' OR kind=$4) AND ($5='' OR file_path=$5)
	AND strpos(lower(name || ' ' || COALESCE(payload->>'signature','')), lower($3))>0
), filtered AS MATERIALIZED (
	SELECT f.path,f.package,f.overview,f.completeness,f.symbol_count,f.kinds
	FROM developa_files f LEFT JOIN symbol_matches m ON m.file_path=f.path
	WHERE f.repository_id=$1 AND f.snapshot_id=$2
	AND ($3='' OR strpos(lower(f.path || ' ' || f.package || ' ' || f.overview), lower($3))>0
		OR m.file_path IS NOT NULL)
	AND ($4='' OR f.kinds ? $4) AND ($5='' OR f.path=$5)
), page AS (
	SELECT path,jsonb_build_object('path',path,'package',package,'overview',overview,
		'completeness',completeness,'symbol_count',symbol_count,'kinds',kinds) AS item
	FROM filtered ORDER BY path LIMIT $6 OFFSET $7
)
SELECT EXISTS(SELECT 1 FROM developa_snapshots WHERE repository_id=$1 AND id=$2),
	(SELECT count(*) FROM filtered), COALESCE(jsonb_agg(item ORDER BY path),'[]'::jsonb) FROM page`

const symbolsSQL = `WITH filtered AS MATERIALIZED (
	SELECT id,file_path,name,source_line,payload
	FROM developa_symbols WHERE repository_id=$1 AND snapshot_id=$2
	AND ($3='' OR strpos(lower(name || ' ' || file_path || ' ' || (payload->>'signature')), lower($3))>0)
	AND ($4='' OR kind=$4) AND ($5='' OR file_path=$5)
), page AS (
	SELECT s.id,s.file_path,s.source_line,jsonb_build_object('path',s.file_path,'symbol',s.payload,'review',r.payload) AS item
	FROM (SELECT * FROM filtered ORDER BY file_path,source_line,id LIMIT $6 OFFSET $7) s
	LEFT JOIN developa_function_reviews r ON r.repository_id=$1 AND r.snapshot_id=$2 AND r.symbol_id=s.id
)
SELECT EXISTS(SELECT 1 FROM developa_snapshots WHERE repository_id=$1 AND id=$2),
	(SELECT count(*) FROM filtered), COALESCE(jsonb_agg(item ORDER BY file_path,source_line,id),'[]'::jsonb) FROM page`
