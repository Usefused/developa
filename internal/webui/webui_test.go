package webui

import (
	"io/fs"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
)

func TestEmbeddedExplorerAndSecurityHeaders(t *testing.T) {
	response := httptest.NewRecorder()
	Handler().ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/", nil))
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), "/assets/entry.client-") {
		t.Fatal("explorer entrypoint was not served")
	}
	policy := response.Header().Get("Content-Security-Policy")
	if !strings.Contains(policy, "script-src 'self'") || strings.Contains(policy, "unsafe-inline") {
		t.Fatalf("unsafe script policy: %s", policy)
	}
}

func TestEmbeddedAssetsDoNotExposeHostFiles(t *testing.T) {
	files, err := fs.ReadDir(assets, "dist/assets")
	if err != nil || len(files) == 0 {
		t.Fatal("missing framework assets", err)
	}
	for _, file := range files {
		path := "/assets/" + file.Name()
		response := httptest.NewRecorder()
		Handler().ServeHTTP(response, httptest.NewRequest(http.MethodGet, path, nil))
		if response.Code != http.StatusOK {
			t.Fatalf("missing embedded asset: %s", path)
		}
	}
	response := httptest.NewRecorder()
	Handler().ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/assets/../../go.mod", nil))
	if response.Code == http.StatusOK {
		t.Fatal("asset request escaped embedded filesystem")
	}
}

func TestFrameworkDeepLinksAndFreshNonce(t *testing.T) {
	handler := Handler()
	seen := make(map[string]bool)
	for _, path := range []string{"/", "/blocks?file=main.go", "/flow?root=example", "/features?snapshot=example", "/changes", "/analysis", "/chain"} {
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, path, nil))
		nonce := assertFrameworkDocument(t, response)
		if seen[nonce] {
			t.Fatal("CSP nonce reused across responses")
		}
		seen[nonce] = true
	}
}

func assertFrameworkDocument(t *testing.T, response *httptest.ResponseRecorder) string {
	t.Helper()
	if response.Code != http.StatusOK || !strings.Contains(response.Header().Get("Content-Type"), "text/html") {
		t.Fatal("deep link did not return the framework shell")
	}
	page := response.Body.String()
	if strings.Contains(page, "__DENVERR_CSP_NONCE__") || !strings.Contains(page, "streamController.enqueue") {
		t.Fatal("incomplete framework hydration document")
	}
	match := regexp.MustCompile(`'nonce-([a-zA-Z0-9]+)'`).FindStringSubmatch(response.Header().Get("Content-Security-Policy"))
	if len(match) != 2 {
		t.Fatal("missing response CSP nonce")
	}
	for _, script := range regexp.MustCompile(`<script[^>]*>`).FindAllString(page, -1) {
		if !strings.Contains(script, `nonce="`+match[1]+`"`) {
			t.Fatal("framework script does not match the response nonce")
		}
	}
	return match[1]
}

func TestUnknownPagesAndAPIAreNotSPAFallbacks(t *testing.T) {
	for _, path := range []string{"/unknown", "/api/missing", "/assets/missing.js", "/.vite/manifest.json", "/assets/app.js"} {
		response := httptest.NewRecorder()
		Handler().ServeHTTP(response, httptest.NewRequest(http.MethodGet, path, nil))
		if response.Code != http.StatusNotFound {
			t.Fatalf("unknown path returned the SPA: %s", path)
		}
	}
}
