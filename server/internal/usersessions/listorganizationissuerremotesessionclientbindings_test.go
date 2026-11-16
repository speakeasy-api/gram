package usersessions_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	orggen "github.com/speakeasy-api/gram/server/gen/organization_user_session_issuers"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	metamcprepo "github.com/speakeasy-api/gram/server/internal/metamcp/repo"
	"github.com/speakeasy-api/gram/server/internal/oops"
	testrepo "github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
)

func remoteClient(id uuid.UUID, slug string) *orggen.UserSessionIssuerRemoteSessionClient {
	return &orggen.UserSessionIssuerRemoteSessionClient{
		ID:                      id.String(),
		ClientID:                slug + "-cid",
		RemoteSessionIssuerID:   "",
		RemoteSessionIssuerName: slug,
		RemoteSessionIssuerURL:  "https://" + slug + ".example.com",
	}
}

// withoutIssuerIDs blanks the authorization server ids the seed helper picks,
// so listings compare on what the test chose.
func withoutIssuerIDs(t *testing.T, clients []*orggen.UserSessionIssuerRemoteSessionClient) []*orggen.UserSessionIssuerRemoteSessionClient {
	t.Helper()

	for _, client := range clients {
		require.NotEmpty(t, client.RemoteSessionIssuerID)
		client.RemoteSessionIssuerID = ""
	}
	return clients
}

func TestOrganizationUserSessionIssuerListRemoteSessionClientBindings(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	projectID := *authCtx.ProjectID
	issuerID := createOrganizationIssuer(t, ctx, ti, "bindings-issuer")
	otherIssuerID := createOrganizationIssuer(t, ctx, ti, "bindings-other-issuer")

	shared := seedRevocableClient(t, ctx, ti, issuerID, "shared", "", false)
	unused := seedRevocableClient(t, ctx, ti, issuerID, "unused", "", false)
	stray := seedRevocableClient(t, ctx, ti, otherIssuerID, "stray", "", false)

	toolset := createPlainToolset(t, ctx, ti, projectID, "bindings-toolset")
	serverID := attachIssuerMCPServer(t, ctx, ti, projectID, issuerID, toolset, "bindings-server")
	otherToolset := createPlainToolset(t, ctx, ti, projectID, "bindings-other-toolset")
	otherServerID := attachIssuerMCPServer(t, ctx, ti, projectID, otherIssuerID, otherToolset, "bindings-other-server")

	// A server attached to another issuer still uses this one when a gateway
	// attached to this issuer fronts it.
	gatewayed := seedRevocableClient(t, ctx, ti, otherIssuerID, "gatewayed", "", false)
	memberToolset := createPlainToolset(t, ctx, ti, projectID, "bindings-member-toolset")
	memberServerID := attachIssuerMCPServer(t, ctx, ti, projectID, otherIssuerID, memberToolset, "bindings-member-server")
	gateways := metamcprepo.New(ti.conn)
	gateway, err := gateways.CreateMetaMCPServer(ctx, metamcprepo.CreateMetaMCPServerParams{
		OrganizationID: authCtx.ActiveOrganizationID, ProjectID: projectID, Name: "Bindings gateway", UserSessionIssuerID: conv.ToNullUUID(issuerID), Visibility: "private",
	})
	require.NoError(t, err)
	_, err = gateways.CreateMetaMCPMember(ctx, metamcprepo.CreateMetaMCPMemberParams{ProjectID: projectID, MetaMcpServerID: gateway.ID, McpServerID: memberServerID, SortOrder: 0})
	require.NoError(t, err)

	fixtures := testrepo.New(ti.conn)
	for _, association := range []struct{ server, client uuid.UUID }{
		{serverID, shared},
		{serverID, stray},
		{otherServerID, stray},
		{memberServerID, gatewayed},
	} {
		require.NoError(t, fixtures.InsertMCPServerRemoteSessionClientFixture(ctx, testrepo.InsertMCPServerRemoteSessionClientFixtureParams{
			ProjectID:             projectID,
			OrganizationID:        authCtx.ActiveOrganizationID,
			McpServerID:           association.server,
			RemoteSessionClientID: association.client,
		}))
	}

	listed, err := ti.service.ListRemoteSessionClientBindings(ctx, &orggen.ListRemoteSessionClientBindingsPayload{UserSessionIssuerID: issuerID.String()})
	require.NoError(t, err)

	require.ElementsMatch(t, []*orggen.UserSessionIssuerRemoteSessionClient{
		remoteClient(shared, "shared"),
		remoteClient(unused, "unused"),
	}, withoutIssuerIDs(t, listed.BoundClients))

	type use struct{ server, client string }
	uses := make([]use, 0, len(listed.ServerClients))
	for _, entry := range listed.ServerClients {
		require.Equal(t, projectID.String(), entry.ProjectID)
		uses = append(uses, use{entry.McpServerName, entry.RemoteSessionClient.RemoteSessionIssuerName})
	}
	require.ElementsMatch(t, []use{
		{"bindings-server", "shared"},
		{"bindings-server", "stray"},
		{"bindings-member-server", "gatewayed"},
	}, uses)
}

func TestOrganizationUserSessionIssuerListRemoteSessionClientBindingsHidesOtherOrganizations(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	foreignID := seedOtherOrganizationIssuer(t, ctx, ti)

	_, err := ti.service.ListRemoteSessionClientBindings(ctx, &orggen.ListRemoteSessionClientBindingsPayload{UserSessionIssuerID: foreignID.String()})
	requireOopsCode(t, err, oops.CodeNotFound)
}

func TestOrganizationUserSessionIssuerListRemoteSessionClientBindingsEmpty(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	issuerID := createOrganizationIssuer(t, ctx, ti, "empty-bindings-issuer")

	listed, err := ti.service.ListRemoteSessionClientBindings(ctx, &orggen.ListRemoteSessionClientBindingsPayload{UserSessionIssuerID: issuerID.String()})
	require.NoError(t, err)
	require.Empty(t, listed.BoundClients)
	require.Empty(t, listed.ServerClients)
}

func TestOrganizationUserSessionIssuerListRemoteSessionClientBindingsRefusesUnknownIssuer(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	_, err := ti.service.ListRemoteSessionClientBindings(ctx, &orggen.ListRemoteSessionClientBindingsPayload{UserSessionIssuerID: uuid.NewString()})
	requireOopsCode(t, err, oops.CodeNotFound)
}

func TestOrganizationUserSessionIssuerListRemoteSessionClientBindingsRBAC(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	issuerID := createOrganizationIssuer(t, ctx, ti, "rbac-bindings-issuer")
	payload := &orggen.ListRemoteSessionClientBindingsPayload{UserSessionIssuerID: issuerID.String()}

	readCtx := withExactAuthzGrants(t, ctx, ti.conn, authz.NewGrant(authz.ScopeOrgRead, authCtx.ActiveOrganizationID))
	_, err := ti.service.ListRemoteSessionClientBindings(readCtx, payload)
	require.NoError(t, err)

	projectReadCtx := withExactAuthzGrants(t, ctx, ti.conn, authz.NewGrant(authz.ScopeProjectRead, authCtx.ProjectID.String()))
	_, err = ti.service.ListRemoteSessionClientBindings(projectReadCtx, payload)
	requireOopsCode(t, err, oops.CodeForbidden)
}
