package golang

import (
	"context"
	"errors"
	"go/ast"
	"go/token"
	"go/types"
	"sort"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
)

type callAnalyzer struct {
	ctx          context.Context
	result       *Result
	fset         *token.FileSet
	files        []*callFile
	byPath       map[string]*callFile
	packages     []*callPackage
	imports      map[string]*callPackage
	functions    map[*types.Func]callTarget
	declarations map[types.Object]callTarget
	signatures   map[*types.Func]methodSyntax
	types        []declaredType
	modules      []capturedModule
}

type callFile struct {
	source    SourceFile
	ast       *ast.File
	block     *FileBlock
	pkg       *callPackage
	exclusion string
	symbols   map[int][]*Symbol
	errors    []Diagnostic
}

type callPackage struct {
	path      string
	goVersion string
	module    string
	files     []*callFile
	info      *types.Info
	typed     *types.Package
	state     string
	invalid   bool
}

type callTarget struct {
	symbol *Symbol
	file   *callFile
}

// AnalyzeCalls uses only the supplied snapshot. A resolved edge means that the
// source expression binds to the recorded local declaration; it is not evidence
// that the repository builds or that the call will execute at runtime.
func AnalyzeCalls(ctx context.Context, files []SourceFile, result *Result) error {
	ctx, span := otel.Tracer("denverr/internal/indexer/golang").Start(ctx, "golang.analyze_calls")
	defer span.End()
	span.AddEvent("execution.started")
	if result == nil {
		span.SetStatus(codes.Error, "missing syntax index")
		span.AddEvent("execution.failed")
		return errors.New("syntax index is required")
	}
	analyzer := newCallAnalyzer(ctx, result)
	err := analyzer.analyze(files)
	if err != nil {
		result.CallAnalysis.Status = "partial"
		result.ImplementationAnalysis.Status = "partial"
		span.RecordError(err)
		span.SetStatus(codes.Error, "call analysis canceled")
		span.AddEvent("execution.canceled")
		return err
	}
	analyzer.finish()
	span.SetAttributes(attribute.Int("analysis.resolved_calls", result.CallAnalysis.Resolved), attribute.Int("analysis.unresolved_calls", result.CallAnalysis.Unresolved))
	span.SetAttributes(attribute.String("analysis.status", result.CallAnalysis.Status), attribute.Int("analysis.diagnostic_count", len(result.CallAnalysis.Diagnostics)))
	span.SetAttributes(attribute.Int("analysis.implementation_count", len(result.Implementations)), attribute.String("analysis.implementation_status", result.ImplementationAnalysis.Status))
	span.AddEvent("execution.completed")
	return nil
}

func newCallAnalyzer(ctx context.Context, result *Result) *callAnalyzer {
	initializeImplementations(result)
	result.Calls = []Call{}
	result.CallAnalysis = CallAnalysis{Status: "complete", Diagnostics: []Diagnostic{}, Limitations: []string{
		"Static local binding only; no claim that a call executes or the repository builds.",
		"Only unconditional, non-platform, non-test Go files are type-checked; excluded file calls remain unresolved.",
		"Only packages under each captured module's declared path are imported; external/standard-library declarations and cross-module build selection are unavailable. No dependencies are executed or fetched.",
		"Function values and interface/type-parameter dispatch are unresolved; conversions with known types are excluded.",
		"A resolved local binding can retain unknown imported signature types; diagnostics remain authoritative and resolution does not validate argument compatibility or compilation.",
		"Type checking uses fixed gc/amd64 integer sizes, not ambient GOOS/GOARCH; workspace and replacement build configurations are not evaluated.",
		"Calls are inventoried in function/closure bodies and variable/constant initializers, not type expressions or generated runtime initialization.",
	}}
	return &callAnalyzer{ctx: ctx, result: result, fset: token.NewFileSet(), byPath: map[string]*callFile{}, imports: map[string]*callPackage{}, functions: map[*types.Func]callTarget{}, declarations: map[types.Object]callTarget{}, signatures: map[*types.Func]methodSyntax{}}
}

func (a *callAnalyzer) analyze(files []SourceFile) error {
	if err := a.ctx.Err(); err != nil {
		return err
	}
	a.loadModules(files)
	a.loadFiles(files)
	a.groupPackages()
	for _, pkg := range a.packages {
		if _, err := a.checkPackage(pkg); err != nil && a.ctx.Err() != nil {
			return a.ctx.Err()
		}
	}
	a.registerTypes()
	a.collectImplementations()
	for _, file := range a.files {
		if err := a.ctx.Err(); err != nil {
			return err
		}
		sort.Slice(file.errors, func(i, j int) bool { return file.errors[i].Position.Offset < file.errors[j].Position.Offset })
		a.collectFileCalls(file)
	}
	return a.ctx.Err()
}

func (a *callAnalyzer) finish() {
	a.finishImplementations()
	for _, call := range a.result.Calls {
		if call.Resolution == "resolved" {
			a.result.CallAnalysis.Resolved++
		} else if call.Resolution == "unresolved" {
			a.result.CallAnalysis.Unresolved++
		}
	}
	if a.result.CallAnalysis.Unresolved > 0 || len(a.result.CallAnalysis.Diagnostics) > 0 {
		a.result.CallAnalysis.Status = "partial"
	}
	sort.Slice(a.result.Calls, func(i, j int) bool {
		left, right := a.result.Calls[i], a.result.Calls[j]
		if left.Path != right.Path {
			return left.Path < right.Path
		}
		return left.Span.Start.Offset < right.Span.Start.Offset
	})
}

func (a *callAnalyzer) diagnostic(file, code, message string, pos token.Pos) Diagnostic {
	position := sourcePosition(a.fset.PositionFor(pos, false))
	diagnostic := Diagnostic{Path: file, Code: code, Message: message, Position: position}
	a.result.CallAnalysis.Diagnostics = append(a.result.CallAnalysis.Diagnostics, diagnostic)
	return diagnostic
}
