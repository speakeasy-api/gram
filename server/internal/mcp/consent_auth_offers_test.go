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

func TestConfiguredAuthOfferCapability(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name       string
		clientType string
		tunneled   bool
		supported  bool
		oauth      bool
		personal   bool
	}{
		{"personal", authz.AuthClientTypeRemoteSession, false, true, true, true},
		{"shared", authz.AuthClientTypeServiceAccount, false, true, true, false},
		{"identity provider", authz.AuthClientTypeEMA, false, true, true, true},
		{"static", authz.AuthClientTypeStaticCredential, false, true, false, false},
		{"caller headers", authz.AuthClientTypePassThrough, false, true, false, false},
		{"tunneled static unsupported", authz.AuthClientTypeStaticCredential, true, false, false, false},
		{"tunneled personal", authz.AuthClientTypeRemoteSession, true, true, true, true},
		{"unknown", "unknown", false, false, false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			registration := uuid.New()
			offer := configuredAuthOffer{clients: []upstreamclientsrepo.ListServerConsentAuthClientsRow{{
				AuthClientID:          uuid.NullUUID{UUID: uuid.New(), Valid: true},
				ClientType:            pgtype.Text{String: tc.clientType, Valid: true},
				RemoteSessionClientID: uuid.NullUUID{UUID: registration, Valid: true},
			}}}
			supported, oauth := offer.gatewayCapability(tc.tunneled, nil)
			require.Equal(t, tc.supported, supported)
			require.Equal(t, tc.oauth, oauth)
			if tc.personal {
				require.Equal(t, []uuid.UUID{registration}, offer.personalRegistrations())
			} else {
				require.Empty(t, offer.personalRegistrations(), "shared credentials must never become personal consent cards")
			}
		})
	}
}

func TestConfiguredAuthOfferPendingCapability(t *testing.T) {
	t.Parallel()
	for _, pending := range []bool{false, true} {
		offer := configuredAuthOffer{pending: pending}
		supported, oauth := offer.gatewayCapability(false, nil)
		require.True(t, supported, "transport capability alone does not determine pending discovery eligibility")
		require.False(t, oauth)
		require.Empty(t, offer.personalRegistrations())
	}
}

func TestConfiguredAuthOfferCallerHeaders(t *testing.T) {
	t.Parallel()
	id := uuid.New()
	offer := configuredAuthOffer{clients: []upstreamclientsrepo.ListServerConsentAuthClientsRow{{
		AuthClientID: uuid.NullUUID{UUID: id, Valid: true},
		ClientType:   pgtype.Text{String: authz.AuthClientTypeRemoteSession, Valid: true},
	}}}
	for _, tc := range []struct {
		name      string
		headers   []proxy.ConfiguredHeader
		supported bool
	}{
		{"auxiliary required caller header", []proxy.ConfiguredHeader{{Name: "X-Key", ValueFromRequestHeader: "X-Key", IsRequired: true}}, false},
		{"OAuth replaces authorization", []proxy.ConfiguredHeader{{Name: "Authorization", ValueFromRequestHeader: "Authorization", IsRequired: true}}, true},
		{"static auxiliary header", []proxy.ConfiguredHeader{{Name: "X-Key", StaticValue: "configured", IsRequired: true}}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			supported, _ := offer.gatewayCapability(false, map[uuid.UUID][]proxy.ConfiguredHeader{id: tc.headers})
			require.Equal(t, tc.supported, supported)
		})
	}
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
	supported := gatewayOfferCapability(member, offers[f.serverID], metadata)
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
	supported := gatewayOfferCapability(member, offers[f.serverID], metadata)
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
		require.True(t, gatewayOfferCapability(member, offers[member.serverID], metadata))
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
