package golang

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
)

func parseFixture(t *testing.T, source string) Result {
	t.Helper()
	result, err := Parse(context.Background(), []SourceFile{{Path: "sample.go", Content: []byte(source)}})
	if err != nil {
		t.Fatal(err)
	}
	if result.Completeness != Complete {
		t.Fatalf("unexpected incomplete parse: %+v", result.Diagnostics)
	}
	return result
}

func findSymbol(t *testing.T, result Result, kind Kind, name string) Symbol {
	t.Helper()
	for _, file := range result.Files {
		for _, symbol := range file.Symbols {
			if symbol.Kind == kind && symbol.Name == name {
				return symbol
			}
		}
	}
	t.Fatalf("missing %s %q", kind, name)
	return Symbol{}
}

func TestGroupedVariadicParametersAndNamedResults(t *testing.T) {
	result := parseFixture(t, `package sample
// Join combines inputs.
func Join[T ~string](a, b T, rest ...T) (value T, err error) { return a, nil }
`)
	symbol := findSymbol(t, result, Function, "Join")
	assertParameter(t, symbol.Parameters[0], "a", "T", 0, 0, false)
	assertParameter(t, symbol.Parameters[1], "b", "T", 1, 0, false)
	assertParameter(t, symbol.Parameters[2], "rest", "T", 2, 1, true)
	assertParameter(t, symbol.Results[0], "value", "T", 0, 0, false)
	assertParameter(t, symbol.Results[1], "err", "error", 1, 1, false)
	assertParameter(t, symbol.TypeParameters[0], "T", "~string", 0, 0, false)
	if symbol.Doc != "Join combines inputs." || symbol.Visibility != "exported" {
		t.Fatalf("missing documentation/visibility: %+v", symbol)
	}
	if strings.Contains(symbol.Signature, "return") {
		t.Fatalf("body included in signature: %s", symbol.Signature)
	}
}

func assertParameter(t *testing.T, got Parameter, name, typ string, position, group int, variadic bool) {
	t.Helper()
	expected := Parameter{Name: name, Type: typ, Position: position, Group: group, Variadic: variadic, Span: got.Span}
	if !reflect.DeepEqual(got, expected) {
		t.Fatalf("parameter: got %+v, want %+v", got, expected)
	}
}

func TestUnnamedParametersAndResults(t *testing.T) {
	result := parseFixture(t, `package sample
func Handle(string, ...int) (string, error) { return "", nil }
func Ready() bool { return true }
`)
	symbol := findSymbol(t, result, Function, "Handle")
	assertParameter(t, symbol.Parameters[0], "", "string", 0, 0, false)
	assertParameter(t, symbol.Parameters[1], "", "int", 1, 1, true)
	assertParameter(t, symbol.Results[0], "", "string", 0, 0, false)
	assertParameter(t, symbol.Results[1], "", "error", 1, 1, false)
	assertParameter(t, findSymbol(t, result, Function, "Ready").Results[0], "", "bool", 0, 0, false)
}

func TestMethodsAndIdentity(t *testing.T) {
	result := parseFixture(t, `package sample
type A[T any] struct{}
type B struct{}
func (a *A[T]) Fetch(key T) T { return key }
func (B) Fetch(key int) int { return key }
`)
	methods := symbolsOfKind(result, Method)
	if len(methods) != 2 || methods[0].ID == methods[1].ID {
		t.Fatalf("receiver identities collide: %+v", methods)
	}
	if methods[0].Receiver != "*A[T]" || methods[0].ReceiverName != "a" {
		t.Fatalf("lost pointer/generic receiver: %+v", methods[0])
	}
	if methods[1].Receiver != "B" || methods[1].ReceiverName != "" {
		t.Fatalf("lost unnamed receiver: %+v", methods[1])
	}
}

func symbolsOfKind(result Result, kind Kind) []Symbol {
	var symbols []Symbol
	for _, file := range result.Files {
		for _, symbol := range file.Symbols {
			if symbol.Kind == kind {
				symbols = append(symbols, symbol)
			}
		}
	}
	return symbols
}

func TestPartialSyntaxAndNonGoInventory(t *testing.T) {
	files := []SourceFile{
		{Path: "readme.md", Content: []byte("not Go")},
		{Path: "good.go", Content: []byte("package sample\nfunc Good() {}")},
		{Path: "bad.go", Content: []byte("package sample\nfunc Bad( {")},
	}
	result, err := Parse(context.Background(), files)
	if err != nil || result.Completeness != Partial {
		t.Fatalf("syntax error should be a partial result: %+v, %v", result, err)
	}
	if len(result.Files) != 2 || len(result.Diagnostics) == 0 || len(result.Skipped) != 1 {
		t.Fatalf("missing inventory/diagnostics: %+v", result)
	}
	findSymbol(t, result, Function, "Good")
}

func TestCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	result, err := Parse(ctx, []SourceFile{{Path: "sample.go", Content: []byte("package sample")}})
	if !errors.Is(err, context.Canceled) || result.Completeness != Partial {
		t.Fatalf("expected canceled partial parse: %+v, %v", result, err)
	}
}

func TestDuplicateNormalizedPathIsPartial(t *testing.T) {
	files := []SourceFile{{Path: "./sample.go", Content: []byte("package a")}, {Path: "sample.go", Content: []byte("package b")}}
	result, err := Parse(context.Background(), files)
	if err != nil || result.Completeness != Partial || len(result.Files) != 1 {
		t.Fatalf("duplicate paths must not silently overwrite: %+v, %v", result, err)
	}
	if result.Diagnostics[0].Code != "duplicate_path" {
		t.Fatalf("unexpected diagnostic: %+v", result.Diagnostics)
	}
}

func TestCapturedFileSourcePreservesFullBytesOutsideJSON(t *testing.T) {
	content := []byte("package sample\r\n//line imaginary.go:900\r\nfunc Large() { _ = `" + strings.Repeat("界", 4000) + "` }\r\n")
	result, err := Parse(context.Background(), []SourceFile{{Path: "physical.go", Content: content}})
	if err != nil {
		t.Fatal(err)
	}
	file := result.Files[0]
	if !bytes.Equal(file.Source, content) || file.Path != "physical.go" {
		t.Fatal("captured file source changed bytes or physical path")
	}
	symbol := file.Symbols[0]
	if !symbol.SourceTruncated || len(symbol.Source) > 8192 || symbol.Span.Start.Line != 3 {
		t.Fatal("full source changed the preview budget or physical positions")
	}
	content[0] = '!'
	if file.Source[0] != 'p' {
		t.Fatal("caller mutation changed retained source")
	}
	assertCapturedSourceNotSerialized(t, file)
}

func assertCapturedSourceNotSerialized(t *testing.T, file FileBlock) {
	t.Helper()
	encoded, err := json.Marshal(file)
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(encoded, &fields); err != nil {
		t.Fatal(err)
	}
	if _, ok := fields["source"]; ok {
		t.Fatal("full captured file leaked into the parser JSON contract")
	}
}
