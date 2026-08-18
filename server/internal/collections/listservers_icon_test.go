package collections_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/collections"
	assetsRepo "github.com/speakeasy-api/gram/server/internal/assets/repo"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	mcpmetarepo "github.com/speakeasy-api/gram/server/internal/mcpmetadata/repo"
	"github.com/speakeasy-api/gram/server/internal/mcpservers"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

// createLogoAsset inserts an image asset row and returns its id.
func createLogoAsset(t *testing.T, ctx context.Context, ti *testInstance) uuid.UUID {
	t.Helper()

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	asset, err := assetsRepo.New(ti.conn).CreateAsset(ctx, assetsRepo.CreateAssetParams{
		Name:           "logo-" + uuid.New().String()[:8] + ".png",
		Url:            "https://example.com/logo.png",
		ProjectID:      *authCtx.ProjectID,
		OrganizationID: authCtx.ActiveOrganizationID,
		Sha256:         uuid.New().String(),
		Kind:           "image",
		ContentType:    "image/png",
		ContentLength:  1024,
	})
	require.NoError(t, err)
	return asset.ID
}

func expectedIconURL(t *testing.T, logoID uuid.UUID) string {
	t.Helper()

	u := *testenv.DefaultSiteURL(t)
	u.Path = "/rpc/assets.serveImage"
	q := u.Query()
	q.Set("id", logoID.String())
	u.RawQuery = q.Encode()
	return u.String()
}

func TestCollectionsService_ListServers_ToolsetLogoIconURL(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestCollectionsService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	toolset := createMCPEnabledToolset(t, ctx, ti, "Logo Toolset", "")
	toolsetID, err := uuid.Parse(toolset.ID)
	require.NoError(t, err)

	logoID := createLogoAsset(t, ctx, ti)
	_, err = mcpmetarepo.New(ti.conn).UpsertMetadata(ctx, mcpmetarepo.UpsertMetadataParams{
		ToolsetID:                 uuid.NullUUID{UUID: toolsetID, Valid: true},
		ProjectID:                 *authCtx.ProjectID,
		ExternalDocumentationUrl:  pgtype.Text{},
		ExternalDocumentationText: pgtype.Text{},
		LogoID:                    uuid.NullUUID{UUID: logoID, Valid: true},
		Instructions:              pgtype.Text{},
		DefaultEnvironmentID:      uuid.NullUUID{},
		InstallationOverrideUrl:   pgtype.Text{},
	})
	require.NoError(t, err)

	collection := createCollection(t, ctx, ti, "Registry", "registry", "com.speakeasy.registry")
	_, err = ti.service.AttachServer(ctx, &gen.AttachServerPayload{
		CollectionID: collection.ID,
		ToolsetID:    &toolset.ID,
		McpServerID:  nil,
		SessionToken: nil,
		ApikeyToken:  nil,
	})
	require.NoError(t, err)

	listed, err := ti.service.ListServers(ctx, &gen.ListServersPayload{
		CollectionSlug: collection.Slug,
		SessionToken:   nil,
		ApikeyToken:    nil,
	})
	require.NoError(t, err)
	require.Len(t, listed.Servers, 1)
	require.NotNil(t, listed.Servers[0].IconURL)
	require.Equal(t, expectedIconURL(t, logoID), *listed.Servers[0].IconURL)
}

func TestCollectionsService_ListServers_McpServerLogoIconURL(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestCollectionsService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	server := createMCPServerWithEndpoint(t, ctx, ti, "Logo Server", "logo-server", mcpservers.VisibilityPrivate, uuid.NullUUID{})

	logoID := createLogoAsset(t, ctx, ti)
	_, err := mcpmetarepo.New(ti.conn).UpsertMetadataByMcpServerID(ctx, mcpmetarepo.UpsertMetadataByMcpServerIDParams{
		McpServerID:               uuid.NullUUID{UUID: server.id, Valid: true},
		ProjectID:                 *authCtx.ProjectID,
		ExternalDocumentationUrl:  pgtype.Text{},
		ExternalDocumentationText: pgtype.Text{},
		LogoID:                    uuid.NullUUID{UUID: logoID, Valid: true},
		Instructions:              pgtype.Text{},
		DefaultEnvironmentID:      uuid.NullUUID{},
		InstallationOverrideUrl:   pgtype.Text{},
	})
	require.NoError(t, err)

	collection := createCollection(t, ctx, ti, "Registry", "registry", "com.speakeasy.registry")
	_, err = ti.service.AttachServer(ctx, &gen.AttachServerPayload{
		CollectionID: collection.ID,
		ToolsetID:    nil,
		McpServerID:  &server.idStr,
		SessionToken: nil,
		ApikeyToken:  nil,
	})
	require.NoError(t, err)

	listed, err := ti.service.ListServers(ctx, &gen.ListServersPayload{
		CollectionSlug: collection.Slug,
		SessionToken:   nil,
		ApikeyToken:    nil,
	})
	require.NoError(t, err)
	require.Len(t, listed.Servers, 1)
	require.NotNil(t, listed.Servers[0].IconURL)
	require.Equal(t, expectedIconURL(t, logoID), *listed.Servers[0].IconURL)
}

func TestCollectionsService_ListServers_NoLogoNoIconURL(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestCollectionsService(t)

	toolset := createMCPEnabledToolset(t, ctx, ti, "Plain Toolset", "")
	collection := createCollection(t, ctx, ti, "Registry", "registry", "com.speakeasy.registry")
	_, err := ti.service.AttachServer(ctx, &gen.AttachServerPayload{
		CollectionID: collection.ID,
		ToolsetID:    &toolset.ID,
		McpServerID:  nil,
		SessionToken: nil,
		ApikeyToken:  nil,
	})
	require.NoError(t, err)

	listed, err := ti.service.ListServers(ctx, &gen.ListServersPayload{
		CollectionSlug: collection.Slug,
		SessionToken:   nil,
		ApikeyToken:    nil,
	})
	require.NoError(t, err)
	require.Len(t, listed.Servers, 1)
	require.Nil(t, listed.Servers[0].IconURL)
}
