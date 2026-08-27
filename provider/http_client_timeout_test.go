package provider_test

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

// TestProvidersDoNotHardCodeWholeRequestTimeouts prevents provider defaults
// from imposing deadlines that compete with the caller's context budget.
func TestProvidersDoNotHardCodeWholeRequestTimeouts(t *testing.T) {
	fset := token.NewFileSet()
	err := filepath.WalkDir(".", func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}

		file, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			return err
		}
		httpPackages := importedHTTPPackages(file)
		ast.Inspect(file, func(node ast.Node) bool {
			literal, ok := node.(*ast.CompositeLit)
			if !ok || !isHTTPClientType(literal.Type, httpPackages) {
				return true
			}
			for _, element := range literal.Elts {
				field, ok := element.(*ast.KeyValueExpr)
				if !ok {
					continue
				}
				name, ok := field.Key.(*ast.Ident)
				if ok && name.Name == "Timeout" {
					t.Errorf("%s: provider HTTP clients must leave whole-request timeouts to caller contexts", fset.Position(field.Pos()))
				}
			}
			return true
		})
		return nil
	})
	if err != nil {
		t.Fatalf("scan provider sources: %v", err)
	}
}

func importedHTTPPackages(file *ast.File) map[string]bool {
	names := make(map[string]bool)
	for _, spec := range file.Imports {
		path, err := strconv.Unquote(spec.Path.Value)
		if err != nil || path != "net/http" {
			continue
		}
		name := "http"
		if spec.Name != nil {
			name = spec.Name.Name
		}
		names[name] = true
	}
	return names
}

func isHTTPClientType(expr ast.Expr, httpPackages map[string]bool) bool {
	if httpPackages["."] {
		name, ok := expr.(*ast.Ident)
		if ok && name.Name == "Client" {
			return true
		}
	}
	selector, ok := expr.(*ast.SelectorExpr)
	if !ok || selector.Sel.Name != "Client" {
		return false
	}
	pkg, ok := selector.X.(*ast.Ident)
	return ok && httpPackages[pkg.Name]
}
