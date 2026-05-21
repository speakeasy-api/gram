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

func TestBuildUsageSyncConfigWorkflowID(t *testing.T) {
	t.Parallel()

	require.Equal(t, "v1:ai-integration-usage-sync-config:cursor:org_123", buildUsageSyncConfigWorkflowID(aiintegrations.ProviderCursor, "org_123"))
}

func TestAIIntegrationUsageSyncCadenceAndRetryConfig(t *testing.T) {
	t.Parallel()

	require.Equal(t, 5*time.Minute, aiIntegrationUsageSyncInterval)
	require.Equal(t, 50*time.Minute, aiIntegrationUsageSyncPollActivityTimeout)
	require.Equal(t, 5, aiIntegrationUsageSyncChildConcurrency)
	require.Equal(t, 3, activities.SyncAIIntegrationUsageMaxAttempts)
}

func TestAIIntegrationUsageSyncWorkflowListsCandidatesAndStartsChildren(t *testing.T) {
	t.Parallel()

	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()
	start := time.Date(2026, 5, 19, 10, 0, 0, 0, time.UTC)
	env.SetStartTime(start)
	env.RegisterWorkflow(AIIntegrationUsageSyncConfigWorkflow)

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
			require.Equal(t, start, input.PollDueBefore)
			require.Equal(t, int32(aiIntegrationUsageSyncChildConcurrency), input.Limit)

			switch listCalls {
			case 1:
				return candidates[:aiIntegrationUsageSyncChildConcurrency], nil
			case 2:
				return candidates[aiIntegrationUsageSyncChildConcurrency:], nil
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
		func(_ context.Context, input activities.SyncAIIntegrationUsageInput) error {
			require.Equal(t, start, input.EndTime)
			synced = append(synced, input.ConfigID)
			return nil
		},
		activity.RegisterOptions{Name: "SyncAIIntegrationUsage"},
	)

	env.ExecuteWorkflow(AIIntegrationUsageSyncWorkflow)

	require.True(t, env.IsWorkflowCompleted())
	require.NoError(t, env.GetWorkflowError())
	require.Equal(t, 3, listCalls)
	require.ElementsMatch(t, candidateIDs(candidates), synced)
}

func TestAIIntegrationUsageSyncWorkflowContinuesAfterChildFailure(t *testing.T) {
	t.Parallel()

	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()
	start := time.Date(2026, 5, 19, 10, 0, 0, 0, time.UTC)
	env.SetStartTime(start)
	env.RegisterWorkflow(AIIntegrationUsageSyncConfigWorkflow)

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
			require.Equal(t, start, input.PollDueBefore)
			require.Equal(t, int32(aiIntegrationUsageSyncChildConcurrency), input.Limit)

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
		func(_ context.Context, input activities.SyncAIIntegrationUsageInput) error {
			require.Equal(t, start, input.EndTime)
			attemptsByConfigID[input.ConfigID]++
			if input.ConfigID == failedCandidate.ID.String() {
				return errors.New("cursor API unavailable")
			}
			synced = append(synced, input.ConfigID)
			return nil
		},
		activity.RegisterOptions{Name: "SyncAIIntegrationUsage"},
	)

	env.ExecuteWorkflow(AIIntegrationUsageSyncWorkflow)

	require.True(t, env.IsWorkflowCompleted())
	require.NoError(t, env.GetWorkflowError())
	require.Equal(t, 3, listCalls)
	require.Equal(t, activities.SyncAIIntegrationUsageMaxAttempts, attemptsByConfigID[failedCandidate.ID.String()])
	require.ElementsMatch(t, []string{successCandidate.ID.String(), nextBatchCandidate.ID.String()}, synced)
}

func candidateIDs(candidates []aiintegrations.UsagePollCandidate) []string {
	out := make([]string, 0, len(candidates))
	for _, candidate := range candidates {
		out = append(out, candidate.ID.String())
	}
	return out
}
