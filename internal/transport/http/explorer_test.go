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

const testToken = "test-operator-secret-token-0123456789"

var snapshotID = strings.Repeat("a", 64)
var symbolID = strings.Repeat("b", 64)

type catalogStub struct {
	calls    int
	method   string
	repo     string
	snapshot string
	id       string
	filter   domain.Filter
	err      error
}

func (s *catalogStub) record(method, repo, snapshot, id string, filter domain.Filter) {
	s.calls++
	s.method, s.repo, s.snapshot, s.id, s.filter = method, repo, snapshot, id, filter
}

func (s *catalogStub) Latest(_ context.Context, repo string) (domain.Snapshot, error) {
	s.record("latest", repo, "", "", domain.Filter{})
	return domain.Snapshot{}, s.err
}

func (s *catalogStub) Files(_ context.Context, repo, snapshot string, filter domain.Filter) (domain.FilePage, error) {
	s.record("files", repo, snapshot, "", filter)
	return domain.FilePage{Items: []domain.FileSummary{}, Limit: filter.Limit, Offset: filter.Offset}, s.err
}

func (s *catalogStub) File(_ context.Context, repo, snapshot, path string) (domain.FileDetail, error) {
	s.record("file", repo, snapshot, path, domain.Filter{})
	return domain.FileDetail{FileSummary: domain.FileSummary{Path: path}}, s.err
}

func (s *catalogStub) Symbols(_ context.Context, repo, snapshot string, filter domain.Filter) (domain.SymbolPage, error) {
	s.record("symbols", repo, snapshot, "", filter)
	return domain.SymbolPage{Items: []domain.SymbolDetail{}, Limit: filter.Limit}, s.err
}

func (s *catalogStub) Symbol(_ context.Context, repo, snapshot, symbol string) (domain.SymbolDetail, error) {
	s.record("symbol", repo, snapshot, symbol, domain.Filter{})
	return domain.SymbolDetail{}, s.err
}

func (s *catalogStub) Details(_ context.Context, repo, snapshot string) (domain.SnapshotDetails, error) {
	s.record("details", repo, snapshot, "", domain.Filter{})
	return domain.SnapshotDetails{Snapshot: domain.Snapshot{ID: snapshot}}, s.err
}

type trackerStub struct {
	projects int
	scans    int
	err      error
}

func (s *trackerStub) Project(context.Context) (domain.Project, error) {
	s.projects++
	return domain.Project{Configured: true, Repository: domain.Repository{ID: "configured-repo", Name: "Example"}}, s.err
}

func (s *trackerStub) RequestScan(context.Context) (domain.Execution, error) {
	s.scans++
	return domain.Execution{ID: "execution-id", Actor: "operator", Status: "queued"}, s.err
}

func explorerFixture() (http.Handler, *catalogStub, *trackerStub) {
	catalog, tracker := &catalogStub{}, &trackerStub{}
	cfg := testConfig()
	cfg.Explorer = &Explorer{Catalog: catalog, Tracker: tracker, RepositoryID: "configured-repo", Token: testToken}
	return NewHandler(nil, cfg), catalog, tracker
}

func authorizedRequest(handler http.Handler, method, target, body string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(method, target, strings.NewReader(body))
	request.Header.Set("Authorization", "Bearer "+testToken)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}

func TestExplorerInfoExposesOnlyConfigurationFlags(t *testing.T) {
	handler, _, tracker := explorerFixture()
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/info", nil))
	var info map[string]bool
	if err := json.Unmarshal(response.Body.Bytes(), &info); err != nil {
		t.Fatal(err)
	}
	if response.Code != http.StatusOK || len(info) != 2 || !info["configured"] || !info["authentication_required"] {
		t.Fatalf("unexpected public info: %d %s", response.Code, response.Body)
	}
	if tracker.projects != 0 {
		t.Fatal("public info must not access repository data")
	}
}

func TestExplorerNotConfigured(t *testing.T) {
	handler := NewHandler(nil, testConfig())
	response := authorizedRequest(handler, http.MethodGet, "/api/project", "")
	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("unconfigured API status: %d", response.Code)
	}
	info := authorizedRequest(handler, http.MethodGet, "/api/info", "")
	if info.Code != http.StatusOK || !strings.Contains(info.Body.String(), `"configured":false`) {
		t.Fatalf("unconfigured info: %d %s", info.Code, info.Body)
	}
}

func TestExplorerAuthentication(t *testing.T) {
	cases := []string{"", "Basic " + testToken, "Bearer wrong-token", "Bearer " + testToken + " "}
	for _, header := range cases {
		handler, _, tracker := explorerFixture()
		request := httptest.NewRequest(http.MethodGet, "/api/project", nil)
		request.Header.Set("Authorization", header)
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Code != http.StatusUnauthorized || tracker.projects != 0 {
			t.Fatalf("bad authorization reached project: %d", response.Code)
		}
		if response.Header().Get("WWW-Authenticate") != "Bearer" {
			t.Fatal("missing bearer authentication challenge")
		}
	}
}

func TestExplorerRejectsDuplicateAuthorizationAndShortConfiguredToken(t *testing.T) {
	handler, _, tracker := explorerFixture()
	request := httptest.NewRequest(http.MethodGet, "/api/project", nil)
	request.Header.Add("Authorization", "Bearer "+testToken)
	request.Header.Add("Authorization", "Bearer "+testToken)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusUnauthorized || tracker.projects != 0 {
		t.Fatal("duplicate Authorization accepted")
	}
	cfg := testConfig()
	cfg.Explorer = &Explorer{Catalog: &catalogStub{}, Tracker: tracker, RepositoryID: "repo", Token: "short"}
	response = authorizedRequest(NewHandler(nil, cfg), http.MethodGet, "/api/project", "")
	if response.Code != http.StatusServiceUnavailable {
		t.Fatal("short configured token must fail closed")
	}
}

func TestExplorerReadsStayRepositoryAndSnapshotScoped(t *testing.T) {
	cases := []struct{ route, method, id string }{
		{"/files", "files", ""}, {"/file?path=src/sample.go", "file", "src/sample.go"},
		{"/symbols", "symbols", ""}, {"/symbols/" + symbolID, "symbol", symbolID},
		{"/details", "details", ""},
	}
	for _, tc := range cases {
		t.Run(tc.method, func(t *testing.T) {
			handler, catalog, _ := explorerFixture()
			response := authorizedRequest(handler, http.MethodGet, "/api/snapshots/"+snapshotID+tc.route, "")
			if response.Code != http.StatusOK || catalog.calls != 1 {
				t.Fatalf("catalog call failed: %d %s", response.Code, response.Body)
			}
			if catalog.repo != "configured-repo" || catalog.snapshot != snapshotID || catalog.id != tc.id || catalog.method != tc.method {
				t.Fatalf("reader scope mismatch: %+v", catalog)
			}
		})
	}
}

func TestExplorerForwardsBoundedFiltersAndDefaults(t *testing.T) {
	handler, catalog, _ := explorerFixture()
	response := authorizedRequest(handler, http.MethodGet, "/api/snapshots/"+snapshotID+"/symbols?file=src/a.go&q=Load&kind=function&limit=12&offset=24", "")
	if response.Code != http.StatusOK || catalog.filter != (domain.Filter{File: "src/a.go", Query: "Load", Kind: "function", Limit: 12, Offset: 24}) {
		t.Fatalf("filter not forwarded intact: %+v", catalog.filter)
	}
	authorizedRequest(handler, http.MethodGet, "/api/snapshots/"+snapshotID+"/files", "")
	if catalog.filter.Limit != 24 || catalog.filter.Offset != 0 {
		t.Fatalf("invalid file defaults: %+v", catalog.filter)
	}
	authorizedRequest(handler, http.MethodGet, "/api/snapshots/"+snapshotID+"/symbols", "")
	if catalog.filter.Limit != 50 {
		t.Fatalf("invalid symbol defaults: %+v", catalog.filter)
	}
}

func TestExplorerErrorsAreMappedWithoutDetails(t *testing.T) {
	cases := []struct {
		err    error
		status int
	}{
		{domain.ErrNotFound, http.StatusNotFound}, {domain.ErrBusy, http.StatusConflict},
		{domain.ErrNotConfigured, http.StatusServiceUnavailable}, {errors.New("secret-query-password"), http.StatusServiceUnavailable},
	}
	for _, tc := range cases {
		handler, catalog, _ := explorerFixture()
		catalog.err = tc.err
		response := authorizedRequest(handler, http.MethodGet, "/api/snapshots/"+snapshotID+"/details", "")
		if response.Code != tc.status || strings.Contains(response.Body.String(), "secret") {
			t.Fatalf("unsafe error response: %d %s", response.Code, response.Body)
		}
	}
}

func TestExplorerUIRoutesCannotInterceptAPI(t *testing.T) {
	cfg := testConfig()
	calls := 0
	cfg.UI = http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { calls++; _, _ = w.Write([]byte("UI")) })
	cfg.Explorer = &Explorer{Catalog: &catalogStub{}, Tracker: &trackerStub{}, RepositoryID: "repo", Token: testToken}
	handler := NewHandler(nil, cfg)
	paths := []string{"/", "/assets/app.js", "/blocks?file=main.go", "/flow?root=example", "/features?snapshot=example", "/changes", "/analysis", "/chain"}
	for _, target := range paths {
		if response := authorizedRequest(handler, http.MethodGet, target, ""); response.Code != http.StatusOK {
			t.Fatalf("UI route failed: %s", target)
		}
	}
	response := authorizedRequest(handler, http.MethodGet, "/api/missing", "")
	if response.Code != http.StatusNotFound || calls != len(paths) {
		t.Fatalf("UI intercepted unknown API route: %d %d", response.Code, calls)
	}
}
