CREATE TABLE developa_repositories (
    id text PRIMARY KEY,
    name text NOT NULL,
    latest_snapshot_id text,
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE developa_snapshots (
    repository_id text NOT NULL REFERENCES developa_repositories(id),
    id text NOT NULL,
    fingerprint text NOT NULL,
    metadata jsonb NOT NULL,
    details jsonb NOT NULL,
    indexed_at timestamptz NOT NULL,
    PRIMARY KEY (repository_id, id),
    UNIQUE (repository_id, fingerprint)
);

ALTER TABLE developa_repositories ADD CONSTRAINT developa_latest_snapshot_fk
    FOREIGN KEY (id, latest_snapshot_id) REFERENCES developa_snapshots(repository_id, id);

CREATE TABLE developa_files (
    repository_id text NOT NULL,
    snapshot_id text NOT NULL,
    path text NOT NULL,
    package text NOT NULL,
    overview text NOT NULL,
    completeness text NOT NULL,
    symbol_count integer NOT NULL CHECK (symbol_count >= 0),
    kinds jsonb NOT NULL,
    doc text NOT NULL,
    imports jsonb NOT NULL,
    PRIMARY KEY (repository_id, snapshot_id, path),
    FOREIGN KEY (repository_id, snapshot_id) REFERENCES developa_snapshots(repository_id, id)
);

CREATE TABLE developa_symbols (
    repository_id text NOT NULL,
    snapshot_id text NOT NULL,
    id text NOT NULL,
    file_path text NOT NULL,
    name text NOT NULL,
    kind text NOT NULL,
    source_line integer NOT NULL,
    payload jsonb NOT NULL,
    PRIMARY KEY (repository_id, snapshot_id, id),
    FOREIGN KEY (repository_id, snapshot_id, file_path)
        REFERENCES developa_files(repository_id, snapshot_id, path)
);

CREATE INDEX developa_symbols_file_idx ON developa_symbols
    (repository_id, snapshot_id, file_path, source_line, id);
CREATE INDEX developa_symbols_kind_idx ON developa_symbols
    (repository_id, snapshot_id, kind, name, id);

CREATE TABLE developa_audit_events (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    repository_id text NOT NULL REFERENCES developa_repositories(id),
    snapshot_id text,
    execution_id text NOT NULL,
    actor text NOT NULL,
    trigger text NOT NULL,
    trace_id text NOT NULL,
    outcome text NOT NULL,
    counts jsonb NOT NULL DEFAULT '{}',
    created_at timestamptz NOT NULL DEFAULT now(),
    FOREIGN KEY (repository_id, snapshot_id) REFERENCES developa_snapshots(repository_id, id)
);

CREATE INDEX developa_audit_execution_idx ON developa_audit_events
    (repository_id, execution_id, id);

-- An outbox preserves exportable event identities independently of trace sampling.
-- Dispatch and retention are separate workers; no source payload is duplicated here.
CREATE TABLE developa_audit_outbox (
    event_id bigint PRIMARY KEY REFERENCES developa_audit_events(id),
    created_at timestamptz NOT NULL DEFAULT now(),
    delivered_at timestamptz
);
