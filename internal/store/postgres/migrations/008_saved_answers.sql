-- Existing answer rows have no request identity; do not guess which question
-- they answer. A subsequent explicit cached/generation request records the key.
ALTER TABLE developa_answers ADD COLUMN context_key text CHECK (context_key ~ '^[0-9a-f]{64}$');
ALTER TABLE developa_answers ADD COLUMN stored_at timestamptz NOT NULL DEFAULT clock_timestamp();
CREATE INDEX developa_answers_context_idx ON developa_answers (repository_id,context_key,stored_at DESC,id DESC)
    WHERE context_key IS NOT NULL;
