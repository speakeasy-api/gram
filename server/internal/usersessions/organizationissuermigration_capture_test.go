package usersessions_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	orggen "github.com/speakeasy-api/gram/server/gen/organization_user_session_issuers"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/mcpservers"
	mcpserversrepo "github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
	upstreamclientsrepo "github.com/speakeasy-api/gram/server/internal/mcpservers/upstreamclients/repo"
	"github.com/speakeasy-api/gram/server/internal/productfeatures"
	productfeaturesrepo "github.com/speakeasy-api/gram/server/internal/productfeatures/repo"
	remotesessionsrepo "github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
	toolsetsrepo "github.com/speakeasy-api/gram/server/internal/toolsets/repo"
)

// Migrating an issuer moves its servers onto the target, which configures
// their sign-in there. Under upstream auth enforcement a moved server that is
// the target's only consumer is associated with the clients bound to it, and
// one that shares the target is associated with none.
func TestOrganizationUserSessionIssuerMigrateAssociatesSoleConsumerClientsUnderEnforcement(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		shared bool
	}{
		{name: "sole consumer", shared: false},
		{name: "shared target", shared: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			ctx, ti := newTestService(t)
			authCtx, ok := contextvalues.GetAuthContext(ctx)
			require.True(t, ok)
			require.NotNil(t, authCtx.ProjectID)
			projectID := *authCtx.ProjectID
			_, err := productfeaturesrepo.New(ti.conn).EnableFeature(ctx, productfeaturesrepo.EnableFeatureParams{
				OrganizationID: authCtx.ActiveOrganizationID,
				FeatureName:    string(productfeatures.FeatureUpstreamAuthEnforced),
			})
			require.NoError(t, err)

			sourceID := seedIssuer(t, ctx, ti, "capture-migrate-source")
			targetID := seedIssuer(t, ctx, ti, "capture-migrate-target")
			moved := seedHostedServerOnIssuer(t, ctx, ti, projectID, authCtx.ActiveOrganizationID, sourceID, "capture-migrate-moved")
			if tc.shared {
				seedHostedServerOnIssuer(t, ctx, ti, projectID, authCtx.ActiveOrganizationID, targetID, "capture-migrate-there")
			}

			remote := remotesessionsrepo.New(ti.conn)
			remoteIssuer, err := remote.CreateRemoteSessionIssuer(ctx, remotesessionsrepo.CreateRemoteSessionIssuerParams{
				ProjectID:                         conv.ToNullUUID(projectID),
				OrganizationID:                    conv.ToPGText(authCtx.ActiveOrganizationID),
				Slug:                              "capture-migrate-provider",
				Issuer:                            "https://capture-migrate.example.com",
				AuthorizationEndpoint:             conv.ToPGText("https://capture-migrate.example.com/authorize"),
				TokenEndpoint:                     conv.ToPGText("https://capture-migrate.example.com/token"),
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
				ClientID:              "capture-migrate-client",
				ClientIDIssuedAt:      pgtype.Timestamptz{Time: time.Now(), Valid: true},
			})
			require.NoError(t, err)
			require.NoError(t, remote.AttachRemoteSessionClientToUserSessionIssuer(ctx, remotesessionsrepo.AttachRemoteSessionClientToUserSessionIssuerParams{
				RemoteSessionClientID: client.ID,
				UserSessionIssuerID:   targetID,
			}))

			_, err = ti.service.MigrateIssuer(ctx, &orggen.MigrateIssuerPayload{SourceID: sourceID.String(), TargetID: targetID.String()})
			require.NoError(t, err)

			associations, err := upstreamclientsrepo.New(ti.conn).ListMCPServerRemoteSessionClients(ctx, upstreamclientsrepo.ListMCPServerRemoteSessionClientsParams{McpServerID: moved, ProjectID: projectID})
			require.NoError(t, err)
			if tc.shared {
				require.Empty(t, associations, "a target another server uses configures nothing for the moved server")
				return
			}
			require.Len(t, associations, 1)
			require.Equal(t, client.ID, associations[0].RemoteSessionClientID)
		})
	}
}

func seedHostedServerOnIssuer(t *testing.T, ctx context.Context, ti *testInstance, projectID uuid.UUID, organizationID string, issuerID uuid.UUID, slug string) uuid.UUID {
	t.Helper()

	toolset, err := toolsetsrepo.New(ti.conn).CreateToolset(ctx, toolsetsrepo.CreateToolsetParams{
		OrganizationID: organizationID, ProjectID: projectID,
		Name: slug, Slug: slug, Description: pgtype.Text{}, DefaultEnvironmentSlug: pgtype.Text{},
	})
	require.NoError(t, err)
	serverID := uuid.New()
	_, err = mcpserversrepo.New(ti.conn).CreateMCPServer(ctx, mcpserversrepo.CreateMCPServerParams{
		ID: serverID, ProjectID: projectID,
		Name: pgtype.Text{String: slug, Valid: true}, Slug: pgtype.Text{String: slug, Valid: true},
		EnvironmentID: uuid.NullUUID{}, UserSessionIssuerID: conv.ToNullUUID(issuerID), RemoteMcpServerID: uuid.NullUUID{},
		ToolsetID: conv.ToNullUUID(toolset.ID), ToolVariationsGroupID: uuid.NullUUID{}, Visibility: mcpservers.VisibilityPrivate,
	})
	require.NoError(t, err)
	return serverID
}
