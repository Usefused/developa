package golang

import (
	"reflect"
	"strings"
	"testing"
)

func implementationRows(result Result, iface, receiver string) []Implementation {
	var rows []Implementation
	for _, row := range result.Implementations {
		if row.Interface.Name == iface && row.Receiver.Name == receiver {
			rows = append(rows, row)
		}
	}
	return rows
}

func requireImplementationRows(t *testing.T, result Result, iface, receiver string, count int, pointer bool, evidence string) []Implementation {
	t.Helper()
	rows := implementationRows(result, iface, receiver)
	if len(rows) != count {
		t.Fatalf("%s implemented by %s: got %d rows, want %d; rows=%+v; diagnostics=%+v", iface, receiver, len(rows), count, result.Implementations, result.CallAnalysis.Diagnostics)
	}
	for _, row := range rows {
		if row.Pointer != pointer || row.Evidence != evidence {
			t.Fatalf("unexpected evidence/pointer semantics: %+v", row)
		}
	}
	return rows
}

func TestImplementationsRequireEntireInterfaceMethodSet(t *testing.T) {
	result := analyzeFixture(t, map[string]string{"sample.go": `package sample
type Reader interface { Read(int) (string, error) }
type Store interface { Reader; Close() }
type Value struct{}
func (Value) Read(int) (string, error) { return "", nil }
func (Value) Close() {}
type Pointer struct{}
func (*Pointer) Read(int) (string, error) { return "", nil }
func (*Pointer) Close() {}
type Promoted struct { *Pointer }
type Missing struct{}
func (Missing) Read(int) (string, error) { return "", nil }
type Wrapped struct { Store }
type Alias = Value
func Use(store Store) { store.Read(1) }
`})
	requireImplementationRows(t, result, "Store", "Value", 2, false, methodSetEvidence)
	requireImplementationRows(t, result, "Store", "Pointer", 2, true, methodSetEvidence)
	rows := requireImplementationRows(t, result, "Store", "Promoted", 2, false, methodSetEvidence)
	for _, receiver := range []string{"Missing", "Wrapped", "Alias"} {
		requireImplementationRows(t, result, "Store", receiver, 0, false, "")
	}
	if rows[0].Target.SymbolID == rows[0].Receiver.SymbolID || result.ImplementationAnalysis.Status != "complete" {
		t.Fatalf("promoted declaration or completeness is incorrect: %+v %+v", rows, result.ImplementationAnalysis)
	}
	call := assertCall(t, result, "Use", "store.Read", "unresolved")
	if call.TargetID != "" || call.Target != nil || call.Interface.Name != "Store" || call.InterfaceMethod.Name != "Read" {
		t.Fatalf("interface call lost declaration links or invented runtime resolution: %+v", call)
	}
}

func TestImplementationsRejectKnownSignatureMismatches(t *testing.T) {
	cases := []string{
		"Run(string) string",
		"Run(int) int",
		"Run(...int) string",
		"Run(int, int) string",
	}
	for _, signature := range cases {
		t.Run(signature, func(t *testing.T) {
			result := analyzeFixture(t, map[string]string{"sample.go": "package sample\ntype Runner interface { Run(int) string }\ntype Wrong struct{}\nfunc (Wrong) " + signature + " { panic(\"not executed\") }\n"})
			requireImplementationRows(t, result, "Runner", "Wrong", 0, false, "")
		})
	}
}

func TestImplementationsImportedAliasesAndUnexportedMethods(t *testing.T) {
	result := analyzeFixture(t, map[string]string{
		"go.mod": "module example.test/repo\ngo 1.26\n",
		"contract/api.go": `package contract
type Number = int
type Runner interface { Run(Number) string }
type Private interface { hidden() }
`,
		"worker/worker.go": `package worker
import api "example.test/repo/contract"
type Worker struct{}
func (*Worker) Run(int) string { return "" }
func (*Worker) hidden() {}
func Use(value api.Runner) { value.Run(1) }
`,
	})
	rows := requireImplementationRows(t, result, "Runner", "Worker", 1, true, methodSetEvidence)
	requireImplementationRows(t, result, "Private", "Worker", 0, false, "")
	if rows[0].Method.Path != "contract/api.go" || rows[0].Target.Path != "worker/worker.go" {
		t.Fatal("cross-package implementation locations were not preserved")
	}
}

func TestImplementationsMissingImportsProduceOnlyConditionalCandidates(t *testing.T) {
	result := analyzeFixture(t, map[string]string{
		"go.mod": "module example.test/repo\ngo 1.26\n",
		"contract/api.go": `package contract
import "context"
type Record struct{}
type WorkspaceStore interface {
 SaveWorkspaces(context.Context, []Record) error
 SavedWorkspaces(context.Context) ([]Record, error)
}
`,
		"worker/worker.go": `package worker
import (
 ctx "context"
 other "other.example/context"
 api "example.test/repo/contract"
)
type Store struct{}
func (*Store) SaveWorkspaces(ctx.Context, []api.Record) error { return nil }
func (*Store) SavedWorkspaces(ctx.Context) ([]api.Record, error) { return nil, nil }
type Wrong struct{}
func (*Wrong) SaveWorkspaces(other.Context, []api.Record) error { return nil }
func (*Wrong) SavedWorkspaces(other.Context) ([]api.Record, error) { return nil, nil }
type Incomplete struct{}
func (*Incomplete) SaveWorkspaces(ctx.Context, []api.Record) error { return nil }
func Use(value api.WorkspaceStore, context ctx.Context) { value.SaveWorkspaces(context, nil) }
`,
	})
	requireImplementationRows(t, result, "WorkspaceStore", "Store", 2, true, conditionalSignatureEvidence)
	requireImplementationRows(t, result, "WorkspaceStore", "Wrong", 0, false, "")
	requireImplementationRows(t, result, "WorkspaceStore", "Incomplete", 0, false, "")
	if result.ImplementationAnalysis.Status != "partial" || len(result.CallAnalysis.Diagnostics) == 0 {
		t.Fatal("unavailable imported declarations were concealed")
	}
	call := assertCall(t, result, "Use", "value.SaveWorkspaces", "unresolved")
	if call.Interface == nil || call.InterfaceMethod == nil || call.Target != nil {
		t.Fatalf("missing imported argument types lost the selected interface declaration: %+v", call)
	}
}

func TestImplementationsIncompleteSignaturesStillRejectKnownDifferences(t *testing.T) {
	cases := []string{
		"Run(context.Context, string) string",
		"Run(context.Context, int) int",
		"Run(context.Context, ...int) string",
		"Run(context.Context, []int) string",
	}
	for _, signature := range cases {
		t.Run(signature, func(t *testing.T) {
			result := analyzeFixture(t, map[string]string{"sample.go": "package sample\nimport \"context\"\ntype Runner interface { Run(context.Context, int) string }\ntype Wrong struct{}\nfunc (Wrong) " + signature + " { panic(\"not executed\") }\n"})
			requireImplementationRows(t, result, "Runner", "Wrong", 0, false, "")
		})
	}
}

func TestImplementationsDoNotTrustUnknownEmbeddingsOrGenericDeclarations(t *testing.T) {
	result := analyzeFixture(t, map[string]string{"sample.go": `package sample
import "unknown.example/types"
type Runner interface { Run() }
type Incomplete interface { types.Interface; Run() }
type Worker struct{}
func (Worker) Run() {}
type Unknown struct { types.External; Worker }
type Generic[T any] struct{}
func (Generic[T]) Run() {}
type Constraint interface { ~int; Run() }
`})
	requireImplementationRows(t, result, "Runner", "Worker", 1, false, methodSetEvidence)
	requireImplementationRows(t, result, "Runner", "Unknown", 0, false, "")
	requireImplementationRows(t, result, "Runner", "Generic", 0, false, "")
	requireImplementationRows(t, result, "Incomplete", "Worker", 0, false, "")
	requireImplementationRows(t, result, "Constraint", "Worker", 0, false, "")
	if result.ImplementationAnalysis.Status != "partial" {
		t.Fatal("unknown embeddings and generic coverage did not leave analysis partial")
	}
}

func TestImplementationAndCallReferencesUsePhysicalDeclarationSpans(t *testing.T) {
	source := `package sample
//line fake.go:900
type Café interface { Run(int) string }
type Worker struct{}
func (Worker) Run(int) string { return "" }
func Direct(value Worker) { value.Run(1) }
func Generic[T Café](value T) { value.Run(1) }
func Invalid(value Café) { value.Run("wrong") }
`
	result := analyzeFixture(t, map[string]string{"sample.go": source})
	rows := requireImplementationRows(t, result, "Café", "Worker", 1, false, methodSetEvidence)
	for _, ref := range []SymbolReference{rows[0].Interface, rows[0].Method, rows[0].Receiver, rows[0].Target} {
		assertPhysicalReference(t, source, ref)
	}
	call := assertCall(t, result, "Direct", "Run", "resolved")
	if call.Target == nil || call.Target.SymbolID != rows[0].Target.SymbolID || call.Target.Span != rows[0].Target.Span {
		t.Fatalf("resolved call omitted navigable target location: %+v", call)
	}
	for _, caller := range []string{"Generic", "Invalid"} {
		call := assertCall(t, result, caller, "value.Run", "unresolved")
		if call.Interface == nil || call.Interface.SymbolID != rows[0].Interface.SymbolID || call.InterfaceMethod == nil {
			t.Fatalf("dynamic/error call omitted interface links: %+v", call)
		}
	}
}

func assertPhysicalReference(t *testing.T, source string, ref SymbolReference) {
	t.Helper()
	if ref.Path != "sample.go" || ref.Span.Start.Line >= 900 || !strings.Contains(source[ref.Span.Start.Offset:ref.Span.End.Offset], ref.Name) {
		t.Fatalf("reference does not select its physical declaration: %+v", ref)
	}
}

func TestImplementationsAreDeterministicAndRespectExcludedFiles(t *testing.T) {
	sources := map[string]string{
		"api.go":          "package sample\ntype Runner interface { Run() }\n",
		"worker.go":       "package sample\ntype Worker struct{}\nfunc (Worker) Run() {}\n",
		"variant_test.go": "package sample\ntype Excluded struct{}\nfunc (Excluded) Run() {}\n",
	}
	first, second := analyzeFixture(t, sources), analyzeFixture(t, sources)
	if !reflect.DeepEqual(first.Implementations, second.Implementations) || !reflect.DeepEqual(first.ImplementationAnalysis, second.ImplementationAnalysis) {
		t.Fatal("identical captured bytes changed implementation results")
	}
	requireImplementationRows(t, first, "Runner", "Worker", 1, false, methodSetEvidence)
	requireImplementationRows(t, first, "Runner", "Excluded", 0, false, "")
	if first.ImplementationAnalysis.Status != "partial" {
		t.Fatal("excluded declarations were presented as complete candidate coverage")
	}
}

func TestImplementationsRejectInvalidDeclarationShapes(t *testing.T) {
	cases := []struct{ iface, method string }{
		{"Run(int); Run(string)", "Run(int)"},
		{"Run([-1]int)", "Run([-1]int)"},
		{"Run(context.Context, [-1]int)", "Run(context.Context, [-1]int)"},
	}
	for _, tc := range cases {
		t.Run(tc.iface, func(t *testing.T) {
			result := analyzeFixture(t, map[string]string{"sample.go": "package sample\nimport \"context\"\ntype Runner interface { " + tc.iface + " }\ntype Worker struct{}\nfunc (Worker) " + tc.method + " {}\n"})
			requireImplementationRows(t, result, "Runner", "Worker", 0, false, "")
			if result.ImplementationAnalysis.Status != "partial" {
				t.Fatal("invalid declaration produced complete analysis")
			}
		})
	}
}
