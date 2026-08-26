package domain

import (
	"context"

	goparser "developa/internal/indexer/golang"
)

// FlowOptions selects a bounded call graph, not runtime control-flow ordering.
// Empty selectors discover application roots; feature and symbol are exclusive.
type FlowOptions struct {
	SymbolID  string `json:"symbol_id,omitempty"`
	FeatureID string `json:"feature_id,omitempty"`
	Depth     int    `json:"depth,omitempty"`
	Limit     int    `json:"limit,omitempty"`
}

type FlowNode struct {
	SymbolDetail
	Seed              bool     `json:"seed"`
	RootKind          string   `json:"root_kind"`
	IncomingCount     int      `json:"incoming_count"`
	OutgoingCount     int      `json:"outgoing_count"`
	UnresolvedCount   int      `json:"unresolved_count"`
	IncomingIDs       []string `json:"incoming_ids"`
	OutgoingIDs       []string `json:"outgoing_ids"`
	Description       string   `json:"description"`
	DescriptionSource string   `json:"description_source"`
}

// Roots are classified against the full snapshot, never just the visible slice.
// Each symbol appears once; several caller edges represent a shared dependency.
type CodeFlow struct {
	SnapshotID  string          `json:"snapshot_id"`
	Mode        string          `json:"mode"`
	Options     FlowOptions     `json:"options"`
	SeedIDs     []string        `json:"seed_ids"`
	Nodes       []FlowNode      `json:"nodes"`
	Edges       []goparser.Call `json:"edges"`
	CycleGroups [][]string      `json:"cycle_groups"`
	Truncated   bool            `json:"truncated"`
	Limitations []string        `json:"limitations"`
}

type FlowReader interface {
	Flow(context.Context, string, string, FlowOptions) (CodeFlow, error)
}
