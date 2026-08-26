package golang

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestDocumentationCompilesOwnCommentsInSourceOrder(t *testing.T) {
	source := `package sample
// Send delivers a value.
// value is the payload; false means it was rejected.
func Send(value string) bool {
 // Reject an empty payload before delivery.
 if value == "" { return false }
 _ = "// this is a string, not documentation"
 /* Reuse the caller's connection.
    No additional connection is opened. */
 return true // Delivery was accepted.
}
// Other is unrelated.
func Other() {}
`
	symbol := findSymbol(t, parseFixture(t, source), Function, "Send")
	doc := symbol.Documentation
	if doc == nil || doc.Origin != "indexed_source" || doc.Truncated || len(doc.Comments) != 4 {
		t.Fatalf("unexpected documentation: %+v", doc)
	}
	if !strings.HasPrefix(doc.Summary, symbol.Doc+"\n\nReject an empty payload") || !strings.HasSuffix(doc.Summary, "Delivery was accepted.") {
		t.Fatalf("comments were not compiled in source order: %q", doc.Summary)
	}
	if strings.Contains(doc.Summary, "this is a string") || strings.Contains(doc.Summary, "unrelated") {
		t.Fatal("non-comment or another declaration leaked into documentation")
	}
	assertDocumentationSpans(t, source, doc)
}

func assertDocumentationSpans(t *testing.T, source string, doc *Documentation) {
	t.Helper()
	for _, comment := range doc.Comments {
		span := comment.Span
		if span == nil || !strings.HasPrefix(source[span.Start.Offset:span.End.Offset], "/") {
			t.Fatal("comment locator does not point to captured source")
		}
	}
}

func TestDocumentationKeepsNestedClosureCommentsSeparate(t *testing.T) {
	source := `package sample
func Outer() {
 // Prepare the callback.
 callback := func() {
  // Retry only this callback.
  _ = func() { /* Inner closure only. */ }
 }
 _ = callback // Keep it alive.
}
`
	result := parseFixture(t, source)
	outer := findSymbol(t, result, Function, "Outer")
	if outer.Documentation.Summary != "Prepare the callback.\n\nKeep it alive." {
		t.Fatal(outer.Documentation.Summary)
	}
	closures := symbolsOfKind(result, Closure)
	if len(closures) != 2 || closures[0].Documentation.Summary != "Retry only this callback." || closures[1].Documentation.Summary != "Inner closure only." {
		t.Fatal("closure comments have the wrong owner")
	}
}

func TestDocumentationUsesFullCapturedFileBeyondSourceExcerpt(t *testing.T) {
	source := "package sample\nfunc Large() {\n_ = `" + strings.Repeat("x", maxSymbolSourceBytes+100) + "`\n// Late evidence matters.\n}\n"
	symbol := findSymbol(t, parseFixture(t, source), Function, "Large")
	if !symbol.SourceTruncated || symbol.Documentation.Truncated || symbol.Documentation.Summary != "Late evidence matters." {
		t.Fatal("documentation incorrectly depended on the bounded implementation excerpt")
	}
}

func TestDocumentationBoundsUnicodeAndCommentCount(t *testing.T) {
	large := "package sample\nfunc Large() {\n// " + strings.Repeat("界", 5000) + "\n}"
	symbol := findSymbol(t, parseFixture(t, large), Function, "Large")
	if !symbol.Documentation.Truncated || len(symbol.Documentation.Summary) > maxDocumentationBytes || !utf8.ValidString(symbol.Documentation.Summary) {
		t.Fatal("documentation exceeded its UTF-8 byte bound")
	}
	many := "package sample\nfunc Many() {\n" + strings.Repeat("// comment\n_ = 1\n", 100) + "}"
	symbol = findSymbol(t, parseFixture(t, many), Function, "Many")
	if !symbol.Documentation.Truncated || len(symbol.Documentation.Comments) != maxDocumentationComments {
		t.Fatal("comment record bound was not enforced")
	}
}

func TestDocumentationDoesNotInventPurposeForUncommentedFunction(t *testing.T) {
	symbol := findSymbol(t, parseFixture(t, "package sample\nfunc DeleteEverything() {}"), Function, "DeleteEverything")
	if symbol.Documentation.Summary != "" || symbol.Documentation.Truncated || len(symbol.Documentation.Comments) != 0 {
		t.Fatal("uncommented implementation acquired an invented summary")
	}
}

func TestLegacyDocumentationUsesOnlySavedEvidenceAndPhysicalPositions(t *testing.T) {
	result := parseFixture(t, "package sample\n// Run documents value.\nfunc Run() {\n//line imaginary.go:999\n// Captured body.\n_ = func() { /* Callback only. */ }\n}")
	for _, symbol := range result.Files[0].Symbols {
		assertLegacyDocumentation(t, symbol)
	}
}

func assertLegacyDocumentation(t *testing.T, symbol Symbol) {
	t.Helper()
	want := symbol.Documentation
	symbol.Documentation = nil
	got := DocumentationFor(symbol)
	if got.Summary != want.Summary || got.Origin != "captured_excerpt" || got.Truncated {
		t.Fatalf("legacy summary differs: %+v", got)
	}
	for i, comment := range got.Comments {
		if comment.Kind == "body" && *comment.Span != *want.Comments[i].Span {
			t.Fatal("legacy physical coordinates changed")
		}
	}
}

func TestLegacyDocumentationMarksTruncatedCapture(t *testing.T) {
	symbol := Symbol{Kind: Function, Doc: "Original documentation.", Source: "func Broken() {\n// Captured before truncation.\n", SourceTruncated: true}
	doc := DocumentationFor(symbol)
	if !doc.Truncated || !strings.Contains(doc.Summary, "Captured before truncation.") {
		t.Fatalf("legacy incomplete evidence hidden: %+v", doc)
	}
}
