package mcpservers_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/mcp_servers"
	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oops"
	toolsetsrepo "github.com/speakeasy-api/gram/server/internal/toolsets/repo"
)

// seedToolset inserts an empty toolset in the caller's project so an mcp server
// can be backed by it. Tool population is exercised by the toolsets
// ListToolFilters integration test; here the toolset stays empty so the focus is
// the mcp_servers → toolsets group resolution.
func seedToolset(t *testing.T, ctx context.Context, conn *pgxpool.Pool, organizationID string, projectID uuid.UUID) toolsetsrepo.Toolset {
	t.Helper()

	slug := "ts-" + uuid.New().String()[:8]
	toolset, err := toolsetsrepo.New(conn).CreateToolset(ctx, toolsetsrepo.CreateToolsetParams{
		OrganizationID:         organizationID,
		ProjectID:              projectID,
		Name:                   slug,
		Slug:                   slug,
		Description:            pgtype.Text{String: "", Valid: false},
		DefaultEnvironmentSlug: pgtype.Text{String: "", Valid: false},
		McpSlug:                pgtype.Text{String: "", Valid: false},
		McpEnabled:             false,
	})
	require.NoError(t, err)

	return toolset
}

func TestListToolFilters_RemoteBackedReturnsDisabled(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	remoteID := seedRemoteMcpServer(t, ctx, ti.conn, *authCtx.ProjectID).String()
	created, err := ti.service.CreateMcpServer(ctx, &gen.CreateMcpServerPayload{
		SessionToken:      nil,
		ApikeyToken:       nil,
		ProjectSlugInput:  nil,
		Name:              "remote backed server",
		EnvironmentID:     nil,
		RemoteMcpServerID: &remoteID,
		ToolsetID:         nil,
		Visibility:        types.McpServerVisibility("disabled"),
	})
	require.NoError(t, err)

	res, err := ti.service.ListToolFilters(ctx, &gen.ListToolFiltersPayload{
		ID:               &created.ID,
		Slug:             nil,
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	})
	require.NoError(t, err)
	require.False(t, res.FilteringEnabled, "remote-backed servers have no toolset tools to filter")
	require.Empty(t, res.Scopes)
	require.Empty(t, res.Excluded)
}

func TestListToolFilters_NotFound(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	id := uuid.New().String()
	_, err := ti.service.ListToolFilters(ctx, &gen.ListToolFiltersPayload{
		ID:               &id,
		Slug:             nil,
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	})
	requireOopsCode(t, err, oops.CodeNotFound)
}

func TestListToolFilters_RequiresIDOrSlug(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	_, err := ti.service.ListToolFilters(ctx, &gen.ListToolFiltersPayload{
		ID:               nil,
		Slug:             nil,
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	})
	requireOopsCode(t, err, oops.CodeBadRequest)
}

func TestListToolFilters_IDAndSlugMutuallyExclusive(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	id := uuid.New().String()
	slug := "some-slug"
	_, err := ti.service.ListToolFilters(ctx, &gen.ListToolFiltersPayload{
		ID:               &id,
		Slug:             &slug,
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	})
	requireOopsCode(t, err, oops.CodeBadRequest)
}

func TestListToolFilters_ToolsetBackedResolvesServerGroup(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	toolset := seedToolset(t, ctx, ti.conn, authCtx.ActiveOrganizationID, *authCtx.ProjectID)
	groupID := seedToolVariationsGroup(t, ctx, ti.conn, *authCtx.ProjectID)

	toolsetID := toolset.ID.String()
	groupIDStr := groupID.String()
	created, err := ti.service.CreateMcpServer(ctx, &gen.CreateMcpServerPayload{
		SessionToken:          nil,
		ApikeyToken:           nil,
		ProjectSlugInput:      nil,
		Name:                  "toolset backed server",
		EnvironmentID:         nil,
		RemoteMcpServerID:     nil,
		ToolsetID:             &toolsetID,
		ToolVariationsGroupID: &groupIDStr,
		Visibility:            types.McpServerVisibility("disabled"),
	})
	require.NoError(t, err)

	res, err := ti.service.ListToolFilters(ctx, &gen.ListToolFiltersPayload{
		ID:               &created.ID,
		Slug:             nil,
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	})
	require.NoError(t, err)
	require.True(t, res.FilteringEnabled)
	require.NotNil(t, res.ToolVariationsGroupID)
	require.Equal(t, groupIDStr, *res.ToolVariationsGroupID, "mcp_server group should win the resolution chain")
	require.NotNil(t, res.ToolVariationsGroupName)
	require.Equal(t, "Global tool variations", *res.ToolVariationsGroupName)
	// The backing toolset has no tools; populated scopes/excluded are covered by
	// the toolsets ListToolFilters integration test, which exercises the same
	// DescribeToolset -> BuildView path with real tools.
	require.Empty(t, res.Scopes)
	require.Empty(t, res.Excluded)
}

func TestListToolFilters_ToolsetBackedFallsBackToToolsetGroup(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	toolset := seedToolset(t, ctx, ti.conn, authCtx.ActiveOrganizationID, *authCtx.ProjectID)
	groupID := seedToolVariationsGroup(t, ctx, ti.conn, *authCtx.ProjectID)

	// Assign the group to the toolset, and leave the mcp server's own group
	// unset, so resolution must fall back to the toolset's column.
	_, err := toolsetsrepo.New(ti.conn).UpdateToolsetToolVariationsGroup(ctx, toolsetsrepo.UpdateToolsetToolVariationsGroupParams{
		ToolVariationsGroupID: uuid.NullUUID{UUID: groupID, Valid: true},
		Slug:                  toolset.Slug,
		ProjectID:             *authCtx.ProjectID,
	})
	require.NoError(t, err)

	toolsetID := toolset.ID.String()
	created, err := ti.service.CreateMcpServer(ctx, &gen.CreateMcpServerPayload{
		SessionToken:          nil,
		ApikeyToken:           nil,
		ProjectSlugInput:      nil,
		Name:                  "toolset backed server fallback",
		EnvironmentID:         nil,
		RemoteMcpServerID:     nil,
		ToolsetID:             &toolsetID,
		ToolVariationsGroupID: nil,
		Visibility:            types.McpServerVisibility("disabled"),
	})
	require.NoError(t, err)

	res, err := ti.service.ListToolFilters(ctx, &gen.ListToolFiltersPayload{
		ID:               &created.ID,
		Slug:             nil,
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	})
	require.NoError(t, err)
	require.True(t, res.FilteringEnabled)
	require.NotNil(t, res.ToolVariationsGroupID)
	require.Equal(t, groupID.String(), *res.ToolVariationsGroupID, "resolution should fall back to the toolset group")
}

func TestListToolFilters_Unauthenticated(t *testing.T) {
	t.Parallel()

	_, ti := newTestService(t)

	id := uuid.New().String()
	_, err := ti.service.ListToolFilters(t.Context(), &gen.ListToolFiltersPayload{
		ID:               &id,
		Slug:             nil,
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	})
	requireOopsCode(t, err, oops.CodeUnauthorized)
}

func TestListToolFilters_RBAC_DeniedWithoutGrant(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	// No grants: the mcp:read project-scope check rejects the caller before any
	// server lookup, so a random id still yields forbidden.
	deniedCtx := withExactAuthzGrants(t, ctx, ti.conn)
	id := uuid.New().String()
	_, err := ti.service.ListToolFilters(deniedCtx, &gen.ListToolFiltersPayload{
		ID:               &id,
		Slug:             nil,
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	})
	requireOopsCode(t, err, oops.CodeForbidden)
}

func TestListToolFilters_RBAC_AllowedWithProjectGrant(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	remoteID := seedRemoteMcpServer(t, ctx, ti.conn, *authCtx.ProjectID).String()
	created, err := ti.service.CreateMcpServer(ctx, &gen.CreateMcpServerPayload{
		SessionToken:      nil,
		ApikeyToken:       nil,
		ProjectSlugInput:  nil,
		Name:              "rbac allowed server",
		EnvironmentID:     nil,
		RemoteMcpServerID: &remoteID,
		ToolsetID:         nil,
		Visibility:        types.McpServerVisibility("disabled"),
	})
	require.NoError(t, err)

	grantedCtx := withExactAuthzGrants(t, ctx, ti.conn, authz.Grant{
		Scope:    authz.ScopeMCPRead,
		Selector: authz.NewSelector(authz.ScopeMCPRead, authCtx.ProjectID.String()),
	})
	res, err := ti.service.ListToolFilters(grantedCtx, &gen.ListToolFiltersPayload{
		ID:               &created.ID,
		Slug:             nil,
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	})
	require.NoError(t, err)
	require.False(t, res.FilteringEnabled, "remote-backed server reports filtering disabled")
}
