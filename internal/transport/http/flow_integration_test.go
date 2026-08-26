package httptransport

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"developa/internal/domain"
)

func TestIntegrationFlowFromGitThroughPostgresAndHTTP(t *testing.T) {
	fixture := newIntegrationExplorer(t)
	integrationWrite(t, fixture.root, "main.go", "package main\nfunc main(){ Parent() }\nfunc Parent(){ Selected(); Sibling() }\n// Selected invokes the\n// fixture helper.\n//\n// Full documentation remains available.\nfunc Selected(){ /* Reuse the helper. */ Helper() }\nfunc Helper(){}\nfunc Sibling(){}\ntype Config struct{}\n")
	integrationWrite(t, fixture.root, "removed.go", "package main\nfunc Unrelated(){}\n")
	fixture.manager.Start(context.Background())
	snapshot := awaitIntegrationSnapshot(t, fixture, "")
	selected := flowIntegrationSymbol(t, fixture, snapshot.ID, "Selected")
	base := "/api/snapshots/" + snapshot.ID
	var flow domain.CodeFlow
	integrationRead(t, fixture, base+"/flow?symbol_id="+selected, &flow)
	assertIntegratedFlowGraph(t, flow, snapshot.ID, selected)
	assertIntegratedFeatureFlow(t, fixture, snapshot, selected)
	var capabilities struct {
		Flows bool `json:"flows"`
	}
	integrationRead(t, fixture, "/api/capabilities", &capabilities)
	if !capabilities.Flows {
		t.Fatal("flow support was not discoverable through the capabilities API")
	}
	if status := integrationRequest(t, fixture, http.MethodGet, base+"/flow", false, nil); status != http.StatusUnauthorized {
		t.Fatal("flow endpoint permitted anonymous source access")
	}
	if status := integrationRequest(t, fixture, http.MethodGet, "/api/snapshots/"+strings.Repeat("f", 64)+"/flow", true, nil); status != http.StatusNotFound {
		t.Fatal("flow endpoint did not reject an unknown snapshot")
	}
}

func flowIntegrationSymbol(t *testing.T, fixture *integrationExplorer, snapshot, name string) string {
	t.Helper()
	var page domain.SymbolPage
	integrationRead(t, fixture, "/api/snapshots/"+snapshot+"/symbols?q="+name, &page)
	if len(page.Items) != 1 {
		t.Fatalf("expected one %s declaration", name)
	}
	return page.Items[0].Symbol.ID
}

func assertIntegratedFlowGraph(t *testing.T, flow domain.CodeFlow, snapshot, seed string) {
	t.Helper()
	if flow.SnapshotID != snapshot || flow.Mode != "symbol" || len(flow.Nodes) != 4 || len(flow.Edges) != 3 || flow.Truncated {
		t.Fatalf("wrong persisted flow: %+v", flow)
	}
	if len(flow.SeedIDs) != 1 || flow.SeedIDs[0] != seed || flow.CycleGroups == nil {
		t.Fatal("flow API omitted seed or cycle annotations")
	}
	assertIntegratedFlowNodes(t, flow)
	assertIntegratedFlowDescriptions(t, flow)
}

func assertIntegratedFlowDescriptions(t *testing.T, flow domain.CodeFlow) {
	t.Helper()
	for _, node := range flow.Nodes {
		want, source := "No parameters or return values.", "signature"
		if node.Symbol.Name == "Selected" {
			want, source = "Selected invokes the fixture helper. Full documentation remains available. Reuse the helper.", "source_comments"
			if !strings.Contains(node.Symbol.Doc, "Full documentation remains available.") {
				t.Fatal("flow preview replaced full source documentation")
			}
		}
		if node.Symbol.Name == "Config" {
			want = "Struct with 0 declared fields."
		}
		if node.Description != want || node.DescriptionSource != source {
			t.Fatalf("flow API description for %s = %q (%s), want %q (%s)", node.Symbol.Name, node.Description, node.DescriptionSource, want, source)
		}
	}
}

func assertIntegratedFlowNodes(t *testing.T, flow domain.CodeFlow) {
	t.Helper()
	for _, node := range flow.Nodes {
		if node.Symbol.Name == "Sibling" || node.Symbol.Name == "Unrelated" {
			t.Fatal("flow API expanded an ancestor's unrelated branch")
		}
		if node.Symbol.Name == "main" && node.RootKind != "main" {
			t.Fatal("flow API lost the recognized application root")
		}
		if node.Seed && (len(node.IncomingIDs) != 1 || len(node.OutgoingIDs) != 1) {
			t.Fatal("flow API omitted explicit neighbor IDs for agent navigation")
		}
	}
}

func assertIntegratedFeatureFlow(t *testing.T, fixture *integrationExplorer, snapshot domain.Snapshot, selected string) {
	t.Helper()
	config := flowIntegrationSymbol(t, fixture, snapshot.ID, "Config")
	featureID := strings.Repeat("e", 64)
	run := domain.FeatureRun{ID: "flow-evidence-run", SnapshotID: snapshot.ID, Model: "fixture-output", Status: "completed",
		AnalyzedSymbols: snapshot.SymbolCount, TotalSymbols: snapshot.SymbolCount}
	features := []domain.Feature{{ID: featureID, Title: "Fixture flow", Summary: "Evidence fixture without inference", Status: "inferred",
		Evidence: []domain.Citation{{SymbolID: selected}, {SymbolID: config}}}}
	execution := domain.Execution{ID: "flow-evidence-execution", Actor: "system", Trigger: "flow_fixture"}
	if err := fixture.store.SaveFeatures(context.Background(), fixture.manager.Repository().ID, run, features, execution); err != nil {
		t.Fatal(err)
	}
	var flow domain.CodeFlow
	integrationRead(t, fixture, "/api/snapshots/"+snapshot.ID+"/flow?feature_id="+featureID, &flow)
	if flow.Mode != "feature" || len(flow.SeedIDs) != 2 || len(flow.Nodes) != 5 || len(flow.Edges) != 3 {
		t.Fatalf("feature flow did not retain canonical noncallable evidence: %+v", flow)
	}
	assertIntegratedFlowDescriptions(t, flow)
}
