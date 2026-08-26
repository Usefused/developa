CREATE TABLE developa_implementations (
    repository_id text NOT NULL,
    snapshot_id text NOT NULL,
    interface_id text NOT NULL,
    method_id text NOT NULL,
    receiver_id text NOT NULL,
    target_id text NOT NULL,
    payload jsonb NOT NULL,
    PRIMARY KEY (repository_id,snapshot_id,interface_id,method_id,receiver_id,target_id),
    FOREIGN KEY (repository_id,snapshot_id,interface_id) REFERENCES developa_symbols(repository_id,snapshot_id,id),
    FOREIGN KEY (repository_id,snapshot_id,method_id) REFERENCES developa_symbols(repository_id,snapshot_id,id),
    FOREIGN KEY (repository_id,snapshot_id,receiver_id) REFERENCES developa_symbols(repository_id,snapshot_id,id),
    FOREIGN KEY (repository_id,snapshot_id,target_id) REFERENCES developa_symbols(repository_id,snapshot_id,id)
);
CREATE INDEX developa_implementations_method_idx ON developa_implementations
    (repository_id,snapshot_id,method_id,receiver_id,target_id);

-- Candidates deliberately do not populate developa_calls.target_id: that would
-- silently turn possible implementations into resolved call-graph edges.
