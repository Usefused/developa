package domain

import "context"

// RepositorySummary exposes configured repository identity and its latest
// publication without disclosing the operator's local filesystem path.
type RepositorySummary struct {
	Repository
	Snapshot *Snapshot `json:"snapshot"`
}

type RepositoryPage struct {
	Items               []RepositorySummary `json:"items"`
	Total               int                 `json:"total"`
	Limit               int                 `json:"limit"`
	Offset              int                 `json:"offset"`
	DefaultRepositoryID string              `json:"default_repository_id"`
}

// Configured IDs are an explicit allowlist, never an optional filter. An empty
// list must return no repositories even if the database retains older indexes.
type RepositoryReader interface {
	Repositories(context.Context, []string, Filter) (RepositoryPage, error)
}
