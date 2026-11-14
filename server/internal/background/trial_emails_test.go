package background

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.temporal.io/sdk/activity"
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

func TestTrialLifecycleEmailWorkflowFailsWhenTrialStartedSyncFails(t *testing.T) {
	t.Parallel()

	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()
	var attempts atomic.Int32
	env.RegisterActivityWithOptions(
		func(context.Context, TrialLifecycleEmailInput) error {
			attempts.Add(1)
			return errors.New("Loops unavailable")
		},
		activity.RegisterOptions{Name: "SendTrialLifecycleEmail"},
	)

	env.ExecuteWorkflow(TrialLifecycleEmailWorkflow, TrialLifecycleEmailInput{Kind: TrialStartedEmailKind, OrganizationID: "<ORGANIZATION_ID>", UserID: "", ReminderOnly: false})

	require.True(t, env.IsWorkflowCompleted())
	require.Error(t, env.GetWorkflowError())
	require.Equal(t, trialLifecycleEmailRetryMaximumAttempts, attempts.Load())
}

// The trial reminders come from the Loops workflow that trial_started enters,
// so a trial-started run sends that event and ends. Only SendTrialLifecycleEmail
// is registered: scheduling a retired reminder activity fails the test.
func TestTrialLifecycleEmailWorkflowTrialStartedSendsOnlyLifecycleEvent(t *testing.T) {
	t.Parallel()

	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()
	startedAt := env.Now()
	var received TrialLifecycleEmailInput
	env.RegisterActivityWithOptions(
		func(_ context.Context, input TrialLifecycleEmailInput) error {
			received = input
			return nil
		},
		activity.RegisterOptions{Name: "SendTrialLifecycleEmail"},
	)

	input := TrialLifecycleEmailInput{Kind: TrialStartedEmailKind, OrganizationID: "<ORGANIZATION_ID>", UserID: "", ReminderOnly: false}
	env.ExecuteWorkflow(TrialLifecycleEmailWorkflow, input)

	require.True(t, env.IsWorkflowCompleted())
	require.NoError(t, env.GetWorkflowError())
	require.Equal(t, input, received)
	require.True(t, startedAt.Equal(env.Now()), "the run must not wait for a reminder")
}

// A reminder-only run continues a run that started before the reminder was
// retired. It has nothing left to send. No activity is registered.
func TestTrialLifecycleEmailWorkflowReminderOnlyContinuationEnds(t *testing.T) {
	t.Parallel()

	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()

	env.ExecuteWorkflow(TrialLifecycleEmailWorkflow, TrialLifecycleEmailInput{Kind: TrialStartedEmailKind, OrganizationID: "<ORGANIZATION_ID>", UserID: "", ReminderOnly: true})

	require.True(t, env.IsWorkflowCompleted())
	require.NoError(t, env.GetWorkflowError())
}

// A run started before the reminder was retired replays the legacy activity
// sequence. When its reminder timer fires, the retired send activity runs and
// sends nothing.
func TestLegacyTrialLifecycleEmailRunSendsNothingWhenReminderTimerFires(t *testing.T) {
	t.Parallel()

	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()
	env.OnGetVersion(trialEndingSoonRetiredChangeID, workflow.DefaultVersion, trialEndingSoonRetiredVersion).Return(workflow.DefaultVersion)
	now := env.Now().UTC()
	trialEndsAt := now.Add(7 * 24 * time.Hour)
	var lifecycleCalls atomic.Int32

	env.RegisterActivityWithOptions(
		func(context.Context, TrialLifecycleEmailInput) error {
			lifecycleCalls.Add(1)
			return nil
		},
		activity.RegisterOptions{Name: "SendTrialLifecycleEmail"},
	)
	// The reminder state a pre-change run recorded before it started waiting.
	env.RegisterActivityWithOptions(
		func(context.Context, string) (legacyTrialEndingReminderState, error) {
			return legacyTrialEndingReminderState{
				Active:         true,
				TrialCreatedAt: now.Add(-24 * time.Hour),
				TrialEndsAt:    trialEndsAt,
				SendAt:         trialEndsAt.Add(-72 * time.Hour),
			}, nil
		},
		activity.RegisterOptions{Name: "ResolveTrialEndingReminder"},
	)
	a := &Activities{}
	env.RegisterActivity(a.SendTrialEndingSoonEmail)

	env.ExecuteWorkflow(TrialLifecycleEmailWorkflow, TrialLifecycleEmailInput{Kind: TrialStartedEmailKind, OrganizationID: "<ORGANIZATION_ID>", UserID: "", ReminderOnly: false})

	require.True(t, env.IsWorkflowCompleted())
	require.NoError(t, env.GetWorkflowError())
	require.Equal(t, int32(1), lifecycleCalls.Load())
	require.True(t, trialEndsAt.Add(-72*time.Hour).Equal(env.Now()))
}

func TestRetiredTrialEndingReminderActivitiesSendNothing(t *testing.T) {
	t.Parallel()

	a := &Activities{}
	state, err := a.ResolveTrialEndingReminder(t.Context(), "<ORGANIZATION_ID>")
	require.NoError(t, err)
	require.False(t, state.Active)
	result, err := a.SendTrialEndingSoonEmail(t.Context(), legacyTrialEndingSoonInput{OrganizationID: "<ORGANIZATION_ID>", TrialCreatedAt: time.Time{}, TrialEndsAt: time.Time{}})
	require.NoError(t, err)
	require.False(t, result.Reschedule)
}

func TestTrialLifecycleEmailActivitiesExcludeLocalFixtures(t *testing.T) {
	t.Parallel()
	for _, gateErr := range []error{nil, errors.New("fixture projection failed")} {
		a := &Activities{trialFixtureHandler: func(context.Context, string) (bool, error) { return true, gateErr }}
		ctx := t.Context()
		for _, kind := range []TrialLifecycleEmailKind{TrialStartedEmailKind, AdminAddedEmailKind, TrialInactiveEmailKind} {
			err := a.SendTrialLifecycleEmail(ctx, TrialLifecycleEmailInput{Kind: kind, OrganizationID: "org_fixture"})
			require.ErrorIs(t, err, gateErr)
		}
	}
}

func TestTrialLifecycleEmailActivitiesContinueForNonFixtures(t *testing.T) {
	t.Parallel()
	calls := 0
	a := &Activities{trialFixtureHandler: func(_ context.Context, orgID string) (bool, error) {
		require.Equal(t, "org_non_fixture", orgID)
		calls++
		return false, nil
	}}
	for _, kind := range []TrialLifecycleEmailKind{TrialStartedEmailKind, AdminAddedEmailKind, TrialInactiveEmailKind} {
		err := a.SendTrialLifecycleEmail(t.Context(), TrialLifecycleEmailInput{Kind: kind, OrganizationID: "org_non_fixture"})
		require.EqualError(t, err, "trial email service is not configured")
	}
	require.Equal(t, 3, calls)
}
