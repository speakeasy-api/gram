package toolsets_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/toolsets"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
)

func TestToolsetsService_DeleteToolset_Success(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestToolsetsService(t)

	// Create a toolset first
	created, err := ti.service.CreateToolset(ctx, &gen.CreateToolsetPayload{
		SessionToken:           nil,
		Name:                   "Toolset to Delete",
		Description:            new("This toolset will be deleted"),
		ToolUrns:               []string{},
		ResourceUrns:           nil,
		DefaultEnvironmentSlug: nil,
		ProjectSlugInput:       nil,
	})
	require.NoError(t, err)

	// Delete the toolset
	err = ti.service.DeleteToolset(ctx, &gen.DeleteToolsetPayload{
		Slug:             created.Slug,
		SessionToken:     nil,
		ProjectSlugInput: nil,
	})
	require.NoError(t, err)

	// Verify it's deleted by trying to get it
	_, err = ti.service.GetToolset(ctx, &gen.GetToolsetPayload{
		Slug:             created.Slug,
		SessionToken:     nil,
		ProjectSlugInput: nil,
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "not found")
}

func TestToolsetsService_DeleteToolset_NotFound(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestToolsetsService(t)

	// Try to delete a non-existent toolset
	err := ti.service.DeleteToolset(ctx, &gen.DeleteToolsetPayload{
		Slug:             "non-existent-slug",
		SessionToken:     nil,
		ProjectSlugInput: nil,
	})
	require.NoError(t, err) // Delete operations are typically idempotent
}

func TestToolsetsService_DeleteToolset_Unauthorized(t *testing.T) {
	t.Parallel()

	_, ti := newTestToolsetsService(t)

	// Test with context that has no auth context
	ctx := t.Context()

	err := ti.service.DeleteToolset(ctx, &gen.DeleteToolsetPayload{
		Slug:             "some-slug",
		SessionToken:     nil,
		ProjectSlugInput: nil,
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "unauthorized")
}

func TestToolsetsService_DeleteToolset_NoProjectID(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestToolsetsService(t)

	// Create auth context without project ID
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	authCtx.ProjectID = nil
	ctx = contextvalues.SetAuthContext(ctx, authCtx)

	err := ti.service.DeleteToolset(ctx, &gen.DeleteToolsetPayload{
		Slug:             "some-slug",
		SessionToken:     nil,
		ProjectSlugInput: nil,
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "unauthorized")
}

func TestToolsetsService_DeleteToolset_VerifyListAfterDelete(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestToolsetsService(t)

	// Create two toolsets
	created1, err := ti.service.CreateToolset(ctx, &gen.CreateToolsetPayload{
		SessionToken:           nil,
		Name:                   "First Toolset",
		Description:            nil,
		ToolUrns:               []string{},
		ResourceUrns:           nil,
		DefaultEnvironmentSlug: nil,
		ProjectSlugInput:       nil,
	})
	require.NoError(t, err)

	created2, err := ti.service.CreateToolset(ctx, &gen.CreateToolsetPayload{
		SessionToken:           nil,
		Name:                   "Second Toolset",
		Description:            nil,
		ToolUrns:               []string{},
		ResourceUrns:           nil,
		DefaultEnvironmentSlug: nil,
		ProjectSlugInput:       nil,
	})
	require.NoError(t, err)

	// Verify both exist
	result, err := ti.service.ListToolsets(ctx, &gen.ListToolsetsPayload{
		SessionToken:     nil,
		ProjectSlugInput: nil,
	})
	require.NoError(t, err)
	require.Len(t, result.Toolsets, 2)

	// Delete one toolset
	err = ti.service.DeleteToolset(ctx, &gen.DeleteToolsetPayload{
		Slug:             created1.Slug,
		SessionToken:     nil,
		ProjectSlugInput: nil,
	})
	require.NoError(t, err)

	// Verify only one remains
	result, err = ti.service.ListToolsets(ctx, &gen.ListToolsetsPayload{
		SessionToken:     nil,
		ProjectSlugInput: nil,
	})
	require.NoError(t, err)
	require.Len(t, result.Toolsets, 1)
	require.Equal(t, created2.ID, result.Toolsets[0].ID)
}
