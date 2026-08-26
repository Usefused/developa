package golang

import (
	"context"
	"reflect"
	"strings"
	"testing"
)

func TestLogicalIdentityStableAfterBodyEdit(t *testing.T) {
	before := parseFixture(t, "package sample\nfunc Run() int { return 1 }\nfunc End() {}\n")
	after := parseFixture(t, "package sample\nfunc Run() int {\n return 200\n}\nfunc End() {}\n")
	for _, name := range []string{"Run", "End"} {
		t.Run(name, func(t *testing.T) {
			old := findSymbol(t, before, Function, name)
			updated := findSymbol(t, after, Function, name)
			if old.ID != updated.ID || old.SourceID == updated.SourceID {
				t.Fatalf("logical identity/occurrence invariant failed: %+v %+v", old, updated)
			}
		})
	}
	if findSymbol(t, before, Function, "Run").ContentHash == findSymbol(t, after, Function, "Run").ContentHash {
		t.Fatal("changed body must change declaration content hash")
	}
}

func TestIdentitySeparatesPathsPackagesAndDuplicateNames(t *testing.T) {
	files := []SourceFile{
		{Path: "one.go", Content: []byte("package a\nfunc init() {}\nfunc init() {}")},
		{Path: "two.go", Content: []byte("package a\nfunc init() {}")},
		{Path: "other/one.go", Content: []byte("package b\nfunc init() {}")},
	}
	result, err := Parse(context.Background(), files)
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for _, symbol := range symbolsOfKind(result, Function) {
		if seen[symbol.ID] {
			t.Fatalf("colliding logical ID: %+v", symbol)
		}
		seen[symbol.ID] = true
	}
	if len(seen) != 4 {
		t.Fatalf("missing init declarations: %d", len(seen))
	}
}

func TestInputOrderingDoesNotChangeResult(t *testing.T) {
	a := SourceFile{Path: "a.go", Content: []byte("package sample\nfunc A() {}")}
	b := SourceFile{Path: "b.go", Content: []byte("package sample\nfunc B() {}")}
	first, err := Parse(context.Background(), []SourceFile{a, b})
	if err != nil {
		t.Fatal(err)
	}
	second, err := Parse(context.Background(), []SourceFile{b, a})
	if err != nil || !reflect.DeepEqual(first, second) {
		t.Fatalf("nondeterministic extraction: %v", err)
	}
}

func TestUnicodeByteSpansAndPhysicalLineDirectives(t *testing.T) {
	source := "package sample\n//line imaginary.go:900\nfunc Café(名 string) string { return 名 }\n"
	result := parseFixture(t, source)
	symbol := findSymbol(t, result, Function, "Café")
	span := symbol.Span
	if span.Start.Line != 3 || span.Start.Column != 1 || span.End.Line != 3 {
		t.Fatalf("editor links must address physical source: %+v", span)
	}
	declaration := "func Café(名 string) string { return 名 }"
	if source[span.Start.Offset:span.End.Offset] != declaration || span.End.Column != len(declaration)+1 {
		t.Fatalf("span must use exclusive UTF-8 byte offsets/columns: %+v", span)
	}
	parameter := symbol.Parameters[0].Span
	if parameter.Start.Column != len("func Café(")+1 || source[parameter.Start.Offset:parameter.End.Offset] != "名 string" {
		t.Fatalf("parameter byte span incorrect: %+v", parameter)
	}
}

func TestSyntaxDiagnosticsIgnoreLineDirectives(t *testing.T) {
	source := "package sample\n//line fake.go:800\nfunc Broken( {\n"
	result, err := Parse(context.Background(), []SourceFile{{Path: "sample.go", Content: []byte(source)}})
	if err != nil || len(result.Diagnostics) == 0 {
		t.Fatalf("expected parse diagnostic: %+v %v", result, err)
	}
	diagnostic := result.Diagnostics[0]
	if diagnostic.Path != "sample.go" || diagnostic.Position.Line != 3 {
		t.Fatalf("diagnostic must point to physical source: %+v", diagnostic)
	}
	lineStart := strings.Index(source, "func Broken")
	if diagnostic.Position.Offset != lineStart+diagnostic.Position.Column-1 {
		t.Fatalf("incorrect error offset: %+v", diagnostic)
	}
}
