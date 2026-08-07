package background

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/testsuite"

	"github.com/speakeasy-api/gram/server/internal/aiintegrations"
	"github.com/speakeasy-api/gram/server/internal/background/activities"
)

func TestBuildAIUsagePollerWorkflowID(t *testing.T) {
	t.Parallel()

	syncID := uuid.MustParse("11111111-1111-1111-1111-111111111111")
	require.Equal(t, "v1:ai-usage-poller:acme:11111111-1111-1111-1111-111111111111", buildAIUsagePollerWorkflowID("acme", syncID))
}

func TestAIUsagePollerCadenceAndRetryConfig(t *testing.T) {
	t.Parallel()

	require.Equal(t, 5*time.Minute, aiUsagePollerCoordinatorInterval)
	require.Equal(t, 8*time.Hour, aiUsagePollerCoordinatorRunTimeout)
	require.Equal(t, 2*time.Hour, aiUsagePollerActivityTimeout)
	require.Equal(t, 12*time.Hour, aiUsagePollerActivityScheduleToCloseTimeout)
	require.Equal(t, 5, aiUsagePollerCoordinatorChildConcurrency)
	require.Equal(t, 5, activities.PollUsageMaxAttempts)
}

func TestAIUsagePollerWorkflowAcceptsSyncIDInput(t *testing.T) {
	t.Parallel()

	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()
	start := time.Date(2026, 7, 16, 20, 0, 0, 0, time.UTC)
	env.SetStartTime(start)

	syncID := uuid.MustParse("22222222-2222-2222-2222-222222222222")
	env.SetStartWorkflowOptions(client.StartWorkflowOptions{
		ID:        buildAIUsagePollerWorkflowID("acme", syncID),
		TaskQueue: "test-task-queue",
	})
	env.RegisterWorkflow(AIUsagePollerWorkflow)

	var actual string
	env.RegisterActivityWithOptions(
		func(_ context.Context, input string) error {
			actual = input
			return nil
		},
		activity.RegisterOptions{Name: "PollAIData"},
	)

	env.ExecuteWorkflow("AIUsagePollerWorkflow", syncID.String())

	require.True(t, env.IsWorkflowCompleted())
	require.NoError(t, env.GetWorkflowError())
	require.Equal(t, syncID.String(), actual)
}

func TestAIUsagePollerCoordinatorWorkflowListsCandidatesAndStartsChildren(t *testing.T) {
	t.Parallel()

	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()
	start := time.Date(2026, 5, 19, 10, 0, 0, 0, time.UTC)
	env.SetStartTime(start)
	env.RegisterWorkflow(AIUsagePollerWorkflow)

	candidates := []aiintegrations.UsagePollCandidate{
		{
			SyncID:           uuid.MustParse("11111111-1111-1111-1111-111111111111"),
			OrganizationID:   "org_a",
			OrganizationSlug: "org-a",
			Provider:         aiintegrations.ProviderCursor,
			Schedule:         aiintegrations.ScheduleCursor,
			Kind:             aiintegrations.SyncKindTime,
		},
		{
			SyncID:           uuid.MustParse("22222222-2222-2222-2222-222222222222"),
			OrganizationID:   "org_b",
			OrganizationSlug: "org-b",
			Provider:         aiintegrations.ProviderCursor,
			Schedule:         aiintegrations.ScheduleCursor,
			Kind:             aiintegrations.SyncKindTime,
		},
		{
			SyncID:           uuid.MustParse("33333333-3333-3333-3333-333333333333"),
			OrganizationID:   "org_c",
			OrganizationSlug: "org-c",
			Provider:         aiintegrations.ProviderCursor,
			Schedule:         aiintegrations.ScheduleCursor,
			Kind:             aiintegrations.SyncKindTime,
		},
		{
			SyncID:           uuid.MustParse("44444444-4444-4444-4444-444444444444"),
			OrganizationID:   "org_d",
			OrganizationSlug: "org-d",
			Provider:         aiintegrations.ProviderCursor,
			Schedule:         aiintegrations.ScheduleCursor,
			Kind:             aiintegrations.SyncKindTime,
		},
		{
			SyncID:           uuid.MustParse("55555555-5555-5555-5555-555555555555"),
			OrganizationID:   "org_e",
			OrganizationSlug: "org-e",
			Provider:         aiintegrations.ProviderCursor,
			Schedule:         aiintegrations.ScheduleCursor,
			Kind:             aiintegrations.SyncKindTime,
		},
		{
			SyncID:           uuid.MustParse("66666666-6666-6666-6666-666666666666"),
			OrganizationID:   "org_f",
			OrganizationSlug: "org-f",
			Provider:         aiintegrations.ProviderCursor,
			Schedule:         aiintegrations.ScheduleCursor,
			Kind:             aiintegrations.SyncKindTime,
		},
		{
			SyncID:           uuid.MustParse("77777777-7777-7777-7777-777777777777"),
			OrganizationID:   "org_g",
			OrganizationSlug: "org-g",
			Provider:         aiintegrations.ProviderCursor,
			Schedule:         aiintegrations.ScheduleCursor,
			Kind:             aiintegrations.SyncKindTime,
		},
	}

	listCalls := 0
	env.RegisterActivityWithOptions(
		func(_ context.Context, input activities.GetAIIntegrationsCandidatesInput) ([]aiintegrations.UsagePollCandidate, error) {
			listCalls++
			require.False(t, input.PollDueBefore.Before(start))
			require.Equal(t, int32(aiUsagePollerCoordinatorChildConcurrency), input.Limit)

			switch listCalls {
			case 1:
				return candidates[:aiUsagePollerCoordinatorChildConcurrency], nil
			case 2:
				return candidates[aiUsagePollerCoordinatorChildConcurrency:], nil
			case 3:
				return nil, nil
			default:
				t.Fatalf("unexpected candidate list call %d", listCalls)
				return nil, nil
			}
		},
		activity.RegisterOptions{Name: "GetAIIntegrationsCandidates"},
	)

	var synced []string
	env.RegisterActivityWithOptions(
		func(_ context.Context, input string) error {
			synced = append(synced, input)
			return nil
		},
		activity.RegisterOptions{Name: "PollAIData"},
	)

	env.ExecuteWorkflow(AIUsagePollerCoordinatorWorkflow)

	require.True(t, env.IsWorkflowCompleted())
	require.NoError(t, env.GetWorkflowError())
	require.Equal(t, 3, listCalls)
	require.ElementsMatch(t, candidateSyncIDs(candidates), synced)
}

func TestAIUsagePollerCoordinatorWorkflowContinuesAfterChildFailure(t *testing.T) {
	t.Parallel()

	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()
	start := time.Date(2026, 5, 19, 10, 0, 0, 0, time.UTC)
	env.SetStartTime(start)
	env.RegisterWorkflow(AIUsagePollerWorkflow)

	failedCandidate := aiintegrations.UsagePollCandidate{
		SyncID:           uuid.MustParse("11111111-1111-1111-1111-111111111111"),
		OrganizationID:   "org_a",
		OrganizationSlug: "org-a",
		Provider:         aiintegrations.ProviderCursor,
		Schedule:         aiintegrations.ScheduleCursor,
		Kind:             aiintegrations.SyncKindTime,
	}
	successCandidate := aiintegrations.UsagePollCandidate{
		SyncID:           uuid.MustParse("22222222-2222-2222-2222-222222222222"),
		OrganizationID:   "org_b",
		OrganizationSlug: "org-b",
		Provider:         aiintegrations.ProviderCursor,
		Schedule:         aiintegrations.ScheduleCursor,
		Kind:             aiintegrations.SyncKindTime,
	}
	nextBatchCandidate := aiintegrations.UsagePollCandidate{
		SyncID:           uuid.MustParse("33333333-3333-3333-3333-333333333333"),
		OrganizationID:   "org_c",
		OrganizationSlug: "org-c",
		Provider:         aiintegrations.ProviderCursor,
		Schedule:         aiintegrations.ScheduleCursor,
		Kind:             aiintegrations.SyncKindTime,
	}

	listCalls := 0
	env.RegisterActivityWithOptions(
		func(_ context.Context, input activities.GetAIIntegrationsCandidatesInput) ([]aiintegrations.UsagePollCandidate, error) {
			listCalls++
			require.False(t, input.PollDueBefore.Before(start))
			require.Equal(t, int32(aiUsagePollerCoordinatorChildConcurrency), input.Limit)

			switch listCalls {
			case 1:
				return []aiintegrations.UsagePollCandidate{failedCandidate, successCandidate}, nil
			case 2:
				return []aiintegrations.UsagePollCandidate{nextBatchCandidate}, nil
			case 3:
				return nil, nil
			default:
				t.Fatalf("unexpected candidate list call %d", listCalls)
				return nil, nil
			}
		},
		activity.RegisterOptions{Name: "GetAIIntegrationsCandidates"},
	)

	attemptsByConfigID := map[string]int{}
	var synced []string
	env.RegisterActivityWithOptions(
		func(_ context.Context, input string) error {
			attemptsByConfigID[input]++
			if input == failedCandidate.SyncID.String() {
				return errors.New("cursor API unavailable")
			}
			synced = append(synced, input)
			return nil
		},
		activity.RegisterOptions{Name: "PollAIData"},
	)

	env.ExecuteWorkflow(AIUsagePollerCoordinatorWorkflow)

	require.True(t, env.IsWorkflowCompleted())
	require.NoError(t, env.GetWorkflowError())
	require.Equal(t, 3, listCalls)
	require.Equal(t, activities.PollUsageMaxAttempts, attemptsByConfigID[failedCandidate.SyncID.String()])
	require.ElementsMatch(t, []string{successCandidate.SyncID.String(), nextBatchCandidate.SyncID.String()}, synced)
}

func TestAIUsagePollerCoordinatorStartsIndependentWorkflowsForConfigSchedules(t *testing.T) {
	t.Parallel()

	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()
	start := time.Date(2026, 5, 19, 10, 0, 0, 0, time.UTC)
	env.SetStartTime(start)
	env.RegisterWorkflow(AIUsagePollerWorkflow)

	candidates := []aiintegrations.UsagePollCandidate{
		{
			SyncID:           uuid.MustParse("11111111-1111-1111-1111-111111111111"),
			OrganizationID:   "org_a",
			OrganizationSlug: "org-a",
			Provider:         aiintegrations.ProviderAnthropicCompliance,
			Schedule:         aiintegrations.ScheduleAnthropicAnalyticsUsage,
			Kind:             aiintegrations.SyncKindTime,
		},
		{
			SyncID:           uuid.MustParse("22222222-2222-2222-2222-222222222222"),
			OrganizationID:   "org_a",
			OrganizationSlug: "org-a",
			Provider:         aiintegrations.ProviderAnthropicCompliance,
			Schedule:         aiintegrations.ScheduleAnthropicAnalyticsCost,
			Kind:             aiintegrations.SyncKindTime,
		},
	}

	listCalls := 0
	env.RegisterActivityWithOptions(
		func(_ context.Context, _ activities.GetAIIntegrationsCandidatesInput) ([]aiintegrations.UsagePollCandidate, error) {
			listCalls++
			if listCalls == 1 {
				return candidates, nil
			}
			return nil, nil
		},
		activity.RegisterOptions{Name: "GetAIIntegrationsCandidates"},
	)

	var synced []string
	env.RegisterActivityWithOptions(
		func(_ context.Context, input string) error {
			synced = append(synced, input)
			return nil
		},
		activity.RegisterOptions{Name: "PollAIData"},
	)

	env.ExecuteWorkflow(AIUsagePollerCoordinatorWorkflow)

	require.True(t, env.IsWorkflowCompleted())
	require.NoError(t, env.GetWorkflowError())
	require.ElementsMatch(t, candidateSyncIDs(candidates), synced)
}

func candidateSyncIDs(candidates []aiintegrations.UsagePollCandidate) []string {
	out := make([]string, 0, len(candidates))
	for _, candidate := range candidates {
		out = append(out, candidate.SyncID.String())
	}
	return out
}
