package background

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/speakeasy-api/gram/server/internal/billingnotifications"
	"github.com/stretchr/testify/require"
	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/converter"
	"go.temporal.io/sdk/testsuite"
	"go.temporal.io/sdk/workflow"
)

func trialLifecycleEmailRetryBudget() time.Duration {
	policy := trialLifecycleEmailRetryPolicy()
	budget := trialLifecycleEmailActivityTimeout * time.Duration(policy.MaximumAttempts)
	retryDelay := policy.InitialInterval
	for attempt := int32(1); attempt < policy.MaximumAttempts; attempt++ {
		budget += retryDelay
		retryDelay = min(time.Duration(float64(retryDelay)*policy.BackoffCoefficient), policy.MaximumInterval)
	}
	return budget
}

func TestTrialLifecycleEmailWorkflowRunTimeoutCoversRetryBudget(t *testing.T) {
	t.Parallel()

	require.GreaterOrEqual(t, trialLifecycleEmailWorkflowRunTimeout, trialLifecycleEmailRetryBudget())
}

func TestTrialLifecycleEmailWorkflowDispatchesAdminAdded(t *testing.T) {
	t.Parallel()

	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()
	var received TrialLifecycleEmailInput
	env.RegisterActivityWithOptions(
		func(_ context.Context, input TrialLifecycleEmailInput) error {
			received = input
			return nil
		},
		activity.RegisterOptions{Name: "SendTrialLifecycleEmail"},
	)

	input := TrialLifecycleEmailInput{
		Kind:           AdminAddedEmailKind,
		OrganizationID: "<ORGANIZATION_ID>",
		UserID:         "<USER_ID>",
		ReminderOnly:   false,
	}
	env.ExecuteWorkflow(TrialLifecycleEmailWorkflow, input)

	require.True(t, env.IsWorkflowCompleted())
	require.NoError(t, env.GetWorkflowError())
	require.Equal(t, input, received)
}

func TestTrialLifecycleEmailWorkflowRetriesActivity(t *testing.T) {
	t.Parallel()

	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()
	var attempts atomic.Int32
	env.RegisterActivityWithOptions(
		func(context.Context, TrialLifecycleEmailInput) error {
			if attempts.Add(1) < 3 {
				return errors.New("temporary Loops failure")
			}
			return nil
		},
		activity.RegisterOptions{Name: "SendTrialLifecycleEmail"},
	)

	env.ExecuteWorkflow(TrialLifecycleEmailWorkflow, TrialLifecycleEmailInput{
		Kind:           AdminAddedEmailKind,
		OrganizationID: "<ORGANIZATION_ID>",
		UserID:         "",
		ReminderOnly:   false,
	})

	require.True(t, env.IsWorkflowCompleted())
	require.NoError(t, env.GetWorkflowError())
	require.Equal(t, int32(3), attempts.Load())
}

func TestTrialLifecycleEmailWorkflowKeepsReminderAfterLegacySyncFailure(t *testing.T) {
	t.Parallel()

	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()
	var attempts atomic.Int32
	env.RegisterActivityWithOptions(
		func(context.Context, TrialLifecycleEmailInput) error {
			attempts.Add(1)
			return errors.New("legacy workflow unavailable")
		},
		activity.RegisterOptions{Name: "SendTrialLifecycleEmail"},
	)
	env.RegisterActivityWithOptions(
		func(context.Context, string) (billingnotifications.TrialReminderState, error) {
			return billingnotifications.TrialReminderState{Active: false, TrialCreatedAt: time.Time{}, TrialEndsAt: time.Time{}, SendAt: time.Time{}}, nil
		},
		activity.RegisterOptions{Name: "ResolveTrialEndingReminder"},
	)

	env.ExecuteWorkflow(TrialLifecycleEmailWorkflow, TrialLifecycleEmailInput{Kind: TrialStartedEmailKind, OrganizationID: "<ORGANIZATION_ID>", UserID: "", ReminderOnly: false})

	require.NoError(t, env.GetWorkflowError())
	require.Equal(t, trialLifecycleEmailRetryMaximumAttempts, attempts.Load())
}

func TestTrialLifecycleEmailWorkflowWaitsUntilThreeDaysBeforeEnd(t *testing.T) {
	t.Parallel()

	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()
	now := env.Now().UTC()
	trialCreatedAt := now.Add(-24 * time.Hour)
	trialEndsAt := now.Add(7 * 24 * time.Hour)
	var sentAt time.Time

	env.RegisterActivityWithOptions(
		func(context.Context, TrialLifecycleEmailInput) error { return nil },
		activity.RegisterOptions{Name: "SendTrialLifecycleEmail"},
	)
	env.RegisterActivityWithOptions(
		func(context.Context, string) (billingnotifications.TrialReminderState, error) {
			return billingnotifications.TrialReminderState{
				Active:         true,
				TrialCreatedAt: trialCreatedAt,
				TrialEndsAt:    trialEndsAt,
				SendAt:         trialEndsAt.Add(-72 * time.Hour),
			}, nil
		},
		activity.RegisterOptions{Name: "ResolveTrialEndingReminder"},
	)
	env.RegisterActivityWithOptions(
		func(_ context.Context, input billingnotifications.SendTrialEndingSoonInput) (billingnotifications.SendTrialEndingSoonResult, error) {
			sentAt = env.Now()
			require.Equal(t, trialCreatedAt, input.TrialCreatedAt)
			require.Equal(t, trialEndsAt, input.TrialEndsAt)
			return billingnotifications.SendTrialEndingSoonResult{}, nil
		},
		activity.RegisterOptions{Name: "SendTrialEndingSoonEmail"},
	)

	env.ExecuteWorkflow(TrialLifecycleEmailWorkflow, TrialLifecycleEmailInput{
		Kind:           TrialStartedEmailKind,
		OrganizationID: "<ORGANIZATION_ID>",
		UserID:         "",
		ReminderOnly:   false,
	})

	require.True(t, env.IsWorkflowCompleted())
	require.NoError(t, env.GetWorkflowError())
	require.True(t, trialEndsAt.Add(-72*time.Hour).Equal(sentAt))
}

func TestTrialLifecycleEmailWorkflowRetimesExtendedTrial(t *testing.T) {
	t.Parallel()

	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()
	now := env.Now().UTC()
	createdAt := now.Add(-24 * time.Hour)
	firstEnd := now.Add(4 * 24 * time.Hour)
	extendedEnd := now.Add(8 * 24 * time.Hour)
	var sends atomic.Int32

	env.RegisterActivityWithOptions(
		func(context.Context, TrialLifecycleEmailInput) error { return nil },
		activity.RegisterOptions{Name: "SendTrialLifecycleEmail"},
	)
	env.RegisterActivityWithOptions(
		func(context.Context, string) (billingnotifications.TrialReminderState, error) {
			return billingnotifications.TrialReminderState{
				Active:         true,
				TrialCreatedAt: createdAt,
				TrialEndsAt:    firstEnd,
				SendAt:         firstEnd.Add(-72 * time.Hour),
			}, nil
		},
		activity.RegisterOptions{Name: "ResolveTrialEndingReminder"},
	)
	env.RegisterActivityWithOptions(
		func(context.Context, billingnotifications.SendTrialEndingSoonInput) (billingnotifications.SendTrialEndingSoonResult, error) {
			sends.Add(1)
			return billingnotifications.SendTrialEndingSoonResult{Reschedule: true}, nil
		},
		activity.RegisterOptions{Name: "SendTrialEndingSoonEmail"},
	)

	env.ExecuteWorkflow(TrialLifecycleEmailWorkflow, TrialLifecycleEmailInput{Kind: TrialStartedEmailKind, OrganizationID: "<ORGANIZATION_ID>", UserID: "", ReminderOnly: false})

	var continueErr *workflow.ContinueAsNewError
	require.ErrorAs(t, env.GetWorkflowError(), &continueErr)
	var nextInput TrialLifecycleEmailInput
	require.NoError(t, converter.GetDefaultDataConverter().FromPayloads(continueErr.Input, &nextInput))
	require.True(t, nextInput.ReminderOnly)
	require.Equal(t, int32(1), sends.Load())

	env = suite.NewTestWorkflowEnvironment()
	env.RegisterActivityWithOptions(
		func(context.Context, string) (billingnotifications.TrialReminderState, error) {
			return billingnotifications.TrialReminderState{
				Active:         true,
				TrialCreatedAt: createdAt,
				TrialEndsAt:    extendedEnd,
				SendAt:         extendedEnd.Add(-72 * time.Hour),
			}, nil
		},
		activity.RegisterOptions{Name: "ResolveTrialEndingReminder"},
	)
	env.RegisterActivityWithOptions(
		func(context.Context, billingnotifications.SendTrialEndingSoonInput) (billingnotifications.SendTrialEndingSoonResult, error) {
			sends.Add(1)
			return billingnotifications.SendTrialEndingSoonResult{}, nil
		},
		activity.RegisterOptions{Name: "SendTrialEndingSoonEmail"},
	)
	env.ExecuteWorkflow(TrialLifecycleEmailWorkflow, nextInput)
	require.NoError(t, env.GetWorkflowError())
	require.Equal(t, int32(2), sends.Load())
}

func TestTrialLifecycleEmailWorkflowChunksLongReminderTimer(t *testing.T) {
	t.Parallel()

	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()
	now := env.Now().UTC()
	trialEndsAt := now.Add(90 * 24 * time.Hour)
	var lifecycleCalls atomic.Int32
	env.RegisterActivityWithOptions(
		func(context.Context, TrialLifecycleEmailInput) error {
			lifecycleCalls.Add(1)
			return nil
		},
		activity.RegisterOptions{Name: "SendTrialLifecycleEmail"},
	)
	env.RegisterActivityWithOptions(
		func(context.Context, string) (billingnotifications.TrialReminderState, error) {
			return billingnotifications.TrialReminderState{
				Active:         true,
				TrialCreatedAt: now,
				TrialEndsAt:    trialEndsAt,
				SendAt:         trialEndsAt.Add(-72 * time.Hour),
			}, nil
		},
		activity.RegisterOptions{Name: "ResolveTrialEndingReminder"},
	)

	env.ExecuteWorkflow(TrialLifecycleEmailWorkflow, TrialLifecycleEmailInput{Kind: TrialStartedEmailKind, OrganizationID: "<ORGANIZATION_ID>", UserID: "", ReminderOnly: false})

	var continueErr *workflow.ContinueAsNewError
	require.ErrorAs(t, env.GetWorkflowError(), &continueErr)
	var nextInput TrialLifecycleEmailInput
	require.NoError(t, converter.GetDefaultDataConverter().FromPayloads(continueErr.Input, &nextInput))
	require.True(t, nextInput.ReminderOnly)
	require.Equal(t, int32(1), lifecycleCalls.Load())
	require.True(t, now.Add(trialReminderTimerChunk).Equal(env.Now()))
}
