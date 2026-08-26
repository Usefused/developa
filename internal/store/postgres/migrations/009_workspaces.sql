CREATE TABLE developa_workspaces (
    repository_id text PRIMARY KEY REFERENCES developa_repositories(id),
    root text NOT NULL UNIQUE CHECK (length(root) BETWEEN 1 AND 4096),
    added_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX developa_workspaces_order_idx ON developa_workspaces (added_at, repository_id);
