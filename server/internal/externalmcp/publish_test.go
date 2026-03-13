package externalmcp_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/mcp_registries"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
)

func TestPublish(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	registry, err := ti.service.Publish(ctx, &gen.PublishPayload{
		Name:       "My Catalog",
		Slug:       "my-catalog",
		Visibility: "private",
		ToolsetIds: []string{},
	})
	require.NoError(t, err)
	require.NotEmpty(t, registry.ID)
	require.Equal(t, "My Catalog", registry.Name)
	require.NotNil(t, registry.Slug)
	require.Equal(t, "my-catalog", *registry.Slug)
	require.NotNil(t, registry.Visibility)
	require.Equal(t, "private", *registry.Visibility)
}

func TestPublishSetsProjectID(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	registry, err := ti.service.Publish(ctx, &gen.PublishPayload{
		Name:       "Project Catalog",
		Slug:       "project-catalog",
		Visibility: "private",
		ToolsetIds: []string{},
	})
	require.NoError(t, err)

	// Verify project_id was stored by reading back from the DB
	row, err := ti.repo.GetMCPRegistryByID(ctx, uuid.MustParse(registry.ID))
	require.NoError(t, err)
	require.True(t, row.ProjectID.Valid)
	require.Equal(t, *authCtx.ProjectID, row.ProjectID.UUID)
}

func TestPublishWithToolsets(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	registry, err := ti.service.Publish(ctx, &gen.PublishPayload{
		Name:       "Toolset Catalog",
		Slug:       "toolset-catalog",
		Visibility: "public",
		ToolsetIds: []string{},
	})
	require.NoError(t, err)

	// Verify no toolset links
	links, err := ti.repo.ListRegistryToolsetLinks(ctx, uuid.MustParse(registry.ID))
	require.NoError(t, err)
	require.Empty(t, links)
}

func TestPublishRequiresAdmin(t *testing.T) {
	t.Parallel()

	ctx, ti := newNonAdminTestService(t)

	_, err := ti.service.Publish(ctx, &gen.PublishPayload{
		Name:       "Unauthorized Catalog",
		Slug:       "unauth-catalog",
		Visibility: "private",
		ToolsetIds: []string{},
	})
	require.Error(t, err)
}

func TestPublishInvalidToolsetID(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	_, err := ti.service.Publish(ctx, &gen.PublishPayload{
		Name:       "Bad Catalog",
		Slug:       "bad-catalog",
		Visibility: "private",
		ToolsetIds: []string{"not-a-uuid"},
	})
	require.Error(t, err)
}
