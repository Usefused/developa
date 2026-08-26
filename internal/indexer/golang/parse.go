package golang

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/scanner"
	"go/token"
	"path"
	"sort"
	"strings"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

var incompleteSyntax = errors.New("Go syntax extraction is partial")

// Parse extracts declarations from captured source bytes. Repository build flags
// are intentionally not applied: this slice inventories syntax, not a build graph.
func Parse(ctx context.Context, files []SourceFile) (Result, error) {
	ctx, span := otel.Tracer("developa/internal/indexer/golang").Start(ctx, "golang.parse")
	defer span.End()
	span.SetAttributes(attribute.Int("source.file_count", len(files)))
	span.AddEvent("execution.started")
	result := emptyResult()
	ordered := append([]SourceFile(nil), files...)
	sort.SliceStable(ordered, func(i, j int) bool { return ordered[i].Path < ordered[j].Path })
	seen := make(map[string]bool, len(ordered))
	for _, source := range ordered {
		if err := ctx.Err(); err != nil {
			return canceledResult(result, span, err)
		}
		addSource(&result, source, seen)
	}
	if err := ctx.Err(); err != nil {
		return canceledResult(result, span, err)
	}
	finishSpan(span, result)
	return result, nil
}

func emptyResult() Result {
	return Result{
		Analysis: "syntax", Completeness: Complete,
		Files: []FileBlock{}, Diagnostics: []Diagnostic{}, Skipped: []SkippedFile{},
		Calls: []Call{}, CallAnalysis: CallAnalysis{Status: "not_analyzed", Limitations: []string{}, Diagnostics: []Diagnostic{}},
		Implementations: []Implementation{}, ImplementationAnalysis: ImplementationAnalysis{Status: "not_analyzed", Limitations: []string{}},
		Limitations: []string{
			"The symbol inventory contains syntax facts; type-informed call binding status is reported separately in call_analysis.",
			"All supplied Go files are parsed without evaluating build constraints or package variants.",
			"Local named declarations are not indexed; anonymous functions are indexed under their enclosing symbol.",
			"Logical identities include file paths; closures and duplicate declarations use source-order ordinals.",
		},
	}
}

func addSource(result *Result, source SourceFile, seen map[string]bool) {
	source.Path = path.Clean(source.Path)
	if !strings.HasSuffix(source.Path, ".go") {
		result.Skipped = append(result.Skipped, SkippedFile{Path: source.Path, Reason: "unsupported_language"})
		return
	}
	if seen[source.Path] {
		result.Completeness = Partial
		result.Diagnostics = append(result.Diagnostics, Diagnostic{Path: source.Path, Code: "duplicate_path", Message: "duplicate source path"})
		return
	}
	seen[source.Path] = true
	block, diagnostics := parseFile(source)
	result.Files = append(result.Files, block)
	result.Diagnostics = append(result.Diagnostics, diagnostics...)
	if block.Completeness == Partial {
		result.Completeness = Partial
	}
}

func parseFile(source SourceFile) (FileBlock, []Diagnostic) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, source.Path, source.Content, parser.ParseComments|parser.AllErrors|parser.SkipObjectResolution)
	block := FileBlock{Path: source.Path, ContentHash: hashBytes(source.Content), Source: bytes.Clone(source.Content), Completeness: Complete, Symbols: []Symbol{}, Imports: []Import{}}
	diagnostics := parseDiagnostics(source, fset, err)
	if err != nil {
		block.Completeness = Partial
	}
	if file == nil {
		block.Overview = "Go source could not be parsed."
		return block, diagnostics
	}
	block.Package = file.Name.Name
	block.Doc = commentText(file.Doc)
	extractor := extraction{source: source, fset: fset, block: &block, identities: map[string]int{}}
	extractor.extractFile(file)
	block.Overview = overview(block)
	return block, diagnostics
}

func parseDiagnostics(source SourceFile, fset *token.FileSet, err error) []Diagnostic {
	if err == nil {
		return nil
	}
	var syntaxErrors scanner.ErrorList
	if !errors.As(err, &syntaxErrors) {
		return []Diagnostic{{Path: source.Path, Code: "syntax_error", Message: err.Error()}}
	}
	diagnostics := make([]Diagnostic, 0, len(syntaxErrors))
	for _, entry := range syntaxErrors {
		position := rawErrorPosition(fset, entry.Pos)
		diagnostics = append(diagnostics, Diagnostic{Path: source.Path, Code: "syntax_error", Message: entry.Msg, Position: position})
	}
	return diagnostics
}

func rawErrorPosition(fset *token.FileSet, position token.Position) Position {
	// //line directives must not redirect editor links to a different physical file.
	fset.Iterate(func(file *token.File) bool {
		position = file.PositionFor(file.Pos(position.Offset), false)
		return false
	})
	return sourcePosition(position)
}

func canceledResult(result Result, span trace.Span, err error) (Result, error) {
	result.Completeness = Partial
	span.RecordError(err)
	span.SetStatus(codes.Error, "source parsing canceled")
	span.AddEvent("execution.canceled")
	return result, err
}

func finishSpan(span trace.Span, result Result) {
	span.SetAttributes(attribute.Int("analysis.file_count", len(result.Files)), attribute.Int("analysis.diagnostic_count", len(result.Diagnostics)))
	if result.Completeness == Partial {
		span.RecordError(incompleteSyntax)
		span.SetStatus(codes.Error, "syntax extraction partial")
		span.AddEvent("execution.failed")
		return
	}
	span.AddEvent("execution.completed")
}

func overview(block FileBlock) string {
	counts := map[Kind]int{}
	for _, symbol := range block.Symbols {
		counts[symbol.Kind]++
	}
	parts := []string{}
	for _, kind := range []Kind{Function, Method, Struct, Interface, Alias, NamedType, Constant, Variable, Closure} {
		if count := counts[kind]; count > 0 {
			parts = append(parts, fmt.Sprintf("%s: %d", kind, count))
		}
	}
	if len(parts) == 0 {
		return fmt.Sprintf("Package %s; no declarations.", block.Package)
	}
	return fmt.Sprintf("Package %s; %s.", block.Package, strings.Join(parts, ", "))
}

func commentText(group *ast.CommentGroup) string {
	if group == nil {
		return ""
	}
	return strings.TrimSpace(group.Text())
}
