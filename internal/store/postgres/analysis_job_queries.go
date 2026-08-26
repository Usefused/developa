package postgres

// Keep lease secrets separate from the public JSON representation. Job origin
// identity is only used for durable audit records, never returned as API data.
const analysisJobJSON = `((to_jsonb(j)-ARRAY['repository_id','execution_id','actor','trigger','trace_id','trace_parent','lease_token','commit_id']) || jsonb_build_object('commit',j.commit_id))`

const analysisStatusSQL = `SELECT CASE WHEN j.id IS NULL THEN jsonb_build_object(
	'snapshot_id',s.id,'commit',s.metadata->>'commit','status','not_queued','total_symbols',(s.metadata->>'symbol_count')::integer)
	ELSE ` + analysisJobJSON + ` END,COALESCE(j.trace_parent,''),''
	FROM developa_snapshots s LEFT JOIN developa_analysis_jobs j ON j.repository_id=s.repository_id AND j.snapshot_id=s.id
	WHERE s.repository_id=$1 AND s.id=$2`

const enqueueAnalysisSQL = `WITH inserted AS (
	INSERT INTO developa_analysis_jobs AS existing (repository_id,snapshot_id,id,base_run_id,status,automatic,
		analyzed_symbols,total_symbols,feature_count,execution_id,actor,trigger,trace_id,trace_parent,commit_id)
	SELECT s.repository_id,s.id,$3,s.latest_feature_run_id,
		CASE WHEN $4 AND r.latest_snapshot_id IS DISTINCT FROM s.id THEN 'superseded' ELSE 'queued' END,$4,
		COALESCE((f.metadata->>'analyzed_symbols')::integer,0),(s.metadata->>'symbol_count')::integer,
		COALESCE((f.metadata->>'feature_count')::integer,0),$5,$6,$7,$8,$9,COALESCE(s.metadata->>'commit','')
	FROM developa_snapshots s JOIN developa_repositories r ON r.id=s.repository_id
	LEFT JOIN developa_feature_runs f ON f.repository_id=s.repository_id AND f.snapshot_id=s.id AND f.id=s.latest_feature_run_id
	WHERE s.repository_id=$1 AND s.id=$2
	ON CONFLICT (repository_id,snapshot_id) DO UPDATE SET base_run_id=EXCLUDED.base_run_id,status='queued',automatic=false,
		attempts=0,chunks=0,analyzed_symbols=EXCLUDED.analyzed_symbols,total_symbols=EXCLUDED.total_symbols,
		feature_count=EXCLUDED.feature_count,error_code='',available_at=clock_timestamp(),lease_until=NULL,lease_token=NULL,
		execution_id=$5,actor=$6,trigger=$7,trace_id=$8,trace_parent=$9,updated_at=clock_timestamp()
	WHERE NOT $4 AND existing.status IN ('completed','failed','superseded') RETURNING *
), result AS (
	SELECT *,true AS changed FROM inserted UNION ALL
	SELECT *,false AS changed FROM developa_analysis_jobs WHERE repository_id=$1 AND snapshot_id=$2 AND NOT EXISTS(SELECT 1 FROM inserted)
) SELECT ` + analysisJobJSON + `,j.trace_parent,j.changed FROM result j`

const jobAuditCTE = `, events AS (
	INSERT INTO developa_audit_events(repository_id,snapshot_id,execution_id,actor,trigger,trace_id,outcome,counts,job_id)
	SELECT repository_id,snapshot_id,execution_id,actor,trigger,trace_id,
		CASE WHEN status='failed' THEN 'error' ELSE status END,
		jsonb_build_object('jobs',1,'attempts',attempts,'chunks',chunks),id FROM changed RETURNING id
), outbox AS (INSERT INTO developa_audit_outbox(event_id) SELECT id FROM events RETURNING event_id)
`

const supersedeAnalysisSQL = `WITH changed AS (
	UPDATE developa_analysis_jobs j SET status='superseded',updated_at=clock_timestamp()
	WHERE repository_id=$1 AND snapshot_id<>$2 AND automatic AND status='queued'
	AND commit_id IS DISTINCT FROM (SELECT metadata->>'commit' FROM developa_snapshots WHERE repository_id=$1 AND id=$2)
	AND EXISTS(SELECT 1 FROM developa_repositories WHERE id=$1 AND latest_snapshot_id=$2) RETURNING j.*
)` + jobAuditCTE + `SELECT count(*) FROM changed`

const expireAnalysisSQL = `WITH expired AS (
	SELECT repository_id,snapshot_id FROM developa_analysis_jobs WHERE repository_id=$1 AND status='running'
	AND ($2 OR NOT automatic)
	AND lease_until<=clock_timestamp() AND attempts>=2 ORDER BY lease_until LIMIT 100 FOR UPDATE SKIP LOCKED
), changed AS (
	UPDATE developa_analysis_jobs j SET status='failed',attempts=3,error_code='lease_expired',lease_token=NULL,lease_until=NULL,
	updated_at=clock_timestamp() FROM expired e WHERE j.repository_id=e.repository_id AND j.snapshot_id=e.snapshot_id RETURNING j.*
)` + jobAuditCTE + `SELECT count(*) FROM changed`

const claimAnalysisSQL = `WITH candidate AS (
	SELECT repository_id,snapshot_id FROM developa_analysis_jobs WHERE repository_id=$1 AND
	($4 OR NOT automatic) AND
	((status='queued' AND available_at<=clock_timestamp()) OR (status='running' AND lease_until<=clock_timestamp() AND attempts<2))
	ORDER BY available_at,created_at,id LIMIT 1 FOR UPDATE SKIP LOCKED
), changed AS (
	UPDATE developa_analysis_jobs j SET status='running',
		attempts=j.attempts+CASE WHEN j.status='running' THEN 1 ELSE 0 END,
		lease_token=$2,lease_until=clock_timestamp()+($3::double precision * interval '1 second'),updated_at=clock_timestamp()
	FROM candidate c WHERE j.repository_id=c.repository_id AND j.snapshot_id=c.snapshot_id RETURNING j.*
)` + jobAuditCTE + `SELECT ` + analysisJobJSON + `,j.trace_parent,j.lease_token FROM changed j`
