package application

import (
	"context"
	"errors"
	"testing"
	"time"

	"developa/internal/domain"

	"go.opentelemetry.io/otel"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

func TestManagerSerializesConcurrentManualRequests(t *testing.T) {
	store := newManagerStore()
	root := fixtureRepository(t, "package fixture\nfunc First() {}\n")
	manager := fixtureManager(t, root, store, time.Hour)
	awaitReady(t, manager, "1")
	<-store.entered
	gate := make(chan struct{})
	store.setGate(gate)
	writeFixture(t, root, "fixture.go", "package fixture\nfunc Second() {}\n")
	accepted := concurrentRequests(t, manager, 20)
	awaitSaveEntered(t, store)
	if !store.hasOutcome(accepted.ID, "queued") {
		t.Fatal("accepted work lacked durable queued audit")
	}
	close(gate)
	awaitReady(t, manager, "2")
	store.mu.Lock()
	defer store.mu.Unlock()
	if store.maxSaving != 1 {
		t.Fatal("snapshot writes overlapped")
	}
}

type scanRequestResult struct {
	execution domain.Execution
	err       error
}

func concurrentRequests(t *testing.T, manager *Manager, count int) domain.Execution {
	t.Helper()
	start := make(chan struct{})
	results := make(chan scanRequestResult, count)
	for range count {
		go func() {
			<-start
			execution, err := manager.RequestScan(context.Background())
			results <- scanRequestResult{execution: execution, err: err}
		}()
	}
	close(start)
	var accepted []domain.Execution
	for range count {
		result := <-results
		if result.err == nil {
			accepted = append(accepted, result.execution)
			continue
		}
		if !errors.Is(result.err, domain.ErrBusy) {
			t.Fatal(result.err)
		}
	}
	if len(accepted) != 1 {
		t.Fatalf("accepted %d concurrent requests", len(accepted))
	}
	return accepted[0]
}

func awaitSaveEntered(t *testing.T, store *managerStore) {
	t.Helper()
	select {
	case <-store.entered:
	case <-time.After(10 * time.Second):
		t.Fatal("save was never attempted")
	}
}

func TestManagerShutdownCancelsActiveScanAndAudits(t *testing.T) {
	store := newManagerStore()
	root := fixtureRepository(t, "package fixture\nfunc First() {}\n")
	manager := fixtureManager(t, root, store, time.Hour)
	awaitReady(t, manager, "1")
	<-store.entered
	store.setGate(make(chan struct{}))
	writeFixture(t, root, "fixture.go", "package fixture\nfunc Second() {}\n")
	execution := requestManagerScan(t, manager)
	awaitSaveEntered(t, store)
	manager.Close()
	project, err := manager.Project(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if project.Watching || project.Snapshot.ID != "1" {
		t.Fatal("shutdown discarded published state")
	}
	if !store.hasOutcome(execution.ID, "canceled") {
		t.Fatal("canceled job audit missing")
	}
	if _, err := manager.RequestScan(context.Background()); !errors.Is(err, context.Canceled) {
		t.Fatalf("closed manager accepted work: %v", err)
	}
}

func TestManagerRequestPreservesContextAfterHTTPCancellation(t *testing.T) {
	_, provider := managerTraceProvider(t)
	store := newManagerStore()
	manager := fixtureManager(t, fixtureRepository(t, "package fixture\n"), store, time.Hour)
	awaitReady(t, manager, "1")
	ctx, parent := provider.Tracer("http-test").Start(context.WithValue(context.Background(), testContextKey{}, "trusted"), "http.execution")
	ctx, cancel := context.WithCancel(ctx)
	execution, err := manager.RequestScan(ctx)
	if err != nil {
		t.Fatal(err)
	}
	cancel()
	parent.End()
	awaitReady(t, manager, "1")
	if execution.Actor != "operator" || execution.TraceID != parent.SpanContext().TraceID().String() {
		t.Fatal("execution attribution or trace correlation lost")
	}
	assertExecutionContext(t, store, execution.ID)
}

func managerTraceProvider(t *testing.T) (*tracetest.InMemoryExporter, *sdktrace.TracerProvider) {
	t.Helper()
	exporter := tracetest.NewInMemoryExporter()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exporter))
	previousProvider := otel.GetTracerProvider()
	otel.SetTracerProvider(provider)
	t.Cleanup(func() { otel.SetTracerProvider(previousProvider); _ = provider.Shutdown(context.Background()) })
	return exporter, provider
}

func TestManagerSlowWatchLeavesManualAdmissionWindow(t *testing.T) {
	exporter, _ := managerTraceProvider(t)
	store := newManagerStore()
	root := fixtureRepository(t, "package fixture\nfunc First() {}\n")
	interval := 100 * time.Millisecond
	manager := fixtureManager(t, root, store, interval)
	awaitReady(t, manager, "1")
	<-store.entered
	gate := make(chan struct{})
	store.setGate(gate)
	writeFixture(t, root, "fixture.go", "package fixture\nfunc Second() {}\n")
	awaitSaveEntered(t, store)
	// Hold a real watch publication beyond its polling period to expose a queued
	// ticker event; wall-clock speed of Git or the test runner is not the trigger.
	timer := time.NewTimer(2 * interval)
	defer timer.Stop()
	<-timer.C
	close(gate)
	awaitReady(t, manager, "2")
	execution := requestManagerScan(t, manager)
	awaitReady(t, manager, "2")
	if !store.hasOutcome(execution.ID, "completed") {
		t.Fatal("manual work was not completed after a slow watch scan")
	}
	spans := awaitManagerWatchSpans(t, exporter, 2)
	if gap := spans[1].StartTime.Sub(spans[0].EndTime); gap < interval {
		t.Fatalf("watch scans consumed the manual admission window: gap=%s interval=%s", gap, interval)
	}
}

func awaitManagerWatchSpans(t *testing.T, exporter *tracetest.InMemoryExporter, count int) tracetest.SpanStubs {
	t.Helper()
	deadline := time.NewTimer(10 * time.Second)
	defer deadline.Stop()
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		spans := managerWatchSpans(exporter.GetSpans())
		if len(spans) >= count {
			return spans
		}
		select {
		case <-deadline.C:
			t.Fatalf("expected %d completed watch spans, got %d", count, len(spans))
		case <-ticker.C:
		}
	}
}

func managerWatchSpans(spans tracetest.SpanStubs) tracetest.SpanStubs {
	var watches tracetest.SpanStubs
	for _, span := range spans {
		if span.Name != "repository.scan" {
			continue
		}
		for _, attr := range span.Attributes {
			if string(attr.Key) == "execution.trigger" && attr.Value.AsString() == "watch" {
				watches = append(watches, span)
			}
		}
	}
	return watches
}

func assertExecutionContext(t *testing.T, store *managerStore, id string) {
	t.Helper()
	store.mu.Lock()
	defer store.mu.Unlock()
	count := 0
	for _, record := range store.executions {
		if record.execution.ID != id {
			continue
		}
		count++
		if record.contextValue != "trusted" {
			t.Fatal("async execution lost request context")
		}
	}
	if count != 3 {
		t.Fatalf("expected queued/running/completed audit, got %d", count)
	}
}

func TestManagerRejectedAuditDoesNotEnqueue(t *testing.T) {
	store := newManagerStore()
	manager := fixtureManager(t, fixtureRepository(t, "package fixture\n"), store, time.Hour)
	awaitReady(t, manager, "1")
	store.setFailure(false, true)
	if _, err := manager.RequestScan(context.Background()); err == nil {
		t.Fatal("request accepted without durable audit")
	}
	project := awaitManager(t, manager, func(p domain.Project) bool { return p.Status == "error" })
	if project.Snapshot.ID != "1" || len(manager.requests) != 0 {
		t.Fatal("failed audit enqueued work or changed snapshot")
	}
}
