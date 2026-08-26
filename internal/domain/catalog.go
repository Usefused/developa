// Package domain defines snapshot-pinned explorer contracts shared by the API,
// application, and persistence layers.
package domain

import (
	"context"
	"errors"
	"time"

	goparser "developa/internal/indexer/golang"
	source "developa/internal/source/git"
)

var (
	ErrNotFound      = errors.New("record not found")
	ErrBusy          = errors.New("scan already queued or running")
	ErrNotConfigured = errors.New("repository not configured")
)

// IndexVersion forces reconciliation when analysis capabilities change, even if source bytes do not.
const IndexVersion = "4"

type Repository struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type Snapshot struct {
	IndexVersion    string    `json:"index_version"`
	ID              string    `json:"id"`
	Fingerprint     string    `json:"fingerprint"`
	Commit          string    `json:"commit"`
	Branch          string    `json:"branch"`
	Dirty           bool      `json:"dirty"`
	SourceComplete  bool      `json:"source_complete"`
	Analysis        string    `json:"analysis"`
	Completeness    string    `json:"completeness"`
	FileCount       int       `json:"file_count"`
	SymbolCount     int       `json:"symbol_count"`
	PackageCount    int       `json:"package_count"`
	DiagnosticCount int       `json:"diagnostic_count"`
	ExclusionCount  int       `json:"exclusion_count"`
	ChangeCount     int       `json:"change_count"`
	ChangesKnown    bool      `json:"changes_known"`
	Tags            []string  `json:"tags"`
	IndexedAt       time.Time `json:"indexed_at"`
}

type Project struct {
	Configured     bool       `json:"configured"`
	Repository     Repository `json:"repository"`
	Status         string     `json:"status"`
	Watching       bool       `json:"watching"`
	PollIntervalMS int64      `json:"poll_interval_ms"`
	LastError      string     `json:"last_error,omitempty"`
	Snapshot       *Snapshot  `json:"snapshot"`
}

type Execution struct {
	ID         string `json:"id"`
	Actor      string `json:"actor"`
	Trigger    string `json:"trigger"`
	TraceID    string `json:"trace_id"`
	Status     string `json:"status"`
	JobID      string `json:"-"`
	LeaseToken string `json:"-"`
}

// Filter is validated by transports and bounded again by storage. A snapshot ID
// is mandatory in every reader operation so concurrent indexing cannot mix data.
type Filter struct {
	Query  string
	Kind   string
	File   string
	Limit  int
	Offset int
}

type FileSummary struct {
	Path         string         `json:"path"`
	Package      string         `json:"package"`
	Overview     string         `json:"overview"`
	Completeness string         `json:"completeness"`
	SymbolCount  int            `json:"symbol_count"`
	Kinds        map[string]int `json:"kinds"`
}

type FileDetail struct {
	FileSummary
	Doc     string            `json:"doc"`
	Imports []goparser.Import `json:"imports"`
}

type FilePage struct {
	Items  []FileSummary `json:"items"`
	Total  int           `json:"total"`
	Limit  int           `json:"limit"`
	Offset int           `json:"offset"`
}

type SymbolDetail struct {
	Path   string          `json:"path"`
	Symbol goparser.Symbol `json:"symbol"`
	Review *FunctionReview `json:"review,omitempty"`
}

type SymbolPage struct {
	Items  []SymbolDetail `json:"items"`
	Total  int            `json:"total"`
	Limit  int            `json:"limit"`
	Offset int            `json:"offset"`
}

type SnapshotDetails struct {
	ImplementationAnalysis goparser.ImplementationAnalysis `json:"implementation_analysis"`
	CallAnalysis           goparser.CallAnalysis           `json:"call_analysis"`
	Snapshot               Snapshot                        `json:"snapshot"`
	Limitations            []string                        `json:"limitations"`
	Diagnostics            []goparser.Diagnostic           `json:"diagnostics"`
	Exclusions             []source.Exclusion              `json:"exclusions"`
	Changes                []source.Change                 `json:"changes"`
	Skipped                []goparser.SkippedFile          `json:"skipped"`
}

type CatalogReader interface {
	Latest(context.Context, string) (Snapshot, error)
	Files(context.Context, string, string, Filter) (FilePage, error)
	File(context.Context, string, string, string) (FileDetail, error)
	Symbols(context.Context, string, string, Filter) (SymbolPage, error)
	Symbol(context.Context, string, string, string) (SymbolDetail, error)
	Details(context.Context, string, string) (SnapshotDetails, error)
}

type Tracker interface {
	Project(context.Context) (Project, error)
	RequestScan(context.Context) (Execution, error)
}
