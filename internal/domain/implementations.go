package domain

import (
	"context"

	goparser "developa/internal/indexer/golang"
)

type ImplementationOptions struct {
	Limit  int
	Offset int
}

// ImplementationPage lists static candidates, never observed runtime dispatch.
// Both the interface type ID and a method declaration ID are valid selectors.
type ImplementationPage struct {
	RepositoryID string                          `json:"repository_id"`
	SnapshotID   string                          `json:"snapshot_id"`
	SymbolID     string                          `json:"symbol_id"`
	Items        []goparser.Implementation       `json:"items"`
	Total        int                             `json:"total"`
	Limit        int                             `json:"limit"`
	Offset       int                             `json:"offset"`
	Analysis     goparser.ImplementationAnalysis `json:"analysis"`
}

type ImplementationReader interface {
	Implementations(context.Context, string, string, string, ImplementationOptions) (ImplementationPage, error)
}
