package httptransport

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"developa/internal/domain"
)

type implementationKnowledge struct {
	*knowledgeStub
	options domain.ImplementationOptions
}

func (s *implementationKnowledge) Implementations(_ context.Context, repo, snapshot, symbol string, options domain.ImplementationOptions) (domain.ImplementationPage, error) {
	s.record("implementations", repo, snapshot)
	s.id, s.options = symbol, options
	return domain.ImplementationPage{RepositoryID: repo, SnapshotID: snapshot, SymbolID: symbol, Limit: options.Limit, Offset: options.Offset}, s.err
}

func implementationHandler() (http.Handler, *implementationKnowledge) {
	knowledge := &implementationKnowledge{knowledgeStub: &knowledgeStub{}}
	cfg := testConfig()
	cfg.Explorer = &Explorer{Catalog: &catalogStub{}, Tracker: &trackerStub{}, RepositoryID: "configured-repo", Token: testToken, Knowledge: knowledge}
	return NewHandler(nil, cfg), knowledge
}

func implementationRoute() string {
	return "/api/snapshots/" + snapshotID + "/symbols/" + symbolID + "/implementations"
}

func TestImplementationEndpointPinsRepositorySnapshotAndOptions(t *testing.T) {
	handler, knowledge := implementationHandler()
	response := authorizedRequest(handler, http.MethodGet, implementationRoute(), "")
	if response.Code != http.StatusOK || knowledge.options != (domain.ImplementationOptions{Limit: 20}) {
		t.Fatal("implementation defaults did not reach storage")
	}
	assertImplementationResponseScope(t, response)
	path := "/api/repositories/configured-repo/snapshots/" + snapshotID + "/symbols/" + symbolID + "/implementations?limit=100&offset=100000"
	response = authorizedRequest(handler, http.MethodGet, path, "")
	if response.Code != http.StatusOK || knowledge.options != (domain.ImplementationOptions{Limit: 100, Offset: 100000}) {
		t.Fatal("explicit implementation bounds did not reach storage")
	}
	if knowledge.repo != "configured-repo" || knowledge.snapshot != snapshotID || knowledge.id != symbolID {
		t.Fatal("repository-qualified implementation request lost scope")
	}
}

func assertImplementationResponseScope(t *testing.T, response *httptest.ResponseRecorder) {
	t.Helper()
	var page domain.ImplementationPage
	if err := json.Unmarshal(response.Body.Bytes(), &page); err != nil {
		t.Fatal(err)
	}
	if page.RepositoryID != "configured-repo" || page.SnapshotID != snapshotID || page.SymbolID != symbolID {
		t.Fatalf("response lost implementation scope: %+v", page)
	}
}

func TestImplementationEndpointRequiresAuthentication(t *testing.T) {
	handler, knowledge := implementationHandler()
	request := httptest.NewRequest(http.MethodGet, implementationRoute()+"?token="+testToken, nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusUnauthorized || knowledge.method != "" {
		t.Fatal("anonymous or query-token implementation request reached storage")
	}
}

func TestImplementationEndpointRejectsInvalidQueriesBeforeStorage(t *testing.T) {
	queries := []string{"limit=0", "limit=101", "limit=-1", "limit=1.5", "offset=-1", "offset=100001", "limit=", "offset=", "limit=1&limit=2", "offset=0&offset=1", "repo=other", "offset=%GG", "limit=1;offset=0"}
	for _, query := range queries {
		handler, knowledge := implementationHandler()
		response := authorizedRequest(handler, http.MethodGet, implementationRoute()+"?"+query, "")
		if response.Code != http.StatusBadRequest || knowledge.method != "" {
			t.Fatalf("invalid query reached implementation reader: %q %d", query, response.Code)
		}
	}
}

func TestImplementationEndpointRejectsMalformedIDsBeforeStorage(t *testing.T) {
	paths := []string{
		"/api/snapshots/invalid/symbols/" + symbolID + "/implementations",
		"/api/snapshots/" + snapshotID + "/symbols/invalid/implementations",
		"/api/snapshots/" + snapshotID + "/symbols/" + strings.Repeat("B", 64) + "/implementations",
	}
	for _, path := range paths {
		handler, knowledge := implementationHandler()
		response := authorizedRequest(handler, http.MethodGet, path, "")
		if response.Code != http.StatusBadRequest || knowledge.method != "" {
			t.Fatalf("invalid ID reached implementation reader: %s %d", path, response.Code)
		}
	}
}

func TestImplementationEndpointPreservesNotFoundAndSanitizesErrors(t *testing.T) {
	handler, knowledge := implementationHandler()
	knowledge.err = domain.ErrNotFound
	response := authorizedRequest(handler, http.MethodGet, implementationRoute(), "")
	if response.Code != http.StatusNotFound {
		t.Fatal("missing implementation target did not preserve scoped 404")
	}
	knowledge.err = errors.New("private SQL source credential")
	response = authorizedRequest(handler, http.MethodGet, implementationRoute(), "")
	if response.Code != http.StatusServiceUnavailable || strings.Contains(response.Body.String(), "private SQL") {
		t.Fatal("implementation persistence error was not sanitized")
	}
}

func TestImplementationEndpointMissingReaderOrRepository(t *testing.T) {
	handler, _, _ := intelligenceFixture()
	response := authorizedRequest(handler, http.MethodGet, implementationRoute(), "")
	if response.Code != http.StatusServiceUnavailable {
		t.Fatal("missing optional implementation reader did not return unavailable")
	}
	handler, knowledge := implementationHandler()
	path := "/api/repositories/other/snapshots/" + snapshotID + "/symbols/" + symbolID + "/implementations"
	response = authorizedRequest(handler, http.MethodGet, path, "")
	if response.Code != http.StatusNotFound || knowledge.method != "" {
		t.Fatal("unconfigured repository reached implementation storage")
	}
}
