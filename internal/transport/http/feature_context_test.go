package httptransport

import (
	"context"
	"net/http"
	"testing"

	"developa/internal/domain"
)

type featureContextServiceStub struct {
	snapshot, feature string
	options           domain.FeatureContextOptions
	calls             int
}

func (s *featureContextServiceStub) FeatureContext(_ context.Context, snapshot, feature string, options domain.FeatureContextOptions) (domain.FeatureContextBundle, error) {
	s.snapshot, s.feature, s.options = snapshot, feature, options
	s.calls++
	return domain.FeatureContextBundle{RepositoryID: "repo", SnapshotID: snapshot, Feature: domain.Feature{ID: feature}, Options: options}, nil
}

func TestFeatureContextEndpointPinsScopeAndBounds(t *testing.T) {
	service := &featureContextServiceStub{}
	cfg := testConfig()
	cfg.Explorer = &Explorer{Catalog: &catalogStub{}, Tracker: &trackerStub{}, RepositoryID: "repo", Token: testToken, Knowledge: &knowledgeStub{}, FeatureContexts: service}
	handler := NewHandler(nil, cfg)
	path := "/api/snapshots/" + snapshotID + "/features/" + symbolID + "/context?source_limit=9&depth=8&flow_limit=70"
	response := authorizedRequest(handler, http.MethodGet, path, "")
	if response.Code != http.StatusOK || service.calls != 1 || service.snapshot != snapshotID || service.feature != symbolID {
		t.Fatalf("feature context scope failed: %d %+v", response.Code, service)
	}
	if service.options != (domain.FeatureContextOptions{SourceLimit: 9, Depth: 8, FlowLimit: 70}) {
		t.Fatal("feature context bounds did not reach the application service")
	}
}

func TestFeatureContextEndpointStrictlyRejectsInvalidQueries(t *testing.T) {
	queries := []string{"source_limit=0", "source_limit=21", "depth=13", "flow_limit=101", "unknown=1", "depth=6&depth=8", "depth="}
	for _, query := range queries {
		service := &featureContextServiceStub{}
		cfg := testConfig()
		cfg.Explorer = &Explorer{Catalog: &catalogStub{}, Tracker: &trackerStub{}, RepositoryID: "repo", Token: testToken, Knowledge: &knowledgeStub{}, FeatureContexts: service}
		path := "/api/snapshots/" + snapshotID + "/features/" + symbolID + "/context?" + query
		if response := authorizedRequest(NewHandler(nil, cfg), http.MethodGet, path, ""); response.Code != http.StatusBadRequest || service.calls != 0 {
			t.Fatalf("invalid query reached service: %s %d", query, response.Code)
		}
	}
}

func TestFeatureContextEndpointRequiresConfiguredService(t *testing.T) {
	handler, _, _ := intelligenceFixture()
	response := authorizedRequest(handler, http.MethodGet, "/api/snapshots/"+snapshotID+"/features/"+symbolID+"/context", "")
	if response.Code != http.StatusServiceUnavailable {
		t.Fatal("missing feature context service did not fail closed")
	}
}
