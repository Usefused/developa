package application

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	"developa/internal/domain"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

func TestIntelligenceCarriesWorkerFenceThroughAuditAndPublication(t *testing.T) {
	store := &intelligenceTestStore{symbols: intelligenceFacts(1)}
	service := fixtureIntelligence(t, store, featureTestModel(), IntelligenceConfig{})
	ctx := withAnalysisLease(context.Background(), domain.AnalysisJob{ID: "job", LeaseToken: "PRIVATE-LEASE"})
	if _, err := service.Discover(ctx, "snapshot"); err != nil {
		t.Fatal(err)
	}
	if len(store.executions) != 2 {
		t.Fatal("background execution audit missing")
	}
	for _, execution := range store.executions {
		if execution.JobID != "job" || execution.LeaseToken != "PRIVATE-LEASE" || execution.Actor != "system" {
			t.Fatal("background execution lost its trusted lease or actor")
		}
		data, _ := json.Marshal(execution)
		if strings.Contains(string(data), "PRIVATE-LEASE") {
			t.Fatal("execution API serialization exposed the fencing token")
		}
	}
}

func TestIntelligenceLostLeaseAtAuditPreventsModelExecution(t *testing.T) {
	store := &intelligenceTestStore{symbols: intelligenceFacts(1), auditError: domain.ErrLeaseLost}
	model := featureTestModel()
	service := fixtureIntelligence(t, store, model, IntelligenceConfig{})
	ctx := withAnalysisLease(context.Background(), domain.AnalysisJob{ID: "job", LeaseToken: "PRIVATE-LEASE"})
	_, err := service.Discover(ctx, "snapshot")
	if !errors.Is(err, domain.ErrLeaseLost) || model.calls.Load() != 0 {
		t.Fatal("lost audit fence was hidden or permitted model execution")
	}
}

func TestAnalysisWorkerLinksOriginalTraceWithoutExportingLease(t *testing.T) {
	exporter := tracetest.NewInMemoryExporter()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exporter))
	previous := otel.GetTracerProvider()
	otel.SetTracerProvider(provider)
	t.Cleanup(func() { otel.SetTracerProvider(previous); _ = provider.Shutdown(context.Background()) })
	ctx, origin := otel.Tracer("fixture").Start(context.Background(), "request")
	carrier := propagation.MapCarrier{}
	propagation.TraceContext{}.Inject(ctx, carrier)
	worker, store, intelligence := analysisFixture(t, 1)
	queuedAnalysis(t, worker)
	store.job.TraceParent = carrier.Get("traceparent")
	var leaseToken string
	intelligence.discover = func(ctx context.Context, snapshot string) (domain.FeatureRun, error) {
		leaseToken = ctx.Value(analysisLeaseKey{}).(analysisLease).token
		return store.advance(ctx, snapshot, 1)
	}
	origin.End()
	processAnalysis(t, worker)
	spans := exporter.GetSpans()
	assertAnalysisTraceLink(t, spans, origin.SpanContext().TraceID().String())
	if leaseToken == "" || strings.Contains(fmt.Sprint(spans), leaseToken) {
		t.Fatal("lease was missing or exported in telemetry")
	}
}

func assertAnalysisTraceLink(t *testing.T, spans tracetest.SpanStubs, traceID string) {
	t.Helper()
	for _, span := range spans {
		if span.Name != "analysis.chunk" {
			continue
		}
		if len(span.Links) != 1 || span.Links[0].SpanContext.TraceID().String() != traceID {
			t.Fatal("background trace lost original execution link")
		}
		if span.SpanContext.TraceID().String() == traceID {
			t.Fatal("background attempt reused a completed request trace")
		}
		return
	}
	t.Fatal("background execution span missing")
}
