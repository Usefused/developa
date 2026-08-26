package postgres

const featuresSQL = `WITH latest AS (
	SELECT r.id,r.metadata FROM developa_feature_runs r JOIN developa_snapshots s
	ON s.repository_id=r.repository_id AND s.id=r.snapshot_id AND s.latest_feature_run_id=r.id
	WHERE s.repository_id=$1 AND s.id=$2
), filtered AS MATERIALIZED (
	SELECT f.id,f.title,f.summary,f.status,f.run_id FROM developa_features f JOIN latest l ON l.id=f.run_id
	WHERE f.repository_id=$1 AND f.snapshot_id=$2
	AND ($3='' OR strpos(lower(f.title || ' ' || f.summary),lower($3))>0)
), selected AS MATERIALIZED (SELECT * FROM filtered ORDER BY title,id LIMIT $4 OFFSET $5), evidence AS (
	SELECT e.feature_id,jsonb_agg(jsonb_build_object('symbol_id',s.id,'path',s.file_path,
		'name',s.name,'span',s.payload->'span') ORDER BY s.file_path,s.source_line,s.id) AS items
	FROM developa_feature_evidence e JOIN selected p ON p.id=e.feature_id AND p.run_id=e.run_id
	JOIN developa_symbols s ON s.repository_id=e.repository_id AND s.snapshot_id=e.snapshot_id AND s.id=e.symbol_id
	WHERE e.repository_id=$1 AND e.snapshot_id=$2 GROUP BY e.feature_id
), page AS (
	SELECT p.id,p.title,jsonb_build_object('id',p.id,'title',p.title,'summary',p.summary,'status',p.status,
		'evidence',COALESCE(e.items,'[]'::jsonb)) AS item FROM selected p LEFT JOIN evidence e ON e.feature_id=p.id
)
SELECT EXISTS(SELECT 1 FROM developa_snapshots WHERE repository_id=$1 AND id=$2),
	COALESCE((SELECT metadata FROM latest),'null'::jsonb),(SELECT count(*) FROM filtered),
	COALESCE((SELECT s.metadata FROM developa_snapshots s
		WHERE s.repository_id=$1 AND s.id<>$2 AND s.latest_feature_run_id IS NOT NULL
		AND NOT EXISTS(SELECT 1 FROM latest)
		ORDER BY s.indexed_at DESC,s.id DESC LIMIT 1),'null'::jsonb),
	COALESCE(jsonb_agg(item ORDER BY title,id),'[]'::jsonb) FROM page`

const featureSQL = `SELECT jsonb_build_object('id',f.id,'title',f.title,'summary',f.summary,'status',f.status,
	'evidence',COALESCE(jsonb_agg(jsonb_build_object('symbol_id',y.id,'path',y.file_path,
	'name',y.name,'span',y.payload->'span') ORDER BY y.file_path,y.source_line,y.id) FILTER (WHERE y.id IS NOT NULL),'[]'::jsonb))
FROM developa_features f JOIN developa_snapshots s ON s.repository_id=f.repository_id AND s.id=f.snapshot_id AND s.latest_feature_run_id=f.run_id
LEFT JOIN developa_feature_evidence e ON e.repository_id=f.repository_id AND e.snapshot_id=f.snapshot_id AND e.run_id=f.run_id AND e.feature_id=f.id
LEFT JOIN developa_symbols y ON y.repository_id=e.repository_id AND y.snapshot_id=e.snapshot_id AND y.id=e.symbol_id
WHERE f.repository_id=$1 AND f.snapshot_id=$2 AND f.id=$3 GROUP BY f.repository_id,f.snapshot_id,f.run_id,f.id`
