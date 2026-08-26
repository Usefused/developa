package golang

import (
	"go/ast"
	"strconv"
)

func (e *extraction) function(node *ast.FuncDecl) {
	kind := Function
	receiver, receiverName := e.receiver(node.Recv)
	if node.Recv != nil {
		kind = Method
	}
	symbol := e.newSymbol(kind, node.Name.Name, "", receiver, node)
	// The function body is source evidence, not part of its public signature.
	signature := *node
	signature.Body = nil
	signature.Doc = nil
	symbol.Signature = e.format(&signature)
	symbol.Doc = commentText(node.Doc)
	symbol.Documentation = e.functionDocumentation(node.Doc, node.Body)
	symbol.ReceiverName = receiverName
	e.functionType(&symbol, node.Type)
	e.block.Symbols = append(e.block.Symbols, symbol)
	if node.Body != nil {
		e.closures(node.Body, symbol.ID)
	}
}

func (e *extraction) receiver(fields *ast.FieldList) (string, string) {
	if fields == nil || len(fields.List) == 0 {
		return "", ""
	}
	name := ""
	if len(fields.List[0].Names) > 0 {
		name = fields.List[0].Names[0].Name
	}
	return e.format(fields.List[0].Type), name
}

func (e *extraction) functionType(symbol *Symbol, node *ast.FuncType) {
	symbol.Parameters = e.parameters(node.Params)
	symbol.Results = e.parameters(node.Results)
	symbol.TypeParameters = e.parameters(node.TypeParams)
}

func (e *extraction) parameters(fields *ast.FieldList) []Parameter {
	if fields == nil {
		return nil
	}
	var result []Parameter
	for group, field := range fields.List {
		names := field.Names
		if len(names) == 0 {
			names = []*ast.Ident{nil}
		}
		for _, name := range names {
			result = append(result, e.parameter(field, name, len(result), group))
		}
	}
	return result
}

func (e *extraction) parameter(field *ast.Field, name *ast.Ident, position, group int) Parameter {
	typeNode := field.Type
	variadic := false
	if ellipsis, ok := typeNode.(*ast.Ellipsis); ok {
		typeNode = ellipsis.Elt
		variadic = true
	}
	result := Parameter{Position: position, Group: group, Type: e.format(typeNode), Variadic: variadic, Span: e.span(field)}
	if name != nil {
		result.Name = name.Name
	}
	return result
}

func (e *extraction) closures(node ast.Node, parent string) {
	ordinal := 0
	ast.Inspect(node, func(child ast.Node) bool {
		literal, ok := child.(*ast.FuncLit)
		if !ok {
			return true
		}
		ordinal++
		symbol := e.newSymbol(Closure, "$closure"+strconv.Itoa(ordinal), parent, "", literal)
		symbol.Signature = e.format(literal.Type)
		symbol.Visibility = "local"
		symbol.Documentation = e.functionDocumentation(nil, literal.Body)
		e.functionType(&symbol, literal.Type)
		e.block.Symbols = append(e.block.Symbols, symbol)
		e.closures(literal.Body, symbol.ID)
		return false
	})
}
