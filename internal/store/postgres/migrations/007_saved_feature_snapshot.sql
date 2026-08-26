-- Most working-tree snapshots have no AI run. Saved-analysis discovery should
-- walk only analyzed snapshots, not the entire source-tracking history.
CREATE INDEX developa_saved_feature_snapshot_idx ON developa_snapshots
    (repository_id, indexed_at DESC, id DESC) WHERE latest_feature_run_id IS NOT NULL;
