package golang

import (
	"go/ast"
	"go/parser"
	"path"
	"sort"
	"strings"
)

func (a *callAnalyzer) loadFiles(sources []SourceFile) {
	blocks := map[string]*FileBlock{}
	for index := range a.result.Files {
		block := &a.result.Files[index]
		blocks[block.Path] = block
	}
	ordered := append([]SourceFile(nil), sources...)
	sort.SliceStable(ordered, func(i, j int) bool { return ordered[i].Path < ordered[j].Path })
	for _, source := range ordered {
		if a.ctx.Err() != nil {
			return
		}
		source.Path = path.Clean(source.Path)
		block := blocks[source.Path]
		if block == nil || a.byPath[source.Path] != nil {
			continue
		}
		a.loadCallFile(source, block)
	}
	for file := range blocks {
		if a.byPath[file] == nil {
			a.diagnostic(file, "source_unavailable", "Syntax record has no matching captured source for call analysis.", 0)
		}
	}
}

func (a *callAnalyzer) loadCallFile(source SourceFile, block *FileBlock) {
	if hashBytes(source.Content) != block.ContentHash {
		a.diagnostic(source.Path, "source_mismatch", "Captured source does not match the syntax index hash.", 0)
		return
	}
	parsed, err := parser.ParseFile(a.fset, source.Path, source.Content, parser.ParseComments|parser.AllErrors|parser.SkipObjectResolution)
	if parsed == nil {
		a.diagnostic(source.Path, "syntax_unavailable", "Source cannot be parsed for call analysis.", 0)
		return
	}
	file := &callFile{source: source, ast: parsed, block: block, symbols: map[int][]*Symbol{}}
	for index := range block.Symbols {
		symbol := &block.Symbols[index]
		file.symbols[symbol.Span.Start.Offset] = append(file.symbols[symbol.Span.Start.Offset], symbol)
	}
	file.exclusion = callFileExclusion(source.Path, parsed)
	if err != nil {
		file.exclusion = "syntax_errors"
	}
	if file.exclusion != "" {
		a.diagnostic(source.Path, file.exclusion, "File is excluded from the deterministic typed selection; its callsites remain unresolved.", parsed.Package)
	}
	a.files = append(a.files, file)
	a.byPath[source.Path] = file
}

func callFileExclusion(filename string, file *ast.File) string {
	base := path.Base(filename)
	if strings.HasSuffix(base, "_test.go") {
		return "test_variant_excluded"
	}
	if strings.HasPrefix(base, ".") || strings.HasPrefix(base, "_") {
		return "ignored_go_filename"
	}
	if platformFilename(base) {
		return "platform_variant_excluded"
	}
	if hasBuildConstraint(file) {
		return "build_constraint_excluded"
	}
	return ""
}

func platformFilename(filename string) bool {
	parts := strings.Split(strings.TrimSuffix(filename, ".go"), "_")
	if len(parts) < 2 {
		return false
	}
	targets := " aix android darwin dragonfly freebsd hurd illumos ios js linux nacl netbsd openbsd plan9 solaris wasip1 windows zos 386 amd64 amd64p32 arm armbe arm64 arm64be loong64 mips mipsle mips64 mips64le mips64p32 mips64p32le ppc ppc64 ppc64le riscv riscv64 s390 s390x sparc sparc64 wasm "
	return strings.Contains(targets, " "+parts[len(parts)-1]+" ")
}

func hasBuildConstraint(file *ast.File) bool {
	for _, group := range file.Comments {
		if group.Pos() > file.Package {
			break
		}
		for _, comment := range group.List {
			if strings.HasPrefix(comment.Text, "//go:build ") || strings.HasPrefix(comment.Text, "// +build ") {
				return true
			}
		}
	}
	return false
}

func (a *callAnalyzer) groupPackages() {
	groups := map[string]*callPackage{}
	for _, file := range a.files {
		importPath, version, module := a.packageIdentity(path.Dir(file.source.Path))
		key := path.Dir(file.source.Path) + "\x00" + file.block.Package
		pkg := groups[key]
		if pkg == nil {
			pkg = &callPackage{path: importPath, goVersion: version, module: module}
			groups[key] = pkg
			a.packages = append(a.packages, pkg)
		}
		file.pkg = pkg
		pkg.files = append(pkg.files, file)
	}
	for _, pkg := range a.packages {
		a.registerImportPackage(pkg)
	}
}

func (a *callAnalyzer) registerImportPackage(pkg *callPackage) {
	if len(selectedFiles(pkg)) == 0 {
		return
	}
	if previous := a.imports[pkg.path]; previous != nil {
		previous.invalid, pkg.invalid = true, true
		a.diagnostic(pkg.files[0].source.Path, "ambiguous_package", "Multiple captured packages have the same import path.", pkg.files[0].ast.Package)
		return
	}
	a.imports[pkg.path] = pkg
}

func selectedFiles(pkg *callPackage) []*ast.File {
	var files []*ast.File
	for _, file := range pkg.files {
		if file.exclusion == "" {
			files = append(files, file.ast)
		}
	}
	return files
}

func (a *callAnalyzer) symbolFor(file *callFile, node ast.Node, kind Kind) *Symbol {
	offset := a.fset.PositionFor(node.Pos(), false).Offset
	for _, symbol := range file.symbols[offset] {
		if symbol.Kind == kind {
			return symbol
		}
	}
	return nil
}

func (a *callAnalyzer) callSpan(node ast.Node) Span {
	return Span{Start: sourcePosition(a.fset.PositionFor(node.Pos(), false)), End: sourcePosition(a.fset.PositionFor(node.End(), false))}
}
