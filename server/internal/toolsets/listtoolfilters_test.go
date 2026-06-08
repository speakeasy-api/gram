package toolsets_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/toolsets"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/authztest"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
	"github.com/speakeasy-api/gram/server/internal/urn"
	variationsrepo "github.com/speakeasy-api/gram/server/internal/variations/repo"
)

// seedTaggedVariation upserts a variation carrying the given tags (and an
// optional name override) for a tool URN into the given group.
func seedTaggedVariation(t *testing.T, ctx context.Context, conn *pgxpool.Pool, groupID uuid.UUID, toolURN urn.Tool, srcName string, nameOverride string, tags []string) {
	t.Helper()

	_, err := variationsrepo.New(conn).UpsertToolVariation(ctx, variationsrepo.UpsertToolVariationParams{
		GroupID:         groupID,
		SrcToolUrn:      toolURN,
		SrcToolName:     srcName,
		Confirm:         pgtype.Text{String: "", Valid: false},
		ConfirmPrompt:   pgtype.Text{String: "", Valid: false},
		Name:            pgtype.Text{String: nameOverride, Valid: nameOverride != ""},
		Summary:         pgtype.Text{String: "", Valid: false},
		Description:     pgtype.Text{String: "", Valid: false},
		Tags:            tags,
		Summarizer:      pgtype.Text{String: "", Valid: false},
		Title:           pgtype.Text{String: "", Valid: false},
		ReadOnlyHint:    pgtype.Bool{Bool: false, Valid: false},
		DestructiveHint: pgtype.Bool{Bool: false, Valid: false},
		IdempotentHint:  pgtype.Bool{Bool: false, Valid: false},
		OpenWorldHint:   pgtype.Bool{Bool: false, Valid: false},
	})
	require.NoError(t, err)
}

func TestToolsetsService_ListToolFilters_FilteringDisabled(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestToolsetsService(t)

	dep := createPetstoreDeployment(t, ctx, ti)
	tools, err := testrepo.New(ti.conn).ListDeploymentHTTPTools(ctx, uuid.MustParse(dep.Deployment.ID))
	require.NoError(t, err)
	require.GreaterOrEqual(t, len(tools), 2)

	created, err := ti.service.CreateToolset(ctx, &gen.CreateToolsetPayload{
		SessionToken:           nil,
		ApikeyToken:            nil,
		Name:                   "filters disabled",
		Description:            nil,
		ToolUrns:               []string{tools[0].ToolUrn.String(), tools[1].ToolUrn.String()},
		ResourceUrns:           nil,
		DefaultEnvironmentSlug: nil,
		ProjectSlugInput:       nil,
	})
	require.NoError(t, err)

	res, err := ti.service.ListToolFilters(ctx, &gen.ListToolFiltersPayload{
		Slug:             created.Slug,
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	})
	require.NoError(t, err)
	require.False(t, res.FilteringEnabled, "no group assigned means filtering disabled")
	require.Nil(t, res.ToolVariationsGroupID)
	require.Nil(t, res.ToolVariationsGroupName)
	require.Empty(t, res.Scopes)
	require.Empty(t, res.Excluded)
}

func TestToolsetsService_ListToolFilters_ProjectDefaultTagsNotTreatedAsFiltering(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestToolsetsService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	dep := createPetstoreDeployment(t, ctx, ti)
	tools, err := testrepo.New(ti.conn).ListDeploymentHTTPTools(ctx, uuid.MustParse(dep.Deployment.ID))
	require.NoError(t, err)
	require.GreaterOrEqual(t, len(tools), 1)

	created, err := ti.service.CreateToolset(ctx, &gen.CreateToolsetPayload{
		SessionToken:           nil,
		ApikeyToken:            nil,
		Name:                   "project default tags",
		Description:            nil,
		ToolUrns:               []string{tools[0].ToolUrn.String()},
		ResourceUrns:           nil,
		DefaultEnvironmentSlug: nil,
		ProjectSlugInput:       nil,
	})
	require.NoError(t, err)

	// Seed the project-default (global) variations group with a tagged variation,
	// but do NOT assign any explicit group to the toolset. The panel treats only
	// an explicit group as filtering, so this must report disabled even though
	// the runtime ?tags= path would honor the project-default tags.
	groupID := seedToolVariationsGroup(t, ctx, ti.conn, *authCtx.ProjectID)
	seedTaggedVariation(t, ctx, ti.conn, groupID, tools[0].ToolUrn, tools[0].Name, "", []string{"read"})

	res, err := ti.service.ListToolFilters(ctx, &gen.ListToolFiltersPayload{
		Slug:             created.Slug,
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	})
	require.NoError(t, err)
	require.False(t, res.FilteringEnabled, "project-default group tags must not enable the scopes panel")
	require.Nil(t, res.ToolVariationsGroupID)
	require.Empty(t, res.Scopes)
	require.Empty(t, res.Excluded)
}

func TestToolsetsService_ListToolFilters_WithScopesAndExcluded(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestToolsetsService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	dep := createPetstoreDeployment(t, ctx, ti)
	tools, err := testrepo.New(ti.conn).ListDeploymentHTTPTools(ctx, uuid.MustParse(dep.Deployment.ID))
	require.NoError(t, err)
	require.GreaterOrEqual(t, len(tools), 2)

	created, err := ti.service.CreateToolset(ctx, &gen.CreateToolsetPayload{
		SessionToken:           nil,
		ApikeyToken:            nil,
		Name:                   "filters enabled",
		Description:            nil,
		ToolUrns:               []string{tools[0].ToolUrn.String(), tools[1].ToolUrn.String()},
		ResourceUrns:           nil,
		DefaultEnvironmentSlug: nil,
		ProjectSlugInput:       nil,
	})
	require.NoError(t, err)

	// Assign a variation group. Tag the first tool, and give the second tool an
	// explicit empty tag set so it is excluded from every filter (overriding any
	// source tags the petstore spec attaches).
	groupID := seedToolVariationsGroup(t, ctx, ti.conn, *authCtx.ProjectID)
	seedTaggedVariation(t, ctx, ti.conn, groupID, tools[0].ToolUrn, tools[0].Name, "Renamed Tool", []string{"read", "admin"})
	seedTaggedVariation(t, ctx, ti.conn, groupID, tools[1].ToolUrn, tools[1].Name, "", []string{})

	groupIDStr := groupID.String()
	_, err = ti.service.SetToolVariationsGroup(ctx, &gen.SetToolVariationsGroupPayload{
		SessionToken:          nil,
		ApikeyToken:           nil,
		Slug:                  created.Slug,
		ToolVariationsGroupID: &groupIDStr,
		ProjectSlugInput:      nil,
	})
	require.NoError(t, err)

	res, err := ti.service.ListToolFilters(ctx, &gen.ListToolFiltersPayload{
		Slug:             created.Slug,
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	})
	require.NoError(t, err)

	require.True(t, res.FilteringEnabled)
	require.NotNil(t, res.ToolVariationsGroupID)
	require.Equal(t, groupIDStr, *res.ToolVariationsGroupID)
	require.NotNil(t, res.ToolVariationsGroupName)
	require.Equal(t, "Global tool variations", *res.ToolVariationsGroupName)

	// Scopes are sorted by tag; the renamed first tool appears under both tags.
	require.Len(t, res.Scopes, 2)
	require.Equal(t, "admin", res.Scopes[0].Tag)
	require.Equal(t, "read", res.Scopes[1].Tag)
	for _, scope := range res.Scopes {
		require.Equal(t, 1, scope.ToolCount)
		require.Len(t, scope.Tools, 1)
		require.Equal(t, tools[0].ToolUrn.String(), scope.Tools[0].ToolUrn)
		require.Equal(t, "Renamed Tool", scope.Tools[0].Name, "variation rename should be reflected")
	}

	require.Len(t, res.Excluded, 1)
	require.Equal(t, tools[1].ToolUrn.String(), res.Excluded[0].ToolUrn)
}

func TestToolsetsService_ListToolFilters_NotFound(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestToolsetsService(t)

	_, err := ti.service.ListToolFilters(ctx, &gen.ListToolFiltersPayload{
		Slug:             "non-existent-slug",
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	})
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeNotFound, oopsErr.Code)
}

func TestToolsetsService_ListToolFilters_Unauthorized(t *testing.T) {
	t.Parallel()

	_, ti := newTestToolsetsService(t)

	_, err := ti.service.ListToolFilters(t.Context(), &gen.ListToolFiltersPayload{
		Slug:             "some-slug",
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "unauthorized")
}

func TestToolsetsService_ListToolFilters_RBAC_DeniedWithoutGrant(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestToolsetsService(t)

	toolset := createMinimalPrivateToolset(t, ctx, ti, "rbac-listfilters-denied")

	// Restrict to no grants; the mcp:read check must reject the caller.
	deniedCtx := authztest.WithExactGrants(t, ctx)
	_, err := ti.service.ListToolFilters(deniedCtx, &gen.ListToolFiltersPayload{
		Slug:             toolset.Slug,
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	})
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeForbidden, oopsErr.Code)
}

func TestToolsetsService_ListToolFilters_RBAC_AllowedWithToolsetGrant(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestToolsetsService(t)

	toolset := createMinimalPrivateToolset(t, ctx, ti, "rbac-listfilters-allowed")

	// A grant scoped to this specific toolset is sufficient, confirming the
	// handler authorizes at toolset granularity (a project-scope-only gate would
	// wrongly reject this caller).
	grantedCtx := authztest.WithExactGrants(t, ctx, authz.Grant{
		Scope:    authz.ScopeMCPRead,
		Selector: authz.NewSelector(authz.ScopeMCPRead, toolset.ID),
	})
	res, err := ti.service.ListToolFilters(grantedCtx, &gen.ListToolFiltersPayload{
		Slug:             toolset.Slug,
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	})
	require.NoError(t, err)
	require.False(t, res.FilteringEnabled, "no group assigned means filtering disabled")
}
