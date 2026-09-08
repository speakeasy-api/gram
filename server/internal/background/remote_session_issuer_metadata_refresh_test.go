package background

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"go.temporal.io/api/enums/v1"
	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/converter"
	"go.temporal.io/sdk/testsuite"

	"github.com/speakeasy-api/gram/server/internal/background/activities"
)

func issuerCandidate(host string) activities.RemoteSessionIssuerMetadataRefreshCandidate {
	return activities.RemoteSessionIssuerMetadataRefreshCandidate{
		ID:             uuid.New(),
		IssuerURL:      "https://" + host,
		Host:           host,
		ProjectID:      uuid.Nil,
		OrganizationID: "",
	}
}

func TestGroupIssuersByHost(t *testing.T) {
	t.Parallel()

	a, b, c, d := issuerCandidate("zeta.example.com"), issuerCandidate("alpha.example.com"), issuerCandidate("zeta.example.com"), issuerCandidate("zeta.example.com")
	unparseable := issuerCandidate("")
	jobs := groupIssuersByHost([]activities.RemoteSessionIssuerMetadataRefreshCandidate{a, b, c, a, unparseable, d}, 2)

	require.Equal(t, []activities.RefreshRemoteSessionIssuerMetadataHostInput{
		{Host: "alpha.example.com", Issuers: []activities.RemoteSessionIssuerMetadataRefreshCandidate{b}},
		{Host: "zeta.example.com", Issuers: []activities.RemoteSessionIssuerMetadataRefreshCandidate{a, c}},
	}, jobs, "hosts sort, one job per host holds the first cap issuers, a duplicate runs once, and a candidate without a host is dropped")
	require.Empty(t, groupIssuersByHost(nil, 2))
}

func TestRemoteSessionIssuerMetadataRefreshWorkflow_ReprojectsThenFetchesPerHost(t *testing.T) {
	t.Parallel()

	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()

	var mu sync.Mutex
	var calls []string
	var reprojected activities.ReprojectRemoteSessionIssuerMetadataInput
	var hosts []activities.RefreshRemoteSessionIssuerMetadataHostInput
	record := func(name string) {
		mu.Lock()
		defer mu.Unlock()
		calls = append(calls, name)
	}

	stale, fresh := issuerCandidate("a.example.com"), issuerCandidate("b.example.com")
	fetchA, fetchB, fetchC := issuerCandidate("idp.example.com"), issuerCandidate("other.example.com"), issuerCandidate("idp.example.com")
	env.RegisterActivityWithOptions(
		func(context.Context, activities.ListRemoteSessionIssuerMetadataReprojectCandidatesInput) ([]activities.RemoteSessionIssuerMetadataRefreshCandidate, error) {
			record("list-reproject")
			return []activities.RemoteSessionIssuerMetadataRefreshCandidate{stale, fresh}, nil
		},
		activity.RegisterOptions{Name: "ListRemoteSessionIssuerMetadataReprojectCandidates"},
	)
	env.RegisterActivityWithOptions(
		func(_ context.Context, input activities.ReprojectRemoteSessionIssuerMetadataInput) (activities.RemoteSessionIssuerMetadataRefreshResult, error) {
			record("reproject")
			mu.Lock()
			defer mu.Unlock()
			reprojected = input
			return activities.RemoteSessionIssuerMetadataRefreshResult{Outcomes: map[string]int{"reprojected": 2}}, nil
		},
		activity.RegisterOptions{Name: "ReprojectRemoteSessionIssuerMetadata"},
	)
	env.RegisterActivityWithOptions(
		func(context.Context, activities.ListRemoteSessionIssuerMetadataRefreshCandidatesInput) ([]activities.RemoteSessionIssuerMetadataRefreshCandidate, error) {
			record("list-refresh")
			return []activities.RemoteSessionIssuerMetadataRefreshCandidate{fetchA, fetchB, fetchC}, nil
		},
		activity.RegisterOptions{Name: "ListRemoteSessionIssuerMetadataRefreshCandidates"},
	)
	env.RegisterActivityWithOptions(
		func(_ context.Context, input activities.RefreshRemoteSessionIssuerMetadataHostInput) (activities.RemoteSessionIssuerMetadataRefreshResult, error) {
			record("refresh-host")
			mu.Lock()
			defer mu.Unlock()
			hosts = append(hosts, input)
			return activities.RemoteSessionIssuerMetadataRefreshResult{Outcomes: map[string]int{"refreshed": len(input.Issuers)}}, nil
		},
		activity.RegisterOptions{Name: "RefreshRemoteSessionIssuerMetadataHost"},
	)

	env.ExecuteWorkflow(RemoteSessionIssuerMetadataRefreshWorkflow)

	require.True(t, env.IsWorkflowCompleted())
	require.NoError(t, env.GetWorkflowError())
	mu.Lock()
	defer mu.Unlock()
	require.Equal(t, []string{"list-reproject", "reproject", "list-refresh", "refresh-host", "refresh-host"}, calls, "re-projection completes before the fetch pass lists its candidates")
	require.Equal(t, []activities.RemoteSessionIssuerMetadataRefreshCandidate{stale, fresh}, reprojected.Issuers)
	require.ElementsMatch(t, []activities.RefreshRemoteSessionIssuerMetadataHostInput{
		{Host: "idp.example.com", Issuers: []activities.RemoteSessionIssuerMetadataRefreshCandidate{fetchA, fetchC}},
		{Host: "other.example.com", Issuers: []activities.RemoteSessionIssuerMetadataRefreshCandidate{fetchB}},
	}, hosts)
}

func TestRemoteSessionIssuerMetadataRefreshWorkflow_SkipsReprojectWhenNothingQualifies(t *testing.T) {
	t.Parallel()

	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()

	var unexpected atomic.Int32
	env.RegisterActivityWithOptions(
		func(context.Context, activities.ListRemoteSessionIssuerMetadataReprojectCandidatesInput) ([]activities.RemoteSessionIssuerMetadataRefreshCandidate, error) {
			return nil, nil
		},
		activity.RegisterOptions{Name: "ListRemoteSessionIssuerMetadataReprojectCandidates"},
	)
	env.RegisterActivityWithOptions(
		func(context.Context, activities.ReprojectRemoteSessionIssuerMetadataInput) (activities.RemoteSessionIssuerMetadataRefreshResult, error) {
			unexpected.Add(1)
			return activities.RemoteSessionIssuerMetadataRefreshResult{Outcomes: nil}, errors.New("reproject must not run for an empty candidate list")
		},
		activity.RegisterOptions{Name: "ReprojectRemoteSessionIssuerMetadata"},
	)
	env.RegisterActivityWithOptions(
		func(context.Context, activities.ListRemoteSessionIssuerMetadataRefreshCandidatesInput) ([]activities.RemoteSessionIssuerMetadataRefreshCandidate, error) {
			return nil, nil
		},
		activity.RegisterOptions{Name: "ListRemoteSessionIssuerMetadataRefreshCandidates"},
	)
	env.RegisterActivityWithOptions(
		func(context.Context, activities.RefreshRemoteSessionIssuerMetadataHostInput) (activities.RemoteSessionIssuerMetadataRefreshResult, error) {
			unexpected.Add(1)
			return activities.RemoteSessionIssuerMetadataRefreshResult{Outcomes: nil}, errors.New("no host activity without due issuers")
		},
		activity.RegisterOptions{Name: "RefreshRemoteSessionIssuerMetadataHost"},
	)

	env.ExecuteWorkflow(RemoteSessionIssuerMetadataRefreshWorkflow)

	require.True(t, env.IsWorkflowCompleted())
	require.NoError(t, env.GetWorkflowError())
	require.Zero(t, unexpected.Load(), "neither the reproject nor a host activity ran")
}

func TestRemoteSessionIssuerMetadataRefreshWorkflow_OneActivityPerHost(t *testing.T) {
	t.Parallel()

	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()

	const bigHost = "big.example.com"
	due := make([]activities.RemoteSessionIssuerMetadataRefreshCandidate, 0, remoteSessionIssuerMetadataRefreshHostCap+20)
	for range remoteSessionIssuerMetadataRefreshHostCap + 5 {
		due = append(due, issuerCandidate(bigHost))
	}
	for i := range 2 * remoteSessionIssuerMetadataRefreshHostConcurrency {
		due = append(due, issuerCandidate(fmt.Sprintf("small-%02d.example.com", i)))
	}

	var mu sync.Mutex
	inFlight := map[string]int{}
	activitiesPerHost := map[string]int{}
	issuersPerHost := map[string]int{}
	var overlaps, maxInFlight, totalInFlight int
	env.SetOnActivityStartedListener(func(info *activity.Info, _ context.Context, args converter.EncodedValues) {
		if info.ActivityType.Name != "RefreshRemoteSessionIssuerMetadataHost" {
			return
		}
		var input activities.RefreshRemoteSessionIssuerMetadataHostInput
		if err := args.Get(&input); err != nil {
			return
		}
		mu.Lock()
		defer mu.Unlock()
		inFlight[input.Host]++
		if inFlight[input.Host] > 1 {
			overlaps++
		}
		totalInFlight++
		maxInFlight = max(maxInFlight, totalInFlight)
		activitiesPerHost[input.Host]++
		issuersPerHost[input.Host] += len(input.Issuers)
		for _, issuer := range input.Issuers {
			if issuer.Host != input.Host {
				overlaps++
			}
		}
	})
	env.SetOnActivityCompletedListener(func(info *activity.Info, _ converter.EncodedValue, _ error) {
		if info.ActivityType.Name != "RefreshRemoteSessionIssuerMetadataHost" {
			return
		}
		mu.Lock()
		defer mu.Unlock()
		totalInFlight--
	})

	env.RegisterActivityWithOptions(
		func(context.Context, activities.ListRemoteSessionIssuerMetadataReprojectCandidatesInput) ([]activities.RemoteSessionIssuerMetadataRefreshCandidate, error) {
			return nil, nil
		},
		activity.RegisterOptions{Name: "ListRemoteSessionIssuerMetadataReprojectCandidates"},
	)
	env.RegisterActivityWithOptions(
		func(context.Context, activities.ReprojectRemoteSessionIssuerMetadataInput) (activities.RemoteSessionIssuerMetadataRefreshResult, error) {
			return activities.RemoteSessionIssuerMetadataRefreshResult{Outcomes: nil}, errors.New("nothing to reproject")
		},
		activity.RegisterOptions{Name: "ReprojectRemoteSessionIssuerMetadata"},
	)
	env.RegisterActivityWithOptions(
		func(context.Context, activities.ListRemoteSessionIssuerMetadataRefreshCandidatesInput) ([]activities.RemoteSessionIssuerMetadataRefreshCandidate, error) {
			return due, nil
		},
		activity.RegisterOptions{Name: "ListRemoteSessionIssuerMetadataRefreshCandidates"},
	)
	env.RegisterActivityWithOptions(
		func(_ context.Context, input activities.RefreshRemoteSessionIssuerMetadataHostInput) (activities.RemoteSessionIssuerMetadataRefreshResult, error) {
			mu.Lock()
			defer mu.Unlock()
			inFlight[input.Host]--
			return activities.RemoteSessionIssuerMetadataRefreshResult{Outcomes: map[string]int{"refreshed": len(input.Issuers)}}, nil
		},
		activity.RegisterOptions{Name: "RefreshRemoteSessionIssuerMetadataHost"},
	)

	env.ExecuteWorkflow(RemoteSessionIssuerMetadataRefreshWorkflow)

	require.True(t, env.IsWorkflowCompleted())
	require.NoError(t, env.GetWorkflowError())
	mu.Lock()
	defer mu.Unlock()
	require.Zero(t, overlaps, "a host never has two activities in flight and never receives another host's issuers")
	require.LessOrEqual(t, maxInFlight, remoteSessionIssuerMetadataRefreshHostConcurrency)
	require.Len(t, activitiesPerHost, 1+2*remoteSessionIssuerMetadataRefreshHostConcurrency, "every host ran")
	for host, n := range activitiesPerHost {
		require.Equal(t, 1, n, "host %s ran once", host)
	}
	require.Equal(t, remoteSessionIssuerMetadataRefreshHostCap, issuersPerHost[bigHost], "a host past the cap keeps its overflow for the next tick")
	require.Equal(t, 1, issuersPerHost["small-00.example.com"])
}

func TestRemoteSessionIssuerMetadataRefreshScheduleOptions(t *testing.T) {
	t.Parallel()

	options := remoteSessionIssuerMetadataRefreshScheduleOptions("test-task-queue")

	require.Equal(t, "v1:remote-session-issuer-metadata-refresh:test-task-queue", options.ID, "the id is queue-scoped so preview workers never repoint each other")
	require.Equal(t, enums.SCHEDULE_OVERLAP_POLICY_SKIP, options.Overlap)
	require.Equal(t, []client.ScheduleIntervalSpec{{Every: time.Hour, Offset: 0}}, options.Spec.Intervals)
	require.Equal(t, 10*time.Minute, options.Spec.Jitter)

	action, ok := options.Action.(*client.ScheduleWorkflowAction)
	require.True(t, ok)
	require.Equal(t, options.ID+"/scheduled", action.ID)
	require.Equal(t, "test-task-queue", action.TaskQueue)
	require.Less(t, action.WorkflowRunTimeout, time.Hour, "a run must finish before the next tick")
	require.Greater(t, action.WorkflowRunTimeout, remoteSessionIssuerMetadataRefreshHostTimeout+remoteSessionIssuerMetadataRefreshReprojectTimeout)
	require.GreaterOrEqual(t, remoteSessionIssuerMetadataRefreshHostTimeout, time.Duration(remoteSessionIssuerMetadataRefreshHostCap)*13*time.Second, "the host timeout covers cap issuers at the 10 s discovery budget plus the 3 s gap")
}
