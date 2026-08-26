package domain

import (
	"context"
	"errors"

	goparser "developa/internal/indexer/golang"
)

var ErrSourceUnavailable = errors.New("complete declaration source unavailable")

const (
	DefaultSourceLimit = 8192
	MaxSourceLimit     = 16384
	MinSourceLimit     = 4
)

// SourceOptions measures zero-based byte offsets relative to the declaration,
// not the physical file. A chunk ends before any partial UTF-8 rune.
type SourceOptions struct {
	Offset int
	Limit  int
}

func (o SourceOptions) Validate() (SourceOptions, error) {
	if o.Limit == 0 {
		o.Limit = DefaultSourceLimit
	}
	if o.Offset < 0 || o.Limit < MinSourceLimit || o.Limit > MaxSourceLimit {
		return o, ErrInvalidInput
	}
	return o, nil
}

type SymbolSource struct {
	SnapshotID  string        `json:"snapshot_id"`
	Path        string        `json:"path"`
	SymbolID    string        `json:"symbol_id"`
	Span        goparser.Span `json:"span"`
	SourceID    string        `json:"source_id"`
	ContentHash string        `json:"content_hash"`
	Offset      int           `json:"offset"`
	NextOffset  *int          `json:"next_offset"`
	TotalBytes  int           `json:"total_bytes"`
	Source      string        `json:"source"`
	// Complete describes retained declaration coverage, not whether this chunk is last.
	Complete    bool     `json:"complete"`
	Limitations []string `json:"limitations"`
}

type SymbolSourceReader interface {
	Source(context.Context, string, string, string, SourceOptions) (SymbolSource, error)
}
