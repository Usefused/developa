package application

import (
	"context"
	"errors"
	"time"

	"developa/internal/domain"
	"developa/internal/telemetry"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

func (m *Manager) RequestScan(ctx context.Context) (domain.Execution, error) {
	ctx, span := scanTracer().Start(ctx, "repository.request_scan")
	defer span.End()
	if err := ctx.Err(); err != nil {
		return domain.Execution{}, err
	}
	if err := m.reserveManual(); err != nil {
		return domain.Execution{}, err
	}
	defer m.requestWG.Done()
	ctx, cancel := context.WithTimeout(ctx, m.cfg.ScanTimeout)
	stop := context.AfterFunc(m.lifecycle, cancel)
	defer stop()
	defer cancel()
	request := newScanRequest(ctx, "operator", "manual")
	span.SetAttributes(attribute.String("execution.id", request.execution.ID), attribute.String("actor.type", "operator"))
	if err := m.store.RecordExecution(ctx, m.repository.ID, request.execution, "queued"); err != nil {
		telemetry.Fail(span, "audit_unavailable")
		m.finish(err)
		return domain.Execution{}, errors.New("scan request could not be recorded")
	}
	if err := m.enqueue(request); err != nil {
		m.recordOutcome(request.ctx, request.execution, "canceled")
		m.finish(err)
		return domain.Execution{}, err
	}
	span.AddEvent("execution.queued")
	return request.execution, nil
}

func newScanRequest(ctx context.Context, actor, trigger string) scanRequest {
	execution := domain.Execution{ID: newExecutionID(), Actor: actor, Trigger: trigger, Status: "queued"}
	span := trace.SpanContextFromContext(ctx)
	if span.IsValid() {
		execution.TraceID = span.TraceID().String()
	}
	// HTTP completion must not cancel an accepted job, but its trace and trusted context survive.
	return scanRequest{ctx: context.WithoutCancel(ctx), execution: execution}
}

func (m *Manager) enqueue(request scanRequest) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return context.Canceled
	}
	m.requests <- request
	return nil
}

func (m *Manager) run(ctx context.Context) {
	defer m.wg.Done()
	defer m.shutdown()
	m.execute(ctx, newScanRequest(ctx, "system", "startup"))
	poll := time.NewTimer(m.cfg.PollInterval)
	defer poll.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case request := <-m.requests:
			m.execute(ctx, request)
		case <-poll.C:
			if err := m.reserve(); err == nil {
				m.execute(ctx, newScanRequest(ctx, "system", "watch"))
			}
		}
		// A slow capture must not leave a pending tick that immediately reserves
		// another watch scan and starves manual admission under sustained load.
		poll.Reset(m.cfg.PollInterval)
	}
}

func (m *Manager) shutdown() {
	// Closing admission before draining prevents a concurrent durable admission from orphaning work.
	m.stopWatching()
	m.cancelPending()
}

func (m *Manager) stopWatching() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.watching, m.busy, m.closed = false, false, true
}

func (m *Manager) cancelPending() {
	select {
	case request := <-m.requests:
		m.recordOutcome(request.ctx, request.execution, "canceled")
	default:
	}
}
