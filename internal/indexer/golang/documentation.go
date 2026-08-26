package golang

import (
	"go/ast"
	"go/token"
	"sort"
	"strings"
	"unicode/utf8"
)

const maxDocumentationBytes = 8 << 10
const maxDocumentationComments = 64

func (e *extraction) functionDocumentation(doc *ast.CommentGroup, body *ast.BlockStmt) *Documentation {
	result := &Documentation{Origin: "indexed_source", Comments: []SourceComment{}, Truncated: e.block.Completeness != Complete}
	if doc != nil {
		span := e.span(doc)
		appendSourceComment(result, SourceComment{Kind: "doc", Text: commentText(doc), Span: &span})
	}
	if body == nil {
		return result
	}
	end := e.commentBodyEnd(body)
	closures := nestedFunctionRanges(body, end)
	start := sort.Search(len(e.comments), func(i int) bool { return e.comments[i].Pos() >= body.Pos() })
	for _, group := range e.comments[start:] {
		if group.Pos() >= end {
			break
		}
		if commentInsideClosure(group, closures) {
			continue
		}
		span := e.span(group)
		appendSourceComment(result, SourceComment{Kind: "body", Text: commentText(group), Span: &span})
	}
	return result
}

type functionRange struct{ start, end token.Pos }

func (e *extraction) commentBodyEnd(body *ast.BlockStmt) token.Pos {
	if body.Rbrace.IsValid() {
		return body.End()
	}
	// Partial stored excerpts commonly end inside the body. ParseComments still
	// captures their comments even when the AST has no closing brace position.
	file := e.fset.File(body.Pos())
	return token.Pos(file.Base() + file.Size() + 1)
}

func nestedFunctionRanges(body *ast.BlockStmt, bodyEnd token.Pos) []functionRange {
	var ranges []functionRange
	ast.Inspect(body, func(node ast.Node) bool {
		literal, ok := node.(*ast.FuncLit)
		if !ok {
			return true
		}
		// Closures have their own symbol records. Their comments must not become
		// claims about the enclosing function merely because they share a file.
		end := literal.End()
		if literal.Body != nil && !literal.Body.Rbrace.IsValid() {
			end = bodyEnd
		}
		ranges = append(ranges, functionRange{literal.Pos(), end})
		return false
	})
	return ranges
}

func commentInsideClosure(group *ast.CommentGroup, ranges []functionRange) bool {
	i := sort.Search(len(ranges), func(i int) bool { return ranges[i].end > group.Pos() })
	return i < len(ranges) && ranges[i].start <= group.Pos()
}

func appendSourceComment(doc *Documentation, comment SourceComment) {
	if comment.Text == "" {
		return
	}
	separator := ""
	if doc.Summary != "" {
		separator = "\n\n"
	}
	remaining := maxDocumentationBytes - len(doc.Summary) - len(separator)
	if remaining <= 0 || len(doc.Comments) >= maxDocumentationComments {
		doc.Truncated = true
		return
	}
	text := documentationPrefix(comment.Text, remaining)
	doc.Truncated = doc.Truncated || len(text) < len(comment.Text)
	comment.Text = text
	doc.Comments = append(doc.Comments, comment)
	doc.Summary += separator + text
}

func documentationPrefix(value string, limit int) string {
	value = strings.ToValidUTF8(value, "�")
	if len(value) <= limit {
		return value
	}
	for limit > 0 && !utf8.RuneStart(value[limit]) {
		limit--
	}
	return value[:limit]
}
