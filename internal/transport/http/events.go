package httptransport

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"developa/internal/domain"
	"developa/internal/telemetry"
	"github.com/go-chi/chi/v5"
	"go.opentelemetry.io/otel/trace"
)

const (
	eventStreamLifetime   = 5 * time.Minute
	eventPollInterval     = time.Second
	eventIdlePollInterval = 2 * time.Minute
	eventHeartbeat        = 15 * time.Second
	eventIOTimeout        = 5 * time.Second
	maxEventBytes         = 256 << 10
)

type eventWriter struct {
	writer     http.ResponseWriter
	controller *http.ResponseController
}

func newEventWriter(w http.ResponseWriter) eventWriter {
	w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("X-Accel-Buffering", "no")
	return eventWriter{writer: w, controller: http.NewResponseController(w)}
}

func (w eventWriter) event(name string, value any) error {
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	frame := fmt.Sprintf("event: %s\ndata: %s\n\n", name, data)
	if len(frame) > maxEventBytes {
		return errors.New("event exceeds delivery limit")
	}
	return w.write(frame)
}

func (w eventWriter) write(data string) error {
	// A stalled reader must not block a handler beyond cancellation or the next
	// heartbeat. Per-write deadlines also replace the ordinary server write limit.
	if err := w.controller.SetWriteDeadline(time.Now().Add(eventIOTimeout)); err != nil && !errors.Is(err, http.ErrNotSupported) {
		return err
	}
	// HTTP/2 expires an armed write deadline even while idle. Clear the per-write
	// bound after flushing so model latency and heartbeat gaps do not reset streams.
	defer func() { _ = w.controller.SetWriteDeadline(time.Time{}) }()
	if _, err := fmt.Fprint(w.writer, data); err != nil {
		return err
	}
	return w.controller.Flush()
}

func (w eventWriter) heartbeat() error { return w.write(": keepalive\n\n") }

func (w eventWriter) failure(ctx context.Context, err error) {
	code, _ := errorStatus(err)
	telemetry.Fail(trace.SpanFromContext(ctx), "stream_failed")
	_ = w.event("error", StreamError{Status: code, TraceID: eventTraceID(ctx)})
}

func eventTraceID(ctx context.Context) string {
	span := trace.SpanContextFromContext(ctx)
	if !span.IsValid() {
		return ""
	}
	return span.TraceID().String()
}

func (e *Explorer) analysisEvents(w http.ResponseWriter, r *http.Request) {
	if e.Jobs == nil {
		writeError(w, domain.ErrNotConfigured)
		return
	}
	job, err := e.readAnalysisEvent(r.Context(), chi.URLParam(r, "snapshot"))
	if err != nil {
		writeError(w, err)
		return
	}
	e.streamAnalysis(newEventWriter(w), r, job)
}

func (e *Explorer) readAnalysisEvent(ctx context.Context, snapshot string) (domain.AnalysisJob, error) {
	ctx, cancel := context.WithTimeout(ctx, eventIOTimeout)
	defer cancel()
	return e.Jobs.Status(ctx, snapshot)
}

func (e *Explorer) streamAnalysis(writer eventWriter, r *http.Request, previous domain.AnalysisJob) {
	if writer.event("analysis", previous) != nil {
		return
	}
	poll, heartbeat := time.NewTimer(analysisPollInterval(previous.Status)), time.NewTicker(eventHeartbeat)
	defer poll.Stop()
	defer heartbeat.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-heartbeat.C:
			if writer.heartbeat() != nil {
				return
			}
		case <-poll.C:
			if !e.nextAnalysisEvent(writer, r, &previous) {
				return
			}
			poll.Reset(analysisPollInterval(previous.Status))
		}
	}
}

func analysisPollInterval(status string) time.Duration {
	if status == "queued" || status == "running" {
		return eventPollInterval
	}
	return eventIdlePollInterval
}

func (e *Explorer) nextAnalysisEvent(writer eventWriter, r *http.Request, previous *domain.AnalysisJob) bool {
	job, err := e.readAnalysisEvent(r.Context(), chi.URLParam(r, "snapshot"))
	if err != nil {
		writer.failure(r.Context(), err)
		return false
	}
	if job.Status == previous.Status && job.UpdatedAt.Equal(previous.UpdatedAt) {
		return true
	}
	*previous = job
	return writer.event("analysis", job) == nil
}

func (e *Explorer) answerStream(w http.ResponseWriter, r *http.Request) {
	request, ok := e.prepareAnswer(w, r)
	if !ok {
		return
	}
	if err := e.answerStreamScope(r.Context(), chi.URLParam(r, "snapshot"), request); err != nil {
		writeError(w, err)
		return
	}
	writer := newEventWriter(w)
	if writer.event("started", StreamStarted{Status: "started", TraceID: eventTraceID(r.Context())}) != nil {
		return
	}
	e.streamAnswer(writer, r, request)
}

func (e *Explorer) answerStreamScope(ctx context.Context, snapshot string, request domain.AnswerRequest) error {
	ctx, cancel := context.WithTimeout(ctx, eventIOTimeout)
	defer cancel()
	if request.Flow != nil {
		request.SymbolID, request.FeatureID = request.Flow.SymbolID, request.Flow.FeatureID
	}
	if request.FeatureID != "" {
		_, err := e.Knowledge.Feature(ctx, e.RepositoryID, snapshot, request.FeatureID)
		return err
	}
	if request.SymbolID != "" {
		_, err := e.Catalog.Symbol(ctx, e.RepositoryID, snapshot, request.SymbolID)
		return err
	}
	// Generic questions still verify snapshot scope before committing stream headers.
	_, err := e.Catalog.Files(ctx, e.RepositoryID, snapshot, domain.Filter{Limit: 1})
	return err
}

type savedEvent struct {
	value any
	err   error
}

func (e *Explorer) streamAnswer(writer eventWriter, r *http.Request, request domain.AnswerRequest) {
	snapshot := chi.URLParam(r, "snapshot")
	streamSavedResult(writer, r, "answer", func(ctx context.Context) (any, error) { return e.Intelligence.Answer(ctx, snapshot, request) })
}

func streamSavedResult(writer eventWriter, r *http.Request, name string, run func(context.Context) (any, error)) {
	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()
	result := make(chan savedEvent, 1)
	go func() {
		value, err := run(ctx)
		// A buffered result cannot strand a completed model job after its client leaves.
		result <- savedEvent{value: value, err: err}
	}()
	awaitSavedEvent(writer, ctx, name, result)
}

func awaitSavedEvent(writer eventWriter, ctx context.Context, name string, result <-chan savedEvent) {
	heartbeat := time.NewTicker(eventHeartbeat)
	defer heartbeat.Stop()
	for {
		select {
		case <-ctx.Done():
			if errors.Is(ctx.Err(), context.DeadlineExceeded) {
				writer.failure(ctx, ctx.Err())
			}
			return
		case <-heartbeat.C:
			if writer.heartbeat() != nil {
				return
			}
		case event := <-result:
			finishSavedEvent(writer, ctx, name, event)
			return
		}
	}
}

func finishSavedEvent(writer eventWriter, ctx context.Context, name string, event savedEvent) {
	if event.err != nil {
		writer.failure(ctx, event.err)
		return
	}
	// Services return only after validation and audited persistence have succeeded.
	if err := writer.event(name, event.value); err != nil {
		writer.failure(ctx, err)
	}
}
