package toolsets_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/toolsets"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oops"
	projectsrepo "github.com/speakeasy-api/gram/server/internal/projects/repo"
	variationsrepo "github.com/speakeasy-api/gram/server/internal/variations/repo"
)

// seedToolVariationsGroup creates the project-default tool variations group for
// the given project directly through the generated repo.
func seedToolVariationsGroup(t *testing.T, ctx context.Context, conn *pgxpool.Pool, projectID uuid.UUID) uuid.UUID {
	t.Helper()

	groupID, err := variationsrepo.New(conn).InitGlobalToolVariationsGroup(ctx, variationsrepo.InitGlobalToolVariationsGroupParams{
		ProjectID:   projectID,
		Name:        "Global tool variations",
		Description: pgtype.Text{String: "", Valid: false},
	})
	require.NoError(t, err)

	return groupID
}

// seedOtherProjectToolVariationsGroup creates an additional project in the
// caller's organization and seeds a tool variations group under that *other*
// project. Used to exercise cross-project rejection.
func seedOtherProjectToolVariationsGroup(t *testing.T, ctx context.Context, conn *pgxpool.Pool, organizationID string) uuid.UUID {
	t.Helper()

	slug := "other-" + uuid.New().String()[:8]
	otherProject, err := projectsrepo.New(conn).CreateProject(ctx, projectsrepo.CreateProjectParams{
		Name:           slug,
		Slug:           slug,
		OrganizationID: organizationID,
	})
	require.NoError(t, err)

	return seedToolVariationsGroup(t, ctx, conn, otherProject.ID)
}

func TestSetToolVariationsGroup_AssignsAndClears(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestToolsetsService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	toolset := createMinimalPrivateToolset(t, ctx, ti, "variations toolset")
	groupID := seedToolVariationsGroup(t, ctx, ti.conn, *authCtx.ProjectID).String()

	assigned, err := ti.service.SetToolVariationsGroup(ctx, &gen.SetToolVariationsGroupPayload{
		SessionToken:          nil,
		ApikeyToken:           nil,
		Slug:                  toolset.Slug,
		ToolVariationsGroupID: &groupID,
		ProjectSlugInput:      nil,
	})
	require.NoError(t, err)
	require.NotNil(t, assigned.ToolVariationsGroupID)
	require.Equal(t, groupID, *assigned.ToolVariationsGroupID)

	cleared, err := ti.service.SetToolVariationsGroup(ctx, &gen.SetToolVariationsGroupPayload{
		SessionToken:          nil,
		ApikeyToken:           nil,
		Slug:                  toolset.Slug,
		ToolVariationsGroupID: nil,
		ProjectSlugInput:      nil,
	})
	require.NoError(t, err)
	require.Nil(t, cleared.ToolVariationsGroupID, "passing null should disable filtering")
}

func TestSetToolVariationsGroup_RejectsCrossProjectGroup(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestToolsetsService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	toolset := createMinimalPrivateToolset(t, ctx, ti, "variations toolset")
	otherGroupID := seedOtherProjectToolVariationsGroup(t, ctx, ti.conn, authCtx.ActiveOrganizationID).String()

	_, err := ti.service.SetToolVariationsGroup(ctx, &gen.SetToolVariationsGroupPayload{
		SessionToken:          nil,
		ApikeyToken:           nil,
		Slug:                  toolset.Slug,
		ToolVariationsGroupID: &otherGroupID,
		ProjectSlugInput:      nil,
	})
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeNotFound, oopsErr.Code)
}

func TestSetToolVariationsGroup_InvalidGroupID(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestToolsetsService(t)

	toolset := createMinimalPrivateToolset(t, ctx, ti, "variations toolset")
	bad := "not-a-uuid"

	_, err := ti.service.SetToolVariationsGroup(ctx, &gen.SetToolVariationsGroupPayload{
		SessionToken:          nil,
		ApikeyToken:           nil,
		Slug:                  toolset.Slug,
		ToolVariationsGroupID: &bad,
		ProjectSlugInput:      nil,
	})
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeBadRequest, oopsErr.Code)
}
