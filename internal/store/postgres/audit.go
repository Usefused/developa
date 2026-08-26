package postgres

import (
	"context"
	"encoding/json"
	"regexp"

	"developa/internal/domain"
	"github.com/jackc/pgx/v5/pgconn"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

type auditWriter interface {
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
}

var auditToken = regexp.MustCompile(`^[A-Za-z0-9_.:-]{1,128}$`)

func (s *Store) RecordExecution(ctx context.Context, repositoryID string, execution domain.Execution, outcome string) (err error) {
	ctx, done := operation(ctx, "postgres.record_execution")
	defer func() { done(err) }()
	if !validExecution(execution, outcome) {
		return ErrInvalidInput
	}
	traceExecution(ctx, repositoryID, execution)
	err = databaseError(appendAudit(ctx, s.pool, repositoryID, nil, execution, outcome, map[string]int{}))
	if err == nil {
		trace.SpanFromContext(ctx).AddEvent("audit.recorded", trace.WithAttributes(attribute.String("execution.outcome", outcome)))
	}
	return err
}

func traceExecution(ctx context.Context, repositoryID string, execution domain.Execution) {
	trace.SpanFromContext(ctx).SetAttributes(attribute.String("repository.id", repositoryID),
		attribute.String("execution.id", execution.ID), attribute.String("execution.actor", execution.Actor),
		attribute.String("execution.trigger", execution.Trigger))
	if execution.JobID != "" {
		trace.SpanFromContext(ctx).SetAttributes(attribute.String("job.id", execution.JobID))
	}
}

func validExecution(execution domain.Execution, outcome string) bool {
	if !auditToken.MatchString(execution.ID) || !auditToken.MatchString(execution.Trigger) {
		return false
	}
	if execution.TraceID != "" && !auditToken.MatchString(execution.TraceID) {
		return false
	}
	return validActor(execution.Actor) && validOutcome(outcome)
}

func validActor(actor string) bool {
	switch actor {
	case "user", "agent", "system", "unknown", "operator":
		return true
	default:
		return false
	}
}

func validOutcome(outcome string) bool {
	switch outcome {
	case "queued", "running", "completed", "error", "failed", "canceled", "cancelled":
		return true
	default:
		return false
	}
}

func appendAudit(ctx context.Context, writer auditWriter, repositoryID string, snapshotID *string, execution domain.Execution, outcome string, counts map[string]int) error {
	payload, err := json.Marshal(counts)
	if err != nil {
		return err
	}
	_, err = writer.Exec(ctx, `WITH event AS (
		INSERT INTO developa_audit_events
		(repository_id,snapshot_id,execution_id,actor,trigger,trace_id,outcome,counts,job_id)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,NULLIF($9,'')) RETURNING id
	) INSERT INTO developa_audit_outbox (event_id) SELECT id FROM event`,
		repositoryID, snapshotID, execution.ID, execution.Actor, execution.Trigger, execution.TraceID, outcome, payload, execution.JobID)
	return err
}
