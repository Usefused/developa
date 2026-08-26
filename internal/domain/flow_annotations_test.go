package domain

import (
	"reflect"
	"testing"

	goparser "developa/internal/indexer/golang"
)

func TestFlowAnnotationsSharedDependencies(t *testing.T) {
	flow := annotatedFixture([]string{"main", "a", "b", "util"}, [][2]string{{"main", "a"}, {"main", "b"}, {"a", "util"}, {"a", "util"}, {"b", "util"}})
	AnnotateFlow(&flow)
	if !reflect.DeepEqual(flow.Nodes[3].IncomingIDs, []string{"a", "b"}) || len(flow.CycleGroups) != 0 {
		t.Fatalf("wrong shared dependency facts: %+v", flow)
	}
	if !reflect.DeepEqual(flow.Nodes[0].OutgoingIDs, []string{"a", "b"}) || len(flow.Edges) != 5 {
		t.Fatal("annotation must retain all call sites and deduplicate navigation IDs")
	}
}

func TestFlowAnnotationsRecursionAndSelfCalls(t *testing.T) {
	flow := annotatedFixture([]string{"main", "a", "b", "self", "unused"}, [][2]string{{"main", "a"}, {"a", "b"}, {"b", "a"}, {"self", "self"}})
	AnnotateFlow(&flow)
	if !reflect.DeepEqual(flow.CycleGroups, [][]string{{"a", "b"}, {"self"}}) {
		t.Fatalf("wrong strongly connected groups: %+v", flow.CycleGroups)
	}
	if flow.Nodes[4].IncomingIDs == nil || flow.Nodes[4].OutgoingIDs == nil {
		t.Fatal("isolated nodes must have empty navigation arrays, not null")
	}
}

func TestFlowAnnotationsEmptyAndRepeatable(t *testing.T) {
	flow := CodeFlow{}
	AnnotateFlow(&flow)
	if flow.CycleGroups == nil || len(flow.CycleGroups) != 0 {
		t.Fatal("empty graph must expose an empty cycle array")
	}
	flow = annotatedFixture([]string{"z", "a"}, [][2]string{{"a", "z"}, {"z", "a"}})
	AnnotateFlow(&flow)
	first := flow.CycleGroups
	AnnotateFlow(&flow)
	if !reflect.DeepEqual(first, flow.CycleGroups) {
		t.Fatal("annotations changed without a source change")
	}
}

func annotatedFixture(ids []string, edges [][2]string) CodeFlow {
	flow := CodeFlow{}
	for _, id := range ids {
		flow.Nodes = append(flow.Nodes, FlowNode{SymbolDetail: SymbolDetail{Symbol: goparser.Symbol{ID: id}}})
	}
	for _, pair := range edges {
		flow.Edges = append(flow.Edges, goparser.Call{CallerID: pair[0], TargetID: pair[1], Resolution: "resolved"})
	}
	return flow
}
