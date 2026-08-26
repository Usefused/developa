CREATE TABLE developa_calls (
    repository_id text NOT NULL,
    snapshot_id text NOT NULL,
    id text NOT NULL,
    caller_id text NOT NULL,
    target_id text,
    resolution text NOT NULL CHECK (resolution IN ('resolved','unresolved','external','builtin')),
    path text NOT NULL,
    source_line integer NOT NULL,
    payload jsonb NOT NULL,
    PRIMARY KEY (repository_id,snapshot_id,id),
    FOREIGN KEY (repository_id,snapshot_id,caller_id) REFERENCES developa_symbols(repository_id,snapshot_id,id),
    FOREIGN KEY (repository_id,snapshot_id,target_id) REFERENCES developa_symbols(repository_id,snapshot_id,id),
    CHECK ((resolution='resolved') = (target_id IS NOT NULL))
);
CREATE INDEX developa_calls_caller_idx ON developa_calls (repository_id,snapshot_id,caller_id,target_id);
CREATE INDEX developa_calls_target_idx ON developa_calls (repository_id,snapshot_id,target_id,caller_id);

ALTER TABLE developa_symbols ADD COLUMN search_document tsvector GENERATED ALWAYS AS (
    to_tsvector('simple',name || ' ' || COALESCE(payload->>'signature','') || ' ' ||
        COALESCE(payload->>'doc','') || ' ' || COALESCE(payload->>'source',''))
) STORED;
CREATE INDEX developa_symbols_search_idx ON developa_symbols USING gin(search_document);

CREATE TABLE developa_feature_runs (
    repository_id text NOT NULL,
    snapshot_id text NOT NULL,
    id text NOT NULL,
    metadata jsonb NOT NULL,
    PRIMARY KEY (repository_id,snapshot_id,id),
    FOREIGN KEY (repository_id,snapshot_id) REFERENCES developa_snapshots(repository_id,id)
);
ALTER TABLE developa_snapshots ADD COLUMN latest_feature_run_id text;
ALTER TABLE developa_snapshots ADD CONSTRAINT developa_latest_feature_run_fk
    FOREIGN KEY (repository_id,id,latest_feature_run_id) REFERENCES developa_feature_runs(repository_id,snapshot_id,id);

CREATE TABLE developa_features (
    repository_id text NOT NULL,
    snapshot_id text NOT NULL,
    run_id text NOT NULL,
    id text NOT NULL,
    title text NOT NULL,
    summary text NOT NULL,
    status text NOT NULL CHECK (status='inferred'),
    evidence_ids text[] NOT NULL,
    PRIMARY KEY (repository_id,snapshot_id,run_id,id),
    FOREIGN KEY (repository_id,snapshot_id,run_id) REFERENCES developa_feature_runs(repository_id,snapshot_id,id)
);
CREATE TABLE developa_feature_evidence (
    repository_id text NOT NULL,
    snapshot_id text NOT NULL,
    run_id text NOT NULL,
    feature_id text NOT NULL,
    symbol_id text NOT NULL,
    PRIMARY KEY (repository_id,snapshot_id,run_id,feature_id,symbol_id),
    FOREIGN KEY (repository_id,snapshot_id,run_id,feature_id) REFERENCES developa_features(repository_id,snapshot_id,run_id,id),
    FOREIGN KEY (repository_id,snapshot_id,symbol_id) REFERENCES developa_symbols(repository_id,snapshot_id,id)
);
CREATE TABLE developa_answers (
    repository_id text NOT NULL,
    snapshot_id text NOT NULL,
    id text NOT NULL,
    metadata jsonb NOT NULL,
    PRIMARY KEY (repository_id,snapshot_id,id),
    FOREIGN KEY (repository_id,snapshot_id) REFERENCES developa_snapshots(repository_id,id)
);
CREATE TABLE developa_answer_evidence (
    repository_id text NOT NULL,
    snapshot_id text NOT NULL,
    answer_id text NOT NULL,
    symbol_id text NOT NULL,
    PRIMARY KEY (repository_id,snapshot_id,answer_id,symbol_id),
    FOREIGN KEY (repository_id,snapshot_id,answer_id) REFERENCES developa_answers(repository_id,snapshot_id,id),
    FOREIGN KEY (repository_id,snapshot_id,symbol_id) REFERENCES developa_symbols(repository_id,snapshot_id,id)
);
