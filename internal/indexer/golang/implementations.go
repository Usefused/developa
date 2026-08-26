package golang

import (
	"go/types"
	"sort"
)

const (
	methodSetEvidence            = "go_types_method_set"
	conditionalSignatureEvidence = "signature_match_with_unavailable_types"
)

func initializeImplementations(result *Result) {
	result.Implementations = []Implementation{}
	result.ImplementationAnalysis = ImplementationAnalysis{Status: "complete", Limitations: []string{
		"Implementation candidates describe captured declaration method sets, not runtime receiver wiring or resolved dynamic calls.",
		"Only named, non-generic types in the deterministic typed file selection are enumerated; aliases do not duplicate relations and generic instantiations are not enumerated.",
		"All methods of each captured interface must match. Empty interfaces, constraint type sets, unavailable embeddings, and methods without captured concrete declarations are not enumerated.",
		"go_types_method_set means complete method signatures satisfy go/types; signature_match_with_unavailable_types is only a conditional candidate with matching import-qualified source signatures, not proof of implementation or compilation.",
		"Standard-library and external declarations are not loaded from the ambient environment. Missing types, excluded files, and unsupported declarations leave this analysis partial.",
		"Assignments, type assertions, reflection, dependency injection, and runtime receiver flow are not analyzed as wiring evidence.",
	}}
}

func (a *callAnalyzer) collectImplementations() {
	for _, declaration := range a.types {
		if a.ctx.Err() != nil {
			return
		}
		named := a.implementationType(declaration)
		if named == nil {
			continue
		}
		iface, ok := named.Underlying().(*types.Interface)
		if !ok || iface.NumMethods() == 0 {
			continue
		}
		if !interfaceShapeKnown(iface, map[types.Type]bool{}) {
			a.result.ImplementationAnalysis.Status = "partial"
			continue
		}
		a.collectInterfaceImplementations(declaration, iface)
	}
}

func (a *callAnalyzer) implementationType(declaration declaredType) *types.Named {
	if declaration.object.IsAlias() {
		return nil
	}
	named, ok := declaration.object.Type().(*types.Named)
	if !ok {
		return nil
	}
	if named.TypeParams().Len() != 0 {
		a.result.ImplementationAnalysis.Status = "partial"
		return nil
	}
	return named
}

func (a *callAnalyzer) collectInterfaceImplementations(declaration declaredType, iface *types.Interface) {
	for _, receiver := range a.types {
		if a.ctx.Err() != nil {
			return
		}
		named := a.implementationType(receiver)
		if named == nil || interfaceType(named) || receiver.target.file.pkg.module != declaration.target.file.pkg.module {
			continue
		}
		if !embeddedMethodSetsKnown(named, map[types.Type]bool{}) {
			a.result.ImplementationAnalysis.Status = "partial"
			continue
		}
		rows := a.matchImplementation(declaration, iface, receiver, named, false)
		if len(rows) == 0 {
			rows = a.matchImplementation(declaration, iface, receiver, types.NewPointer(named), true)
		}
		a.result.Implementations = append(a.result.Implementations, rows...)
	}
}

func (a *callAnalyzer) matchImplementation(declaration declaredType, iface *types.Interface, receiver declaredType, value types.Type, pointer bool) []Implementation {
	methods := types.NewMethodSet(value)
	rows := make([]Implementation, 0, iface.NumMethods())
	evidence := methodSetEvidence
	for index := 0; index < iface.NumMethods(); index++ {
		method := iface.Method(index)
		row, match := a.matchImplementationMethod(method, methods)
		if !match {
			return nil
		}
		if row.Evidence == conditionalSignatureEvidence {
			evidence = conditionalSignatureEvidence
		}
		row.Interface, row.Receiver, row.Pointer = *declaration.target.reference(), *receiver.target.reference(), pointer
		rows = append(rows, row)
	}
	if evidence == methodSetEvidence && !types.Implements(value, iface) {
		return nil
	}
	for index := range rows {
		rows[index].Evidence = evidence
	}
	return rows
}

func (a *callAnalyzer) matchImplementationMethod(method *types.Func, methods *types.MethodSet) (Implementation, bool) {
	selection := methods.Lookup(method.Pkg(), method.Name())
	if selection == nil {
		return Implementation{}, false
	}
	function, ok := selection.Obj().(*types.Func)
	if !ok || interfaceMethod(function) {
		return Implementation{}, false
	}
	target, declaration := a.functions[function.Origin()], a.declarations[method.Origin()]
	if target.reference() == nil || declaration.reference() == nil {
		return Implementation{}, false
	}
	if target.file.exclusion != "" || target.file.pkg.invalid {
		return Implementation{}, false
	}
	evidence := a.signatureEvidence(method, function)
	if evidence == "" {
		return Implementation{}, false
	}
	return Implementation{Method: *declaration.reference(), Target: *target.reference(), Evidence: evidence}, true
}

func interfaceMethod(function *types.Func) bool {
	signature := function.Type().(*types.Signature)
	return signature.Recv() != nil && interfaceType(signature.Recv().Type())
}

func (a *callAnalyzer) finishImplementations() {
	if len(a.result.CallAnalysis.Diagnostics) > 0 {
		a.result.ImplementationAnalysis.Status = "partial"
	}
	for _, row := range a.result.Implementations {
		if row.Evidence == conditionalSignatureEvidence {
			a.result.ImplementationAnalysis.Status = "partial"
		}
	}
	sort.Slice(a.result.Implementations, func(i, j int) bool {
		return implementationKey(a.result.Implementations[i]) < implementationKey(a.result.Implementations[j])
	})
}

func implementationKey(row Implementation) string {
	return row.Interface.SymbolID + ":" + row.Method.SymbolID + ":" + row.Receiver.SymbolID + ":" + row.Target.SymbolID
}
