package toolkit

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// TestToolsDoNotInventExecutionDeadlines keeps tool lifetimes under the
// caller's context. Shell is the sole exception because its public schema
// exposes an explicit per-call timeout.
func TestToolsDoNotInventExecutionDeadlines(t *testing.T) {
	fset := token.NewFileSet()
	err := filepath.WalkDir(".", func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") || filepath.Base(path) == "shell.go" {
			return nil
		}

		file, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			return err
		}
		contextPackages := importedContextPackages(file)
		ast.Inspect(file, func(node ast.Node) bool {
			call, ok := node.(*ast.CallExpr)
			if !ok {
				return true
			}
			if contextPackages["."] {
				name, ok := call.Fun.(*ast.Ident)
				if ok && name.Name == "WithTimeout" {
					t.Errorf("%s: tool deadlines must come from the caller or an explicit tool parameter", fset.Position(call.Pos()))
					return true
				}
			}
			selector, ok := call.Fun.(*ast.SelectorExpr)
			if !ok || selector.Sel.Name != "WithTimeout" {
				return true
			}
			pkg, ok := selector.X.(*ast.Ident)
			if ok && contextPackages[pkg.Name] {
				t.Errorf("%s: tool deadlines must come from the caller or an explicit tool parameter", fset.Position(call.Pos()))
			}
			return true
		})
		return nil
	})
	if err != nil {
		t.Fatalf("scan toolkit sources: %v", err)
	}
}

func importedContextPackages(file *ast.File) map[string]bool {
	names := make(map[string]bool)
	for _, spec := range file.Imports {
		path, err := strconv.Unquote(spec.Path.Value)
		if err != nil || path != "context" {
			continue
		}
		name := "context"
		if spec.Name != nil {
			name = spec.Name.Name
		}
		names[name] = true
	}
	return names
}
