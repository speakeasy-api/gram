package gram

import (
	"go/ast"
	"go/parser"
	"go/token"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestTrialFixtureHandlerLocalOnly(t *testing.T) {
	t.Parallel()
	require.NotNil(t, newTrialFixtureHandler("local", nil, nil))
	for _, environment := range []string{"", "dev", "development", "staging", "production", "preview"} {
		require.Nil(t, newTrialFixtureHandler(environment, nil, nil), environment)
	}
}

// Inspect the actual startup options: a factory-only test would miss a startup
// path forgetting to install the callback (as dev-single-process once did).
// Parsing avoids booting services or invoking any billing/email providers.
func TestWorkerStartupPathsInstallTrialFixtureHandler(t *testing.T) {
	t.Parallel()
	for _, path := range []string{"start.go", "worker.go"} {
		fset := token.NewFileSet()
		file, err := parser.ParseFile(fset, path, nil, 0)
		require.NoError(t, err)
		workers := 0
		handlers := 0
		ast.Inspect(file, func(node ast.Node) bool {
			literal, ok := node.(*ast.CompositeLit)
			if !ok {
				return true
			}
			typ, ok := literal.Type.(*ast.SelectorExpr)
			if !ok || typ.Sel.Name != "WorkerOptions" {
				return true
			}
			pkg, ok := typ.X.(*ast.Ident)
			if !ok || pkg.Name != "background" {
				return true
			}
			workers++
			for _, element := range literal.Elts {
				field, ok := element.(*ast.KeyValueExpr)
				if !ok {
					continue
				}
				key, ok := field.Key.(*ast.Ident)
				if !ok || key.Name != "TrialFixtureHandler" {
					continue
				}
				call, ok := field.Value.(*ast.CallExpr)
				require.True(t, ok, path)
				factory, ok := call.Fun.(*ast.Ident)
				require.True(t, ok, path)
				require.Equal(t, "newTrialFixtureHandler", factory.Name, path)
				require.Len(t, call.Args, 3, path)
				for _, arg := range call.Args[1:] {
					dependency, ok := arg.(*ast.Ident)
					require.True(t, ok, path)
					require.NotEqual(t, "nil", dependency.Name, path)
				}
				environment, ok := call.Args[0].(*ast.CallExpr)
				require.True(t, ok, path)
				method, ok := environment.Fun.(*ast.SelectorExpr)
				require.True(t, ok, path)
				require.Equal(t, "String", method.Sel.Name, path)
				require.Len(t, environment.Args, 1, path)
				flag, ok := environment.Args[0].(*ast.BasicLit)
				require.True(t, ok, path)
				require.Equal(t, token.STRING, flag.Kind, path)
				require.Equal(t, `"environment"`, flag.Value, path)
				handlers++
			}
			return true
		})
		require.Positive(t, workers, path)
		require.Equal(t, workers, handlers, path)
	}
}
