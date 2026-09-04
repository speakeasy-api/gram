package organizations_test

import (
	"errors"
	"strings"
	"testing"

	gen "github.com/speakeasy-api/gram/server/gen/organizations"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/loops"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func TestService_UpdateSetupTaskSendsAssignmentEmailAfterCommit(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestOrganizationsServiceWithEmail(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.Email)

	ti.loops.On("SendTransactional", mock.Anything, mock.MatchedBy(func(input loops.SendTransactionalInput) bool {
		return input.TransactionalID == "setup-task-assignment-test-id" &&
			input.Email == *authCtx.Email &&
			input.DataVariables["task_title"] == "Configure policies" &&
			input.DataVariables["task_description"] == "Choose the organization's initial risk policies." &&
			input.DataVariables["assigner_name"] != "" &&
			input.DataVariables["organization_name"] != "" &&
			strings.HasSuffix(input.DataVariables["setup_link"], "/setup?step=configure-policies") &&
			strings.HasPrefix(input.IdempotencyKey, "setup-task-assignment:") &&
			len(input.IdempotencyKey) <= 100 &&
			!input.AddToAudience
	})).Return(nil).Once()

	result, err := ti.service.UpdateSetupTask(ctx, &gen.UpdateSetupTaskPayload{
		TaskKey:  "configure-policies",
		Assignee: &gen.SetupTaskAssigneeInput{UserID: &authCtx.UserID},
	})
	require.NoError(t, err)
	require.Equal(t, authCtx.UserID, *result.Assignee.UserID)
}

func TestService_UpdateSetupTaskSkipsNonAssignmentNotifications(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestOrganizationsServiceWithEmail(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	ti.loops.On("SendTransactional", mock.Anything, mock.Anything).Return(nil).Once()

	assignment := &gen.UpdateSetupTaskPayload{TaskKey: "configure-policies", Assignee: &gen.SetupTaskAssigneeInput{UserID: &authCtx.UserID}}
	_, err := ti.service.UpdateSetupTask(ctx, assignment)
	require.NoError(t, err)
	_, err = ti.service.UpdateSetupTask(ctx, assignment)
	require.NoError(t, err)
	done := "done"
	_, err = ti.service.UpdateSetupTask(ctx, &gen.UpdateSetupTaskPayload{TaskKey: "configure-policies", Status: &done})
	require.NoError(t, err)
	clearAssignee := true
	_, err = ti.service.UpdateSetupTask(ctx, &gen.UpdateSetupTaskPayload{TaskKey: "configure-policies", ClearAssignee: &clearAssignee})
	require.NoError(t, err)
}

func TestService_UpdateSetupTaskAssignmentEmailIsBestEffort(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestOrganizationsServiceWithEmail(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	ti.loops.On("SendTransactional", mock.Anything, mock.Anything).Return(errors.New("delivery unavailable")).Once()

	result, err := ti.service.UpdateSetupTask(ctx, &gen.UpdateSetupTaskPayload{
		TaskKey:  "configure-policies",
		Assignee: &gen.SetupTaskAssigneeInput{UserID: &authCtx.UserID},
	})
	require.NoError(t, err)
	require.NotNil(t, result)
}

func TestService_UpdateSetupTaskSkipsAssignmentEmailWhenDisabledOrTransactionFails(t *testing.T) {
	t.Parallel()

	t.Run("disabled", func(t *testing.T) {
		t.Parallel()
		ctx, ti := newTestOrganizationsServiceWithEmailEnabled(t, false)
		authCtx, ok := contextvalues.GetAuthContext(ctx)
		require.True(t, ok)
		_, err := ti.service.UpdateSetupTask(ctx, &gen.UpdateSetupTaskPayload{
			TaskKey:  "configure-policies",
			Assignee: &gen.SetupTaskAssigneeInput{UserID: &authCtx.UserID},
		})
		require.NoError(t, err)
	})

	t.Run("failed transaction", func(t *testing.T) {
		t.Parallel()
		ctx, ti := newTestOrganizationsServiceWithEmail(t)
		authCtx, ok := contextvalues.GetAuthContext(ctx)
		require.True(t, ok)
		require.NoError(t, audittest.RejectAction(ctx, ti.conn, audit.ActionOrganizationSetupTaskUpdated))
		result, err := ti.service.UpdateSetupTask(ctx, &gen.UpdateSetupTaskPayload{
			TaskKey:  "configure-policies",
			Assignee: &gen.SetupTaskAssigneeInput{UserID: &authCtx.UserID},
		})
		require.Error(t, err)
		require.Nil(t, result)
	})
}
