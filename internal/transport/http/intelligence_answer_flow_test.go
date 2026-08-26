package httptransport

import (
	"net/http"
	"strings"
	"testing"

	"developa/internal/domain"
)

func TestFlowAnswerValidatesNestedScopeBeforeServiceOrStream(t *testing.T) {
	bodies := []string{
		`{"question":"Explain","flow":{"symbol_id":"bad"}}`,
		`{"question":"Explain","flow":{"feature_id":"bad"}}`,
		`{"question":"Explain","flow":{"symbol_id":"` + symbolID + `","feature_id":"` + symbolID + `"}}`,
		`{"question":"Explain","symbol_id":"` + symbolID + `","flow":{}}`,
		`{"question":"Explain","feature_id":"` + symbolID + `","flow":{}}`,
		`{"question":"Explain","flow":{"depth":13}}`,
		`{"question":"Explain","flow":{"depth":-1}}`,
		`{"question":"Explain","flow":{"limit":101}}`,
		`{"question":"Explain","flow":{"limit":-1}}`,
		`{"question":"Explain","flow":{"graph_hash":"untrusted"}}`,
		`{"question":"Explain","flow":[]}`,
	}
	for _, suffix := range []string{"/answers", "/answers/stream"} {
		for _, body := range bodies {
			handler, _, service := intelligenceFixture()
			response := authorizedRequest(handler, http.MethodPost, "/api/snapshots/"+snapshotID+suffix, body)
			if response.Code != http.StatusBadRequest || service.method != "" || strings.HasPrefix(response.Header().Get("Content-Type"), "text/event-stream") {
				t.Fatalf("invalid flow reached inference or committed stream headers: %d", response.Code)
			}
		}
	}
}

func TestFlowAnswerNormalizesAndForwardsPinnedSelection(t *testing.T) {
	cases := []struct {
		body    string
		options domain.FlowOptions
	}{
		{`{}`, domain.FlowOptions{Depth: 6, Limit: 80}},
		{`{"symbol_id":"` + symbolID + `","depth":12,"limit":100}`, domain.FlowOptions{SymbolID: symbolID, Depth: 12, Limit: 100}},
		{`{"feature_id":"` + symbolID + `","depth":1,"limit":1}`, domain.FlowOptions{FeatureID: symbolID, Depth: 1, Limit: 1}},
	}
	for _, tc := range cases {
		handler, _, service := intelligenceFixture()
		body := `{"question":"Explain this static flow","flow":` + tc.body + `}`
		response := authorizedRequest(handler, http.MethodPost, "/api/snapshots/"+snapshotID+"/answers", body)
		if response.Code != http.StatusOK || service.snapshot != snapshotID || service.request.Flow == nil {
			t.Fatal("explicit flow answer lost its snapshot or selector")
		}
		if *service.request.Flow != tc.options || service.request.SymbolID != "" || service.request.FeatureID != "" {
			t.Fatal("flow options were mixed with a top-level target")
		}
	}
}

func TestFlowStreamPreflightChecksNestedTargetsWithoutGraphRead(t *testing.T) {
	for _, selection := range []string{"symbol_id", "feature_id"} {
		catalog, knowledge, service := &catalogStub{err: domain.ErrNotFound}, &knowledgeStub{err: domain.ErrNotFound}, &intelligenceStub{}
		cfg := testConfig()
		cfg.Explorer = &Explorer{Catalog: catalog, Tracker: &trackerStub{}, Knowledge: knowledge, Intelligence: service, RepositoryID: "repo", Token: testToken}
		body := `{"question":"Explain","flow":{"` + selection + `":"` + symbolID + `"}}`
		response := authorizedRequest(NewHandler(nil, cfg), http.MethodPost, "/api/snapshots/"+snapshotID+"/answers/stream", body)
		if response.Code != http.StatusNotFound || service.method != "" || strings.Contains(response.Body.String(), "event:") {
			t.Fatal("missing nested target committed stream headers or invoked inference")
		}
		assertNestedFlowPreflight(t, selection, catalog, knowledge)
	}
}

func assertNestedFlowPreflight(t *testing.T, selection string, catalog *catalogStub, knowledge *knowledgeStub) {
	t.Helper()
	if selection == "symbol_id" {
		if catalog.calls != 1 || catalog.method != "symbol" || catalog.snapshot != snapshotID || catalog.id != symbolID {
			t.Fatal("symbol flow preflight was not a single scoped symbol read")
		}
		return
	}
	if catalog.calls != 0 || knowledge.method != "feature" || knowledge.snapshot != snapshotID || knowledge.id != symbolID {
		t.Fatal("feature flow preflight read an unrelated graph or snapshot")
	}
}

func TestFlowAnswerServiceMissPreservesJSONNotFound(t *testing.T) {
	handler, _, service := intelligenceFixture()
	service.err = domain.ErrNotFound
	response := authorizedRequest(handler, http.MethodPost, "/api/snapshots/"+snapshotID+"/answers", `{"question":"Explain","flow":{}}`)
	if response.Code != http.StatusNotFound {
		t.Fatalf("flow scope miss returned %d", response.Code)
	}
}
