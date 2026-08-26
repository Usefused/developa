package golang

import (
	"go/ast"
	"strconv"
	"strings"
)

func (e *extraction) typeDeclaration(spec *ast.TypeSpec, groupDoc *ast.CommentGroup) {
	symbol := e.newSymbol(typeKind(spec), spec.Name.Name, "", "", spec)
	symbol.Signature = "type " + e.format(spec)
	symbol.Type = e.format(spec.Type)
	symbol.TypeParameters = e.parameters(spec.TypeParams)
	symbol.Doc = e.documentation(spec.Doc, groupDoc)
	symbol.Comment = commentText(spec.Comment)
	switch aggregate := spec.Type.(type) {
	case *ast.StructType:
		symbol.Fields = e.fields(aggregate.Fields)
	case *ast.InterfaceType:
		symbol.Fields = e.interfaceEmbeddings(aggregate.Methods)
	}
	e.block.Symbols = append(e.block.Symbols, symbol)
	e.typeMembers(spec.Type, symbol.ID)
}

func typeKind(spec *ast.TypeSpec) Kind {
	if spec.Assign.IsValid() {
		return Alias
	}
	switch spec.Type.(type) {
	case *ast.StructType:
		return Struct
	case *ast.InterfaceType:
		return Interface
	default:
		return NamedType
	}
}

func (e *extraction) fields(list *ast.FieldList) []FieldInfo {
	var result []FieldInfo
	for _, field := range list.List {
		if len(field.Names) == 0 {
			result = append(result, e.fieldInfo(field, embeddedName(field.Type), true))
			continue
		}
		for _, name := range field.Names {
			result = append(result, e.fieldInfo(field, name.Name, false))
		}
	}
	return result
}

func (e *extraction) fieldInfo(field *ast.Field, name string, embedded bool) FieldInfo {
	result := FieldInfo{Name: name, Type: e.format(field.Type), Embedded: embedded, Doc: commentText(field.Doc), Comment: commentText(field.Comment), Span: e.span(field)}
	if field.Tag != nil {
		result.TagLiteral = field.Tag.Value
		result.Tag, _ = strconv.Unquote(field.Tag.Value)
	}
	return result
}

func embeddedName(node ast.Expr) string {
	switch expression := node.(type) {
	case *ast.Ident:
		return expression.Name
	case *ast.SelectorExpr:
		return expression.Sel.Name
	case *ast.StarExpr:
		return embeddedName(expression.X)
	case *ast.IndexExpr:
		return embeddedName(expression.X)
	case *ast.IndexListExpr:
		return embeddedName(expression.X)
	default:
		return ""
	}
}

func (e *extraction) interfaceEmbeddings(list *ast.FieldList) []FieldInfo {
	var result []FieldInfo
	for _, field := range list.List {
		if len(field.Names) == 0 {
			result = append(result, e.fieldInfo(field, embeddedName(field.Type), true))
		}
	}
	return result
}

func (e *extraction) typeMembers(expression ast.Expr, parent string) {
	switch node := expression.(type) {
	case *ast.StructType:
		e.structMembers(node, parent)
	case *ast.InterfaceType:
		e.interfaceMethods(node, parent)
	}
}

func (e *extraction) structMembers(node *ast.StructType, parent string) {
	for _, field := range node.Fields.List {
		names := field.Names
		if len(names) == 0 {
			e.structMember(field, embeddedName(field.Type), parent)
			continue
		}
		for _, name := range names {
			e.structMember(field, name.Name, parent)
		}
	}
}

func (e *extraction) structMember(field *ast.Field, name, parent string) {
	symbol := e.newSymbol(Field, name, parent, "", field)
	symbol.Signature = e.fieldSignature(field)
	symbol.Type = e.format(field.Type)
	symbol.Fields = []FieldInfo{e.fieldInfo(field, name, len(field.Names) == 0)}
	symbol.Doc = commentText(field.Doc)
	symbol.Comment = commentText(field.Comment)
	e.block.Symbols = append(e.block.Symbols, symbol)
}

func (e *extraction) interfaceMethods(node *ast.InterfaceType, parent string) {
	for _, field := range node.Methods.List {
		function, ok := field.Type.(*ast.FuncType)
		if !ok {
			continue
		}
		for _, name := range field.Names {
			symbol := e.newSymbol(InterfaceMethod, name.Name, parent, "", field)
			symbol.Signature = name.Name + strings.TrimPrefix(e.format(function), "func")
			symbol.Doc = commentText(field.Doc)
			symbol.Comment = commentText(field.Comment)
			e.functionType(&symbol, function)
			e.block.Symbols = append(e.block.Symbols, symbol)
		}
	}
}

func (e *extraction) fieldSignature(field *ast.Field) string {
	names := make([]string, 0, len(field.Names))
	for _, name := range field.Names {
		names = append(names, name.Name)
	}
	typeText := e.format(field.Type)
	parts := []string{}
	if len(names) > 0 {
		parts = append(parts, strings.Join(names, ", "))
	}
	parts = append(parts, typeText)
	if field.Tag != nil {
		parts = append(parts, field.Tag.Value)
	}
	return strings.Join(parts, " ")
}
