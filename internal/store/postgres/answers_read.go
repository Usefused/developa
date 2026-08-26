package postgres

import (
	"context"

	"developa/internal/domain"
)

var _ domain.SavedAnswerStore = (*Store)(nil)

func (s *Store) SavedAnswer(ctx context.Context, repository, snapshot, key string) (answer domain.Answer, err error) {
	ctx, done := operation(ctx, "postgres.saved_answer")
	defer func() { done(err) }()
	if !hexID.MatchString(snapshot) || !hexID.MatchString(key) {
		return answer, domain.ErrInvalidInput
	}
	var payload []byte
	err = s.pool.QueryRow(ctx, savedAnswerSQL, repository, snapshot, key).Scan(&payload)
	if err != nil {
		return answer, databaseError(err)
	}
	err = decodeJSON(payload, &answer)
	return answer, err
}

// Read one indexed match and rebind its bounded citations in the same statement.
// A missing or changed cited declaration cannot silently disappear from proof.
const savedAnswerSQL = `WITH selected AS MATERIALIZED (
	SELECT * FROM developa_answers WHERE repository_id=$1 AND context_key=$3
	ORDER BY stored_at DESC,id DESC LIMIT 1
), evidence AS (
	SELECT s.id,s.file_path,s.name,s.payload FROM selected a
	JOIN developa_answer_evidence e ON e.repository_id=a.repository_id AND e.snapshot_id=a.snapshot_id AND e.answer_id=a.id
	JOIN developa_symbols old ON old.repository_id=e.repository_id AND old.snapshot_id=e.snapshot_id AND old.id=e.symbol_id
	JOIN developa_symbols s ON s.repository_id=$1 AND s.snapshot_id=$2 AND s.id=e.symbol_id
	AND s.payload->>'content_hash'=old.payload->>'content_hash'
)
SELECT a.metadata || jsonb_build_object('snapshot_id',$2::text,'generated_snapshot_id',a.snapshot_id,'cached',true,
	'evidence',COALESCE((SELECT jsonb_agg(jsonb_build_object('symbol_id',id,'path',file_path,'name',name,'span',payload->'span')
	ORDER BY file_path,id) FROM evidence),'[]'::jsonb))
FROM selected a WHERE EXISTS(SELECT 1 FROM developa_snapshots WHERE repository_id=$1 AND id=$2)
	AND (SELECT count(*) FROM evidence)=(SELECT count(*) FROM developa_answer_evidence
	WHERE repository_id=a.repository_id AND snapshot_id=a.snapshot_id AND answer_id=a.id)`
