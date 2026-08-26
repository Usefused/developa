package golang

import (
	"go/ast"
	"go/constant"
	"go/types"
	"path"
	"strconv"
	"strings"
)

func (a *callAnalyzer) signatureEvidence(required, candidate *types.Func) string {
	if signatureTypeKnown(required.Type()) && signatureTypeKnown(candidate.Type()) {
		if types.Identical(required.Type(), candidate.Type()) {
			return methodSetEvidence
		}
		return ""
	}
	left, leftOK := a.signatures[required.Origin()].fingerprint()
	right, rightOK := a.signatures[candidate.Origin()].fingerprint()
	if leftOK && rightOK && left == right && strings.Contains(left, "import(") {
		return conditionalSignatureEvidence
	}
	return ""
}

func (syntax methodSyntax) fingerprint() (string, bool) {
	if syntax.signature == nil || syntax.signature.TypeParams != nil {
		return "", false
	}
	params, paramsOK := syntax.parameterFingerprint(syntax.signature.Params)
	results, resultsOK := syntax.parameterFingerprint(syntax.signature.Results)
	return "func(" + params + ")(" + results + ")", paramsOK && resultsOK
}

func (syntax methodSyntax) parameterFingerprint(fields *ast.FieldList) (string, bool) {
	if fields == nil {
		return "", true
	}
	parts := []string{}
	for _, field := range fields.List {
		value, known := syntax.typeFingerprint(field.Type)
		if !known {
			return "", false
		}
		for range max(1, len(field.Names)) {
			parts = append(parts, value)
		}
	}
	return strings.Join(parts, ","), true
}

func (syntax methodSyntax) typeFingerprint(expression ast.Expr) (string, bool) {
	if variadic, ok := expression.(*ast.Ellipsis); ok {
		return syntax.prefixedFingerprint("...", variadic.Elt)
	}
	value := syntax.file.pkg.info.TypeOf(expression)
	if signatureTypeKnown(value) {
		return types.TypeString(types.Unalias(value), func(pkg *types.Package) string { return pkg.Path() }), true
	}
	switch typed := expression.(type) {
	case *ast.ParenExpr:
		return syntax.typeFingerprint(typed.X)
	case *ast.SelectorExpr:
		return syntax.importedTypeFingerprint(typed)
	case *ast.StarExpr:
		return syntax.prefixedFingerprint("*", typed.X)
	case *ast.ArrayType:
		return syntax.arrayFingerprint(typed)
	default:
		return syntax.compositeFingerprint(expression)
	}
}

func (syntax methodSyntax) prefixedFingerprint(prefix string, expression ast.Expr) (string, bool) {
	value, known := syntax.typeFingerprint(expression)
	return prefix + value, known
}

func (syntax methodSyntax) compositeFingerprint(expression ast.Expr) (string, bool) {
	switch typed := expression.(type) {
	case *ast.MapType:
		key, keyOK := syntax.typeFingerprint(typed.Key)
		value, valueOK := syntax.typeFingerprint(typed.Value)
		return "map[" + key + "]" + value, keyOK && valueOK
	case *ast.ChanType:
		return syntax.prefixedFingerprint(channelPrefix(typed.Dir), typed.Value)
	case *ast.FuncType:
		return (methodSyntax{file: syntax.file, signature: typed}).fingerprint()
	default:
		// Unknown local identifiers, generic instantiations and anonymous aggregate
		// shapes are not replaced with spelling-based guesses.
		return "", false
	}
}

func (syntax methodSyntax) arrayFingerprint(array *ast.ArrayType) (string, bool) {
	if array.Len == nil {
		return syntax.prefixedFingerprint("[]", array.Elt)
	}
	length := syntax.file.pkg.info.Types[array.Len].Value
	if length == nil {
		return "", false
	}
	if integer, exact := constant.Int64Val(length); !exact || integer < 0 {
		return "", false
	}
	return syntax.prefixedFingerprint("["+length.ExactString()+"]", array.Elt)
}

func channelPrefix(direction ast.ChanDir) string {
	switch direction {
	case ast.SEND:
		return "chan<- "
	case ast.RECV:
		return "<-chan "
	default:
		return "chan "
	}
}

func (syntax methodSyntax) importedTypeFingerprint(selector *ast.SelectorExpr) (string, bool) {
	qualifier, ok := selector.X.(*ast.Ident)
	if !ok || !ast.IsExported(selector.Sel.Name) {
		return "", false
	}
	if object := syntax.file.pkg.info.ObjectOf(qualifier); object != nil {
		if _, imported := object.(*types.PkgName); !imported {
			return "", false
		}
	}
	for _, spec := range syntax.file.ast.Imports {
		importPath, err := strconv.Unquote(spec.Path.Value)
		if err == nil && syntax.importName(spec, importPath) == qualifier.Name {
			return "import(" + strconv.Quote(importPath) + ")." + selector.Sel.Name, true
		}
	}
	return "", false
}

func (syntax methodSyntax) importName(spec *ast.ImportSpec, importPath string) string {
	if spec.Name != nil {
		return spec.Name.Name
	}
	if object, ok := syntax.file.pkg.info.Implicits[spec].(*types.PkgName); ok {
		return object.Name()
	}
	return path.Base(importPath)
}
