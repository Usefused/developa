// Package application composes source capture and language analysis without
// tying either component to a transport or persistence implementation.
package application

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"time"

	goparser "developa/internal/indexer/golang"
	source "developa/internal/source/git"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

// SnapshotInfo omits source contents so a report cannot accidentally dump the
// captured repository along with its structural inventory.
type SnapshotInfo struct {
	Fingerprint string             `json:"fingerprint"`
	Commit      string             `json:"commit,omitempty"`
	Branch      string             `json:"branch,omitempty"`
	Dirty       bool               `json:"dirty"`
	Files       int                `json:"files"`
	Complete    bool               `json:"complete"`
	Tags        []string           `json:"tags"`
	Exclusions  []source.Exclusion `json:"exclusions,omitempty"`
}

// Report is the local scan format, not the persisted snapshot REST contract.
type Report struct {
	SchemaVersion string          `json:"schema_version"`
	ExecutionID   string          `json:"execution_id"`
	TraceID       string          `json:"trace_id"`
	Snapshot      SnapshotInfo    `json:"snapshot"`
	Index         goparser.Result `json:"index"`
	Changes       []source.Change `json:"changes,omitempty"`
	ChangesKnown  bool            `json:"changes_known"`
}

type Scanner struct {
	repository  *source.Repository
	executionID string
}

// Open starts one execution identity that remains stable across watch updates.
func Open(ctx context.Context, root string, options source.Options) (*Scanner, error) {
	id := newExecutionID()
	ctx, span := scanTracer().Start(ctx, "scan.open", trace.WithAttributes(attribute.String("execution.id", id)))
	defer span.End()
	repository, err := source.Open(ctx, root, options)
	if err != nil {
		span.SetStatus(codes.Error, "repository unavailable")
		return nil, err
	}
	return &Scanner{repository: repository, executionID: id}, nil
}

// Scan captures a consistent eligible-file snapshot before parsing it.
func (s *Scanner) Scan(ctx context.Context) (Report, error) {
	ctx, span := s.start(ctx, "scan.execute")
	defer span.End()
	snapshot, err := s.repository.Capture(ctx)
	if err != nil {
		span.SetStatus(codes.Error, "capture failed")
		return Report{}, err
	}
	return s.analyze(ctx, snapshot, nil)
}

// Watch emits the initial inventory and each reconciled source update. The
// callback applies backpressure instead of accumulating full snapshots.
func (s *Scanner) Watch(ctx context.Context, interval time.Duration, publish func(Report) error) error {
	if publish == nil {
		return errors.New("scan publisher is required")
	}
	ctx, span := s.start(ctx, "scan.watch")
	defer span.End()
	err := s.repository.Watch(ctx, interval, func(updateCtx context.Context, update source.Update) error {
		report, err := s.analyze(updateCtx, update.Current, update.Changes)
		if err != nil {
			return err
		}
		report.ChangesKnown = update.Previous != nil
		return publish(report)
	})
	if err != nil && ctx.Err() == nil {
		span.SetStatus(codes.Error, "watch failed")
	}
	return err
}

func (s *Scanner) analyze(ctx context.Context, snapshot *source.Snapshot, changes []source.Change) (Report, error) {
	ctx, span := s.start(ctx, "scan.parse_snapshot")
	defer span.End()
	files := make([]goparser.SourceFile, 0, len(snapshot.Files))
	for _, file := range snapshot.Files {
		files = append(files, goparser.SourceFile{Path: file.Path, Content: file.Content})
	}
	index, err := goparser.Parse(ctx, files)
	if err != nil {
		span.SetStatus(codes.Error, "parse failed")
		return Report{}, err
	}
	if err := goparser.AnalyzeCalls(ctx, files, &index); err != nil {
		span.SetStatus(codes.Error, "call analysis failed")
		return Report{}, err
	}
	span.SetAttributes(attribute.Int("source.file_count", len(files)), attribute.Int("source.change_count", len(changes)))
	span.AddEvent("scan.completed")
	return Report{
		SchemaVersion: "0.1", ExecutionID: s.executionID,
		TraceID:  span.SpanContext().TraceID().String(),
		Snapshot: snapshotInfo(snapshot), Index: index, Changes: changes,
	}, nil
}

func (s *Scanner) start(ctx context.Context, name string) (context.Context, trace.Span) {
	return scanTracer().Start(ctx, name, trace.WithAttributes(attribute.String("execution.id", s.executionID)))
}

func snapshotInfo(snapshot *source.Snapshot) SnapshotInfo {
	return SnapshotInfo{
		Fingerprint: snapshot.Fingerprint, Commit: snapshot.Commit, Branch: snapshot.Branch,
		Dirty: snapshot.Dirty, Files: len(snapshot.Files), Complete: snapshot.Complete,
		Tags: snapshot.Tags, Exclusions: snapshot.Exclusions,
	}
}

func scanTracer() trace.Tracer {
	return otel.Tracer("denverr/application")
}

func newExecutionID() string {
	var id [16]byte
	_, _ = rand.Read(id[:])
	id[6] = (id[6] & 0x0f) | 0x40
	id[8] = (id[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", id[0:4], id[4:6], id[6:8], id[8:10], id[10:16])
}
