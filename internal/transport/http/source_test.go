package httptransport

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"developa/internal/domain"
)

type sourceCatalogStub struct {
	catalogStub
	options domain.SourceOptions
}

func (s *sourceCatalogStub) Source(_ context.Context, repo, snapshot, symbol string, options domain.SourceOptions) (domain.SymbolSource, error) {
	s.record("source", repo, snapshot, symbol, domain.Filter{})
	s.options = options
	return domain.SymbolSource{SnapshotID: snapshot, SymbolID: symbol, Path: "physical.go", Source: "func Run() {}", Complete: true, Limitations: []string{}}, s.err
}

func sourceExplorerFixture() (http.Handler, *sourceCatalogStub) {
	catalog := &sourceCatalogStub{}
	cfg := testConfig()
	cfg.Explorer = &Explorer{Catalog: catalog, Tracker: &trackerStub{}, RepositoryID: "configured-repo", Token: testToken}
	return NewHandler(nil, cfg), catalog
}

func TestSourceRouteScopesAndOptions(t *testing.T) {
	handler, catalog := sourceExplorerFixture()
	path := "/api/snapshots/" + snapshotID + "/symbols/" + symbolID + "/source"
	response := authorizedRequest(handler, http.MethodGet, path+"?offset=12&limit=64", "")
	if response.Code != http.StatusOK || catalog.options != (domain.SourceOptions{Offset: 12, Limit: 64}) {
		t.Fatalf("source options not forwarded: %+v %s", catalog.options, response.Body)
	}
	if catalog.repo != "configured-repo" || catalog.snapshot != snapshotID || catalog.id != symbolID || catalog.calls != 1 {
		t.Fatal("source route lost repository/snapshot/symbol scope")
	}
	var chunk domain.SymbolSource
	if err := json.Unmarshal(response.Body.Bytes(), &chunk); err != nil {
		t.Fatal(err)
	}
	if !chunk.Complete || chunk.Source != "func Run() {}" {
		t.Fatal("source wire response was lost")
	}
	assertSourceRouteDefaults(t, handler, catalog, path)
}

func assertSourceRouteDefaults(t *testing.T, handler http.Handler, catalog *sourceCatalogStub, path string) {
	t.Helper()
	response := authorizedRequest(handler, http.MethodGet, path, "")
	if response.Code != http.StatusOK || catalog.options != (domain.SourceOptions{Limit: 8192}) {
		t.Fatalf("source defaults incorrect: %+v %s", catalog.options, response.Body)
	}
}

func TestSourceRouteRejectsInvalidOptionsBeforeReading(t *testing.T) {
	queries := []string{"offset=-1", "offset=word", "offset=999999999999999999999", "limit=0", "limit=3", "limit=16385", "limit=", "limit=4&limit=8", "offset=0&offset=1", "unknown=1", "offset=%xx", "limit=4;offset=0"}
	for _, query := range queries {
		handler, catalog := sourceExplorerFixture()
		response := authorizedRequest(handler, http.MethodGet, "/api/snapshots/"+snapshotID+"/symbols/"+symbolID+"/source?"+query, "")
		if response.Code != http.StatusBadRequest || catalog.calls != 0 {
			t.Fatalf("invalid source query %q reached persistence: %d", query, response.Code)
		}
	}
}

func TestSourceRouteRejectsInvalidIdentityAndUnauthorizedReads(t *testing.T) {
	handler, catalog := sourceExplorerFixture()
	for _, path := range []string{"/api/snapshots/bad/symbols/" + symbolID + "/source", "/api/snapshots/" + snapshotID + "/symbols/bad/source"} {
		response := authorizedRequest(handler, http.MethodGet, path, "")
		if response.Code != http.StatusBadRequest || catalog.calls != 0 {
			t.Fatal("invalid source identity reached persistence")
		}
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/snapshots/"+snapshotID+"/symbols/"+symbolID+"/source", nil))
	if response.Code != http.StatusUnauthorized || catalog.calls != 0 {
		t.Fatal("unauthenticated source read reached persistence")
	}
}

func TestSourceRouteErrorsAndUnavailableReader(t *testing.T) {
	path := "/api/snapshots/" + snapshotID + "/symbols/" + symbolID + "/source"
	for _, tc := range []struct {
		err    error
		status int
	}{{domain.ErrInvalidInput, 400}, {domain.ErrNotFound, 404}, {domain.ErrSourceUnavailable, 409}, {context.DeadlineExceeded, 504}} {
		handler, catalog := sourceExplorerFixture()
		catalog.err = tc.err
		response := authorizedRequest(handler, http.MethodGet, path, "")
		if response.Code != tc.status {
			t.Fatalf("source error %v returned %d", tc.err, response.Code)
		}
	}
	handler, _, _ := explorerFixture()
	if response := authorizedRequest(handler, http.MethodGet, path, ""); response.Code != http.StatusConflict {
		t.Fatalf("missing source capability did not report unavailability: %d", response.Code)
	}
}
