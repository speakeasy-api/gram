// Keepalive re-check end to end: an idle grant with no refresh token is probed on the sweep, and the verdict lands without anyone opening the consent page.

package mcp_test

import (
	"context"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/mcp"
	mcpendpoints_repo "github.com/speakeasy-api/gram/server/internal/mcpendpoints/repo"
	mcpservers_repo "github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
	"github.com/speakeasy-api/gram/server/internal/networkaccess"
	"github.com/speakeasy-api/gram/server/internal/ratelimit"
	remotemcp_repo "github.com/speakeasy-api/gram/server/internal/remotemcp/repo"
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
	remotesessions_repo "github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
	tunneledmcprepo "github.com/speakeasy-api/gram/server/internal/tunneledmcp/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

const recheckTestInterval = 24 * time.Hour

// shapeGrantForKeepalive turns one grant into the population the sweep owns: no access expiry, no refresh token (the
// fixture never stores one), created age ago so it is past the interval, and a live Gram session for the subject.
func shapeGrantForKeepalive(t *testing.T, ctx context.Context, ti *testInstance, projectID, issuerID, clientID uuid.UUID, subject urn.SessionSubject, age time.Duration, jti string) remotesessions_repo.RemoteSession {
	t.Helper()
	q := remotesessions_repo.New(ti.conn)
	sess, err := q.GetActiveRemoteSession(ctx, remotesessions_repo.GetActiveRemoteSessionParams{SubjectUrn: subject, RemoteSessionClientID: clientID})
	require.NoError(t, err)
	require.False(t, sess.RefreshTokenEncrypted.Valid)
	require.NoError(t, q.SetRemoteSessionAccessExpiresAt(ctx, remotesessions_repo.SetRemoteSessionAccessExpiresAtParams{
		AccessExpiresAt: pgtype.Timestamptz{Time: time.Time{}, Valid: false, InfinityModifier: pgtype.Finite},
		ID:              sess.ID,
		ProjectID:       conv.ToNullUUID(projectID),
	}))
	require.NoError(t, q.SetRemoteSessionValidationTrackingFixture(ctx, remotesessions_repo.SetRemoteSessionValidationTrackingFixtureParams{
		LastValidatedAt:      pgtype.Timestamptz{Time: time.Time{}, Valid: false, InfinityModifier: pgtype.Finite},
		LastRefreshAttemptAt: pgtype.Timestamptz{Time: time.Time{}, Valid: false, InfinityModifier: pgtype.Finite},
		CreatedAt:            conv.ToPGTimestamptz(time.Now().Add(-age)),
		ID:                   sess.ID,
		ProjectID:            conv.ToNullUUID(projectID),
	}))
	persistTestUserSession(t, ti, issuerID, subject, jti)
	return sess
}

// makeKeepaliveShaped shapes the fixture's own grant; see shapeGrantForKeepalive.
func makeKeepaliveShaped(t *testing.T, ctx context.Context, fx validationFixture, prefix string) {
	t.Helper()
	shapeGrantForKeepalive(t, ctx, fx.ti, fx.endpoint.ProjectID, fx.endpoint.UserSessionIssuerID, fx.clientID, fx.subject, recheckTestInterval+time.Hour, "jti-"+prefix)
}

// ageIntoRecheckWindow backdates the tracking columns so the grant is due again and its claim lease has lapsed.
func ageIntoRecheckWindow(t *testing.T, ctx context.Context, fx validationFixture) {
	t.Helper()
	sess := storedSession(t, ctx, fx)
	ago := conv.ToPGTimestamptz(time.Now().Add(-(recheckTestInterval + time.Hour)))
	require.NoError(t, remotesessions_repo.New(fx.ti.conn).SetRemoteSessionValidationTrackingFixture(ctx, remotesessions_repo.SetRemoteSessionValidationTrackingFixtureParams{
		LastValidatedAt:      ago,
		LastRefreshAttemptAt: ago,
		CreatedAt:            pgtype.Timestamptz{Time: time.Time{}, Valid: false, InfinityModifier: pgtype.Finite},
		ID:                   sess.ID,
		ProjectID:            conv.ToNullUUID(fx.endpoint.ProjectID),
	}))
}

// validationTriggers reads gram.remote_session.validation back by trigger.
func validationTriggers(t *testing.T, reader *sdkmetric.ManualReader) map[string]int64 {
	t.Helper()
	var rm metricdata.ResourceMetrics
	require.NoError(t, reader.Collect(t.Context(), &rm))
	counts := map[string]int64{}
	for _, scope := range rm.ScopeMetrics {
		for _, m := range scope.Metrics {
			if m.Name != "gram.remote_session.validation" {
				continue
			}
			sum, ok := m.Data.(metricdata.Sum[int64])
			require.True(t, ok)
			for _, dp := range sum.DataPoints {
				trigger, found := dp.Attributes.Value(attr.OAuthValidationTriggerKey)
				require.True(t, found)
				counts[trigger.AsString()] += dp.Value
			}
		}
	}
	return counts
}

// A revoked no-expiry grant reads rejected_by_member after the sweep, with nobody opening the consent page; the
// verdict is the member's own answer, presented through the grant's endpoint with the grant's own credential.
func TestRemoteSessionRecheck_RevokedNoExpiryGrantReadsRejected(t *testing.T) {
	t.Parallel()

	ctx, fx := seedStandaloneValidationFixture(t, "aim261-standalone")
	bearer := "token-aim261-standalone"
	makeKeepaliveShaped(t, ctx, fx, "aim261-standalone")
	require.Contains(t, renderConsent(t, fx), "Not yet verified")
	require.Empty(t, fx.member.drain(), "rendering alone never probes")

	fx.member.set(memberRejects)
	checked, err := fx.ti.service.SweepRemoteSessionRechecks(ctx)
	require.NoError(t, err)
	require.Equal(t, 1, checked)
	requireProbe(t, fx.member.drain(), bearer, false, false)

	sess := storedSession(t, ctx, fx)
	require.Equal(t, string(remotesessions.ValidationOutcomeRejectedByMember), sess.ValidationStatus.String)
	require.Equal(t, "Rejected by "+fx.name, sess.ValidationReason.String)
	require.True(t, sess.LastValidatedAt.Valid)
	require.True(t, sess.LastRefreshAttemptAt.Valid, "the claim lease is stamped")
	require.Contains(t, renderConsent(t, fx), `data-validation="rejected"`)
	require.Equal(t, map[string]int64{"rejected_by_member": 1}, validationCounts(t, fx.reader))
	require.Equal(t, map[string]int64{"keepalive": 1}, validationTriggers(t, fx.reader))

	// A fresh verdict and a live lease keep the grant out of the next pass.
	checked, err = fx.ti.service.SweepRemoteSessionRechecks(ctx)
	require.NoError(t, err)
	require.Zero(t, checked)
	require.Empty(t, fx.member.drain())

	// Once the interval has passed the grant is due again; a member that accepts it now moves it past the rejection.
	ageIntoRecheckWindow(t, ctx, fx)
	fx.member.set(memberAccepts)
	checked, err = fx.ti.service.SweepRemoteSessionRechecks(ctx)
	require.NoError(t, err)
	require.Equal(t, 1, checked)
	requireProbe(t, fx.member.drain(), bearer, true, true)
	sess = storedSession(t, ctx, fx)
	require.Equal(t, string(remotesessions.ValidationOutcomeValid), sess.ValidationStatus.String)
	require.Equal(t, map[string]int64{"rejected_by_member": 1, "valid": 1}, validationCounts(t, fx.reader))
	require.NoError(t, fx.ti.service.Shutdown(ctx))
}

// A grant connected inside the interval is the connect auto-verify's; the sweep leaves it alone until it ages.
func TestRemoteSessionRecheck_LeavesFreshGrantsToConnectVerify(t *testing.T) {
	t.Parallel()

	ctx, fx := seedStandaloneValidationFixture(t, "aim261-fresh")
	shapeGrantForKeepalive(t, ctx, fx.ti, fx.endpoint.ProjectID, fx.endpoint.UserSessionIssuerID, fx.clientID, fx.subject, time.Hour, "jti-aim261-fresh")

	fx.member.set(memberRejects)
	checked, err := fx.ti.service.SweepRemoteSessionRechecks(ctx)
	require.NoError(t, err)
	require.Zero(t, checked)
	require.Empty(t, fx.member.drain())
	sess := storedSession(t, ctx, fx)
	require.False(t, sess.ValidationStatus.Valid)
	require.False(t, sess.LastRefreshAttemptAt.Valid, "never claimed")
}

// A grant that still carries a refresh token belongs to the refresh sweep and is never probed here.
func TestRemoteSessionRecheck_LeavesRenewableGrantsToTheRefreshSweep(t *testing.T) {
	t.Parallel()

	ctx, fx := seedStandaloneValidationFixture(t, "aim261-renewable")
	makeKeepaliveShaped(t, ctx, fx, "aim261-renewable")
	sess := storedSession(t, ctx, fx)
	refreshCiphertext, err := fx.ti.enc.Encrypt([]byte("refresh-aim261"))
	require.NoError(t, err)
	_, err = remotesessions_repo.New(fx.ti.conn).UpdateRemoteSessionTokensIfUnchanged(ctx, remotesessions_repo.UpdateRemoteSessionTokensIfUnchangedParams{
		AccessTokenEncrypted:   sess.AccessTokenEncrypted,
		AccessExpiresAt:        sess.AccessExpiresAt,
		RefreshTokenEncrypted:  conv.ToPGText(refreshCiphertext),
		AuthorizationExpiresAt: sess.AuthorizationExpiresAt,
		RefreshTokenRotated:    true,
		RefreshExpiresAt:       sess.RefreshExpiresAt,
		Scopes:                 sess.Scopes,
		BackfillResource:       pgtype.Text{String: "", Valid: false},
		SubjectUrn:             fx.subject,
		RemoteSessionClientID:  fx.clientID,
		ExpectedUpdatedAt:      sess.UpdatedAt,
	})
	require.NoError(t, err)

	fx.member.set(memberRejects)
	checked, err := fx.ti.service.SweepRemoteSessionRechecks(ctx)
	require.NoError(t, err)
	require.Zero(t, checked)
	require.Empty(t, fx.member.drain())
	require.False(t, storedSession(t, ctx, fx).ValidationStatus.Valid)
}

// A rate-limited issuer host stops the pass: the refused row gives its lease back and is not counted, and rows
// that were never claimed wait for the next tick instead of being leased unprobed.
func TestRemoteSessionRecheck_RateLimitedHostStopsThePass(t *testing.T) {
	t.Parallel()

	const prefix = "aim261-pace"
	ctx, fx := seedStandaloneValidationFixture(t, prefix)
	fx.ti.service.SetRemoteSessionRecheckPacing(ratelimit.PerMinute(1), 1)
	projectID, orgID := fx.endpoint.ProjectID, fx.endpoint.OrganizationID
	shared := fx.endpoint.UserSessionIssuerID

	// Three due grants, oldest first: two on the fixture's issuer host, one on another host.
	shapeGrantForKeepalive(t, ctx, fx.ti, projectID, shared, fx.clientID, fx.subject, 30*time.Hour, "jti-"+prefix+"-a")
	subjectB := urn.NewUserSubject(uuid.NewString())
	insertQualifiedRemoteSessionToken(t, ctx, fx.ti, shared, fx.clientID, subjectB, "token-"+prefix+"-b", fx.member.url)
	shapeGrantForKeepalive(t, ctx, fx.ti, projectID, shared, fx.clientID, subjectB, 29*time.Hour, "jti-"+prefix+"-b")
	otherClient := createConsentRemoteClient(t, ctx, fx.ti.conn, projectID, orgID, prefix+"-other", "", []uuid.UUID{shared})
	subjectC := urn.NewUserSubject(uuid.NewString())
	insertQualifiedRemoteSessionToken(t, ctx, fx.ti, shared, otherClient, subjectC, "token-"+prefix+"-c", fx.member.url)
	shapeGrantForKeepalive(t, ctx, fx.ti, projectID, shared, otherClient, subjectC, 28*time.Hour, "jti-"+prefix+"-c")

	fx.member.set(memberAccepts)
	checked, err := fx.ti.service.SweepRemoteSessionRechecks(ctx)
	require.NoError(t, err)
	require.Equal(t, 1, checked, "only the probed grant is counted")
	requireProbe(t, fx.member.drain(), "token-"+prefix, true, true)

	q := remotesessions_repo.New(fx.ti.conn)
	sessA := storedSession(t, ctx, fx)
	require.Equal(t, string(remotesessions.ValidationOutcomeValid), sessA.ValidationStatus.String)
	require.True(t, sessA.LastRefreshAttemptAt.Valid)

	sessB, err := q.GetActiveRemoteSession(ctx, remotesessions_repo.GetActiveRemoteSessionParams{SubjectUrn: subjectB, RemoteSessionClientID: fx.clientID})
	require.NoError(t, err)
	require.False(t, sessB.ValidationStatus.Valid, "the refused row was never probed")
	require.False(t, sessB.LastRefreshAttemptAt.Valid, "the refused row gives its lease back")

	sessC, err := q.GetActiveRemoteSession(ctx, remotesessions_repo.GetActiveRemoteSessionParams{SubjectUrn: subjectC, RemoteSessionClientID: otherClient})
	require.NoError(t, err)
	require.False(t, sessC.ValidationStatus.Valid)
	require.False(t, sessC.LastRefreshAttemptAt.Valid, "the pass stopped claiming after the refusal")
	require.Equal(t, map[string]int64{"keepalive": 1}, validationTriggers(t, fx.reader))
}

// A disabled endpoint listed ahead of a live one on the same issuer does not shadow it: the grant is probed through the live one.
func TestRemoteSessionRecheck_DisabledEndpointDoesNotShadowLiveSibling(t *testing.T) {
	t.Parallel()

	const prefix = "aim261-shadow"
	ctx, fx := seedStandaloneValidationFixture(t, prefix)
	makeKeepaliveShaped(t, ctx, fx, prefix)
	projectID := fx.endpoint.ProjectID
	endpoints := mcpendpoints_repo.New(fx.ti.conn)

	// Re-create the live endpoint after a disabled sibling so the disabled one sorts first.
	live, err := endpoints.GetMCPEndpointByCustomDomainAndSlug(ctx, mcpendpoints_repo.GetMCPEndpointByCustomDomainAndSlugParams{
		Slug:           fx.endpoint.Slug,
		CustomDomainID: uuid.NullUUID{UUID: uuid.Nil, Valid: false},
	})
	require.NoError(t, err)
	_, err = endpoints.DeleteMCPEndpoint(ctx, mcpendpoints_repo.DeleteMCPEndpointParams{ID: live.ID, ProjectID: projectID})
	require.NoError(t, err)
	deadUpstream, err := remotemcp_repo.New(fx.ti.conn).CreateServer(ctx, remotemcp_repo.CreateServerParams{
		ID:            uuid.New(),
		ProjectID:     projectID,
		TransportType: "streamable-http",
		Url:           fx.member.url,
	})
	require.NoError(t, err)
	dead, err := mcpservers_repo.New(fx.ti.conn).CreateMCPServer(ctx, mcpservers_repo.CreateMCPServerParams{
		ID:                  uuid.New(),
		ProjectID:           projectID,
		Name:                conv.ToPGText(prefix + "-dead"),
		Slug:                conv.ToPGText(prefix + "-dead"),
		RemoteMcpServerID:   conv.ToNullUUID(deadUpstream.ID),
		Visibility:          "disabled",
		UserSessionIssuerID: conv.ToNullUUID(fx.endpoint.UserSessionIssuerID),
	})
	require.NoError(t, err)
	for _, ep := range []struct {
		slug     string
		serverID uuid.UUID
	}{{prefix + "-dead", dead.ID}, {fx.endpoint.Slug, fx.endpoint.McpServerID.UUID}} {
		_, err = endpoints.CreateMCPEndpoint(ctx, mcpendpoints_repo.CreateMCPEndpointParams{
			ProjectID:       projectID,
			CustomDomainID:  uuid.NullUUID{UUID: uuid.Nil, Valid: false},
			McpServerID:     conv.ToNullUUID(ep.serverID),
			MetaMcpServerID: uuid.NullUUID{UUID: uuid.Nil, Valid: false},
			Slug:            ep.slug,
		})
		require.NoError(t, err)
	}

	fx.member.set(memberRejects)
	checked, err := fx.ti.service.SweepRemoteSessionRechecks(ctx)
	require.NoError(t, err)
	require.Equal(t, 1, checked)
	requireProbe(t, fx.member.drain(), "token-"+prefix, false, false)
	require.Equal(t, string(remotesessions.ValidationOutcomeRejectedByMember), storedSession(t, ctx, fx).ValidationStatus.String)
}

// A private-only endpoint refuses the public surface; the sweep presents the grant through the organization's own instead.
func TestRemoteSessionRecheck_PrivateOnlyEndpointIsProbedPrivately(t *testing.T) {
	t.Parallel()

	const prefix = "aim261-private"
	ctx, fx := seedStandaloneValidationFixture(t, prefix)
	makeKeepaliveShaped(t, ctx, fx, prefix)
	servers := mcpservers_repo.New(fx.ti.conn)
	server, err := servers.GetMCPServerByIDAndProjectID(ctx, mcpservers_repo.GetMCPServerByIDAndProjectIDParams{ID: fx.endpoint.McpServerID.UUID, ProjectID: fx.endpoint.ProjectID})
	require.NoError(t, err)
	_, err = servers.UpdateMCPServer(ctx, mcpservers_repo.UpdateMCPServerParams{
		Name:                  server.Name,
		Slug:                  server.Slug,
		EnvironmentID:         server.EnvironmentID,
		UserSessionIssuerID:   server.UserSessionIssuerID,
		RemoteMcpServerID:     server.RemoteMcpServerID,
		TunneledMcpServerID:   server.TunneledMcpServerID,
		ToolsetID:             server.ToolsetID,
		UnproxiedMcpServerID:  server.UnproxiedMcpServerID,
		ToolVariationsGroupID: server.ToolVariationsGroupID,
		Visibility:            server.Visibility,
		NetworkAccessModeSet:  true,
		NetworkAccessMode:     conv.ToPGText(string(networkaccess.ModePrivateOnly)),
		ID:                    server.ID,
		ProjectID:             server.ProjectID,
	})
	require.NoError(t, err)

	fx.member.set(memberRejects)
	checked, err := fx.ti.service.SweepRemoteSessionRechecks(ctx)
	require.NoError(t, err)
	require.Equal(t, 1, checked)
	requireProbe(t, fx.member.drain(), "token-"+prefix, false, false)
	require.Equal(t, string(remotesessions.ValidationOutcomeRejectedByMember), storedSession(t, ctx, fx).ValidationStatus.String)
	require.Equal(t, map[string]int64{"keepalive": 1}, validationTriggers(t, fx.reader))
}

// A gateway member's grant is placed through the gateway endpoint and judged by the member it routes to.
func TestRemoteSessionRecheck_ProbesMetaMemberThroughGateway(t *testing.T) {
	t.Parallel()

	ctx, fx := seedMetaValidationFixture(t, "aim261-meta")
	makeKeepaliveShaped(t, ctx, fx, "aim261-meta")
	_, err := mcpendpoints_repo.New(fx.ti.conn).CreateMCPEndpoint(ctx, mcpendpoints_repo.CreateMCPEndpointParams{
		ProjectID:       fx.endpoint.ProjectID,
		CustomDomainID:  uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		McpServerID:     uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		MetaMcpServerID: fx.endpoint.MetaMcpServerID,
		Slug:            fx.endpoint.Slug,
	})
	require.NoError(t, err)

	fx.member.set(memberForbids)
	checked, err := fx.ti.service.SweepRemoteSessionRechecks(ctx)
	require.NoError(t, err)
	require.Equal(t, 1, checked)
	requireProbe(t, fx.member.drain(), "token-aim261-meta", false, false)
	sess := storedSession(t, ctx, fx)
	require.Equal(t, string(remotesessions.ValidationOutcomeRejectedByMember), sess.ValidationStatus.String)
	require.Equal(t, "Rejected by "+fx.name, sess.ValidationReason.String)
	require.Equal(t, map[string]int64{"keepalive": 1}, validationTriggers(t, fx.reader))
}

// createPrivateTunneledServer is createTunneledServer under private visibility: a public tunnel has no OAuth surface to place a grant on.
func createPrivateTunneledServer(t *testing.T, ctx context.Context, ti *testInstance, projectID, issuerID uuid.UUID, slug, identifier string) (uuid.UUID, uuid.UUID) {
	t.Helper()
	tunneled, err := tunneledmcprepo.New(ti.conn).CreateServer(ctx, tunneledmcprepo.CreateServerParams{
		ID:                 uuid.New(),
		ProjectID:          projectID,
		Name:               slug,
		KeyHash:            "hash-" + slug,
		KeyPrefix:          "pfx",
		ResourceIdentifier: conv.ToPGTextEmpty(identifier),
	})
	require.NoError(t, err)
	server, err := mcpservers_repo.New(ti.conn).CreateMCPServer(ctx, mcpservers_repo.CreateMCPServerParams{
		ID:                  uuid.New(),
		ProjectID:           projectID,
		Name:                conv.ToPGText(slug),
		Slug:                conv.ToPGText(slug),
		TunneledMcpServerID: conv.ToNullUUID(tunneled.ID),
		Visibility:          "private",
		UserSessionIssuerID: conv.ToNullUUID(issuerID),
	})
	require.NoError(t, err)
	return server.ID, tunneled.ID
}

// seedTunneledRecheckFixture: a private tunneled endpoint with a keepalive-shaped grant keyed by the tunnel's identifier.
func seedTunneledRecheckFixture(t *testing.T, prefix, identifier string) (context.Context, validationFixture, uuid.UUID) {
	t.Helper()
	reader, provider := newValidationMeterProvider()
	ctx, ti := newTestMCPServiceWithMetaRuntime(t, provider, mcp.MetaRuntimeConfig{MemberCallTimeout: 0, ValidationTimeout: validationProbeTimeout, AutoVerifyWait: 0, RecheckInterval: recheckTestInterval})
	projectID, orgID := consentTestTenant(t, ctx)
	shared := createUserSessionIssuer(t, ctx, ti.conn, projectID)
	serverID, tunnelID := createPrivateTunneledServer(t, ctx, ti, projectID, shared, prefix+"-server", identifier)
	endpoint, _, subject := mintFirstPartyConsentState(t, ctx, ti, projectID, orgID, shared, prefix+"-server")
	endpoint.McpServerID = conv.ToNullUUID(serverID)
	_, err := mcpendpoints_repo.New(ti.conn).CreateMCPEndpoint(ctx, mcpendpoints_repo.CreateMCPEndpointParams{
		ProjectID:       projectID,
		CustomDomainID:  uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		McpServerID:     conv.ToNullUUID(serverID),
		MetaMcpServerID: uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		Slug:            endpoint.Slug,
	})
	require.NoError(t, err)
	clientID := createConsentRemoteClient(t, ctx, ti.conn, projectID, orgID, prefix, "", []uuid.UUID{shared})
	stampRemoteSessionIssuer(t, ctx, ti.conn, projectID, serverID, conv.ToNullUUID(clientRemoteIssuerID(t, ctx, ti.conn, projectID, orgID, clientID)))
	insertQualifiedRemoteSessionToken(t, ctx, ti, shared, clientID, subject, "token-"+prefix, identifier)
	// The sweep probes as the subject, who must hold mcp:connect on a private server just as they did to connect.
	seedMetaMemberConnectGrant(t, ctx, ti.conn, orgID, serverID)
	fx := validationFixture{ti: ti, reader: reader, endpoint: endpoint, stateID: "", subject: subject, member: nil, clientID: clientID, name: prefix + "-server"}
	makeKeepaliveShaped(t, ctx, fx, prefix)
	return ctx, fx, tunnelID
}

// A tunneled backend with a live route is dialled through its gateway under the keepalive trigger.
func TestRemoteSessionRecheck_LiveTunnelIsProbedThroughTheGateway(t *testing.T) {
	t.Parallel()

	const prefix = "aim261-tunnel-live"
	ctx, fx, tunnelID := seedTunneledRecheckFixture(t, prefix, "urn:gram:tunnel:aim261-live")
	gateway := &fakeTunnelGateway{t: t, agentSessionID: "agent-1", backendSessionID: "backend-secret-session", legacy: false, dead: false, busy: false, challenge: "", mu: sync.Mutex{}, forwards: nil, forwardBodies: nil}
	gatewayServer := httptest.NewServer(gateway)
	t.Cleanup(gatewayServer.Close)
	require.NoError(t, fx.ti.tunnelRoutes.Publish(ctx, tunnelID.String(), gatewayServer.URL, time.Hour))

	checked, err := fx.ti.service.SweepRemoteSessionRechecks(ctx)
	require.NoError(t, err)
	require.Equal(t, 1, checked)
	headers, bodies := tunnelForwards(gateway)
	requireTunnelProbe(t, headers, bodies, "token-"+prefix)
	sess := storedSession(t, ctx, fx)
	require.Equal(t, string(remotesessions.ValidationOutcomeValid), sess.ValidationStatus.String)
	require.Equal(t, map[string]int64{"valid": 1}, validationCounts(t, fx.reader))
	require.Equal(t, map[string]int64{"keepalive": 1}, validationTriggers(t, fx.reader))
}

// A tunneled backend with no live route is never dialed and nothing is recorded: the member is offline, not answering,
// and a reconnect is judged on the next attempt window instead of waiting out a stale verdict.
func TestRemoteSessionRecheck_OfflineTunnelRecordsNothing(t *testing.T) {
	t.Parallel()

	const prefix = "aim261-tunnel"
	ctx, fx, _ := seedTunneledRecheckFixture(t, prefix, "urn:gram:tunnel:aim261")

	checked, err := fx.ti.service.SweepRemoteSessionRechecks(ctx)
	require.NoError(t, err)
	require.Zero(t, checked, "an offline member is a skip, not a probe")
	sess := storedSession(t, ctx, fx)
	require.False(t, sess.ValidationStatus.Valid, "no verdict is written")
	require.False(t, sess.LastValidatedAt.Valid)
	require.True(t, sess.LastRefreshAttemptAt.Valid, "the claim lease paces the retry")
	require.Empty(t, validationCounts(t, fx.reader))
}
