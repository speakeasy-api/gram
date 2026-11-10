package mcp

import (
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/mcpservers/upstreamclients/authclientbackfill"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

func TestEnforcedUnchosenGatewayKeepsLivePersonalConnectionWithIdentityFallback(t *testing.T) {
	t.Parallel()
	pool, err := TestInfra.CloneTestDatabase(t, "mcp_unchosen_identity_fallback")
	require.NoError(t, err)

	for _, tc := range []struct {
		name       string
		grants     []authz.Grant
		connected  bool
		personal   bool
		separate   bool
		wantServed bool
	}{
		{name: "personal permission only", grants: []authz.Grant{personalAuthGrant()}, connected: true, personal: true, wantServed: true},
		{name: "personal and fallback permissions", grants: []authz.Grant{personalAuthGrant(), authz.NewGrantWithSelector(authz.ScopeMCPAuth, authz.AuthClientTypeSelector(authz.AuthClientTypeEMA, ""))}, connected: true, personal: true, wantServed: true},
		{name: "wildcard permission", grants: []authz.Grant{authz.NewGrant(authz.ScopeMCPAuth, authz.WildcardResource)}, connected: true, personal: true, wantServed: true},
		{name: "no live connection", grants: []authz.Grant{authz.NewGrant(authz.ScopeMCPAuth, authz.WildcardResource)}, personal: true},
		{name: "identity provider alone still needs a check", grants: []authz.Grant{authz.NewGrant(authz.ScopeMCPAuth, authz.WildcardResource)}, connected: true},
		{name: "independent identity registration still needs a choice", grants: []authz.Grant{authz.NewGrant(authz.ScopeMCPAuth, authz.WildcardResource)}, connected: true, personal: true, separate: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			f := newEnforcedCallerFixture(t, pool)
			subject := urn.NewUserSubject("user_1")
			var personal uuid.UUID
			if tc.personal {
				personal = f.adminClient(authClientTypeRemoteSession, validUUID(f.registration))
			}
			registration := f.registration
			if tc.separate {
				registration = uuid.New()
				require.NoError(t, f.q.SeedRemoteSessionClientFixture(t.Context(), authclientbackfill.SeedRemoteSessionClientFixtureParams{
					ID: registration, ProjectID: validUUID(f.projectID), OrganizationID: text(f.orgID), RemoteSessionIssuerID: f.issuerID,
					ClientID: "fallback-" + registration.String(), CredentialOwner: "subject", TokenEndpointAuthMethod: pgtype.Text{},
				}))
			}
			ema := f.adminClient(authClientTypeEMA, validUUID(registration))
			require.NoError(t, f.q.SeedAuthClientEMAFixture(t.Context(), authclientbackfill.SeedAuthClientEMAFixtureParams{Resource: "https://api.example.com", AuthClientID: ema}))
			if tc.connected {
				f.connect(subject, "upstream-token")
			}
			enforcedShadowService(t, f.authClientFixture)
			ctx := sessionContext(t, f.orgID, tc.grants...)
			endpoint := &ResolvedMcpEndpoint{ProjectID: f.projectID, OrganizationID: f.orgID, UserSessionIssuerID: f.signIn}
			choices := f.service.unchosenMemberChoices(endpoint, subject)
			members := []metaMember{{serverID: f.serverID, projectID: f.projectID, slug: "member"}}
			choices.apply(ctx, testenv.NewLogger(t), members, true)
			if !tc.wantServed {
				require.Error(t, members[0].upstreamUnavailable)
				return
			}
			require.NoError(t, members[0].upstreamUnavailable)
			require.Equal(t, personal, members[0].authClient.clientID, "the live personal connection is chosen, not its identity fallback")
			require.Equal(t, validUUID(f.registration), members[0].chosenClientID)
			tokens, err := choices.tokens(ctx, members[0])
			require.NoError(t, err)
			require.Equal(t, "upstream-token", tokens[f.issuerID].Token)
		})
	}
}
