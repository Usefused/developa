package postgres

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"
	"testing"
	"unicode/utf8"

	"developa/internal/domain"
)

func TestIntegrationSourceLargeDeclarationChunks(t *testing.T) {
	store, counter := catalogFixture(t)
	declaration := "func Large() { _ = `" + strings.Repeat("界😀", 4000) + "` }"
	report, ids := flowReport(t, "package sample\n//line imaginary.go:900\n"+declaration+"\n", nil, "large-source")
	snapshot := saveReport(t, store, "repo", report)
	symbol := report.Index.Files[0].Symbols[0]
	if !symbol.SourceTruncated || len(symbol.Source) > 8192 {
		t.Fatal("fixture must retain only a bounded declaration preview")
	}
	actual := readAllSource(t, store, counter, snapshot.ID, ids["Large"])
	if actual != declaration {
		t.Fatal("chunked declaration changed or omitted captured bytes")
	}
	assertSourceProvenance(t, store, snapshot.ID, ids["Large"], symbol.SourceID, symbol.ContentHash)
	chunk, err := store.Source(context.Background(), "repo", snapshot.ID, ids["Large"], domain.SourceOptions{})
	if err != nil || chunk.Span != symbol.Span || chunk.Path != report.Index.Files[0].Path {
		t.Fatal("source span or physical path was lost")
	}
}

func assertSourceProvenance(t *testing.T, store *Store, snapshot, symbol, sourceID, hash string) {
	t.Helper()
	chunk, err := store.Source(context.Background(), "repo", snapshot, symbol, domain.SourceOptions{})
	if err != nil || chunk.SourceID != sourceID || chunk.ContentHash != hash {
		t.Fatalf("source provenance changed: %+v %v", chunk, err)
	}
	if chunk.SnapshotID != snapshot || chunk.SymbolID != symbol {
		t.Fatal("source snapshot/symbol identity was lost")
	}
}

func readAllSource(t *testing.T, store *Store, counter *queryCounter, snapshot, symbol string) string {
	t.Helper()
	var result strings.Builder
	for offset := 0; ; {
		counter.Store(0)
		chunk, err := store.Source(context.Background(), "repo", snapshot, symbol, domain.SourceOptions{Offset: offset})
		assertCompleteSourceChunk(t, chunk, counter.Load(), err)
		result.WriteString(chunk.Source)
		if chunk.NextOffset == nil {
			return result.String()
		}
		if *chunk.NextOffset <= offset || *chunk.NextOffset != result.Len() {
			t.Fatal("source continuation must advance by returned UTF-8 bytes")
		}
		offset = *chunk.NextOffset
	}
}

func assertCompleteSourceChunk(t *testing.T, chunk domain.SymbolSource, queries int64, err error) {
	t.Helper()
	if err != nil || queries != 1 || len(chunk.Source) > domain.DefaultSourceLimit {
		t.Fatalf("source chunk failed its query/byte budget: queries=%d err=%v", queries, err)
	}
	if !chunk.Complete || !utf8.ValidString(chunk.Source) || len(chunk.Limitations) != 0 {
		t.Fatal("complete captured source must remain valid UTF-8 without limitations")
	}
}

func TestIntegrationSourceUTF8OffsetsAndEnd(t *testing.T) {
	store, _ := catalogFixture(t)
	declaration := "func Run() { _ = `界😀` }"
	report, ids := flowReport(t, "package sample\n"+declaration, nil, "unicode-source")
	snapshot := saveReport(t, store, "repo", report)
	offset := strings.Index(declaration, "界")
	chunk, err := store.Source(context.Background(), "repo", snapshot.ID, ids["Run"], domain.SourceOptions{Offset: offset, Limit: 4})
	if err != nil || chunk.Source != "界" || chunk.NextOffset == nil || *chunk.NextOffset != offset+3 {
		t.Fatalf("chunk split a Unicode rune: %+v %v", chunk, err)
	}
	assertSourceOffsetsRejected(t, store, snapshot.ID, ids["Run"], []int{offset + 1, offset + 4, len(declaration) + 1, math.MaxInt})
	chunk, err = store.Source(context.Background(), "repo", snapshot.ID, ids["Run"], domain.SourceOptions{Offset: len(declaration)})
	if err != nil || chunk.Source != "" || chunk.NextOffset != nil || !chunk.Complete {
		t.Fatalf("exact declaration end must be an empty completed chunk: %+v %v", chunk, err)
	}
}

func assertSourceOffsetsRejected(t *testing.T, store *Store, snapshot, symbol string, offsets []int) {
	t.Helper()
	for _, offset := range offsets {
		_, err := store.Source(context.Background(), "repo", snapshot, symbol, domain.SourceOptions{Offset: offset})
		if !errors.Is(err, domain.ErrInvalidInput) {
			t.Fatalf("invalid byte offset %d returned %v", offset, err)
		}
	}
}

func TestIntegrationSourceRepositorySnapshotIsolationAndCancellation(t *testing.T) {
	store, _ := catalogFixture(t)
	report, ids := flowReport(t, "package sample\nfunc Run() {}", nil, "source-scope")
	snapshot := saveReport(t, store, "repo", report)
	report.Snapshot.Fingerprint = fingerprint("foreign-source")
	foreign := saveReport(t, store, "other", report)
	for _, scope := range [][3]string{{"other", snapshot.ID, ids["Run"]}, {"repo", foreign.ID, ids["Run"]}, {"repo", snapshot.ID, "absent"}, {"absent", snapshot.ID, ids["Run"]}} {
		_, err := store.Source(context.Background(), scope[0], scope[1], scope[2], domain.SourceOptions{})
		if !errors.Is(err, domain.ErrNotFound) {
			t.Fatalf("source crossed missing repository/snapshot/symbol scope: %v", err)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := store.Source(ctx, "repo", snapshot.ID, ids["Run"], domain.SourceOptions{})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("source read ignored cancellation: %v", err)
	}
}

func TestIntegrationSourceLegacyAvailability(t *testing.T) {
	store, _ := catalogFixture(t)
	source := "package sample\nfunc Small() {}\nfunc Large() { _ = `" + strings.Repeat("x", 9000) + "` }"
	report, ids := flowReport(t, source, nil, "legacy-source")
	snapshot := saveReport(t, store, "repo", report)
	_, err := store.pool.Exec(context.Background(), `UPDATE developa_files SET source=NULL WHERE repository_id='repo' AND snapshot_id=$1`, snapshot.ID)
	if err != nil {
		t.Fatal(err)
	}
	small, err := store.Source(context.Background(), "repo", snapshot.ID, ids["Small"], domain.SourceOptions{})
	if err != nil || small.Source != "func Small() {}" || !small.Complete || len(small.Limitations) != 1 {
		t.Fatalf("legacy complete excerpt must disclose provenance: %+v %v", small, err)
	}
	_, err = store.Source(context.Background(), "repo", snapshot.ID, ids["Large"], domain.SourceOptions{})
	if !errors.Is(err, domain.ErrSourceUnavailable) {
		t.Fatalf("truncated legacy excerpt presented as complete: %v", err)
	}
}

func TestIntegrationSourceQueryBudgetDoesNotGrowWithCatalog(t *testing.T) {
	store, counter := catalogFixture(t)
	for _, count := range []int{1, 10, 100} {
		report := catalogReport(t, count, fmt.Sprint(count))
		snapshot := saveReport(t, store, "repo", report)
		counter.Store(0)
		chunk, err := store.Source(context.Background(), "repo", snapshot.ID, report.Index.Files[0].Symbols[0].ID, domain.SourceOptions{Limit: 4})
		if err != nil || counter.Load() != 1 || chunk.Source != "func" {
			t.Fatalf("source query/row byte budget grew with catalog size %d: queries=%d err=%v", count, counter.Load(), err)
		}
	}
}

func TestSourceInvalidOptionsDoNotQueryDatabase(t *testing.T) {
	store := &Store{}
	for _, options := range []domain.SourceOptions{{Offset: -1}, {Limit: 3}, {Limit: domain.MaxSourceLimit + 1}} {
		_, err := store.Source(context.Background(), "repo", "snapshot", "symbol", options)
		if !errors.Is(err, domain.ErrInvalidInput) {
			t.Fatalf("invalid source options reached database: %v", err)
		}
	}
}

func TestSourceInvalidStoredUTF8IsNotReplaced(t *testing.T) {
	for _, chunk := range [][]byte{{'a', 0xff}, {'a', 0xe7}} {
		_, err := sourceText(chunk, false)
		if !errors.Is(err, domain.ErrSourceUnavailable) {
			t.Fatalf("invalid captured UTF-8 silently replaced: %v", err)
		}
	}
}
