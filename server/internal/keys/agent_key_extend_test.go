package keys_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/keys"
	agentsrepo "github.com/speakeasy-api/gram/server/internal/agents/repo"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

func TestKeysService_ExtendAgentKeysAddsServerToExistingKey(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestKeysService(t)
	agentID, first := connectAgentFixture(t, ctx, ti)
	created, err := ti.service.CreateKey(ctx, &gen.CreateKeyPayload{
		AgentID: new(agentID.String()), Name: "gateway key", DelegatedGrantsVersion: new(2),
		RequestedGrants: []*gen.AgentPolicyGrantForm{connectForm(first)},
	})
	require.NoError(t, err)
	require.NotNil(t, created.Key)

	second := uuid.NewString()
	grantConnect(t, ctx, ti, agentID, second)
	auditBefore, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionKeyPolicyUpdate)
	require.NoError(t, err)

	extended, err := ti.service.ExtendAgentKeys(ctx, &gen.ExtendAgentKeysPayload{
		AgentID: agentID.String(), RequestedGrants: []*gen.AgentPolicyGrantForm{connectForm(second)},
	})
	require.NoError(t, err)
	require.Len(t, extended.Keys, 1)
	require.Equal(t, created.ID, extended.Keys[0].ID)
	require.ElementsMatch(t, []string{first, second}, requestedServers(extended.Keys[0]))

	_, err = ti.service.APIKeyAuth(t.Context(), *created.Key, agentAPIKeyScheme())
	require.NoError(t, err, "the same secret keeps working with its wider policy")

	listed, err := ti.service.ListKeys(ctx, &gen.ListKeysPayload{AgentID: new(agentID.String())})
	require.NoError(t, err)
	require.Len(t, listed.Keys, 1)
	require.ElementsMatch(t, []string{first, second}, requestedServers(listed.Keys[0]))

	auditAfter, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionKeyPolicyUpdate)
	require.NoError(t, err)
	require.Equal(t, auditBefore+1, auditAfter)

	again, err := ti.service.ExtendAgentKeys(ctx, &gen.ExtendAgentKeysPayload{
		AgentID: agentID.String(), RequestedGrants: []*gen.AgentPolicyGrantForm{connectForm(second)},
	})
	require.NoError(t, err)
	require.Empty(t, again.Keys, "a key that already holds the grant is left as it is")
}

func TestKeysService_ExtendAgentKeysRejectsServerOutsideAgentPolicy(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestKeysService(t)
	agentID, first := connectAgentFixture(t, ctx, ti)
	_, err := ti.service.CreateKey(ctx, &gen.CreateKeyPayload{
		AgentID: new(agentID.String()), Name: "gateway key", DelegatedGrantsVersion: new(2),
		RequestedGrants: []*gen.AgentPolicyGrantForm{connectForm(first)},
	})
	require.NoError(t, err)

	// The caller can reach this server, but the agent was never given it.
	ungranted := uuid.NewString()
	upsertGrant(t, ctx, ti, urn.NewPrincipal(urn.PrincipalTypeUser, testAuthContext(t, ctx).UserID), authz.ScopeMCPConnect, ungranted)

	_, err = ti.service.ExtendAgentKeys(ctx, &gen.ExtendAgentKeysPayload{
		AgentID: agentID.String(), RequestedGrants: []*gen.AgentPolicyGrantForm{connectForm(ungranted)},
	})
	requireOopsCode(t, err, oops.CodeForbidden)

	listed, err := ti.service.ListKeys(ctx, &gen.ListKeysPayload{AgentID: new(agentID.String())})
	require.NoError(t, err)
	require.Equal(t, []string{first}, requestedServers(listed.Keys[0]))
}

func TestKeysService_ExtendAgentKeysLeavesAllServersKeyAlone(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestKeysService(t)
	authCtx := testAuthContext(t, ctx)
	require.NotNil(t, authCtx.ProjectID)
	agentID, _ := connectAgentFixture(t, ctx, ti)
	wildcard := authz.Selector{authz.SelectorKeyResourceKind: authz.ResourceKindMCP, authz.SelectorKeyResourceID: "*", authz.SelectorKeyProjectID: authCtx.ProjectID.String()}
	upsertGrantSelector(t, ctx, ti, urn.NewPrincipal(urn.PrincipalTypeAgent, agentID.String()), authz.ScopeMCPConnect, wildcard)
	upsertGrantSelector(t, ctx, ti, urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID), authz.ScopeMCPConnect, wildcard)
	projectID := authCtx.ProjectID.String()
	_, err := ti.service.CreateKey(ctx, &gen.CreateKeyPayload{
		AgentID: new(agentID.String()), Name: "all servers key", DelegatedGrantsVersion: new(2),
		RequestedGrants: []*gen.AgentPolicyGrantForm{{
			Scope: string(authz.ScopeMCPConnect), Effect: "allow", Selector: &gen.AgentPolicySelector{ResourceKind: authz.ResourceKindMCP, ResourceID: "*", ProjectID: &projectID},
		}},
	})
	require.NoError(t, err)

	added := connectForm(uuid.NewString())
	added.Selector.ProjectID = &projectID
	extended, err := ti.service.ExtendAgentKeys(ctx, &gen.ExtendAgentKeysPayload{
		AgentID: agentID.String(), RequestedGrants: []*gen.AgentPolicyGrantForm{added},
	})
	require.NoError(t, err)
	require.Empty(t, extended.Keys, "a key that reaches every server in the project already reaches this one")
}

func TestKeysService_ExtendAgentKeysAcceptsOnlyServerConnectGrants(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestKeysService(t)
	agentID, projectID := createAgentKeyFixture(t, ctx, ti)

	_, err := ti.service.ExtendAgentKeys(ctx, &gen.ExtendAgentKeysPayload{
		AgentID: agentID.String(), RequestedGrants: agentKeyPayload(agentID, projectID).RequestedGrants,
	})
	requireOopsCode(t, err, oops.CodeBadRequest)
}

// connectAgentFixture is an agent, and its owner, that can both reach one
// server.
func connectAgentFixture(t *testing.T, ctx context.Context, ti *testInstance) (uuid.UUID, string) {
	t.Helper()
	authCtx := testAuthContext(t, ctx)
	agent, err := agentsrepo.New(ti.conn).CreateAgent(ctx, agentsrepo.CreateAgentParams{
		OrganizationID: authCtx.ActiveOrganizationID, OwnerUserID: authCtx.UserID, Name: "gateway-agent-" + uuid.NewString(),
	})
	require.NoError(t, err)
	serverID := uuid.NewString()
	grantConnect(t, ctx, ti, agent.ID, serverID)
	return agent.ID, serverID
}

func grantConnect(t *testing.T, ctx context.Context, ti *testInstance, agentID uuid.UUID, serverID string) {
	t.Helper()
	upsertGrant(t, ctx, ti, urn.NewPrincipal(urn.PrincipalTypeAgent, agentID.String()), authz.ScopeMCPConnect, serverID)
	upsertGrant(t, ctx, ti, urn.NewPrincipal(urn.PrincipalTypeUser, testAuthContext(t, ctx).UserID), authz.ScopeMCPConnect, serverID)
}

func connectForm(serverID string) *gen.AgentPolicyGrantForm {
	return &gen.AgentPolicyGrantForm{
		Scope: string(authz.ScopeMCPConnect), Effect: "allow", Selector: &gen.AgentPolicySelector{ResourceKind: authz.ResourceKindMCP, ResourceID: serverID},
	}
}

func requestedServers(key *gen.Key) []string {
	servers := make([]string, 0, len(key.DelegatedGrants.Requested))
	for _, grant := range key.DelegatedGrants.Requested {
		if grant.Scope == string(authz.ScopeMCPConnect) {
			servers = append(servers, grant.Selector.ResourceID)
		}
	}
	return servers
}
