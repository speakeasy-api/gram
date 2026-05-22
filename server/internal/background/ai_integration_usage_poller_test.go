package background

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/testsuite"

	"github.com/speakeasy-api/gram/server/internal/aiintegrations"
	"github.com/speakeasy-api/gram/server/internal/background/activities"
)

func TestBuildAIUsagePollerWorkflowID(t *testing.T) {
	t.Parallel()

	require.Equal(t, "v1:ai-usage-poller:org_123:cursor", buildAIUsagePollerWorkflowID(aiintegrations.ProviderCursor, "org_123"))
}

func TestAIUsagePollerCadenceAndRetryConfig(t *testing.T) {
	t.Parallel()

	require.Equal(t, 5*time.Minute, aiUsagePollerCoordinatorInterval)
	require.Equal(t, 50*time.Minute, aiUsagePollerActivityTimeout)
	require.Equal(t, 5, aiUsagePollerCoordinatorChildConcurrency)
	require.Equal(t, 3, activities.PollUsageMaxAttempts)
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
			ID:             uuid.MustParse("11111111-1111-1111-1111-111111111111"),
			OrganizationID: "org_a",
			Provider:       aiintegrations.ProviderCursor,
		},
		{
			ID:             uuid.MustParse("22222222-2222-2222-2222-222222222222"),
			OrganizationID: "org_b",
			Provider:       aiintegrations.ProviderCursor,
		},
		{
			ID:             uuid.MustParse("33333333-3333-3333-3333-333333333333"),
			OrganizationID: "org_c",
			Provider:       aiintegrations.ProviderCursor,
		},
		{
			ID:             uuid.MustParse("44444444-4444-4444-4444-444444444444"),
			OrganizationID: "org_d",
			Provider:       aiintegrations.ProviderCursor,
		},
		{
			ID:             uuid.MustParse("55555555-5555-5555-5555-555555555555"),
			OrganizationID: "org_e",
			Provider:       aiintegrations.ProviderCursor,
		},
		{
			ID:             uuid.MustParse("66666666-6666-6666-6666-666666666666"),
			OrganizationID: "org_f",
			Provider:       aiintegrations.ProviderCursor,
		},
		{
			ID:             uuid.MustParse("77777777-7777-7777-7777-777777777777"),
			OrganizationID: "org_g",
			Provider:       aiintegrations.ProviderCursor,
		},
	}

	listCalls := 0
	env.RegisterActivityWithOptions(
		func(_ context.Context, input activities.GetAIIntegrationsCandidatesInput) ([]aiintegrations.UsagePollCandidate, error) {
			listCalls++
			require.Equal(t, aiintegrations.ProviderCursor, input.Provider)
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
		func(_ context.Context, configID string) error {
			synced = append(synced, configID)
			return nil
		},
		activity.RegisterOptions{Name: "SyncAIIntegrationUsage"},
	)

	env.ExecuteWorkflow(AIUsagePollerCoordinatorWorkflow)

	require.True(t, env.IsWorkflowCompleted())
	require.NoError(t, env.GetWorkflowError())
	require.Equal(t, 3, listCalls)
	require.ElementsMatch(t, candidateIDs(candidates), synced)
}

func TestAIUsagePollerCoordinatorWorkflowContinuesAfterChildFailure(t *testing.T) {
	t.Parallel()

	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()
	start := time.Date(2026, 5, 19, 10, 0, 0, 0, time.UTC)
	env.SetStartTime(start)
	env.RegisterWorkflow(AIUsagePollerWorkflow)

	failedCandidate := aiintegrations.UsagePollCandidate{
		ID:             uuid.MustParse("11111111-1111-1111-1111-111111111111"),
		OrganizationID: "org_a",
		Provider:       aiintegrations.ProviderCursor,
	}
	successCandidate := aiintegrations.UsagePollCandidate{
		ID:             uuid.MustParse("22222222-2222-2222-2222-222222222222"),
		OrganizationID: "org_b",
		Provider:       aiintegrations.ProviderCursor,
	}
	nextBatchCandidate := aiintegrations.UsagePollCandidate{
		ID:             uuid.MustParse("33333333-3333-3333-3333-333333333333"),
		OrganizationID: "org_c",
		Provider:       aiintegrations.ProviderCursor,
	}

	listCalls := 0
	env.RegisterActivityWithOptions(
		func(_ context.Context, input activities.GetAIIntegrationsCandidatesInput) ([]aiintegrations.UsagePollCandidate, error) {
			listCalls++
			require.Equal(t, aiintegrations.ProviderCursor, input.Provider)
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
		func(_ context.Context, configID string) error {
			attemptsByConfigID[configID]++
			if configID == failedCandidate.ID.String() {
				return errors.New("cursor API unavailable")
			}
			synced = append(synced, configID)
			return nil
		},
		activity.RegisterOptions{Name: "SyncAIIntegrationUsage"},
	)

	env.ExecuteWorkflow(AIUsagePollerCoordinatorWorkflow)

	require.True(t, env.IsWorkflowCompleted())
	require.NoError(t, env.GetWorkflowError())
	require.Equal(t, 3, listCalls)
	require.Equal(t, activities.PollUsageMaxAttempts, attemptsByConfigID[failedCandidate.ID.String()])
	require.ElementsMatch(t, []string{successCandidate.ID.String(), nextBatchCandidate.ID.String()}, synced)
}

func candidateIDs(candidates []aiintegrations.UsagePollCandidate) []string {
	out := make([]string, 0, len(candidates))
	for _, candidate := range candidates {
		out = append(out, candidate.ID.String())
	}
	return out
}
