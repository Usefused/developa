package golang

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"go/ast"
	"go/printer"
	"go/token"
	"strconv"
	"strings"
	"unicode/utf8"
)

type extraction struct {
	source     SourceFile
	fset       *token.FileSet
	block      *FileBlock
	identities map[string]int
	comments   []*ast.CommentGroup
}

func (e *extraction) extractFile(file *ast.File) {
	e.comments = file.Comments
	for _, item := range file.Imports {
		value, _ := strconv.Unquote(item.Path.Value)
		entry := Import{Path: value, Span: e.span(item)}
		if item.Name != nil {
			entry.Alias = item.Name.Name
		}
		e.block.Imports = append(e.block.Imports, entry)
	}
	for _, declaration := range file.Decls {
		switch node := declaration.(type) {
		case *ast.FuncDecl:
			e.function(node)
		case *ast.GenDecl:
			e.general(node)
		}
	}
}

func (e *extraction) general(declaration *ast.GenDecl) {
	var inherited *ast.ValueSpec
	for _, item := range declaration.Specs {
		switch spec := item.(type) {
		case *ast.TypeSpec:
			e.typeDeclaration(spec, declaration.Doc)
		case *ast.ValueSpec:
			values := spec
			// The Go grammar permits omitted const expressions to repeat the prior
			// declaration. Preserve that syntax without pretending to evaluate iota.
			if declaration.Tok == token.CONST && len(spec.Values) == 0 && inherited != nil {
				values = inherited
			}
			e.values(spec, values, declaration)
			inherited = values
		}
	}
}

func (e *extraction) newSymbol(kind Kind, name, parent, receiver string, node ast.Node) Symbol {
	key := strings.Join([]string{e.source.Path, e.block.Package, string(kind), parent, receiver, name}, "\x00")
	ordinal := e.identities[key]
	e.identities[key]++
	id := hashBytes([]byte(key + "\x00" + strconv.Itoa(ordinal)))
	span := e.span(node)
	contentHash := e.contentHash(span)
	source, truncated := e.sourceExcerpt(span)
	return Symbol{
		ID: id, SourceID: hashBytes([]byte(fmt.Sprintf("%s:%s:%d:%d", id, e.block.ContentHash, span.Start.Offset, span.End.Offset))),
		ContentHash: contentHash, Kind: kind, Name: name, ParentID: parent, Receiver: receiver,
		Visibility: visibility(name), Span: span, Source: source, SourceTruncated: truncated,
	}
}

const maxSymbolSourceBytes = 8 << 10

func (e *extraction) sourceExcerpt(span Span) (string, bool) {
	if span.Start.Offset < 0 || span.End.Offset > len(e.source.Content) || span.End.Offset < span.Start.Offset {
		return "", false
	}
	content := e.source.Content[span.Start.Offset:span.End.Offset]
	if len(content) <= maxSymbolSourceBytes {
		return string(content), false
	}
	end := maxSymbolSourceBytes
	for end > 0 && !utf8.RuneStart(content[end]) {
		end--
	}
	return string(content[:end]), true
}

func (e *extraction) contentHash(span Span) string {
	if span.Start.Offset < 0 || span.End.Offset > len(e.source.Content) || span.End.Offset < span.Start.Offset {
		return ""
	}
	return hashBytes(e.source.Content[span.Start.Offset:span.End.Offset])
}

func (e *extraction) span(node ast.Node) Span {
	return Span{Start: sourcePosition(e.fset.PositionFor(node.Pos(), false)), End: sourcePosition(e.fset.PositionFor(node.End(), false))}
}

func sourcePosition(position token.Position) Position {
	return Position{Line: position.Line, Column: position.Column, Offset: position.Offset}
}

func hashBytes(content []byte) string {
	digest := sha256.Sum256(content)
	return hex.EncodeToString(digest[:])
}

func visibility(name string) string {
	if ast.IsExported(name) {
		return "exported"
	}
	return "unexported"
}

func (e *extraction) format(node ast.Node) string {
	if node == nil {
		return ""
	}
	var output bytes.Buffer
	_ = printer.Fprint(&output, e.fset, node)
	return output.String()
}

func (e *extraction) documentation(own, group *ast.CommentGroup) string {
	if own != nil {
		return commentText(own)
	}
	return commentText(group)
}

func (e *extraction) values(spec, expression *ast.ValueSpec, declaration *ast.GenDecl) {
	kind := Variable
	if declaration.Tok == token.CONST {
		kind = Constant
	}
	for _, name := range spec.Names {
		symbol := e.newSymbol(kind, name.Name, "", "", spec)
		symbol.Signature = declaration.Tok.String() + " " + e.format(spec)
		symbol.Type = e.format(expression.Type)
		symbol.Doc = e.documentation(spec.Doc, declaration.Doc)
		symbol.Comment = commentText(spec.Comment)
		for _, value := range expression.Values {
			symbol.Values = append(symbol.Values, e.format(value))
		}
		e.block.Symbols = append(e.block.Symbols, symbol)
	}
	// One tuple initializer belongs to a declaration, not independently to each
	// variable. Attach any closures to its first name to avoid duplicate records.
	if len(spec.Names) > 0 && len(spec.Values) > 0 {
		parent := e.block.Symbols[len(e.block.Symbols)-len(spec.Names)].ID
		e.closures(spec, parent)
	}
}
