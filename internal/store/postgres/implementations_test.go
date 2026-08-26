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

func TestIntegrationImplementationsArePagedWithConstantQueryBudget(t *testing.T) {
	store, counter := catalogFixture(t)
	for _, count := range []int{1, 10, 100} {
		report, ids := implementationReport(t, count, fmt.Sprint(count))
		counter.Store(0)
		snapshot, err := store.SaveSnapshot(context.Background(), "repo", report, testExecution())
		if err != nil || counter.Load() != 11 {
			t.Fatalf("candidate publication must use 11 fixed queries: count=%d queries=%d err=%v", count, counter.Load(), err)
		}
		counter.Store(0)
		page, err := store.Implementations(context.Background(), "repo", snapshot.ID, ids["Runner"], domain.ImplementationOptions{Limit: 1})
		if err != nil || page.Total != count || len(page.Items) != 1 || counter.Load() != 1 {
			t.Fatalf("candidate page/budget failed: count=%d page=%+v err=%v", count, page, err)
		}
		assertImplementationEvidence(t, page.Items[0])
		assertImplementationMethodPage(t, store, snapshot.ID, page.Items[0].Method.SymbolID, count)
	}
}

func assertImplementationEvidence(t *testing.T, candidate goparser.Implementation) {
	t.Helper()
	if candidate.Evidence != "go_types_method_set" || !candidate.Pointer {
		t.Fatalf("candidate evidence changed: %+v", candidate)
	}
	for _, ref := range []goparser.SymbolReference{candidate.Interface, candidate.Method, candidate.Receiver, candidate.Target} {
		if ref.SymbolID == "" || ref.Path != "example.go" || ref.Span.Start.Line < 2 || ref.Span.End.Offset <= ref.Span.Start.Offset {
			t.Fatalf("candidate source location missing: %+v", ref)
		}
	}
}

func assertImplementationMethodPage(t *testing.T, store *Store, snapshot, method string, count int) {
	t.Helper()
	page, err := store.Implementations(context.Background(), "repo", snapshot, method, domain.ImplementationOptions{Limit: 1, Offset: count})
	if err != nil || page.Total != count || len(page.Items) != 0 || page.Analysis.Status != "complete" {
		t.Fatalf("method selector/empty page lost analysis: %+v %v", page, err)
	}
}

func TestIntegrationImplementationCandidatesNeverBecomeResolvedCalls(t *testing.T) {
	store, _ := catalogFixture(t)
	report, ids := implementationReport(t, 2, "candidate-calls")
	snapshot := saveReport(t, store, "repo", report)
	page, err := store.Calls(context.Background(), "repo", snapshot.ID, domain.CallFilter{SymbolID: ids["Use"]})
	if err != nil || page.Total != 1 {
		t.Fatalf("expected interface call: %+v %v", page, err)
	}
	call := page.Items[0]
	requireUnresolvedInterfaceReference(t, call)
	implementations, err := store.Implementations(context.Background(), "repo", snapshot.ID, call.InterfaceMethod.SymbolID, domain.ImplementationOptions{})
	if err != nil || implementations.Total != 2 {
		t.Fatalf("interface call did not lead to candidates: %+v %v", implementations, err)
	}
	chain, err := store.Chain(context.Background(), "repo", snapshot.ID, ids["Use"], domain.ChainOptions{})
	if err != nil || len(chain.Nodes) != 1 || len(chain.Edges) != 0 {
		t.Fatalf("resolved chain followed speculative candidates: %+v %v", chain, err)
	}
}

func requireUnresolvedInterfaceReference(t *testing.T, call goparser.Call) {
	t.Helper()
	if call.Resolution != "unresolved" || call.TargetID != "" || call.Target != nil || call.InterfaceMethod == nil {
		t.Fatalf("candidate was promoted to resolved call: %+v", call)
	}
}

func TestIntegrationImplementationSnapshotAndRepositoryIsolation(t *testing.T) {
	store, _ := catalogFixture(t)
	report, ids := implementationReport(t, 2, "old-implementations")
	old := saveReport(t, store, "repo", report)
	saveReport(t, store, "other", report)
	report.Snapshot.Fingerprint = fingerprint("new-implementations")
	report.Index.Implementations = nil
	newer := saveReport(t, store, "repo", report)
	for _, expected := range []struct {
		snapshot string
		total    int
	}{{old.ID, 2}, {newer.ID, 0}} {
		page, err := store.Implementations(context.Background(), "repo", expected.snapshot, ids["Runner"], domain.ImplementationOptions{})
		if err != nil || page.Total != expected.total {
			t.Fatalf("candidate snapshot isolation failed: %+v %v", page, err)
		}
	}
	_, err := store.Implementations(context.Background(), "other", newer.ID, ids["Runner"], domain.ImplementationOptions{})
	if !errors.Is(err, domain.ErrNotFound) {
		t.Fatal("implementation read crossed repository scope")
	}
}

func TestIntegrationImplementationsDiscloseMissingLegacyAnalysis(t *testing.T) {
	store, _ := catalogFixture(t)
	report := catalogReport(t, 1, "legacy-implementation")
	snapshot := saveReport(t, store, "repo", report)
	page, err := store.Implementations(context.Background(), "repo", snapshot.ID, report.Index.Files[0].Symbols[0].ID, domain.ImplementationOptions{})
	if err != nil || page.Analysis.Status != "unavailable" || len(page.Analysis.Limitations) == 0 || page.Items == nil {
		t.Fatalf("legacy absence looked complete: %+v %v", page, err)
	}
}

func TestIntegrationInvalidImplementationAbortsSnapshotPublication(t *testing.T) {
	store, _ := catalogFixture(t)
	report, _ := implementationReport(t, 1, "valid-implementation")
	old := saveReport(t, store, "repo", report)
	report.Snapshot.Fingerprint = fingerprint("broken-implementation")
	report.Index.Implementations[0].Target.SymbolID = "missing-target"
	_, err := store.SaveSnapshot(context.Background(), "repo", report, testExecution())
	if !errors.Is(err, ErrUnavailable) {
		t.Fatalf("invalid candidate should roll back publication: %v", err)
	}
	assertLatestID(t, store, old.ID)
	assertTableCount(t, store, "developa_snapshots", 1)
	assertTableCount(t, store, "developa_implementations", 1)
	assertTableCount(t, store, "developa_audit_events", 1)
}

func TestImplementationOptionsRejectInvalidBoundsBeforeQuery(t *testing.T) {
	store := &Store{}
	for _, options := range []domain.ImplementationOptions{{Limit: -1}, {Limit: 101}, {Offset: -1}, {Offset: 100001}} {
		_, err := store.Implementations(context.Background(), "repo", "snapshot", "symbol", options)
		if !errors.Is(err, domain.ErrInvalidInput) {
			t.Fatalf("invalid bounds accepted: %+v %v", options, err)
		}
	}
}

func TestIntegrationImplementationReadHonorsCancellation(t *testing.T) {
	store, _ := catalogFixture(t)
	report, ids := implementationReport(t, 1, "cancel-implementation")
	snapshot := saveReport(t, store, "repo", report)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := store.Implementations(ctx, "repo", snapshot.ID, ids["Runner"], domain.ImplementationOptions{})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled candidate read continued: %v", err)
	}
}

func implementationReport(t *testing.T, count int, seed string) (application.Report, map[string]string) {
	t.Helper()
	var content strings.Builder
	content.WriteString("package example\ntype Runner interface { Run(int) string }\nfunc Use(r Runner) string { return r.Run(1) }\n")
	for i := range count {
		fmt.Fprintf(&content, "type Worker%d struct{}\nfunc (*Worker%d) Run(n int) string { return \"ok\" }\n", i, i)
	}
	files := []goparser.SourceFile{{Path: "go.mod", Content: []byte("module example\ngo 1.26\n")}, {Path: "example.go", Content: []byte(content.String())}}
	index, err := goparser.Parse(context.Background(), files)
	if err != nil {
		t.Fatal(err)
	}
	if err := goparser.AnalyzeCalls(context.Background(), files, &index); err != nil {
		t.Fatal(err)
	}
	ids := map[string]string{}
	for _, symbol := range index.Files[0].Symbols {
		ids[symbol.Name] = symbol.ID
	}
	return application.Report{Snapshot: application.SnapshotInfo{Fingerprint: fingerprint(seed), Complete: true, Files: 1}, Index: index}, ids
}
