package httptransport

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"developa/internal/domain"
)

func TestScanRejectsCrossOriginBeforeExecution(t *testing.T) {
	cases := []struct{ origin, site string }{
		{"http://attacker.invalid", ""}, {"null", ""}, {"http://example.com", "cross-site"},
		{"", "cross-site"}, {"https://example.com", "same-origin"}, {"http://example.com/path", ""},
	}
	for _, tc := range cases {
		handler, _, tracker := explorerFixture()
		request := httptest.NewRequest(http.MethodPost, "/api/scan", nil)
		request.Header.Set("Authorization", "Bearer "+testToken)
		request.Header.Set("Origin", tc.origin)
		request.Header.Set("Sec-Fetch-Site", tc.site)
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Code != http.StatusForbidden || tracker.scans != 0 {
			t.Fatalf("cross-origin scan ran: %+v %d", tc, response.Code)
		}
	}
}

func TestScanAllowsSameOriginAndCLIRequests(t *testing.T) {
	for _, origin := range []string{"", "http://example.com"} {
		handler, _, tracker := explorerFixture()
		request := httptest.NewRequest(http.MethodPost, "/api/scan", nil)
		request.Header.Set("Authorization", "Bearer "+testToken)
		request.Header.Set("Content-Type", "application/json")
		if origin != "" {
			request.Header.Set("Origin", origin)
		}
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Code != http.StatusAccepted || tracker.scans != 1 {
			t.Fatalf("valid scan rejected: %d %s", response.Code, response.Body)
		}
		if !strings.Contains(response.Body.String(), `"actor":"operator"`) {
			t.Fatal("scan response lost execution identity")
		}
	}
}

func TestScanRejectsUnknownOrOversizedBodies(t *testing.T) {
	bodies := []string{`{"root":"/etc"}`, `{"actor":"system"}`, "{", "{} {}", strings.Repeat(" ", 1025)}
	for _, body := range bodies {
		handler, _, tracker := explorerFixture()
		response := authorizedRequest(handler, http.MethodPost, "/api/scan", body)
		if response.Code != http.StatusBadRequest || tracker.scans != 0 {
			t.Fatalf("invalid scan body executed: %d", response.Code)
		}
	}
}

func TestScanAllowsEmptyJSONAndReportsBusy(t *testing.T) {
	handler, _, tracker := explorerFixture()
	response := authorizedRequest(handler, http.MethodPost, "/api/scan", "{}")
	if response.Code != http.StatusAccepted || tracker.scans != 1 {
		t.Fatalf("empty JSON scan failed: %d", response.Code)
	}
	tracker.err = domain.ErrBusy
	response = authorizedRequest(handler, http.MethodPost, "/api/scan", "")
	if response.Code != http.StatusConflict {
		t.Fatalf("busy scan response: %d", response.Code)
	}
}

func TestScanRejectsNonJSONContentType(t *testing.T) {
	handler, _, tracker := explorerFixture()
	request := httptest.NewRequest(http.MethodPost, "/api/scan", strings.NewReader("{}"))
	request.Header.Set("Authorization", "Bearer "+testToken)
	request.Header.Set("Content-Type", "text/plain")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusBadRequest || tracker.scans != 0 {
		t.Fatal("non-JSON scan body accepted")
	}
	if response.Header().Get("Access-Control-Allow-Origin") != "" {
		t.Fatal("API must not enable CORS")
	}
}

func TestAuthenticatedAPITraceHasTrustedActorAndNoRequestData(t *testing.T) {
	exporter := installTraceProvider(t)
	handler, _, _ := explorerFixture()
	response := authorizedRequest(handler, http.MethodGet, "/api/snapshots/"+snapshotID+"/symbols?q=secret-source&actor=system", "")
	if response.Code != http.StatusOK {
		t.Fatalf("trace fixture failed: %d", response.Code)
	}
	spans := exporter.GetSpans()
	if len(spans) != 1 || spans[0].Name != "HTTP /api/snapshots/{snapshot}/symbols" {
		t.Fatalf("expected safe route template: %+v", spans)
	}
	assertSafeRequestSpan(t, spans[0])
	actor := ""
	for _, attribute := range spans[0].Attributes {
		if string(attribute.Key) == "actor.type" {
			actor = attribute.Value.AsString()
		}
	}
	if actor != "operator" {
		t.Fatalf("authenticated actor was not operator: %q", actor)
	}
}
