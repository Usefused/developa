package git

import (
	"context"
	"fmt"
	"strings"
	"testing"

	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"
)

func TestTelemetryParentageAndRedaction(t *testing.T) {
	exporter := tracetest.NewInMemoryExporter()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exporter))
	defer provider.Shutdown(context.Background())
	oldTracer := tracer
	tracer = provider.Tracer("test/source")
	defer func() { tracer = oldTracer }()
	dir, repo := testRepository(t)
	writeTestFile(t, dir, ".env", "SECRET-CONTENT-MUST-NOT-EXPORT")
	ctx, parent := tracer.Start(context.Background(), "execution")
	parentID := parent.SpanContext()
	if _, err := repo.Capture(ctx); err != nil {
		t.Fatal(err)
	}
	parent.End()
	spans := exporter.GetSpans()
	assertCaptureParent(t, spans, parentID)
	serialized := fmt.Sprint(spans)
	for _, forbidden := range []string{dir, ".env", "SECRET-CONTENT-MUST-NOT-EXPORT"} {
		if strings.Contains(serialized, forbidden) {
			t.Fatalf("telemetry leaked %q", forbidden)
		}
	}
}

func assertCaptureParent(t *testing.T, spans tracetest.SpanStubs, parent trace.SpanContext) {
	t.Helper()
	for _, span := range spans {
		if span.Name != "source.capture" {
			continue
		}
		if span.Parent.SpanID() != parent.SpanID() {
			t.Fatal("capture lost parent execution")
		}
		for _, event := range span.Events {
			if event.Name == "completion" {
				return
			}
		}
		t.Fatal("capture completion event missing")
	}
	t.Fatal("capture span missing")
}

func TestTelemetryErrorsDoNotExportFilesystemPaths(t *testing.T) {
	exporter := tracetest.NewInMemoryExporter()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exporter))
	defer provider.Shutdown(context.Background())
	oldTracer := tracer
	tracer = provider.Tracer("test/source")
	defer func() { tracer = oldTracer }()
	_, err := Open(context.Background(), "/nonexistent/PRIVATE-SOURCE-PATH", Options{})
	if err == nil {
		t.Fatal("missing repository opened")
	}
	spans := exporter.GetSpans()
	if len(spans) != 1 {
		t.Fatal("failure span missing")
	}
	if strings.Contains(fmt.Sprint(spans), "PRIVATE-SOURCE-PATH") {
		t.Fatal("filesystem error leaked path")
	}
}
