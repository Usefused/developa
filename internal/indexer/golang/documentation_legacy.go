package golang

import (
	"go/ast"
	"go/parser"
	"go/token"
)

// DocumentationFor keeps historical snapshots useful without consulting today's
// working tree or rewriting immutable records. Legacy summaries use only the
// stored excerpt, so incomplete captures remain explicitly labeled.
func DocumentationFor(symbol Symbol) *Documentation {
	if symbol.Documentation != nil {
		return symbol.Documentation
	}
	result := &Documentation{Origin: "captured_excerpt", Comments: []SourceComment{}}
	appendSourceComment(result, SourceComment{Kind: "doc", Text: symbol.Doc})
	appendSourceComment(result, SourceComment{Kind: "comment", Text: symbol.Comment})
	switch symbol.Kind {
	case Function, Method, Closure:
		appendCapturedComments(result, symbol)
	}
	return result
}

func appendCapturedComments(result *Documentation, symbol Symbol) {
	prefix := "package captured\n"
	if symbol.Kind == Closure {
		prefix += "var _ = "
	}
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "captured.go", prefix+symbol.Source, parser.ParseComments|parser.AllErrors|parser.SkipObjectResolution)
	result.Truncated = result.Truncated || symbol.SourceTruncated || err != nil || symbol.Source == ""
	if file == nil {
		return
	}
	body := capturedFunctionBody(file)
	e := extraction{fset: fset, comments: file.Comments, block: &FileBlock{Completeness: Complete}}
	comments := e.functionDocumentation(nil, body)
	for _, comment := range comments.Comments {
		span := *comment.Span
		span.Start = capturedPosition(span.Start, symbol.Span.Start, len(prefix))
		span.End = capturedPosition(span.End, symbol.Span.Start, len(prefix))
		comment.Span = &span
		appendSourceComment(result, comment)
	}
	result.Truncated = result.Truncated || comments.Truncated || body == nil
}

func capturedFunctionBody(file *ast.File) *ast.BlockStmt {
	var body *ast.BlockStmt
	ast.Inspect(file, func(node ast.Node) bool {
		if body != nil {
			return false
		}
		switch function := node.(type) {
		case *ast.FuncDecl:
			body = function.Body
		case *ast.FuncLit:
			body = function.Body
		}
		return body == nil
	})
	return body
}

func capturedPosition(position, start Position, prefixBytes int) Position {
	position.Offset += start.Offset - prefixBytes
	if position.Line == 2 {
		// The artificial package line is absent in the captured declaration.
		position.Column += start.Column - 1 - (prefixBytes - len("package captured\n"))
	}
	position.Line += start.Line - 2
	return position
}
