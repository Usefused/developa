package httptransport

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"developa/internal/domain"
)

type knowledgeStub struct {
	domain.IntelligenceStore
	method, repo, snapshot, id, query string
	filter                            domain.CallFilter
	chain                             domain.ChainOptions
	err                               error
}

func (s *knowledgeStub) record(method, repo, snapshot string) {
	s.method, s.repo, s.snapshot = method, repo, snapshot
}
func (s *knowledgeStub) Calls(_ context.Context, repo, snapshot string, filter domain.CallFilter) (domain.CallPage, error) {
	s.record("calls", repo, snapshot)
	s.filter = filter
	return domain.CallPage{Limit: filter.Limit, Offset: filter.Offset}, s.err
}
func (s *knowledgeStub) Chain(_ context.Context, repo, snapshot, id string, options domain.ChainOptions) (domain.CallChain, error) {
	s.record("chain", repo, snapshot)
	s.id, s.chain = id, options
	return domain.CallChain{SnapshotID: snapshot, RootID: id, Direction: options.Direction, Depth: options.Depth}, s.err
}
func (s *knowledgeStub) Context(_ context.Context, repo, snapshot, query string, _ int) (domain.ContextPack, error) {
	s.record("context", repo, snapshot)
	s.query = query
	return domain.ContextPack{RepositoryID: repo, SnapshotID: snapshot, Query: query}, s.err
}
func (s *knowledgeStub) Features(_ context.Context, repo, snapshot string, _ domain.Filter) (domain.FeaturePage, error) {
	s.record("features", repo, snapshot)
	return domain.FeaturePage{Items: []domain.Feature{}}, s.err
}
func (s *knowledgeStub) Feature(_ context.Context, repo, snapshot, id string) (domain.Feature, error) {
	s.record("feature", repo, snapshot)
	s.id = id
	return domain.Feature{ID: id}, s.err
}

type intelligenceStub struct {
	method, snapshot string
	request          domain.AnswerRequest
	err              error
	wait             bool
}

func (s *intelligenceStub) SavedAnswer(_ context.Context, snapshot string, request domain.AnswerRequest) (*domain.Answer, error) {
	s.method, s.snapshot, s.request = "saved_answer", snapshot, request
	return nil, s.err
}

func TestSavedAnswerLookupIsReadOnlyValidatedAndAuthenticated(t *testing.T) {
	handler, _, intelligence := intelligenceFixture()
	route := "/api/snapshots/" + snapshotID + "/answers/lookup"
	response := authorizedRequest(handler, http.MethodPost, route, `{"question":"Explain","symbol_id":"`+symbolID+`"}`)
	if response.Code != http.StatusOK || intelligence.method != "saved_answer" || intelligence.snapshot != snapshotID {
		t.Fatal("saved lookup invoked generation or lost snapshot scope")
	}
	if !strings.Contains(response.Body.String(), `"answer":null`) {
		t.Fatal("missing saved explanation must be an explicit null")
	}
	intelligence.method = ""
	for _, body := range []string{`{"question":"Explain","context_key":"caller-key"}`, `{"question":"Explain","symbol_id":"invalid"}`} {
		if response := authorizedRequest(handler, http.MethodPost, route, body); response.Code != http.StatusBadRequest {
			t.Fatal("lookup accepted invalid selectors or caller-chosen context")
		}
	}
	if intelligence.method != "" {
		t.Fatal("invalid lookup reached the service")
	}
	unauthorized := httptest.NewRecorder()
	handler.ServeHTTP(unauthorized, httptest.NewRequest(http.MethodPost, route, strings.NewReader(`{"question":"Explain"}`)))
	if unauthorized.Code != http.StatusUnauthorized {
		t.Fatal("saved explanation escaped authentication")
	}
}

func (*intelligenceStub) Available() bool { return true }
func (s *intelligenceStub) Discover(_ context.Context, snapshot string) (domain.FeatureRun, error) {
	s.method, s.snapshot = "discover", snapshot
	return domain.FeatureRun{SnapshotID: snapshot}, s.err
}
func (s *intelligenceStub) Answer(ctx context.Context, snapshot string, request domain.AnswerRequest) (domain.Answer, error) {
	s.method, s.snapshot, s.request = "answer", snapshot, request
	if s.wait {
		<-ctx.Done()
		return domain.Answer{}, ctx.Err()
	}
	return domain.Answer{SnapshotID: snapshot, Text: "Evidence-backed answer"}, s.err
}

func intelligenceFixture() (http.Handler, *knowledgeStub, *intelligenceStub) {
	knowledge, intelligence := &knowledgeStub{}, &intelligenceStub{}
	cfg := testConfig()
	cfg.Explorer = &Explorer{Catalog: &catalogStub{}, Tracker: &trackerStub{}, RepositoryID: "configured-repo", Token: testToken, Knowledge: knowledge, Intelligence: intelligence}
	return NewHandler(nil, cfg), knowledge, intelligence
}

func TestCapabilitiesDiscloseCloudWithoutCredentials(t *testing.T) {
	cfg := testConfig()
	cfg.Explorer = &Explorer{Catalog: &catalogStub{}, Tracker: &trackerStub{}, RepositoryID: "repo", Token: testToken, Knowledge: &knowledgeStub{}, Intelligence: &intelligenceStub{}, OllamaCloud: true}
	response := authorizedRequest(NewHandler(nil, cfg), http.MethodGet, "/api/capabilities", "")
	var values map[string]bool
	if err := json.Unmarshal(response.Body.Bytes(), &values); err != nil {
		t.Fatal(err)
	}
	if response.Code != http.StatusOK || !values["ollama_cloud"] || !values["ollama_configured"] || len(values) != 11 {
		t.Fatal("capabilities must disclose cloud mode and only capability flags")
	}
}

func TestIntelligenceReadsAreScopedAndBounded(t *testing.T) {
	cases := []struct{ route, method string }{
		{"/calls?direction=in&symbol_id=" + symbolID + "&resolution=resolved&limit=10&offset=20", "calls"},
		{"/symbols/" + symbolID + "/chain?direction=in&depth=3&limit=12", "chain"},
		{"/context?q=load+repository&limit=8", "context"},
		{"/features", "features"}, {"/features/" + symbolID, "feature"},
	}
	for _, tc := range cases {
		handler, knowledge, _ := intelligenceFixture()
		response := authorizedRequest(handler, http.MethodGet, "/api/snapshots/"+snapshotID+tc.route, "")
		if response.Code != http.StatusOK {
			t.Fatalf("%s: %d %s", tc.route, response.Code, response.Body)
		}
		if knowledge.method != tc.method || knowledge.repo != "configured-repo" || knowledge.snapshot != snapshotID {
			t.Fatalf("scope mismatch: %+v", knowledge)
		}
	}
}

func TestCallAndChainFiltersReachStoreIntact(t *testing.T) {
	handler, knowledge, _ := intelligenceFixture()
	authorizedRequest(handler, http.MethodGet, "/api/snapshots/"+snapshotID+"/calls?symbol_id="+symbolID+"&direction=in&resolution=resolved&limit=10&offset=20", "")
	if knowledge.filter != (domain.CallFilter{SymbolID: symbolID, Direction: "in", Resolution: "resolved", Limit: 10, Offset: 20}) {
		t.Fatalf("call filters: %+v", knowledge.filter)
	}
	authorizedRequest(handler, http.MethodGet, "/api/snapshots/"+snapshotID+"/symbols/"+symbolID+"/chain?direction=in&depth=3&limit=12", "")
	if knowledge.chain != (domain.ChainOptions{Direction: "in", Depth: 3, Limit: 12}) {
		t.Fatalf("chain filters: %+v", knowledge.chain)
	}
}

func TestIntelligenceRejectsInvalidReadFiltersBeforeStorage(t *testing.T) {
	routes := []string{"/calls?symbol_id=bad", "/calls?direction=both", "/calls?resolution=guessed", "/calls?limit=101", "/calls?offset=-1", "/symbols/" + symbolID + "/chain?depth=6", "/symbols/bad/chain", "/context?limit=21", "/features?kind=function", "/features/bad"}
	for _, route := range routes {
		handler, knowledge, _ := intelligenceFixture()
		response := authorizedRequest(handler, http.MethodGet, "/api/snapshots/"+snapshotID+route, "")
		if response.Code != http.StatusBadRequest || knowledge.method != "" {
			t.Fatalf("invalid route reached store: %s %d", route, response.Code)
		}
	}
}

func TestAIEndpointsValidateBodiesBeforeCallingService(t *testing.T) {
	bodies := []string{"", "null", "{}", `{"question":" "}`, `{"question":"why","model":"remote"}`, `{"question":"why","symbol_id":"bad"}`, `{"question":"why","feature_id":"bad"}`, `{"question":"why","feature_id":"` + symbolID + `","symbol_id":"` + symbolID + `"}`, `{"question":"why"}{}`, `{"question":"` + strings.Repeat("x", 2001) + `"}`}
	for _, body := range bodies {
		handler, _, intelligence := intelligenceFixture()
		response := authorizedRequest(handler, http.MethodPost, "/api/snapshots/"+snapshotID+"/answers", body)
		if response.Code != http.StatusBadRequest || intelligence.method != "" {
			t.Fatalf("invalid body accepted: %d", response.Code)
		}
	}
}

func TestAnswerAcceptsEscapedUnicodeWithinDecodedLimit(t *testing.T) {
	handler, _, intelligence := intelligenceFixture()
	body := `{"question":"` + strings.Repeat(`\u00e9`, 1000) + `"}`
	response := authorizedRequest(handler, http.MethodPost, "/api/snapshots/"+snapshotID+"/answers", body)
	if response.Code != http.StatusOK || intelligence.request.Question != strings.Repeat("é", 1000) {
		t.Fatal("valid escaped Unicode question was rejected")
	}
}

func TestFeatureAnswerIsSnapshotScopedAndReturnsNotFound(t *testing.T) {
	handler, _, intelligence := intelligenceFixture()
	body := `{"question":"Explain this feature from its code evidence.","feature_id":"` + symbolID + `"}`
	response := authorizedRequest(handler, http.MethodPost, "/api/snapshots/"+snapshotID+"/answers", body)
	if response.Code != http.StatusOK || intelligence.snapshot != snapshotID || intelligence.request.FeatureID != symbolID || intelligence.request.SymbolID != "" {
		t.Fatal("feature answer lost its pinned target")
	}
	intelligence.err = domain.ErrNotFound
	response = authorizedRequest(handler, http.MethodPost, "/api/snapshots/"+snapshotID+"/answers", body)
	if response.Code != http.StatusNotFound {
		t.Fatalf("missing feature returned %d", response.Code)
	}
}

func TestAIMutationsAreAuthenticatedAndSameOrigin(t *testing.T) {
	for _, suffix := range []string{"/answers", "/features/generate"} {
		handler, _, intelligence := intelligenceFixture()
		request := httptest.NewRequest(http.MethodPost, "/api/snapshots/"+snapshotID+suffix, strings.NewReader(`{"question":"why"}`))
		request.Header.Set("Origin", "https://attacker.invalid")
		request.Header.Set("Authorization", "Bearer "+testToken)
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Code != http.StatusForbidden || intelligence.method != "" {
			t.Fatalf("cross-origin call: %d", response.Code)
		}
		request.Header.Del("Authorization")
		response = httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Code != http.StatusUnauthorized {
			t.Fatalf("anonymous call: %d", response.Code)
		}
	}
}

func TestAIMutationsForwardSnapshotAndReturnSafeFailures(t *testing.T) {
	handler, _, intelligence := intelligenceFixture()
	response := authorizedRequest(handler, http.MethodPost, "/api/snapshots/"+snapshotID+"/answers", `{"question":"What does this do?","symbol_id":"`+symbolID+`"}`)
	if response.Code != http.StatusOK || intelligence.snapshot != snapshotID || intelligence.request.SymbolID != symbolID {
		t.Fatalf("answer failed: %d %s", response.Code, response.Body)
	}
	intelligence.err = domain.ErrModelUnavailable
	response = authorizedRequest(handler, http.MethodPost, "/api/snapshots/"+snapshotID+"/answers", `{"question":"why"}`)
	if response.Code != http.StatusServiceUnavailable || intelligence.method != "answer" {
		t.Fatalf("model error: %d %s", response.Code, response.Body)
	}
	intelligence.err = domain.ErrInvalidModelOutput
	response = authorizedRequest(handler, http.MethodPost, "/api/snapshots/"+snapshotID+"/answers", `{"question":"why"}`)
	if response.Code != http.StatusBadGateway {
		t.Fatalf("invalid output: %d", response.Code)
	}
}

func TestModelRoutesHaveSeparateBoundedDeadline(t *testing.T) {
	cfg := testConfig()
	cfg.RequestTimeout = 5 * time.Millisecond
	cfg.AITimeout = 40 * time.Millisecond
	intelligence := &intelligenceStub{wait: true}
	cfg.Explorer = &Explorer{Catalog: &catalogStub{}, Tracker: &trackerStub{}, RepositoryID: "repo", Token: testToken, Knowledge: &knowledgeStub{}, Intelligence: intelligence}
	start := time.Now()
	response := authorizedRequest(NewHandler(nil, cfg), http.MethodPost, "/api/snapshots/"+snapshotID+"/answers", `{"question":"why"}`)
	if response.Code != http.StatusGatewayTimeout || time.Since(start) < 30*time.Millisecond {
		t.Fatalf("wrong AI deadline: %d %s", response.Code, time.Since(start))
	}
}
