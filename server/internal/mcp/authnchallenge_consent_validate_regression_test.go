package mcp_test

import (
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/conv"
	mcpservers_repo "github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/productfeatures"
	productfeaturesrepo "github.com/speakeasy-api/gram/server/internal/productfeatures/repo"
	remotesessionsrepo "github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
	tunneledmcprepo "github.com/speakeasy-api/gram/server/internal/tunneledmcp/repo"
)

// A grant and a sign-in link do not make an unoffered registration available for verification.
func TestConsentValidationRejectsAnUnofferedGatewayRegistration(t *testing.T) {
	t.Parallel()
	ctx, fx := seedMetaValidationFixture(t, "validate-duplicate-meta")
	projectID, orgID := consentTestTenant(t, ctx)
	duplicate := createConsentRemoteClient(t, ctx, fx.ti.conn, projectID, orgID, "validate-duplicate-other", "", []uuid.UUID{fx.endpoint.UserSessionIssuerID})
	insertQualifiedRemoteSessionToken(t, ctx, fx.ti, fx.endpoint.UserSessionIssuerID, duplicate, fx.subject, "token-duplicate-other", fx.member.url)
	_, err := postValidate(t, fx, duplicate)
	var shareable *oops.ShareableError
	require.ErrorAs(t, err, &shareable)
	require.Equal(t, oops.CodeBadRequest, shareable.Code)
	require.Empty(t, fx.member.drain(), "unoffered credentials must not reach the backend")
}

// A gateway member verifies with the card's credential even when another
// member offers two registrations of the same authorization server.
func TestConsentValidationVerifiesAnOfferedGatewayMemberBesideAnotherProvidersDuplicateClients(t *testing.T) {
	t.Parallel()
	ctx, fx := seedMetaValidationFixture(t, "validate-associated-meta")
	projectID, orgID := consentTestTenant(t, ctx)

	first := createConsentRemoteClient(t, ctx, fx.ti.conn, projectID, orgID, "validate-associated-other", "", []uuid.UUID{fx.endpoint.UserSessionIssuerID})
	otherIssuer := clientRemoteIssuerID(t, ctx, fx.ti.conn, projectID, orgID, first)
	q := remotesessionsrepo.New(fx.ti.conn)
	second, err := q.CreateRemoteSessionClient(ctx, remotesessionsrepo.CreateRemoteSessionClientParams{
		ProjectID:             conv.ToNullUUID(projectID),
		OrganizationID:        conv.ToPGTextEmpty(orgID),
		RemoteSessionIssuerID: otherIssuer,
		ClientID:              "validate-associated-other-second-external-client",
		ClientIDIssuedAt:      pgtype.Timestamptz{Time: time.Now(), Valid: true},
		CallbackBaseUrl:       conv.ToPGTextEmpty(""),
	})
	require.NoError(t, err)

	sibling := newValidationMember(t)
	server := createMetaMember(t, ctx, fx.ti.conn, projectID, fx.endpoint.MetaMcpServerID.UUID, "validate-associated-sibling", sibling.url, conv.ToNullUUID(otherIssuer), 1)
	seedMetaMemberConnectGrant(t, ctx, fx.ti.conn, orgID, server.mcpServerID)
	seedConsentAuthOffer(t, ctx, fx.ti, orgID, projectID, server.mcpServerID, first, authz.AuthClientTypeRemoteSession)
	seedConsentAuthOffer(t, ctx, fx.ti, orgID, projectID, server.mcpServerID, second.ID, authz.AuthClientTypeRemoteSession)
	insertQualifiedRemoteSessionToken(t, ctx, fx.ti, fx.endpoint.UserSessionIssuerID, first, fx.subject, "token-other-first", sibling.url)
	insertQualifiedRemoteSessionToken(t, ctx, fx.ti, fx.endpoint.UserSessionIssuerID, second.ID, fx.subject, "token-other-second", sibling.url)

	requireValidated(t, fx)
	requireProbe(t, fx.member.drain(), "token-validate-associated-meta", true, true)
	require.Empty(t, sibling.drain(), "the other member must not be probed")
}

// Where upstream auth is enforced, a gateway member's typed auth client
// decides its credential, so a card that client offers verifies even when the
// sign-in also binds another client of the same authorization server, with
// its own grant for the member's upstream.
func TestConsentValidationVerifiesATypedOfferBesideASameProviderClientUnderEnforcement(t *testing.T) {
	t.Parallel()
	ctx, fx := seedMetaValidationFixture(t, "validate-typed-meta")
	projectID, orgID := consentTestTenant(t, ctx)
	_, err := productfeaturesrepo.New(fx.ti.conn).EnableFeature(ctx, productfeaturesrepo.EnableFeatureParams{OrganizationID: orgID, FeatureName: string(productfeatures.FeatureUpstreamAuthEnforced)})
	require.NoError(t, err)
	fx.ti.service.SetAuthClientPermission(func() bool { return true })

	q := remotesessionsrepo.New(fx.ti.conn)
	duplicate, err := q.CreateRemoteSessionClient(ctx, remotesessionsrepo.CreateRemoteSessionClientParams{
		ProjectID:             conv.ToNullUUID(projectID),
		OrganizationID:        conv.ToPGTextEmpty(orgID),
		RemoteSessionIssuerID: clientRemoteIssuerID(t, ctx, fx.ti.conn, projectID, orgID, fx.clientID),
		ClientID:              "validate-typed-meta-duplicate-external-client",
		ClientIDIssuedAt:      pgtype.Timestamptz{Time: time.Now(), Valid: true},
		CallbackBaseUrl:       conv.ToPGTextEmpty(""),
	})
	require.NoError(t, err)
	insertQualifiedRemoteSessionToken(t, ctx, fx.ti, fx.endpoint.UserSessionIssuerID, duplicate.ID, fx.subject, "token-typed-duplicate", fx.member.url)

	requireValidated(t, fx)
	requireProbe(t, fx.member.drain(), "token-validate-typed-meta", true, true)
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
	seedConsentAuthOffer(t, ctx, ti, orgID, projectID, server.ID, owner, authz.AuthClientTypeRemoteSession)
	insertQualifiedRemoteSessionToken(t, ctx, ti, shared, owner, subject, "token-owner", "")
	foreign := createConsentRemoteClient(t, ctx, ti.conn, projectID, orgID, "validate-selected-foreign", "", []uuid.UUID{shared})
	seedConsentAuthOffer(t, ctx, ti, orgID, projectID, server.ID, foreign, authz.AuthClientTypeRemoteSession)
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
		require.Equal(t, "Bearer token-foreign", header.Get("Authorization"), "the probe uses the selected card's credential")
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

// Direct Verify scopes typed offers to the target server and selects the posted registration.
func TestConsentValidationDirectServerScopesTypedOffers(t *testing.T) {
	t.Parallel()
	for _, relevant := range []bool{false, true} {
		name := "unrelated"
		if relevant {
			name = "relevant"
		}
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			prefix := "validate-offers-" + name
			ctx, fx := seedStandaloneValidationFixture(t, prefix)
			projectID, orgID := consentTestTenant(t, ctx)
			first := fx.clientID
			boundServer := fx.endpoint.McpServerID.UUID
			resource := fx.member.url
			var unrelated *validationMember
			if !relevant {
				unrelated = newValidationMember(t)
				resource = unrelated.url
				boundServer = createPublicRemoteMcpServer(t, ctx, fx.ti.conn, projectID, fx.endpoint.UserSessionIssuerID, prefix+"-sibling", resource)
				first = createConsentRemoteClient(t, ctx, fx.ti.conn, projectID, orgID, prefix+"-other", "", []uuid.UUID{fx.endpoint.UserSessionIssuerID})
				seedConsentAuthOffer(t, ctx, fx.ti, orgID, projectID, boundServer, first, authz.AuthClientTypeRemoteSession)
				insertQualifiedRemoteSessionToken(t, ctx, fx.ti, fx.endpoint.UserSessionIssuerID, first, fx.subject, "token-other", resource)
			}
			providerID := clientRemoteIssuerID(t, ctx, fx.ti.conn, projectID, orgID, first)
			duplicate, err := remotesessionsrepo.New(fx.ti.conn).CreateRemoteSessionClient(ctx, remotesessionsrepo.CreateRemoteSessionClientParams{
				ProjectID: conv.ToNullUUID(projectID), OrganizationID: conv.ToPGText(orgID), RemoteSessionIssuerID: providerID,
				ClientID: prefix + "-duplicate", ClientIDIssuedAt: pgtype.Timestamptz{Time: time.Now(), Valid: true},
			})
			require.NoError(t, err)
			seedConsentAuthOffer(t, ctx, fx.ti, orgID, projectID, boundServer, duplicate.ID, authz.AuthClientTypeRemoteSession)
			insertQualifiedRemoteSessionToken(t, ctx, fx.ti, fx.endpoint.UserSessionIssuerID, duplicate.ID, fx.subject, "token-duplicate", resource)

			requireValidated(t, fx)
			requireProbe(t, fx.member.drain(), "token-"+prefix, true, true)
			if relevant {
				fx.clientID = duplicate.ID
				requireValidated(t, fx)
				requireProbe(t, fx.member.drain(), "token-duplicate", true, true)
			} else {
				_, err := postValidate(t, fx, first)
				var refused *oops.ShareableError
				require.ErrorAs(t, err, &refused)
				require.Equal(t, oops.CodeBadRequest, refused.Code, "an unrelated card cannot be verified on this endpoint")
				require.Empty(t, fx.member.drain(), "an unrelated card must not probe the target")
				require.Empty(t, unrelated.drain(), "an unrelated card must not probe its own backend")
			}
		})
	}
}
