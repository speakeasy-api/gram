package toolsets_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/toolsets"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	pluginsrepo "github.com/speakeasy-api/gram/server/internal/plugins/repo"
)

func TestToolsetsService_DeleteToolset_Success(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestToolsetsService(t)
	beforeCount, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionToolsetDelete)
	require.NoError(t, err)

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

	afterCount, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionToolsetDelete)
	require.NoError(t, err)
	require.Equal(t, beforeCount+1, afterCount)
}

func TestToolsetsService_DeleteToolset_NotFound(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestToolsetsService(t)
	beforeCount, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionToolsetDelete)
	require.NoError(t, err)

	// Try to delete a non-existent toolset
	err = ti.service.DeleteToolset(ctx, &gen.DeleteToolsetPayload{
		Slug:             "non-existent-slug",
		SessionToken:     nil,
		ProjectSlugInput: nil,
	})
	require.NoError(t, err) // Delete operations are typically idempotent

	afterCount, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionToolsetDelete)
	require.NoError(t, err)
	require.Equal(t, beforeCount, afterCount)
}

func TestToolsetsService_DeleteToolset_Unauthorized(t *testing.T) {
	t.Parallel()

	_, ti := newTestToolsetsService(t)
	beforeCount, err := audittest.AuditLogCountByAction(t.Context(), ti.conn, audit.ActionToolsetDelete)
	require.NoError(t, err)

	// Test with context that has no auth context
	ctx := t.Context()

	err = ti.service.DeleteToolset(ctx, &gen.DeleteToolsetPayload{
		Slug:             "some-slug",
		SessionToken:     nil,
		ProjectSlugInput: nil,
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "unauthorized")

	afterCount, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionToolsetDelete)
	require.NoError(t, err)
	require.Equal(t, beforeCount, afterCount)
}

func TestToolsetsService_DeleteToolset_NoProjectID(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestToolsetsService(t)
	beforeCount, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionToolsetDelete)
	require.NoError(t, err)

	// Create auth context without project ID
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	authCtx.ProjectID = nil
	ctx = contextvalues.SetAuthContext(ctx, authCtx)

	err = ti.service.DeleteToolset(ctx, &gen.DeleteToolsetPayload{
		Slug:             "some-slug",
		SessionToken:     nil,
		ProjectSlugInput: nil,
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "unauthorized")

	afterCount, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionToolsetDelete)
	require.NoError(t, err)
	require.Equal(t, beforeCount, afterCount)
}

func TestToolsetsService_DeleteToolset_VerifyListAfterDelete(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestToolsetsService(t)
	beforeCount, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionToolsetDelete)
	require.NoError(t, err)

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

	afterCount, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionToolsetDelete)
	require.NoError(t, err)
	require.Equal(t, beforeCount+1, afterCount)
}

func TestToolsetsService_DeleteToolset_AuditLog(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestToolsetsService(t)
	beforeCount, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionToolsetDelete)
	require.NoError(t, err)

	created, err := ti.service.CreateToolset(ctx, &gen.CreateToolsetPayload{
		SessionToken:           nil,
		Name:                   "Audit Delete Toolset",
		Description:            nil,
		ToolUrns:               []string{},
		ResourceUrns:           nil,
		DefaultEnvironmentSlug: nil,
		ProjectSlugInput:       nil,
	})
	require.NoError(t, err)

	err = ti.service.DeleteToolset(ctx, &gen.DeleteToolsetPayload{
		Slug:             created.Slug,
		SessionToken:     nil,
		ProjectSlugInput: nil,
	})
	require.NoError(t, err)

	record, err := audittest.LatestAuditLogByAction(ctx, ti.conn, audit.ActionToolsetDelete)
	require.NoError(t, err)
	require.Equal(t, string(audit.ActionToolsetDelete), record.Action)
	require.Equal(t, "toolset", record.SubjectType)
	require.Equal(t, created.Name, record.SubjectDisplay)
	require.Equal(t, string(created.Slug), record.SubjectSlug)
	require.Nil(t, record.BeforeSnapshot)
	require.Nil(t, record.AfterSnapshot)

	afterCount, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionToolsetDelete)
	require.NoError(t, err)
	require.Equal(t, beforeCount+1, afterCount)
}

func TestToolsetsService_DeleteToolset_NotFound_NoAuditLog(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestToolsetsService(t)
	beforeCount, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionToolsetDelete)
	require.NoError(t, err)

	err = ti.service.DeleteToolset(ctx, &gen.DeleteToolsetPayload{
		Slug:             "non-existent-slug",
		SessionToken:     nil,
		ProjectSlugInput: nil,
	})
	require.NoError(t, err)

	afterCount, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionToolsetDelete)
	require.NoError(t, err)
	require.Equal(t, beforeCount, afterCount)
}

// TestToolsetsService_DeleteToolset_RemovesDefaultPluginServer guards the
// delete cascade: a deleted toolset's plugin_servers rows must be
// soft-deleted too, or they keep publishing a dead toolset and hold its
// display name against the (plugin_id, display_name) unique index —
// blocking a same-named replacement from attaching later.
func TestToolsetsService_DeleteToolset_RemovesDefaultPluginServer(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestToolsetsService(t)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	pluginsQueries := pluginsrepo.New(ti.conn)
	defaultPlugin, err := pluginsQueries.CreateDefaultPlugin(ctx, pluginsrepo.CreateDefaultPluginParams{
		OrganizationID: authCtx.ActiveOrganizationID,
		ProjectID:      *authCtx.ProjectID,
	})
	require.NoError(t, err)

	// The org's first toolset auto-enables MCP and auto-attaches to the
	// Default plugin.
	created, err := ti.service.CreateToolset(ctx, &gen.CreateToolsetPayload{
		SessionToken:           nil,
		Name:                   "Cascade Delete Toolset",
		Description:            nil,
		ToolUrns:               []string{},
		ResourceUrns:           nil,
		DefaultEnvironmentSlug: nil,
		ProjectSlugInput:       nil,
	})
	require.NoError(t, err)

	servers, err := pluginsQueries.ListPluginServers(ctx, defaultPlugin.ID)
	require.NoError(t, err)
	require.Len(t, servers, 1)

	beforeRemoveCount, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionPluginServerRemove)
	require.NoError(t, err)

	err = ti.service.DeleteToolset(ctx, &gen.DeleteToolsetPayload{
		SessionToken:     nil,
		Slug:             created.Slug,
		ProjectSlugInput: nil,
	})
	require.NoError(t, err)

	servers, err = pluginsQueries.ListPluginServers(ctx, defaultPlugin.ID)
	require.NoError(t, err)
	require.Empty(t, servers, "deleting the toolset must soft-delete its plugin_servers rows")

	afterRemoveCount, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionPluginServerRemove)
	require.NoError(t, err)
	require.Equal(t, beforeRemoveCount+1, afterRemoveCount)
}
