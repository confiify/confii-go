// Copyright 2026 The Confii Contributors
// SPDX-License-Identifier: MIT

package confii_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPublicContextAPINaming(t *testing.T) {
	t.Parallel()

	root, err := os.Getwd()
	require.NoError(t, err)

	type declaration struct {
		key      string
		name     string
		position token.Position
	}

	fset := token.NewFileSet()
	declarations := make(map[string]declaration)
	var contextVariants []declaration

	err = filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			if path != root && (strings.HasPrefix(entry.Name(), ".") || entry.Name() == "bin" || entry.Name() == "graphify-out" || entry.Name() == "site" || entry.Name() == "vendor") {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}

		file, parseErr := parser.ParseFile(fset, path, nil, 0)
		if parseErr != nil {
			return parseErr
		}
		packageKey, relativeErr := filepath.Rel(root, filepath.Dir(path))
		if relativeErr != nil {
			return relativeErr
		}
		for _, node := range file.Decls {
			fn, ok := node.(*ast.FuncDecl)
			if !ok || !ast.IsExported(fn.Name.Name) {
				continue
			}
			receiver := receiverName(fn)
			decl := declaration{
				key:      packageKey + "|" + receiver + "|" + fn.Name.Name,
				name:     fn.Name.Name,
				position: fset.Position(fn.Name.Pos()),
			}
			declarations[decl.key] = decl
			if strings.HasSuffix(decl.name, "WithContext") {
				contextVariants = append(contextVariants, decl)
			}
			if strings.HasSuffix(decl.name, "Ctx") || (strings.HasSuffix(decl.name, "Context") && !strings.HasSuffix(decl.name, "WithContext")) {
				t.Errorf("%s:exported context API %s must use the OperationWithContext form", decl.position, decl.name)
			}
		}
		return nil
	})
	require.NoError(t, err)

	for _, variant := range contextVariants {
		baseKey := strings.TrimSuffix(variant.key, "WithContext")
		if _, ok := declarations[baseKey]; !ok {
			t.Errorf("%s:%s requires a context-free %s counterpart", variant.position, variant.name, strings.TrimSuffix(variant.name, "WithContext"))
		}
	}
}

func receiverName(fn *ast.FuncDecl) string {
	if fn.Recv == nil || len(fn.Recv.List) == 0 {
		return ""
	}
	expression := fn.Recv.List[0].Type
	if star, ok := expression.(*ast.StarExpr); ok {
		expression = star.X
	}
	if index, ok := expression.(*ast.IndexExpr); ok {
		expression = index.X
	}
	if ident, ok := expression.(*ast.Ident); ok {
		return ident.Name
	}
	return "unknown"
}
