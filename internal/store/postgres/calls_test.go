package postgres

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"developa/internal/application"
	"developa/internal/domain"
	goparser "developa/internal/indexer/golang"
)

func TestIntegrationCallsAreScopedFilteredAndPaged(t *testing.T) {
	store, counter := catalogFixture(t)
	report := callReport(t, 3, "calls")
	snapshot := saveReport(t, store, "repo", report)
	root := report.Index.Files[0].Symbols[0].ID
	counter.Store(0)
	page, err := store.Calls(context.Background(), "repo", snapshot.ID, domain.CallFilter{SymbolID: root, Direction: "out", Limit: 1})
	if err != nil || page.Total != 3 || len(page.Items) != 1 || counter.Load() != 1 {
		t.Fatalf("calls page/budget failed: %+v, %v", page, err)
	}
	assertCallDirections(t, store, snapshot, root)
	details, err := store.Details(context.Background(), "repo", snapshot.ID)
	if err != nil || details.Snapshot.IndexVersion != domain.IndexVersion || details.CallAnalysis.Resolved != 3 {
		t.Fatal("call analysis metadata did not persist")
	}
}

func assertCallDirections(t *testing.T, store *Store, snapshot domain.Snapshot, root string) {
	t.Helper()
	page, err := store.Calls(context.Background(), "repo", snapshot.ID, domain.CallFilter{SymbolID: root, Direction: "in", Resolution: "resolved"})
	if err != nil || page.Total != 1 || page.Items[0].TargetID != root {
		t.Fatalf("incoming calls filter failed: %+v, %v", page, err)
	}
	page, err = store.Calls(context.Background(), "repo", snapshot.ID, domain.CallFilter{SymbolID: root, Resolution: "external", Offset: 20})
	if err != nil || page.Total != 1 || len(page.Items) != 0 {
		t.Fatal("empty call page lost filtered total")
	}
}

func TestIntegrationChainCyclesAndBounds(t *testing.T) {
	store, counter := catalogFixture(t)
	report := callReport(t, 3, "chain-cycle")
	snapshot := saveReport(t, store, "repo", report)
	root := report.Index.Files[0].Symbols[0].ID
	counter.Store(0)
	chain, err := store.Chain(context.Background(), "repo", snapshot.ID, root, domain.ChainOptions{Depth: 5, Limit: 100})
	if err != nil || len(chain.Nodes) != 3 || len(chain.Edges) != 3 || chain.Truncated {
		t.Fatalf("cycle-safe chain failed: %+v, %v", chain, err)
	}
	if counter.Load() != 1 {
		t.Fatal("chain did not use one SQL query")
	}
	assertChainLimits(t, store, snapshot, root)
}

func assertChainLimits(t *testing.T, store *Store, snapshot domain.Snapshot, root string) {
	t.Helper()
	chain, err := store.Chain(context.Background(), "repo", snapshot.ID, root, domain.ChainOptions{Depth: 1, Limit: 100})
	if err != nil || !chain.Truncated || len(chain.Nodes) != 2 {
		t.Fatalf("depth limit was not disclosed: %+v, %v", chain, err)
	}
	chain, err = store.Chain(context.Background(), "repo", snapshot.ID, root, domain.ChainOptions{Depth: 5, Limit: 2, Direction: "in"})
	if err != nil || !chain.Truncated || len(chain.Nodes) > 2 || len(chain.Edges) > 2 {
		t.Fatalf("incoming chain exceeded cap: %+v, %v", chain, err)
	}
}

func TestIntegrationCallSnapshotAndRepositoryIsolation(t *testing.T) {
	store, _ := catalogFixture(t)
	report := callReport(t, 3, "call-old")
	old := saveReport(t, store, "repo", report)
	saveReport(t, store, "other", report)
	report.Snapshot.Fingerprint = fingerprint("call-new")
	report.Index.Calls = nil
	newer := saveReport(t, store, "repo", report)
	page, err := store.Calls(context.Background(), "repo", newer.ID, domain.CallFilter{})
	if err != nil || page.Total != 0 {
		t.Fatal("new snapshot inherited old calls")
	}
	page, err = store.Calls(context.Background(), "repo", old.ID, domain.CallFilter{})
	if err != nil || page.Total != 5 {
		t.Fatal("older call snapshot was changed")
	}
	_, err = store.Calls(context.Background(), "other", newer.ID, domain.CallFilter{})
	if !errors.Is(err, domain.ErrNotFound) {
		t.Fatal("call read crossed repository scope")
	}
}

func TestIntegrationContextRanksStoredSourceAndStaysBounded(t *testing.T) {
	store, counter := catalogFixture(t)
	report := catalogReport(t, 100, "context")
	report.Index.Files[9].Symbols[0].Source = "func Run009() { custodian_unique_body() }"
	snapshot := saveReport(t, store, "repo", report)
	counter.Store(0)
	pack, err := store.Context(context.Background(), "repo", snapshot.ID, "explain custodian_unique_body", 20)
	if err != nil || pack.Total != 1 || len(pack.Items) != 1 || pack.Items[0].Path != "file009.go" {
		t.Fatalf("source context relevance failed: %+v, %v", pack, err)
	}
	if counter.Load() != 1 {
		t.Fatal("context retrieval exceeded one query")
	}
	pack, err = store.Context(context.Background(), "repo", snapshot.ID, "", 20)
	if err != nil || len(pack.Items) != 20 || !pack.Truncated || pack.Total != 200 {
		t.Fatalf("context limit was not enforced: %+v, %v", pack, err)
	}
}

func TestIntelligenceReadRejectsInvalidBounds(t *testing.T) {
	store, _ := catalogFixture(t)
	_, callErr := store.Calls(context.Background(), "repo", "snapshot", domain.CallFilter{Direction: "sideways"})
	_, offsetErr := store.Calls(context.Background(), "repo", "snapshot", domain.CallFilter{Offset: 100001})
	_, chainErr := store.Chain(context.Background(), "repo", "snapshot", "root", domain.ChainOptions{Depth: 6})
	_, contextErr := store.Context(context.Background(), "repo", "snapshot", "query", 21)
	for _, err := range []error{callErr, offsetErr, chainErr, contextErr} {
		if !errors.Is(err, domain.ErrInvalidInput) {
			t.Fatalf("expected invalid filter, got %v", err)
		}
	}
}

func TestIntegrationChainPrioritizesDiscoveryEdgesOverCycles(t *testing.T) {
	store, counter := catalogFixture(t)
	report := discoveryReport(t)
	snapshot := saveReport(t, store, "repo", report)
	root := report.Index.Files[0].Symbols[2].ID
	for _, direction := range []string{"out", "in"} {
		counter.Store(0)
		chain, err := store.Chain(context.Background(), "repo", snapshot.ID, root, domain.ChainOptions{Direction: direction, Depth: 5, Limit: 3})
		if err != nil || len(chain.Nodes) != 3 || len(chain.Edges) != 3 || !chain.Truncated {
			t.Fatalf("discovery chain invalid: %+v, %v", chain, err)
		}
		if counter.Load() != 1 {
			t.Fatal("discovery provenance required extra SQL queries")
		}
		assertReturnedChainConnected(t, chain)
	}
}

func discoveryReport(t *testing.T) application.Report {
	t.Helper()
	index, err := goparser.Parse(context.Background(), []goparser.SourceFile{{Path: "chain.go", Content: []byte("package example\nfunc A() {}\nfunc B() {}\nfunc Root() {}\n")}})
	if err != nil {
		t.Fatal(err)
	}
	// The late Root declaration makes every low-line cycle sort before Root's
	// outgoing edge, which previously disconnected the returned graph at limit=3.
	for i, edge := range [][2]int{{2, 0}, {0, 1}, {0, 0}, {0, 2}, {1, 1}, {1, 0}} {
		caller, target := index.Files[0].Symbols[edge[0]], index.Files[0].Symbols[edge[1]]
		index.Calls = append(index.Calls, goparser.Call{ID: fingerprint(fmt.Sprintf("discovery-%d", i)),
			CallerID: caller.ID, CallerName: caller.Name, TargetID: target.ID, TargetName: target.Name,
			Resolution: "resolved", Path: "chain.go", Span: caller.Span})
	}
	return application.Report{Snapshot: application.SnapshotInfo{Fingerprint: fingerprint("discovery"), Complete: true, Files: 1}, Index: index}
}

func assertReturnedChainConnected(t *testing.T, chain domain.CallChain) {
	t.Helper()
	seen := map[string]bool{chain.RootID: true}
	for range chain.Nodes {
		for _, edge := range chain.Edges {
			from, to := chainEndpoints(edge, chain.Direction)
			if seen[from] {
				seen[to] = true
			}
		}
	}
	for _, node := range chain.Nodes {
		if !seen[node.Symbol.ID] {
			t.Fatalf("returned node %s has no discovery path from root", node.Symbol.Name)
		}
	}
}

func chainEndpoints(call goparser.Call, direction string) (string, string) {
	if direction == "in" {
		return call.TargetID, call.CallerID
	}
	return call.CallerID, call.TargetID
}

func callReport(t *testing.T, count int, seed string) application.Report {
	t.Helper()
	report := catalogReport(t, count, seed)
	for i := range count {
		caller, target := report.Index.Files[i].Symbols[0], report.Index.Files[(i+1)%count].Symbols[0]
		report.Index.Calls = append(report.Index.Calls, goparser.Call{ID: fingerprint(fmt.Sprintf("call-%s-%d", seed, i)),
			CallerID: caller.ID, CallerName: caller.Name, TargetID: target.ID, TargetName: target.Name,
			Resolution: "resolved", Path: report.Index.Files[i].Path, Span: caller.Span})
	}
	appendUnresolvedCalls(&report, seed)
	report.Index.CallAnalysis = goparser.CallAnalysis{Status: "partial", Resolved: count, Unresolved: 1, Limitations: []string{"fixture"}}
	return report
}

func appendUnresolvedCalls(report *application.Report, seed string) {
	caller := report.Index.Files[0].Symbols[0]
	for _, resolution := range []string{"external", "unresolved"} {
		report.Index.Calls = append(report.Index.Calls, goparser.Call{ID: fingerprint(seed + resolution), CallerID: caller.ID,
			CallerName: caller.Name, TargetName: "candidate", Resolution: resolution, Path: report.Index.Files[0].Path, Span: caller.Span})
	}
}

func TestIntegrationChainHighFanoutDoesNotExceedBudget(t *testing.T) {
	store, counter := catalogFixture(t)
	report := callReport(t, 100, "fanout")
	root := report.Index.Files[0].Symbols[0].ID
	for i := range report.Index.Calls {
		report.Index.Calls[i].CallerID = root
	}
	snapshot := saveReport(t, store, "repo", report)
	counter.Store(0)
	chain, err := store.Chain(context.Background(), "repo", snapshot.ID, root, domain.ChainOptions{Depth: 5, Limit: 10})
	if err != nil || !chain.Truncated || len(chain.Nodes) != 10 || len(chain.Edges) > 10 {
		t.Fatalf("high fanout exceeded bounded traversal: %+v, %v", chain, err)
	}
	if counter.Load() != 1 {
		t.Fatal("high fanout created additional round trips")
	}
}

func TestIntegrationContextCannotCrossRepositoryOrSnapshot(t *testing.T) {
	store, _ := catalogFixture(t)
	original := saveReport(t, store, "repo", catalogReport(t, 1, "context-old"))
	report := catalogReport(t, 1, "context-new")
	report.Index.Files[0].Symbols[0].Source = "hiddencontexttoken"
	newer := saveReport(t, store, "repo", report)
	saveReport(t, store, "other", report)
	pack, err := store.Context(context.Background(), "repo", original.ID, "hiddencontexttoken", 20)
	if err != nil || pack.Total != 0 {
		t.Fatal("context leaked from another snapshot")
	}
	_, err = store.Context(context.Background(), "absent", newer.ID, "hiddencontexttoken", 20)
	if !errors.Is(err, domain.ErrNotFound) {
		t.Fatal("context leaked from another repository")
	}
}
