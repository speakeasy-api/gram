package toolsets_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/toolsets"
	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
)

func TestToolsetsService_CloneToolset_Success(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestToolsetsService(t)
	beforeCount, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionToolsetCreate)
	require.NoError(t, err)

	// Create deployment with petstore fixture
	dep := createPetstoreDeployment(t, ctx, ti)

	// Get the tools from the deployment
	repo := testrepo.New(ti.conn)
	tools, err := repo.ListDeploymentHTTPTools(ctx, uuid.MustParse(dep.Deployment.ID))
	require.NoError(t, err, "list deployment tools")
	require.GreaterOrEqual(t, len(tools), 2, "expected at least 2 tools from petstore")

	// Extract tool URNs
	toolUrns := make([]string, 2)
	toolUrns[0] = tools[0].ToolUrn.String()
	toolUrns[1] = tools[1].ToolUrn.String()

	// Create an original toolset to clone
	original, err := ti.service.CreateToolset(ctx, &gen.CreateToolsetPayload{
		SessionToken:           nil,
		Name:                   "Original Toolset",
		Description:            new("Original toolset description"),
		ToolUrns:               toolUrns,
		ResourceUrns:           nil,
		DefaultEnvironmentSlug: nil,
		ProjectSlugInput:       nil,
	})
	require.NoError(t, err)
	require.NotNil(t, original)

	// Clone the toolset
	cloned, err := ti.service.CloneToolset(ctx, &gen.CloneToolsetPayload{
		SessionToken:     nil,
		Slug:             original.Slug,
		ProjectSlugInput: nil,
	})
	require.NoError(t, err)
	require.NotNil(t, cloned)

	// Verify the cloned toolset
	require.Equal(t, "Original Toolset_copy", cloned.Name)
	require.Equal(t, "original-toolsetcopy", string(cloned.Slug)) // Slug conversion removes underscore
	require.Equal(t, "Original toolset description", *cloned.Description)
	require.Len(t, cloned.Tools, 2, "should have same number of HTTP tools")
	require.NotEqual(t, original.ID, cloned.ID, "should have different ID")

	// Verify the tools are correctly copied
	originalToolNames := make([]string, len(original.Tools))
	for i, tool := range original.Tools {
		baseTool, err := conv.ToBaseTool(tool)
		require.NoError(t, err)
		originalToolNames[i] = baseTool.Name
	}
	clonedToolNames := make([]string, len(cloned.Tools))
	for i, tool := range cloned.Tools {
		baseTool, err := conv.ToBaseTool(tool)
		require.NoError(t, err)
		clonedToolNames[i] = baseTool.Name
	}
	require.ElementsMatch(t, originalToolNames, clonedToolNames)

	afterCount, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionToolsetCreate)
	require.NoError(t, err)
	require.Equal(t, beforeCount+2, afterCount)
}

func TestToolsetsService_CloneToolset_MultipleClones(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestToolsetsService(t)
	beforeCount, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionToolsetCreate)
	require.NoError(t, err)

	// Create deployment with petstore fixture
	dep := createPetstoreDeployment(t, ctx, ti)

	// Get a tool from the deployment
	repo := testrepo.New(ti.conn)
	tools, err := repo.ListDeploymentHTTPTools(ctx, uuid.MustParse(dep.Deployment.ID))
	require.NoError(t, err, "list deployment tools")
	require.NotEmpty(t, tools, "expected tools from petstore")

	// Create an original toolset
	original, err := ti.service.CreateToolset(ctx, &gen.CreateToolsetPayload{
		SessionToken:           nil,
		Name:                   "Original",
		Description:            nil,
		ToolUrns:               []string{tools[0].ToolUrn.String()},
		ResourceUrns:           nil,
		DefaultEnvironmentSlug: nil,
		ProjectSlugInput:       nil,
	})
	require.NoError(t, err)

	// Clone the toolset multiple times
	cloned1, err := ti.service.CloneToolset(ctx, &gen.CloneToolsetPayload{
		SessionToken:     nil,
		Slug:             original.Slug,
		ProjectSlugInput: nil,
	})
	require.NoError(t, err)
	require.Equal(t, "Original_copy", cloned1.Name)

	// Clone again - should get a numbered suffix
	cloned2, err := ti.service.CloneToolset(ctx, &gen.CloneToolsetPayload{
		SessionToken:     nil,
		Slug:             original.Slug,
		ProjectSlugInput: nil,
	})
	require.NoError(t, err)
	require.Equal(t, "Original_copy2", cloned2.Name)

	// Clone once more
	cloned3, err := ti.service.CloneToolset(ctx, &gen.CloneToolsetPayload{
		SessionToken:     nil,
		Slug:             original.Slug,
		ProjectSlugInput: nil,
	})
	require.NoError(t, err)
	require.Equal(t, "Original_copy3", cloned3.Name)

	// Verify all have different IDs and slugs
	require.NotEqual(t, original.ID, cloned1.ID)
	require.NotEqual(t, original.ID, cloned2.ID)
	require.NotEqual(t, original.ID, cloned3.ID)
	require.NotEqual(t, cloned1.ID, cloned2.ID)
	require.NotEqual(t, cloned1.ID, cloned3.ID)
	require.NotEqual(t, cloned2.ID, cloned3.ID)

	require.NotEqual(t, original.Slug, cloned1.Slug)
	require.NotEqual(t, original.Slug, cloned2.Slug)
	require.NotEqual(t, original.Slug, cloned3.Slug)
	require.NotEqual(t, cloned1.Slug, cloned2.Slug)
	require.NotEqual(t, cloned1.Slug, cloned3.Slug)
	require.NotEqual(t, cloned2.Slug, cloned3.Slug)

	afterCount, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionToolsetCreate)
	require.NoError(t, err)
	require.Equal(t, beforeCount+4, afterCount)
}

func TestToolsetsService_CloneToolset_NotFound(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestToolsetsService(t)
	beforeCount, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionToolsetCreate)
	require.NoError(t, err)

	// Try to clone a non-existent toolset
	_, err = ti.service.CloneToolset(ctx, &gen.CloneToolsetPayload{
		SessionToken:     nil,
		Slug:             types.Slug("non-existent-toolset"),
		ProjectSlugInput: nil,
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "toolset not found")

	afterCount, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionToolsetCreate)
	require.NoError(t, err)
	require.Equal(t, beforeCount, afterCount)
}

func TestToolsetsService_CloneToolset_Unauthorized(t *testing.T) {
	t.Parallel()

	_, ti := newTestToolsetsService(t)
	beforeCount, err := audittest.AuditLogCountByAction(t.Context(), ti.conn, audit.ActionToolsetCreate)
	require.NoError(t, err)

	// Test with context that has no auth context
	ctx := t.Context()

	_, err = ti.service.CloneToolset(ctx, &gen.CloneToolsetPayload{
		SessionToken:     nil,
		Slug:             types.Slug("some-toolset"),
		ProjectSlugInput: nil,
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "unauthorized")

	afterCount, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionToolsetCreate)
	require.NoError(t, err)
	require.Equal(t, beforeCount, afterCount)
}

func TestToolsetsService_CloneToolset_NoProjectID(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestToolsetsService(t)
	beforeCount, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionToolsetCreate)
	require.NoError(t, err)

	// Create auth context without project ID
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	authCtx.ProjectID = nil
	ctx = contextvalues.SetAuthContext(ctx, authCtx)

	_, err = ti.service.CloneToolset(ctx, &gen.CloneToolsetPayload{
		SessionToken:     nil,
		Slug:             types.Slug("some-toolset"),
		ProjectSlugInput: nil,
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "unauthorized")

	afterCount, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionToolsetCreate)
	require.NoError(t, err)
	require.Equal(t, beforeCount, afterCount)
}

func TestToolsetsService_CloneToolset_AuditLog(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestToolsetsService(t)
	beforeCount, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionToolsetCreate)
	require.NoError(t, err)

	original, err := ti.service.CreateToolset(ctx, &gen.CreateToolsetPayload{
		SessionToken:           nil,
		Name:                   "Clone Audit Original",
		Description:            nil,
		ToolUrns:               []string{},
		ResourceUrns:           nil,
		DefaultEnvironmentSlug: nil,
		ProjectSlugInput:       nil,
	})
	require.NoError(t, err)

	middleCount, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionToolsetCreate)
	require.NoError(t, err)
	require.Equal(t, beforeCount+1, middleCount)

	cloned, err := ti.service.CloneToolset(ctx, &gen.CloneToolsetPayload{
		SessionToken:     nil,
		Slug:             original.Slug,
		ProjectSlugInput: nil,
	})
	require.NoError(t, err)
	require.NotNil(t, cloned)

	record, err := audittest.LatestAuditLogByAction(ctx, ti.conn, audit.ActionToolsetCreate)
	require.NoError(t, err)
	require.Equal(t, string(audit.ActionToolsetCreate), record.Action)
	require.Equal(t, "toolset", record.SubjectType)
	require.Equal(t, cloned.Name, record.SubjectDisplay)
	require.Equal(t, string(cloned.Slug), record.SubjectSlug)
	require.Nil(t, record.BeforeSnapshot)
	require.Nil(t, record.AfterSnapshot)

	afterCount, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionToolsetCreate)
	require.NoError(t, err)
	require.Equal(t, middleCount+1, afterCount)
}

func TestToolsetsService_CloneToolset_NotFound_NoAuditLog(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestToolsetsService(t)
	beforeCount, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionToolsetCreate)
	require.NoError(t, err)

	_, err = ti.service.CloneToolset(ctx, &gen.CloneToolsetPayload{
		SessionToken:     nil,
		Slug:             types.Slug("non-existent-toolset"),
		ProjectSlugInput: nil,
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "toolset not found")

	afterCount, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionToolsetCreate)
	require.NoError(t, err)
	require.Equal(t, beforeCount, afterCount)
}
