package domain

import (
	"context"
	"errors"
	"time"

	goparser "developa/internal/indexer/golang"
)

var (
	ErrModelUnavailable   = errors.New("model unavailable")
	ErrInvalidModelOutput = errors.New("model output failed evidence validation")
	ErrInvalidInput       = errors.New("invalid input")
)

type CallFilter struct {
	SymbolID   string
	Direction  string
	Resolution string
	Limit      int
	Offset     int
}

type CallPage struct {
	Items  []goparser.Call `json:"items"`
	Total  int             `json:"total"`
	Limit  int             `json:"limit"`
	Offset int             `json:"offset"`
}

type ChainOptions struct {
	Direction string
	Depth     int
	Limit     int
}

type CallChain struct {
	SnapshotID string          `json:"snapshot_id"`
	RootID     string          `json:"root_id"`
	Direction  string          `json:"direction"`
	Depth      int             `json:"depth"`
	Nodes      []SymbolDetail  `json:"nodes"`
	Edges      []goparser.Call `json:"edges"`
	Truncated  bool            `json:"truncated"`
}

type Citation struct {
	SymbolID string        `json:"symbol_id"`
	Path     string        `json:"path"`
	Name     string        `json:"name"`
	Span     goparser.Span `json:"span"`
}

type ContextPack struct {
	RepositoryID string         `json:"repository_id"`
	SnapshotID   string         `json:"snapshot_id"`
	Query        string         `json:"query"`
	Items        []SymbolDetail `json:"items"`
	Total        int            `json:"total"`
	Truncated    bool           `json:"truncated"`
}

type Feature struct {
	ID       string     `json:"id"`
	Title    string     `json:"title"`
	Summary  string     `json:"summary"`
	Status   string     `json:"status"`
	Evidence []Citation `json:"evidence"`
}

type FeatureRun struct {
	ParentRunID     string    `json:"parent_run_id,omitempty"`
	ID              string    `json:"id"`
	SnapshotID      string    `json:"snapshot_id"`
	Model           string    `json:"model"`
	Status          string    `json:"status"`
	AnalyzedSymbols int       `json:"analyzed_symbols"`
	TotalSymbols    int       `json:"total_symbols"`
	FeatureCount    int       `json:"feature_count"`
	CachedBatches   int       `json:"cached_batches"`
	ModelCalls      int       `json:"model_calls"`
	CreatedAt       time.Time `json:"created_at"`
	Limitations     []string  `json:"limitations"`
}

type FeaturePage struct {
	// SavedSnapshot is a navigation hint, never evidence for the requested snapshot.
	SavedSnapshot *Snapshot   `json:"saved_snapshot,omitempty"`
	Run           *FeatureRun `json:"run"`
	Items         []Feature   `json:"items"`
	Total         int         `json:"total"`
	Limit         int         `json:"limit"`
	Offset        int         `json:"offset"`
}

type AnswerRequest struct {
	Question  string       `json:"question"`
	SymbolID  string       `json:"symbol_id,omitempty"`
	FeatureID string       `json:"feature_id,omitempty"`
	Flow      *FlowOptions `json:"flow,omitempty"`
}

type Answer struct {
	// ContextKey is an internal source/request fingerprint, never a caller-selected cache key.
	ContextKey           string     `json:"-"`
	GeneratedSnapshotID  string     `json:"generated_snapshot_id,omitempty"`
	ID                   string     `json:"id"`
	SnapshotID           string     `json:"snapshot_id"`
	Model                string     `json:"model"`
	Text                 string     `json:"text"`
	Evidence             []Citation `json:"evidence"`
	InsufficientEvidence bool       `json:"insufficient_evidence"`
	ContextTruncated     bool       `json:"context_truncated"`
	Cached               bool       `json:"cached"`
	Limitations          []string   `json:"limitations"`
	CreatedAt            time.Time  `json:"created_at"`
}

type SavedAnswerStore interface {
	SavedAnswer(context.Context, string, string, string) (Answer, error)
}

type SavedAnswerReader interface {
	SavedAnswer(context.Context, string, AnswerRequest) (*Answer, error)
}

// FeatureContextReader selects bounded canonical source evidence for one scoped feature in SQL.
type FeatureContextReader interface {
	FeatureContext(context.Context, string, string, string, int) (ContextPack, error)
}

// IntelligenceStore keeps retrieval scoped and bounded in SQL. Feature generation
// walks immutable symbol pages; it must not load a repository to filter in Go.
type IntelligenceStore interface {
	CatalogReader
	Calls(context.Context, string, string, CallFilter) (CallPage, error)
	Chain(context.Context, string, string, string, ChainOptions) (CallChain, error)
	Context(context.Context, string, string, string, int) (ContextPack, error)
	Features(context.Context, string, string, Filter) (FeaturePage, error)
	Feature(context.Context, string, string, string) (Feature, error)
	SaveFeatures(context.Context, string, FeatureRun, []Feature, Execution) error
	SaveAnswer(context.Context, string, Answer, Execution) error
	RecordExecution(context.Context, string, Execution, string) error
}

type Intelligence interface {
	Available() bool
	Discover(context.Context, string) (FeatureRun, error)
	Answer(context.Context, string, AnswerRequest) (Answer, error)
}
