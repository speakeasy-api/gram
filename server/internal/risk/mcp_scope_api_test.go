package risk_test

import (
	"context"
	"slices"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/risk"
	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	metamcprepo "github.com/speakeasy-api/gram/server/internal/metamcp/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
)

func TestRiskPolicyMCPScopeRoundTripsAndFiltersEnabledPolicies(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestRiskService(t)
	projectID, organizationID := riskTestProject(t, ctx)
	serverID, otherServerID, gatewayID := seedRiskMCPServers(t, ctx, ti, projectID, organizationID)

	allPolicy := createMCPScopedPolicy(t, ctx, ti, "All servers", true, nil)
	directPolicy := createMCPScopedPolicy(t, ctx, ti, "Direct search", true, &types.RiskMCPScope{Servers: []*types.RiskMCPServerScope{{
		McpServerID: serverID.String(),
		Tools:       []string{"search"},
	}}})
	gatewayPolicy := createMCPScopedPolicy(t, ctx, ti, "Gateway", true, &types.RiskMCPScope{Servers: []*types.RiskMCPServerScope{{
		McpServerID: gatewayID.String(),
	}}})
	createMCPScopedPolicy(t, ctx, ti, "Other server", true, &types.RiskMCPScope{Servers: []*types.RiskMCPServerScope{{
		McpServerID: otherServerID.String(),
		Tools:       []string{"search"},
	}}})
	createMCPScopedPolicy(t, ctx, ti, "Disabled", false, &types.RiskMCPScope{Servers: []*types.RiskMCPServerScope{{
		McpServerID: serverID.String(),
	}}})

	got, err := ti.service.GetRiskPolicy(ctx, &gen.GetRiskPolicyPayload{ID: directPolicy.ID})
	require.NoError(t, err)
	require.Equal(t, directPolicy.McpScope, got.McpScope)

	searchPolicies, err := ti.service.ListRiskPoliciesForMcpServer(ctx, &gen.ListRiskPoliciesForMcpServerPayload{
		McpServerID: serverID.String(),
		ToolName:    new("search"),
	})
	require.NoError(t, err)
	require.Equal(t, []string{"All servers", "Direct search", "Gateway"}, sortedPolicyNames(searchPolicies.Policies))

	otherToolPolicies, err := ti.service.ListRiskPoliciesForMcpServer(ctx, &gen.ListRiskPoliciesForMcpServerPayload{
		McpServerID: serverID.String(),
		ToolName:    new("write"),
	})
	require.NoError(t, err)
	require.Equal(t, []string{"All servers", "Gateway"}, sortedPolicyNames(otherToolPolicies.Policies))

	serverOnlyPolicies, err := ti.service.ListRiskPoliciesForMcpServer(ctx, &gen.ListRiskPoliciesForMcpServerPayload{
		McpServerID: serverID.String(),
	})
	require.NoError(t, err)
	require.Equal(t, []string{"All servers", "Direct search", "Gateway"}, sortedPolicyNames(serverOnlyPolicies.Policies))

	cleared, err := ti.service.UpdateRiskPolicy(ctx, &gen.UpdateRiskPolicyPayload{
		ID:       gatewayPolicy.ID,
		Name:     gatewayPolicy.Name,
		McpScope: &types.RiskMCPScope{Servers: []*types.RiskMCPServerScope{}},
	})
	require.NoError(t, err)
	require.Nil(t, cleared.McpScope)

	_, err = ti.service.CreateRiskPolicy(ctx, &gen.CreateRiskPolicyPayload{
		Name:    new("Foreign server"),
		Sources: []string{"destructive_tool"},
		Action:  "flag",
		McpScope: &types.RiskMCPScope{Servers: []*types.RiskMCPServerScope{{
			McpServerID: uuid.NewString(),
		}}},
	})
	require.ErrorContains(t, err, "does not belong to the project")

	require.NotNil(t, allPolicy)
}

func createMCPScopedPolicy(
	t *testing.T,
	ctx context.Context,
	ti *testInstance,
	name string,
	enabled bool,
	scope *types.RiskMCPScope,
) *types.RiskPolicy {
	t.Helper()
	policy, err := ti.service.CreateRiskPolicy(ctx, &gen.CreateRiskPolicyPayload{
		Name:     &name,
		Sources:  []string{"destructive_tool"},
		Enabled:  &enabled,
		Action:   "flag",
		McpScope: scope,
	})
	require.NoError(t, err)
	return policy
}

func riskTestProject(t *testing.T, ctx context.Context) (uuid.UUID, string) {
	t.Helper()
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)
	return *authCtx.ProjectID, authCtx.ActiveOrganizationID
}

func seedRiskMCPServers(
	t *testing.T,
	ctx context.Context,
	ti *testInstance,
	projectID uuid.UUID,
	organizationID string,
) (uuid.UUID, uuid.UUID, uuid.UUID) {
	t.Helper()
	fixtures := testrepo.New(ti.conn)
	toolsetID := uuid.New()
	_, err := fixtures.CreateToolsetFixture(ctx, testrepo.CreateToolsetFixtureParams{
		ID:             toolsetID,
		OrganizationID: organizationID,
		ProjectID:      projectID,
		Name:           "Risk scope tools",
		Slug:           "risk-scope-tools",
	})
	require.NoError(t, err)

	createServer := func() uuid.UUID {
		id := uuid.New()
		_, err := fixtures.CreateRemoteMCPServerFixture(ctx, testrepo.CreateRemoteMCPServerFixtureParams{
			ID:         id,
			ProjectID:  projectID,
			ToolsetID:  uuid.NullUUID{UUID: toolsetID, Valid: true},
			Visibility: "private",
		})
		require.NoError(t, err)
		return id
	}
	serverID := createServer()
	otherServerID := createServer()
	gatewayID := uuid.New()
	_, err = fixtures.CreateMCPGatewayFixture(ctx, testrepo.CreateMCPGatewayFixtureParams{
		ID:             gatewayID,
		OrganizationID: organizationID,
		ProjectID:      projectID,
		Name:           "Risk scope gateway",
	})
	require.NoError(t, err)
	_, err = metamcprepo.New(ti.conn).CreateMetaMCPMember(ctx, metamcprepo.CreateMetaMCPMemberParams{
		ProjectID:       projectID,
		MetaMcpServerID: gatewayID,
		McpServerID:     serverID,
		SortOrder:       0,
	})
	require.NoError(t, err)
	return serverID, otherServerID, gatewayID
}

func sortedPolicyNames(policies []*types.RiskPolicy) []string {
	names := make([]string, 0, len(policies))
	for _, policy := range policies {
		names = append(names, policy.Name)
	}
	slices.Sort(names)
	return names
}
