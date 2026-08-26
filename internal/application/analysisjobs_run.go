package application

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"developa/internal/domain"
	"developa/internal/telemetry"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"
)

const analysisDBTimeout = 5 * time.Second

func (w *AnalysisWorker) run(ctx context.Context) {
	defer w.wg.Done()
	reconciled := false
	for ctx.Err() == nil {
		if !reconciled {
			reconciled = w.reconcile(ctx) == nil
		}
		if reconciled {
			if err := w.processNext(ctx); err != nil && ctx.Err() == nil {
				slog.Error("analysis queue operation failed")
			}
		}
		if !waitAnalysisPoll(ctx, w.cfg.PollInterval) {
			return
		}
	}
}

func waitAnalysisPoll(ctx context.Context, interval time.Duration) bool {
	// A delay after every chunk bounds background model traffic even when a backlog is ready.
	timer := time.NewTimer(interval)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

func (w *AnalysisWorker) reconcile(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, analysisDBTimeout)
	defer cancel()
	ctx, span := scanTracer().Start(ctx, "analysis.reconcile")
	defer span.End()
	span.SetAttributes(attribute.String("repository.id", w.cfg.RepositoryID))
	execution := newAnalysisExecution(ctx, "system", "feature_startup")
	err := w.store.EnsureAnalysis(ctx, w.cfg.RepositoryID, execution)
	if err != nil {
		telemetry.Fail(span, "analysis_reconcile_failed")
		slog.Error("analysis queue reconciliation failed")
	}
	return err
}

func (w *AnalysisWorker) processNext(ctx context.Context) error {
	release, err := w.cfg.Admission.acquire(ctx)
	if err != nil {
		return err
	}
	defer release()
	claimCtx, cancel := context.WithTimeout(ctx, analysisDBTimeout)
	job, err := w.store.ClaimAnalysis(claimCtx, w.cfg.RepositoryID, newExecutionID(), w.cfg.ExecutionTimeout+30*time.Second)
	cancel()
	if errors.Is(err, domain.ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	return w.process(ctx, job)
}

func (w *AnalysisWorker) process(ctx context.Context, job domain.AnalysisJob) error {
	ctx, span := analysisJobSpan(ctx, job)
	defer span.End()
	span.SetAttributes(attribute.String("repository.id", w.cfg.RepositoryID))
	span.AddEvent("execution.started")
	workCtx, cancel := context.WithTimeout(ctx, w.cfg.ExecutionTimeout)
	update, err := w.analyze(withAnalysisLease(workCtx, job), job)
	cancel()
	if errors.Is(err, domain.ErrLeaseLost) {
		span.AddEvent("execution.lease_lost")
		return nil
	}
	if err != nil {
		update = w.failureUpdate(ctx, job, err)
		if ctx.Err() != nil {
			span.AddEvent("execution.canceled")
		} else {
			telemetry.Fail(span, update.ErrorCode)
		}
	}
	// Shutdown still acknowledges or releases owned work, bounded by the lease's publication reserve.
	finishCtx, finishCancel := context.WithTimeout(context.WithoutCancel(ctx), analysisDBTimeout)
	defer finishCancel()
	if err := w.store.UpdateAnalysis(finishCtx, w.cfg.RepositoryID, job.ID, job.LeaseToken, update); err != nil {
		if errors.Is(err, domain.ErrLeaseLost) {
			span.AddEvent("execution.lease_lost")
			return nil
		}
		return err
	}
	span.SetAttributes(attribute.String("job.status", update.Status))
	span.AddEvent("execution." + update.Status)
	return nil
}

func analysisJobSpan(ctx context.Context, job domain.AnalysisJob) (context.Context, trace.Span) {
	opts := []trace.SpanStartOption{trace.WithNewRoot()}
	origin := propagation.TraceContext{}.Extract(context.Background(), propagation.MapCarrier{"traceparent": job.TraceParent})
	if span := trace.SpanContextFromContext(origin); span.IsValid() {
		opts = append(opts, trace.WithLinks(trace.Link{SpanContext: span}))
	}
	ctx, span := scanTracer().Start(ctx, "analysis.chunk", opts...)
	span.SetAttributes(attribute.String("job.id", job.ID), attribute.String("snapshot.id", job.SnapshotID), attribute.String("actor.type", "system"))
	return ctx, span
}
