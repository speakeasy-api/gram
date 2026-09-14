package risk_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	meteringv1 "github.com/speakeasy-api/gram/infra/gen/gram/metering/v1"
	"github.com/speakeasy-api/gram/infra/pkg/gcp"
	gen "github.com/speakeasy-api/gram/server/gen/risk"
	accessrepo "github.com/speakeasy-api/gram/server/internal/access/repo"
	agentsrepo "github.com/speakeasy-api/gram/server/internal/agents/repo"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/metering"
	"github.com/speakeasy-api/gram/server/internal/risk"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// agentRequestContext makes ctx look like an admitted agent-principal key for
// a new agent, optionally assigned roleURN.
func agentRequestContext(t *testing.T, ctx context.Context, ti *testInstance, name, roleURN string) context.Context {
	t.Helper()
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	agent, err := agentsrepo.New(ti.conn).CreateAgent(ctx, agentsrepo.CreateAgentParams{
		OrganizationID: authCtx.ActiveOrganizationID, OwnerUserID: authCtx.UserID, Name: name,
	})
	require.NoError(t, err)
	if roleURN != "" {
		_, err = accessrepo.New(ti.conn).UpsertAgentRoleAssignment(ctx, accessrepo.UpsertAgentRoleAssignmentParams{
			OrganizationID: authCtx.ActiveOrganizationID, RoleUrn: roleURN, AgentID: agent.ID,
		})
		require.NoError(t, err)
	}

	clone := *authCtx
	clone.UserID = ""
	clone.Email = nil
	clone.APIKeyScopes = nil
	clone.SessionID = nil
	credential := contextvalues.PrincipalCredential{AuthorizerUserID: authCtx.UserID, DelegatedGrants: nil, DelegatedGrantsVersion: 0}
	return contextvalues.WithPrincipalAPIKeyAuthorization(ctx, &clone, urn.NewPrincipal(urn.PrincipalTypeAgent, agent.ID.String()), credential)
}

func TestScanner_LookupShadowMCPBlockingPolicy_AgentRoleAudience(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestRiskService(t)

	authCtx, _ := contextvalues.GetAuthContext(ctx)
	ctx = withExactAccessGrants(t, ctx, ti.conn,
		authz.Grant{Scope: authz.ScopeOrgAdmin, Selector: authz.NewSelector(authz.ScopeOrgAdmin, authCtx.ActiveOrganizationID)},
	)

	now := time.Now().UTC()
	role, err := accessrepo.New(ti.conn).CreateOrganizationRole(ctx, accessrepo.CreateOrganizationRoleParams{
		OrganizationID:    authCtx.ActiveOrganizationID,
		WorkosSlug:        "hooks-agents",
		WorkosName:        "Hooks agents",
		WorkosDescription: conv.ToPGTextEmpty(""),
		WorkosCreatedAt:   conv.ToPGTimestamptz(now),
		WorkosUpdatedAt:   conv.ToPGTimestamptz(now),
		WorkosLastEventID: conv.ToPGTextEmpty(""),
	})
	require.NoError(t, err)

	_, err = ti.service.CreateRiskPolicy(ctx, &gen.CreateRiskPolicyPayload{
		Name:                  new("Agent role shadow MCP"),
		Sources:               []string{"shadow_mcp"},
		AudienceType:          "targeted",
		AudiencePrincipalUrns: []string{role.RoleUrn},
		Action:                "block",
	})
	require.NoError(t, err)

	scanner, err := risk.NewScanner(
		testenv.NewLogger(t),
		testenv.NewTracerProvider(t),
		testenv.NewMeterProvider(t),
		ti.conn,
		newTestCustomRuleAnalyzer(t, ti.conn),
		nil,
		nil,
		nil,
		nil,
		testCELEngine(t), metering.NewRiskRecorder(gcp.NewNoopPublisher[*meteringv1.MeterReading]()))
	require.NoError(t, err)

	memberCtx := agentRequestContext(t, ctx, ti, "Role member agent", role.RoleUrn)
	memberPolicy, err := scanner.LookupShadowMCPBlockingPolicy(memberCtx, authCtx.ActiveOrganizationID, *authCtx.ProjectID, "")
	require.NoError(t, err)
	require.NotNil(t, memberPolicy, "an agent holding the targeted role is blocked")
	require.Equal(t, "Agent role shadow MCP", memberPolicy.Name)

	outsiderCtx := agentRequestContext(t, ctx, ti, "Outsider agent", "")
	outsiderPolicy, err := scanner.LookupShadowMCPBlockingPolicy(outsiderCtx, authCtx.ActiveOrganizationID, *authCtx.ProjectID, "")
	require.NoError(t, err)
	require.Nil(t, outsiderPolicy, "an agent without the targeted role is not blocked")
}
