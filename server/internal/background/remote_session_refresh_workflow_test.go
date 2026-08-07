package background

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/testsuite"
	"go.temporal.io/sdk/workflow"

	"github.com/speakeasy-api/gram/server/internal/background/activities"
)

func TestRemoteSessionRefreshWorkflow_DrainsClaimedPages(t *testing.T) {
	t.Parallel()

	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()
	env.RegisterWorkflow(RemoteSessionRefreshWorkflow)

	candidates := []activities.RemoteSessionRefreshCandidate{
		{SessionID: uuid.New(), OrganizationID: "org-a"},
		{SessionID: uuid.New(), OrganizationID: "org-b"},
		{SessionID: uuid.New(), OrganizationID: "org-a"},
	}
	pages := [][]activities.RemoteSessionRefreshCandidate{
		candidates[:2],
		candidates[2:],
		nil,
	}
	claimCalls := 0
	env.RegisterActivityWithOptions(
		func(_ context.Context, input activities.ClaimDueRemoteSessionRefreshCandidatesInput) ([]activities.RemoteSessionRefreshCandidate, error) {
			require.EqualValues(t, remoteSessionRefreshBatchSize, input.Limit)
			page := pages[claimCalls]
			claimCalls++
			return page, nil
		},
		activity.RegisterOptions{Name: "ClaimDueRemoteSessionRefreshCandidates"},
	)

	attempts := map[uuid.UUID]int{}
	var attemptsMu sync.Mutex
	env.RegisterActivityWithOptions(
		func(_ context.Context, input activities.RefreshRemoteSessionInput) (activities.RefreshRemoteSessionResult, error) {
			attemptsMu.Lock()
			attempts[input.SessionID]++
			attemptsMu.Unlock()
			if input.SessionID == candidates[1].SessionID {
				return activities.RefreshRemoteSessionResult{}, errors.New("candidate load failed")
			}
			return activities.RefreshRemoteSessionResult{RateLimited: false}, nil
		},
		activity.RegisterOptions{Name: "RefreshRemoteSession"},
	)

	env.ExecuteWorkflow(RemoteSessionRefreshWorkflow)

	require.True(t, env.IsWorkflowCompleted())
	require.NoError(t, env.GetWorkflowError(), "one failed refresh must not fail the best-effort sweep")
	require.Equal(t, 1, attempts[candidates[0].SessionID])
	require.Equal(t, 1, attempts[candidates[1].SessionID], "the hourly sweep, not Temporal, retries refresh failures")
	require.Equal(t, 1, attempts[candidates[2].SessionID])
	require.Equal(t, 3, claimCalls)
}

func TestRemoteSessionRefreshWorkflow_ClaimFailureFailsRun(t *testing.T) {
	t.Parallel()

	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()
	env.RegisterWorkflow(RemoteSessionRefreshWorkflow)
	env.RegisterActivityWithOptions(
		func(context.Context, activities.ClaimDueRemoteSessionRefreshCandidatesInput) ([]activities.RemoteSessionRefreshCandidate, error) {
			return nil, errors.New("database unavailable")
		},
		activity.RegisterOptions{Name: "ClaimDueRemoteSessionRefreshCandidates"},
	)

	env.ExecuteWorkflow(RemoteSessionRefreshWorkflow)

	require.True(t, env.IsWorkflowCompleted())
	require.Error(t, env.GetWorkflowError())
}

func TestRemoteSessionRefreshWorkflow_CircuitBreaksRateLimitedProvider(t *testing.T) {
	t.Parallel()

	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()
	env.RegisterWorkflow(RemoteSessionRefreshWorkflow)

	rateLimited := activities.RemoteSessionRefreshCandidate{
		SessionID:      uuid.New(),
		OrganizationID: "org-a",
		ProviderKey:    "https://provider.example.com",
	}
	sameProvider := activities.RemoteSessionRefreshCandidate{
		SessionID:      uuid.New(),
		OrganizationID: "org-b",
		ProviderKey:    rateLimited.ProviderKey,
	}
	otherProvider := activities.RemoteSessionRefreshCandidate{
		SessionID:      uuid.New(),
		OrganizationID: "org-c",
		ProviderKey:    "https://other.example.com",
	}
	pages := [][]activities.RemoteSessionRefreshCandidate{
		{rateLimited},
		{sameProvider, otherProvider},
		nil,
	}
	claimCalls := 0
	env.RegisterActivityWithOptions(
		func(context.Context, activities.ClaimDueRemoteSessionRefreshCandidatesInput) ([]activities.RemoteSessionRefreshCandidate, error) {
			page := pages[claimCalls]
			claimCalls++
			return page, nil
		},
		activity.RegisterOptions{Name: "ClaimDueRemoteSessionRefreshCandidates"},
	)

	attempts := map[uuid.UUID]int{}
	var attemptsMu sync.Mutex
	env.RegisterActivityWithOptions(
		func(_ context.Context, input activities.RefreshRemoteSessionInput) (activities.RefreshRemoteSessionResult, error) {
			attemptsMu.Lock()
			attempts[input.SessionID]++
			attemptsMu.Unlock()
			if input.SessionID == rateLimited.SessionID {
				return activities.RefreshRemoteSessionResult{RateLimited: true}, nil
			}
			return activities.RefreshRemoteSessionResult{RateLimited: false}, nil
		},
		activity.RegisterOptions{Name: "RefreshRemoteSession"},
	)

	env.ExecuteWorkflow(RemoteSessionRefreshWorkflow)

	require.True(t, env.IsWorkflowCompleted())
	require.NoError(t, env.GetWorkflowError())
	require.Equal(t, 1, attempts[rateLimited.SessionID])
	require.Zero(t, attempts[sameProvider.SessionID], "the provider circuit must protect other organizations sharing the endpoint")
	require.Equal(t, 1, attempts[otherProvider.SessionID])
}

func TestRemoteSessionRefreshWorkflow_ContinuesAsNewWhenSuggested(t *testing.T) {
	t.Parallel()

	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()
	env.RegisterWorkflow(RemoteSessionRefreshWorkflow)

	candidate := activities.RemoteSessionRefreshCandidate{
		SessionID:      uuid.New(),
		OrganizationID: "org-a",
		ProviderKey:    "https://provider.example.com",
	}
	env.RegisterActivityWithOptions(
		func(context.Context, activities.ClaimDueRemoteSessionRefreshCandidatesInput) ([]activities.RemoteSessionRefreshCandidate, error) {
			env.SetContinueAsNewSuggested(true)
			return []activities.RemoteSessionRefreshCandidate{candidate}, nil
		},
		activity.RegisterOptions{Name: "ClaimDueRemoteSessionRefreshCandidates"},
	)
	env.RegisterActivityWithOptions(
		func(context.Context, activities.RefreshRemoteSessionInput) (activities.RefreshRemoteSessionResult, error) {
			return activities.RefreshRemoteSessionResult{RateLimited: true}, nil
		},
		activity.RegisterOptions{Name: "RefreshRemoteSession"},
	)

	env.ExecuteWorkflow(RemoteSessionRefreshWorkflow)

	var continueErr *workflow.ContinueAsNewError
	require.ErrorAs(t, env.GetWorkflowError(), &continueErr)
	require.Equal(t, "RemoteSessionRefreshWorkflow", continueErr.WorkflowType.Name)
	require.Nil(t, continueErr.Input)
}
