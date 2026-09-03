package organizations_test

import (
	"errors"
	"testing"

	"github.com/jackc/pgx/v5"

	gen "github.com/speakeasy-api/gram/server/gen/organizations"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/authztest"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/oops"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	"github.com/stretchr/testify/require"
)

func TestService_UpdateSetupTaskPromotesTodoAssignmentAndPreservesReassignmentStatus(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestOrganizationsService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	result, err := ti.service.UpdateSetupTask(ctx, &gen.UpdateSetupTaskPayload{
		TaskKey: "instrument-agents", Assignee: &gen.SetupTaskAssigneeInput{UserID: &authCtx.UserID, Email: nil},
	})
	require.NoError(t, err)
	require.Equal(t, "in_progress", result.Status)
	require.Equal(t, authCtx.UserID, *result.Assignee.UserID)

	awaitingSupport := "awaiting_support"
	_, err = ti.service.UpdateSetupTask(ctx, &gen.UpdateSetupTaskPayload{TaskKey: "instrument-agents", Status: &awaitingSupport})
	require.NoError(t, err)
	require.NotNil(t, authCtx.Email)
	result, err = ti.service.UpdateSetupTask(ctx, &gen.UpdateSetupTaskPayload{
		TaskKey: "instrument-agents", Assignee: &gen.SetupTaskAssigneeInput{UserID: nil, Email: authCtx.Email},
	})
	require.NoError(t, err)
	require.Equal(t, "awaiting_support", result.Status)
	clearAssignee := true
	result, err = ti.service.UpdateSetupTask(ctx, &gen.UpdateSetupTaskPayload{TaskKey: "instrument-agents", ClearAssignee: &clearAssignee})
	require.NoError(t, err)
	require.Nil(t, result.Assignee)
	require.Equal(t, "awaiting_support", result.Status)
}

func TestService_UpdateSetupTaskValidatesTaskStatusAndMembership(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestOrganizationsService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	invalidStatus := "queued"
	clearAssignee := true
	tests := []*gen.UpdateSetupTaskPayload{
		{TaskKey: "unknown-task", Status: conv.PtrEmpty("done")},
		{TaskKey: "instrument-agents", Status: &invalidStatus},
		{TaskKey: "instrument-agents"},
		{TaskKey: "instrument-agents", Assignee: &gen.SetupTaskAssigneeInput{}},
		{TaskKey: "instrument-agents", Assignee: &gen.SetupTaskAssigneeInput{UserID: conv.PtrEmpty("   ")}},
		{TaskKey: "instrument-agents", Assignee: &gen.SetupTaskAssigneeInput{UserID: &authCtx.UserID, Email: conv.PtrEmpty("both@example.test")}},
		{TaskKey: "instrument-agents", Assignee: &gen.SetupTaskAssigneeInput{UserID: &authCtx.UserID}, ClearAssignee: &clearAssignee},
		{TaskKey: "instrument-agents", Assignee: &gen.SetupTaskAssigneeInput{UserID: conv.PtrEmpty("user_outside_org")}},
		{TaskKey: "instrument-agents", Assignee: &gen.SetupTaskAssigneeInput{Email: conv.PtrEmpty("not-an-email")}},
	}
	for _, payload := range tests {
		result, err := ti.service.UpdateSetupTask(ctx, payload)
		require.Nil(t, result)
		var oopsErr *oops.ShareableError
		require.ErrorAs(t, err, &oopsErr)
		require.Equal(t, oops.CodeBadRequest, oopsErr.Code)
	}
}

func TestService_UpdateSetupTaskEnforcesFieldAuthorization(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestOrganizationsService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	_, err := ti.service.UpdateSetupTask(ctx, &gen.UpdateSetupTaskPayload{
		TaskKey: "instrument-agents", Assignee: &gen.SetupTaskAssigneeInput{UserID: &authCtx.UserID, Email: nil},
	})
	require.NoError(t, err)

	readOnlyCtx := authztest.WithExactGrants(t, ctx, authz.NewGrant(authz.ScopeOrgRead, authCtx.ActiveOrganizationID))
	done := "done"
	result, err := ti.service.UpdateSetupTask(readOnlyCtx, &gen.UpdateSetupTaskPayload{TaskKey: "instrument-agents", Status: &done})
	require.NoError(t, err)
	require.Equal(t, "done", result.Status)

	otherEmail := "owner@example.test"
	_, err = ti.service.UpdateSetupTask(ctx, &gen.UpdateSetupTaskPayload{
		TaskKey: "configure-policies", Assignee: &gen.SetupTaskAssigneeInput{UserID: nil, Email: &otherEmail},
	})
	require.NoError(t, err)
	result, err = ti.service.UpdateSetupTask(readOnlyCtx, &gen.UpdateSetupTaskPayload{TaskKey: "configure-policies", Status: &done})
	require.Nil(t, result)
	requireOopsCode(t, err, oops.CodeForbidden)

	result, err = ti.service.UpdateSetupTask(readOnlyCtx, &gen.UpdateSetupTaskPayload{
		TaskKey: "confirm-traffic", Assignee: &gen.SetupTaskAssigneeInput{UserID: &authCtx.UserID, Email: nil},
	})
	require.Nil(t, result)
	requireOopsCode(t, err, oops.CodeForbidden)
}

func TestService_UpdateSetupTaskKeepsBlockedAssignmentsTodoAndRejectsActiveStatuses(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestOrganizationsService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	result, err := ti.service.UpdateSetupTask(ctx, &gen.UpdateSetupTaskPayload{
		TaskKey:  "confirm-traffic",
		Assignee: &gen.SetupTaskAssigneeInput{UserID: &authCtx.UserID},
	})
	require.NoError(t, err)
	require.Equal(t, "todo", result.Status)
	require.Equal(t, []string{"instrument-agents"}, result.BlockedBy)
	require.NotNil(t, result.Assignee)

	for _, status := range []string{"in_progress", "awaiting_support", "done"} {
		result, err := ti.service.UpdateSetupTask(ctx, &gen.UpdateSetupTaskPayload{TaskKey: "confirm-traffic", Status: &status})
		require.Nil(t, result)
		requireOopsCode(t, err, oops.CodeBadRequest)
	}

	todo := "todo"
	result, err = ti.service.UpdateSetupTask(ctx, &gen.UpdateSetupTaskPayload{TaskKey: "confirm-traffic", Status: &todo})
	require.NoError(t, err)
	require.Equal(t, "todo", result.Status)
}

func TestService_UpdateSetupTaskRestrictsHideToPlatformAdmin(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestOrganizationsService(t)
	hidden := true
	result, err := ti.service.UpdateSetupTask(ctx, &gen.UpdateSetupTaskPayload{TaskKey: "platform-mcp", Hidden: &hidden})
	require.Nil(t, result)
	requireOopsCode(t, err, oops.CodeForbidden)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	platformAuth := *authCtx
	platformAuth.IsAdmin = true
	result, err = ti.service.UpdateSetupTask(contextvalues.SetAuthContext(ctx, &platformAuth), &gen.UpdateSetupTaskPayload{TaskKey: "platform-mcp", Hidden: &hidden})
	require.NoError(t, err)
	require.True(t, result.Hidden)
}

func TestService_UpdateSetupTaskWritesBoundedAuditSnapshotsAtomically(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestOrganizationsService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	beforeCount, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionOrganizationSetupTaskUpdated)
	require.NoError(t, err)

	result, err := ti.service.UpdateSetupTask(ctx, &gen.UpdateSetupTaskPayload{
		TaskKey: "instrument-agents", Assignee: &gen.SetupTaskAssigneeInput{UserID: &authCtx.UserID, Email: nil},
	})
	require.NoError(t, err)
	require.Equal(t, "in_progress", result.Status)

	afterCount, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionOrganizationSetupTaskUpdated)
	require.NoError(t, err)
	require.Equal(t, beforeCount+1, afterCount)
	record, err := audittest.LatestAuditLogByAction(ctx, ti.conn, audit.ActionOrganizationSetupTaskUpdated)
	require.NoError(t, err)
	require.Equal(t, authCtx.ActiveOrganizationID, record.SubjectID)
	require.Equal(t, "organization", record.SubjectType)
	metadata, err := audittest.DecodeAuditData(record.Metadata)
	require.NoError(t, err)
	require.Equal(t, "instrument-agents", metadata["task_key"])
	before, err := audittest.DecodeAuditData(record.BeforeSnapshot)
	require.NoError(t, err)
	after, err := audittest.DecodeAuditData(record.AfterSnapshot)
	require.NoError(t, err)
	require.Equal(t, "todo", before["status"])
	require.Equal(t, "in_progress", after["status"])

	stored, err := orgrepo.New(ti.conn).GetOrganizationSetupTask(ctx, orgrepo.GetOrganizationSetupTaskParams{OrganizationID: authCtx.ActiveOrganizationID, TaskKey: "instrument-agents"})
	require.NoError(t, err)
	require.Equal(t, "in_progress", stored.Status)
}

func TestService_UpdateSetupTaskAuditFailureRollsBackTaskAndAudit(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestOrganizationsService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	beforeCount, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionOrganizationSetupTaskUpdated)
	require.NoError(t, err)
	require.NoError(t, audittest.RejectAction(ctx, ti.conn, audit.ActionOrganizationSetupTaskUpdated))

	result, err := ti.service.UpdateSetupTask(ctx, &gen.UpdateSetupTaskPayload{
		TaskKey:  "instrument-agents",
		Assignee: &gen.SetupTaskAssigneeInput{UserID: &authCtx.UserID},
	})
	require.Nil(t, result)
	requireOopsCode(t, err, oops.CodeUnexpected)

	_, err = orgrepo.New(ti.conn).GetOrganizationSetupTask(ctx, orgrepo.GetOrganizationSetupTaskParams{
		OrganizationID: authCtx.ActiveOrganizationID,
		TaskKey:        "instrument-agents",
	})
	require.True(t, errors.Is(err, pgx.ErrNoRows))
	afterCount, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionOrganizationSetupTaskUpdated)
	require.NoError(t, err)
	require.Equal(t, beforeCount, afterCount)
}

func requireOopsCode(t *testing.T, err error, code oops.Code) {
	t.Helper()
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, code, oopsErr.Code)
}
