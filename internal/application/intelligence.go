package application

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"time"

	"developa/internal/domain"
	"developa/internal/telemetry"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

type StructuredModel interface {
	Model() string
	Generate(context.Context, string, string, json.RawMessage) (json.RawMessage, error)
}

type IntelligenceConfig struct {
	RepositoryID    string
	Timeout         time.Duration
	BatchSize       int
	MaxModelCalls   int
	MaxContextBytes int
}

type IntelligenceService struct {
	store domain.IntelligenceStore
	model StructuredModel
	cfg   IntelligenceConfig
	gate  chan struct{}
}

func NewIntelligence(store domain.IntelligenceStore, model StructuredModel, cfg IntelligenceConfig) (*IntelligenceService, error) {
	if store == nil {
		return nil, errors.New("intelligence store is required")
	}
	if cfg.Timeout == 0 {
		cfg.Timeout = 120 * time.Second
	}
	if cfg.BatchSize == 0 {
		cfg.BatchSize = 8
	}
	if cfg.MaxModelCalls == 0 {
		cfg.MaxModelCalls = 64
	}
	if cfg.MaxContextBytes == 0 {
		cfg.MaxContextBytes = 16 << 10
	}
	if err := validateIntelligenceConfig(cfg); err != nil {
		return nil, err
	}
	return &IntelligenceService{store: store, model: model, cfg: cfg, gate: make(chan struct{}, 1)}, nil
}

func validateIntelligenceConfig(cfg IntelligenceConfig) error {
	if cfg.Timeout <= 0 || cfg.Timeout > 5*time.Minute {
		return errors.New("invalid intelligence timeout")
	}
	if cfg.BatchSize < 1 || cfg.BatchSize > 16 {
		return errors.New("invalid intelligence batch size")
	}
	if cfg.MaxModelCalls < 1 || cfg.MaxModelCalls > 64 {
		return errors.New("invalid intelligence call limit")
	}
	if cfg.MaxContextBytes < 1024 || cfg.MaxContextBytes > 16<<10 {
		return errors.New("invalid intelligence context limit")
	}
	return nil
}

// Available describes configuration, not live model readiness; adapters check readiness before source transfer.
func (s *IntelligenceService) Available() bool { return s.model != nil && s.cfg.RepositoryID != "" }

type intelligenceExecution struct {
	ctx       context.Context
	cancel    context.CancelFunc
	span      trace.Span
	execution domain.Execution
}

func (s *IntelligenceService) begin(ctx context.Context, trigger string) (intelligenceExecution, error) {
	if !s.Available() {
		return intelligenceExecution{}, domain.ErrModelUnavailable
	}
	select {
	case s.gate <- struct{}{}:
	default:
		return intelligenceExecution{}, domain.ErrBusy
	}
	ctx, cancel := context.WithTimeout(ctx, s.cfg.Timeout)
	ctx, span := scanTracer().Start(ctx, "intelligence."+trigger)
	execution := domain.Execution{ID: newExecutionID(), Actor: "operator", Trigger: trigger, Status: "running", TraceID: span.SpanContext().TraceID().String()}
	execution = applyAnalysisLease(ctx, execution)
	span.SetAttributes(attribute.String("execution.id", execution.ID), attribute.String("actor.type", execution.Actor), attribute.String("repository.id", s.cfg.RepositoryID))
	job := intelligenceExecution{ctx: ctx, cancel: cancel, span: span, execution: execution}
	if err := s.store.RecordExecution(ctx, s.cfg.RepositoryID, execution, "running"); err != nil {
		contextErr := ctx.Err()
		s.end(job, err)
		if contextErr != nil {
			return intelligenceExecution{}, contextErr
		}
		if errors.Is(err, domain.ErrLeaseLost) {
			return intelligenceExecution{}, domain.ErrLeaseLost
		}
		return intelligenceExecution{}, errors.New("intelligence execution could not be recorded")
	}
	span.AddEvent("execution.started")
	return job, nil
}

func (s *IntelligenceService) end(job intelligenceExecution, err error) {
	defer job.cancel()
	defer job.span.End()
	defer func() { <-s.gate }()
	if err == nil {
		job.span.AddEvent("execution.completed")
		return
	}
	telemetry.Fail(job.span, "intelligence_failed")
	outcome := "error"
	if errors.Is(err, context.Canceled) {
		outcome = "canceled"
	}
	auditCtx, cancel := context.WithTimeout(context.WithoutCancel(job.ctx), 5*time.Second)
	defer cancel()
	execution := job.execution
	execution.Status = outcome
	if auditErr := s.store.RecordExecution(auditCtx, s.cfg.RepositoryID, execution, outcome); auditErr != nil {
		slog.Error("intelligence outcome audit failed", "execution_id", execution.ID, "outcome", outcome)
	}
}
