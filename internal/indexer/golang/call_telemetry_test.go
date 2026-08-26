package golang

import (
	"context"
	"testing"
)

func TestCallAnalysisTelemetryPropagatesAndRedactsSource(t *testing.T) {
	files := []SourceFile{{Path: "secret-source.go", Content: []byte("package secretpackage\ntype PrivateInterface interface { Secret() }\ntype PrivateWorker struct{}\nfunc (PrivateWorker) Secret() {}\nfunc PrivateSecret() {}\nfunc Use(){ PrivateSecret() }\n")}}
	result, err := Parse(context.Background(), files)
	if err != nil {
		t.Fatal(err)
	}
	exporter, provider := traceFixture(t)
	ctx, parent := provider.Tracer("test").Start(context.Background(), "execution")
	if err := AnalyzeCalls(ctx, files, &result); err != nil {
		t.Fatal(err)
	}
	parent.End()
	spans := exporter.GetSpans()
	if len(spans) != 2 || spans[0].Parent.SpanID() != parent.SpanContext().SpanID() {
		t.Fatal("call analysis lost its execution parent")
	}
	assertEvents(t, spans[0], "execution.started", "execution.completed")
	assertRedacted(t, spans[0], "secret-source", "secretpackage", "PrivateSecret", "PrivateInterface", "PrivateWorker")
	attributes := map[string]string{}
	for _, attribute := range spans[0].Attributes {
		attributes[string(attribute.Key)] = attribute.Value.Emit()
	}
	if attributes["analysis.implementation_count"] != "1" || attributes["analysis.implementation_status"] != "complete" {
		t.Fatalf("implementation analysis omitted count/status telemetry: %+v", attributes)
	}
}

func TestCallAnalysisCancellationIsTraced(t *testing.T) {
	exporter, _ := traceFixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	result := Result{}
	_ = AnalyzeCalls(ctx, nil, &result)
	spans := exporter.GetSpans()
	if len(spans) != 1 {
		t.Fatalf("expected one canceled call-analysis span, got %d", len(spans))
	}
	assertEvents(t, spans[0], "execution.started", "execution.canceled")
}
