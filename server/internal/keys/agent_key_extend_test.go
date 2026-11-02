package keys_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/keys"
	accessrepo "github.com/speakeasy-api/gram/server/internal/access/repo"
	agentsrepo "github.com/speakeasy-api/gram/server/internal/agents/repo"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/mcpservers/upstreamclients/authclientbackfill"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
	"github.com/speakeasy-api/gram/server/internal/urn"
	usersrepo "github.com/speakeasy-api/gram/server/internal/users/repo"
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
		AgentID: agentID.String(), RequestedGrants: []*gen.ExtendAgentKeyGrantForm{extendForm(second)},
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
		AgentID: agentID.String(), RequestedGrants: []*gen.ExtendAgentKeyGrantForm{extendForm(second)},
	})
	require.NoError(t, err)
	require.Empty(t, again.Keys, "a key that already holds the grant is left as it is")
}

func TestKeysService_ExtendAgentKeysRecordsCredentialForAddedServer(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestKeysService(t)
	authCtx := testAuthContext(t, ctx)
	require.NotNil(t, authCtx.ProjectID)
	projectID := *authCtx.ProjectID
	agentID, first := connectAgentFixture(t, ctx, ti)
	created, err := ti.service.CreateKey(ctx, &gen.CreateKeyPayload{
		AgentID: new(agentID.String()), Name: "gateway key", DelegatedGrantsVersion: new(2),
		RequestedGrants: []*gen.AgentPolicyGrantForm{connectForm(first)},
	})
	require.NoError(t, err)

	// A server offering a shared credential, added to the agent after the key.
	fixtures := testrepo.New(ti.conn)
	remoteID := uuid.New()
	require.NoError(t, fixtures.CreateRemoteMCPBackendFixture(ctx, testrepo.CreateRemoteMCPBackendFixtureParams{ID: remoteID, ProjectID: projectID, Url: "https://upstream.example.com/" + remoteID.String()}))
	serverID := uuid.New()
	require.NoError(t, fixtures.CreateRemoteBackedMCPServerFixture(ctx, testrepo.CreateRemoteBackedMCPServerFixtureParams{
		ID: serverID, ProjectID: projectID, Slug: conv.ToPGText("srv-" + serverID.String()[:8]), RemoteMcpServerID: uuid.NullUUID{UUID: remoteID, Valid: true}, Visibility: "private",
	}))
	clients := authclientbackfill.New(ti.conn)
	shared, err := clients.SeedAuthClientFixture(ctx, authclientbackfill.SeedAuthClientFixtureParams{OrganizationID: authCtx.ActiveOrganizationID, ProjectID: uuid.NullUUID{UUID: projectID, Valid: true}, ClientType: authz.AuthClientTypeStaticCredential, Source: "admin"})
	require.NoError(t, err)
	require.NoError(t, clients.SeedServerAuthClientLinkFixture(ctx, authclientbackfill.SeedServerAuthClientLinkFixtureParams{ProjectID: projectID, OrganizationID: authCtx.ActiveOrganizationID, McpServerID: serverID, AuthClientID: shared, Source: "admin"}))
	grantConnect(t, ctx, ti, agentID, serverID.String())

	_, err = ti.service.ExtendAgentKeys(ctx, &gen.ExtendAgentKeysPayload{
		AgentID:           agentID.String(),
		RequestedGrants:   []*gen.ExtendAgentKeyGrantForm{extendForm(serverID.String())},
		AuthClientChoices: []*gen.AgentKeyAuthClientChoice{{McpServerID: serverID.String(), AuthClientID: new(shared.String())}},
	})
	require.NoError(t, err)

	choice, err := clients.GetSelectionFixture(ctx, authclientbackfill.GetSelectionFixtureParams{CredentialID: uuid.MustParse(created.ID), McpServerID: serverID})
	require.NoError(t, err)
	require.Equal(t, uuid.NullUUID{UUID: shared, Valid: true}, choice.AuthClientID)
}

func TestKeysService_ExtendAgentKeysIgnoresServerTheAgentLost(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestKeysService(t)
	agentID, first := connectAgentFixture(t, ctx, ti)
	created, err := ti.service.CreateKey(ctx, &gen.CreateKeyPayload{
		AgentID: new(agentID.String()), Name: "gateway key", DelegatedGrantsVersion: new(2),
		RequestedGrants: []*gen.AgentPolicyGrantForm{connectForm(first)},
	})
	require.NoError(t, err)

	// An earlier edit took the first server away from the agent. The key still
	// names it, which must not stop it gaining the next server.
	selectors, err := authz.NewSelector(authz.ScopeMCPConnect, first).MarshalJSON()
	require.NoError(t, err)
	removed, err := accessrepo.New(ti.conn).DeletePrincipalGrantByIdentity(ctx, accessrepo.DeletePrincipalGrantByIdentityParams{
		OrganizationID: testAuthContext(t, ctx).ActiveOrganizationID, PrincipalUrn: urn.NewPrincipal(urn.PrincipalTypeAgent, agentID.String()),
		Scope: string(authz.ScopeMCPConnect), Selectors: selectors,
	})
	require.NoError(t, err)
	require.Equal(t, int64(1), removed)

	second := uuid.NewString()
	grantConnect(t, ctx, ti, agentID, second)
	extended, err := ti.service.ExtendAgentKeys(ctx, &gen.ExtendAgentKeysPayload{
		AgentID: agentID.String(), RequestedGrants: []*gen.ExtendAgentKeyGrantForm{extendForm(second)},
	})
	require.NoError(t, err)
	require.Len(t, extended.Keys, 1)
	require.Equal(t, created.ID, extended.Keys[0].ID)
	require.ElementsMatch(t, []string{first, second}, requestedServers(extended.Keys[0]))
}

func TestKeysService_ExtendAgentKeysRejectsChoiceForUnrequestedServer(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestKeysService(t)
	agentID, first := connectAgentFixture(t, ctx, ti)
	_, err := ti.service.ExtendAgentKeys(ctx, &gen.ExtendAgentKeysPayload{
		AgentID:           agentID.String(),
		RequestedGrants:   []*gen.ExtendAgentKeyGrantForm{extendForm(first)},
		AuthClientChoices: []*gen.AgentKeyAuthClientChoice{{McpServerID: uuid.NewString()}},
	})
	requireOopsCode(t, err, oops.CodeBadRequest)
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
		AgentID: agentID.String(), RequestedGrants: []*gen.ExtendAgentKeyGrantForm{extendForm(ungranted)},
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

	added := extendForm(uuid.NewString())
	added.Selector.ProjectID = &projectID
	extended, err := ti.service.ExtendAgentKeys(ctx, &gen.ExtendAgentKeysPayload{
		AgentID: agentID.String(), RequestedGrants: []*gen.ExtendAgentKeyGrantForm{added},
	})
	require.NoError(t, err)
	require.Empty(t, extended.Keys, "a key that reaches every server in the project already reaches this one")
}

func TestKeysService_ExtendAgentKeysAcceptsOnlyServerConnectGrants(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestKeysService(t)
	agentID, projectID := createAgentKeyFixture(t, ctx, ti)

	_, err := ti.service.ExtendAgentKeys(ctx, &gen.ExtendAgentKeysPayload{
		AgentID: agentID.String(), RequestedGrants: []*gen.ExtendAgentKeyGrantForm{{
			Scope: string(authz.ScopeProjectRead), Effect: "allow", Selector: &gen.AgentPolicySelector{ResourceKind: authz.ResourceKindProject, ResourceID: projectID.String()},
		}},
	})
	requireOopsCode(t, err, oops.CodeBadRequest)
}

func TestKeysService_ExtendAgentKeysSkipsOtherIssuersAndExpiredKeys(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestKeysService(t)
	agentID, first := connectAgentFixture(t, ctx, ti)
	issue := func(name string) string {
		created, err := ti.service.CreateKey(ctx, &gen.CreateKeyPayload{
			AgentID: new(agentID.String()), Name: name, DelegatedGrantsVersion: new(2),
			RequestedGrants: []*gen.AgentPolicyGrantForm{connectForm(first)},
		})
		require.NoError(t, err)
		return created.ID
	}
	own, othersKey, expired := issue("own key"), issue("someone else's key"), issue("expired key")

	// A key acts for whoever issued it, so this caller's authority must not
	// widen another person's key.
	otherID := "issuer-" + uuid.NewString()
	_, err := usersrepo.New(ti.conn).UpsertUser(ctx, usersrepo.UpsertUserParams{
		ID: otherID, Email: otherID + "@example.com", DisplayName: otherID, PhotoUrl: conv.PtrToPGText(nil), Admin: false,
	})
	require.NoError(t, err)
	fixtures := testrepo.New(ti.conn)
	require.NoError(t, fixtures.SetAPIKeyCreatorFixture(ctx, testrepo.SetAPIKeyCreatorFixtureParams{CreatedByUserID: otherID, ID: uuid.MustParse(othersKey)}))
	require.NoError(t, fixtures.ExpireAPIKeyFixture(ctx, testrepo.ExpireAPIKeyFixtureParams{ID: uuid.MustParse(expired), OrganizationID: testAuthContext(t, ctx).ActiveOrganizationID}))

	second := uuid.NewString()
	grantConnect(t, ctx, ti, agentID, second)
	extended, err := ti.service.ExtendAgentKeys(ctx, &gen.ExtendAgentKeysPayload{
		AgentID: agentID.String(), RequestedGrants: []*gen.ExtendAgentKeyGrantForm{extendForm(second)},
	})
	require.NoError(t, err)
	require.Len(t, extended.Keys, 1)
	require.Equal(t, own, extended.Keys[0].ID)

	listed, err := ti.service.ListKeys(ctx, &gen.ListKeysPayload{AgentID: new(agentID.String())})
	require.NoError(t, err)
	reach := map[string][]string{}
	for _, key := range listed.Keys {
		reach[key.ID] = requestedServers(key)
	}
	require.ElementsMatch(t, []string{first, second}, reach[own])
	require.Equal(t, []string{first}, reach[othersKey])
	require.Equal(t, []string{first}, reach[expired])
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

func extendForm(serverID string) *gen.ExtendAgentKeyGrantForm {
	return &gen.ExtendAgentKeyGrantForm{
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
