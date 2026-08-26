package golang

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/codes"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

func TestParseTelemetryPropagatesParentAndExcludesContent(t *testing.T) {
	exporter, provider := traceFixture(t)
	ctx, parent := provider.Tracer("test").Start(context.Background(), "execution")
	_, err := Parse(ctx, []SourceFile{{Path: "sensitive-file.go", Content: []byte("package sensitivePackage\nfunc PrivateSecret() {}")}})
	if err != nil {
		t.Fatal(err)
	}
	parent.End()
	spans := exporter.GetSpans()
	if len(spans) != 2 || spans[0].Parent.SpanID() != parent.SpanContext().SpanID() {
		t.Fatalf("missing parent linkage: %+v", spans)
	}
	assertEvents(t, spans[0], "execution.started", "execution.completed")
	assertRedacted(t, spans[0], "sensitive-file", "sensitivePackage", "PrivateSecret")
}

func TestParseFailureTelemetryDoesNotLeakParserMessages(t *testing.T) {
	exporter, _ := traceFixture(t)
	_, err := Parse(context.Background(), []SourceFile{{Path: "secret-path.go", Content: []byte("package sample\nfunc Run() { SECRET_TOKEN } @secret-value")}})
	if err != nil {
		t.Fatal(err)
	}
	spans := exporter.GetSpans()
	if len(spans) != 1 || spans[0].Status.Code != codes.Error {
		t.Fatalf("expected failed parse span: %+v", spans)
	}
	assertEvents(t, spans[0], "execution.started", "execution.failed")
	assertRedacted(t, spans[0], "SECRET_TOKEN", "secret-value", "secret-path", "illegal character")
}

func traceFixture(t *testing.T) (*tracetest.InMemoryExporter, *sdktrace.TracerProvider) {
	t.Helper()
	exporter := tracetest.NewInMemoryExporter()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exporter))
	previous := otel.GetTracerProvider()
	otel.SetTracerProvider(provider)
	t.Cleanup(func() {
		otel.SetTracerProvider(previous)
		if err := provider.Shutdown(context.Background()); err != nil {
			t.Error(err)
		}
	})
	return exporter, provider
}

func assertEvents(t *testing.T, span tracetest.SpanStub, expected ...string) {
	t.Helper()
	seen := map[string]bool{}
	for _, event := range span.Events {
		seen[event.Name] = true
	}
	for _, name := range expected {
		if !seen[name] {
			t.Fatalf("missing %q event: %+v", name, span.Events)
		}
	}
}

func assertRedacted(t *testing.T, span tracetest.SpanStub, excluded ...string) {
	t.Helper()
	serialized := fmt.Sprintf("%+v", span)
	for _, secret := range excluded {
		if strings.Contains(serialized, secret) {
			t.Fatalf("telemetry leaked %q", secret)
		}
	}
}
