package risk_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/risk"
	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/risk/policycore"
)

func TestListEnabledForMCP_FiltersByPrincipalAudience(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestRiskService(t)

	authCtx, _ := contextvalues.GetAuthContext(ctx)
	ctx = withExactAccessGrants(t, ctx, ti.conn,
		authz.Grant{Scope: authz.ScopeOrgAdmin, Selector: authz.NewSelector(authz.ScopeOrgAdmin, authCtx.ActiveOrganizationID)},
	)

	allServers := &types.RiskMCPScope{AllServers: true, Servers: []*types.RiskMCPServerScope{}}
	for _, policy := range []*gen.CreateRiskPolicyPayload{
		{Name: new("Everyone"), Sources: []string{"gitleaks"}, Action: "block", AudienceType: "everyone", McpScope: allServers},
		{Name: new("Targeted"), Sources: []string{"gitleaks"}, Action: "block", AudienceType: "targeted", AudiencePrincipalUrns: []string{"user:" + authCtx.UserID}, McpScope: allServers},
	} {
		_, err := ti.service.CreateRiskPolicy(ctx, policy)
		require.NoError(t, err)
	}
	_, agentID := agentRequestContextWithID(t, ctx, ti, "Roleless agent", "")

	core := policycore.New(ti.conn)
	list := func(principal *policycore.MCPPrincipal) []string {
		t.Helper()
		policies, err := core.ListEnabledForMCP(ctx, authCtx.ActiveOrganizationID, *authCtx.ProjectID, policycore.MCPTarget{
			ServerID:        uuid.New(),
			ToolName:        "send_message",
			ToolAnnotations: nil,
			PlatformToolset: true,
			Principal:       principal,
		})
		require.NoError(t, err)
		names := make([]string, 0, len(policies))
		for _, policy := range policies {
			names = append(names, policy.Name)
		}
		return names
	}

	require.ElementsMatch(t, []string{"Everyone", "Targeted"}, list(nil), "a listing without a principal ignores audience")
	require.ElementsMatch(t, []string{"Everyone", "Targeted"}, list(&policycore.MCPPrincipal{UserID: authCtx.UserID, AgentID: ""}))
	require.ElementsMatch(t, []string{"Everyone"}, list(&policycore.MCPPrincipal{UserID: "", AgentID: ""}), "an unattributed caller only gets everyone-audience policies")
	require.ElementsMatch(t, []string{"Everyone"}, list(&policycore.MCPPrincipal{UserID: "", AgentID: agentID.String()}))
}
