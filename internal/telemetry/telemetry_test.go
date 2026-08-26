package telemetry

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/propagation"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

func TestSetupRejectsUnsafeEndpoints(t *testing.T) {
	for _, endpoint := range []string{"ftp://localhost", "http:///path", "http://user:secret@localhost", "http://localhost?token=secret", "http://localhost/#secret", "%invalid"} {
		_, err := Setup(context.Background(), Config{ServiceName: "test", Endpoint: endpoint})
		if err == nil {
			t.Fatal("unsafe endpoint was accepted")
		}
		if strings.Contains(err.Error(), "secret") {
			t.Fatal("configuration error leaked endpoint contents")
		}
	}
}

func TestSetupExportsToConfiguredCollectorAndPropagatesContext(t *testing.T) {
	restoreGlobals(t)
	requests := make(chan string, 1)
	collector := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		requests <- r.URL.Path
		w.Header().Set("Content-Type", "application/x-protobuf")
		w.WriteHeader(http.StatusOK)
	}))
	defer collector.Close()
	shutdown, err := Setup(context.Background(), Config{ServiceName: "test", Endpoint: collector.URL + "/collector"})
	if err != nil {
		t.Fatal(err)
	}
	ctx, span := otel.Tracer("test").Start(context.Background(), "test.execution")
	carrier := propagation.MapCarrier{}
	otel.GetTextMapPropagator().Inject(ctx, carrier)
	span.End()
	if carrier.Get("traceparent") == "" || carrier.Get("baggage") != "" {
		t.Fatal("trace propagation was not configured safely")
	}
	if err := shutdown(context.Background()); err != nil {
		t.Fatal("collector export failed")
	}
	assertExportPath(t, requests)
}

func assertExportPath(t *testing.T, requests <-chan string) {
	t.Helper()
	select {
	case path := <-requests:
		if path != "/collector/v1/traces" {
			t.Fatalf("unexpected OTLP path %q", path)
		}
	case <-time.After(time.Second):
		t.Fatal("configured collector received no spans")
	}
}

func TestSetupWithoutEndpointUsesStderrExporter(t *testing.T) {
	restoreGlobals(t)
	output, err := os.CreateTemp(t.TempDir(), "spans")
	if err != nil {
		t.Fatal(err)
	}
	previous := os.Stderr
	os.Stderr = output
	t.Cleanup(func() {
		os.Stderr = previous
		_ = output.Close()
	})
	shutdown, err := Setup(context.Background(), Config{ServiceName: "test"})
	if err != nil {
		t.Fatal(err)
	}
	_, span := otel.Tracer("test").Start(context.Background(), "stderr.execution")
	span.End()
	if err := shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile(output.Name())
	if err != nil || !strings.Contains(string(content), "stderr.execution") {
		t.Fatal("default exporter did not write the span to stderr")
	}
}

func TestFailCreatesSafeFailureEvent(t *testing.T) {
	exporter := tracetest.NewInMemoryExporter()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exporter))
	defer provider.Shutdown(context.Background())
	_, span := provider.Tracer("test").Start(context.Background(), "test.execution")
	Fail(span, "database_unavailable")
	span.End()
	spans := exporter.GetSpans()
	if len(spans) != 1 || spans[0].Status.Code != codes.Error || len(spans[0].Events) != 1 {
		t.Fatal("failed execution did not emit failure status and event")
	}
}

func restoreGlobals(t *testing.T) {
	t.Helper()
	provider, propagator, handler := otel.GetTracerProvider(), otel.GetTextMapPropagator(), otel.GetErrorHandler()
	t.Cleanup(func() {
		otel.SetTracerProvider(provider)
		otel.SetTextMapPropagator(propagator)
		otel.SetErrorHandler(handler)
	})
}
