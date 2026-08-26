package domain

import (
	"context"
	"encoding/json"
	"errors"
	"time"
)

var ErrLeaseLost = errors.New("analysis lease no longer owned")

// AnalysisJob is the current durable work state for one immutable snapshot.
// Credentials and source are never queue payloads; the worker retrieves scoped records.
type AnalysisJob struct {
	ID              string     `json:"id"`
	SnapshotID      string     `json:"snapshot_id"`
	Commit          string     `json:"commit"`
	BaseRunID       string     `json:"base_run_id,omitempty"`
	Status          string     `json:"status"`
	Automatic       bool       `json:"automatic"`
	Attempts        int        `json:"attempts"`
	Chunks          int        `json:"chunks"`
	AnalyzedSymbols int        `json:"analyzed_symbols"`
	TotalSymbols    int        `json:"total_symbols"`
	FeatureCount    int        `json:"feature_count"`
	ErrorCode       string     `json:"error_code,omitempty"`
	AvailableAt     time.Time  `json:"available_at"`
	LeaseUntil      *time.Time `json:"lease_until,omitempty"`
	CreatedAt       time.Time  `json:"created_at"`
	UpdatedAt       time.Time  `json:"updated_at"`
	TraceParent     string     `json:"-"`
	LeaseToken      string     `json:"-"`
}

type AnalysisJobUpdate struct {
	Status          string
	ErrorCode       string
	AvailableAt     time.Time
	AnalyzedSymbols int
	TotalSymbols    int
	FeatureCount    int
	Progress        bool // A published chunk resets consecutive failures and increments Chunks.
	Failed          bool // A failed attempt increments the bounded retry counter.
}

type AnalysisJobStore interface {
	IntelligenceStore
	EnqueueAnalysis(context.Context, string, string, Execution) (AnalysisJob, error)
	EnsureAnalysis(context.Context, string, Execution) error
	AnalysisStatus(context.Context, string, string) (AnalysisJob, error)
	ClaimAnalysis(context.Context, string, string, time.Duration) (AnalysisJob, error)
	UpdateAnalysis(context.Context, string, string, string, AnalysisJobUpdate) error
}

type AnalysisQueue interface {
	Available() bool
	Queue(context.Context, string) (AnalysisJob, error)
	Status(context.Context, string) (AnalysisJob, error)
}

// AnalysisCacheStore reuses validated model output for the same bounded input,
// prompt version and verified provider identity, never across repository scopes.
type AnalysisCacheStore interface {
	CachedAnalysis(context.Context, string, string) (json.RawMessage, error)
	CacheAnalysis(context.Context, string, string, string, json.RawMessage) error
}

// AnalysisPageReader keeps each bounded page inside one file in SQL. A new
// declaration in one file must not shift the cache boundaries of every later file.
type AnalysisPageReader interface {
	AnalysisPage(context.Context, string, string, int, int) (SymbolPage, error)
}
