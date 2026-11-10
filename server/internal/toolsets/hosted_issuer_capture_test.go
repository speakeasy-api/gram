package toolsets_test

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/mcpservers"
	upstreamclientsrepo "github.com/speakeasy-api/gram/server/internal/mcpservers/upstreamclients/repo"
	"github.com/speakeasy-api/gram/server/internal/productfeatures"
	productfeaturesrepo "github.com/speakeasy-api/gram/server/internal/productfeatures/repo"
	remotesessionsrepo "github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
)

// Moving a hosted server onto a user session issuer configures its sign-in
// there. Under upstream auth enforcement a server that is the issuer's only
// consumer is associated with the clients bound to it, and one that shares the
// issuer is associated with none.
func TestHostedServerIssuerMoveAssociatesSoleConsumerClientsUnderEnforcement(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		shared bool
	}{
		{name: "sole consumer", shared: false},
		{name: "shared issuer", shared: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			ctx, ti := newTestToolsetsService(t)
			authCtx, ok := contextvalues.GetAuthContext(ctx)
			require.True(t, ok)
			projectID := *authCtx.ProjectID
			_, err := productfeaturesrepo.New(ti.conn).EnableFeature(ctx, productfeaturesrepo.EnableFeatureParams{
				OrganizationID: authCtx.ActiveOrganizationID,
				FeatureName:    string(productfeatures.FeatureUpstreamAuthEnforced),
			})
			require.NoError(t, err)

			issuer := uuid.MustParse(createShimIssuer(t, ctx, ti, "hosted-capture-"+uuid.NewString()[:8]))
			setIssuer := func(name string) uuid.UUID {
				toolset := createMinimalPublicToolset(t, ctx, ti, name)
				_, err := ti.service.UpdateHostedServer(ctx, hostedUpdate(toolset, "public", func(u *mcpservers.HostedServerUpdate) {
					u.UserSessionIssuerID = uuid.NullUUID{UUID: issuer, Valid: true}
				}))
				require.NoError(t, err)
				return uuid.MustParse(toolset.ID)
			}
			if tc.shared {
				setIssuer("Hosted already there")
			}

			remote := remotesessionsrepo.New(ti.conn)
			slug := "hosted-capture-" + uuid.NewString()[:8]
			remoteIssuer, err := remote.CreateRemoteSessionIssuer(ctx, remotesessionsrepo.CreateRemoteSessionIssuerParams{
				ProjectID:                         conv.ToNullUUID(projectID),
				OrganizationID:                    conv.ToPGText(authCtx.ActiveOrganizationID),
				Slug:                              slug,
				Issuer:                            "https://" + slug + ".example.com",
				AuthorizationEndpoint:             conv.ToPGText("https://" + slug + ".example.com/authorize"),
				TokenEndpoint:                     conv.ToPGText("https://" + slug + ".example.com/token"),
				ScopesSupported:                   []string{"openid"},
				GrantTypesSupported:               []string{"authorization_code", "refresh_token"},
				ResponseTypesSupported:            []string{"code"},
				TokenEndpointAuthMethodsSupported: []string{"none"},
				CodeChallengeMethodsSupported:     []string{"S256"},
			})
			require.NoError(t, err)
			client, err := remote.CreateRemoteSessionClient(ctx, remotesessionsrepo.CreateRemoteSessionClientParams{
				ProjectID:             conv.ToNullUUID(projectID),
				OrganizationID:        conv.ToPGText(authCtx.ActiveOrganizationID),
				RemoteSessionIssuerID: remoteIssuer.ID,
				ClientID:              slug + "-client",
				ClientIDIssuedAt:      pgtype.Timestamptz{Time: time.Now(), Valid: true},
			})
			require.NoError(t, err)
			require.NoError(t, remote.AttachRemoteSessionClientToUserSessionIssuer(ctx, remotesessionsrepo.AttachRemoteSessionClientToUserSessionIssuerParams{
				RemoteSessionClientID: client.ID,
				UserSessionIssuerID:   issuer,
			}))

			moved := setIssuer("Hosted moved")

			associations, err := upstreamclientsrepo.New(ti.conn).ListMCPServerRemoteSessionClients(ctx, upstreamclientsrepo.ListMCPServerRemoteSessionClientsParams{McpServerID: moved, ProjectID: projectID})
			require.NoError(t, err)
			if tc.shared {
				require.Empty(t, associations, "an issuer another server uses configures nothing for the moved server")
				return
			}
			require.Len(t, associations, 1)
			require.Equal(t, client.ID, associations[0].RemoteSessionClientID)
		})
	}
}
