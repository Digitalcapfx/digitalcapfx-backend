package main

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
)

func main() {
	fset := token.NewFileSet()
	handlersDir := filepath.Join("internal", "api", "handlers")
	
	files, err := os.ReadDir(handlersDir)
	if err != nil {
		panic(err)
	}

	for _, f := range files {
		if !strings.HasSuffix(f.Name(), ".go") || strings.HasSuffix(f.Name(), "_test.go") {
			continue
		}
		
		path := filepath.Join(handlersDir, f.Name())
		node, err := parser.ParseFile(fset, path, nil, parser.ParseComments)
		if err != nil {
			panic(err)
		}

		for _, decl := range node.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok {
				continue
			}
			
			// Must be a method
			if fn.Recv == nil || len(fn.Recv.List) == 0 {
				continue
			}
			
			// Must be on a *Handler receiver
			starExpr, ok := fn.Recv.List[0].Type.(*ast.StarExpr)
			if !ok {
				continue
			}
			ident, ok := starExpr.X.(*ast.Ident)
			if !ok || !strings.HasSuffix(ident.Name, "Handler") {
				continue
			}
			
			// Must be exported
			if !fn.Name.IsExported() {
				continue
			}

			hasSummary := false
			if fn.Doc != nil {
				for _, c := range fn.Doc.List {
					if strings.Contains(c.Text, "@Summary") {
						hasSummary = true
						break
					}
				}
			}

			if !hasSummary {
				fmt.Printf("%s: %s\n", f.Name(), fn.Name.Name)
			}
		}
	}
}
