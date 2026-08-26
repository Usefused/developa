package domain

import (
	"context"
	"encoding/json"
	"time"
)

// ParameterReview points to the parser's zero-based position, never an invented name.
type ParameterReview struct {
	Position    int    `json:"position"`
	Description string `json:"description"`
}

type FunctionReview struct {
	SymbolID             string            `json:"symbol_id"`
	SourceID             string            `json:"source_id"`
	Summary              string            `json:"summary"`
	Parameters           []ParameterReview `json:"parameters"`
	InsufficientEvidence bool              `json:"insufficient_evidence"`
	ContextTruncated     bool              `json:"context_truncated"`
	Model                string            `json:"model"`
	PromptVersion        string            `json:"prompt_version"`
	CreatedAt            time.Time         `json:"created_at"`
	Cached               bool              `json:"cached"`
	Evidence             Citation          `json:"evidence"`
}

type ReviewOptions struct {
	SymbolID string `json:"symbol_id,omitempty"`
	CalleeOf string `json:"callee_of,omitempty"`
	Limit    int    `json:"limit"`
	Offset   int    `json:"offset"`
}

type ReviewPage struct {
	SnapshotID      string         `json:"snapshot_id"`
	Options         ReviewOptions  `json:"options"`
	Items           []SymbolDetail `json:"items"`
	Total           int            `json:"total"`
	NextOffset      *int           `json:"next_offset"`
	UnresolvedCount int            `json:"unresolved_count"`
	ModelCalls      int            `json:"model_calls"`
	CachedCount     int            `json:"cached_count"`
	Limitations     []string       `json:"limitations"`
}

type ReviewCacheEntry struct {
	Key     string          `json:"key"`
	Model   string          `json:"model"`
	Payload json.RawMessage `json:"payload"`
}

type ReviewStore interface {
	ReviewPage(context.Context, string, string, ReviewOptions) (ReviewPage, error)
	CachedReviews(context.Context, string, []string) (map[string]json.RawMessage, error)
	SaveReviews(context.Context, string, string, []FunctionReview, []ReviewCacheEntry, Execution) error
}

type FunctionReviewer interface {
	Available() bool
	Review(context.Context, string, ReviewOptions) (ReviewPage, error)
}

func NormalizeReviewOptions(options ReviewOptions) (ReviewOptions, error) {
	if options.Limit == 0 {
		options.Limit = 4
	}
	if options.Limit < 1 || options.Limit > 8 || options.Offset < 0 || options.Offset > 100000 {
		return options, ErrInvalidInput
	}
	if options.SymbolID != "" && options.CalleeOf != "" {
		return options, ErrInvalidInput
	}
	if !validOptionalFlowID(options.SymbolID) || !validOptionalFlowID(options.CalleeOf) {
		return options, ErrInvalidInput
	}
	return options, nil
}

func (page *ReviewPage) Advance(count int) {
	page.NextOffset = nil
	if next := page.Options.Offset + count; next < page.Total {
		page.NextOffset = &next
	}
}
