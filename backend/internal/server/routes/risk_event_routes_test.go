package routes

import (
	"go/ast"
	"go/parser"
	"go/token"
	"strconv"
	"testing"
)

func TestAuthenticatedReadAliasesRegisterRiskEventMiddleware(t *testing.T) {
	wantPaths := map[string]bool{
		"/responses":          false,
		"/models":             false,
		"/antigravity/models": false,
	}

	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "gateway.go", nil, 0)
	if err != nil {
		t.Fatalf("parse gateway routes: %v", err)
	}

	ast.Inspect(file, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok || len(call.Args) == 0 {
			return true
		}
		selector, ok := call.Fun.(*ast.SelectorExpr)
		if !ok || selector.Sel.Name != "GET" {
			return true
		}
		receiver, ok := selector.X.(*ast.Ident)
		if !ok || receiver.Name != "r" {
			return true
		}
		pathLiteral, ok := call.Args[0].(*ast.BasicLit)
		if !ok || pathLiteral.Kind != token.STRING {
			return true
		}
		path, err := strconv.Unquote(pathLiteral.Value)
		if err != nil {
			return true
		}
		if _, tracked := wantPaths[path]; !tracked {
			return true
		}
		authIndex := -1
		groupIndex := -1
		riskIndex := -1
		for index, arg := range call.Args[1:] {
			if expressionContainsIdentifier(arg, "apiKeyAuth") {
				authIndex = index
			}
			if expressionContainsIdentifier(arg, "requireGroupAnthropic") {
				groupIndex = index
			}
			if expressionContainsIdentifier(arg, "riskEvents") {
				riskIndex = index
			}
		}
		wantPaths[path] = authIndex >= 0 && groupIndex > authIndex && riskIndex > groupIndex
		return true
	})

	for path, registered := range wantPaths {
		if !registered {
			t.Errorf("GET %s must register riskEvents after authentication", path)
		}
	}
}

func expressionContainsIdentifier(expression ast.Expr, name string) bool {
	found := false
	ast.Inspect(expression, func(node ast.Node) bool {
		if ident, ok := node.(*ast.Ident); ok && ident.Name == name {
			found = true
			return false
		}
		return !found
	})
	return found
}
