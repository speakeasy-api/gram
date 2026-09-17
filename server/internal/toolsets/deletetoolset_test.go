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
	projectsrepo "github.com/speakeasy-api/gram/server/internal/projects/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
	toolsetsrepo "github.com/speakeasy-api/gram/server/internal/toolsets/repo"
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
	otherProject, err := projectsrepo.New(ti.conn).CreateProject(ctx, projectsrepo.CreateProjectParams{
		Name: "Other project", Slug: "other-project", OrganizationID: authCtx.ActiveOrganizationID,
	})
	require.NoError(t, err)
	otherAssistant, err := ar.CreateAssistant(ctx, assistantsrepo.CreateAssistantParams{
		ProjectID: otherProject.ID, OrganizationID: authCtx.ActiveOrganizationID,
		Name: "other assistant", Model: "test-model", Status: "active", MaxConcurrency: 1,
	})
	require.NoError(t, err)
	otherTarget, err := toolsetsrepo.New(ti.conn).CreateToolset(ctx, toolsetsrepo.CreateToolsetParams{
		ProjectID: otherProject.ID, OrganizationID: authCtx.ActiveOrganizationID, Name: "Other tools", Slug: "other-tools",
	})
	require.NoError(t, err)
	_, err = ar.AddAssistantToolsets(ctx, []assistantsrepo.AddAssistantToolsetsParams{{
		AssistantID: otherAssistant.ID, ToolsetID: otherTarget.ID, ProjectID: otherProject.ID,
	}})
	require.NoError(t, err)
	otherBefore, err := ar.LoadAssistantToolsets(ctx, assistantsrepo.LoadAssistantToolsetsParams{
		AssistantIds: []uuid.UUID{otherAssistant.ID}, ProjectID: otherProject.ID,
	})
	require.NoError(t, err)
	require.Len(t, otherBefore, 1)
	var ids []string
	var deleteID string
	for i, name := range []string{"deleted target", "retained target"} {
		created, err := ti.service.CreateToolset(ctx, &gen.CreateToolsetPayload{Name: name, ToolUrns: []string{}})
		require.NoError(t, err)
		ids = append(ids, created.ID)
		if i == 0 {
			deleteID = string(created.Slug)
		}
		_, err = ar.AddAssistantToolsets(ctx, []assistantsrepo.AddAssistantToolsetsParams{{
			AssistantID: assistant.ID, ToolsetID: uuid.MustParse(created.ID), ProjectID: *authCtx.ProjectID,
		}})
		require.NoError(t, err)
	}
	err = toolsetsrepo.New(ti.conn).DeleteAssistantToolsetsByToolset(ctx, toolsetsrepo.DeleteAssistantToolsetsByToolsetParams{
		ToolsetID: uuid.MustParse(ids[0]), ProjectID: otherProject.ID,
	})
	require.NoError(t, err)
	before, err := ar.LoadAssistantToolsets(ctx, assistantsrepo.LoadAssistantToolsetsParams{
		AssistantIds: []uuid.UUID{assistant.ID}, ProjectID: *authCtx.ProjectID,
	})
	require.NoError(t, err)
	require.Len(t, before, 2, "a different project cannot detach these attachments")
	err = ti.service.DeleteToolset(ctx, &gen.DeleteToolsetPayload{Slug: types.Slug(deleteID)})
	require.NoError(t, err)
	counts, err := testrepo.New(ti.conn).CountAssistantAttachments(ctx, testrepo.CountAssistantAttachmentsParams{
		ProjectID: *authCtx.ProjectID, AssistantID: assistant.ID,
	})
	require.NoError(t, err)
	require.EqualValues(t, 1, counts.Toolsets, "deleted target must be physically detached")
	remaining, err := ar.LoadAssistantToolsets(ctx, assistantsrepo.LoadAssistantToolsetsParams{
		AssistantIds: []uuid.UUID{assistant.ID}, ProjectID: *authCtx.ProjectID,
	})
	require.NoError(t, err)
	require.Len(t, remaining, 1)
	require.Equal(t, uuid.MustParse(ids[1]), remaining[0].ToolsetID, "unrelated target must remain")
	otherAfter, err := ar.LoadAssistantToolsets(ctx, assistantsrepo.LoadAssistantToolsetsParams{
		AssistantIds: []uuid.UUID{otherAssistant.ID}, ProjectID: otherProject.ID,
	})
	require.NoError(t, err)
	require.Equal(t, otherBefore, otherAfter, "another project's independent attachments must survive deletion")

	core := assistants.NewServiceCore(testenv.NewLogger(t), testenv.NewTracerProvider(t), testenv.NewMeterProvider(t), ti.conn, nil, nil, nil, nil, nil, nil, nil, nil, audit.NewLogger())
	reloaded, err := core.GetAssistant(ctx, *authCtx.ProjectID, assistant.ID)
	require.NoError(t, err)
	require.Len(t, reloaded.Toolsets, 1)
	name := "updated assistant"
	updated, err := core.UpdateAssistant(ctx, *authCtx.ProjectID, reloaded.ID, &name, nil, nil, []*types.AssistantToolsetRef{{ToolsetSlug: reloaded.Toolsets[0].ToolsetSlug}}, nil, nil, nil, nil)
	require.NoError(t, err)
	require.Equal(t, name, updated.Name)
}
