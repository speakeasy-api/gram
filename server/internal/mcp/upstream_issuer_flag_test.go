package mcp_test

import (
	"context"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/attribute"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/feature"
	"github.com/speakeasy-api/gram/server/internal/mcp/metamcp"
	"github.com/speakeasy-api/gram/server/internal/mcpservers"
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

// upstreamIssuerParityPoints reads the mcp.upstream_issuer.parity counter
// back, keyed by attribute set.
func upstreamIssuerParityPoints(t *testing.T, reader *sdkmetric.ManualReader) map[attribute.Set]int64 {
	t.Helper()

	var rm metricdata.ResourceMetrics
	require.NoError(t, reader.Collect(t.Context(), &rm))
	points := map[attribute.Set]int64{}
	for _, scope := range rm.ScopeMetrics {
		for _, m := range scope.Metrics {
			if m.Name != "mcp.upstream_issuer.parity" {
				continue
			}
			sum, ok := m.Data.(metricdata.Sum[int64])
			require.True(t, ok, "the parity instrument must be an int64 counter")
			for _, dp := range sum.DataPoints {
				points[dp.Attributes] = dp.Value
			}
		}
	}
	return points
}

// A hosted gateway member receives only the unqualified token keyed by its
// upstream issuer, with no client choice to replace it, so the flag decides
// which token it gets: its column names one provider and its association
// another. Off, the column-keyed token connects it; on, the association-keyed
// one does, and only once that provider holds a token. Each tool dispatch
// counts the two sources as different under either setting.
func TestUpstreamIssuerFlag_HostedGatewayMemberTakesTheFlaggedSourcesToken(t *testing.T) {
	t.Parallel()

	reader := sdkmetric.NewManualReader()
	ctx, ti := newTestMCPServiceWithMeterProvider(t, sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)))
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)
	projectID := *authCtx.ProjectID
	orgID := authCtx.ActiveOrganizationID

	sharedIssuerID := createUserSessionIssuer(t, ctx, ti.conn, projectID)
	metaSlug := "meta-hosted-source-" + uuid.NewString()[:8]
	meta := createMetaMcpEndpoint(t, ctx, ti.conn, projectID, orgID, metaSlug, sharedIssuerID)
	hosted := seedHostedMetaMember(t, ctx, ti, meta.ID, "Hosted member", 0, mcpservers.VisibilityPublic, "alpha_tool")

	columnClient := createConsentRemoteClient(t, ctx, ti.conn, projectID, orgID, "hosted-source-column", "", []uuid.UUID{sharedIssuerID})
	associationClient := createConsentRemoteClient(t, ctx, ti.conn, projectID, orgID, "hosted-source-association", "", []uuid.UUID{sharedIssuerID})
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

	hostedDiffer := func(source string) attribute.Set {
		return attribute.NewSet(
			attr.UpstreamIssuerSurface("gateway_hosted"),
			attr.UpstreamIssuerSite("route"),
			attr.UpstreamIssuerOutcome("differ"),
			attr.UpstreamIssuerSource(source),
			attr.AuthClientResolverMode("off"),
		)
	}
	dispatch := func() {
		t.Helper()
		// The fixture tool has no server URL, so the call fails member-scoped
		// after the member's credential is picked, which is all this needs.
		executeMetaTool(t, ti, metaSlug, bearer, hosted.slug+"--alpha_tool")
	}

	require.Equal(t, metamcp.SignInConnected, callConnectionStatus(t, ti, context.Background(), metaSlug, bearer).statuses[hosted.slug],
		"off, the column-keyed token connects the member")
	dispatch()
	require.Equal(t, int64(1), upstreamIssuerParityPoints(t, reader)[hostedDiffer("column")],
		"off, the dispatch counts the sources as different with the column deciding")

	ti.features.SetFlag(feature.FlagUpstreamIssuerFromAssociations, orgID, true)

	require.Equal(t, metamcp.SignInNotConnected, callConnectionStatus(t, ti, context.Background(), metaSlug, bearer).statuses[hosted.slug],
		"on, the column-keyed token no longer serves the member")
	insertQualifiedRemoteSessionToken(t, ctx, ti, sharedIssuerID, associationClient, subject, "token-association", "")
	require.Equal(t, metamcp.SignInConnected, callConnectionStatus(t, ti, context.Background(), metaSlug, bearer).statuses[hosted.slug],
		"on, the association-keyed token connects the member")
	dispatch()
	require.Equal(t, int64(1), upstreamIssuerParityPoints(t, reader)[hostedDiffer("associations")],
		"on, the dispatch counts the sources as different with the associations deciding")
}
