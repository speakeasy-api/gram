package mcp_test

import (
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/feature"
	mcpserversrepo "github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
	metamcprepo "github.com/speakeasy-api/gram/server/internal/metamcp/repo"
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
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

	client := createConsentRemoteClient(t, ctx, ti.conn, projectID, orgID, "meta-upstream-source", "", []uuid.UUID{sharedIssuerID, memberIssuerID})
	require.NoError(t, remotesessions.ResyncMCPServerRemoteSessionIssuers(ctx, ti.conn, orgID, projectID, []uuid.UUID{memberIssuerID}))
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

	bearer := mintMetaIssuerBearer(t, ti, metaSlug, sharedIssuerID, subject)

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
