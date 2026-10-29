package mcp

import (
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/mcpservers/upstreamclients/authclientbackfill"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

func TestCatalogNoAuthRejectsCredentialBearingProposals(t *testing.T) {
	t.Parallel()
	pool, err := TestInfra.CloneTestDatabase(t, "mcp_catalog_no_auth")
	require.NoError(t, err)
	for _, tc := range []struct {
		name                                                   string
		requires                                               pgtype.Bool
		headerClient, uncovered, registration, oauth, accepted bool
	}{
		{name: "plain no auth", accepted: true},

		{name: "required header credentials", requires: pgtype.Bool{Bool: true, Valid: true}, headerClient: true},
		{name: "undecided header credentials", headerClient: true},
		{name: "uncovered header credentials", requires: pgtype.Bool{Bool: false, Valid: true}, headerClient: true, uncovered: true},
		{name: "captured registration", requires: pgtype.Bool{Bool: false, Valid: true}, headerClient: true, registration: true},
		{name: "oauth required", requires: pgtype.Bool{Bool: true, Valid: true}, oauth: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			f := newAuthClientFixture(t, pool)
			if tc.name == "required header credentials" {
				f.tunneled()
			}
			require.NoError(t, f.q.SetRequiresUpstreamAuthFixture(t.Context(), authclientbackfill.SetRequiresUpstreamAuthFixtureParams{RequiresUpstreamAuth: tc.requires, ID: f.serverID}))
			if tc.headerClient {
				f.client(authClientTypeStaticCredential, uuid.NullUUID{}, true)
			}
			if tc.name == "required header credentials" {
				resolved, err := f.service.resolveAuthClientForConsent(t.Context(), f.orgID, f.serverID, f.projectID, uuid.NullUUID{}, urn.NewUserSubject("user_1"))
				require.NoError(t, err)
				require.Equal(t, authClientLegacy, resolved.outcome)
				require.True(t, resolved.choice.noAuth(), "noAuth only means no OAuth registration; a legacy headers-only client is not an anonymous approval")
			}
			if tc.uncovered {
				f.requireSourceHeader(f.sourceHeader("X-Api-Key", false, text("fixture-key"), pgtype.Text{}))
			}
			if tc.oauth {
				f.oauthClient(authClientTypeRemoteSession)
			}
			member := connectionCatalogMember{ServerID: f.serverID, ProjectID: f.projectID, Auth: &connectionCatalogAuth{}}
			if tc.registration {
				member.Auth.RegistrationID = validUUID(f.registration)
			}
			err := f.service.validateCatalogMemberAuth(t.Context(), &ResolvedMcpEndpoint{OrganizationID: f.orgID}, urn.NewUserSubject("user_1"), member, nil, true)
			if tc.accepted {
				require.NoError(t, err)
			} else {
				require.ErrorIs(t, err, errConsentConfigurationChanged)
			}
		})
	}
}

func TestDirectConsentPickDoesNotBuildCardsForInapplicableEndpoints(t *testing.T) {
	t.Parallel()
	subject := urn.NewUserSubject("user_1")
	for _, tc := range []struct {
		name     string
		endpoint ResolvedMcpEndpoint
		state    AuthnChallengeState
	}{
		{name: "first party", endpoint: ResolvedMcpEndpoint{McpServerID: validUUID(uuid.New())}, state: AuthnChallengeState{FirstParty: true, Subject: &subject}},
		{name: "gateway", endpoint: ResolvedMcpEndpoint{MetaMcpServerID: validUUID(uuid.New())}, state: AuthnChallengeState{Subject: &subject}},
		{name: "hosted", endpoint: ResolvedMcpEndpoint{McpServerID: validUUID(uuid.New()), ToolsetID: validUUID(uuid.New())}, state: AuthnChallengeState{Subject: &subject}},
		{name: "no subject", endpoint: ResolvedMcpEndpoint{McpServerID: validUUID(uuid.New())}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			// No card-building dependencies: the caller supplies its already-built cards.
			picks, err := (&Service{}).consentDirectAuthClientPick(t.Context(), &tc.endpoint, tc.state, "", nil, nil)
			require.NoError(t, err)
			require.Nil(t, picks)
		})
	}
}
