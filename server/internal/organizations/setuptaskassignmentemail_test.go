package organizations_test

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	gen "github.com/speakeasy-api/gram/server/gen/organizations"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/loops"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func TestService_UpdateSetupTaskAssignmentEmailIsDetachedNonblockingAndBounded(t *testing.T) {
	t.Parallel()

	parentCtx, ti := newTestOrganizationsServiceWithEmail(t)
	authCtx, ok := contextvalues.GetAuthContext(parentCtx)
	require.True(t, ok)

	ctx, cancel := context.WithCancel(parentCtx)
	deliveryStarted := make(chan context.Context, 1)
	releaseDelivery := make(chan struct{})
	var releaseOnce sync.Once
	release := func() { releaseOnce.Do(func() { close(releaseDelivery) }) }
	t.Cleanup(release)
	ti.loops.On("SendTransactional", mock.Anything, mock.Anything).Run(func(args mock.Arguments) {
		deliveryCtx, ok := args.Get(0).(context.Context)
		if !ok {
			t.Errorf("expected delivery context, got %T", args.Get(0))
			return
		}
		deliveryStarted <- deliveryCtx
		<-releaseDelivery
	}).Return(nil).Once()

	type updateResult struct {
		task *gen.SetupTask
		err  error
	}
	updateDone := make(chan updateResult, 1)
	go func() {
		result, err := ti.service.UpdateSetupTask(ctx, &gen.UpdateSetupTaskPayload{
			TaskKey:  "configure-policies",
			Assignee: &gen.SetupTaskAssigneeInput{UserID: &authCtx.UserID},
		})
		updateDone <- updateResult{task: result, err: err}
	}()

	select {
	case result := <-updateDone:
		require.NoError(t, result.err)
		require.NotNil(t, result.task)
	case <-time.After(time.Second):
		t.Fatal("setup task update waited for assignment email delivery")
	}
	cancel()

	var deliveryCtx context.Context
	select {
	case deliveryCtx = <-deliveryStarted:
	case <-time.After(time.Second):
		t.Fatal("assignment email did not start")
	}
	require.NoError(t, deliveryCtx.Err(), "delivery context must survive request cancellation")
	deadline, ok := deliveryCtx.Deadline()
	require.True(t, ok, "delivery context must be bounded")
	require.WithinDuration(t, time.Now().Add(10*time.Second), deadline, time.Second)
	release()
}

func TestService_UpdateSetupTaskSendsAssignmentEmailAfterCommit(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestOrganizationsServiceWithEmail(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.Email)

	sent := make(chan struct{})
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
	})).Run(func(mock.Arguments) { close(sent) }).Return(nil).Once()

	result, err := ti.service.UpdateSetupTask(ctx, &gen.UpdateSetupTaskPayload{
		TaskKey:  "configure-policies",
		Assignee: &gen.SetupTaskAssigneeInput{UserID: &authCtx.UserID},
	})
	require.NoError(t, err)
	require.Equal(t, authCtx.UserID, *result.Assignee.UserID)
	waitForAssignmentEmail(t, sent)
}

func TestService_UpdateSetupTaskSkipsNonAssignmentNotifications(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestOrganizationsServiceWithEmail(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	sent := make(chan struct{})
	ti.loops.On("SendTransactional", mock.Anything, mock.Anything).Run(func(mock.Arguments) { close(sent) }).Return(nil).Once()

	assignment := &gen.UpdateSetupTaskPayload{TaskKey: "configure-policies", Assignee: &gen.SetupTaskAssigneeInput{UserID: &authCtx.UserID}}
	_, err := ti.service.UpdateSetupTask(ctx, assignment)
	require.NoError(t, err)
	waitForAssignmentEmail(t, sent)
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
	sent := make(chan struct{})
	ti.loops.On("SendTransactional", mock.Anything, mock.Anything).Run(func(mock.Arguments) { close(sent) }).Return(errors.New("delivery unavailable")).Once()

	result, err := ti.service.UpdateSetupTask(ctx, &gen.UpdateSetupTaskPayload{
		TaskKey:  "configure-policies",
		Assignee: &gen.SetupTaskAssigneeInput{UserID: &authCtx.UserID},
	})
	require.NoError(t, err)
	require.NotNil(t, result)
	waitForAssignmentEmail(t, sent)
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

func waitForAssignmentEmail(t *testing.T, sent <-chan struct{}) {
	t.Helper()
	select {
	case <-sent:
	case <-time.After(time.Second):
		t.Fatal("assignment email was not sent")
	}
}
