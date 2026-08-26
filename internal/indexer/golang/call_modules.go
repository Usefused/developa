package golang

import (
	"path"
	"sort"
	"strings"

	"golang.org/x/mod/modfile"
	"golang.org/x/mod/module"
)

type capturedModule struct {
	directory string
	path      string
	version   string
}

func (a *callAnalyzer) loadModules(files []SourceFile) {
	for _, source := range files {
		if a.ctx.Err() != nil {
			return
		}
		if path.Base(source.Path) != "go.mod" {
			continue
		}
		a.loadModule(source)
	}
	// Nested modules define their own import namespace instead of inheriting the
	// nearest enclosing module's path.
	sort.Slice(a.modules, func(i, j int) bool { return len(a.modules[i].directory) > len(a.modules[j].directory) })
}

func (a *callAnalyzer) loadModule(source SourceFile) {
	parsed, err := modfile.Parse(source.Path, source.Content, nil)
	if err != nil || parsed.Module == nil {
		a.diagnostic(source.Path, "invalid_module", "Captured go.mod could not establish a module path.", 0)
		a.modules = append(a.modules, capturedModule{directory: path.Dir(path.Clean(source.Path))})
		return
	}
	if err := module.CheckImportPath(parsed.Module.Mod.Path); err != nil {
		a.diagnostic(source.Path, "invalid_module", "Captured module path is not a valid import path.", 0)
		a.modules = append(a.modules, capturedModule{directory: path.Dir(path.Clean(source.Path))})
		return
	}
	entry := capturedModule{directory: path.Dir(path.Clean(source.Path)), path: parsed.Module.Mod.Path}
	if parsed.Go != nil {
		entry.version = "go" + parsed.Go.Version
	}
	if len(parsed.Replace) > 0 {
		a.diagnostic(source.Path, "replacements_not_evaluated", "Module replacements are not evaluated; only declared captured module paths are available.", 0)
	}
	a.modules = append(a.modules, entry)
}

func (a *callAnalyzer) packageIdentity(directory string) (string, string, string) {
	for _, module := range a.modules {
		relative, found := relativeDirectory(module.directory, directory)
		if found {
			if module.path == "" {
				return path.Join("snapshot.invalid", directory), "", module.directory
			}
			return path.Join(module.path, relative), module.version, module.directory
		}
	}
	return path.Join("snapshot.local", directory), "", "."
}

func relativeDirectory(root, directory string) (string, bool) {
	if root == directory {
		return "", true
	}
	if root == "." {
		return directory, true
	}
	if strings.HasPrefix(directory, root+"/") {
		return strings.TrimPrefix(directory, root+"/"), true
	}
	return "", false
}
