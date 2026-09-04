package platformmcp

import (
	"encoding/json"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"
)

func TestAccessReadToolsAreExternalReadOnlyTools(t *testing.T) {
	t.Parallel()

	_, registrar := newServer(nil, nil, nil, "", nil, nil, nil, nil, nil, nil, nil, nil, CatalogDescriptor{})
	wanted := map[string]ProjectScope{
		"list_access_roles":   ProjectScopeNone,
		"list_access_members": ProjectScopeNone,
		"get_mcp_access":      ProjectScopeExplicit,
	}
	for _, descriptor := range registrar.Descriptors() {
		projectScope, ok := wanted[descriptor.Name]
		if !ok {
			continue
		}
		require.Equal(t, projectScope, descriptor.Meta.ProjectScope)
		require.Equal(t, externalOnly, descriptor.Meta.Audiences)
		require.NotEmpty(t, descriptor.InputSchema)
		delete(wanted, descriptor.Name)
	}
	require.Empty(t, wanted)

	tools := registrar.For(AudienceAssistant)
	for _, descriptor := range tools {
		require.NotContains(t, []string{"list_access_roles", "list_access_members", "get_mcp_access"}, descriptor.Name)
	}
}

func TestAccessReadToolRefusalsAreStructured(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name string
		err  error
		code string
	}{
		{name: "query", err: ErrAccessQueryRequired, code: "invalid_request"},
		{name: "reference", err: ErrAccessReferenceNotFound, code: "not_found"},
		{name: "mcp", err: ErrAccessMCPNotFound, code: "not_found"},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			result, ok := accessReadToolResult(test.err)
			require.True(t, ok)
			require.True(t, result.IsError)
			require.Len(t, result.Content, 1)
			text, ok := result.Content[0].(*mcp.TextContent)
			require.True(t, ok)
			var refusal accessReadRefusalResult
			require.NoError(t, json.Unmarshal([]byte(text.Text), &refusal))
			require.Equal(t, test.code, refusal.Code)
			require.NotEmpty(t, refusal.Message)
		})
	}
}
