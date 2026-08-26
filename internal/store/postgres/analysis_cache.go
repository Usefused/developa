package postgres

import (
	"context"
	"encoding/json"

	"developa/internal/domain"
	"go.opentelemetry.io/otel/trace"
)

var _ domain.AnalysisCacheStore = (*Store)(nil)

func (s *Store) CachedAnalysis(ctx context.Context, repositoryID, key string) (payload json.RawMessage, err error) {
	ctx, done := operation(ctx, "postgres.cached_analysis")
	defer func() { done(err) }()
	if !hexID.MatchString(key) {
		return nil, domain.ErrInvalidInput
	}
	err = s.pool.QueryRow(ctx, `SELECT payload FROM developa_analysis_cache WHERE repository_id=$1 AND cache_key=$2`, repositoryID, key).Scan(&payload)
	return payload, databaseError(err)
}

func (s *Store) CacheAnalysis(ctx context.Context, repositoryID, key, model string, payload json.RawMessage) (err error) {
	ctx, done := operation(ctx, "postgres.cache_analysis")
	defer func() { done(err) }()
	if !hexID.MatchString(key) || !validModel(model) || !validCachePayload(payload) {
		return domain.ErrInvalidInput
	}
	// Only validated output is cached by the application. Cache writes survive a
	// later publication rollback so retrying the same evidence need not infer again.
	traceID := trace.SpanContextFromContext(ctx).TraceID().String()
	_, err = s.pool.Exec(ctx, cacheAnalysisSQL, repositoryID, key, model, payload, traceID)
	return databaseError(err)
}

func validCachePayload(payload json.RawMessage) bool {
	return len(payload) > 0 && len(payload) <= 256*1024 && json.Valid(payload)
}

const cacheAnalysisSQL = `WITH cached AS (
	INSERT INTO developa_analysis_cache(repository_id,cache_key,model,payload) VALUES ($1,$2,$3,$4)
	ON CONFLICT(repository_id,cache_key) DO NOTHING RETURNING repository_id
), event AS (
	INSERT INTO developa_audit_events(repository_id,execution_id,actor,trigger,trace_id,outcome,counts)
	SELECT repository_id,'cache-'||$2,'system','analysis_cache',$5,'completed',jsonb_build_object('cache_entries',1) FROM cached RETURNING id
) INSERT INTO developa_audit_outbox(event_id) SELECT id FROM event`
