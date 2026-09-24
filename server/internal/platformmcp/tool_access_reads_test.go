package platformmcp

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"
)

func TestAccessReadToolsAreExternalReadOnlyTools(t *testing.T) {
	t.Parallel()

	_, registrar := newServer(nil, nil, nil, "", nil, nil, nil, nil, nil, nil, nil, nil, CatalogDescriptor{})
	requireAccessReadToolDescriptors(t, registrar)
}

func TestAvailableAccessReadToolsAreExternalReadOnlyTools(t *testing.T) {
	t.Parallel()

	registrar := newRegistrar(mcp.NewServer(&mcp.Implementation{Name: "test", Version: "test"}, nil))
	registerAccessReadTools(registrar, nil)
	requireAccessReadToolDescriptors(t, registrar)
}

func requireAccessReadToolDescriptors(t *testing.T, registrar *Registrar) {
	t.Helper()

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
		require.NotNil(t, descriptor.Annotations)
		require.True(t, descriptor.Annotations.ReadOnlyHint)
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

func TestPrincipalToolCallRequiresPrincipal(t *testing.T) {
	t.Parallel()

	result, output, err := principalToolCall(t.Context(), func(error) (*mcp.CallToolResult, bool) {
		t.Fatal("principal errors must not be translated")
		return nil, false
	}, func(Principal) (string, error) {
		t.Fatal("call must not run without a principal")
		return "", nil
	})
	require.ErrorIs(t, err, ErrUnauthorized)
	require.Nil(t, result)
	require.Empty(t, output)
}

func TestPrincipalToolCallReturnsSuccessfulOutput(t *testing.T) {
	t.Parallel()

	principal := registrationServicePrincipal()
	ctx := contextWithPrincipal(t.Context(), principal)
	result, output, err := principalToolCall(ctx, accessReadToolResult, func(actual Principal) (string, error) {
		require.Equal(t, principal, actual)
		return "output", nil
	})
	require.NoError(t, err)
	require.Nil(t, result)
	require.Equal(t, "output", output)
}

func TestPrincipalToolCallPreservesUnexpectedErrors(t *testing.T) {
	t.Parallel()

	unexpected := errors.New("unexpected service failure")
	ctx := contextWithPrincipal(t.Context(), registrationServicePrincipal())
	for _, refusalResult := range []func(error) (*mcp.CallToolResult, bool){accessReadToolResult, pluginToolResult} {
		result, output, err := principalToolCall(ctx, refusalResult, func(Principal) (string, error) {
			return "partial output", unexpected
		})
		require.ErrorIs(t, err, unexpected)
		require.Nil(t, result)
		require.Empty(t, output)
	}
}

func TestPrincipalToolCallTranslatesKnownRefusals(t *testing.T) {
	t.Parallel()

	ctx := contextWithPrincipal(t.Context(), registrationServicePrincipal())
	for _, test := range []struct {
		refusalResult func(error) (*mcp.CallToolResult, bool)
		err           error
	}{
		{accessReadToolResult, ErrAccessQueryRequired},
		{accessReadToolResult, ErrAccessReferenceNotFound},
		{accessReadToolResult, ErrAccessMCPNotFound},
		{accessReadToolResult, ErrOperationRateLimited},
		{accessReadToolResult, ErrOperationBudgetUnavailable},
		{pluginToolResult, ErrPluginProjectNotFound},
		{pluginToolResult, ErrPluginNotFound},
		{pluginToolResult, ErrPluginAmbiguous},
		{pluginToolResult, ErrPluginCursorInvalid},
		{pluginToolResult, ErrOperationRateLimited},
		{pluginToolResult, ErrOperationBudgetUnavailable},
	} {
		result, output, err := principalToolCall(ctx, test.refusalResult, func(Principal) (string, error) {
			return "partial output", test.err
		})
		require.NoError(t, err)
		require.Empty(t, output)
		expected, ok := test.refusalResult(test.err)
		require.True(t, ok)
		require.Equal(t, expected, result)
		require.True(t, result.IsError)
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
