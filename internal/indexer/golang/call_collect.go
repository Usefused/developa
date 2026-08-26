package golang

import (
	"fmt"
	"go/ast"
	"go/token"
)

func (a *callAnalyzer) collectFileCalls(file *callFile) {
	for _, declaration := range file.ast.Decls {
		switch node := declaration.(type) {
		case *ast.FuncDecl:
			a.collectFunctionCalls(file, node)
		case *ast.GenDecl:
			a.collectInitializerCalls(file, node)
		}
	}
}

func (a *callAnalyzer) collectFunctionCalls(file *callFile, declaration *ast.FuncDecl) {
	kind := Function
	if declaration.Recv != nil {
		kind = Method
	}
	caller := a.symbolFor(file, declaration, kind)
	if caller != nil && declaration.Body != nil {
		a.walkCalls(file, caller, declaration.Body)
	}
}

func (a *callAnalyzer) collectInitializerCalls(file *callFile, declaration *ast.GenDecl) {
	kind := Variable
	if declaration.Tok == token.CONST {
		kind = Constant
	}
	for _, item := range declaration.Specs {
		spec, ok := item.(*ast.ValueSpec)
		if !ok {
			continue
		}
		caller := a.symbolFor(file, spec, kind)
		if caller == nil {
			continue
		}
		for _, value := range spec.Values {
			a.walkCalls(file, caller, value)
		}
	}
}

func (a *callAnalyzer) walkCalls(file *callFile, caller *Symbol, body ast.Node) {
	ast.Inspect(body, func(node ast.Node) bool {
		if a.ctx.Err() != nil {
			return false
		}
		switch expression := node.(type) {
		case *ast.FuncLit:
			a.collectClosureCalls(file, expression)
			return false
		case *ast.CallExpr:
			a.addCall(file, caller, expression)
		}
		return true
	})
}

func (a *callAnalyzer) collectClosureCalls(file *callFile, expression *ast.FuncLit) {
	caller := a.symbolFor(file, expression, Closure)
	if caller != nil {
		a.walkCalls(file, caller, expression.Body)
	}
}

func (a *callAnalyzer) addCall(file *callFile, caller *Symbol, expression *ast.CallExpr) {
	if knownConversion(file.pkg.info, expression.Fun) {
		return
	}
	span := a.callSpan(expression)
	call := Call{CallerID: caller.ID, CallerName: caller.Name, Path: file.source.Path, Span: span,
		Resolution: "unresolved", TargetName: callName(expression.Fun)}
	call.ID = hashBytes([]byte(fmt.Sprintf("%s:%s:%d:%d", caller.ID, file.block.ContentHash, span.Start.Offset, span.End.Offset)))
	a.resolveCall(file, expression, &call)
	a.result.Calls = append(a.result.Calls, call)
}
