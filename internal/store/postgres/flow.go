package postgres

import (
	"context"

	"developa/internal/domain"
	goparser "developa/internal/indexer/golang"
)

var _ domain.FlowReader = (*Store)(nil)

func (s *Store) Flow(ctx context.Context, repositoryID, snapshotID string, options domain.FlowOptions) (flow domain.CodeFlow, err error) {
	ctx, done := operation(ctx, "postgres.flow")
	defer func() { done(err) }()
	options, err = domain.NormalizeFlowOptions(options)
	if err != nil {
		return flow, err
	}
	flow = domain.CodeFlow{SnapshotID: snapshotID, Mode: flowMode(options), Options: options}
	var exists, complete bool
	var seeds, nodes, edges []byte
	var rootClass int
	err = s.pool.QueryRow(ctx, flowSQL, repositoryID, snapshotID, options.SymbolID, options.FeatureID, options.Depth, options.Limit).
		Scan(&exists, &seeds, &nodes, &edges, &flow.Truncated, &rootClass, &complete)
	if err := pageError(err, exists); err != nil {
		return flow, err
	}
	if err := decodeFlow(&flow, seeds, nodes, edges); err != nil {
		return flow, err
	}
	domain.AnnotateFlow(&flow)
	flow.Limitations = flowLimitations(flow, rootClass, complete)
	return flow, nil
}

func decodeFlow(flow *domain.CodeFlow, seeds, nodes, edges []byte) error {
	if err := decodeJSON(seeds, &flow.SeedIDs); err != nil {
		return err
	}
	if err := decodeJSON(nodes, &flow.Nodes); err != nil {
		return err
	}
	for i := range flow.Nodes {
		symbol := &flow.Nodes[i].Symbol
		symbol.Documentation = goparser.DocumentationFor(*symbol)
	}
	return decodeJSON(edges, &flow.Edges)
}

func flowMode(options domain.FlowOptions) string {
	if options.SymbolID != "" {
		return "symbol"
	}
	if options.FeatureID != "" {
		return "feature"
	}
	return "application"
}

func flowLimitations(flow domain.CodeFlow, rootClass int, complete bool) []string {
	limitations := []string{
		"This is an indexed call graph, not runtime control flow, execution order, or proof of application entry points.",
		"Only resolved local calls form edges. Unresolved counts combine unresolved and external call sites; builtin calls are not drawn.",
		"Incoming and outgoing counts are distinct resolved callers and callees across the full snapshot. Candidate roots have no indexed resolved callers; boundary nodes have omitted upstream callers.",
		"Caller ancestry is expanded above the selected evidence. Descendants expand from the selected evidence, not unrelated branches of its ancestors.",
	}
	if !complete {
		limitations = append(limitations, "Call analysis is incomplete; dynamic dispatch, excluded source, and unsupported constructs can hide relationships.")
	}
	if flow.Truncated {
		limitations = append(limitations, "Seed, depth, node, or edge limits omit part of the reachable flow.")
	}
	if flow.Mode == "application" {
		limitations = append(limitations, applicationRootLimitation(rootClass))
	}
	return limitations
}

func applicationRootLimitation(rootClass int) string {
	switch rootClass {
	case 0:
		return "Application seeds are recognized main.main and init declarations; package initialization order is not reconstructed. Disconnected components may not be shown."
	case 1:
		return "No main.main or init roots were found. Zero-caller functions, methods, and closures are candidate roots, not proven handlers or entry points."
	case 2:
		return "No recognized or zero-caller roots were found. A deterministic callable seeds one component; cycles, initialization records, or missing callers can prevent a callable root. Disconnected components may not be shown."
	default:
		return "No callable application seeds were found in this indexed snapshot."
	}
}
