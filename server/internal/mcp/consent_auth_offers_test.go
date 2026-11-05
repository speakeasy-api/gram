package mcp

import (
	"context"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"sync/atomic"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	authclientsrepo "github.com/speakeasy-api/gram/server/internal/authclients/repo"
	"github.com/speakeasy-api/gram/server/internal/authz"
	upstreamclientsrepo "github.com/speakeasy-api/gram/server/internal/mcpservers/upstreamclients/repo"
	"github.com/speakeasy-api/gram/server/internal/remotemcp/proxy"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

func TestConfiguredAuthOfferPersonalRegistrations(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		clientType string
		personal   bool
	}{
		{authz.AuthClientTypeRemoteSession, true},
		{authz.AuthClientTypeEMA, true},
		{authz.AuthClientTypeServiceAccount, false},
		{authz.AuthClientTypeStaticCredential, false},
		{authz.AuthClientTypePassThrough, false},
	} {
		t.Run(tc.clientType, func(t *testing.T) {
			t.Parallel()
			registration := uuid.New()
			offer := configuredAuthOffer{clients: []upstreamclientsrepo.ListServerConsentAuthClientsRow{{
				AuthClientID:          uuid.NullUUID{UUID: uuid.New(), Valid: true},
				ClientType:            pgtype.Text{String: tc.clientType, Valid: true},
				RemoteSessionClientID: uuid.NullUUID{UUID: registration, Valid: true},
			}}}
			if tc.personal {
				require.Equal(t, []uuid.UUID{registration}, offer.personalRegistrations())
			} else {
				require.Empty(t, offer.personalRegistrations(), "shared credentials must never become personal consent cards")
			}
		})
	}
}

// Discovery must agree with what execution serves for the same offered client.
func TestGatewayOfferCapabilityFollowsServingResolver(t *testing.T) {
	t.Parallel()
	type row = upstreamclientsrepo.GetAuthClientSelectionRow
	same := func(*row) {}
	for _, tc := range []struct {
		name       string
		clientType string
		edit       func(*row)
		tunneled   bool
		associated bool
		linked     bool
		headers    []proxy.ConfiguredHeader
		supported  bool
	}{
		{name: "personal without a sign-in link", clientType: authClientTypeRemoteSession, edit: same, supported: true},
		{name: "shared without a sign-in link", clientType: authClientTypeServiceAccount, edit: same, supported: true},
		{name: "static", clientType: authClientTypeStaticCredential, edit: same, supported: true},
		{name: "unknown type", clientType: "unknown", edit: same},
		{name: "disabled client", clientType: authClientTypeRemoteSession, edit: func(r *row) { r.ClientEnabled.Bool = false }},
		{name: "deleted registration", clientType: authClientTypeRemoteSession, edit: func(r *row) { r.RegistrationLive = false }},
		{name: "detached registration", clientType: authClientTypeRemoteSession, edit: func(r *row) { r.RegistrationAttached = false }},
		{name: "uncovered credential header", clientType: authClientTypeRemoteSession, edit: func(r *row) { r.CredentialHeadersUncovered = true }},
		{name: "identity provider without a sign-in link", clientType: authClientTypeEMA, edit: same, associated: true},
		{name: "identity provider not associated", clientType: authClientTypeEMA, edit: same, linked: true},
		{name: "identity provider linked", clientType: authClientTypeEMA, edit: same, associated: true, linked: true, supported: true},
		{name: "chaining without a sign-in link", clientType: authClientTypeRemoteSession, edit: func(r *row) { r.RegistrationHasEma = true }, associated: true},
		{name: "chaining linked", clientType: authClientTypeRemoteSession, edit: func(r *row) { r.RegistrationHasEma = true }, associated: true, linked: true, supported: true},
		{name: "stale OAuth headers without a sign-in link", clientType: authClientTypeServiceAccount, edit: func(r *row) { r.HeadersStale = true }, associated: true},
		{name: "stale OAuth headers linked", clientType: authClientTypeServiceAccount, edit: func(r *row) { r.HeadersStale = true }, associated: true, linked: true, supported: true},
		{name: "stale static headers", clientType: authClientTypeStaticCredential, edit: func(r *row) { r.HeadersStale = true }, supported: true},
		{name: "stale static headers beside a registration", clientType: authClientTypeStaticCredential, edit: func(r *row) { r.HeadersStale = true }, associated: true},
		{name: "required caller header", clientType: authClientTypeRemoteSession, edit: same, headers: []proxy.ConfiguredHeader{{Name: "X-Key", ValueFromRequestHeader: "X-Key", IsRequired: true}}},
		{name: "OAuth replaces authorization", clientType: authClientTypeRemoteSession, edit: same, headers: []proxy.ConfiguredHeader{{Name: "Authorization", ValueFromRequestHeader: "Authorization", IsRequired: true}}, supported: true},
		{name: "static auxiliary header", clientType: authClientTypeRemoteSession, edit: same, headers: []proxy.ConfiguredHeader{{Name: "X-Key", StaticValue: "configured", IsRequired: true}}, supported: true},
		{name: "tunneled personal", clientType: authClientTypeRemoteSession, edit: same, tunneled: true, supported: true},
		{name: "tunneled personal with a deleted registration", clientType: authClientTypeRemoteSession, edit: func(r *row) { r.RegistrationLive = false }, tunneled: true},
		{name: "tunneled static", clientType: authClientTypeStaticCredential, edit: same, tunneled: true, supported: true},
		{name: "tunneled static beside a registration", clientType: authClientTypeStaticCredential, edit: same, tunneled: true, associated: true},
		{name: "tunneled identity provider without a sign-in link", clientType: authClientTypeEMA, edit: same, tunneled: true, associated: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			serverID, clientID, registration := uuid.New(), uuid.New(), uuid.New()
			selection := row{
				McpServerID:          serverID,
				AuthClientID:         validUUID(clientID),
				ClientID:             validUUID(clientID),
				ClientType:           pgtype.Text{String: tc.clientType, Valid: true},
				ClientEnabled:        pgtype.Bool{Bool: true, Valid: true},
				ClientDeleted:        pgtype.Bool{Bool: false, Valid: true},
				RegistrationID:       validUUID(registration),
				RegistrationLive:     true,
				RegistrationAttached: true,
				Associated:           true,
				RequiresUpstreamAuth: true,
				Tunneled:             tc.tunneled,
			}
			if isHeadersOnlyType(tc.clientType) {
				selection.RegistrationID = uuid.NullUUID{}
			}
			tc.edit(&selection)
			member := metaMember{serverID: serverID, remoteServerID: validUUID(uuid.New())}
			if tc.tunneled {
				member = metaMember{serverID: serverID, tunneledServerID: validUUID(uuid.New())}
			}
			if tc.associated {
				member.associatedClients = map[uuid.UUID]bool{registration: true}
			}
			offer := configuredAuthOffer{clients: []upstreamclientsrepo.ListServerConsentAuthClientsRow{{
				AuthClientID: validUUID(clientID),
				ClientType:   pgtype.Text{String: tc.clientType, Valid: true},
			}}}
			metadata := gatewayOfferMetadata{
				headers:    map[uuid.UUID][]proxy.ConfiguredHeader{clientID: tc.headers},
				selections: map[[2]uuid.UUID]upstreamclientsrepo.GetAuthClientSelectionRow{{serverID, clientID}: selection},
			}
			require.Equal(t, tc.supported, gatewayOfferCapability(member, offer, metadata, map[uuid.UUID]bool{registration: tc.linked}))
		})
	}
}

func TestGatewayOfferCapabilityDropsDeletedRegistration(t *testing.T) {
	t.Parallel()
	pool, err := TestInfra.CloneTestDatabase(t, "mcp_gateway_offer_registration_gone")
	require.NoError(t, err)
	f := newAuthClientFixture(t, pool)
	f.oauthClient(authClientTypeRemoteSession)
	member := metaMember{serverID: f.serverID, remoteServerID: validUUID(f.remoteID), associatedClients: map[uuid.UUID]bool{f.registration: true}}
	capability := func() bool {
		offers, err := f.service.configuredAuthOffers(t.Context(), f.orgID, []uuid.UUID{f.serverID})
		require.NoError(t, err)
		require.Len(t, offers[f.serverID].clients, 1)
		metadata, err := f.service.loadGatewayOfferMetadata(t.Context(), f.orgID, []metaMember{member}, offers)
		require.NoError(t, err)
		return gatewayOfferCapability(member, offers[f.serverID], metadata, nil)
	}
	require.True(t, capability())
	require.NoError(t, f.q.SoftDeleteRemoteSessionClientFixture(t.Context(), f.registration))
	require.False(t, capability(), "execution refuses a client whose registration is gone")
}

func TestGatewayOfferCapabilityTunneledHeaderClient(t *testing.T) {
	t.Parallel()
	pool, err := TestInfra.CloneTestDatabase(t, "mcp_gateway_offer_tunneled")
	require.NoError(t, err)
	f := newAuthClientFixture(t, pool)
	f.tunneled()
	id := f.adminClient(authClientTypeStaticCredential, uuid.NullUUID{})
	offers, err := f.service.configuredAuthOffers(t.Context(), f.orgID, []uuid.UUID{f.serverID})
	require.NoError(t, err)
	member := metaMember{serverID: f.serverID, tunneledServerID: validUUID(uuid.New())}
	metadata, err := f.service.loadGatewayOfferMetadata(t.Context(), f.orgID, []metaMember{member}, offers)
	require.NoError(t, err)
	require.Contains(t, metadata.selections, [2]uuid.UUID{f.serverID, id})
	require.True(t, gatewayOfferCapability(member, offers[f.serverID], metadata, nil), "the tunnel serves a header client with its own headers")
	member.associatedClients = map[uuid.UUID]bool{f.registration: true}
	require.False(t, gatewayOfferCapability(member, offers[f.serverID], metadata, nil))
}

func TestConfiguredAuthOffersMissingIsNotNoAuth(t *testing.T) {
	t.Parallel()
	pool, err := TestInfra.CloneTestDatabase(t, "mcp_configured_auth_offers")
	require.NoError(t, err)
	f := newAuthClientFixture(t, pool)
	missing := uuid.New()
	offers, err := f.service.configuredAuthOffers(t.Context(), f.orgID, []uuid.UUID{f.serverID, missing})
	require.NoError(t, err)
	require.Contains(t, offers, f.serverID)
	require.NotContains(t, offers, missing)
	otherTenant, err := f.service.configuredAuthOffers(t.Context(), "org_other", []uuid.UUID{f.serverID})
	require.NoError(t, err)
	require.Empty(t, otherTenant)
	choices, err := f.service.consentAuthChoices(t.Context(), f.orgID, urn.NewUserSubject("user_1"), []consentAuthServer{{id: missing, projectID: f.projectID}, {id: f.serverID, projectID: f.projectID}}, nil, nil, false)
	require.NoError(t, err)
	require.Equal(t, consentAuthSetupIncomplete, choices[missing].Status)
	require.Equal(t, consentAuthNoAuth, choices[f.serverID].Status)
}

func TestConfiguredAuthOffersDoNotDecryptOrVetoAdminHeaders(t *testing.T) {
	t.Parallel()
	pool, err := TestInfra.CloneTestDatabase(t, "mcp_gateway_offer_metadata")
	require.NoError(t, err)
	f := newAuthClientFixture(t, pool)
	f.requireSourceHeader(f.sourceHeader("X-API-Key", true, pgtype.Text{}, text("X-API-Key")))
	id := f.adminClient(authClientTypeStaticCredential, uuid.NullUUID{})
	f.copyHeader(id, "X-API-Key", true, true, text("not-valid-ciphertext"), pgtype.Text{})
	// Discovery must work without decrypting stored secrets.
	f.service.enc = nil
	offers, err := f.service.configuredAuthOffers(t.Context(), f.orgID, []uuid.UUID{f.serverID})
	require.NoError(t, err)
	headers, err := f.service.gatewayAuthHeaderMetadata(t.Context(), f.orgID, []uuid.UUID{id})
	require.NoError(t, err)
	require.Equal(t, "configured", headers[id][0].StaticValue)
	member := metaMember{serverID: f.serverID, remoteServerID: validUUID(f.remoteID)}
	metadata, err := f.service.loadGatewayOfferMetadata(t.Context(), f.orgID, []metaMember{member}, offers)
	require.NoError(t, err)
	supported := gatewayOfferCapability(member, offers[f.serverID], metadata, nil)
	require.True(t, supported, "administrator client replaces the source credential headers")
}

func TestConfiguredAuthOffersBackfillUsesLegacyHeaderSource(t *testing.T) {
	t.Parallel()
	pool, err := TestInfra.CloneTestDatabase(t, "mcp_gateway_offer_backfill")
	require.NoError(t, err)
	f := newAuthClientFixture(t, pool)
	f.requireSourceHeader(f.sourceHeader("X-API-Key", true, pgtype.Text{}, text("X-API-Key")))
	id := f.client(authClientTypeStaticCredential, uuid.NullUUID{}, true)
	f.copyHeader(id, "X-API-Key", true, true, text("old-static-value"), pgtype.Text{})
	offers, err := f.service.configuredAuthOffers(t.Context(), f.orgID, []uuid.UUID{f.serverID})
	require.NoError(t, err)
	headers, err := f.service.gatewayAuthHeaderMetadata(t.Context(), f.orgID, []uuid.UUID{id})
	require.NoError(t, err)
	require.NotEmpty(t, headers)
	member := metaMember{serverID: f.serverID, remoteServerID: validUUID(f.remoteID)}
	metadata, err := f.service.loadGatewayOfferMetadata(t.Context(), f.orgID, []metaMember{member}, offers)
	require.NoError(t, err)
	supported := gatewayOfferCapability(member, offers[f.serverID], metadata, nil)
	require.False(t, supported, "stale backfill falls back to the source's required caller header")
}

func TestGatewayOfferMetadataBatchesMembersAndClients(t *testing.T) {
	t.Parallel()
	pool, err := TestInfra.CloneTestDatabase(t, "mcp_gateway_offer_batch")
	require.NoError(t, err)
	f := newAuthClientFixture(t, pool)
	var members []metaMember
	var ids []uuid.UUID
	for range 3 {
		f.serverID, f.remoteID = f.server()
		f.requireSourceHeader(f.sourceHeader("X-API-Key", true, pgtype.Text{}, text("X-API-Key")))
		admin := f.adminClient(authClientTypeStaticCredential, uuid.NullUUID{})
		f.copyHeader(admin, "X-API-Key", true, true, text("not-ciphertext"), pgtype.Text{})
		stale := f.client(authClientTypeStaticCredential, uuid.NullUUID{}, true)
		f.copyHeader(stale, "X-API-Key", true, true, text("old-copy"), pgtype.Text{})
		members = append(members, metaMember{serverID: f.serverID, remoteServerID: validUUID(f.remoteID)})
		ids = append(ids, f.serverID)
	}
	offers, err := f.service.configuredAuthOffers(t.Context(), f.orgID, ids)
	require.NoError(t, err)
	tracer := &gatewayMetadataQueryCounter{}
	config := pool.Config()
	config.ConnConfig.Tracer = tracer
	countedPool, err := pgxpool.NewWithConfig(t.Context(), config)
	require.NoError(t, err)
	defer countedPool.Close()
	f.service.db = countedPool
	f.service.enc = nil
	metadata, err := f.service.loadGatewayOfferMetadata(t.Context(), f.orgID, members, offers)
	require.NoError(t, err)
	require.EqualValues(t, 3, tracer.queries.Load(), "header and selection reads are constant, not per member or client")
	require.Len(t, metadata.selections, 6)
	for _, member := range members {
		require.True(t, gatewayOfferCapability(member, offers[member.serverID], metadata, nil))
		for _, client := range offers[member.serverID].clients {
			single, err := upstreamclientsrepo.New(pool).GetAuthClientSelection(t.Context(), upstreamclientsrepo.GetAuthClientSelectionParams{
				Proposed: true, ProposedAuthClientID: client.AuthClientID, McpServerIds: []uuid.UUID{member.serverID}, OrganizationID: f.orgID,
			})
			require.NoError(t, err)
			require.Len(t, single, 1)
			require.Equal(t, single[0], metadata.selections[[2]uuid.UUID{member.serverID, client.AuthClientID.UUID}], "batch must preserve the serving resolver's decision inputs")
		}
	}
	other, err := f.service.loadGatewayOfferMetadata(t.Context(), "org_other", members, offers)
	require.NoError(t, err)
	require.Empty(t, other.source)
	require.Empty(t, other.headers)
	require.Empty(t, other.selections)
}

type gatewayMetadataQueryCounter struct{ queries atomic.Int64 }

func (q *gatewayMetadataQueryCounter) TraceQueryStart(ctx context.Context, _ *pgx.Conn, _ pgx.TraceQueryStartData) context.Context {
	q.queries.Add(1)
	return ctx
}

func (*gatewayMetadataQueryCounter) TraceQueryEnd(context.Context, *pgx.Conn, pgx.TraceQueryEndData) {
}

func TestGatewayOfferMetadataDetachedClientRetainsSourceRequirements(t *testing.T) {
	t.Parallel()
	pool, err := TestInfra.CloneTestDatabase(t, "mcp_gateway_offer_detached")
	require.NoError(t, err)
	f := newAuthClientFixture(t, pool)
	f.requireSourceHeader(f.sourceHeader("X-API-Key", true, pgtype.Text{}, text("X-API-Key")))
	// A stored secret must not be decrypted, even after the last link is removed.
	f.requireSourceHeader(f.sourceHeader("X-Other-Key", true, text("not-ciphertext"), pgtype.Text{}))
	id := f.adminClient(authClientTypeStaticCredential, uuid.NullUUID{})
	_, err = authclientsrepo.New(pool).DetachAuthClientFromServer(t.Context(), authclientsrepo.DetachAuthClientFromServerParams{OrganizationID: f.orgID, McpServerID: f.serverID, AuthClientID: id})
	require.NoError(t, err)
	offers, err := f.service.configuredAuthOffers(t.Context(), f.orgID, []uuid.UUID{f.serverID})
	require.NoError(t, err)
	require.Empty(t, offers[f.serverID].clients)
	require.False(t, offers[f.serverID].pending, "a historical link suppresses pending despite remaining source headers")
	f.service.enc = nil
	member := metaMember{serverID: f.serverID, remoteServerID: validUUID(f.remoteID)}
	metadata, err := f.service.loadGatewayOfferMetadata(t.Context(), f.orgID, []metaMember{member}, offers)
	require.NoError(t, err)
	require.Len(t, metadata.source[f.remoteID], 2)
	supported, _ := remoteMemberAuth(metadata.source[f.remoteID], false)
	require.False(t, supported, "detached clients do not erase required caller headers")
}
