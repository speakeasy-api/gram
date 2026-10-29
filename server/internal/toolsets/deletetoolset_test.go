package toolsets_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/toolsets"
	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/assistants"
	assistantsrepo "github.com/speakeasy-api/gram/server/internal/assistants/repo"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
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
		Slug:             string(created.Slug),
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

func TestToolsetsService_DeleteToolset_DetachesFromAssistants(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestToolsetsService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	ar := assistantsrepo.New(ti.conn)
	assistant, err := ar.CreateAssistant(ctx, assistantsrepo.CreateAssistantParams{
		ProjectID: *authCtx.ProjectID, OrganizationID: authCtx.ActiveOrganizationID,
		Name: "attachment test assistant", Model: "test-model", Status: "active", MaxConcurrency: 1,
	})
	require.NoError(t, err)
	var deleteID string
	var retainedID uuid.UUID
	for i, name := range []string{"deleted target", "retained target"} {
		created, err := ti.service.CreateToolset(ctx, &gen.CreateToolsetPayload{Name: name, ToolUrns: []string{}})
		require.NoError(t, err)
		if i == 0 {
			deleteID = string(created.Slug)
		} else {
			retainedID = uuid.MustParse(created.ID)
		}
		_, err = ar.AddAssistantMcpServers(ctx, []assistantsrepo.AddAssistantMcpServersParams{{
			AssistantID: assistant.ID, McpServerID: uuid.MustParse(created.ID), ProjectID: *authCtx.ProjectID,
		}})
		require.NoError(t, err)
	}
	err = ti.service.DeleteToolset(ctx, &gen.DeleteToolsetPayload{Slug: types.Slug(deleteID)})
	require.NoError(t, err)
	counts, err := testrepo.New(ti.conn).CountAssistantAttachments(ctx, testrepo.CountAssistantAttachmentsParams{
		ProjectID: *authCtx.ProjectID, AssistantID: assistant.ID,
	})
	require.NoError(t, err)
	require.EqualValues(t, 1, counts, "deleted target must be physically detached")
	remaining, err := ar.ListAssistantAttachedServerIDs(ctx, assistantsrepo.ListAssistantAttachedServerIDsParams{AssistantID: assistant.ID, ProjectID: *authCtx.ProjectID})
	require.NoError(t, err)
	require.Equal(t, []uuid.UUID{retainedID}, remaining, "unrelated target must remain")

	core := assistants.NewServiceCore(testenv.NewLogger(t), testenv.NewTracerProvider(t), testenv.NewMeterProvider(t), ti.conn, nil, nil, nil, nil, nil, nil, nil, nil, audit.NewLogger(), nil, nil)
	reloaded, err := core.GetAssistant(ctx, *authCtx.ProjectID, assistant.ID)
	require.NoError(t, err)
	require.Len(t, reloaded.MCPServers, 1)
	name := "updated assistant"
	updated, err := core.UpdateAssistant(ctx, *authCtx.ProjectID, reloaded.ID, &name, nil, nil, []*types.AssistantToolsetRef{{ToolsetSlug: reloaded.MCPServers[0].ToolsetSlug.String}}, nil, nil, nil, nil, nil)
	require.NoError(t, err)
	require.Equal(t, name, updated.Name)
}
