package golang

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"unicode/utf8"
)

func analyzeFixture(t *testing.T, sources map[string]string) Result {
	t.Helper()
	files := make([]SourceFile, 0, len(sources))
	for path, content := range sources {
		files = append(files, SourceFile{Path: path, Content: []byte(content)})
	}
	result, err := Parse(context.Background(), files)
	if err != nil {
		t.Fatal(err)
	}
	if err := AnalyzeCalls(context.Background(), files, &result); err != nil {
		t.Fatal(err)
	}
	return result
}

func assertCall(t *testing.T, result Result, caller, target, resolution string) Call {
	t.Helper()
	for _, call := range result.Calls {
		if call.CallerName == caller && call.TargetName == target && call.Resolution == resolution {
			return call
		}
	}
	t.Fatalf("missing %s -> %s (%s): %+v; diagnostics=%+v", caller, target, resolution, result.Calls, result.CallAnalysis.Diagnostics)
	return Call{}
}

func TestCallsDirectRecursionAndSamePackageFiles(t *testing.T) {
	result := analyzeFixture(t, map[string]string{
		"a.go": "package sample\nfunc A() { B(); A() }\n",
		"b.go": "package sample\nfunc B() {}\n",
	})
	call := assertCall(t, result, "A", "B", "resolved")
	if call.TargetID != findSymbol(t, result, Function, "B").ID || call.CallerID != findSymbol(t, result, Function, "A").ID {
		t.Fatal("resolved call did not retain exact syntax symbol IDs")
	}
	assertCall(t, result, "A", "A", "resolved")
	if result.CallAnalysis.Status != "complete" || result.CallAnalysis.Resolved != 2 {
		t.Fatalf("unexpected direct-call analysis: %+v", result.CallAnalysis)
	}
}

func TestCallsLocalModuleImportAliases(t *testing.T) {
	result := analyzeFixture(t, map[string]string{
		"go.mod":     "module example.test/repo\ngo 1.26\n",
		"main.go":    "package main\nimport renamed \"example.test/repo/lib\"\nfunc Run() { renamed.Work() }\n",
		"lib/lib.go": "package distinctname\nfunc Work() {}\n",
	})
	call := assertCall(t, result, "Run", "Work", "resolved")
	if call.TargetID != findSymbol(t, result, Function, "Work").ID || call.Path != "main.go" {
		t.Fatal("cross-package alias did not bind to the captured declaration")
	}
}

func TestCallsConcreteMethodsPromotionAndGenericInstantiation(t *testing.T) {
	result := analyzeFixture(t, map[string]string{"sample.go": `package sample
type Box[T any] struct { Value T }
func (b *Box[T]) Get() T { return b.Value }
type Outer struct { Box[int] }
func Identity[T any](value T) T { return value }
func Use() {
 b := Box[int]{}
 b.Get()
 o := Outer{}
 o.Get()
 Identity[int](1)
 (*Box[int]).Get(&b)
}
`})
	if result.CallAnalysis.Resolved != 4 || result.CallAnalysis.Unresolved != 0 {
		t.Fatalf("concrete/generic calls were not all resolved: %+v %+v", result.Calls, result.CallAnalysis)
	}
	assertCall(t, result, "Use", "Get", "resolved")
	assertCall(t, result, "Use", "Identity", "resolved")
}

func TestCallsDynamicDispatchAndShadowingRemainUnresolved(t *testing.T) {
	result := analyzeFixture(t, map[string]string{"sample.go": `package sample
type Runner interface { Run() }
type Concrete struct{}
func (Concrete) Run() {}
func Target() {}
func Dynamic(r Runner, f func()) {
 r.Run()
 f()
 Target := f
 Target()
}
func Variable() { f := Target; f() }
`})
	assertCall(t, result, "Dynamic", "r.Run", "unresolved")
	assertCall(t, result, "Dynamic", "Target", "unresolved")
	assertCall(t, result, "Variable", "f", "unresolved")
	if result.CallAnalysis.Resolved != 0 || result.CallAnalysis.Unresolved != 4 {
		t.Fatalf("dynamic dispatch invented direct edges: %+v", result.Calls)
	}
}

func TestCallsImmediateClosuresAndInitializerOwnership(t *testing.T) {
	result := analyzeFixture(t, map[string]string{"sample.go": `package sample
func Target() {}
func Outer() { (func() { Target() })() }
var Initialized = func() int { Target(); return 1 }()
`})
	outer := assertCall(t, result, "Outer", "$closure1", "resolved")
	initializer := assertCall(t, result, "Initialized", "$closure1", "resolved")
	if outer.TargetID == initializer.TargetID || initializer.CallerID == "" {
		t.Fatal("closure or initializer symbol ownership collided")
	}
	if result.CallAnalysis.Resolved != 4 {
		t.Fatalf("nested closures should own their body calls: %+v", result.Calls)
	}
}

func TestCallsExcludeConversionsButRetainBuiltins(t *testing.T) {
	result := analyzeFixture(t, map[string]string{"sample.go": `package sample
type Number int
func Use() { _ = Number(1); _ = int(2); _ = len("x"); _ = make([]byte, 2) }
`})
	assertCall(t, result, "Use", "len", "builtin")
	assertCall(t, result, "Use", "make", "builtin")
	if len(result.Calls) != 2 || result.CallAnalysis.Status != "complete" {
		t.Fatalf("conversions or builtins were misclassified: %+v %+v", result.Calls, result.CallAnalysis)
	}
}

func TestCallsTypeErrorsAreLocalToAffectedCalls(t *testing.T) {
	result := analyzeFixture(t, map[string]string{"sample.go": `package sample
func Target(value string) {}
func Wrong() { Target(1); Target("valid") }
func Healthy() { Target("valid") }
`})
	assertCall(t, result, "Wrong", "Target", "unresolved")
	assertCall(t, result, "Healthy", "Target", "resolved")
	if result.CallAnalysis.Resolved != 2 || result.CallAnalysis.Status != "partial" {
		t.Fatalf("type errors must be visible without hiding independently valid bindings: %+v", result.CallAnalysis)
	}
}

func TestCallsDeclarationErrorsPreventAmbiguousResolution(t *testing.T) {
	result := analyzeFixture(t, map[string]string{"sample.go": `package sample
func Duplicate() {}
func Duplicate() {}
func Use() { Duplicate() }
`})
	call := assertCall(t, result, "Use", "Duplicate", "unresolved")
	if call.Reason != "package_declaration_errors" || result.CallAnalysis.Resolved != 0 {
		t.Fatalf("ambiguous declaration produced a proven edge: %+v", result.Calls)
	}
}

func TestCallsUnavailableImportsDoNotHideProvableLocalBindings(t *testing.T) {
	result := analyzeFixture(t, map[string]string{"sample.go": `package sample
import "fmt"
func Local() {}
func Use() { fmt.Println("value"); Local() }
`})
	assertCall(t, result, "Use", "fmt.Println", "unresolved")
	assertCall(t, result, "Use", "Local", "resolved")
	if result.CallAnalysis.Status != "partial" || len(result.CallAnalysis.Diagnostics) == 0 {
		t.Fatal("unavailable dependency declarations must be disclosed")
	}
}

func TestCallsCancellationAndMissingIndex(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	result := Result{}
	if err := AnalyzeCalls(ctx, nil, &result); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation was not propagated: %v", err)
	}
	if err := AnalyzeCalls(context.Background(), nil, nil); err == nil {
		t.Fatal("nil syntax index was accepted")
	}
}

func TestCallsAreDeterministicAndUsePhysicalByteSpans(t *testing.T) {
	source := "package sample\n//line fake.go:900\nfunc Café() { Target() }\nfunc Target() {}\n"
	first := analyzeFixture(t, map[string]string{"sample.go": source})
	second := analyzeFixture(t, map[string]string{"sample.go": source})
	if !reflect.DeepEqual(first.Calls, second.Calls) {
		t.Fatal("identical captured bytes changed call records")
	}
	call := assertCall(t, first, "Café", "Target", "resolved")
	if call.Span.Start.Line != 3 || call.Span.Start.Column != len("func Café() { ")+1 {
		t.Fatalf("call span followed a line directive or counted Unicode characters: %+v", call.Span)
	}
	if source[call.Span.Start.Offset:call.Span.End.Offset] != "Target()" {
		t.Fatalf("call span does not select its source evidence: %+v", call.Span)
	}
}

func TestSymbolSourcesAreBoundedUTF8Prefixes(t *testing.T) {
	body := "func Large() string { return \"" + strings.Repeat("界", maxSymbolSourceBytes) + "\" }"
	result := parseFixture(t, "package sample\n"+body)
	symbol := findSymbol(t, result, Function, "Large")
	if !symbol.SourceTruncated || len(symbol.Source) > maxSymbolSourceBytes || !utf8.ValidString(symbol.Source) {
		t.Fatal("source excerpt exceeded its bound or split a UTF-8 rune")
	}
	if !strings.HasPrefix(body, symbol.Source) {
		t.Fatal("source excerpt was not copied from the captured declaration")
	}
	small := findSymbol(t, parseFixture(t, "package sample\nfunc Small() {}"), Function, "Small")
	if small.Source != "func Small() {}" || small.SourceTruncated {
		t.Fatal("small source declaration did not round trip")
	}
}
