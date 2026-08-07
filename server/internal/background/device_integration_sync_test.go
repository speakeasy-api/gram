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

	"github.com/speakeasy-api/gram/server/internal/background/activities"
	"github.com/speakeasy-api/gram/server/internal/deviceintegrations"
)

func deviceSyncCandidate(id string, org string) deviceintegrations.SyncCandidate {
	return deviceintegrations.SyncCandidate{
		SyncID:           uuid.MustParse(id),
		OrganizationID:   "org_" + org,
		OrganizationSlug: org,
		Provider:         "testmdm",
		Schedule:         "testmdm_inventory",
	}
}

func TestDeviceIntegrationSyncCoordinatorRunsEachCandidateOnce(t *testing.T) {
	t.Parallel()

	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()
	start := time.Date(2026, 7, 24, 10, 0, 0, 0, time.UTC)
	env.SetStartTime(start)
	env.RegisterWorkflow(DeviceIntegrationSyncWorkflow)

	candidates := []deviceintegrations.SyncCandidate{
		deviceSyncCandidate("11111111-1111-1111-1111-111111111111", "org-a"),
		deviceSyncCandidate("22222222-2222-2222-2222-222222222222", "org-b"),
	}

	listCalls := 0
	env.RegisterActivityWithOptions(
		func(_ context.Context, input activities.GetDeviceIntegrationSyncCandidatesInput) ([]deviceintegrations.SyncCandidate, error) {
			listCalls++
			require.Equal(t, int32(deviceIntegrationSyncCoordinatorChildConcurrency), input.Limit)
			if listCalls == 1 {
				return candidates, nil
			}
			return nil, nil
		},
		activity.RegisterOptions{Name: "GetDeviceIntegrationSyncCandidates"},
	)

	// The child activity receives the sync id string and NOTHING else — the
	// signature is the contract that credentials never enter Temporal
	// payloads.
	var ran []string
	env.RegisterActivityWithOptions(
		func(_ context.Context, input string) error {
			ran = append(ran, input)
			return nil
		},
		activity.RegisterOptions{Name: "RunDeviceIntegrationSync"},
	)

	env.ExecuteWorkflow(DeviceIntegrationSyncCoordinatorWorkflow)

	require.True(t, env.IsWorkflowCompleted())
	require.NoError(t, env.GetWorkflowError())
	require.Equal(t, 2, listCalls)
	require.ElementsMatch(t, []string{
		"11111111-1111-1111-1111-111111111111",
		"22222222-2222-2222-2222-222222222222",
	}, ran)
}

func TestDeviceIntegrationSyncCoordinatorContinuesAfterChildFailure(t *testing.T) {
	t.Parallel()

	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()
	env.RegisterWorkflow(DeviceIntegrationSyncWorkflow)

	candidates := []deviceintegrations.SyncCandidate{
		deviceSyncCandidate("11111111-1111-1111-1111-111111111111", "org-a"),
		deviceSyncCandidate("22222222-2222-2222-2222-222222222222", "org-b"),
	}

	listCalls := 0
	env.RegisterActivityWithOptions(
		func(_ context.Context, input activities.GetDeviceIntegrationSyncCandidatesInput) ([]deviceintegrations.SyncCandidate, error) {
			listCalls++
			if listCalls == 1 {
				return candidates, nil
			}
			return nil, nil
		},
		activity.RegisterOptions{Name: "GetDeviceIntegrationSyncCandidates"},
	)

	var ran []string
	env.RegisterActivityWithOptions(
		func(_ context.Context, input string) error {
			ran = append(ran, input)
			if input == "11111111-1111-1111-1111-111111111111" {
				return errors.New("infra exploded")
			}
			return nil
		},
		activity.RegisterOptions{Name: "RunDeviceIntegrationSync"},
	)

	env.ExecuteWorkflow(DeviceIntegrationSyncCoordinatorWorkflow)

	// One child failing must not abort the coordinator or the sibling sync.
	require.True(t, env.IsWorkflowCompleted())
	require.NoError(t, env.GetWorkflowError())
	require.Contains(t, ran, "22222222-2222-2222-2222-222222222222")
}

func TestDeviceIntegrationSyncWorkflowPassesSyncID(t *testing.T) {
	t.Parallel()

	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()

	syncID := uuid.MustParse("33333333-3333-3333-3333-333333333333")
	var actual string
	env.RegisterActivityWithOptions(
		func(_ context.Context, input string) error {
			actual = input
			return nil
		},
		activity.RegisterOptions{Name: "RunDeviceIntegrationSync"},
	)
	env.RegisterWorkflow(DeviceIntegrationSyncWorkflow)

	env.ExecuteWorkflow("DeviceIntegrationSyncWorkflow", syncID.String())

	require.True(t, env.IsWorkflowCompleted())
	require.NoError(t, env.GetWorkflowError())
	require.Equal(t, syncID.String(), actual)
}
