package background

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/testsuite"

	"github.com/speakeasy-api/gram/server/internal/aiintegrations"
	"github.com/speakeasy-api/gram/server/internal/background/activities"
)

func TestAIIntegrationUsageSyncConfigWorkflowID(t *testing.T) {
	t.Parallel()

	require.Equal(t, "v1:ai-integration-usage-sync-config:cursor:org_123", aiIntegrationUsageSyncConfigWorkflowID(aiintegrations.ProviderCursor, "org_123"))
}

func TestAIIntegrationUsageSyncChildRuntimeStaysBelowHourlyInterval(t *testing.T) {
	t.Parallel()

	require.Equal(t, 50*time.Minute, aiIntegrationUsageSyncChildRuntime)
	require.Less(t, aiIntegrationUsageSyncChildRuntime, aiIntegrationUsageSyncInterval)
}

func TestAIIntegrationUsageSyncWorkflowListsCandidatesAndStartsChildren(t *testing.T) {
	t.Parallel()

	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()
	start := time.Date(2026, 5, 19, 10, 0, 0, 0, time.UTC)
	lastPolledAt := start.Add(-time.Hour)
	env.SetStartTime(start)
	env.RegisterWorkflow(AIIntegrationUsageSyncConfigWorkflow)

	configs := []activities.AIIntegrationUsagePollConfig{
		{
			ID:             "11111111-1111-1111-1111-111111111111",
			OrganizationID: "org_a",
			Provider:       aiintegrations.ProviderCursor,
			ProjectID:      "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa",
			APIKey:         "key-a",
			LastPolledAt:   lastPolledAt,
		},
		{
			ID:             "22222222-2222-2222-2222-222222222222",
			OrganizationID: "org_b",
			Provider:       aiintegrations.ProviderCursor,
			ProjectID:      "bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb",
			APIKey:         "key-b",
			LastPolledAt:   lastPolledAt.Add(time.Minute),
		},
	}

	listCalls := 0
	env.RegisterActivityWithOptions(
		func(_ context.Context, input activities.ListAIIntegrationUsagePollCandidatesInput) ([]activities.AIIntegrationUsagePollConfig, error) {
			listCalls++
			require.Equal(t, aiintegrations.ProviderCursor, input.Provider)
			require.Equal(t, start, input.EndTime)
			require.Equal(t, int32(aiIntegrationUsageSyncCandidateBatchSize), input.Limit)

			switch listCalls {
			case 1:
				require.Nil(t, input.Cursor)
				return configs, nil
			case 2:
				require.NotNil(t, input.Cursor)
				require.Equal(t, configs[1].LastPolledAt, input.Cursor.LastPolledAt)
				require.Equal(t, configs[1].OrganizationID, input.Cursor.OrganizationID)
				require.Equal(t, configs[1].Provider, input.Cursor.Provider)
				return nil, nil
			default:
				t.Fatalf("unexpected candidate list call %d", listCalls)
				return nil, nil
			}
		},
		activity.RegisterOptions{Name: "ListAIIntegrationUsagePollCandidates"},
	)

	var synced []activities.AIIntegrationUsagePollConfig
	env.RegisterActivityWithOptions(
		func(_ context.Context, input activities.SyncAIIntegrationUsageInput) error {
			require.Equal(t, start, input.EndTime)
			synced = append(synced, input.Config)
			return nil
		},
		activity.RegisterOptions{Name: "SyncAIIntegrationUsage"},
	)

	env.ExecuteWorkflow(AIIntegrationUsageSyncWorkflow)

	require.True(t, env.IsWorkflowCompleted())
	require.NoError(t, env.GetWorkflowError())
	require.Equal(t, 2, listCalls)
	require.ElementsMatch(t, configs, synced)
}
