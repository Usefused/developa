package postgres

// Each recursive row carries bounded global IDs, rather than a row per path.
// Upward discovery runs first; downward discovery starts from seeds only and has
// its own visited set so an ancestor that is also a callee is expanded just once.
const flowSQL = `WITH RECURSIVE incoming AS MATERIALIZED (
	SELECT target_id AS id,count(DISTINCT caller_id)::integer AS amount FROM developa_calls
	WHERE repository_id=$1 AND snapshot_id=$2 AND resolution='resolved' GROUP BY target_id
), outgoing AS MATERIALIZED (
	SELECT caller_id AS id,count(DISTINCT target_id) FILTER(WHERE resolution='resolved')::integer AS amount,
		count(*) FILTER(WHERE resolution IN ('unresolved','external'))::integer AS unresolved
	FROM developa_calls WHERE repository_id=$1 AND snapshot_id=$2 GROUP BY caller_id
), selected_feature AS MATERIALIZED (
	SELECT f.repository_id,f.snapshot_id,f.run_id,f.id FROM developa_features f JOIN developa_snapshots s
	ON s.repository_id=f.repository_id AND s.id=f.snapshot_id AND s.latest_feature_run_id=f.run_id
	WHERE f.repository_id=$1 AND f.snapshot_id=$2 AND f.id=$4
), root_candidates AS MATERIALIZED (
	SELECT s.id,s.file_path,s.source_line,CASE
		WHEN s.kind='function' AND (s.name='init' OR (s.name='main' AND f.package='main')) THEN 0
		WHEN COALESCE(i.amount,0)=0 THEN 1 ELSE 2 END AS priority
	FROM developa_symbols s JOIN developa_files f ON f.repository_id=s.repository_id AND f.snapshot_id=s.snapshot_id AND f.path=s.file_path
	LEFT JOIN incoming i ON i.id=s.id WHERE s.repository_id=$1 AND s.snapshot_id=$2
	AND s.kind IN ('function','method','closure') AND $3='' AND $4=''
), root_class AS (SELECT COALESCE(min(priority),-1) AS priority FROM root_candidates), all_seeds AS (
	SELECT s.id,s.file_path,s.source_line FROM developa_symbols s WHERE s.repository_id=$1 AND s.snapshot_id=$2 AND s.id=$3
	UNION ALL
	SELECT s.id,s.file_path,s.source_line FROM selected_feature f JOIN developa_feature_evidence e
	ON e.repository_id=f.repository_id AND e.snapshot_id=f.snapshot_id AND e.run_id=f.run_id AND e.feature_id=f.id
	JOIN developa_symbols s ON s.repository_id=e.repository_id AND s.snapshot_id=e.snapshot_id AND s.id=e.symbol_id
	UNION ALL
	SELECT r.id,r.file_path,r.source_line FROM root_candidates r,root_class c WHERE r.priority=c.priority
	AND (c.priority<2 OR r.id=(SELECT id FROM root_candidates ORDER BY file_path,source_line,id LIMIT 1))
), bounded_seeds AS (
	SELECT id,row_number() OVER(ORDER BY file_path,source_line,id) AS ordinal FROM all_seeds
	ORDER BY file_path,source_line,id LIMIT $6+1
), seeds AS (
	SELECT COALESCE(array_agg(id ORDER BY ordinal) FILTER(WHERE ordinal<=$6),ARRAY[]::text[]) AS ids,count(*)>$6 AS truncated FROM bounded_seeds
), upward(depth,visited,frontier,discovery_ids,truncated) AS (
	SELECT 0,ids,ids,ARRAY[]::text[],truncated FROM seeds
	UNION ALL
	SELECT w.depth+1,w.visited || n.ids,n.ids,w.discovery_ids || n.edge_ids,w.truncated OR n.overflow
	FROM upward w CROSS JOIN LATERAL (
		SELECT COALESCE(array_agg(id ORDER BY id) FILTER(WHERE ordinal<=$6-cardinality(w.visited)),ARRAY[]::text[]) AS ids,
			COALESCE(array_agg(edge_id ORDER BY id) FILTER(WHERE ordinal<=$6-cardinality(w.visited)),ARRAY[]::text[]) AS edge_ids,
			count(*)>$6-cardinality(w.visited) AS overflow FROM (
			SELECT id,edge_id,row_number() OVER(ORDER BY id) AS ordinal FROM (
				SELECT DISTINCT ON(c.caller_id) c.caller_id AS id,c.id AS edge_id,c.path,c.source_line FROM developa_calls c
				WHERE c.repository_id=$1 AND c.snapshot_id=$2 AND c.resolution='resolved' AND c.target_id=ANY(w.frontier)
				AND NOT c.caller_id=ANY(w.visited) ORDER BY c.caller_id,c.path,c.source_line,c.id
				LIMIT GREATEST($6-cardinality(w.visited),0)+1
			) candidates
		) numbered
	) n WHERE w.depth<$5 AND cardinality(w.frontier)>0 AND cardinality(w.visited)<$6
), ancestors AS (SELECT * FROM upward ORDER BY depth DESC LIMIT 1), downward(depth,visited,seen,frontier,discovery_ids,truncated) AS (
	SELECT 0,a.visited,s.ids,s.ids,a.discovery_ids,a.truncated FROM ancestors a,seeds s
	UNION ALL
	SELECT w.depth+1,w.visited || n.new_ids,w.seen || n.ids,n.ids,w.discovery_ids || n.edge_ids,w.truncated OR n.overflow
	FROM downward w CROSS JOIN LATERAL (
		SELECT COALESCE(array_agg(id ORDER BY existing DESC,id) FILTER(WHERE existing OR ordinal<=$6-cardinality(w.visited)),ARRAY[]::text[]) AS ids,
			COALESCE(array_agg(id ORDER BY id) FILTER(WHERE NOT existing AND ordinal<=$6-cardinality(w.visited)),ARRAY[]::text[]) AS new_ids,
			COALESCE(array_agg(edge_id ORDER BY id) FILTER(WHERE NOT existing AND ordinal<=$6-cardinality(w.visited)),ARRAY[]::text[]) AS edge_ids,
			count(*) FILTER(WHERE NOT existing)>$6-cardinality(w.visited) AS overflow FROM (
			SELECT id,edge_id,existing,count(*) FILTER(WHERE NOT existing) OVER(ORDER BY existing DESC,id) AS ordinal FROM (
				SELECT DISTINCT ON(c.target_id=ANY(w.visited),c.target_id) c.target_id AS id,c.id AS edge_id,c.target_id=ANY(w.visited) AS existing
				FROM developa_calls c WHERE c.repository_id=$1 AND c.snapshot_id=$2 AND c.resolution='resolved'
				AND c.caller_id=ANY(w.frontier) AND NOT c.target_id=ANY(w.seen)
				ORDER BY c.target_id=ANY(w.visited) DESC,c.target_id,c.path,c.source_line,c.id LIMIT $6+1
			) candidates
		) numbered
	) n WHERE w.depth<$5 AND cardinality(w.frontier)>0
), final AS (SELECT * FROM downward ORDER BY depth DESC LIMIT 1), edge_candidates AS MATERIALIZED (
	SELECT c.id,c.path,c.source_line,c.payload,array_position(f.discovery_ids,c.id) AS discovery_order
	FROM developa_calls c,final f WHERE c.repository_id=$1 AND c.snapshot_id=$2 AND c.resolution='resolved'
	AND c.caller_id=ANY(f.visited) AND c.target_id=ANY(f.visited)
	ORDER BY discovery_order NULLS LAST,c.path,c.source_line,c.id LIMIT $6*4+1
), edges AS (SELECT * FROM edge_candidates ORDER BY discovery_order NULLS LAST,path,source_line,id LIMIT $6*4), nodes AS (
	SELECT s.id,array_position(w.visited,s.id) AS ordinal,jsonb_build_object('path',s.file_path,'symbol',s.payload,'review',review.payload,'seed',s.id=ANY(seeds.ids),
		'incoming_count',COALESCE(i.amount,0),'outgoing_count',COALESCE(o.amount,0),'unresolved_count',COALESCE(o.unresolved,0),
		'root_kind',CASE WHEN s.kind='function' AND s.name='main' AND file.package='main' THEN 'main'
			WHEN s.kind='function' AND s.name='init' THEN 'init'
			WHEN s.kind IN ('function','method','closure') AND COALESCE(i.amount,0)=0 THEN 'candidate'
			WHEN EXISTS(SELECT 1 FROM developa_calls c WHERE c.repository_id=$1 AND c.snapshot_id=$2 AND c.resolution='resolved'
				AND c.target_id=s.id AND NOT c.caller_id=ANY(w.visited)) THEN 'boundary' ELSE '' END) AS item
	FROM developa_symbols s CROSS JOIN final w CROSS JOIN seeds JOIN developa_files file
	ON file.repository_id=s.repository_id AND file.snapshot_id=s.snapshot_id AND file.path=s.file_path
	LEFT JOIN incoming i ON i.id=s.id LEFT JOIN outgoing o ON o.id=s.id
	LEFT JOIN developa_function_reviews review ON review.repository_id=s.repository_id AND review.snapshot_id=s.snapshot_id AND review.symbol_id=s.id
	WHERE s.repository_id=$1 AND s.snapshot_id=$2 AND s.id=ANY(w.visited)
)
SELECT EXISTS(SELECT 1 FROM developa_snapshots WHERE repository_id=$1 AND id=$2)
	AND ($3='' OR EXISTS(SELECT 1 FROM developa_symbols WHERE repository_id=$1 AND snapshot_id=$2 AND id=$3))
	AND ($4='' OR EXISTS(SELECT 1 FROM selected_feature)),
	(SELECT to_jsonb(ids) FROM seeds),COALESCE((SELECT jsonb_agg(item ORDER BY ordinal) FROM nodes),'[]'::jsonb),
	COALESCE((SELECT jsonb_agg(payload ORDER BY discovery_order NULLS LAST,path,source_line,id) FROM edges),'[]'::jsonb),
	(SELECT truncated FROM final) OR (SELECT count(*)>$6*4 FROM edge_candidates) OR EXISTS(
		SELECT 1 FROM developa_calls c,ancestors a WHERE c.repository_id=$1 AND c.snapshot_id=$2 AND c.resolution='resolved'
		AND c.target_id=ANY(a.frontier) AND NOT c.caller_id=ANY(a.visited)) OR EXISTS(
		SELECT 1 FROM developa_calls c,final f WHERE c.repository_id=$1 AND c.snapshot_id=$2 AND c.resolution='resolved'
		AND c.caller_id=ANY(f.frontier) AND NOT c.target_id=ANY(f.seen)),
	(SELECT priority FROM root_class),COALESCE((SELECT details#>>'{call_analysis,status}'='complete'
		AND (metadata->>'source_complete')::boolean AND metadata->>'completeness'='complete'
		FROM developa_snapshots WHERE repository_id=$1 AND id=$2),false)`
