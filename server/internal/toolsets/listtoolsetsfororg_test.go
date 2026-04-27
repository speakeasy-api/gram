package toolsets_test

import (
	"fmt"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/toolsets"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	projectsRepo "github.com/speakeasy-api/gram/server/internal/projects/repo"
)

func TestToolsetsService_ListToolsetsForOrg_Success(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestToolsetsService(t)

	// Create a toolset within the default project
	toolset1, err := ti.service.CreateToolset(ctx, &gen.CreateToolsetPayload{
		SessionToken:           nil,
		Name:                   "Org Toolset One",
		Description:            new("First org toolset"),
		ToolUrns:               []string{},
		ResourceUrns:           nil,
		DefaultEnvironmentSlug: nil,
		ProjectSlugInput:       nil,
	})
	require.NoError(t, err)

	toolset2, err := ti.service.CreateToolset(ctx, &gen.CreateToolsetPayload{
		SessionToken:           nil,
		Name:                   "Org Toolset Two",
		Description:            new("Second org toolset"),
		ToolUrns:               []string{},
		ResourceUrns:           nil,
		DefaultEnvironmentSlug: nil,
		ProjectSlugInput:       nil,
	})
	require.NoError(t, err)

	beforeCount, err := audittest.AuditLogCount(ctx, ti.conn)
	require.NoError(t, err)

	// Call ListToolsetsForOrg (no project scope needed)
	result, err := ti.service.ListToolsetsForOrg(ctx, &gen.ListToolsetsForOrgPayload{
		SessionToken: nil,
	})
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Len(t, result.Toolsets, 2)

	toolsetIDs := make(map[string]bool)
	for _, ts := range result.Toolsets {
		toolsetIDs[ts.ID] = true
	}
	require.True(t, toolsetIDs[toolset1.ID])
	require.True(t, toolsetIDs[toolset2.ID])

	afterCount, err := audittest.AuditLogCount(ctx, ti.conn)
	require.NoError(t, err)
	require.Equal(t, beforeCount, afterCount)
}

func TestToolsetsService_ListToolsetsForOrg_EmptyList(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestToolsetsService(t)
	beforeCount, err := audittest.AuditLogCount(ctx, ti.conn)
	require.NoError(t, err)

	result, err := ti.service.ListToolsetsForOrg(ctx, &gen.ListToolsetsForOrgPayload{
		SessionToken: nil,
	})
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Empty(t, result.Toolsets)

	afterCount, err := audittest.AuditLogCount(ctx, ti.conn)
	require.NoError(t, err)
	require.Equal(t, beforeCount, afterCount)
}

func TestToolsetsService_ListToolsetsForOrg_Unauthorized(t *testing.T) {
	t.Parallel()

	_, ti := newTestToolsetsService(t)
	beforeCount, err := audittest.AuditLogCount(t.Context(), ti.conn)
	require.NoError(t, err)

	// Use context with no auth context
	ctx := t.Context()

	_, err = ti.service.ListToolsetsForOrg(ctx, &gen.ListToolsetsForOrgPayload{
		SessionToken: nil,
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "unauthorized")

	afterCount, err := audittest.AuditLogCount(ctx, ti.conn)
	require.NoError(t, err)
	require.Equal(t, beforeCount, afterCount)
}

func TestToolsetsService_ListToolsetsForOrg_WithoutProjectID(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestToolsetsService(t)

	// Create a toolset while we have project context
	created, err := ti.service.CreateToolset(ctx, &gen.CreateToolsetPayload{
		SessionToken:           nil,
		Name:                   "Org Toolset No Project",
		Description:            new("Should be visible without project scope"),
		ToolUrns:               []string{},
		ResourceUrns:           nil,
		DefaultEnvironmentSlug: nil,
		ProjectSlugInput:       nil,
	})
	require.NoError(t, err)

	// Remove project from auth context — simulates the RBAC page
	// which has no project slug in the URL
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	authCtx.ProjectID = nil
	ctx = contextvalues.SetAuthContext(ctx, authCtx)

	beforeCount, err := audittest.AuditLogCount(ctx, ti.conn)
	require.NoError(t, err)

	result, err := ti.service.ListToolsetsForOrg(ctx, &gen.ListToolsetsForOrgPayload{
		SessionToken: nil,
	})
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Len(t, result.Toolsets, 1)
	require.Equal(t, created.ID, result.Toolsets[0].ID)

	afterCount, err := audittest.AuditLogCount(ctx, ti.conn)
	require.NoError(t, err)
	require.Equal(t, beforeCount, afterCount)
}

func TestToolsetsService_ListToolsetsForOrg_CrossProject(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestToolsetsService(t)

	// Create a toolset in the first (default) project
	toolset1, err := ti.service.CreateToolset(ctx, &gen.CreateToolsetPayload{
		SessionToken:           nil,
		Name:                   "Project A Toolset",
		Description:            new("Toolset in project A"),
		ToolUrns:               []string{},
		ResourceUrns:           nil,
		DefaultEnvironmentSlug: nil,
		ProjectSlugInput:       nil,
	})
	require.NoError(t, err)

	// Create a second project in the same organization
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	projectSlug2 := fmt.Sprintf("test-%s", uuid.New().String()[:8])
	p2, err := projectsRepo.New(ti.conn).CreateProject(ctx, projectsRepo.CreateProjectParams{
		Name:           projectSlug2,
		Slug:           projectSlug2,
		OrganizationID: authCtx.ActiveOrganizationID,
	})
	require.NoError(t, err)

	// Switch auth context to the second project
	authCtx.ProjectID = &p2.ID
	authCtx.ProjectSlug = &p2.Slug
	ctx = contextvalues.SetAuthContext(ctx, authCtx)

	// Create a toolset in the second project
	toolset2, err := ti.service.CreateToolset(ctx, &gen.CreateToolsetPayload{
		SessionToken:           nil,
		Name:                   "Project B Toolset",
		Description:            new("Toolset in project B"),
		ToolUrns:               []string{},
		ResourceUrns:           nil,
		DefaultEnvironmentSlug: nil,
		ProjectSlugInput:       nil,
	})
	require.NoError(t, err)

	// Clear project scope to simulate org-wide query
	authCtx.ProjectID = nil
	ctx = contextvalues.SetAuthContext(ctx, authCtx)

	beforeCount, err := audittest.AuditLogCount(ctx, ti.conn)
	require.NoError(t, err)

	// ListToolsetsForOrg should return toolsets from both projects
	result, err := ti.service.ListToolsetsForOrg(ctx, &gen.ListToolsetsForOrgPayload{
		SessionToken: nil,
	})
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Len(t, result.Toolsets, 2)

	toolsetIDs := make(map[string]bool)
	projectIDs := make(map[string]bool)
	for _, ts := range result.Toolsets {
		toolsetIDs[ts.ID] = true
		projectIDs[ts.ProjectID] = true
	}
	require.True(t, toolsetIDs[toolset1.ID], "toolset from project A should be present")
	require.True(t, toolsetIDs[toolset2.ID], "toolset from project B should be present")
	require.Len(t, projectIDs, 2, "toolsets should come from two different projects")

	afterCount, err := audittest.AuditLogCount(ctx, ti.conn)
	require.NoError(t, err)
	require.Equal(t, beforeCount, afterCount)
}

func TestToolsetsService_ListToolsetsForOrg_VerifyDetails(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestToolsetsService(t)

	created, err := ti.service.CreateToolset(ctx, &gen.CreateToolsetPayload{
		SessionToken:           nil,
		Name:                   "Detailed Org Toolset",
		Description:            new("An org toolset with details"),
		ToolUrns:               []string{},
		ResourceUrns:           nil,
		DefaultEnvironmentSlug: nil,
		ProjectSlugInput:       nil,
	})
	require.NoError(t, err)

	beforeCount, err := audittest.AuditLogCount(ctx, ti.conn)
	require.NoError(t, err)

	result, err := ti.service.ListToolsetsForOrg(ctx, &gen.ListToolsetsForOrgPayload{
		SessionToken: nil,
	})
	require.NoError(t, err)
	require.Len(t, result.Toolsets, 1)

	ts := result.Toolsets[0]
	require.Equal(t, created.ID, ts.ID)
	require.Equal(t, "Detailed Org Toolset", ts.Name)
	require.Equal(t, "detailed-org-toolset", string(ts.Slug))
	require.NotEmpty(t, ts.ProjectID, "project ID should be populated")
	require.NotEmpty(t, ts.CreatedAt)
	require.NotEmpty(t, ts.UpdatedAt)

	afterCount, err := audittest.AuditLogCount(ctx, ti.conn)
	require.NoError(t, err)
	require.Equal(t, beforeCount, afterCount)
}

func TestToolsetsService_ListToolsetsForOrg_ExcludesDeletedToolsets(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestToolsetsService(t)

	// Create two toolsets
	_, err := ti.service.CreateToolset(ctx, &gen.CreateToolsetPayload{
		SessionToken:           nil,
		Name:                   "Kept Toolset",
		Description:            new("This one stays"),
		ToolUrns:               []string{},
		ResourceUrns:           nil,
		DefaultEnvironmentSlug: nil,
		ProjectSlugInput:       nil,
	})
	require.NoError(t, err)

	deleted, err := ti.service.CreateToolset(ctx, &gen.CreateToolsetPayload{
		SessionToken:           nil,
		Name:                   "Deleted Toolset",
		Description:            new("This one gets deleted"),
		ToolUrns:               []string{},
		ResourceUrns:           nil,
		DefaultEnvironmentSlug: nil,
		ProjectSlugInput:       nil,
	})
	require.NoError(t, err)

	// Delete one of them
	err = ti.service.DeleteToolset(ctx, &gen.DeleteToolsetPayload{
		SessionToken:     nil,
		Slug:             deleted.Slug,
		ProjectSlugInput: nil,
	})
	require.NoError(t, err)

	beforeCount, err := audittest.AuditLogCount(ctx, ti.conn)
	require.NoError(t, err)

	result, err := ti.service.ListToolsetsForOrg(ctx, &gen.ListToolsetsForOrgPayload{
		SessionToken: nil,
	})
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Len(t, result.Toolsets, 1, "deleted toolset should not appear")
	require.Equal(t, "Kept Toolset", result.Toolsets[0].Name)

	afterCount, err := audittest.AuditLogCount(ctx, ti.conn)
	require.NoError(t, err)
	require.Equal(t, beforeCount, afterCount)
}
