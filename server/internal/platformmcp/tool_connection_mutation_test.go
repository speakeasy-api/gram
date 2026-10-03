package platformmcp

import (
	"errors"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/stretchr/testify/require"
)

func TestMCPConnectionMutationToolProjectsScopedPermissionDenials(t *testing.T) {
	t.Parallel()

	denied := connectionMutationAuthorizationError(oops.E(oops.CodeForbidden, nil, "denied"), authz.ScopeMCPWrite)
	result, ok := mcpConnectionMutationToolResult(denied)
	require.True(t, ok)
	require.True(t, result.IsError)
	require.Len(t, result.Content, 1)
	text, ok := result.Content[0].(*mcp.TextContent)
	require.True(t, ok)
	require.JSONEq(t, `{"code":"permission_denied","required_scope":"mcp:write","message":"You do not have permission to use this Platform MCP tool. This action requires mcp:write. Ask an organization administrator to review your access."}`, text.Text)

	other := errors.New("backend unavailable")
	require.ErrorIs(t, connectionMutationAuthorizationError(other, authz.ScopeMCPWrite), other)
}
