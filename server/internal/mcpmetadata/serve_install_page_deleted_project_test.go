package mcpmetadata_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	projects_repo "github.com/speakeasy-api/gram/server/internal/projects/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
	toolsets_repo "github.com/speakeasy-api/gram/server/internal/toolsets/repo"
)

// The legacy slug install page treats a toolset in a deleted project as unknown.
func TestServeInstallPage_LegacyToolsetInDeletedProjectReturnsNotFound(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestMCPMetadataService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	slug := "deleted-proj-install-" + uuid.NewString()[:8]
	toolset, err := ti.toolsetRepo.CreateToolset(ctx, toolsets_repo.CreateToolsetParams{
		OrganizationID: authCtx.ActiveOrganizationID,
		ProjectID:      *authCtx.ProjectID,
		Name:           "Deleted Project Install Fixture",
		Slug:           slug,
		McpSlug:        conv.ToPGText(slug),
		McpEnabled:     true,
	})
	require.NoError(t, err)
	require.NoError(t, testrepo.New(ti.conn).SetToolsetMCPPublicByID(ctx, testrepo.SetToolsetMCPPublicByIDParams{
		ID: toolset.ID, ProjectID: toolset.ProjectID, McpIsPublic: true,
	}))

	serve := func() int {
		req := httptest.NewRequest(http.MethodGet, "/mcp/"+slug+"/install", nil)
		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("mcpSlug", slug)
		req = req.WithContext(context.WithValue(context.Background(), chi.RouteCtxKey, rctx))
		rr := httptest.NewRecorder()
		require.NoError(t, ti.service.ServeInstallPage(rr, req))
		return rr.Code
	}
	require.Equal(t, http.StatusOK, serve())

	_, err = projects_repo.New(ti.conn).DeleteProject(ctx, *authCtx.ProjectID)
	require.NoError(t, err)
	require.Equal(t, http.StatusNotFound, serve())
}
