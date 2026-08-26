package golang

import (
	"strings"
	"testing"
)

func TestStructFieldsTagsEmbeddingAndGenerics(t *testing.T) {
	source := `package sample
type Container[K comparable, V any] struct {
 // Names are retained for both fields.
 First, Last string ` + "`json:\"name\"`" + ` // serialized label
 *external.Client
 Item[V]
 Transform func(V) V
}
`
	result := parseFixture(t, source)
	symbol := findSymbol(t, result, Struct, "Container")
	if len(symbol.Fields) != 5 || len(symbol.TypeParameters) != 2 {
		t.Fatalf("missing field/type parameter metadata: %+v", symbol)
	}
	assertFieldTag(t, symbol.Fields[0])
	assertFieldTag(t, symbol.Fields[1])
	assertEmbeddedField(t, symbol.Fields[2], "Client", "*external.Client")
	assertEmbeddedField(t, symbol.Fields[3], "Item", "Item[V]")
	assertParameter(t, symbol.TypeParameters[0], "K", "comparable", 0, 0, false)
	field := findSymbol(t, result, Field, "Transform")
	if field.ParentID != symbol.ID || field.Signature != "Transform func(V) V" {
		t.Fatalf("function-valued field must not become a method: %+v", field)
	}
}

func assertFieldTag(t *testing.T, field FieldInfo) {
	t.Helper()
	if field.Tag != `json:"name"` || field.TagLiteral != "`json:\"name\"`" {
		t.Fatalf("tag not preserved: %+v", field)
	}
	if field.Doc != "Names are retained for both fields." || field.Comment != "serialized label" {
		t.Fatalf("field comments not preserved: %+v", field)
	}
}

func assertEmbeddedField(t *testing.T, field FieldInfo, name, typ string) {
	t.Helper()
	if field.Name != name || field.Type != typ || !field.Embedded {
		t.Fatalf("embedded field mismatch: %+v", field)
	}
}

func TestInterfaceMethodsAndConstraintTerms(t *testing.T) {
	result := parseFixture(t, `package sample
// Reader is a contract.
type Reader[T any] interface {
 // Read retrieves data.
 Read(key T) (T, error)
 external.Closer
 ~int | ~string
}
`)
	contract := findSymbol(t, result, Interface, "Reader")
	method := findSymbol(t, result, InterfaceMethod, "Read")
	if len(contract.Fields) != 2 || contract.Fields[1].Type != "~int | ~string" {
		t.Fatalf("embedded types/constraints missing: %+v", contract)
	}
	if method.ParentID != contract.ID || method.Doc != "Read retrieves data." {
		t.Fatalf("method membership/docs missing: %+v", method)
	}
	if method.Signature != "Read(key T) (T, error)" || len(method.Results) != 2 {
		t.Fatalf("invalid method signature: %+v", method)
	}
}

func TestAliasesAndNamedTypes(t *testing.T) {
	result := parseFixture(t, `package sample
type ID = string
type IDs[T comparable] = map[T]bool
type Count int64
type Callback func(int) error
`)
	alias := findSymbol(t, result, Alias, "ID")
	if alias.Signature != "type ID = string" || alias.Type != "string" {
		t.Fatalf("alias lost its equals sign: %+v", alias)
	}
	generic := findSymbol(t, result, Alias, "IDs")
	assertParameter(t, generic.TypeParameters[0], "T", "comparable", 0, 0, false)
	if findSymbol(t, result, NamedType, "Count").Type != "int64" {
		t.Fatal("named type not preserved")
	}
	if findSymbol(t, result, NamedType, "Callback").Type != "func(int) error" {
		t.Fatal("function type must remain a type rather than a callable declaration")
	}
}

func TestConstantsVariablesAndImports(t *testing.T) {
	result := parseFixture(t, `// Package sample stores values.
package sample
import alias "example.com/dependency"
const (
 // First establishes the type.
 First Mode = iota
 Second
)
var left, right = Pair()
var hidden string
`)
	first := findSymbol(t, result, Constant, "First")
	second := findSymbol(t, result, Constant, "Second")
	if first.Type != "Mode" || second.Type != "Mode" || second.Values[0] != "iota" {
		t.Fatalf("inherited constant syntax missing: %+v %+v", first, second)
	}
	if second.Signature != "const Second" {
		t.Fatalf("signature must preserve actual declaration: %s", second.Signature)
	}
	if len(symbolsOfKind(result, Variable)) != 3 {
		t.Fatal("grouped variables must expand to separate symbols")
	}
	assertFileMetadata(t, result.Files[0])
}

func assertFileMetadata(t *testing.T, file FileBlock) {
	t.Helper()
	if len(file.Imports) != 1 || file.Imports[0].Path != "example.com/dependency" || file.Imports[0].Alias != "alias" {
		t.Fatalf("import metadata missing: %+v", file.Imports)
	}
	if file.Doc != "Package sample stores values." || !strings.Contains(file.Overview, "constant: 2") {
		t.Fatalf("overview/doc missing: %+v", file)
	}
}

func TestClosuresHaveEnclosingParents(t *testing.T) {
	result := parseFixture(t, `package sample
func Outer() {
 run := func(a int) func() int { return func() int { return a } }
 _ = run
}
var Callback = func() {}
`)
	closures := symbolsOfKind(result, Closure)
	if len(closures) != 3 {
		t.Fatalf("expected three closures: %+v", closures)
	}
	if closures[0].ParentID != findSymbol(t, result, Function, "Outer").ID || closures[1].ParentID != closures[0].ID {
		t.Fatalf("nested closure parent links missing: %+v", closures)
	}
	if closures[2].ParentID != findSymbol(t, result, Variable, "Callback").ID || closures[2].Visibility != "local" {
		t.Fatalf("initializer closure parent missing: %+v", closures[2])
	}
}
