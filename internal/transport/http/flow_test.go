package httptransport

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"developa/internal/domain"
)

type flowKnowledge struct {
	*knowledgeStub
	options domain.FlowOptions
}

func (s *flowKnowledge) Flow(_ context.Context, repo, snapshot string, options domain.FlowOptions) (domain.CodeFlow, error) {
	s.record("flow", repo, snapshot)
	s.options = options
	return domain.CodeFlow{SnapshotID: snapshot, Options: options}, s.err
}

func flowHandler() (http.Handler, *flowKnowledge) {
	knowledge := &flowKnowledge{knowledgeStub: &knowledgeStub{}}
	cfg := testConfig()
	cfg.Explorer = &Explorer{Catalog: &catalogStub{}, Tracker: &trackerStub{}, RepositoryID: "configured-repo", Token: testToken, Knowledge: knowledge}
	return NewHandler(nil, cfg), knowledge
}

func TestFlowEndpointPinsRepositorySnapshotAndOptions(t *testing.T) {
	handler, knowledge := flowHandler()
	response := authorizedRequest(handler, http.MethodGet, "/api/snapshots/"+snapshotID+"/flow?symbol_id="+symbolID+"&depth=12&limit=100", "")
	if response.Code != http.StatusOK || knowledge.repo != "configured-repo" || knowledge.snapshot != snapshotID {
		t.Fatalf("flow scope lost: %d %+v", response.Code, knowledge)
	}
	if knowledge.options != (domain.FlowOptions{SymbolID: symbolID, Depth: 12, Limit: 100}) {
		t.Fatal("explicit flow options did not reach the reader")
	}
	response = authorizedRequest(handler, http.MethodGet, "/api/snapshots/"+snapshotID+"/flow", "")
	if response.Code != http.StatusOK || knowledge.options != (domain.FlowOptions{Depth: 6, Limit: 80}) {
		t.Fatal("application flow defaults did not reach the reader")
	}
}

func TestFlowEndpointRequiresAuthentication(t *testing.T) {
	handler, knowledge := flowHandler()
	request := httptest.NewRequest(http.MethodGet, "/api/snapshots/"+snapshotID+"/flow?token="+testToken, nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusUnauthorized || knowledge.method != "" {
		t.Fatal("anonymous or query-token flow request reached storage")
	}
}

func TestFlowEndpointStrictlyRejectsAmbiguousUnknownAndUnboundedQueries(t *testing.T) {
	queries := []string{"symbol_id=invalid", "feature_id=" + strings.Repeat("A", 64), "symbol_id=" + symbolID + "&feature_id=" + symbolID,
		"depth=0", "depth=13", "depth=-1", "limit=101", "limit=0", "depth=1&depth=2", "limit=", "repo=other", "limit=1.5", "depth=%GG", "depth=1;limit=2"}
	for _, query := range queries {
		handler, knowledge := flowHandler()
		response := authorizedRequest(handler, http.MethodGet, "/api/snapshots/"+snapshotID+"/flow?"+query, "")
		if response.Code != http.StatusBadRequest || knowledge.method != "" {
			t.Fatalf("invalid query reached flow reader: %q %d", query, response.Code)
		}
	}
}

func TestFlowEndpointPreservesNotFoundAndSanitizesErrors(t *testing.T) {
	handler, knowledge := flowHandler()
	knowledge.err = domain.ErrNotFound
	response := authorizedRequest(handler, http.MethodGet, "/api/snapshots/"+snapshotID+"/flow?feature_id="+symbolID, "")
	if response.Code != http.StatusNotFound || knowledge.options.FeatureID != symbolID {
		t.Fatal("missing feature flow did not preserve scoped 404")
	}
	knowledge.err = errors.New("private SQL source credential")
	response = authorizedRequest(handler, http.MethodGet, "/api/snapshots/"+snapshotID+"/flow", "")
	if response.Code != http.StatusServiceUnavailable || strings.Contains(response.Body.String(), "private SQL") {
		t.Fatal("flow persistence error was not sanitized")
	}
}

func TestFlowEndpointMissingReaderOrMalformedSnapshot(t *testing.T) {
	handler, _, _ := intelligenceFixture()
	response := authorizedRequest(handler, http.MethodGet, "/api/snapshots/"+snapshotID+"/flow", "")
	if response.Code != http.StatusServiceUnavailable {
		t.Fatal("missing optional flow reader did not return unavailable")
	}
	handler, knowledge := flowHandler()
	response = authorizedRequest(handler, http.MethodGet, "/api/snapshots/invalid/flow", "")
	if response.Code != http.StatusBadRequest || knowledge.method != "" {
		t.Fatal("malformed snapshot reached flow storage")
	}
}
