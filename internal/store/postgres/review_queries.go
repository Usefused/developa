package postgres

// EXISTS avoids duplicate functions for repeated call sites. Counting and paging
// share one MVCC statement and never load an entire call graph into the service.
const reviewPageSQL = `WITH selected AS MATERIALIZED (
	SELECT s.* FROM developa_symbols s WHERE s.repository_id=$1 AND s.snapshot_id=$2
	AND s.kind IN ('function','method','closure','interface_method') AND ($3='' OR s.id=$3)
	AND ($4='' OR EXISTS(SELECT 1 FROM developa_calls c WHERE c.repository_id=$1 AND c.snapshot_id=$2
		AND c.caller_id=$4 AND c.target_id=s.id AND c.resolution='resolved'))
), page AS (SELECT * FROM selected ORDER BY file_path,source_line,id LIMIT $5 OFFSET $6)
SELECT EXISTS(SELECT 1 FROM developa_snapshots WHERE repository_id=$1 AND id=$2)
	AND ($3='' OR EXISTS(SELECT 1 FROM selected WHERE id=$3))
	AND ($4='' OR EXISTS(SELECT 1 FROM developa_symbols WHERE repository_id=$1 AND snapshot_id=$2 AND id=$4
		AND kind IN ('function','method','closure','interface_method'))),
	(SELECT count(*) FROM selected),COALESCE((SELECT jsonb_agg(jsonb_build_object('path',s.file_path,'symbol',s.payload,'review',r.payload)
		ORDER BY s.file_path,s.source_line,s.id) FROM page s LEFT JOIN developa_function_reviews r
		ON r.repository_id=$1 AND r.snapshot_id=$2 AND r.symbol_id=s.id),'[]'::jsonb),
	(SELECT count(*) FROM developa_calls WHERE repository_id=$1 AND snapshot_id=$2 AND caller_id=$4 AND resolution IN ('unresolved','external'))`

const insertReviewsSQL = `INSERT INTO developa_function_reviews(repository_id,snapshot_id,symbol_id,payload)
	SELECT $1,$2,s.id,(item-'evidence') || jsonb_build_object('evidence',jsonb_build_object(
		'symbol_id',s.id,'path',s.file_path,'name',s.name,'span',s.payload->'span'))
	FROM jsonb_array_elements($3::jsonb) item JOIN developa_symbols s
	ON s.repository_id=$1 AND s.snapshot_id=$2 AND s.id=item->>'symbol_id'
	AND s.payload->>'source_id'=item->>'source_id' AND s.kind IN ('function','method','closure','interface_method')
	ON CONFLICT(repository_id,snapshot_id,symbol_id) DO UPDATE SET payload=EXCLUDED.payload`
