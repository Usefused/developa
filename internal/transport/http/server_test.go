package httptransport

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/propagation"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

type readinessFunc func(context.Context) error

func (f readinessFunc) Ping(ctx context.Context) error { return f(ctx) }

func testConfig() Config {
	return Config{Address: ":8080", ReadinessTimeout: 50 * time.Millisecond, RequestTimeout: time.Second}
}

func TestHealthDoesNotRequireDatabase(t *testing.T) {
	handler := NewHandler(readinessFunc(func(context.Context) error {
		t.Fatal("liveness must not query PostgreSQL")
		return nil
	}), testConfig())
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	if response.Code != http.StatusOK || response.Body.String() != "{\"status\":\"ok\"}\n" {
		t.Fatalf("unexpected liveness response: %d %s", response.Code, response.Body)
	}
}

func TestReadinessChecksDatabaseExactlyOnce(t *testing.T) {
	for _, healthy := range []bool{true, false} {
		calls := 0
		handler := NewHandler(readinessFunc(func(context.Context) error {
			calls++
			if !healthy {
				return errors.New("secret-db-password")
			}
			return nil
		}), testConfig())
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/readyz", nil))
		assertReadiness(t, response, healthy)
		if calls != 1 {
			t.Fatalf("readiness performed %d database queries", calls)
		}
	}
}

func assertReadiness(t *testing.T, response *httptest.ResponseRecorder, healthy bool) {
	t.Helper()
	want := http.StatusServiceUnavailable
	if healthy {
		want = http.StatusOK
	}
	if response.Code != want {
		t.Fatalf("readiness status = %d, want %d", response.Code, want)
	}
	if strings.Contains(response.Body.String(), "secret") {
		t.Fatal("readiness exposed the database failure")
	}
}

func TestReadinessDeadlineReachesDatabase(t *testing.T) {
	handler := NewHandler(readinessFunc(func(ctx context.Context) error {
		<-ctx.Done()
		return ctx.Err()
	}), testConfig())
	response := httptest.NewRecorder()
	started := time.Now()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/readyz", nil))
	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("timed out readiness returned %d", response.Code)
	}
	if time.Since(started) > time.Second {
		t.Fatal("readiness did not obey its deadline")
	}
}

func TestRouterRejectsUnknownRoutesAndMethods(t *testing.T) {
	cases := []struct {
		method, path string
		status       int
	}{
		{http.MethodGet, "/missing", http.StatusNotFound},
		{http.MethodPost, "/healthz", http.StatusMethodNotAllowed},
	}
	handler := NewHandler(nil, testConfig())
	for _, tc := range cases {
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest(tc.method, tc.path, nil))
		if response.Code != tc.status {
			t.Fatalf("%s %s returned %d", tc.method, tc.path, response.Code)
		}
	}
}

func TestRequestTracePreservesParentWithoutRequestContents(t *testing.T) {
	exporter := installTraceProvider(t)
	handler := NewHandler(nil, testConfig())
	request := httptest.NewRequest(http.MethodGet, "/healthz?prompt=secret-prompt", nil)
	request.Header.Set("traceparent", "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	spans := exporter.GetSpans()
	if len(spans) != 1 {
		t.Fatalf("expected one request span, got %d", len(spans))
	}
	span := spans[0]
	if span.Parent.SpanID().String() != "00f067aa0ba902b7" || span.Name != "HTTP /healthz" {
		t.Fatal("request trace lost its parent or router template")
	}
	if response.Header().Get("X-Trace-ID") != "4bf92f3577b34da6a3ce929d0e0e4736" {
		t.Fatal("response missing trace correlation")
	}
	assertSafeRequestSpan(t, span)
}

func TestUnmatchedRequestDoesNotExportRawPath(t *testing.T) {
	exporter := installTraceProvider(t)
	handler := NewHandler(nil, testConfig())
	handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/secret-source-token", nil))
	spans := exporter.GetSpans()
	if len(spans) != 1 || spans[0].Name != "HTTP unmatched" {
		t.Fatal("unmatched request exported the raw URL")
	}
	assertSafeRequestSpan(t, spans[0])
}

func TestReadinessPanicIsSanitizedAndTraced(t *testing.T) {
	exporter := installTraceProvider(t)
	handler := NewHandler(readinessFunc(func(context.Context) error { panic("secret-panic") }), testConfig())
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/readyz", nil))
	if response.Code != http.StatusInternalServerError || strings.Contains(response.Body.String(), "secret") {
		t.Fatal("panic response was not sanitized")
	}
	spans := exporter.GetSpans()
	if len(spans) != 1 || spans[0].Status.Code != codes.Error {
		t.Fatal("panic did not produce a failed span")
	}
}

func assertSafeRequestSpan(t *testing.T, span tracetest.SpanStub) {
	t.Helper()
	for _, attr := range span.Attributes {
		if strings.Contains(attr.Value.AsString(), "secret") {
			t.Fatal("request trace exposed private request contents")
		}
	}
	if len(span.Events) != 2 {
		t.Fatal("request span missing lifecycle events")
	}
}

func installTraceProvider(t *testing.T) *tracetest.InMemoryExporter {
	t.Helper()
	exporter := tracetest.NewInMemoryExporter()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exporter))
	previousProvider, previousPropagation := otel.GetTracerProvider(), otel.GetTextMapPropagator()
	otel.SetTracerProvider(provider)
	otel.SetTextMapPropagator(propagation.TraceContext{})
	t.Cleanup(func() {
		otel.SetTracerProvider(previousProvider)
		otel.SetTextMapPropagator(previousPropagation)
		_ = provider.Shutdown(context.Background())
	})
	return exporter
}
