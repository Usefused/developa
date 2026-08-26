package domain

import "slices"

// AnnotateFlow computes graph facts on the already scoped, bounded SQL result.
// Keeping these facts in the API lets agents navigate shared dependencies and
// recursion without recreating browser-specific graph logic.
func AnnotateFlow(flow *CodeFlow) {
	incoming, outgoing := flowAdjacency(flow)
	for i := range flow.Nodes {
		id := flow.Nodes[i].Symbol.ID
		flow.Nodes[i].IncomingIDs = incoming[id]
		flow.Nodes[i].OutgoingIDs = outgoing[id]
		flow.Nodes[i].Description, flow.Nodes[i].DescriptionSource = reviewedFlowDescription(flow.Nodes[i].SymbolDetail)
	}
	flow.CycleGroups = flowCycles(outgoing, incoming)
}

func flowAdjacency(flow *CodeFlow) (map[string][]string, map[string][]string) {
	incoming, outgoing := map[string][]string{}, map[string][]string{}
	for _, node := range flow.Nodes {
		incoming[node.Symbol.ID], outgoing[node.Symbol.ID] = []string{}, []string{}
	}
	for _, edge := range flow.Edges {
		outgoing[edge.CallerID] = append(outgoing[edge.CallerID], edge.TargetID)
		incoming[edge.TargetID] = append(incoming[edge.TargetID], edge.CallerID)
	}
	uniqueFlowNeighbors(incoming)
	uniqueFlowNeighbors(outgoing)
	return incoming, outgoing
}

func uniqueFlowNeighbors(neighbors map[string][]string) {
	for id, values := range neighbors {
		slices.Sort(values)
		neighbors[id] = slices.Compact(values)
	}
}

func flowCycles(outgoing, incoming map[string][]string) [][]string {
	ids := make([]string, 0, len(outgoing))
	for id := range outgoing {
		ids = append(ids, id)
	}
	slices.Sort(ids)
	order := []string{}
	seen := map[string]bool{}
	for _, id := range ids {
		flowFinish(id, outgoing, seen, &order)
	}
	return flowComponents(order, outgoing, incoming)
}

func flowFinish(id string, adjacency map[string][]string, seen map[string]bool, order *[]string) {
	if seen[id] {
		return
	}
	seen[id] = true
	for _, next := range adjacency[id] {
		flowFinish(next, adjacency, seen, order)
	}
	*order = append(*order, id)
}

func flowComponents(order []string, outgoing, incoming map[string][]string) [][]string {
	groups := [][]string{}
	seen := map[string]bool{}
	for i := len(order) - 1; i >= 0; i-- {
		if seen[order[i]] {
			continue
		}
		group := []string{}
		flowFinish(order[i], incoming, seen, &group)
		if len(group) > 1 || slices.Contains(outgoing[order[i]], order[i]) {
			slices.Sort(group)
			groups = append(groups, group)
		}
	}
	slices.SortFunc(groups, func(a, b []string) int { return slices.Compare(a, b) })
	return groups
}
