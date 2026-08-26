CREATE TABLE developa_analysis_jobs (
    repository_id text NOT NULL,
    snapshot_id text NOT NULL,
    id text NOT NULL,
    base_run_id text,
    status text NOT NULL CHECK (status IN ('queued','running','completed','failed','superseded')),
    automatic boolean NOT NULL,
    attempts integer NOT NULL DEFAULT 0 CHECK (attempts BETWEEN 0 AND 3),
    chunks integer NOT NULL DEFAULT 0 CHECK (chunks >= 0),
    analyzed_symbols integer NOT NULL DEFAULT 0 CHECK (analyzed_symbols >= 0),
    total_symbols integer NOT NULL CHECK (total_symbols >= 0),
    feature_count integer NOT NULL DEFAULT 0 CHECK (feature_count >= 0),
    error_code text NOT NULL DEFAULT '',
    available_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    lease_until timestamptz,
    lease_token text,
    execution_id text NOT NULL,
    actor text NOT NULL,
    trigger text NOT NULL,
    trace_id text NOT NULL,
    trace_parent text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    PRIMARY KEY (repository_id,snapshot_id),
    UNIQUE (repository_id,id),
    FOREIGN KEY (repository_id,snapshot_id) REFERENCES developa_snapshots(repository_id,id),
    FOREIGN KEY (repository_id,snapshot_id,base_run_id) REFERENCES developa_feature_runs(repository_id,snapshot_id,id),
    CHECK ((status='running') = (lease_token IS NOT NULL AND lease_until IS NOT NULL))
);
CREATE INDEX developa_analysis_jobs_claim_idx ON developa_analysis_jobs
    (repository_id,status,available_at,created_at);
CREATE INDEX developa_analysis_jobs_expiry_idx ON developa_analysis_jobs
    (repository_id,lease_until) WHERE status='running';

ALTER TABLE developa_audit_events ADD COLUMN job_id text;
ALTER TABLE developa_audit_events ADD CONSTRAINT developa_audit_job_fk
    FOREIGN KEY (repository_id,job_id) REFERENCES developa_analysis_jobs(repository_id,id);
