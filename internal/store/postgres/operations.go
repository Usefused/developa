package postgres

import (
	"context"
	"errors"
	"time"

	"developa/internal/domain"
	"developa/internal/telemetry"
	"github.com/jackc/pgx/v5"
	"go.opentelemetry.io/otel"
)

var ErrInvalidInput = errors.New("invalid catalog input")

func operation(ctx context.Context, name string) (context.Context, func(error)) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	ctx, span := otel.Tracer("developa/postgres").Start(ctx, name, databaseAttributes())
	span.AddEvent("execution.started")
	return ctx, func(err error) {
		defer cancel()
		defer span.End()
		if err != nil {
			telemetry.Fail(span, "catalog_operation_failed")
			return
		}
		span.AddEvent("execution.completed")
	}
}

func databaseError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.ErrNotFound
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	// Constraint errors can embed source-bearing row values; keep driver details private.
	return ErrUnavailable
}

func rollback(tx pgx.Tx) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = tx.Rollback(ctx)
}

func boundedFilter(filter domain.Filter) domain.Filter {
	if filter.Limit <= 0 {
		filter.Limit = 50
	}
	if filter.Limit > 100 {
		filter.Limit = 100
	}
	if filter.Offset < 0 {
		filter.Offset = 0
	}
	return filter
}
