-- Identical source content can recur with different transition evidence. Snapshot
-- identity now includes the execution, while fingerprint remains content identity.
ALTER TABLE developa_snapshots
    DROP CONSTRAINT developa_snapshots_repository_id_fingerprint_key;

CREATE INDEX developa_snapshots_fingerprint_idx
    ON developa_snapshots (repository_id, fingerprint);
