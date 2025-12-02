package releases_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/releases"
	genToolsets "github.com/speakeasy-api/gram/server/gen/toolsets"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
)

func TestReleasesService_CreateRelease_Success(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestReleasesService(t)

	// Create a toolset first
	dep := createPetstoreDeployment(t, ctx, ti)
	repo := testrepo.New(ti.conn)
	tools, err := repo.ListDeploymentHTTPTools(ctx, uuid.MustParse(dep.Deployment.ID))
	require.NoError(t, err)

	toolUrns := make([]string, len(tools))
	for i, tool := range tools {
		toolUrns[i] = tool.ToolUrn.String()
	}

	toolset, err := ti.toolsetsService.CreateToolset(ctx, &genToolsets.CreateToolsetPayload{
		Name:     "Release Test Toolset",
		ToolUrns: toolUrns[:1],
	})
	require.NoError(t, err)

	// Create a release
	release, err := ti.service.CreateRelease(ctx, &gen.CreateReleasePayload{
		ToolsetSlug: toolset.Slug,
		Notes:       conv.Ptr("Initial release"),
	})
	require.NoError(t, err)
	require.NotNil(t, release)
	require.Equal(t, toolset.ID, release.ToolsetID)
	require.Equal(t, int64(1), release.ReleaseNumber)
	require.NotNil(t, release.Notes)
	require.Equal(t, "Initial release", *release.Notes)
}

func TestReleasesService_ListReleases_Success(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestReleasesService(t)

	// Create a toolset and multiple releases
	dep := createPetstoreDeployment(t, ctx, ti)
	repo := testrepo.New(ti.conn)
	tools, err := repo.ListDeploymentHTTPTools(ctx, uuid.MustParse(dep.Deployment.ID))
	require.NoError(t, err)

	toolUrns := make([]string, len(tools))
	for i, tool := range tools {
		toolUrns[i] = tool.ToolUrn.String()
	}

	toolset, err := ti.toolsetsService.CreateToolset(ctx, &genToolsets.CreateToolsetPayload{
		Name:     "Multi Release Toolset",
		ToolUrns: toolUrns[:1],
	})
	require.NoError(t, err)

	// Create 3 releases
	for i := 1; i <= 3; i++ {
		_, err := ti.service.CreateRelease(ctx, &gen.CreateReleasePayload{
			ToolsetSlug: toolset.Slug,
			Notes:       conv.Ptr("Release " + string(rune('0'+i))),
		})
		require.NoError(t, err)
	}

	// List releases
	result, err := ti.service.ListReleases(ctx, &gen.ListReleasesPayload{
		ToolsetSlug: toolset.Slug,
		Limit:       conv.Ptr(int32(10)),
		Offset:      conv.Ptr(int32(0)),
	})
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Len(t, result.Releases, 3)
	require.Equal(t, int64(3), result.Total)

	// Verify release numbers are in descending order (newest first)
	require.Equal(t, int64(3), result.Releases[0].ReleaseNumber)
	require.Equal(t, int64(2), result.Releases[1].ReleaseNumber)
	require.Equal(t, int64(1), result.Releases[2].ReleaseNumber)
}

func TestReleasesService_ListReleases_Pagination(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestReleasesService(t)

	// Create a toolset and multiple releases
	dep := createPetstoreDeployment(t, ctx, ti)
	repo := testrepo.New(ti.conn)
	tools, err := repo.ListDeploymentHTTPTools(ctx, uuid.MustParse(dep.Deployment.ID))
	require.NoError(t, err)

	toolUrns := make([]string, len(tools))
	for i, tool := range tools {
		toolUrns[i] = tool.ToolUrn.String()
	}

	toolset, err := ti.toolsetsService.CreateToolset(ctx, &genToolsets.CreateToolsetPayload{
		Name:     "Pagination Test Toolset",
		ToolUrns: toolUrns[:1],
	})
	require.NoError(t, err)

	// Create 5 releases
	for i := 1; i <= 5; i++ {
		_, err := ti.service.CreateRelease(ctx, &gen.CreateReleasePayload{
			ToolsetSlug: toolset.Slug,
		})
		require.NoError(t, err)
	}

	// Get first page (2 items)
	page1, err := ti.service.ListReleases(ctx, &gen.ListReleasesPayload{
		ToolsetSlug: toolset.Slug,
		Limit:       conv.Ptr(int32(2)),
		Offset:      conv.Ptr(int32(0)),
	})
	require.NoError(t, err)
	require.Len(t, page1.Releases, 2)
	require.Equal(t, int64(5), page1.Total)

	// Get second page (2 items)
	page2, err := ti.service.ListReleases(ctx, &gen.ListReleasesPayload{
		ToolsetSlug: toolset.Slug,
		Limit:       conv.Ptr(int32(2)),
		Offset:      conv.Ptr(int32(2)),
	})
	require.NoError(t, err)
	require.Len(t, page2.Releases, 2)
	require.Equal(t, int64(5), page2.Total)

	// Verify no overlap between pages
	require.NotEqual(t, page1.Releases[0].ID, page2.Releases[0].ID)
	require.NotEqual(t, page1.Releases[1].ID, page2.Releases[1].ID)
}

func TestReleasesService_GetRelease_Success(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestReleasesService(t)

	// Create a toolset and release
	dep := createPetstoreDeployment(t, ctx, ti)
	repo := testrepo.New(ti.conn)
	tools, err := repo.ListDeploymentHTTPTools(ctx, uuid.MustParse(dep.Deployment.ID))
	require.NoError(t, err)

	toolUrns := make([]string, len(tools))
	for i, tool := range tools {
		toolUrns[i] = tool.ToolUrn.String()
	}

	toolset, err := ti.toolsetsService.CreateToolset(ctx, &genToolsets.CreateToolsetPayload{
		Name:     "Get Release Toolset",
		ToolUrns: toolUrns[:1],
	})
	require.NoError(t, err)

	created, err := ti.service.CreateRelease(ctx, &gen.CreateReleasePayload{
		ToolsetSlug: toolset.Slug,
		Notes:       conv.Ptr("Test release"),
	})
	require.NoError(t, err)

	// Get release by ID
	retrieved, err := ti.service.GetRelease(ctx, &gen.GetReleasePayload{
		ReleaseID: created.ID,
	})
	require.NoError(t, err)
	require.NotNil(t, retrieved)
	require.Equal(t, created.ID, retrieved.ID)
	require.Equal(t, created.ReleaseNumber, retrieved.ReleaseNumber)
}

func TestReleasesService_GetRelease_NotFound(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestReleasesService(t)

	// Try to get non-existent release
	_, err := ti.service.GetRelease(ctx, &gen.GetReleasePayload{
		ReleaseID: uuid.New().String(),
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "release not found")
}

func TestReleasesService_GetReleaseByNumber_Success(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestReleasesService(t)

	// Create a toolset and releases
	dep := createPetstoreDeployment(t, ctx, ti)
	repo := testrepo.New(ti.conn)
	tools, err := repo.ListDeploymentHTTPTools(ctx, uuid.MustParse(dep.Deployment.ID))
	require.NoError(t, err)

	toolUrns := make([]string, len(tools))
	for i, tool := range tools {
		toolUrns[i] = tool.ToolUrn.String()
	}

	toolset, err := ti.toolsetsService.CreateToolset(ctx, &genToolsets.CreateToolsetPayload{
		Name:     "Get By Number Toolset",
		ToolUrns: toolUrns[:1],
	})
	require.NoError(t, err)

	// Create multiple releases
	for i := 1; i <= 3; i++ {
		_, err := ti.service.CreateRelease(ctx, &gen.CreateReleasePayload{
			ToolsetSlug: toolset.Slug,
		})
		require.NoError(t, err)
	}

	// Get release by number
	release, err := ti.service.GetReleaseByNumber(ctx, &gen.GetReleaseByNumberPayload{
		ToolsetSlug:   toolset.Slug,
		ReleaseNumber: 2,
	})
	require.NoError(t, err)
	require.NotNil(t, release)
	require.Equal(t, int64(2), release.ReleaseNumber)
}

func TestReleasesService_GetReleaseByNumber_NotFound(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestReleasesService(t)

	// Create a toolset
	dep := createPetstoreDeployment(t, ctx, ti)
	repo := testrepo.New(ti.conn)
	tools, err := repo.ListDeploymentHTTPTools(ctx, uuid.MustParse(dep.Deployment.ID))
	require.NoError(t, err)

	toolUrns := make([]string, len(tools))
	for i, tool := range tools {
		toolUrns[i] = tool.ToolUrn.String()
	}

	toolset, err := ti.toolsetsService.CreateToolset(ctx, &genToolsets.CreateToolsetPayload{
		Name:     "Empty Releases Toolset",
		ToolUrns: toolUrns[:1],
	})
	require.NoError(t, err)

	// Try to get non-existent release number
	_, err = ti.service.GetReleaseByNumber(ctx, &gen.GetReleaseByNumberPayload{
		ToolsetSlug:   toolset.Slug,
		ReleaseNumber: 999,
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "release not found")
}

func TestReleasesService_GetLatestRelease_Success(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestReleasesService(t)

	// Create a toolset and releases
	dep := createPetstoreDeployment(t, ctx, ti)
	repo := testrepo.New(ti.conn)
	tools, err := repo.ListDeploymentHTTPTools(ctx, uuid.MustParse(dep.Deployment.ID))
	require.NoError(t, err)

	toolUrns := make([]string, len(tools))
	for i, tool := range tools {
		toolUrns[i] = tool.ToolUrn.String()
	}

	toolset, err := ti.toolsetsService.CreateToolset(ctx, &genToolsets.CreateToolsetPayload{
		Name:     "Latest Release Toolset",
		ToolUrns: toolUrns[:1],
	})
	require.NoError(t, err)

	// Create multiple releases
	for i := 1; i <= 3; i++ {
		_, err := ti.service.CreateRelease(ctx, &gen.CreateReleasePayload{
			ToolsetSlug: toolset.Slug,
		})
		require.NoError(t, err)
	}

	// Get latest release
	latest, err := ti.service.GetLatestRelease(ctx, &gen.GetLatestReleasePayload{
		ToolsetSlug: toolset.Slug,
	})
	require.NoError(t, err)
	require.NotNil(t, latest)
	require.Equal(t, int64(3), latest.ReleaseNumber)
}

func TestReleasesService_GetLatestRelease_NotFound(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestReleasesService(t)

	// Create a toolset with no releases
	dep := createPetstoreDeployment(t, ctx, ti)
	repo := testrepo.New(ti.conn)
	tools, err := repo.ListDeploymentHTTPTools(ctx, uuid.MustParse(dep.Deployment.ID))
	require.NoError(t, err)

	toolUrns := make([]string, len(tools))
	for i, tool := range tools {
		toolUrns[i] = tool.ToolUrn.String()
	}

	toolset, err := ti.toolsetsService.CreateToolset(ctx, &genToolsets.CreateToolsetPayload{
		Name:     "No Releases Toolset",
		ToolUrns: toolUrns[:1],
	})
	require.NoError(t, err)

	// Try to get latest release when none exist
	_, err = ti.service.GetLatestRelease(ctx, &gen.GetLatestReleasePayload{
		ToolsetSlug: toolset.Slug,
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "no releases found")
}

func TestReleasesService_RollbackToRelease_Success(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestReleasesService(t)

	// Create a toolset and releases
	dep := createPetstoreDeployment(t, ctx, ti)
	repo := testrepo.New(ti.conn)
	tools, err := repo.ListDeploymentHTTPTools(ctx, uuid.MustParse(dep.Deployment.ID))
	require.NoError(t, err)

	toolUrns := make([]string, len(tools))
	for i, tool := range tools {
		toolUrns[i] = tool.ToolUrn.String()
	}

	toolset, err := ti.toolsetsService.CreateToolset(ctx, &genToolsets.CreateToolsetPayload{
		Name:     "Rollback Test Toolset",
		ToolUrns: toolUrns[:1],
	})
	require.NoError(t, err)

	// Create multiple releases
	for i := 1; i <= 3; i++ {
		_, err := ti.service.CreateRelease(ctx, &gen.CreateReleasePayload{
			ToolsetSlug: toolset.Slug,
		})
		require.NoError(t, err)
	}

	// Rollback to release 2
	result, err := ti.service.RollbackToRelease(ctx, &gen.RollbackToReleasePayload{
		ToolsetSlug:   toolset.Slug,
		ReleaseNumber: 2,
	})
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Equal(t, toolset.ID, result.ID)
}

func TestReleasesService_RollbackToRelease_NotFound(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestReleasesService(t)

	// Create a toolset
	dep := createPetstoreDeployment(t, ctx, ti)
	repo := testrepo.New(ti.conn)
	tools, err := repo.ListDeploymentHTTPTools(ctx, uuid.MustParse(dep.Deployment.ID))
	require.NoError(t, err)

	toolUrns := make([]string, len(tools))
	for i, tool := range tools {
		toolUrns[i] = tool.ToolUrn.String()
	}

	toolset, err := ti.toolsetsService.CreateToolset(ctx, &genToolsets.CreateToolsetPayload{
		Name:     "Rollback Not Found Toolset",
		ToolUrns: toolUrns[:1],
	})
	require.NoError(t, err)

	// Try to rollback to non-existent release
	_, err = ti.service.RollbackToRelease(ctx, &gen.RollbackToReleasePayload{
		ToolsetSlug:   toolset.Slug,
		ReleaseNumber: 999,
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "release not found")
}
