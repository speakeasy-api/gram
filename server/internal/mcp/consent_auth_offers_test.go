package mcp

import (
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

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
	// No decryptor is available to discovery; malformed stored secrets are a
	// serving/readiness concern, not grounds for hiding the configuration.
	f.service.enc = nil
	offers, err := f.service.configuredAuthOffers(t.Context(), f.orgID, []uuid.UUID{f.serverID})
	require.NoError(t, err)
	headers, err := f.service.gatewayAuthHeaderMetadata(t.Context(), f.orgID, []uuid.UUID{id})
	require.NoError(t, err)
	require.Equal(t, "configured", headers[id][0].StaticValue)
	supported, err := f.service.gatewayOfferCapability(t.Context(), f.orgID, metaMember{serverID: f.serverID, remoteServerID: validUUID(f.remoteID)}, offers[f.serverID], headers)
	require.NoError(t, err)
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
	supported, err := f.service.gatewayOfferCapability(t.Context(), f.orgID, metaMember{serverID: f.serverID, remoteServerID: validUUID(f.remoteID)}, offers[f.serverID], headers)
	require.NoError(t, err)
	require.False(t, supported, "stale backfill falls back to the source's required caller header")
}
