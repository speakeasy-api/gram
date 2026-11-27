package mcp_test

import (
	"context"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/feature"
	"github.com/speakeasy-api/gram/server/internal/mcp/metamcp"
	"github.com/speakeasy-api/gram/server/internal/mcpservers"
	mcpserversrepo "github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
	metamcprepo "github.com/speakeasy-api/gram/server/internal/metamcp/repo"
	organizationsrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
	toolsetsrepo "github.com/speakeasy-api/gram/server/internal/toolsets/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// A tunneled gateway member whose column is cleared while it stays associated
// with its provider's client: the configuration hash consent stores differs
// by source, and the member keeps receiving its own token under either
// setting of the organization's flag, because the session's recorded client
// choice routes it.
func TestUpstreamIssuerFlag_TunneledGatewayMemberUnderEitherSource(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)
	projectID := *authCtx.ProjectID
	orgID := authCtx.ActiveOrganizationID

	sharedIssuerID := createUserSessionIssuer(t, ctx, ti.conn, projectID)
	metaSlug := "meta-upstream-source-" + uuid.NewString()[:8]
	meta := createMetaMcpEndpoint(t, ctx, ti.conn, projectID, orgID, metaSlug, sharedIssuerID)

	gateway := &fakeTunnelGateway{t: t, agentSessionID: "agent-source", backendSessionID: "backend-source", legacy: false, dead: false, challenge: ""}
	tunnelID, memberIssuerID := seedTunneledMetaMember(t, ctx, ti, projectID, meta.ID, "Tunneled member", "member-tunnel", 0, "")
	gatewayServer := httptest.NewServer(gateway)
	t.Cleanup(gatewayServer.Close)
	require.NoError(t, ti.tunnelRoutes.Publish(ctx, tunnelID.String(), gatewayServer.URL, time.Hour))

	client := createConsentRemoteClient(t, ctx, ti.conn, projectID, orgID, "meta-upstream-source", "")
	serverID := tunneledFixtureMember(t, ctx, ti, projectID, meta.ID, "member-tunnel")
	associateServerRegistration(t, ctx, ti.conn, orgID, projectID, serverID, client)
	typed := seedServerAuthOffer(t, ctx, ti.conn, orgID, projectID, serverID, client, "remote_session")
	require.NoError(t, remotesessions.RestampMCPServerRemoteSessionIssuers(ctx, ti.conn, orgID, projectID, []uuid.UUID{memberIssuerID}))
	subject := createTestUser(t, ctx, ti, "meta-upstream-source-user-"+uuid.NewString())
	insertQualifiedRemoteSessionToken(t, ctx, ti, sharedIssuerID, client, subject, "token-member", "")

	members, err := metamcprepo.New(ti.conn).ListServableMetaMCPMembers(ctx, metamcprepo.ListServableMetaMCPMembersParams{MetaMcpServerID: meta.ID, ProjectID: projectID})
	require.NoError(t, err)
	require.Len(t, members, 1)
	member := members[0]
	require.True(t, member.McpServerRemoteSessionIssuerID.Valid, "the resync stamps the member's provider")
	require.Equal(t, []uuid.UUID{member.McpServerRemoteSessionIssuerID.UUID}, member.McpServerAssociationProviderIds, "the member is associated with that provider's client alone")

	hashesOf := func() mcpserversrepo.ListMCPServerConsentConfigurationsRow {
		t.Helper()
		rows, err := mcpserversrepo.New(ti.conn).ListMCPServerConsentConfigurations(ctx, mcpserversrepo.ListMCPServerConsentConfigurationsParams{OrganizationID: orgID, McpServerIds: []uuid.UUID{member.McpServerID}})
		require.NoError(t, err)
		require.Len(t, rows, 1)
		return rows[0]
	}
	agreeing := hashesOf()
	require.Equal(t, agreeing.ConfigurationHash, agreeing.AssociationConfigurationHash, "both sources hash alike while they agree")

	cleared, err := testrepo.New(ti.conn).SetMCPServerRemoteSessionIssuerFixture(ctx, testrepo.SetMCPServerRemoteSessionIssuerFixtureParams{
		RemoteSessionIssuerID: uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		ID:                    member.McpServerID,
		ProjectID:             projectID,
	})
	require.NoError(t, err)
	require.Equal(t, int64(1), cleared)

	disagreeing := hashesOf()
	require.NotEqual(t, disagreeing.ConfigurationHash, disagreeing.AssociationConfigurationHash)
	require.Equal(t, agreeing.AssociationConfigurationHash, disagreeing.AssociationConfigurationHash, "clearing the column leaves the association hash alone")

	_, err = organizationsrepo.New(ti.conn).UpsertOrganizationUserRelationship(ctx, organizationsrepo.UpsertOrganizationUserRelationshipParams{OrganizationID: orgID, UserID: conv.ToPGText(subject.ID)})
	require.NoError(t, err)
	seedPrincipalGrant(t, ctx, ti, orgID, urn.NewPrincipal(urn.PrincipalTypeUser, subject.ID), authz.ScopeMCPAuth, typed.String())
	bearer := mintMetaIssuerBearer(t, ti, metaSlug, sharedIssuerID, subject, metaServingChoice{organizationID: orgID, serverID: member.McpServerID, clientID: typed})

	rpc := executeMetaTool(t, ti, metaSlug, bearer, "member-tunnel--ping")
	text, isError := metaToolResultText(t, rpc)
	require.False(t, isError, "the member call must succeed: %s", text)
	require.Equal(t, "Bearer token-member", gateway.forwardFor(`"tools/call"`).Get("Authorization"),
		"off, the session's client choice routes the member's own token")

	ti.features.SetFlag(feature.FlagUpstreamIssuerFromAssociations, orgID, true)

	rpc = executeMetaTool(t, ti, metaSlug, bearer, "member-tunnel--ping")
	text, isError = metaToolResultText(t, rpc)
	require.False(t, isError, "the member call must succeed: %s", text)
	require.Equal(t, "Bearer token-member", gateway.forwardFor(`"tools/call"`).Get("Authorization"),
		"on, the member still receives its own token")
}

// A hosted member always uses its exact registration, independently of the issuer-source flag.
func TestUpstreamIssuerFlag_HostedGatewayAlwaysUsesItsAssignment(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)
	projectID := *authCtx.ProjectID
	orgID := authCtx.ActiveOrganizationID

	sharedIssuerID := createUserSessionIssuer(t, ctx, ti.conn, projectID)
	metaSlug := "meta-hosted-source-" + uuid.NewString()[:8]
	meta := createMetaMcpEndpoint(t, ctx, ti.conn, projectID, orgID, metaSlug, sharedIssuerID)
	hosted := seedHostedMetaMember(t, ctx, ti, meta.ID, "Hosted member", 0, mcpservers.VisibilityPublic, "initial_fixture_tool")
	toolset, err := toolsetsrepo.New(ti.conn).GetToolsetByIDAndProject(ctx, toolsetsrepo.GetToolsetByIDAndProjectParams{ID: hosted.toolsetID, ProjectID: projectID})
	require.NoError(t, err)
	var forwarded atomic.Value
	upstream := newAuthorizationRecorder(t, &forwarded)
	toolName := addOAuthSecuredHTTPTool(t, ctx, ti, hostedToolset{Toolset: toolset, McpSlug: pgtype.Text{}, CustomDomainID: uuid.NullUUID{}, UserSessionIssuerID: uuid.NullUUID{}}, upstream.URL)

	columnClient := createConsentRemoteClient(t, ctx, ti.conn, projectID, orgID, "hosted-source-column", "")
	associationClient := createConsentRemoteClient(t, ctx, ti.conn, projectID, orgID, "hosted-source-association", "")
	columnProvider := clientRemoteIssuerID(t, ctx, ti.conn, projectID, orgID, columnClient)
	associationProvider := clientRemoteIssuerID(t, ctx, ti.conn, projectID, orgID, associationClient)
	require.NotEqual(t, columnProvider, associationProvider)

	stampRemoteSessionIssuer(t, ctx, ti.conn, projectID, hosted.serverID, uuid.NullUUID{UUID: columnProvider, Valid: true})
	require.NoError(t, testrepo.New(ti.conn).InsertMCPServerRemoteSessionClientFixture(ctx, testrepo.InsertMCPServerRemoteSessionClientFixtureParams{
		ProjectID:             projectID,
		OrganizationID:        orgID,
		McpServerID:           hosted.serverID,
		RemoteSessionClientID: associationClient,
	}))

	members, err := metamcprepo.New(ti.conn).ListServableMetaMCPMembers(ctx, metamcprepo.ListServableMetaMCPMembersParams{MetaMcpServerID: meta.ID, ProjectID: projectID})
	require.NoError(t, err)
	require.Len(t, members, 1)
	require.Equal(t, uuid.NullUUID{UUID: columnProvider, Valid: true}, members[0].McpServerRemoteSessionIssuerID)
	require.Equal(t, []uuid.UUID{associationProvider}, members[0].McpServerAssociationProviderIds)

	subject := createTestUser(t, ctx, ti, "hosted-source-user-"+uuid.NewString())
	insertQualifiedRemoteSessionToken(t, ctx, ti, sharedIssuerID, columnClient, subject, "token-column", "")
	bearer := mintMetaIssuerBearer(t, ti, metaSlug, sharedIssuerID, subject)

	for _, enabled := range []bool{false, true} {
		ti.features.SetFlag(feature.FlagUpstreamIssuerFromAssociations, orgID, enabled)
		require.Equal(t, metamcp.SignInNotConnected, callConnectionStatus(t, ti, context.Background(), metaSlug, bearer).statuses[hosted.slug],
			"the unrelated registration must not connect the member, regardless of the issuer flag")
	}
	insertQualifiedRemoteSessionToken(t, ctx, ti, sharedIssuerID, associationClient, subject, "token-association", "")
	for _, enabled := range []bool{false, true} {
		ti.features.SetFlag(feature.FlagUpstreamIssuerFromAssociations, orgID, enabled)
		require.Equal(t, metamcp.SignInConnected, callConnectionStatus(t, ti, context.Background(), metaSlug, bearer).statuses[hosted.slug])
		forwarded.Store("")
		text, isError := metaToolResultText(t, executeMetaTool(t, ti, metaSlug, bearer, hosted.slug+"--"+toolName))
		require.False(t, isError, "%s", text)
		require.Equal(t, "Bearer token-association", forwarded.Load())
	}

}
