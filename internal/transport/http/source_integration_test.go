package httptransport

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"unicode/utf8"

	"developa/internal/domain"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

func TestIntegrationSourceRemainsPinnedAfterGitEdit(t *testing.T) {
	exporter := installTraceProvider(t)
	fixture := newIntegrationExplorer(t)
	declaration := "func Large() { _ = `source-secret-marker" + strings.Repeat("界😀", 2000) + "` }"
	integrationWrite(t, fixture.root, "main.go", "package fixture\n"+declaration+"\n")
	fixture.manager.Start(context.Background())
	initial := awaitIntegrationSnapshot(t, fixture, "")
	original := integrationSourceSymbol(t, fixture, initial.ID)
	if !original.Symbol.SourceTruncated {
		t.Fatal("fixture must exceed the stored symbol preview")
	}
	if actual := integrationFullSource(t, fixture, initial.ID, original.Symbol.ID); actual != declaration {
		t.Fatal("initial source chunks omitted complete declaration bytes")
	}
	integrationWrite(t, fixture.root, "main.go", "package fixture\nfunc Large() {}\n")
	modified := awaitIntegrationSnapshot(t, fixture, initial.ID)
	if actual := integrationFullSource(t, fixture, initial.ID, original.Symbol.ID); actual != declaration {
		t.Fatal("historical source changed after the working tree edit")
	}
	if actual := integrationFullSource(t, fixture, modified.ID, original.Symbol.ID); actual != "func Large() {}" {
		t.Fatal("new snapshot did not retain the edited declaration")
	}
	assertIntegrationSourceIdentity(t, fixture, original, initial.ID, modified.ID)
	assertIntegrationSourceTrace(t, fixture, exporter, initial.ID)
}

func assertIntegrationSourceTrace(t *testing.T, fixture *integrationExplorer, exporter *tracetest.InMemoryExporter, snapshot string) {
	t.Helper()
	path := "/api/snapshots/" + snapshot + "/symbols/" + strings.Repeat("f", 64) + "/source"
	if status := integrationRequest(t, fixture, http.MethodGet, path, true, nil); status != http.StatusNotFound {
		t.Fatalf("missing source must return 404: %d", status)
	}
	seen := map[string]bool{}
	for _, span := range exporter.GetSpans() {
		if span.Name == "postgres.symbol_source" {
			assertIntegrationSourceSpan(t, span, seen)
		}
	}
	if !seen["execution.completed"] || !seen["execution.failed"] {
		t.Fatal("source spans omitted completion/failure lifecycle events")
	}
}

func assertIntegrationSourceSpan(t *testing.T, span tracetest.SpanStub, events map[string]bool) {
	t.Helper()
	if span.SpanContext.TraceID().String() != integrationTraceID || !span.Parent.IsValid() {
		t.Fatal("source SQL span lost its HTTP request trace")
	}
	encoded, err := json.Marshal(span)
	if err != nil || strings.Contains(string(encoded), "source-secret-marker") {
		t.Fatal("source trace serialization failed or exported source content")
	}
	for _, event := range span.Events {
		events[event.Name] = true
	}
}

func integrationSourceSymbol(t *testing.T, fixture *integrationExplorer, snapshot string) domain.SymbolDetail {
	t.Helper()
	var page domain.SymbolPage
	integrationRead(t, fixture, "/api/snapshots/"+snapshot+"/symbols?file=main.go&kind=function", &page)
	if len(page.Items) != 1 {
		t.Fatal("source fixture symbol missing")
	}
	return page.Items[0]
}

func integrationFullSource(t *testing.T, fixture *integrationExplorer, snapshot, symbol string) string {
	t.Helper()
	var text strings.Builder
	for offset := 0; ; {
		var chunk domain.SymbolSource
		path := fmt.Sprintf("/api/snapshots/%s/symbols/%s/source?offset=%d", snapshot, symbol, offset)
		integrationRead(t, fixture, path, &chunk)
		if !chunk.Complete || !utf8.ValidString(chunk.Source) || len(chunk.Source) > 8192 {
			t.Fatal("source HTTP chunk invalid, incomplete, or oversized")
		}
		text.WriteString(chunk.Source)
		if chunk.NextOffset == nil {
			return text.String()
		}
		if *chunk.NextOffset <= offset || *chunk.NextOffset != text.Len() {
			t.Fatal("source continuation did not advance by returned bytes")
		}
		offset = *chunk.NextOffset
	}
}

func assertIntegrationSourceIdentity(t *testing.T, fixture *integrationExplorer, original domain.SymbolDetail, oldSnapshot, newSnapshot string) {
	t.Helper()
	var older, newer domain.SymbolSource
	base := "/symbols/" + original.Symbol.ID + "/source"
	integrationRead(t, fixture, "/api/snapshots/"+oldSnapshot+base, &older)
	integrationRead(t, fixture, "/api/snapshots/"+newSnapshot+base, &newer)
	if older.SourceID != original.Symbol.SourceID || older.ContentHash != original.Symbol.ContentHash || older.Span != original.Symbol.Span {
		t.Fatal("historical source provenance changed")
	}
	if newer.ContentHash == older.ContentHash || newer.SourceID == older.SourceID || newer.SymbolID != older.SymbolID {
		t.Fatal("edited source did not distinguish logical and content identity")
	}
}
