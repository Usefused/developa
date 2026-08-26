package postgres

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"developa/internal/application"
	"developa/internal/domain"
	goparser "developa/internal/indexer/golang"
)

func TestIntegrationFlowIncludesAncestorsAndSelectedDescendantsOnly(t *testing.T) {
	store, counter := catalogFixture(t)
	report, ids := branchingFlowReport(t)
	snapshot := saveReport(t, store, "repo", report)
	counter.Store(0)
	flow, err := store.Flow(context.Background(), "repo", snapshot.ID, domain.FlowOptions{SymbolID: ids["Selected"]})
	if err != nil || counter.Load() != 1 {
		t.Fatalf("flow read/query budget: %v queries=%d", err, counter.Load())
	}
	if flow.Mode != "symbol" || flow.Truncated || len(flow.Nodes) != 6 || len(flow.Edges) != 7 {
		t.Fatalf("unexpected selected flow: %+v", flow)
	}
	nodes := flowNodesByName(flow)
	if _, ok := nodes["Sibling"]; ok {
		t.Fatal("ancestor's unrelated sibling branch was expanded")
	}
	if _, ok := nodes["Other"]; ok {
		t.Fatal("callers of descendants were incorrectly expanded as seed ancestors")
	}
	assertBranchingFlowCounts(t, nodes)
	assertFlowCycleAnnotations(t, flow)
	assertFlowConnectedToSeeds(t, flow)
}

func assertFlowCycleAnnotations(t *testing.T, flow domain.CodeFlow) {
	t.Helper()
	if len(flow.CycleGroups) != 1 || len(flow.CycleGroups[0]) != 2 {
		t.Fatal("bounded graph cycle annotations were not published")
	}
}

func assertBranchingFlowCounts(t *testing.T, nodes map[string]domain.FlowNode) {
	t.Helper()
	if nodes["main"].RootKind != "main" || !nodes["Selected"].Seed {
		t.Fatal("recognized root or selected seed missing")
	}
	shared := nodes["Shared"]
	if shared.IncomingCount != 3 || shared.OutgoingCount != 1 || shared.RootKind != "boundary" {
		t.Fatalf("full-snapshot shared dependency counts were lost: %+v", shared)
	}
	if len(shared.IncomingIDs) != 2 || len(shared.OutgoingIDs) != 1 {
		t.Fatal("visible neighbor IDs were confused with full-snapshot counts")
	}
	if nodes["Selected"].UnresolvedCount != 2 || nodes["Selected"].OutgoingCount != 1 {
		t.Fatal("unresolved/external calls were confused with local resolved callees")
	}
}

func TestIntegrationFlowApplicationAndCandidateRoots(t *testing.T) {
	store, _ := catalogFixture(t)
	report, _ := flowReport(t, "package main\nfunc main(){}\nfunc init(){}\nfunc Used(){}\nfunc Disconnected(){}\n", [][2]string{{"main", "Used"}}, "recognized-roots")
	snapshot := saveReport(t, store, "repo", report)
	flow := readFlow(t, store, snapshot.ID, domain.FlowOptions{})
	if flow.Mode != "application" || len(flow.SeedIDs) != 2 || len(flow.Nodes) != 3 || flow.Truncated {
		t.Fatalf("application root selection invalid: %+v", flow)
	}
	nodes := flowNodesByName(flow)
	if nodes["init"].RootKind != "init" || nodes["main"].RootKind != "main" {
		t.Fatal("main and initialization roots were not distinguished")
	}
	assertLibraryFlowRoots(t, store)
}

func TestIntegrationFlowClosedCycleFallback(t *testing.T) {
	store, _ := catalogFixture(t)
	cycle := saveReport(t, store, "repo", callReport(t, 3, "closed-library-cycle"))
	flow := readFlow(t, store, cycle.ID, domain.FlowOptions{})
	if len(flow.SeedIDs) != 1 || len(flow.Nodes) != 3 || flow.Truncated {
		t.Fatal("closed library cycle was lost or expanded repeatedly")
	}
	if !strings.Contains(strings.Join(flow.Limitations, " "), "No recognized or zero-caller roots") {
		t.Fatal("arbitrary cycle fallback was presented as a proven entry point")
	}
}

func assertLibraryFlowRoots(t *testing.T, store *Store) {
	t.Helper()
	report, _ := flowReport(t, "package library\nfunc main(){}\nfunc Other(){}\nfunc Shared(){}\n", [][2]string{{"main", "Shared"}, {"Other", "Shared"}}, "candidate-roots")
	snapshot := saveReport(t, store, "repo", report)
	flow := readFlow(t, store, snapshot.ID, domain.FlowOptions{})
	nodes := flowNodesByName(flow)
	if len(flow.SeedIDs) != 2 || nodes["main"].RootKind != "candidate" || nodes["Shared"].IncomingCount != 2 {
		t.Fatal("library main was treated as an application entry or shared dependency duplicated")
	}
}

func TestIntegrationFlowFeatureEvidenceRetainsIsolatedNoncallableNodes(t *testing.T) {
	store, _ := catalogFixture(t)
	report, ids := branchingFlowReport(t)
	snapshot := saveReport(t, store, "repo", report)
	features := featureFixture(1, ids["Selected"])
	features[0].Evidence = append(features[0].Evidence, domain.Citation{SymbolID: ids["Evidence"]})
	saveFeatureFixture(t, store, snapshot, "flow-feature-run", features)
	flow := readFlow(t, store, snapshot.ID, domain.FlowOptions{FeatureID: features[0].ID})
	if flow.Mode != "feature" || len(flow.SeedIDs) != 2 || len(flow.Nodes) != 7 {
		t.Fatalf("feature evidence seeds missing: %+v", flow)
	}
	node := flowNodesByName(flow)["Evidence"]
	if !node.Seed || node.RootKind != "" || node.IncomingCount != 0 || node.OutgoingCount != 0 {
		t.Fatal("noncallable evidence was presented as an entry point or discarded")
	}
	assertFeatureSeedCap(t, store, snapshot.ID, features[0].ID)
}

func assertFeatureSeedCap(t *testing.T, store *Store, snapshotID, featureID string) {
	t.Helper()
	limited := readFlow(t, store, snapshotID, domain.FlowOptions{FeatureID: featureID, Limit: 1})
	if !limited.Truncated || len(limited.SeedIDs) != 1 || len(limited.Nodes) != 1 {
		t.Fatal("omitted feature evidence seeds were not disclosed")
	}
}

func TestIntegrationFlowDepthAndSeedCapsAreExplicit(t *testing.T) {
	store, _ := catalogFixture(t)
	source := "package library\nfunc Root(){}\nfunc A(){}\nfunc B(){}\nfunc Selected(){}\nfunc C(){}\nfunc D(){}\n"
	edges := [][2]string{{"Root", "A"}, {"A", "B"}, {"B", "Selected"}, {"Selected", "C"}, {"C", "D"}}
	report, ids := flowReport(t, source, edges, "flow-depth")
	snapshot := saveReport(t, store, "repo", report)
	flow := readFlow(t, store, snapshot.ID, domain.FlowOptions{SymbolID: ids["Selected"], Depth: 1})
	if !flow.Truncated || len(flow.Nodes) != 3 || flowNodesByName(flow)["B"].RootKind != "boundary" {
		t.Fatal("upstream/downstream depth cap was not represented honestly")
	}
	wide := saveReport(t, store, "repo", catalogReport(t, 100, "many-roots"))
	flow = readFlow(t, store, wide.ID, domain.FlowOptions{Limit: 10})
	if !flow.Truncated || len(flow.SeedIDs) != 10 || len(flow.Nodes) != 10 {
		t.Fatal("application seed fanout exceeded its bound")
	}
}

func TestIntegrationFlowPreservesDiscoveryForestUnderEdgeCap(t *testing.T) {
	store, counter := catalogFixture(t)
	report := discoveryReport(t)
	root := report.Index.Files[0].Symbols[2].ID
	for i := range 40 {
		call := report.Index.Calls[2]
		call.ID = fingerprint(fmt.Sprintf("duplicate-cycle-%d", i))
		report.Index.Calls = append(report.Index.Calls, call)
	}
	snapshot := saveReport(t, store, "repo", report)
	counter.Store(0)
	flow := readFlow(t, store, snapshot.ID, domain.FlowOptions{SymbolID: root, Limit: 3})
	if !flow.Truncated || len(flow.Nodes) != 3 || len(flow.Edges) != 12 || counter.Load() != 1 {
		t.Fatal("dense cyclic flow did not retain bounded edge/query counts")
	}
	assertFlowConnectedToSeeds(t, flow)
}

func branchingFlowReport(t *testing.T) (application.Report, map[string]string) {
	t.Helper()
	source := "package main\nfunc main(){}\nfunc Parent(){}\nfunc Selected(){}\nfunc Helper(){}\nfunc Shared(){}\nfunc Other(){}\nfunc Sibling(){}\nfunc Loop(){}\ntype Evidence struct{}\n"
	edges := [][2]string{{"main", "Parent"}, {"Parent", "Selected"}, {"Parent", "Sibling"}, {"Selected", "Helper"},
		{"Helper", "Shared"}, {"Helper", "Shared"}, {"Other", "Shared"}, {"Shared", "Loop"}, {"Loop", "Shared"}}
	report, ids := flowReport(t, source, edges, "branching-flow")
	for _, resolution := range []string{"unresolved", "external", "builtin"} {
		report.Index.Calls = append(report.Index.Calls, goparser.Call{ID: fingerprint("flow-" + resolution), CallerID: ids["Selected"],
			CallerName: "Selected", TargetName: "unlinked", Path: "flow.go", Resolution: resolution})
	}
	return report, ids
}

func flowReport(t *testing.T, source string, edges [][2]string, seed string) (application.Report, map[string]string) {
	t.Helper()
	index, err := goparser.Parse(context.Background(), []goparser.SourceFile{{Path: "flow.go", Content: []byte(source)}})
	if err != nil {
		t.Fatal(err)
	}
	ids := make(map[string]string)
	for _, symbol := range index.Files[0].Symbols {
		ids[symbol.Name] = symbol.ID
	}
	for i, edge := range edges {
		index.Calls = append(index.Calls, goparser.Call{ID: fingerprint(fmt.Sprintf("%s-%d", seed, i)), CallerID: ids[edge[0]], CallerName: edge[0],
			TargetID: ids[edge[1]], TargetName: edge[1], Resolution: "resolved", Path: "flow.go"})
	}
	index.CallAnalysis = goparser.CallAnalysis{Status: "complete", Resolved: len(edges)}
	return application.Report{Snapshot: application.SnapshotInfo{Fingerprint: fingerprint(seed), Complete: true, Files: 1}, Index: index}, ids
}

func readFlow(t *testing.T, store *Store, snapshotID string, options domain.FlowOptions) domain.CodeFlow {
	t.Helper()
	flow, err := store.Flow(context.Background(), "repo", snapshotID, options)
	if err != nil {
		t.Fatal(err)
	}
	return flow
}

func flowNodesByName(flow domain.CodeFlow) map[string]domain.FlowNode {
	nodes := make(map[string]domain.FlowNode)
	for _, node := range flow.Nodes {
		nodes[node.Symbol.Name] = node
	}
	return nodes
}

func assertFlowConnectedToSeeds(t *testing.T, flow domain.CodeFlow) {
	t.Helper()
	seen := make(map[string]bool)
	for _, id := range flow.SeedIDs {
		seen[id] = true
	}
	for range flow.Nodes {
		for _, edge := range flow.Edges {
			if seen[edge.CallerID] || seen[edge.TargetID] {
				seen[edge.CallerID], seen[edge.TargetID] = true, true
			}
		}
	}
	for _, node := range flow.Nodes {
		if !seen[node.Symbol.ID] {
			t.Fatalf("node %s lost its discovery path", node.Symbol.Name)
		}
	}
}

func TestIntegrationFlowRejectsMissingOrInvalidSelection(t *testing.T) {
	store, _ := catalogFixture(t)
	snapshot := saveReport(t, store, "repo", catalogReport(t, 1, "missing-flow"))
	for _, options := range []domain.FlowOptions{{SymbolID: fingerprint("missing")}, {FeatureID: fingerprint("missing")}} {
		_, err := store.Flow(context.Background(), "repo", snapshot.ID, options)
		if !errors.Is(err, domain.ErrNotFound) {
			t.Fatalf("missing flow selection returned %v", err)
		}
	}
	_, err := store.Flow(context.Background(), "repo", snapshot.ID, domain.FlowOptions{Depth: 13})
	if !errors.Is(err, domain.ErrInvalidInput) {
		t.Fatal("unbounded flow was accepted")
	}
}

func TestIntegrationFlowGlobalNodeBudgetAndQueryCount(t *testing.T) {
	store, counter := catalogFixture(t)
	for _, count := range []int{1, 100} {
		report := callReport(t, count, fmt.Sprintf("flow-budget-%d", count))
		snapshot := saveReport(t, store, "repo", report)
		root := report.Index.Files[0].Symbols[0].ID
		counter.Store(0)
		flow := readFlow(t, store, snapshot.ID, domain.FlowOptions{SymbolID: root, Depth: 12, Limit: 10})
		if counter.Load() != 1 || len(flow.Nodes) > 10 || len(flow.Edges) > 40 {
			t.Fatal("flow exceeded one query or its global node/edge budget")
		}
		if count == 100 && !flow.Truncated {
			t.Fatal("node budget omission was not disclosed")
		}
		assertFlowConnectedToSeeds(t, flow)
	}
}

func TestIntegrationFlowCannotCrossRepositoryOrSnapshot(t *testing.T) {
	store, _ := catalogFixture(t)
	report, ids := branchingFlowReport(t)
	old := saveReport(t, store, "repo", report)
	saveReport(t, store, "other", report)
	report.Snapshot.Fingerprint = fingerprint("new-flow-without-calls")
	report.Index.Calls = nil
	newer := saveReport(t, store, "repo", report)
	options := domain.FlowOptions{SymbolID: ids["Selected"]}
	current := readFlow(t, store, newer.ID, options)
	if len(current.Nodes) != 1 || len(current.Edges) != 0 {
		t.Fatal("new snapshot inherited calls from another scope")
	}
	previous := readFlow(t, store, old.ID, options)
	if len(previous.Nodes) != 6 || len(previous.Edges) != 7 {
		t.Fatal("older flow was changed by a new publication")
	}
	_, err := store.Flow(context.Background(), "other", newer.ID, options)
	if !errors.Is(err, domain.ErrNotFound) {
		t.Fatal("flow escaped repository scope")
	}
}

func TestIntegrationFlowFeatureMustBelongToCurrentGeneration(t *testing.T) {
	store, _ := catalogFixture(t)
	report, ids := branchingFlowReport(t)
	snapshot := saveReport(t, store, "repo", report)
	features := featureFixture(1, ids["Selected"])
	saveFeatureFixture(t, store, snapshot, "older-flow-feature", features)
	oldID := features[0].ID
	features[0].ID = fingerprint("new-flow-feature")
	saveFeatureFixture(t, store, snapshot, "current-flow-feature", features)
	_, err := store.Flow(context.Background(), "repo", snapshot.ID, domain.FlowOptions{FeatureID: oldID})
	if !errors.Is(err, domain.ErrNotFound) {
		t.Fatal("flow selected stale feature evidence")
	}
	flow := readFlow(t, store, snapshot.ID, domain.FlowOptions{FeatureID: features[0].ID})
	if len(flow.SeedIDs) != 1 || flow.SeedIDs[0] != ids["Selected"] {
		t.Fatal("current feature flow lost its canonical evidence")
	}
}

func TestIntegrationFlowWithoutCallablesHasExplicitEmptyArrays(t *testing.T) {
	store, _ := catalogFixture(t)
	report, _ := flowReport(t, "package library\ntype Record struct{}\n", nil, "types-only")
	snapshot := saveReport(t, store, "repo", report)
	flow := readFlow(t, store, snapshot.ID, domain.FlowOptions{})
	if flow.Nodes == nil || flow.Edges == nil || flow.SeedIDs == nil || len(flow.Nodes) != 0 || flow.Truncated {
		t.Fatalf("empty flow was not represented explicitly: %+v", flow)
	}
}
