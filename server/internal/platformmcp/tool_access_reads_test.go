package platformmcp

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"
)

// The stub catalogue (service not wired) and the live catalogue must declare
// the same audiences, or an audience would see a tool appear and disappear
// with deployment wiring rather than with a reviewed decision.
func TestAccessReadToolsAreReadOnlyWithStableAudiences(t *testing.T) {
	t.Parallel()

	_, registrar := newServer(nil, nil, nil, "", nil, nil, nil, nil, nil, nil, nil, nil, nil, CatalogDescriptor{})
	requireAccessReadToolDescriptors(t, registrar)
}

func TestAvailableAccessReadToolsAreReadOnlyWithStableAudiences(t *testing.T) {
	t.Parallel()

	registrar := newRegistrar(mcp.NewServer(&mcp.Implementation{Name: "test", Version: "test"}, nil))
	registerAccessReadTools(registrar, nil)
	requireAccessReadToolDescriptors(t, registrar)
}

// list_access_members is the one access read the assistant is admitted to: it
// replaces the managed toolset's organization user lookup, and it is the only
// one of the three that works without a project or a role reference. The
// other two stay external-only.
func requireAccessReadToolDescriptors(t *testing.T, registrar *Registrar) {
	t.Helper()

	wanted := map[string]struct {
		projectScope ProjectScope
		audiences    []Audience
	}{
		"list_access_roles":   {ProjectScopeNone, externalOnly},
		"list_access_members": {ProjectScopeNone, bothAudiences},
		"get_mcp_access":      {ProjectScopeExplicit, externalOnly},
	}
	for _, descriptor := range registrar.Descriptors() {
		expected, ok := wanted[descriptor.Name]
		if !ok {
			continue
		}
		require.NotNil(t, descriptor.Annotations)
		require.True(t, descriptor.Annotations.ReadOnlyHint)
		require.Equal(t, expected.projectScope, descriptor.Meta.ProjectScope)
		require.Equal(t, expected.audiences, descriptor.Meta.Audiences, descriptor.Name)
		require.NotEmpty(t, descriptor.InputSchema)
		delete(wanted, descriptor.Name)
	}
	require.Empty(t, wanted)

	assistant := names(registrar.For(AudienceAssistant))
	require.Contains(t, assistant, "list_access_members")
	require.NotContains(t, assistant, "list_access_roles")
	require.NotContains(t, assistant, "get_mcp_access")
}

// The assistant cannot obtain a role reference (list_access_roles is
// external-only), so its only path is the identity query. The description is
// what tells a model that identities come back masked and that a member
// reference is spent only by assign_mcp_access_role.
func TestListAccessMembersDescriptionExplainsMaskingAndReferences(t *testing.T) {
	t.Parallel()

	registrar := newRegistrar(mcp.NewServer(&mcp.Implementation{Name: "test", Version: "test"}, nil))
	registerAccessReadTools(registrar, nil)
	descriptor := descriptorByName(t, registrar, "list_access_members")
	for _, fragment := range []string{
		"three characters",
		"External clients may instead filter by a role reference",
		"not available to the project assistant",
		"must use the identity query",
		"masked",
		"at least five people",
		"assign_mcp_access_role",
	} {
		require.Contains(t, descriptor.Description, fragment)
	}

	var schema struct {
		Properties map[string]json.RawMessage `json:"properties"`
	}
	require.NoError(t, json.Unmarshal(descriptor.InputSchema, &schema))
	require.Contains(t, schema.Properties, "query")
	require.Contains(t, schema.Properties, "role_reference")
	require.NotContains(t, schema.Properties, "display_name", "identities stay masked; no unmasked field is exposed")
}

// The assistant advertises the descriptor's schema to a model. A stub with a
// different schema would teach the model one contract and then enforce another
// the moment the service is wired.
func TestListAccessMembersStubAdvertisesLiveInputSchema(t *testing.T) {
	t.Parallel()

	live := newRegistrar(mcp.NewServer(&mcp.Implementation{Name: "test", Version: "test"}, nil))
	registerAccessReadTools(live, nil)
	stub := newRegistrar(mcp.NewServer(&mcp.Implementation{Name: "test", Version: "test"}, nil))
	registerUnavailableAccessReadTools(stub)

	liveSchema := descriptorByName(t, live, "list_access_members").InputSchema
	stubSchema := descriptorByName(t, stub, "list_access_members").InputSchema
	require.JSONEq(t, string(liveSchema), string(stubSchema))
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
