package httptransport

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"slices"
	"strings"
	"testing"

	"developa/api"
	"github.com/go-chi/chi/v5"
)

func TestOpenAPIDocumentIsCurrentAndDeterministic(t *testing.T) {
	for range 3 {
		generated, err := OpenAPIDocument()
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(generated, []byte(api.Document())) {
			t.Fatal("OpenAPI is stale: run make api-generate")
		}
	}
}

type contractRuntime struct{}

func (contractRuntime) Explorer(string) (*Explorer, error) { return &Explorer{}, nil }
func (contractRuntime) RepositoryIDs() []string            { return nil }

func TestOpenAPICoversRegisteredRoutesAndNoInventedEndpoints(t *testing.T) {
	cfg := testConfig()
	cfg.WorkspaceRuntime, cfg.Explorer = contractRuntime{}, &Explorer{}
	actual := walkedRoutes(t, NewHandler(nil, cfg).(chi.Router), "")
	repository := chi.NewRouter()
	cfg.Explorer.mountRoutes(repository)
	actual = append(actual, walkedRoutes(t, repository, "/api")...)
	actual = append(actual, walkedRoutes(t, repository, "/api/repositories/{repository}")...)
	var document struct {
		Paths map[string]map[string]json.RawMessage `json:"paths"`
	}
	if err := json.Unmarshal([]byte(api.Document()), &document); err != nil {
		t.Fatal(err)
	}
	want := []string{}
	for path, methods := range document.Paths {
		for method := range methods {
			want = append(want, strings.ToUpper(method)+" "+path)
		}
	}
	slices.Sort(actual)
	slices.Sort(want)
	if !reflect.DeepEqual(actual, want) {
		t.Fatalf("registered routes:\n%s\nOpenAPI routes:\n%s", strings.Join(actual, "\n"), strings.Join(want, "\n"))
	}
}

func walkedRoutes(t *testing.T, router chi.Router, prefix string) []string {
	t.Helper()
	routes := []string{}
	err := chi.Walk(router, func(method, path string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
		path = strings.TrimSuffix(path, "/")
		// Dynamic repository handlers are mounted at runtime; walk their router separately.
		if strings.Contains(path, "*") || path == "/api" || strings.HasSuffix(path, "/repositories/{repository}") {
			return nil
		}
		routes = append(routes, method+" "+prefix+path)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return routes
}

func TestOpenAPIIsPublicWithoutRuntimeDependenciesAndRemainsTraced(t *testing.T) {
	installTraceProvider(t)
	for _, managed := range []bool{false, true} {
		cfg := testConfig()
		cfg.Explorer = &Explorer{Token: testToken, WorkspaceManagement: true}
		if managed {
			cfg.WorkspaceRuntime = contractRuntime{}
		}
		handler := NewHandler(nil, cfg)
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/openapi.json", nil))
		assertOpenAPIResponse(t, response)
		protected := httptest.NewRecorder()
		handler.ServeHTTP(protected, httptest.NewRequest(http.MethodGet, "/api/repositories", nil))
		if protected.Code != http.StatusUnauthorized {
			t.Fatal("spec route weakened API authentication", protected.Code)
		}
	}
}

func assertOpenAPIResponse(t *testing.T, response *httptest.ResponseRecorder) {
	t.Helper()
	if response.Code != http.StatusOK || response.Body.String() != api.Document() {
		t.Fatal("served contract differs from embedded specification", response.Code)
	}
	if response.Header().Get("Content-Type") != "application/json" || response.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("incorrect contract response headers")
	}
	if len(response.Header().Get("X-Trace-ID")) != 32 {
		t.Fatal("contract discovery lost trace correlation")
	}
}
