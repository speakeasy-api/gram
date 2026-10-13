package risk_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/risk"
	accessrepo "github.com/speakeasy-api/gram/server/internal/access/repo"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/risk"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// workloadRequestContext makes ctx look like an admitted workload whose
// assigned agent is a new agent, optionally holding roleURN. withAgent false
// leaves the workload with no recorded agent.
func workloadRequestContext(t *testing.T, ctx context.Context, ti *testInstance, name, roleURN string, withAgent bool) context.Context {
	t.Helper()
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	_, agentID := agentRequestContextWithID(t, ctx, ti, name, roleURN)

	clone := *authCtx
	clone.UserID = ""
	clone.Email = nil
	clone.APIKeyScopes = nil
	clone.SessionID = nil
	workload := urn.NewWorkloadPrincipal(uuid.New(), "device:"+uuid.NewString())
	workloadCtx := contextvalues.WithAuthenticatedActor(ctx, &clone, workload)
	if withAgent {
		workloadCtx = contextvalues.WithWorkloadAgent(workloadCtx, urn.NewPrincipal(urn.PrincipalTypeAgent, agentID.String()))
	}
	return workloadCtx
}

func createAgentsRole(t *testing.T, ctx context.Context, ti *testInstance, slug string) accessrepo.CreateOrganizationRoleRow {
	t.Helper()
	authCtx, _ := contextvalues.GetAuthContext(ctx)
	now := time.Now().UTC()
	role, err := accessrepo.New(ti.conn).CreateOrganizationRole(ctx, accessrepo.CreateOrganizationRoleParams{
		OrganizationID:    authCtx.ActiveOrganizationID,
		WorkosSlug:        slug,
		WorkosName:        slug,
		WorkosDescription: conv.ToPGTextEmpty(""),
		WorkosCreatedAt:   conv.ToPGTimestamptz(now),
		WorkosUpdatedAt:   conv.ToPGTimestamptz(now),
		WorkosLastEventID: conv.ToPGTextEmpty(""),
	})
	require.NoError(t, err)
	return role
}

func TestScanner_LookupShadowMCPBlockingPolicy_WorkloadUsesItsAgentsRoles(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestRiskService(t)

	authCtx, _ := contextvalues.GetAuthContext(ctx)
	ctx = withExactAccessGrants(t, ctx, ti.conn,
		authz.Grant{Scope: authz.ScopeOrgAdmin, Selector: authz.NewSelector(authz.ScopeOrgAdmin, authCtx.ActiveOrganizationID)},
	)
	role := createAgentsRole(t, ctx, ti, "workload-agents")

	_, err := ti.service.CreateRiskPolicy(ctx, &gen.CreateRiskPolicyPayload{
		Name:                  new("Workload agent role shadow MCP"),
		Sources:               []string{"shadow_mcp"},
		AudienceType:          "targeted",
		AudiencePrincipalUrns: []string{role.RoleUrn},
		Action:                "block",
	})
	require.NoError(t, err)
	scanner := newShadowMCPTestScanner(t, ti)

	memberCtx := workloadRequestContext(t, ctx, ti, "Workload role agent", role.RoleUrn, true)
	policy, err := scanner.LookupShadowMCPBlockingPolicy(memberCtx, authCtx.ActiveOrganizationID, *authCtx.ProjectID, "")
	require.NoError(t, err)
	require.NotNil(t, policy, "a workload is blocked by a policy targeting its agent's role")
	require.Equal(t, "Workload agent role shadow MCP", policy.Name)

	outsiderCtx := workloadRequestContext(t, ctx, ti, "Workload outsider agent", "", true)
	policy, err = scanner.LookupShadowMCPBlockingPolicy(outsiderCtx, authCtx.ActiveOrganizationID, *authCtx.ProjectID, "")
	require.NoError(t, err)
	require.Nil(t, policy, "a workload whose agent lacks the role is not targeted")

	unassignedCtx := workloadRequestContext(t, ctx, ti, "Workload unrecorded agent", role.RoleUrn, false)
	policy, err = scanner.LookupShadowMCPBlockingPolicy(unassignedCtx, authCtx.ActiveOrganizationID, *authCtx.ProjectID, "")
	require.NoError(t, err)
	require.Nil(t, policy, "a workload with no recorded agent holds only user:all")
}

func TestScanner_LookupShadowMCPBlockingPolicy_EveryoneAudienceBlocksWorkload(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestRiskService(t)

	authCtx, _ := contextvalues.GetAuthContext(ctx)
	ctx = withExactAccessGrants(t, ctx, ti.conn,
		authz.Grant{Scope: authz.ScopeOrgAdmin, Selector: authz.NewSelector(authz.ScopeOrgAdmin, authCtx.ActiveOrganizationID)},
	)
	_, err := ti.service.CreateRiskPolicy(ctx, &gen.CreateRiskPolicyPayload{
		Name:                  new("Everyone shadow MCP for workloads"),
		Sources:               []string{"shadow_mcp"},
		AudienceType:          "everyone",
		AudiencePrincipalUrns: nil,
		Action:                "block",
	})
	require.NoError(t, err)
	scanner := newShadowMCPTestScanner(t, ti)

	for name, withAgent := range map[string]bool{"with agent": true, "without agent": false} {
		workloadCtx := workloadRequestContext(t, ctx, ti, "Everyone workload "+name, "", withAgent)
		policy, err := scanner.LookupShadowMCPBlockingPolicy(workloadCtx, authCtx.ActiveOrganizationID, *authCtx.ProjectID, "")
		require.NoError(t, err, name)
		require.NotNil(t, policy, "user:all audiences apply to workloads (%s)", name)
	}
}

func TestPolicyBypassEvaluator_WorkloadUsesItsAgentsRoleBypass(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestRiskService(t)

	authCtx, _ := contextvalues.GetAuthContext(ctx)
	role := createAgentsRole(t, ctx, ti, "workload-bypass-agents")

	policyID := "policy_workload_role_bypass"
	serverURL := "https://mcp.example.com/workload-role"
	selector := authz.NewSelector(authz.ScopeRiskPolicyBypass, policyID)
	selector[authz.SelectorKeyServerURL] = serverURL
	rolePrincipal, err := urn.ParsePrincipal(role.RoleUrn)
	require.NoError(t, err)
	require.NoError(t, authz.GrantResourceToPrincipals(ctx, ti.conn, authz.ResourceGrant{
		Resource: authz.Resource{
			OrganizationID: authCtx.ActiveOrganizationID,
			Scope:          authz.ScopeRiskPolicyBypass,
			ResourceID:     policyID,
		},
		Principals: []urn.Principal{rolePrincipal},
		Selector:   selector,
	}))
	target := risk.ShadowMCPServerPolicyBypassTarget(serverURL, "", serverURL)
	evaluation := risk.PolicyBypassEvaluation{
		OrganizationID: authCtx.ActiveOrganizationID,
		UserID:         "",
		PolicyID:       policyID,
		Target:         &target,
	}

	memberCtx := workloadRequestContext(t, ctx, ti, "Workload bypass agent", role.RoleUrn, true)
	require.True(t, risk.NewPolicyBypassEvaluator(testenv.NewLogger(t), ti.conn).CanBypass(memberCtx, evaluation),
		"a workload inherits its agent's role-based bypass")

	outsiderCtx := workloadRequestContext(t, ctx, ti, "Workload no-bypass agent", "", true)
	require.False(t, risk.NewPolicyBypassEvaluator(testenv.NewLogger(t), ti.conn).CanBypass(outsiderCtx, evaluation),
		"a workload whose agent lacks the role cannot bypass")
}
