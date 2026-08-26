package golang

import (
	"context"
	"strings"
	"testing"
)

func TestCallsImportAliasShadowingAndFunctionFields(t *testing.T) {
	result := analyzeFixture(t, map[string]string{
		"go.mod":     "module example.test/repo\ngo 1.26\n",
		"lib/lib.go": "package lib\nfunc Work() {}\n",
		"main.go": `package main
import lib "example.test/repo/lib"
func Direct() { lib.Work() }
func Shadow() { lib := struct { Work func() }{}; lib.Work() }
`,
	})
	assertCall(t, result, "Direct", "Work", "resolved")
	call := assertCall(t, result, "Shadow", "lib.Work", "unresolved")
	if call.Reason != "function_value_dispatch" || call.TargetID != "" {
		t.Fatalf("a shadowed import or function field invented a direct target: %+v", call)
	}
}

func TestCallsPromotedInterfacesAndTypeParametersRemainDynamic(t *testing.T) {
	result := analyzeFixture(t, map[string]string{"sample.go": `package sample
type Runner interface { Run() }
type Wrapper struct { Runner }
func Promoted(value Wrapper) { value.Run() }
func Generic[T Runner](value T) { value.Run() }
`})
	for _, caller := range []string{"Promoted", "Generic"} {
		call := assertCall(t, result, caller, "value.Run", "unresolved")
		if call.Reason != "interface_or_type_parameter_dispatch" {
			t.Fatalf("dynamic method selection was not identified: %+v", call)
		}
	}
}

func TestCallsBuildPlatformAndTestVariantsAreExplicitlyExcluded(t *testing.T) {
	cases := []struct{ path, prefix, reason string }{
		{"variant_test.go", "", "test_variant_excluded"},
		{"variant_linux.go", "", "platform_variant_excluded"},
		{"variant.go", "//go:build custom\n\n", "build_constraint_excluded"},
		{"_ignored.go", "", "ignored_go_filename"},
	}
	for _, tc := range cases {
		result := analyzeFixture(t, map[string]string{
			"normal.go": "package sample\nfunc Target() {}\n",
			tc.path:     tc.prefix + "package sample\nfunc Variant() { Target() }\n",
		})
		call := assertCall(t, result, "Variant", "Target", "unresolved")
		if !strings.HasPrefix(call.Reason, tc.reason) || result.CallAnalysis.Status != "partial" {
			t.Fatalf("excluded variant was not disclosed: %+v %+v", call, result.CallAnalysis)
		}
	}
}

func TestCallsDuplicateModulePathsCannotMergePhysicalPackages(t *testing.T) {
	result := analyzeFixture(t, map[string]string{
		"go.mod":         "module example.test/repo\ngo 1.26\n",
		"main.go":        "package repo\nfunc First() { Second() }\n",
		"nested/go.mod":  "module example.test/repo\ngo 1.26\n",
		"nested/main.go": "package repo\nfunc Second() {}\n",
	})
	assertCall(t, result, "First", "Second", "unresolved")
	if result.CallAnalysis.Resolved != 0 {
		t.Fatal("different physical packages with the same module path were merged")
	}
}

func TestCallsNestedModulesNeedExplicitBuildConfiguration(t *testing.T) {
	result := analyzeFixture(t, map[string]string{
		"go.mod":         "module example.test/repo\ngo 1.26\n",
		"main.go":        "package repo\nimport other \"example.test/other\"\nfunc First() { other.Second() }\n",
		"nested/go.mod":  "module example.test/other\ngo 1.26\n",
		"nested/main.go": "package other\nfunc Second() {}\n",
	})
	assertCall(t, result, "First", "other.Second", "unresolved")
	if result.CallAnalysis.Resolved != 0 || result.CallAnalysis.Status != "partial" {
		t.Fatal("nested module silently overrode dependency/workspace selection")
	}
}

func TestCallsLocalImportCyclesCannotProduceResolvedCycle(t *testing.T) {
	result := analyzeFixture(t, map[string]string{
		"go.mod": "module example.test/repo\ngo 1.26\n",
		"a/a.go": "package a\nimport \"example.test/repo/b\"\nfunc A() { b.B() }\n",
		"b/b.go": "package b\nimport \"example.test/repo/a\"\nfunc B() { a.A() }\n",
	})
	if result.CallAnalysis.Resolved != 0 || result.CallAnalysis.Status != "partial" {
		t.Fatalf("import cycle yielded proven call targets: %+v %+v", result.Calls, result.CallAnalysis)
	}
}

func TestCallsUnavailableDotImportCannotConfirmNamespaceBindings(t *testing.T) {
	result := analyzeFixture(t, map[string]string{"sample.go": `package sample
import . "external.example/unknown"
func Target() {}
func Use() { Target() }
`})
	call := assertCall(t, result, "Use", "Target", "unresolved")
	if call.Reason != "package_declaration_errors" {
		t.Fatal("unknown dot-imported namespace was treated as fully known")
	}
}

func TestCallsCapturedBytesMustMatchSyntaxIndex(t *testing.T) {
	result := parseFixture(t, "package sample\nfunc A() {}\n")
	files := []SourceFile{{Path: "sample.go", Content: []byte("package sample\nfunc B() {}\n")}}
	if err := AnalyzeCalls(context.Background(), files, &result); err != nil {
		t.Fatal(err)
	}
	if result.CallAnalysis.Status != "partial" || len(result.CallAnalysis.Diagnostics) == 0 || len(result.Calls) != 0 {
		t.Fatal("call analysis mixed captured bytes with a different syntax snapshot")
	}
}

func TestCallsMalformedModuleIsExplicitlyPartial(t *testing.T) {
	result := analyzeFixture(t, map[string]string{
		"go.mod":    "module (invalid module\n",
		"sample.go": "package sample\nfunc A() {}\n",
	})
	if result.CallAnalysis.Status != "partial" || len(result.CallAnalysis.Diagnostics) == 0 {
		t.Fatal("malformed module metadata was concealed")
	}
}

func TestCallsImportedGenericMethodsAndVisibility(t *testing.T) {
	result := analyzeFixture(t, map[string]string{
		"go.mod": "module example.test/repo\ngo 1.26\n",
		"lib/lib.go": `package lib
type Box[T any] struct{}
func (Box[T]) Read() {}
func hidden() {}
`,
		"main.go": `package main
import "example.test/repo/lib"
func Use() { value := lib.Box[int]{}; value.Read(); lib.hidden() }
`,
	})
	assertCall(t, result, "Use", "Read", "resolved")
	assertCall(t, result, "Use", "lib.hidden", "unresolved")
	if result.CallAnalysis.Resolved != 1 || result.CallAnalysis.Unresolved != 1 {
		t.Fatalf("cross-package method/visibility resolution is wrong: %+v", result.Calls)
	}
}

func TestCallsExternalTypeConversionsStayExplicitlyAmbiguous(t *testing.T) {
	result := analyzeFixture(t, map[string]string{"sample.go": `package sample
import "external.example/types"
func Use() { _ = types.Value(1) }
`})
	call := assertCall(t, result, "Use", "types.Value", "unresolved")
	if !strings.Contains(call.Reason, "call_or_conversion") {
		t.Fatal("unknown external expression was presented as a definite function call")
	}
}

func TestCallsInvalidNestedModuleCannotInheritParentNamespace(t *testing.T) {
	result := analyzeFixture(t, map[string]string{
		"go.mod":        "module example.test/repo\ngo 1.26\n",
		"main.go":       "package repo\nimport nested \"example.test/repo/nested\"\nfunc Use() { nested.Work() }\n",
		"nested/go.mod": "module (invalid\n",
		"nested/lib.go": "package nested\nfunc Work() {}\n",
	})
	assertCall(t, result, "Use", "nested.Work", "unresolved")
	if result.CallAnalysis.Resolved != 0 {
		t.Fatal("invalid nested module was merged into its parent module")
	}
}

func TestCallsUnknownImportedSignatureTypesPreserveLocalBindings(t *testing.T) {
	result := analyzeFixture(t, map[string]string{"sample.go": `package sample
import "external.example/transport/v5"
type Server struct { Transport transport.Server }
func Helper(value transport.Request) {}
func (server *Server) Handle(value transport.Request) { Helper(value) }
func Entry(server *Server, value transport.Request) { server.Handle(value) }
`})
	assertCall(t, result, "Handle", "Helper", "resolved")
	assertCall(t, result, "Entry", "Handle", "resolved")
	if result.CallAnalysis.Resolved != 2 || result.CallAnalysis.Status != "partial" {
		t.Fatalf("unknown imported types erased provable local bindings: %+v", result.CallAnalysis)
	}
}

func TestCallsUnknownImportedMethodSetsRemainUnresolved(t *testing.T) {
	result := analyzeFixture(t, map[string]string{"sample.go": `package sample
import "external.example/transport/v5"
type Server struct { transport.Server }
func Entry(server *Server) { server.Handle() }
`})
	assertCall(t, result, "Entry", "server.Handle", "unresolved")
	if result.CallAnalysis.Resolved != 0 {
		t.Fatal("unknown imported receiver method set produced a target")
	}
}
