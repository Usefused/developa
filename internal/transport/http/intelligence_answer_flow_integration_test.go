package httptransport

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"developa/internal/application"
	"developa/internal/domain"
)

const integratedFlowSource = "package fixture\n// Original returns a greeting through Helper.\nfunc Original(name string) string { return Helper(name) }\nfunc Helper(name string) string { return Tail(name) }\nfunc Tail(name string) string { return name }\n"

func TestIntegrationExplicitFlowAnswersPersistCacheAndRemainSnapshotScoped(t *testing.T) {
	fixture, model := newIntelligenceIntegration(t)
	integrationWrite(t, fixture.root, "main.go", integratedFlowSource)
	fixture.manager.Start(context.Background())
	snapshot := awaitIntegrationSnapshot(t, fixture, "")
	graph := readIntegratedFlow(t, fixture, snapshot.ID, "")
	if len(graph.Edges) != 2 || model.calls.Load() != 0 {
		t.Fatal("structural flow read invoked inference or lost resolved relationships")
	}
	root := integratedFlowSymbol(t, graph, "Original")
	request := domain.AnswerRequest{Question: "Explain this static flow using only the captured relationships.", Flow: &domain.FlowOptions{SymbolID: root.Symbol.ID}}
	first := integrationStreamAnswer(t, fixture, snapshot.ID, request)
	second := integrationStreamAnswer(t, fixture, snapshot.ID, request)
	assertIntegratedFlowCache(t, model, first, second)
	assertIntegratedFlowRebind(t, fixture, model, snapshot.ID, request, first)
	assertMissingIntegratedFlowScopes(t, fixture, model, snapshot.ID)
}

func readIntegratedFlow(t *testing.T, fixture *integrationExplorer, snapshot, query string) domain.CodeFlow {
	t.Helper()
	var flow domain.CodeFlow
	integrationRead(t, fixture, "/api/snapshots/"+snapshot+"/flow"+query, &flow)
	if flow.SnapshotID != snapshot || flow.Options.Depth != 6 || flow.Options.Limit != 80 {
		t.Fatal("flow API lost its scoped normalized options")
	}
	return flow
}

func integratedFlowSymbol(t *testing.T, flow domain.CodeFlow, name string) domain.FlowNode {
	t.Helper()
	for _, node := range flow.Nodes {
		if node.Symbol.Name == name {
			return node
		}
	}
	t.Fatalf("expected flow node %s", name)
	return domain.FlowNode{}
}

func assertIntegratedFlowCache(t *testing.T, model *protocolModel, first, second domain.Answer) {
	t.Helper()
	if first.Cached || !second.Cached || model.calls.Load() != 1 || second.ID == first.ID {
		t.Fatal("repeated explicit flow spent inference or reused publication identity")
	}
	if !strings.Contains(strings.Join(second.Limitations, " "), "static graph") {
		t.Fatal("flow explanation did not disclose static analysis limitations")
	}
}

func assertIntegratedFlowRebind(t *testing.T, fixture *integrationExplorer, model *protocolModel, previous string, request domain.AnswerRequest, first domain.Answer) {
	t.Helper()
	integrationWrite(t, fixture.root, "main.go", "\n"+integratedFlowSource)
	snapshot := awaitIntegrationSnapshot(t, fixture, previous)
	answer := integrationStreamAnswer(t, fixture, snapshot.ID, request)
	if !answer.Cached || model.calls.Load() != 1 || answer.Evidence[0].Span.Start.Line != first.Evidence[0].Span.Start.Line+1 {
		t.Fatal("unchanged flow after line shift consumed inference or retained stale citations")
	}
	old := integrationStreamAnswer(t, fixture, previous, request)
	if !old.Cached || old.Evidence[0].Span != first.Evidence[0].Span {
		t.Fatal("old snapshot answer followed current source positions")
	}
}

func assertMissingIntegratedFlowScopes(t *testing.T, fixture *integrationExplorer, model *protocolModel, snapshot string) {
	t.Helper()
	before := model.calls.Load()
	for _, selection := range []string{"symbol_id", "feature_id"} {
		body := `{"question":"Explain","flow":{"` + selection + `":"` + strings.Repeat("f", 64) + `"}}`
		for _, suffix := range []string{"/answers", "/answers/stream"} {
			status := integrationPostJSON(t, fixture, "/api/snapshots/"+snapshot+suffix, body, nil)
			if status != http.StatusNotFound {
				t.Fatalf("missing flow selector returned %d", status)
			}
		}
	}
	if model.calls.Load() != before {
		t.Fatal("missing flow selector invoked inference")
	}
}

func TestIntegrationExplicitFeatureFlowUsesCurrentFeatureGeneration(t *testing.T) {
	fixture, model := newIntelligenceIntegration(t)
	integrationWrite(t, fixture.root, "main.go", integratedFlowSource)
	fixture.manager.Start(context.Background())
	snapshot := awaitIntegrationSnapshot(t, fixture, "")
	page := runIntegratedFeaturePage(t, fixture, snapshot.ID)
	feature := page.Items[0]
	flow := readIntegratedFlow(t, fixture, snapshot.ID, "?feature_id="+feature.ID)
	if flow.Mode != "feature" || len(flow.SeedIDs) != len(feature.Evidence) {
		t.Fatal("feature flow did not derive seeds from canonical current evidence")
	}
	request := domain.AnswerRequest{Question: "Explain this feature's supported flow.", Flow: &flow.Options}
	assertExplicitAnswerStreamCache(t, fixture, model, snapshot.ID, request)
	assertReplacedFeatureFlowRejected(t, fixture, model, snapshot.ID, feature.ID, request)
}

func assertReplacedFeatureFlowRejected(t *testing.T, fixture *integrationExplorer, model *protocolModel, snapshot, oldID string, request domain.AnswerRequest) {
	t.Helper()
	page := runIntegratedFeaturePage(t, fixture, snapshot)
	if page.Items[0].ID == oldID {
		t.Fatal("manual feature rebuild did not publish a new generation")
	}
	before := model.calls.Load()
	body, err := json.Marshal(request)
	if err != nil {
		t.Fatal(err)
	}
	for _, suffix := range []string{"/answers", "/answers/stream"} {
		status := integrationPostJSON(t, fixture, "/api/snapshots/"+snapshot+suffix, string(body), nil)
		if status != http.StatusNotFound || model.calls.Load() != before {
			t.Fatal("replaced feature flow reused old evidence or triggered inference")
		}
	}
}

func TestIntegrationSavedExplanationsSurviveRestartAndInvalidateChangedEvidence(t *testing.T) {
	fixture, model := newIntelligenceIntegration(t)
	integrationWrite(t, fixture.root, "main.go", integratedFlowSource)
	fixture.manager.Start(context.Background())
	snapshot := awaitIntegrationSnapshot(t, fixture, "")
	root := integratedFlowSymbol(t, readIntegratedFlow(t, fixture, snapshot.ID, ""), "Original")
	request := domain.AnswerRequest{Question: "Explain this function and its context", SymbolID: root.Symbol.ID}
	if lookupIntegratedAnswer(t, fixture, snapshot.ID, request) != nil || model.calls.Load() != 0 {
		t.Fatal("opening an unexplained function invoked inference")
	}
	first := integrationStreamAnswer(t, fixture, snapshot.ID, request)
	restartAnswerReadsWithoutModel(t, fixture)
	assertRestoredAnswer(t, lookupIntegratedAnswer(t, fixture, snapshot.ID, request), first, snapshot.ID, 0)
	shifted := assertUnrelatedAnswerReuse(t, fixture, snapshot, request, first)
	assertChangedAnswerInvalidation(t, fixture, shifted.ID, request)
	assertRestoredAnswer(t, lookupIntegratedAnswer(t, fixture, snapshot.ID, request), first, snapshot.ID, 0)
	if model.calls.Load() != 1 {
		t.Fatal("saved lookups invoked new inference")
	}
}

func lookupIntegratedAnswer(t *testing.T, fixture *integrationExplorer, snapshot string, request domain.AnswerRequest) *domain.Answer {
	t.Helper()
	body, err := json.Marshal(request)
	if err != nil {
		t.Fatal(err)
	}
	var result struct {
		Answer *domain.Answer `json:"answer"`
	}
	path := "/api/repositories/" + fixture.manager.Repository().ID + "/snapshots/" + snapshot + "/answers/lookup"
	if status := integrationPostJSON(t, fixture, path, string(body), &result); status != http.StatusOK {
		t.Fatalf("saved lookup returned %d", status)
	}
	return result.Answer
}

func restartAnswerReadsWithoutModel(t *testing.T, fixture *integrationExplorer) {
	t.Helper()
	service, err := application.NewIntelligence(fixture.store, nil, application.IntelligenceConfig{RepositoryID: fixture.manager.Repository().ID})
	if err != nil {
		t.Fatal(err)
	}
	fixture.server.Close()
	cfg := testConfig()
	cfg.Explorer = &Explorer{Catalog: fixture.store, Tracker: fixture.manager, Knowledge: fixture.store,
		RepositoryID: fixture.manager.Repository().ID, Token: testToken, Intelligence: service}
	fixture.server = httptest.NewServer(NewHandler(fixture.store, cfg))
	t.Cleanup(fixture.server.Close)
}

func assertUnrelatedAnswerReuse(t *testing.T, fixture *integrationExplorer, previous domain.Snapshot, request domain.AnswerRequest, first domain.Answer) domain.Snapshot {
	t.Helper()
	// A new file and shifted physical lines must not discard an unchanged function's document.
	integrationWrite(t, fixture.root, "unrelated.go", "package fixture\nfunc Unrelated() string { return \"other\" }\n")
	integrationWrite(t, fixture.root, "main.go", "\n\n"+integratedFlowSource)
	shifted := awaitIntegrationSnapshot(t, fixture, previous.ID)
	assertRestoredAnswer(t, lookupIntegratedAnswer(t, fixture, shifted.ID, request), first, shifted.ID, 2)
	return shifted
}

func assertRestoredAnswer(t *testing.T, answer *domain.Answer, first domain.Answer, snapshot string, lineDelta int) {
	t.Helper()
	if answer == nil || answer.ID != first.ID || answer.Text != first.Text || !answer.Cached {
		t.Fatal("saved explanation was not restored")
	}
	if answer.GeneratedSnapshotID != first.SnapshotID || answer.SnapshotID != snapshot || len(answer.Evidence) != len(first.Evidence) {
		t.Fatal("saved explanation lost source provenance")
	}
	if answer.Evidence[0].Span.Start.Line != first.Evidence[0].Span.Start.Line+lineDelta {
		t.Fatal("saved citations were not rebound to the selected source")
	}
}

func assertChangedAnswerInvalidation(t *testing.T, fixture *integrationExplorer, previous string, request domain.AnswerRequest) {
	t.Helper()
	modified := strings.Replace(integratedFlowSource, "return Helper(name)", `return Helper(name + "!")`, 1)
	integrationWrite(t, fixture.root, "main.go", modified)
	changed := awaitIntegrationSnapshot(t, fixture, previous)
	if lookupIntegratedAnswer(t, fixture, changed.ID, request) != nil {
		t.Fatal("modified function displayed a stale explanation")
	}
	modified = strings.Replace(integratedFlowSource, "return Tail(name)", `return Tail(name + "callee changed")`, 1)
	integrationWrite(t, fixture.root, "main.go", modified)
	calleeChanged := awaitIntegrationSnapshot(t, fixture, changed.ID)
	if lookupIntegratedAnswer(t, fixture, calleeChanged.ID, request) != nil {
		t.Fatal("changed supporting implementation reused stale context")
	}
}
