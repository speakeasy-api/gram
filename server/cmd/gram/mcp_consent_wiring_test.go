package gram

import (
	"bytes"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"testing"

	"github.com/stretchr/testify/require"
)

// The dedicated listener deliberately owns its composition rather than sharing
// the monolith's startup. Pin this security-sensitive wiring in both roots:
// consent's integration tests cannot catch a missing production setter.
func TestMCPConsentBindingWiringMatchesMonolith(t *testing.T) {
	t.Parallel()

	authorizers := make(map[string]string, 2)
	for _, path := range []string{"start.go", "mcp.go"} {
		fset := token.NewFileSet()
		source, err := parser.ParseFile(fset, path, nil, 0)
		require.NoError(t, err)
		var authorizer, binding, serving *ast.CallExpr
		ast.Inspect(source, func(node ast.Node) bool {
			call, ok := node.(*ast.CallExpr)
			if !ok {
				return true
			}
			selector, ok := call.Fun.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			receiver, ok := selector.X.(*ast.Ident)
			if !ok {
				return true
			}
			switch receiver.Name + "." + selector.Sel.Name {
			case "remoteSessionsService.SetBindingAuthorizer":
				require.Nil(t, authorizer, path)
				authorizer = call
			case "mcpService.SetConsentBindingService":
				require.Nil(t, binding, path)
				binding = call
			case "mcpService.StartRemoteSessionRecheck":
				serving = call
			}
			return true
		})
		require.NotNil(t, authorizer, path+" must configure the transactional owner authorizer")
		require.NotNil(t, binding, path+" must wire consent attachment management")
		require.Less(t, authorizer.Pos(), binding.Pos(), path)
		if path == "mcp.go" {
			require.NotNil(t, serving)
			require.Less(t, binding.Pos(), serving.Pos(), "wire before starting the MCP service")
		}
		require.Len(t, binding.Args, 1)
		service, ok := binding.Args[0].(*ast.Ident)
		require.True(t, ok)
		require.Equal(t, "remoteSessionsService", service.Name)
		require.Len(t, authorizer.Args, 1)
		var body bytes.Buffer
		require.NoError(t, format.Node(&body, fset, authorizer.Args[0]))
		authorizers[path] = body.String()
	}
	require.Equal(t, authorizers["start.go"], authorizers["mcp.go"], "both listeners must enforce identical rollout, authenticated-human and transactional owner checks")
}
