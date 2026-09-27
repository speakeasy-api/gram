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

func TestMCPConnectionMutationToolsHaveSameInputSchemaWhenUnavailable(t *testing.T) {
	t.Parallel()

	live := newRegistrar(mcp.NewServer(&mcp.Implementation{Name: "connection-mutation-live", Version: "0.0.1"}, nil))
	unavailable := newRegistrar(mcp.NewServer(&mcp.Implementation{Name: "connection-mutation-unavailable", Version: "0.0.1"}, nil))
	registerMCPConnectionMutationTools(live, &MCPConnectionMutationService{})
	registerMCPConnectionMutationTools(unavailable, nil)

	for _, name := range []string{setMCPAddressToolName, setMCPNetworkAccessToolName} {
		liveDescriptor := descriptorByName(t, live, name)
		unavailableDescriptor := descriptorByName(t, unavailable, name)
		require.JSONEq(t, string(liveDescriptor.InputSchema), string(unavailableDescriptor.InputSchema), name)
		require.Equal(t, liveDescriptor.Meta, unavailableDescriptor.Meta, name)
		require.Equal(t, externalOnly, liveDescriptor.Meta.Audiences, name)
		require.Equal(t, ProjectScopeExplicit, liveDescriptor.Meta.ProjectScope, name)
		require.NotEmpty(t, liveDescriptor.InputSchema, name)
		require.Contains(t, liveDescriptor.Description, "requests publication", name)
		require.Contains(t, liveDescriptor.Description, "not that publication", name)
		require.Contains(t, unavailableDescriptor.Description, "requests publication", name)
		args := []byte(`{"project_id":"project","target_kind":"mcp_server","target_id":"target","slug":"slug","expected_version":"version","idempotency_key":"key","confirmed":true}`)
		if name == setMCPNetworkAccessToolName {
			args = []byte(`{"project_id":"project","target_kind":"mcp_server","target_id":"target","mode":"private_only","expected_version":"version","idempotency_key":"key","confirmed":true}`)
		}
		_, err := unavailableDescriptor.Invoke(t.Context(), args)
		var refusal *ToolRefusalError
		require.ErrorAs(t, err, &refusal, name)
		require.Equal(t, unavailableCode, refusal.Code, name)
	}
}
