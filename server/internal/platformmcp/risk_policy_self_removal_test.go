package platformmcp

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/speakeasy-api/gram/server/internal/risk/policycatalog"
	"github.com/stretchr/testify/require"
)

func TestSelfRemovalFailsClosedBeforeDependencies(t *testing.T) {
	t.Parallel()
	// A nil service proves admission, database transactions, and receipt replay
	// are unreachable, even for otherwise valid authenticated requests.
	var service *riskPolicyMutationService
	for _, authenticated := range []bool{false, true} {
		t.Run(fmt.Sprint(authenticated), func(t *testing.T) {
			t.Parallel()
			ctx := t.Context()
			if authenticated {
				ctx = ContextWithPrincipal(ctx, testRiskPrincipal("self"))
			}
			args := map[string]any{"project_slug": "default", "policy_id": "11111111-1111-4111-8111-111111111111", "expected_version": "version", "idempotency_key": "historical-receipt", "confirmed": true}
			for range 2 {
				result, output, err := service.removeSelfFromPolicyTool(ctx, nil, args)
				requireRiskMutationRefusal(t, err, unavailableCode)
				require.ErrorContains(t, err, "effective exclusion are unavailable pending organization-scoped coordination")
				require.Nil(t, result)
				require.Zero(t, output)
			}
			_, _, err := service.mutatePolicyTool(ctx, nil, operationRemoveSelfFromRiskPolicy)
			requireRiskMutationRefusal(t, err, unavailableCode)
		})
	}
}

func TestSelfRemovalToolContract(t *testing.T) {
	t.Parallel()
	catalog, err := policycatalog.Build()
	require.NoError(t, err)
	called := false
	handler := func(context.Context, *mcp.CallToolRequest, map[string]any) (*mcp.CallToolResult, UpdateRiskPolicyToolOutput, error) {
		called = true
		return riskMutationToolRefusal[UpdateRiskPolicyToolOutput](selfRemovalUnavailable())
	}
	reg := newRegistrar(mcp.NewServer(&mcp.Implementation{Name: "test", Version: "test"}, nil))
	reg.withExternalAuthorizer(allowExternalCallAuthorizer{})
	registerRiskMutationHandlers(reg, catalog, &RiskMutationHandlers{ChangeAudience: nil, CreatePolicy: nil, UpdatePolicy: nil, CreateExclusion: nil, UpdateExclusion: nil, Controls: &RiskMutationControls{}, RemoveSelf: handler})
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
	requireRiskMutationRefusal(t, err, unavailableCode)
	require.ErrorContains(t, err, "organization-scoped coordination")
	require.True(t, called)
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
	registerRiskMutationHandlers(reg, catalog, &RiskMutationHandlers{ChangeAudience: nil, CreatePolicy: nil, UpdatePolicy: nil, CreateExclusion: nil, UpdateExclusion: nil, Controls: &RiskMutationControls{}, RemoveSelf: func(context.Context, *mcp.CallToolRequest, map[string]any) (*mcp.CallToolResult, UpdateRiskPolicyToolOutput, error) {
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
	textContent, ok := result.Content[0].(*mcp.TextContent)
	require.True(t, ok)
	require.Contains(t, textContent.Text, "org:admin")
	require.False(t, called)
}
