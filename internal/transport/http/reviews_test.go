package httptransport

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"developa/internal/domain"
)

type reviewerStub struct{ calls int }

func (*reviewerStub) Available() bool { return true }
func (s *reviewerStub) Review(_ context.Context, snapshot string, options domain.ReviewOptions) (domain.ReviewPage, error) {
	s.calls++
	return domain.ReviewPage{SnapshotID: snapshot, Options: options, Items: []domain.SymbolDetail{}}, nil
}

func TestReviewMutationsValidateJSONAndRequireAuthentication(t *testing.T) {
	reviewer := &reviewerStub{}
	cfg := testConfig()
	cfg.Explorer = &Explorer{Catalog: &catalogStub{}, Tracker: &trackerStub{}, RepositoryID: "repo", Token: testToken, Knowledge: &knowledgeStub{}, Reviewer: reviewer}
	handler := NewHandler(nil, cfg)
	path := "/api/snapshots/" + snapshotID + "/function-reviews"
	for _, body := range []string{`{"limit":9}`, `{"offset":-1}`, `{"symbol_id":"bad"}`, `{"unknown":true}`, `{} {}`, `{"limit":1.5}`, `{"symbol_id":"` + snapshotID + `","callee_of":"` + snapshotID + `"}`} {
		response := authorizedRequest(handler, http.MethodPost, path, body)
		if response.Code != http.StatusBadRequest {
			t.Fatalf("invalid review request returned %d", response.Code)
		}
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodPost, path, strings.NewReader(`{}`)))
	if response.Code != http.StatusUnauthorized || reviewer.calls != 0 {
		t.Fatal("unauthorized/invalid request reached review service")
	}
	if response := authorizedRequest(handler, http.MethodPost, path, `{}`); response.Code != http.StatusOK {
		t.Fatal("valid batch request rejected")
	}
}

func TestReviewMutationsRejectCrossOrigin(t *testing.T) {
	e := &Explorer{Reviewer: &reviewerStub{}}
	request := httptest.NewRequest(http.MethodPost, "http://localhost/api/snapshots/"+snapshotID+"/function-reviews", strings.NewReader(`{}`))
	request.Header.Set("Origin", "https://untrusted.invalid")
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	e.reviewFunctions(response, request)
	if response.Code != http.StatusForbidden {
		t.Fatal("cross-origin review request accepted")
	}
}
