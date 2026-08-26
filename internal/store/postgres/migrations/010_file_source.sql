-- Retain captured file bytes once per immutable snapshot, independently of the
-- deliberately bounded symbol preview. NULL identifies pre-capture snapshots.
ALTER TABLE developa_files ADD COLUMN source bytea;
