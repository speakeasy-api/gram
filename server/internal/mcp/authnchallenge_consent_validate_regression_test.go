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

	"github.com/speakeasy-api/gram/server/internal/conv"
	mcpservers_repo "github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
	tunneledmcprepo "github.com/speakeasy-api/gram/server/internal/tunneledmcp/repo"
)

func TestConsentValidationRejectsDuplicateCredentials(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		seed func(*testing.T, string) (context.Context, validationFixture)
	}{
		{name: "meta", seed: seedMetaValidationFixture},
		{name: "standalone", seed: seedStandaloneValidationFixture},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ctx, fx := tc.seed(t, "validate-duplicate-"+tc.name)
			projectID, orgID := consentTestTenant(t, ctx)
			duplicate := createConsentRemoteClient(t, ctx, fx.ti.conn, projectID, orgID, "validate-duplicate-other", "", []uuid.UUID{fx.endpoint.UserSessionIssuerID})
			insertQualifiedRemoteSessionToken(t, ctx, fx.ti, fx.endpoint.UserSessionIssuerID, duplicate, fx.subject, "token-duplicate-other", fx.member.url)
			_, err := postValidate(t, fx, fx.clientID)
			var shareable *oops.ShareableError
			require.ErrorAs(t, err, &shareable)
			require.Equal(t, oops.CodeBadRequest, shareable.Code)
			require.ErrorContains(t, err, "cannot be verified")
			require.Empty(t, fx.member.drain(), "ambiguous credentials must not reach the backend")
		})
	}
}

func TestConsentValidationStandaloneTunnelWillNotProbeWithAnotherCardsCredential(t *testing.T) {
	t.Parallel()
	reader, provider := newValidationMeterProvider()
	ctx, ti := newTestMCPServiceWithValidationTimeout(t, provider, validationProbeTimeout)
	projectID, orgID := consentTestTenant(t, ctx)
	shared := createUserSessionIssuer(t, ctx, ti.conn, projectID)

	tunneled, err := tunneledmcprepo.New(ti.conn).CreateServer(ctx, tunneledmcprepo.CreateServerParams{
		ID: uuid.New(), ProjectID: projectID, Name: "validate-selected-tunnel", KeyHash: uuid.NewString(), KeyPrefix: "gram_tunnel_test", ResourceIdentifier: pgtype.Text{},
	})
	require.NoError(t, err)
	server, err := mcpservers_repo.New(ti.conn).CreateMCPServer(ctx, mcpservers_repo.CreateMCPServerParams{
		ID: uuid.New(), ProjectID: projectID, Name: conv.ToPGText("validate-selected-tunnel"), Slug: conv.ToPGText("validate-selected-tunnel"), TunneledMcpServerID: conv.ToNullUUID(tunneled.ID), Visibility: "public", UserSessionIssuerID: conv.ToNullUUID(shared),
	})
	require.NoError(t, err)
	endpoint, stateID, subject := mintFirstPartyConsentState(t, ctx, ti, projectID, orgID, shared, "validate-selected-tunnel")
	endpoint.McpServerID = conv.ToNullUUID(server.ID)

	owner := createConsentRemoteClient(t, ctx, ti.conn, projectID, orgID, "validate-selected-tunnel", "", []uuid.UUID{shared})
	require.NoError(t, remotesessions.ResyncMCPServerRemoteSessionIssuers(ctx, ti.conn, orgID, projectID, []uuid.UUID{shared}))
	insertQualifiedRemoteSessionToken(t, ctx, ti, shared, owner, subject, "token-owner", "")
	foreign := createConsentRemoteClient(t, ctx, ti.conn, projectID, orgID, "validate-selected-foreign", "", []uuid.UUID{shared})
	insertQualifiedRemoteSessionToken(t, ctx, ti, shared, foreign, subject, "token-foreign", "")

	gateway := &fakeTunnelGateway{t: t, agentSessionID: "agent-1", backendSessionID: "backend-session", mu: sync.Mutex{}}
	gatewayServer := httptest.NewServer(gateway)
	t.Cleanup(gatewayServer.Close)
	require.NoError(t, ti.tunnelRoutes.Publish(ctx, tunneled.ID.String(), gatewayServer.URL, time.Hour))
	fx := validationFixture{ti: ti, reader: reader, endpoint: endpoint, stateID: stateID, subject: subject, clientID: foreign, name: "validate-selected-tunnel"}

	_, err = postValidate(t, fx, foreign)
	require.Error(t, err)
	var shareable *oops.ShareableError
	require.ErrorAs(t, err, &shareable)
	require.Equal(t, oops.CodeBadRequest, shareable.Code)
	require.ErrorContains(t, err, "cannot be verified")
	headers, _ := tunnelForwards(gateway)
	require.Empty(t, headers, "a foreign tunnel issuer must not probe with the owning card")
}

func TestConsentValidationTimeoutStillDeletesMintedSession(t *testing.T) {
	t.Parallel()
	ctx, fx := seedStandaloneValidationFixture(t, "validate-timeout")
	fx.member.set(memberHangsAck)
	requireValidated(t, fx)
	requireProbe(t, fx.member.drain(), "token-validate-timeout", true, true)
	session := storedSession(t, ctx, fx)
	require.Equal(t, "unknown", session.ValidationStatus.String)
	require.Equal(t, fx.name+" did not answer in time", session.ValidationReason.String)
	require.Equal(t, map[string]int64{"unknown": 1}, validationCounts(t, fx.reader))
}
