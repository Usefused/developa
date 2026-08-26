ALTER TABLE developa_analysis_jobs ADD COLUMN commit_id text NOT NULL DEFAULT '';
UPDATE developa_analysis_jobs j SET commit_id=COALESCE(s.metadata->>'commit','')
FROM developa_snapshots s WHERE s.repository_id=j.repository_id AND s.id=j.snapshot_id;

WITH changed AS (
    UPDATE developa_analysis_jobs j SET status='superseded',lease_token=NULL,lease_until=NULL,updated_at=clock_timestamp()
    FROM developa_snapshots s WHERE s.repository_id=j.repository_id AND s.id=j.snapshot_id
    AND j.automatic AND j.status IN ('queued','running')
    AND (j.commit_id='' OR COALESCE((s.metadata->>'dirty')::boolean,true)
         OR NOT COALESCE((s.metadata->>'source_complete')::boolean,false)) RETURNING j.*
), events AS (
    INSERT INTO developa_audit_events(repository_id,snapshot_id,execution_id,actor,trigger,trace_id,outcome,counts,job_id)
    SELECT repository_id,snapshot_id,execution_id,actor,trigger,trace_id,'superseded',jsonb_build_object('jobs',1),id FROM changed RETURNING id
)
INSERT INTO developa_audit_outbox(event_id) SELECT id FROM events;

-- Keep admission separate from mutable job state: a manual promotion or retry
-- must never make the same commit eligible for a second automatic generation.
CREATE TABLE developa_analysis_commits (
    repository_id text NOT NULL,
    commit_id text NOT NULL,
    job_id text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    PRIMARY KEY(repository_id,commit_id),
    FOREIGN KEY(repository_id,job_id) REFERENCES developa_analysis_jobs(repository_id,id)
);
INSERT INTO developa_analysis_commits(repository_id,commit_id,job_id)
SELECT DISTINCT ON (j.repository_id,j.commit_id) j.repository_id,j.commit_id,j.id
FROM developa_analysis_jobs j JOIN developa_snapshots s ON s.repository_id=j.repository_id AND s.id=j.snapshot_id
WHERE j.commit_id<>'' AND NOT COALESCE((s.metadata->>'dirty')::boolean,true)
AND COALESCE((s.metadata->>'source_complete')::boolean,false)
AND (j.automatic OR EXISTS(SELECT 1 FROM developa_audit_events e WHERE e.repository_id=j.repository_id AND e.job_id=j.id AND e.trigger='feature_auto'))
ORDER BY j.repository_id,j.commit_id,j.created_at;

CREATE TABLE developa_analysis_cache (
    repository_id text NOT NULL REFERENCES developa_repositories(id),
    cache_key text NOT NULL CHECK(length(cache_key)=64),
    model text NOT NULL CHECK(octet_length(model) BETWEEN 1 AND 200),
    payload jsonb NOT NULL CHECK(octet_length(payload::text)<=262144),
    created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    PRIMARY KEY(repository_id,cache_key)
);
