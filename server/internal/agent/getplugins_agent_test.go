package agent_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	mockidp "github.com/speakeasy-api/gram/dev-idp/pkg/testidp"
	gen "github.com/speakeasy-api/gram/server/gen/agent"
	agentsrepo "github.com/speakeasy-api/gram/server/internal/agents/repo"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// withAgentKeyAuth rewrites the request to look like an admitted agent-principal
// API key: no human user, no email, no transport scopes.
func withAgentKeyAuth(t *testing.T, ctx context.Context, ti *testInstance, name string) (context.Context, urn.Principal) {
	t.Helper()
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	agent, err := agentsrepo.New(ti.conn).CreateAgent(ctx, agentsrepo.CreateAgentParams{
		OrganizationID: ti.orgID, OwnerUserID: authCtx.UserID, Name: name,
	})
	require.NoError(t, err)
	actor := urn.NewPrincipal(urn.PrincipalTypeAgent, agent.ID.String())

	clone := *authCtx
	clone.UserID = ""
	clone.Email = nil
	clone.APIKeyScopes = nil
	clone.SessionID = nil
	credential := contextvalues.PrincipalCredential{AuthorizerUserID: authCtx.UserID, DelegatedGrants: nil, DelegatedGrantsVersion: 0}
	return contextvalues.WithPrincipalAPIKeyAuthorization(ctx, &clone, actor, credential), actor
}

func TestGetPlugins_AgentKeyResolvesAgentPrincipal(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestAgentService(t)

	publishMarketplace(t, ctx, ti.conn, ti.projectID, "tok")
	agentCtx, actor := withAgentKeyAuth(t, ctx, ti, "CI agent")
	agentTool := seedPlugin(t, ctx, ti.conn, ti.orgID, ti.projectID, "agent-tool")
	assignPlugin(t, ctx, ti.conn, agentTool, ti.orgID, actor.String())
	wildcardTool := seedPlugin(t, ctx, ti.conn, ti.orgID, ti.projectID, "wildcard-tool")
	assignPlugin(t, ctx, ti.conn, wildcardTool, ti.orgID, "*")
	humanTool := seedPlugin(t, ctx, ti.conn, ti.orgID, ti.projectID, "human-tool")
	assignPlugin(t, ctx, ti.conn, humanTool, ti.orgID, "email:"+mockidp.MockUserEmail)

	res, err := ti.service.GetPlugins(agentCtx, &gen.GetPluginsPayload{Email: new(mockidp.MockUserEmail)})
	require.NoError(t, err)

	require.ElementsMatch(t, []string{wantObservability, "agent-tool", "wildcard-tool"}, pluginSlugs(res),
		"a vouched email never widens an agent key's plugin set")
	require.NotNil(t, res.Principal)
	require.Equal(t, actor.String(), res.Principal.Urn)
	require.Equal(t, "CI agent", res.Principal.DisplayName)
}

func TestGetPlugins_HumanPollOmitsPrincipal(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestAgentService(t)

	publishMarketplace(t, ctx, ti.conn, ti.projectID, "tok")

	res, err := ti.service.GetPlugins(ctx, &gen.GetPluginsPayload{Email: new(mockidp.MockUserEmail)})
	require.NoError(t, err)
	require.Nil(t, res.Principal)
}

func TestGetPlugins_AgentKeyWritesNoSyncRow(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestAgentService(t)

	publishMarketplace(t, ctx, ti.conn, ti.projectID, "tok")
	agentCtx, _ := withAgentKeyAuth(t, ctx, ti, "Nightly agent")

	_, err := ti.service.GetPlugins(agentCtx, &gen.GetPluginsPayload{Email: new(mockidp.MockUserEmail)})
	require.NoError(t, err)

	synced, err := ti.service.ListSyncedUsers(ctx, &gen.ListSyncedUsersPayload{})
	require.NoError(t, err)
	require.Empty(t, synced.Users, "agent polls must not be attributed to the vouched email")
}
