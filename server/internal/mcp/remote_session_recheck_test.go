// Keepalive re-check end to end: an idle grant with no refresh token is probed on the sweep, and the verdict lands without anyone opening the consent page.

package mcp_test

import (
	"context"
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
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
	remotesessions_repo "github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
	tunneledmcprepo "github.com/speakeasy-api/gram/server/internal/tunneledmcp/repo"
)

// makeKeepaliveShaped turns the fixture's grant into the population the sweep owns: no access expiry, no refresh
// token (the fixture never stores one), and a live Gram session for the subject under the endpoint's issuer.
func makeKeepaliveShaped(t *testing.T, ctx context.Context, fx validationFixture, prefix string) {
	t.Helper()
	sess := storedSession(t, ctx, fx)
	require.False(t, sess.RefreshTokenEncrypted.Valid)
	require.NoError(t, remotesessions_repo.New(fx.ti.conn).SetRemoteSessionAccessExpiresAt(ctx, remotesessions_repo.SetRemoteSessionAccessExpiresAtParams{
		AccessExpiresAt: pgtype.Timestamptz{Time: time.Time{}, Valid: false, InfinityModifier: pgtype.Finite},
		ID:              sess.ID,
		ProjectID:       conv.ToNullUUID(fx.endpoint.ProjectID),
	}))
	persistTestUserSession(t, fx.ti, fx.endpoint.UserSessionIssuerID, fx.subject, "jti-"+prefix)
}

// ageIntoRecheckWindow backdates the tracking columns so the grant is due again and its claim lease has lapsed.
func ageIntoRecheckWindow(t *testing.T, ctx context.Context, fx validationFixture) {
	t.Helper()
	sess := storedSession(t, ctx, fx)
	ago := conv.ToPGTimestamptz(time.Now().Add(-25 * time.Hour))
	require.NoError(t, remotesessions_repo.New(fx.ti.conn).SetRemoteSessionValidationTrackingFixture(ctx, remotesessions_repo.SetRemoteSessionValidationTrackingFixtureParams{
		LastValidatedAt:      ago,
		LastRefreshAttemptAt: ago,
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

// A gateway member's grant is placed through the gateway endpoint and judged by the member it routes to.
func TestRemoteSessionRecheck_MetaMemberGrantIsProbedThroughTheGateway(t *testing.T) {
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
func createPrivateTunneledServer(t *testing.T, ctx context.Context, ti *testInstance, projectID, issuerID uuid.UUID, slug, identifier string) uuid.UUID {
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
	return server.ID
}

// A tunneled backend with no live route is never dialed: the grant reads unknown with an offline reason, never a rejection.
func TestRemoteSessionRecheck_OfflineTunnelRecordsUnknown(t *testing.T) {
	t.Parallel()

	const prefix = "aim261-tunnel"
	const identifier = "urn:gram:tunnel:aim261"
	reader, provider := newValidationMeterProvider()
	ctx, ti := newTestMCPServiceWithMetaRuntime(t, provider, mcp.MetaRuntimeConfig{MemberCallTimeout: 0, ValidationTimeout: validationProbeTimeout, AutoVerifyWait: 0, RecheckInterval: 0})
	projectID, orgID := consentTestTenant(t, ctx)
	shared := createUserSessionIssuer(t, ctx, ti.conn, projectID)
	serverID := createPrivateTunneledServer(t, ctx, ti, projectID, shared, prefix+"-server", identifier)
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
	fx := validationFixture{ti: ti, reader: reader, endpoint: endpoint, stateID: "", subject: subject, member: nil, clientID: clientID, name: prefix + "-server"}
	makeKeepaliveShaped(t, ctx, fx, prefix)

	checked, err := ti.service.SweepRemoteSessionRechecks(ctx)
	require.NoError(t, err)
	require.Equal(t, 1, checked)
	sess := storedSession(t, ctx, fx)
	require.Equal(t, string(remotesessions.ValidationOutcomeUnknown), sess.ValidationStatus.String)
	require.Equal(t, fx.name+" is offline", sess.ValidationReason.String)
	require.Equal(t, map[string]int64{"unknown": 1}, validationCounts(t, reader))
}
