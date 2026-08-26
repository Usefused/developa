package application

import (
	"context"
	"errors"
	"sync"
	"time"

	"developa/internal/domain"
	"developa/internal/telemetry"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

type AnalysisWorkerConfig struct {
	RepositoryID     string
	PollInterval     time.Duration
	ExecutionTimeout time.Duration
	RetryInterval    time.Duration
	Admission        *AnalysisAdmission
}

// AnalysisWorker checkpoints bounded feature batches through durable, fenced jobs.
// The queue contains snapshot identities, never credentials, prompts, or source.
type AnalysisWorker struct {
	store        domain.AnalysisJobStore
	intelligence domain.Intelligence
	cfg          AnalysisWorkerConfig
	mu           sync.Mutex
	started      bool
	closed       bool
	cancel       context.CancelFunc
	wg           sync.WaitGroup
}

func NewAnalysisWorker(store domain.AnalysisJobStore, intelligence domain.Intelligence, cfg AnalysisWorkerConfig) (*AnalysisWorker, error) {
	if store == nil {
		return nil, errors.New("analysis job store is required")
	}
	cfg = analysisWorkerDefaults(cfg)
	if err := validateAnalysisWorkerConfig(cfg); err != nil {
		return nil, err
	}
	return &AnalysisWorker{store: store, intelligence: intelligence, cfg: cfg}, nil
}

func analysisWorkerDefaults(cfg AnalysisWorkerConfig) AnalysisWorkerConfig {
	if cfg.PollInterval == 0 {
		cfg.PollInterval = 2 * time.Second
	}
	if cfg.ExecutionTimeout == 0 {
		cfg.ExecutionTimeout = 120 * time.Second
	}
	if cfg.RetryInterval == 0 {
		cfg.RetryInterval = 5 * time.Second
	}
	return cfg
}

func validateAnalysisWorkerConfig(cfg AnalysisWorkerConfig) error {
	if cfg.PollInterval <= 0 || cfg.PollInterval > time.Minute {
		return errors.New("invalid analysis polling interval")
	}
	if cfg.ExecutionTimeout <= 0 || cfg.ExecutionTimeout > 5*time.Minute {
		return errors.New("invalid analysis execution timeout")
	}
	if cfg.RetryInterval <= 0 || cfg.RetryInterval > time.Minute {
		return errors.New("invalid analysis retry interval")
	}
	return nil
}

func (w *AnalysisWorker) Available() bool {
	return w.cfg.RepositoryID != "" && w.intelligence != nil && w.intelligence.Available()
}

func (w *AnalysisWorker) Queue(ctx context.Context, snapshotID string) (domain.AnalysisJob, error) {
	ctx, span := scanTracer().Start(ctx, "analysis.queue")
	defer span.End()
	span.SetAttributes(attribute.String("repository.id", w.cfg.RepositoryID))
	if !w.Available() {
		return domain.AnalysisJob{}, domain.ErrModelUnavailable
	}
	execution := newAnalysisExecution(ctx, "operator", "feature_manual")
	span.SetAttributes(attribute.String("execution.id", execution.ID), attribute.String("actor.type", execution.Actor))
	job, err := w.store.EnqueueAnalysis(ctx, w.cfg.RepositoryID, snapshotID, execution)
	if err != nil {
		telemetry.Fail(span, "analysis_enqueue_failed")
		return domain.AnalysisJob{}, err
	}
	span.SetAttributes(attribute.String("job.id", job.ID))
	span.AddEvent("execution.queued")
	return job, nil
}

func (w *AnalysisWorker) Status(ctx context.Context, snapshotID string) (domain.AnalysisJob, error) {
	if w.cfg.RepositoryID == "" {
		return domain.AnalysisJob{}, domain.ErrNotConfigured
	}
	return w.store.AnalysisStatus(ctx, w.cfg.RepositoryID, snapshotID)
}

func newAnalysisExecution(ctx context.Context, actor, trigger string) domain.Execution {
	return domain.Execution{ID: newExecutionID(), Actor: actor, Trigger: trigger, Status: "queued", TraceID: trace.SpanContextFromContext(ctx).TraceID().String()}
}

func (w *AnalysisWorker) Start(ctx context.Context) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.started || w.closed || !w.Available() {
		return
	}
	runCtx, cancel := context.WithCancel(ctx)
	w.cancel, w.started = cancel, true
	w.wg.Add(1)
	go w.run(runCtx)
}

func (w *AnalysisWorker) Close() {
	w.mu.Lock()
	w.closed = true
	if w.cancel != nil {
		w.cancel()
	}
	w.mu.Unlock()
	w.wg.Wait()
}
