package postgres

const callsSQL = `WITH filtered AS MATERIALIZED (
	SELECT id,path,source_line,payload FROM developa_calls WHERE repository_id=$1 AND snapshot_id=$2
	AND ($3='' OR CASE WHEN $4='out' THEN caller_id=$3 ELSE target_id=$3 END)
	AND ($5='' OR resolution=$5)
), page AS (SELECT id,path,source_line,payload FROM filtered ORDER BY path,source_line,id LIMIT $6 OFFSET $7)
SELECT EXISTS(SELECT 1 FROM developa_snapshots WHERE repository_id=$1 AND id=$2)
	AND ($3='' OR EXISTS(SELECT 1 FROM developa_symbols WHERE repository_id=$1 AND snapshot_id=$2 AND id=$3)),
	(SELECT count(*) FROM filtered), COALESCE(jsonb_agg(payload ORDER BY path,source_line,id),'[]'::jsonb) FROM page`

// One recursive row carries a bounded BFS frontier. Global visited IDs eliminate
// repeated paths, so cycles and diamonds cannot produce exponential path growth.
// Discovery edges precede optional edges so an edge cap cannot disconnect nodes.
const chainSQL = `WITH RECURSIVE walk(depth,visited,frontier,discovery_ids,truncated) AS (
	SELECT 0,ARRAY[$3::text],ARRAY[$3::text],ARRAY[]::text[],false
	UNION ALL
	SELECT w.depth+1,w.visited || n.ids,n.ids,w.discovery_ids || n.edge_ids,w.truncated OR n.overflow
	FROM walk w CROSS JOIN LATERAL (
		SELECT COALESCE(array_agg(id ORDER BY id) FILTER (WHERE ordinal <= $6-cardinality(w.visited)),ARRAY[]::text[]) AS ids,
			COALESCE(array_agg(edge_id ORDER BY id) FILTER (WHERE ordinal <= $6-cardinality(w.visited)),ARRAY[]::text[]) AS edge_ids,
			count(*) > $6-cardinality(w.visited) AS overflow
		FROM (SELECT id,edge_id,row_number() OVER (ORDER BY id) AS ordinal FROM (
			SELECT DISTINCT ON (CASE WHEN $4='out' THEN c.target_id ELSE c.caller_id END)
				CASE WHEN $4='out' THEN c.target_id ELSE c.caller_id END AS id,c.id AS edge_id,c.path,c.source_line
			FROM developa_calls c WHERE c.repository_id=$1 AND c.snapshot_id=$2 AND c.resolution='resolved'
			AND CASE WHEN $4='out' THEN c.caller_id=ANY(w.frontier) ELSE c.target_id=ANY(w.frontier) END
			AND NOT (CASE WHEN $4='out' THEN c.target_id ELSE c.caller_id END)=ANY(w.visited)
			ORDER BY id,c.path,c.source_line,c.id LIMIT GREATEST($6-cardinality(w.visited),0)+1
		) candidates) numbered
	) n WHERE w.depth<$5 AND cardinality(w.frontier)>0 AND NOT w.truncated
), final AS (SELECT * FROM walk ORDER BY depth DESC LIMIT 1), edge_candidates AS MATERIALIZED (
	SELECT c.id,c.path,c.source_line,c.payload,array_position(f.discovery_ids,c.id) AS discovery_order
	FROM developa_calls c,final f
	WHERE c.repository_id=$1 AND c.snapshot_id=$2 AND c.resolution='resolved'
	AND c.caller_id=ANY(f.visited) AND c.target_id=ANY(f.visited)
	ORDER BY discovery_order NULLS LAST,c.path,c.source_line,c.id LIMIT $6+1
), edges AS (SELECT * FROM edge_candidates ORDER BY discovery_order NULLS LAST,path,source_line,id LIMIT $6)
SELECT EXISTS(SELECT 1 FROM developa_symbols WHERE repository_id=$1 AND snapshot_id=$2 AND id=$3),
	COALESCE((SELECT jsonb_agg(jsonb_build_object('path',s.file_path,'symbol',s.payload) ORDER BY array_position(f.visited,s.id))
		FROM developa_symbols s,final f WHERE s.repository_id=$1 AND s.snapshot_id=$2 AND s.id=ANY(f.visited)),'[]'::jsonb),
	COALESCE((SELECT jsonb_agg(payload ORDER BY discovery_order NULLS LAST,path,source_line,id) FROM edges),'[]'::jsonb),
	(SELECT truncated FROM final) OR (SELECT count(*)>$6 FROM edge_candidates) OR EXISTS(
		SELECT 1 FROM developa_calls c,final f WHERE c.repository_id=$1 AND c.snapshot_id=$2 AND c.resolution='resolved'
		AND CASE WHEN $4='out' THEN c.caller_id=ANY(f.frontier) ELSE c.target_id=ANY(f.frontier) END
		AND NOT (CASE WHEN $4='out' THEN c.target_id ELSE c.caller_id END)=ANY(f.visited))`

const contextSQL = `WITH terms AS (
	SELECT to_tsquery('simple',COALESCE(string_agg(term,' | '),'')) AS query FROM (
		SELECT DISTINCT term FROM regexp_split_to_table(lower($3),'[^[:alnum:]_]+') AS term
		WHERE length(term)>0 ORDER BY term
	) tokens
), matched AS MATERIALIZED (
	SELECT s.id,s.file_path,s.source_line,s.payload,ts_rank_cd(s.search_document,t.query) AS relevance
	FROM developa_symbols s CROSS JOIN terms t WHERE s.repository_id=$1 AND s.snapshot_id=$2
	AND ($3='' OR s.search_document @@ t.query)
), page AS (
	SELECT * FROM matched ORDER BY relevance DESC,file_path,source_line,id LIMIT $4
)
SELECT EXISTS(SELECT 1 FROM developa_snapshots WHERE repository_id=$1 AND id=$2),
	(SELECT count(*) FROM matched),COALESCE(jsonb_agg(jsonb_build_object('path',file_path,'symbol',payload)
		ORDER BY relevance DESC,file_path,source_line,id),'[]'::jsonb) FROM page`
