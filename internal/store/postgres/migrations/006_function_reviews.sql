CREATE TABLE developa_function_reviews (
    repository_id text NOT NULL,
    snapshot_id text NOT NULL,
    symbol_id text NOT NULL,
    payload jsonb NOT NULL,
    PRIMARY KEY (repository_id,snapshot_id,symbol_id),
    FOREIGN KEY (repository_id,snapshot_id,symbol_id) REFERENCES developa_symbols(repository_id,snapshot_id,id)
);

CREATE TABLE developa_function_review_cache (
    repository_id text NOT NULL REFERENCES developa_repositories(id),
    cache_key text NOT NULL,
    model text NOT NULL,
    payload jsonb NOT NULL,
    PRIMARY KEY (repository_id,cache_key)
);
