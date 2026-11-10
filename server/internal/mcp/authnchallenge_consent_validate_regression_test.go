package mcp_test

import (
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

// On a gateway, two clients holding grants for one member leave the member
// without a decided client, so neither is offered for verification.
func TestConsentValidationRefusesAGatewayMemberWithAmbiguousClients(t *testing.T) {
	t.Parallel()
	ctx, fx := seedMetaValidationFixture(t, "validate-duplicate-meta")
	projectID, orgID := consentTestTenant(t, ctx)
	duplicate := createConsentRemoteClient(t, ctx, fx.ti.conn, projectID, orgID, "validate-duplicate-other", "", []uuid.UUID{fx.endpoint.UserSessionIssuerID})
	insertQualifiedRemoteSessionToken(t, ctx, fx.ti, fx.endpoint.UserSessionIssuerID, duplicate, fx.subject, "token-duplicate-other", fx.member.url)
	_, err := postValidate(t, fx, fx.clientID)
	var shareable *oops.ShareableError
	require.ErrorAs(t, err, &shareable)
	require.Equal(t, oops.CodeBadRequest, shareable.Code)
	require.Empty(t, fx.member.drain(), "ambiguous credentials must not reach the backend")
}

// A standalone server verifies the card's own client with that client's
// credential alone; another client's grant for the same upstream is never a
// competing candidate.
func TestConsentValidationProbesWithTheCardsOwnCredentialBesideAnotherClients(t *testing.T) {
	t.Parallel()
	ctx, fx := seedStandaloneValidationFixture(t, "validate-duplicate-standalone")
	projectID, orgID := consentTestTenant(t, ctx)
	other := createConsentRemoteClient(t, ctx, fx.ti.conn, projectID, orgID, "validate-duplicate-other", "", []uuid.UUID{fx.endpoint.UserSessionIssuerID})
	insertQualifiedRemoteSessionToken(t, ctx, fx.ti, fx.endpoint.UserSessionIssuerID, other, fx.subject, "token-duplicate-other", fx.member.url)
	requireValidated(t, fx)
	requireProbe(t, fx.member.drain(), "token-validate-duplicate-standalone", true, true)
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
	require.NoError(t, err)
	headers, _ := tunnelForwards(gateway)
	require.NotEmpty(t, headers, "the card's own credential is probed")
	for _, header := range headers {
		require.NotContains(t, header.Get("Authorization"), "token-owner", "a card never probes with another card's credential")
	}
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
