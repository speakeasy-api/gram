package mcp_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	authclientsrepo "github.com/speakeasy-api/gram/server/internal/authclients/repo"
	"github.com/speakeasy-api/gram/server/internal/mcp/mcproute"
	"github.com/speakeasy-api/gram/server/internal/mcpservers/upstreamclients/authclientbackfill"
	upstreamclientsrepo "github.com/speakeasy-api/gram/server/internal/mcpservers/upstreamclients/repo"
	remotesessionsrepo "github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
)

func TestConsentGatewayValidationNeverProbesLegacyGitHubCredential(t *testing.T) {
	t.Parallel()
	for _, organization := range []bool{false, true} {
		for _, requiresApp := range []bool{false, true} {
			name := "stored"
			if organization {
				name = "organization"
			}
			if requiresApp {
				name += "-github"
			} else {
				name += "-ordinary"
			}
			t.Run(name, func(t *testing.T) {
				t.Parallel()
				prefix := "probe-" + name
				ctx, fx := seedMetaValidationFixture(t, prefix)
				projectID, organizationID := consentTestTenant(t, ctx)
				if organization {
					issuerID := createOrganizationIssuer(t, ctx, fx.ti, organizationID)
					require.NoError(t, remotesessionsrepo.New(fx.ti.conn).AttachRemoteSessionClientToUserSessionIssuer(ctx, remotesessionsrepo.AttachRemoteSessionClientToUserSessionIssuerParams{RemoteSessionClientID: fx.clientID, UserSessionIssuerID: issuerID}))
					insertQualifiedRemoteSessionToken(t, ctx, fx.ti, issuerID, fx.clientID, fx.subject, "token-"+prefix, fx.member.url)
					fx.endpoint.UserSessionIssuerID = issuerID
					fx.endpoint.RouteBase = mcproute.OrganizationGateway
					state, err := fx.ti.authnChallengeCache.Get(ctx, "authnChallenge:"+fx.stateID)
					require.NoError(t, err)
					state.Endpoint.RouteBase = mcproute.OrganizationGateway
					state.UserSessionIssuerID = issuerID
					require.NoError(t, fx.ti.authnChallengeCache.Store(ctx, state))
				}
				serverID := mcpServerBySlug(t, ctx, fx.ti, projectID, fx.server)
				_, err := upstreamclientsrepo.New(fx.ti.conn).AssociateMCPServerRemoteSessionClient(ctx, upstreamclientsrepo.AssociateMCPServerRemoteSessionClientParams{OrganizationID: organizationID, ProjectID: projectID, McpServerID: serverID, RemoteSessionClientID: fx.clientID})
				require.NoError(t, err)
				if requiresApp {
					fixtures := authclientbackfill.New(fx.ti.conn)
					clientID, err := fixtures.SeedAuthClientFixture(ctx, authclientbackfill.SeedAuthClientFixtureParams{
						OrganizationID: organizationID, ProjectID: uuid.NullUUID{UUID: projectID, Valid: true}, ClientType: "service_account", Source: "admin",
					})
					require.NoError(t, err)
					require.NoError(t, fixtures.SeedServerAuthClientLinkFixture(ctx, authclientbackfill.SeedServerAuthClientLinkFixtureParams{
						ProjectID: projectID, OrganizationID: organizationID, McpServerID: serverID, AuthClientID: clientID, Source: "admin",
					}))
					require.NoError(t, authclientsrepo.New(fx.ti.conn).SetGitHubAppCredential(ctx, authclientsrepo.SetGitHubAppCredentialParams{
						AuthClientID: clientID, OrganizationID: organizationID, InstanceUrl: "https://git.example.com", AppID: 123, InstallationID: 456,
						PrivateKeyEncrypted: "unused-test-key", UpstreamOrigin: fx.member.url,
					}))
				}
				_, err = postValidate(t, fx, fx.clientID)
				require.NoError(t, err)
				requests := fx.member.drain()
				if requiresApp {
					require.Empty(t, requests, "a legacy grant must not probe a GitHub-required member")
				} else {
					requireProbe(t, requests, "token-"+prefix, true, true)
				}
			})
		}
	}
}
