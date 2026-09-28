package platformmcp

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/speakeasy-api/gram/server/internal/risk/policycatalog"
	"github.com/stretchr/testify/require"
)

func TestSelfRemovalAudienceFailsClosed(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name, kind string
		audience   []string
		message    string
	}{
		{"everyone", "everyone", []string{"user:all"}, "everyone-except-one"},
		{"role", "targeted", []string{"user:self", "user:other", "role:organization:11111111-1111-4111-8111-111111111111"}, "role-containing"},
		{"last", "targeted", []string{"user:self"}, "empty"},
		{"missing", "targeted", []string{"user:other"}, "no explicit"},
		{"wildcard", "targeted", []string{"user:self", "user:all"}, "unsupported"},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, err := selfRemovalAudiencePatch(test.kind, test.audience, "self")
			require.ErrorContains(t, err, test.message)
		})
	}
	raw, err := selfRemovalAudiencePatch("targeted", []string{"user:self", "user:other"}, "self")
	require.NoError(t, err)
	value, _, err := parseRiskPolicyAudienceReplacement(raw)
	require.NoError(t, err)
	require.Equal(t, []string{"user:other"}, value.PrincipalURNs)
}

func TestSelfRemovalToolContract(t *testing.T) {
	t.Parallel()
	catalog, err := policycatalog.Build()
	require.NoError(t, err)
	called := false
	handler := func(context.Context, *mcp.CallToolRequest, map[string]any) (*mcp.CallToolResult, UpdateRiskPolicyToolOutput, error) {
		called = true
		return nil, UpdateRiskPolicyToolOutput{}, nil
	}
	for _, enabled := range []bool{false, true} {
		reg := newRegistrar(mcp.NewServer(&mcp.Implementation{Name: "test", Version: "test"}, nil))
		reg.withExternalAuthorizer(allowExternalCallAuthorizer{})
		registerRiskMutationHandlers(reg, catalog, enabled, &RiskMutationHandlers{Controls: &RiskMutationControls{}, RemoveSelf: handler})
		descriptor := descriptorByName(t, reg, operationRemoveSelfFromRiskPolicy)
		require.Equal(t, externalOnly, descriptor.Meta.Audiences)
		require.Equal(t, ExternalAuthorizationOrgAdmin, descriptor.Meta.Authorization)
		require.Equal(t, ProjectScopeExplicit, descriptor.Meta.ProjectScope)
		require.True(t, *descriptor.Annotations.DestructiveHint)
		require.True(t, descriptor.Annotations.IdempotentHint)
		for _, entry := range reg.For(AudienceAssistant) {
			require.NotEqual(t, operationRemoveSelfFromRiskPolicy, entry.Name)
		}
		schema := new(jsonschema.Schema)
		require.NoError(t, json.Unmarshal(descriptor.InputSchema, schema))
		resolved, err := schema.Resolve(nil)
		require.NoError(t, err)
		args := map[string]any{"project_slug": "default", "policy_id": "11111111-1111-4111-8111-111111111111", "expected_version": "version", "idempotency_key": "key", "confirmed": true}
		require.NoError(t, resolved.Validate(args))
		args["user_id"] = "someone-else"
		require.Error(t, resolved.Validate(args))
		delete(args, "user_id")
		args["confirmed"] = false
		require.Error(t, resolved.Validate(args))
		args["confirmed"] = true
		encoded, err := json.Marshal(args)
		require.NoError(t, err)
		_, err = descriptor.Invoke(ContextWithPrincipal(t.Context(), testRiskPrincipal("self")), encoded)
		if enabled {
			require.NoError(t, err)
			require.True(t, called)
		} else {
			var refusal *ToolRefusalError
			require.ErrorAs(t, err, &refusal)
			require.Contains(t, refusal.Payload, "feature_unavailable")
			require.False(t, called)
		}
	}
}

func TestSelfRemovalToolRequiresOrgAdmin(t *testing.T) {
	t.Parallel()
	catalog, err := policycatalog.Build()
	require.NoError(t, err)
	server := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "test"}, nil)
	bindExternalTestPrincipal(server)
	reg := newRegistrar(server)
	reg.withExternalAuthorizer(denyExternalCallAuthorizer{err: &ExternalAuthorizationError{RequiredScope: "org:admin", cause: ErrForbidden}})
	called := false
	registerRiskMutationHandlers(reg, catalog, true, &RiskMutationHandlers{Controls: &RiskMutationControls{}, RemoveSelf: func(context.Context, *mcp.CallToolRequest, map[string]any) (*mcp.CallToolResult, UpdateRiskPolicyToolOutput, error) {
		called = true
		return nil, UpdateRiskPolicyToolOutput{}, nil
	}})
	clientTransport, serverTransport := mcp.NewInMemoryTransports()
	serverSession, err := server.Connect(t.Context(), serverTransport, nil)
	require.NoError(t, err)
	defer func() { _ = serverSession.Close() }()
	client := mcp.NewClient(&mcp.Implementation{Name: "self-removal-authz-test", Version: "test"}, nil)
	session, err := client.Connect(t.Context(), clientTransport, nil)
	require.NoError(t, err)
	defer func() { _ = session.Close() }()
	result, err := session.CallTool(t.Context(), &mcp.CallToolParams{Name: operationRemoveSelfFromRiskPolicy, Arguments: map[string]any{"project_slug": "default", "policy_id": "11111111-1111-4111-8111-111111111111", "expected_version": "version", "idempotency_key": "key", "confirmed": true}})
	require.NoError(t, err)
	require.True(t, result.IsError)
	require.Contains(t, result.Content[0].(*mcp.TextContent).Text, "org:admin")
	require.False(t, called)
}
