package golang

import (
	"go/ast"
	"go/types"
)

type declaredType struct {
	object *types.TypeName
	target callTarget
}

type methodSyntax struct {
	file      *callFile
	signature *ast.FuncType
}

func (target callTarget) reference() *SymbolReference {
	if target.symbol == nil || target.file == nil {
		return nil
	}
	return &SymbolReference{SymbolID: target.symbol.ID, Name: target.symbol.Name, Path: target.file.source.Path, Span: target.symbol.Span}
}

func (a *callAnalyzer) registerTypes() {
	for _, file := range a.files {
		if file.exclusion != "" || file.pkg.invalid {
			continue
		}
		for _, declaration := range file.ast.Decls {
			if group, ok := declaration.(*ast.GenDecl); ok {
				a.registerTypeGroup(file, group)
			}
		}
	}
}

func (a *callAnalyzer) registerTypeGroup(file *callFile, group *ast.GenDecl) {
	for _, spec := range group.Specs {
		declaration, ok := spec.(*ast.TypeSpec)
		if !ok {
			continue
		}
		object, ok := file.pkg.info.Defs[declaration.Name].(*types.TypeName)
		symbol := a.symbolFor(file, declaration, typeKind(declaration))
		if !ok || symbol == nil {
			continue
		}
		target := callTarget{symbol: symbol, file: file}
		a.declarations[object] = target
		a.types = append(a.types, declaredType{object: object, target: target})
		if node, ok := declaration.Type.(*ast.InterfaceType); ok {
			a.registerInterfaceMethods(file, node)
		}
	}
}

func (a *callAnalyzer) registerInterfaceMethods(file *callFile, declaration *ast.InterfaceType) {
	for _, field := range declaration.Methods.List {
		signature, ok := field.Type.(*ast.FuncType)
		if !ok {
			continue
		}
		for _, name := range field.Names {
			a.registerInterfaceMethod(file, field, name, signature)
		}
	}
}

func (a *callAnalyzer) registerInterfaceMethod(file *callFile, field *ast.Field, name *ast.Ident, signature *ast.FuncType) {
	object, ok := file.pkg.info.Defs[name].(*types.Func)
	symbol := a.symbolFor(file, field, InterfaceMethod)
	if !ok || symbol == nil {
		return
	}
	a.declarations[object.Origin()] = callTarget{symbol: symbol, file: file}
	a.signatures[object.Origin()] = methodSyntax{file: file, signature: signature}
}

func (a *callAnalyzer) referenceInterfaceCall(file *callFile, expression ast.Expr, function *types.Func, call *Call) {
	call.InterfaceMethod = a.declarations[function.Origin()].reference()
	if selector, ok := expression.(*ast.SelectorExpr); ok {
		if selection := file.pkg.info.Selections[selector]; selection != nil {
			call.Interface = a.interfaceReference(selection.Recv())
		}
	}
	if call.Interface == nil {
		signature := function.Type().(*types.Signature)
		call.Interface = a.interfaceReference(signature.Recv().Type())
	}
}

func (a *callAnalyzer) referenceInvalidInterfaceCall(file *callFile, expression ast.Expr, call *Call) {
	function, ok := calledObject(file.pkg.info, expression).(*types.Func)
	if ok && interfaceDispatch(file.pkg.info, expression, function) {
		// A rejected argument does not change the selected interface declaration;
		// it still must not produce a resolved target or runtime receiver claim.
		a.referenceInterfaceCall(file, expression, function, call)
	}
}

func (a *callAnalyzer) interfaceReference(value types.Type) *SymbolReference {
	if parameter, ok := value.(*types.TypeParam); ok {
		value = parameter.Constraint()
	}
	if !interfaceType(value) {
		return nil
	}
	switch named := value.(type) {
	case *types.Alias:
		if reference := a.interfaceReference(types.Unalias(named)); reference != nil {
			return reference
		}
		return a.declarations[named.Obj()].reference()
	case *types.Named:
		return a.declarations[named.Origin().Obj()].reference()
	default:
		return nil
	}
}
