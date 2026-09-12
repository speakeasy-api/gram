package tunneledmcp

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/tunneled_mcp"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/authztest"
	"github.com/speakeasy-api/gram/server/internal/conv"
	remotesessionsrepo "github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
)

func TestDeleteServerRetainsRemoteSessionIssuerBinding(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	authCtx := requireAuthContext(t, ctx)
	server := seedTunneledMcpServer(t, ctx, ti.conn, *authCtx.ProjectID)

	issuer, err := remotesessionsrepo.New(ti.conn).CreateRemoteSessionIssuer(ctx, remotesessionsrepo.CreateRemoteSessionIssuerParams{
		ProjectID:                         conv.ToNullUUID(*authCtx.ProjectID),
		OrganizationID:                    conv.ToPGText(authCtx.ActiveOrganizationID),
		Slug:                              "issuer-" + uuid.NewString(),
		Issuer:                            "https://idp.example.com",
		AuthorizationEndpoint:             conv.ToPGText("https://idp.example.com/authorize"),
		TokenEndpoint:                     conv.ToPGText("https://idp.example.com/token"),
		ScopesSupported:                   []string{"openid"},
		GrantTypesSupported:               []string{"authorization_code", "refresh_token"},
		ResponseTypesSupported:            []string{"code"},
		TokenEndpointAuthMethodsSupported: []string{"client_secret_basic"},
		TunneledMcpServerID:               conv.ToNullUUID(server.ID),
	})
	require.NoError(t, err)
	require.True(t, issuer.TunneledMcpServerID.Valid)

	writeCtx := authztest.WithExactGrants(t, ctx, projectScopedMCPGrant(authz.ScopeMCPWrite, *authCtx.ProjectID))
	require.NoError(t, ti.service.DeleteServer(writeCtx, &gen.DeleteServerPayload{
		ID:               server.ID.String(),
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	}))

	issuer, err = remotesessionsrepo.New(ti.conn).GetRemoteSessionIssuerByID(ctx, remotesessionsrepo.GetRemoteSessionIssuerByIDParams{
		ID:                    issuer.ID,
		ProjectID:             conv.ToNullUUID(*authCtx.ProjectID),
		IncludeOrganizational: false,
		OrganizationID:        conv.ToPGText(authCtx.ActiveOrganizationID),
		IncludeGlobal:         false,
	})
	require.NoError(t, err)
	require.True(t, issuer.TunneledMcpServerID.Valid)
	require.Equal(t, server.ID, issuer.TunneledMcpServerID.UUID)
}
