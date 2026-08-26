package golang

import (
	"go/ast"
	"go/types"
	"sort"
)

func (a *callAnalyzer) resolveCall(file *callFile, expression *ast.CallExpr, call *Call) {
	if reason := blockedCall(file, call.Span); reason != "" {
		call.Reason = reason
		if reason == "type_error_at_callsite" {
			a.referenceInvalidInterfaceCall(file, unwrapCallee(expression.Fun), call)
		}
		return
	}
	callee := unwrapCallee(expression.Fun)
	if literal, ok := callee.(*ast.FuncLit); ok {
		a.resolveTarget(call, callTarget{symbol: a.symbolFor(file, literal, Closure), file: file})
		return
	}
	object := calledObject(file.pkg.info, callee)
	switch target := object.(type) {
	case *types.Builtin:
		call.Resolution, call.TargetName = "builtin", target.Name()
	case *types.Func:
		a.resolveFunction(file, callee, target, call)
	case *types.Var:
		call.Reason = "function_value_dispatch"
	default:
		call.Reason = "callee_binding_unavailable_call_or_conversion"
	}
}

func (a *callAnalyzer) resolveFunction(file *callFile, expression ast.Expr, function *types.Func, call *Call) {
	if interfaceDispatch(file.pkg.info, expression, function) {
		call.Reason = "interface_or_type_parameter_dispatch"
		a.referenceInterfaceCall(file, expression, function, call)
		return
	}
	target, local := a.functions[function.Origin()]
	if !local {
		call.Resolution, call.TargetName, call.Reason = "external", function.Name(), "callable_declaration_outside_index"
		return
	}
	a.resolveTarget(call, target)
}

func (a *callAnalyzer) resolveTarget(call *Call, target callTarget) {
	if target.symbol == nil {
		call.Reason = "target_symbol_unavailable"
		return
	}
	if target.file.pkg.invalid || target.file.exclusion != "" {
		call.Reason = "target_declaration_not_type_checked"
		return
	}
	call.TargetID, call.TargetName, call.Resolution = target.symbol.ID, target.symbol.Name, "resolved"
	call.Target = target.reference()
}

func blockedCall(file *callFile, span Span) string {
	if file.exclusion != "" {
		return file.exclusion + ":call_or_conversion_not_type_checked"
	}
	if file.pkg.invalid {
		return "package_declaration_errors"
	}
	index := sort.Search(len(file.errors), func(index int) bool { return file.errors[index].Position.Offset >= span.Start.Offset })
	if index < len(file.errors) && file.errors[index].Position.Offset < span.End.Offset {
		return "type_error_at_callsite"
	}
	return ""
}

func knownConversion(info *types.Info, expression ast.Expr) bool {
	if info.Types[expression].IsType() {
		return true
	}
	_, conversion := calledObject(info, unwrapCallee(expression)).(*types.TypeName)
	return conversion
}

func calledObject(info *types.Info, expression ast.Expr) types.Object {
	switch node := expression.(type) {
	case *ast.Ident:
		return info.ObjectOf(node)
	case *ast.SelectorExpr:
		if selection := info.Selections[node]; selection != nil {
			return selection.Obj()
		}
		return info.ObjectOf(node.Sel)
	default:
		return nil
	}
}

func unwrapCallee(expression ast.Expr) ast.Expr {
	switch node := expression.(type) {
	case *ast.ParenExpr:
		return unwrapCallee(node.X)
	case *ast.IndexExpr:
		return unwrapCallee(node.X)
	case *ast.IndexListExpr:
		return unwrapCallee(node.X)
	default:
		return expression
	}
}

func interfaceDispatch(info *types.Info, expression ast.Expr, function *types.Func) bool {
	if selector, ok := expression.(*ast.SelectorExpr); ok {
		if selection := info.Selections[selector]; selection != nil && interfaceType(selection.Recv()) {
			return true
		}
	}
	signature, ok := function.Type().(*types.Signature)
	return ok && signature.Recv() != nil && interfaceType(signature.Recv().Type())
}

func interfaceType(value types.Type) bool {
	_, isInterface := types.Unalias(value).Underlying().(*types.Interface)
	return isInterface
}

func callName(expression ast.Expr) string {
	switch node := unwrapCallee(expression).(type) {
	case *ast.Ident:
		return node.Name
	case *ast.SelectorExpr:
		return callName(node.X) + "." + node.Sel.Name
	case *ast.FuncLit:
		return "$closure"
	default:
		return "<expression>"
	}
}
