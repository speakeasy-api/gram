package mcp

import (
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/mcpservers/upstreamclients"
	upstreamclientsrepo "github.com/speakeasy-api/gram/server/internal/mcpservers/upstreamclients/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

func TestGatewayReviewUnchosenFailureIsMemberLocal(t *testing.T) {
	t.Parallel()
	pool, err := TestInfra.CloneTestDatabase(t, "mcp_review_member_isolation")
	require.NoError(t, err)
	for _, recorded := range []bool{false, true} {
		name := "unchosen"
		if recorded {
			name = "recorded_choice"
		}
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			f := newEnforcedCallerFixture(t, pool)
			healthy := f.serverID
			client := f.adminClient(authClientTypeStaticCredential, uuid.NullUUID{})
			f.copyHeader(client, "X-Api-Key", true, false, text("healthy-secret"), pgtype.Text{})
			broken, _ := f.server()
			f.serverID = broken
			badClient := f.adminClient(authClientTypeStaticCredential, uuid.NullUUID{})
			f.copyHeader(badClient, "X-Api-Key", true, true, text("not-encrypted"), pgtype.Text{})
			agentID := uuid.New()
			ctx := admittedAgentContext(t, f.orgID, agentID)
			endpoint := &ResolvedMcpEndpoint{ProjectID: f.projectID, OrganizationID: f.orgID, UserSessionIssuerID: f.signIn}
			authentication := &issuerGateAuthentication{endpoint: endpoint, subject: urn.NewAgentSubject(agentID)}
			choices, err := f.service.newMemberChoices(ctx, authentication)
			require.NoError(t, err)
			require.True(t, choices.unchosen)
			if recorded {
				n, err := upstreamclientsrepo.New(pool).RecordAuthClientChoice(ctx, upstreamclientsrepo.RecordAuthClientChoiceParams{
					CredentialKind: upstreamclients.CredentialKindAPIKey, CredentialID: choices.selector.credentialID, Source: upstreamclients.SelectionSourceAdmin,
					OrganizationID: f.orgID, AuthClientID: validUUID(client), McpServerID: healthy,
				})
				require.NoError(t, err)
				require.EqualValues(t, 1, n)
			}
			for _, ids := range [][]uuid.UUID{{broken, healthy}, {healthy, broken}} {
				members := make([]metaMember, len(ids))
				for i, id := range ids {
					members[i] = metaMember{serverID: id, projectID: f.projectID, slug: "member", backend: metaMemberBackendProxied}
				}
				choices.apply(ctx, testenv.NewLogger(t), members, false)
				for _, member := range members {
					if member.serverID == broken {
						require.Error(t, member.upstreamUnavailable)
						require.False(t, member.upstreamNoAuth)
					} else {
						require.NoError(t, member.upstreamUnavailable)
						require.Equal(t, client, member.authClient.clientID)
						require.Len(t, member.authClient.headers, 1)
						require.Equal(t, "healthy-secret", member.authClient.headers[0].StaticValue)
					}
				}
			}
		})
	}
}

func TestGatewayReviewDirectAgentHeaderMaterial(t *testing.T) {
	t.Parallel()
	pool, err := TestInfra.CloneTestDatabase(t, "mcp_review_agent_headers")
	require.NoError(t, err)
	for _, clientType := range []string{authClientTypeStaticCredential, authClientTypePassThrough} {
		t.Run(clientType, func(t *testing.T) {
			t.Parallel()
			f := newEnforcedCallerFixture(t, pool)
			client := f.adminClient(clientType, uuid.NullUUID{})
			value, source := text("configured-secret"), pgtype.Text{}
			if clientType == authClientTypePassThrough {
				value, source = pgtype.Text{}, text("X-Caller-Key")
			}
			f.copyHeader(client, "X-Api-Key", true, false, value, source)
			agentID := uuid.New()
			ctx := admittedAgentContext(t, f.orgID, agentID)
			authentication := f.authentication(urn.NewAgentSubject(agentID))
			_, selected, err := f.service.directUpstreamChoice(ctx, httptest.NewRecorder(), authentication)
			require.NoError(t, err)
			require.True(t, selected)
			require.NotNil(t, authentication.authClient)
			require.Equal(t, client, authentication.authClient.clientID)
			require.True(t, authentication.chosenNoAuth, "header-only clients need no OAuth token")
			require.Len(t, authentication.authClient.headers, 1)
			header := authentication.authClient.headers[0]
			require.Equal(t, "X-Api-Key", header.Name)
			require.Equal(t, value.String, header.StaticValue)
			require.Equal(t, source.String, header.ValueFromRequestHeader)
		})
	}
}
