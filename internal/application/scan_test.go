package application

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	goparser "developa/internal/indexer/golang"
	source "developa/internal/source/git"

	"go.opentelemetry.io/otel"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

func TestScanCapturesAndParsesRepository(t *testing.T) {
	root := fixtureRepository(t, "package fixture\ntype Service struct { Name string }\nfunc (s *Service) Run(input string) (string, error) { return input, nil }\n")
	scanner := fixtureScanner(t, root)
	report, err := scanner.Scan(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if report.Index.Analysis != "syntax" || report.Index.Completeness != goparser.Complete {
		t.Fatalf("unexpected analysis state: %s %s", report.Index.Analysis, report.Index.Completeness)
	}
	symbol := reportSymbol(t, report, "Run")
	if symbol.Kind != goparser.Method || len(symbol.Parameters) != 1 || len(symbol.Results) != 2 {
		t.Fatalf("unexpected method block: %+v", symbol)
	}
	if report.Snapshot.Commit == "" || report.Snapshot.Fingerprint == "" {
		t.Fatal("source revision metadata missing")
	}
}

func TestScanPreservesSyntaxDiagnostics(t *testing.T) {
	root := fixtureRepository(t, "package fixture\nfunc Broken(\n")
	report, err := fixtureScanner(t, root).Scan(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if report.Index.Completeness != goparser.Partial || len(report.Index.Diagnostics) == 0 {
		t.Fatal("invalid syntax must remain visible as a partial index")
	}
}

func TestScanHonorsCancellation(t *testing.T) {
	scanner := fixtureScanner(t, fixtureRepository(t, "package fixture\n"))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := scanner.Scan(ctx)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("wanted cancellation, got %v", err)
	}
}

func TestWatchReparsesChangedSource(t *testing.T) {
	root := fixtureRepository(t, "package fixture\nfunc First() {}\n")
	scanner := fixtureScanner(t, root)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	var reports []Report
	err := scanner.Watch(ctx, 10*time.Millisecond, func(report Report) error {
		reports = append(reports, report)
		if len(reports) == 1 {
			return os.WriteFile(filepath.Join(root, "fixture.go"), []byte("package fixture\nfunc Second() {}\n"), 0600)
		}
		cancel()
		return nil
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("wanted successful watch cancellation, got %v", err)
	}
	checkWatchReports(t, reports)
}

func checkWatchReports(t *testing.T, reports []Report) {
	t.Helper()
	if len(reports) != 2 {
		t.Fatalf("wanted two source states, got %d", len(reports))
	}
	if reports[0].ExecutionID != reports[1].ExecutionID {
		t.Fatal("watch updates lost execution correlation")
	}
	if reports[0].Snapshot.Fingerprint == reports[1].Snapshot.Fingerprint {
		t.Fatal("source fingerprint did not change")
	}
	if len(reports[1].Changes) != 1 || reports[1].Changes[0].Kind != source.Modified {
		t.Fatalf("unexpected source changes: %+v", reports[1].Changes)
	}
	reportSymbol(t, reports[1], "Second")
}

func TestScanEmitsCorrelatedTrace(t *testing.T) {
	exporter := tracetest.NewInMemoryExporter()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exporter))
	previous := otel.GetTracerProvider()
	otel.SetTracerProvider(provider)
	t.Cleanup(func() { otel.SetTracerProvider(previous); _ = provider.Shutdown(context.Background()) })
	report, err := fixtureScanner(t, fixtureRepository(t, "package fixture\nfunc Run() {}\n")).Scan(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	assertParseSpan(t, exporter.GetSpans(), report)
}

func assertParseSpan(t *testing.T, spans tracetest.SpanStubs, report Report) {
	t.Helper()
	for _, span := range spans {
		if span.Name != "scan.parse_snapshot" {
			continue
		}
		if span.SpanContext.TraceID().String() != report.TraceID || !span.Parent.IsValid() {
			t.Fatal("report trace is not linked to scan execution")
		}
		return
	}
	t.Fatal("no scan.parse_snapshot span was exported")
}

func fixtureScanner(t *testing.T, root string) *Scanner {
	t.Helper()
	scanner, err := Open(context.Background(), root, source.Options{MaxFileBytes: 1 << 20, MaxTotalBytes: 8 << 20})
	if err != nil {
		t.Fatal(err)
	}
	return scanner
}

func fixtureRepository(t *testing.T, content string) string {
	t.Helper()
	root := t.TempDir()
	writeFixture(t, root, "go.mod", "module example.test/fixture\n\ngo 1.26.0\n")
	writeFixture(t, root, "fixture.go", content)
	fixtureGit(t, root, "init", "--quiet")
	fixtureGit(t, root, "add", "--", ".")
	fixtureGit(t, root, "-c", "user.name=Fixture", "-c", "user.email=fixture@example.invalid", "-c", "commit.gpgSign=false", "commit", "--quiet", "-m", "fixture")
	return root
}

func writeFixture(t *testing.T, root, name, contents string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(root, name), []byte(contents), 0600); err != nil {
		t.Fatal(err)
	}
}

func fixtureGit(t *testing.T, root string, args ...string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	argv := append([]string{"-C", root, "-c", "core.hooksPath=/dev/null"}, args...)
	command := exec.CommandContext(ctx, "git", argv...)
	command.Env = append(os.Environ(), "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=/dev/null")
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("fixture git failed: %v: %s", err, output)
	}
}

func reportSymbol(t *testing.T, report Report, name string) goparser.Symbol {
	t.Helper()
	for _, file := range report.Index.Files {
		for _, symbol := range file.Symbols {
			if symbol.Name == name {
				return symbol
			}
		}
	}
	t.Fatalf("symbol %q was not extracted", name)
	return goparser.Symbol{}
}
