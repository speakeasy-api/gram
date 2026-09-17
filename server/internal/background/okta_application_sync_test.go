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

	"github.com/speakeasy-api/gram/server/internal/background/activities"
	"github.com/speakeasy-api/gram/server/internal/oktaapplications"
)

func oktaSyncCandidate(id string, org string) oktaapplications.SyncCandidate {
	return oktaapplications.SyncCandidate{
		ConnectionID:     uuid.MustParse(id),
		OrganizationID:   "org_" + org,
		OrganizationSlug: org,
	}
}

func TestOktaApplicationSyncCoordinatorRunsEachCandidateOnceInBoundedBatches(t *testing.T) {
	t.Parallel()

	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()
	env.RegisterWorkflow(OktaApplicationSyncWorkflow)

	// Seven candidates across two passes: the coordinator asks for at most
	// the batch size and excludes what it already attempted.
	all := []oktaapplications.SyncCandidate{
		oktaSyncCandidate("11111111-1111-1111-1111-111111111111", "org-a"),
		oktaSyncCandidate("22222222-2222-2222-2222-222222222222", "org-b"),
		oktaSyncCandidate("33333333-3333-3333-3333-333333333333", "org-c"),
		oktaSyncCandidate("44444444-4444-4444-4444-444444444444", "org-d"),
		oktaSyncCandidate("55555555-5555-5555-5555-555555555555", "org-e"),
		oktaSyncCandidate("66666666-6666-6666-6666-666666666666", "org-f"),
		oktaSyncCandidate("77777777-7777-7777-7777-777777777777", "org-g"),
	}

	listCalls := 0
	env.RegisterActivityWithOptions(
		func(_ context.Context, input activities.GetOktaApplicationSyncCandidatesInput) ([]oktaapplications.SyncCandidate, error) {
			listCalls++
			require.Equal(t, int32(oktaApplicationSyncChildConcurrency), input.Limit)
			excluded := map[uuid.UUID]bool{}
			for _, id := range input.ExcludeConnectionIDs {
				excluded[id] = true
			}
			var out []oktaapplications.SyncCandidate
			for _, c := range all {
				if !excluded[c.ConnectionID] {
					out = append(out, c)
				}
				if len(out) == int(input.Limit) {
					break
				}
			}
			return out, nil
		},
		activity.RegisterOptions{Name: "GetOktaApplicationSyncCandidates"},
	)

	var ran []string
	var ranMu sync.Mutex
	env.RegisterActivityWithOptions(
		func(_ context.Context, input string) error {
			ranMu.Lock()
			defer ranMu.Unlock()
			ran = append(ran, input)
			return nil
		},
		activity.RegisterOptions{Name: "RunOktaApplicationSync"},
	)

	env.ExecuteWorkflow(OktaApplicationSyncCoordinatorWorkflow)

	require.True(t, env.IsWorkflowCompleted())
	require.NoError(t, env.GetWorkflowError())
	require.Equal(t, 3, listCalls, "two batches plus the empty terminating pass")
	ranMu.Lock()
	defer ranMu.Unlock()
	require.Len(t, ran, len(all))
	seen := map[string]bool{}
	for _, id := range ran {
		require.False(t, seen[id], "candidate %s ran twice", id)
		seen[id] = true
	}
}

func TestOktaApplicationSyncCoordinatorContinuesAfterChildFailure(t *testing.T) {
	t.Parallel()

	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()
	env.RegisterWorkflow(OktaApplicationSyncWorkflow)

	candidates := []oktaapplications.SyncCandidate{
		oktaSyncCandidate("11111111-1111-1111-1111-111111111111", "org-a"),
		oktaSyncCandidate("22222222-2222-2222-2222-222222222222", "org-b"),
	}

	listCalls := 0
	env.RegisterActivityWithOptions(
		func(_ context.Context, _ activities.GetOktaApplicationSyncCandidatesInput) ([]oktaapplications.SyncCandidate, error) {
			listCalls++
			if listCalls == 1 {
				return candidates, nil
			}
			return nil, nil
		},
		activity.RegisterOptions{Name: "GetOktaApplicationSyncCandidates"},
	)

	var ran []string
	var ranMu sync.Mutex
	env.RegisterActivityWithOptions(
		func(_ context.Context, input string) error {
			ranMu.Lock()
			ran = append(ran, input)
			ranMu.Unlock()
			if input == "11111111-1111-1111-1111-111111111111" {
				return errors.New("infra exploded")
			}
			return nil
		},
		activity.RegisterOptions{Name: "RunOktaApplicationSync"},
	)

	env.ExecuteWorkflow(OktaApplicationSyncCoordinatorWorkflow)

	require.True(t, env.IsWorkflowCompleted())
	require.NoError(t, env.GetWorkflowError())
	ranMu.Lock()
	defer ranMu.Unlock()
	require.Contains(t, ran, "22222222-2222-2222-2222-222222222222")
}

func TestOktaApplicationSyncWorkflowPassesConnectionID(t *testing.T) {
	t.Parallel()

	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()

	var got string
	env.RegisterActivityWithOptions(
		func(_ context.Context, input string) error {
			got = input
			return nil
		},
		activity.RegisterOptions{Name: "RunOktaApplicationSync"},
	)

	env.ExecuteWorkflow(OktaApplicationSyncWorkflow, "11111111-1111-1111-1111-111111111111")

	require.True(t, env.IsWorkflowCompleted())
	require.NoError(t, env.GetWorkflowError())
	require.Equal(t, "11111111-1111-1111-1111-111111111111", got)
}

func TestOktaApplicationSyncScheduleIDIsQueueScoped(t *testing.T) {
	t.Parallel()

	require.NotEqual(t, oktaApplicationSyncCoordinatorScheduleID("a"), oktaApplicationSyncCoordinatorScheduleID("b"))
}
