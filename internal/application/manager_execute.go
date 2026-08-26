package application

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"developa/internal/domain"
	source "developa/internal/source/git"
	"developa/internal/telemetry"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

func (m *Manager) execute(lifecycle context.Context, request scanRequest) {
	ctx, cancel := context.WithTimeout(request.ctx, m.cfg.ScanTimeout)
	stop := context.AfterFunc(lifecycle, cancel)
	defer stop()
	defer cancel()
	ctx, span := scanTracer().Start(ctx, "repository.scan", trace.WithAttributes(
		attribute.String("repository.id", m.repository.ID),
		attribute.String("execution.id", request.execution.ID), attribute.String("actor.type", request.execution.Actor),
		attribute.String("execution.trigger", request.execution.Trigger)))
	defer span.End()
	execution := request.execution
	execution.Status, execution.TraceID = "running", span.SpanContext().TraceID().String()
	span.AddEvent("execution.started")
	err := m.scan(ctx, execution)
	if err != nil {
		m.recordFailure(ctx, execution, err)
		telemetry.Fail(span, "scan_failed")
	} else {
		span.AddEvent("execution.completed")
	}
	m.finish(err)
}

func (m *Manager) scan(ctx context.Context, execution domain.Execution) error {
	if execution.Trigger == "manual" {
		if err := m.store.RecordExecution(ctx, m.repository.ID, execution, "running"); err != nil {
			return err
		}
	}
	snapshot, err := m.source.Capture(ctx)
	if err != nil {
		return err
	}
	if m.isCurrent(snapshot.Fingerprint) {
		return m.unchanged(ctx, snapshot, execution)
	}
	if execution.Trigger != "manual" {
		if err := m.store.RecordExecution(ctx, m.repository.ID, execution, "running"); err != nil {
			return err
		}
	}
	return m.analyzeAndSave(ctx, snapshot, execution)
}

func (m *Manager) isCurrent(fingerprint string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.latest != nil && m.latest.IndexVersion == domain.IndexVersion && m.latest.Fingerprint == fingerprint
}

func (m *Manager) unchanged(ctx context.Context, snapshot *source.Snapshot, execution domain.Execution) error {
	if execution.Trigger == "manual" {
		if err := m.store.RecordExecution(ctx, m.repository.ID, execution, "completed"); err != nil {
			return err
		}
	}
	m.previous = sourceManifest(snapshot)
	return nil
}

func (m *Manager) analyzeAndSave(ctx context.Context, snapshot *source.Snapshot, execution domain.Execution) error {
	var changes []source.Change
	if m.previous != nil {
		changes = source.Diff(m.previous, snapshot)
	}
	scanner := Scanner{repository: m.source, executionID: execution.ID}
	report, err := scanner.analyze(ctx, snapshot, changes)
	if err != nil {
		return err
	}
	report.ChangesKnown = m.previous != nil
	execution.Status = "completed"
	persisted, err := m.store.SaveSnapshot(ctx, m.repository.ID, report, execution)
	if err != nil {
		return err
	}
	m.previous = sourceManifest(snapshot)
	m.mu.Lock()
	m.latest = &persisted
	m.mu.Unlock()
	trace.SpanFromContext(ctx).AddEvent("snapshot.changed", trace.WithAttributes(attribute.String("snapshot.id", persisted.ID)))
	return nil
}

func sourceManifest(snapshot *source.Snapshot) *source.Snapshot {
	manifest := &source.Snapshot{Fingerprint: snapshot.Fingerprint, Files: make([]source.File, 0, len(snapshot.Files))}
	for _, file := range snapshot.Files {
		manifest.Files = append(manifest.Files, source.File{Path: file.Path, Hash: file.Hash})
	}
	return manifest
}

func (m *Manager) recordFailure(ctx context.Context, execution domain.Execution, err error) {
	outcome := "error"
	if errors.Is(err, context.Canceled) {
		outcome = "canceled"
	}
	m.recordOutcome(ctx, execution, outcome)
}

func (m *Manager) recordOutcome(ctx context.Context, execution domain.Execution, outcome string) {
	// A bounded final audit attempt survives job cancellation without delaying shutdown indefinitely.
	auditCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	execution.Status = outcome
	if err := m.store.RecordExecution(auditCtx, m.repository.ID, execution, outcome); err != nil {
		slog.Error("scan outcome audit failed", "execution_id", execution.ID, "outcome", outcome)
	}
}
