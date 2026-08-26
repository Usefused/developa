package httptransport

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"developa/internal/domain"
)

type repositoryReaderStub struct {
	ids    []string
	filter domain.Filter
	calls  int
	err    error
}

type repositoryKnowledge struct {
	*flowKnowledge
	domain.ReviewStore
}

func (s *repositoryKnowledge) ReviewPage(_ context.Context, repo, snapshot string, options domain.ReviewOptions) (domain.ReviewPage, error) {
	s.record("reviews", repo, snapshot)
	return domain.ReviewPage{SnapshotID: snapshot, Options: options}, s.err
}

func (s *repositoryReaderStub) Repositories(_ context.Context, ids []string, filter domain.Filter) (domain.RepositoryPage, error) {
	s.ids, s.filter = append([]string{}, ids...), filter
	s.calls++
	return domain.RepositoryPage{Items: []domain.RepositorySummary{}, Limit: filter.Limit, Offset: filter.Offset}, s.err
}

func repositoryFixture() (Config, *repositoryReaderStub) {
	reader := &repositoryReaderStub{}
	cfg := testConfig()
	cfg.RepositoryCatalog = reader
	for _, digit := range []string{"1", "2"} {
		cfg.Explorers = append(cfg.Explorers, &Explorer{RepositoryID: strings.Repeat(digit, 64),
			Token: testToken, Catalog: &catalogStub{}, Tracker: &trackerStub{}, Knowledge: &repositoryKnowledge{flowKnowledge: &flowKnowledge{knowledgeStub: &knowledgeStub{}}}, Intelligence: &intelligenceStub{}, Reviewer: &reviewerStub{},
			Jobs: &analysisQueueStub{available: true, job: domain.AnalysisJob{Status: "queued"}}})
	}
	return cfg, reader
}

func TestRepositoriesListSuppliesOnlyConfiguredIDsAndDefault(t *testing.T) {
	cfg, reader := repositoryFixture()
	cfg.Explorer = &Explorer{RepositoryID: "ignored-fallback"}
	cfg.Explorers = append(cfg.Explorers, cfg.Explorers[1], nil)
	response := authorizedRequest(NewHandler(nil, cfg), http.MethodGet, "/api/repositories?q=Payments&limit=3&offset=2", "")
	var page domain.RepositoryPage
	if err := json.Unmarshal(response.Body.Bytes(), &page); err != nil {
		t.Fatal(err)
	}
	if response.Code != 200 || page.DefaultRepositoryID != cfg.Explorers[0].RepositoryID || reader.calls != 1 {
		t.Fatalf("listing response lost configured default: %d %s", response.Code, response.Body)
	}
	if !reflect.DeepEqual(reader.ids, []string{cfg.Explorers[0].RepositoryID, cfg.Explorers[1].RepositoryID}) || reader.filter != (domain.Filter{Query: "Payments", Limit: 3, Offset: 2}) {
		t.Fatalf("listing scope/filter mismatch: %+v", reader)
	}
}

func TestRepositoriesListingRejectsInvalidFiltersBeforeStore(t *testing.T) {
	queries := []string{"limit=101", "offset=-1", "offset=100001", "kind=function", "file=main.go", "q=a&q=b", "limit=1&limit=2", "q=%zz", "q=a;b", "q=" + strings.Repeat("x", 201), "q=%00"}
	for _, query := range queries {
		cfg, reader := repositoryFixture()
		response := authorizedRequest(NewHandler(nil, cfg), http.MethodGet, "/api/repositories?"+query, "")
		if response.Code != 400 || reader.calls != 0 {
			t.Fatalf("invalid listing filter reached storage: %q %d", query, response.Code)
		}
	}
}

func TestRepositoriesListingErrorsAreSanitized(t *testing.T) {
	cfg, reader := repositoryFixture()
	reader.err = errors.New("secret database password")
	response := authorizedRequest(NewHandler(nil, cfg), http.MethodGet, "/api/repositories", "")
	if response.Code != 503 || strings.Contains(response.Body.String(), "secret") {
		t.Fatal("repository listing exposed database diagnostics")
	}
	cfg.RepositoryCatalog = nil
	if response := authorizedRequest(NewHandler(nil, cfg), http.MethodGet, "/api/repositories", ""); response.Code != 503 {
		t.Fatal("missing repository catalog must fail closed")
	}
}

func TestScopedRepositoriesAuthenticateBeforeResolution(t *testing.T) {
	cfg, reader := repositoryFixture()
	handler := NewHandler(nil, cfg)
	paths := []string{"/api/repositories", "/api/repositories/unknown/project", "/api/repositories/" + cfg.Explorers[1].RepositoryID + "/project"}
	for _, path := range paths {
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, path, nil))
		if response.Code != 401 {
			t.Fatalf("repository scope disclosed before authentication: %d", response.Code)
		}
	}
	response := authorizedRequest(handler, http.MethodGet, "/api/repositories/unknown/project", "")
	if response.Code != 404 || reader.calls != 0 {
		t.Fatal("unknown repository did not fail before storage access")
	}
}

func TestScopedRepositoriesBindCatalogAndPreserveDefaultAlias(t *testing.T) {
	cfg, _ := repositoryFixture()
	first, second := cfg.Explorers[0].Catalog.(*catalogStub), cfg.Explorers[1].Catalog.(*catalogStub)
	handler := NewHandler(nil, cfg)
	routes := []string{"/files", "/file?path=main.go", "/symbols", "/symbols/" + symbolID, "/details"}
	for _, route := range routes {
		response := authorizedRequest(handler, http.MethodGet, "/api/repositories/"+cfg.Explorers[1].RepositoryID+"/snapshots/"+snapshotID+route, "")
		if response.Code != 200 || second.repo != cfg.Explorers[1].RepositoryID || second.snapshot != snapshotID || first.calls != 0 {
			t.Fatalf("scoped catalog routing failed: %s %d %s", route, response.Code, response.Body)
		}
	}
	response := authorizedRequest(handler, http.MethodGet, "/api/snapshots/"+snapshotID+"/files", "")
	if response.Code != 200 || first.repo != cfg.Explorers[0].RepositoryID || first.calls != 1 {
		t.Fatal("default alias changed scope")
	}
}

func TestScopedKnowledgeUsesOnlySelectedRepository(t *testing.T) {
	cfg, _ := repositoryFixture()
	first, second := cfg.Explorers[0].Knowledge.(*repositoryKnowledge), cfg.Explorers[1].Knowledge.(*repositoryKnowledge)
	handler := NewHandler(nil, cfg)
	routes := []string{"/calls", "/flow", "/context", "/features", "/features/" + symbolID, "/symbols/" + symbolID + "/chain", "/function-reviews"}
	for _, route := range routes {
		response := authorizedRequest(handler, http.MethodGet, "/api/repositories/"+cfg.Explorers[1].RepositoryID+"/snapshots/"+snapshotID+route, "")
		if response.Code != 200 || second.repo != cfg.Explorers[1].RepositoryID || second.snapshot != snapshotID || first.method != "" {
			t.Fatalf("scoped intelligence routing failed: %s %d %s", route, response.Code, response.Body)
		}
	}
}

func TestScopedMutationsChooseConfiguredServices(t *testing.T) {
	cases := []struct {
		path, body string
		status     int
	}{
		{"/scan", "{}", 202},
		{"/snapshots/" + snapshotID + "/features/generate", "{}", 202},
		{"/snapshots/" + snapshotID + "/answers", `{"question":"why"}`, 200},
		{"/snapshots/" + snapshotID + "/answers/stream", `{"question":"why"}`, 200},
		{"/snapshots/" + snapshotID + "/function-reviews", "{}", 200},
		{"/snapshots/" + snapshotID + "/function-reviews/stream", "{}", 200},
	}
	for _, test := range cases {
		cfg, _ := repositoryFixture()
		response := authorizedRequest(NewHandler(nil, cfg), http.MethodPost, "/api/repositories/"+cfg.Explorers[1].RepositoryID+test.path, test.body)
		if response.Code != test.status {
			t.Fatalf("scoped mutation %s returned %d: %s", test.path, response.Code, response.Body)
		}
		assertDefaultServicesUntouched(t, cfg.Explorers[0])
		assertSelectedServiceInvoked(t, cfg.Explorers[1], test.path)
	}
}

func assertSelectedServiceInvoked(t *testing.T, explorer *Explorer, path string) {
	t.Helper()
	called := false
	switch {
	case path == "/scan":
		called = explorer.Tracker.(*trackerStub).scans == 1
	case strings.Contains(path, "/features/generate"):
		called = explorer.Jobs.(*analysisQueueStub).method == "queue"
	case strings.Contains(path, "/answers"):
		called = explorer.Intelligence.(*intelligenceStub).method == "answer"
	case strings.Contains(path, "/function-reviews"):
		called = explorer.Reviewer.(*reviewerStub).calls == 1
	}
	if !called {
		t.Fatalf("selected repository service was not invoked: %s", path)
	}
}

func assertDefaultServicesUntouched(t *testing.T, explorer *Explorer) {
	t.Helper()
	if explorer.Tracker.(*trackerStub).scans != 0 || explorer.Intelligence.(*intelligenceStub).method != "" || explorer.Reviewer.(*reviewerStub).calls != 0 || explorer.Jobs.(*analysisQueueStub).method != "" {
		t.Fatal("repository-scoped mutation reached a default-repository service")
	}
}

func TestScopedRepositoryUsesOnlyRootTokenAndTraceScope(t *testing.T) {
	exporter := installTraceProvider(t)
	cfg, _ := repositoryFixture()
	cfg.Explorers[1].Token = "a-different-repository-secret-token"
	handler := NewHandler(nil, cfg)
	path := "/api/repositories/" + cfg.Explorers[1].RepositoryID + "/capabilities?prompt=secret"
	response := authorizedRequest(handler, http.MethodGet, path, "")
	if response.Code != 200 {
		t.Fatal("selected repository replaced the root credential")
	}
	spans := exporter.GetSpans()
	if len(spans) != 1 || spans[0].Name != "HTTP /api/repositories/{repository}/capabilities" {
		t.Fatalf("scoped HTTP trace did not retain route template: %+v", spans)
	}
	assertSafeRequestSpan(t, spans[0])
	attributes := map[string]string{}
	for _, attr := range spans[0].Attributes {
		attributes[string(attr.Key)] = attr.Value.AsString()
	}
	if attributes["repository.id"] != cfg.Explorers[1].RepositoryID || attributes["actor.type"] != "operator" {
		t.Fatal("scoped HTTP span lost repository/actor identity")
	}
	request := httptest.NewRequest(http.MethodGet, path, nil)
	request.Header.Set("Authorization", "Bearer "+cfg.Explorers[1].Token)
	unauthorized := httptest.NewRecorder()
	handler.ServeHTTP(unauthorized, request)
	if unauthorized.Code != http.StatusUnauthorized {
		t.Fatal("repository-specific credential bypassed root authentication")
	}
}
