package mcp

import (
	"go/ast"
	"go/parser"
	"go/token"
	"testing"

	"github.com/stretchr/testify/require"
)

// The capturing recorder deliberately has no limiter. Keep public entry points
// wired to Start; recorder limiter tests separately exercise its actual ceiling.
func TestPublicConnectionStartsUseAdmission(t *testing.T) {
	t.Parallel()
	for _, path := range []string{"authnchallenge_authorize.go", "authnchallenge_firstparty.go"} {
		t.Run(path, func(t *testing.T) {
			t.Parallel()
			file, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
			require.NoError(t, err)
			starts := 0
			ast.Inspect(file, func(node ast.Node) bool {
				call, ok := node.(*ast.CallExpr)
				if !ok || len(call.Args) < 4 {
					return true
				}
				step, ok := call.Args[2].(*ast.SelectorExpr)
				if !ok || step.Sel.Name != "StepInboundAuthorize" {
					return true
				}
				result, ok := call.Args[3].(*ast.SelectorExpr)
				if !ok || result.Sel.Name != "ResultAccepted" {
					return true
				}
				method, ok := call.Fun.(*ast.SelectorExpr)
				require.True(t, ok)
				require.Equal(t, "Start", method.Sel.Name, "public initial rows must use recorder admission")
				starts++
				return true
			})
			require.Equal(t, 1, starts)
		})
	}
}
