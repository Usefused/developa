package golang

import (
	"errors"
	"go/ast"
	"go/token"
	"go/types"
	"strings"
)

type snapshotImporter struct {
	analyzer *callAnalyzer
	caller   *callPackage
}

func (i snapshotImporter) Import(importPath string) (*types.Package, error) {
	if importPath == "unsafe" {
		return types.Unsafe, nil
	}
	local := i.analyzer.imports[importPath]
	if local == nil || local.module != i.caller.module {
		return nil, errors.New("declarations are unavailable in the captured local module")
	}
	if local.files[0].block.Package == "main" {
		return nil, errors.New("cannot import a command package")
	}
	return i.analyzer.checkPackage(local)
}

func (a *callAnalyzer) checkPackage(pkg *callPackage) (*types.Package, error) {
	if err := a.ctx.Err(); err != nil {
		return nil, err
	}
	if pkg.state == "checked" {
		return pkg.typed, nil
	}
	if pkg.state == "checking" {
		pkg.invalid = true
		return nil, errors.New("captured local import cycle")
	}
	pkg.state = "checking"
	pkg.info = newTypeInfo()
	config := types.Config{Importer: snapshotImporter{analyzer: a, caller: pkg}, Sizes: types.SizesFor("gc", "amd64"),
		GoVersion: pkg.goVersion, DisableUnusedImportCheck: true, Error: func(err error) { a.typeError(pkg, err) }}
	pkg.typed, _ = config.Check(pkg.path, a.fset, selectedFiles(pkg), pkg.info)
	pkg.state = "checked"
	a.registerFunctions(pkg)
	return pkg.typed, a.ctx.Err()
}

func newTypeInfo() *types.Info {
	return &types.Info{
		Types: map[ast.Expr]types.TypeAndValue{}, Defs: map[*ast.Ident]types.Object{},
		Uses: map[*ast.Ident]types.Object{}, Selections: map[*ast.SelectorExpr]*types.Selection{},
		Instances: map[*ast.Ident]types.Instance{}, Implicits: map[ast.Node]types.Object{},
	}
}

func (a *callAnalyzer) typeError(pkg *callPackage, err error) {
	typed, ok := err.(types.Error)
	if !ok {
		pkg.invalid = true
		return
	}
	position := a.fset.PositionFor(typed.Pos, false)
	file := a.byPath[position.Filename]
	if file == nil {
		pkg.invalid = true
		a.diagnostic("", "type_error", typed.Msg, typed.Pos)
		return
	}
	code := "type_error"
	if strings.HasPrefix(typed.Msg, "could not import ") {
		code = "import_unavailable"
	}
	diagnostic := a.diagnostic(file.source.Path, code, typed.Msg, typed.Pos)
	file.errors = append(file.errors, diagnostic)
	if code == "import_unavailable" && isDotImportPosition(file.ast, typed.Pos) {
		pkg.invalid = true
	}
	if affectsPackageBindings(typed.Msg) {
		// Missing imported parameter/field types do not change a uniquely bound
		// local function's identity. Namespace and receiver ambiguity can, so those
		// errors suppress package bindings; other errors suppress their callsites.
		pkg.invalid = true
	}
}

func affectsPackageBindings(message string) bool {
	for _, fragment := range []string{"redeclared", "already declared", "invalid receiver", "cannot define new methods", "duplicate method"} {
		if strings.Contains(message, fragment) {
			return true
		}
	}
	return false
}

func isDotImportPosition(file *ast.File, position token.Pos) bool {
	for _, spec := range file.Imports {
		if spec.Path.Pos() == position && spec.Name != nil && spec.Name.Name == "." {
			return true
		}
	}
	return false
}

func (a *callAnalyzer) registerFunctions(pkg *callPackage) {
	for _, file := range pkg.files {
		for _, declaration := range file.ast.Decls {
			node, ok := declaration.(*ast.FuncDecl)
			if !ok {
				continue
			}
			a.registerFunction(file, node)
		}
	}
}

func (a *callAnalyzer) registerFunction(file *callFile, declaration *ast.FuncDecl) {
	kind := Function
	if declaration.Recv != nil {
		kind = Method
	}
	symbol := a.symbolFor(file, declaration, kind)
	object, ok := file.pkg.info.Defs[declaration.Name].(*types.Func)
	if ok && symbol != nil {
		a.functions[object.Origin()] = callTarget{symbol: symbol, file: file}
		a.signatures[object.Origin()] = methodSyntax{file: file, signature: declaration.Type}
	}
}
