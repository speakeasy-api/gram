package mcp

import (
	"context"
	"net/url"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/authenforcement"
	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/feature"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/mcpservers/upstreamclients/authclientbackfill"
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

func TestDirectConsentConnectionCards(t *testing.T) {
	t.Parallel()
	pool, err := TestInfra.CloneTestDatabase(t, "mcp_direct_consent_cards")
	require.NoError(t, err)
	for _, tc := range []struct {
		name         string
		clientType   string
		state        string
		enforced     bool
		shadow       bool
		hosted       bool
		issuerLinked bool
		wantCard     bool
		wantTyped    bool
		reconnect    string
	}{
		{name: "reconnect excludes unrelated typed card", clientType: authClientTypeRemoteSession, enforced: true, reconnect: "other"},
		{name: "reconnect retains bound typed card", clientType: authClientTypeRemoteSession, enforced: true, reconnect: "bound", wantCard: true, wantTyped: true},
		{name: "legacy only enforced", enforced: true, issuerLinked: true},
		{name: "personal without issuer link", clientType: authClientTypeRemoteSession, enforced: true, wantCard: true, wantTyped: true},
		{name: "EMA without issuer link", clientType: authClientTypeEMA, enforced: true, wantCard: true, wantTyped: true},
		{name: "personal with issuer link", clientType: authClientTypeRemoteSession, enforced: true, issuerLinked: true, wantCard: true, wantTyped: true},
		{name: "shared excluded", clientType: authClientTypeServiceAccount, enforced: true, issuerLinked: true},
		{name: "static excluded", clientType: authClientTypeStaticCredential, enforced: true, issuerLinked: true},
		{name: "disabled excluded", clientType: authClientTypeRemoteSession, state: "disabled", enforced: true, issuerLinked: true},
		{name: "deleted excluded", clientType: authClientTypeRemoteSession, state: "deleted", enforced: true, issuerLinked: true},
		{name: "unlinked excluded", clientType: authClientTypeRemoteSession, state: "unlinked", enforced: true, issuerLinked: true},
		{name: "off preserves legacy", issuerLinked: true, wantCard: true},
		{name: "shadow preserves legacy", shadow: true, issuerLinked: true, wantCard: true},
		{name: "hosted preserves legacy", hosted: true, enforced: true, issuerLinked: true, wantCard: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			f := newAuthClientFixture(t, pool)
			ctx := t.Context()
			flags := new(feature.InMemory)
			flags.SetFlag(feature.FlagAuthClientResolverShadow, f.orgID, tc.shadow)
			f.service.features = flags
			f.service.authEnforcement = authenforcement.NewGate(pool)
			if tc.enforced {
				f.enforce()
			}
			policy, err := guardian.NewUnsafePolicy(testenv.NewTracerProvider(t), []string{})
			require.NoError(t, err)
			f.service.remoteChallengeMgr = remotesessions.NewChallengeManager(testenv.NewLogger(t), testenv.NewTracerProvider(t), testenv.NewMeterProvider(t), pool, f.service.enc, policy, nil, cache.NoopCache, &url.URL{Scheme: "http", Host: "localhost"})
			signIn := uuid.New()
			require.NoError(t, f.q.SeedUserSessionIssuerFixture(ctx, authclientbackfill.SeedUserSessionIssuerFixtureParams{ID: signIn, ProjectID: validUUID(f.projectID), Slug: "usi-" + signIn.String()[:8]}))
			if tc.issuerLinked {
				require.NoError(t, f.q.BindClientIssuerFixture(ctx, authclientbackfill.BindClientIssuerFixtureParams{RemoteSessionClientID: f.registration, UserSessionIssuerID: signIn}))
				f.attach()
			}
			if tc.clientType != "" {
				registration := validUUID(f.registration)
				if tc.clientType == authClientTypeStaticCredential {
					registration = uuid.NullUUID{}
				}
				id := f.adminClient(tc.clientType, registration)
				switch tc.state {
				case "disabled":
					require.NoError(t, f.q.SetAuthClientEnabledFixture(ctx, authclientbackfill.SetAuthClientEnabledFixtureParams{ID: id, Enabled: false}))
				case "deleted":
					require.NoError(t, f.q.SoftDeleteAuthClientFixture(ctx, id))
				case "unlinked":
					require.NoError(t, f.q.SoftDeleteServerAuthClientLinksFixture(ctx, f.serverID))
				}
			}
			endpoint := &ResolvedMcpEndpoint{OrganizationID: f.orgID, ProjectID: f.projectID, UserSessionIssuerID: signIn, McpServerID: validUUID(f.serverID)}
			if tc.hosted {
				endpoint.ToolsetID = validUUID(uuid.New())
			}
			state := AuthnChallengeState{}
			if tc.reconnect != "" {
				id := uuid.New()
				if tc.reconnect == "bound" {
					id = f.registration
				}
				state.WorkloadReconnect = &WorkloadReconnectBinding{ClientID: id}
			}
			clients, typed, err := f.service.listEndpointClientsAndRegistrations(ctx, endpoint, state)
			require.NoError(t, err)
			if tc.wantCard {
				require.Len(t, clients, 1)
				require.Equal(t, f.registration, clients[0].ID)
			} else {
				require.Empty(t, clients)
			}
			if tc.wantTyped {
				require.Equal(t, map[uuid.UUID]uuid.UUID{f.registration: f.projectID}, typed)
			} else {
				require.Empty(t, typed)
			}
		})
	}
}

func TestDirectConsentConnectionCardsModeErrorFailsClosed(t *testing.T) {
	t.Parallel()
	pool, err := TestInfra.CloneTestDatabase(t, "mcp_direct_consent_mode_error")
	require.NoError(t, err)
	f := newAuthClientFixture(t, pool)
	f.service.authEnforcement = authenforcement.NewGate(pool)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	// No challenge manager: mode lookup must fail before reading legacy clients.
	clients, typed, err := f.service.listEndpointClientsAndRegistrations(ctx, &ResolvedMcpEndpoint{OrganizationID: f.orgID, ProjectID: f.projectID, McpServerID: validUUID(f.serverID)}, AuthnChallengeState{})
	require.ErrorContains(t, err, "read upstream auth enforcement")
	require.Empty(t, clients)
	require.Empty(t, typed)
}
