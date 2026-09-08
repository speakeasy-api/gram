package background

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/google/uuid"
	"go.temporal.io/api/enums/v1"
	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"

	"github.com/speakeasy-api/gram/server/internal/background/activities"
	tenv "github.com/speakeasy-api/gram/server/internal/temporal"
)

// The sweep ticks hourly; each host is one activity of at most HostCap issuers fetched in turn, so a host is never hit concurrently.
const (
	remoteSessionIssuerMetadataRefreshScheduleName = "remote-session-issuer-metadata-refresh"

	remoteSessionIssuerMetadataRefreshInterval        = time.Hour
	remoteSessionIssuerMetadataRefreshScheduleJitter  = 10 * time.Minute
	remoteSessionIssuerMetadataRefreshListLimit       = 500
	remoteSessionIssuerMetadataRefreshHostCap         = 100
	remoteSessionIssuerMetadataRefreshHostConcurrency = 8

	remoteSessionIssuerMetadataRefreshListTimeout          = 30 * time.Second
	remoteSessionIssuerMetadataRefreshReprojectTimeout     = 5 * time.Minute
	remoteSessionIssuerMetadataRefreshHostTimeout          = 25 * time.Minute
	remoteSessionIssuerMetadataRefreshHostHeartbeatTimeout = 2 * time.Minute
	remoteSessionIssuerMetadataRefreshWorkflowTimeout      = 50 * time.Minute
)

func RemoteSessionIssuerMetadataRefreshWorkflow(ctx workflow.Context) error {
	logger := workflow.GetLogger(ctx)
	listCtx := workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
		StartToCloseTimeout: remoteSessionIssuerMetadataRefreshListTimeout,
		RetryPolicy: &temporal.RetryPolicy{
			MaximumAttempts:    3,
			InitialInterval:    5 * time.Second,
			BackoffCoefficient: 2,
		},
	})
	reprojectCtx := workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
		StartToCloseTimeout: remoteSessionIssuerMetadataRefreshReprojectTimeout,
		HeartbeatTimeout:    time.Minute,
		RetryPolicy:         &temporal.RetryPolicy{MaximumAttempts: 1},
	})
	hostCtx := workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
		StartToCloseTimeout: remoteSessionIssuerMetadataRefreshHostTimeout,
		HeartbeatTimeout:    remoteSessionIssuerMetadataRefreshHostHeartbeatTimeout,
		// The next hourly tick is the retry boundary.
		RetryPolicy: &temporal.RetryPolicy{MaximumAttempts: 1},
	})

	var a *Activities

	var reproject []activities.RemoteSessionIssuerMetadataRefreshCandidate
	if err := workflow.ExecuteActivity(
		listCtx,
		a.ListRemoteSessionIssuerMetadataReprojectCandidates,
		activities.ListRemoteSessionIssuerMetadataReprojectCandidatesInput{Limit: remoteSessionIssuerMetadataRefreshListLimit},
	).Get(listCtx, &reproject); err != nil {
		return fmt.Errorf("list issuer metadata reproject candidates: %w", err)
	}
	if len(reproject) > 0 {
		var result activities.RemoteSessionIssuerMetadataRefreshResult
		if err := workflow.ExecuteActivity(
			reprojectCtx,
			a.ReprojectRemoteSessionIssuerMetadata,
			activities.ReprojectRemoteSessionIssuerMetadataInput{Issuers: reproject},
		).Get(reprojectCtx, &result); err != nil {
			logger.Warn("reproject issuer metadata", "error", err)
		}
	}

	var due []activities.RemoteSessionIssuerMetadataRefreshCandidate
	if err := workflow.ExecuteActivity(
		listCtx,
		a.ListRemoteSessionIssuerMetadataRefreshCandidates,
		activities.ListRemoteSessionIssuerMetadataRefreshCandidatesInput{Limit: remoteSessionIssuerMetadataRefreshListLimit},
	).Get(listCtx, &due); err != nil {
		return fmt.Errorf("list issuer metadata refresh candidates: %w", err)
	}

	jobs := groupIssuersByHost(due, remoteSessionIssuerMetadataRefreshHostCap)
	for wave := range slices.Chunk(jobs, remoteSessionIssuerMetadataRefreshHostConcurrency) {
		futures := make([]workflow.Future, 0, len(wave))
		for _, job := range wave {
			futures = append(futures, workflow.ExecuteActivity(hostCtx, a.RefreshRemoteSessionIssuerMetadataHost, job))
		}
		for i, future := range futures {
			var result activities.RemoteSessionIssuerMetadataRefreshResult
			if err := future.Get(hostCtx, &result); err != nil {
				logger.Warn("refresh issuer metadata host", "host", wave[i].Host, "error", err)
			}
		}
	}
	return nil
}

// groupIssuersByHost builds one activity input per host holding its first perHost issuers in list order, each issuer once; hosts sort by name and a candidate with no host is dropped.
func groupIssuersByHost(candidates []activities.RemoteSessionIssuerMetadataRefreshCandidate, perHost int) []activities.RefreshRemoteSessionIssuerMetadataHostInput {
	byHost := map[string][]activities.RemoteSessionIssuerMetadataRefreshCandidate{}
	seen := map[uuid.UUID]bool{}
	hosts := make([]string, 0)
	for _, candidate := range candidates {
		if candidate.Host == "" || seen[candidate.ID] {
			continue
		}
		seen[candidate.ID] = true
		if _, ok := byHost[candidate.Host]; !ok {
			hosts = append(hosts, candidate.Host)
		}
		if len(byHost[candidate.Host]) < perHost {
			byHost[candidate.Host] = append(byHost[candidate.Host], candidate)
		}
	}
	slices.Sort(hosts)

	jobs := make([]activities.RefreshRemoteSessionIssuerMetadataHostInput, 0, len(hosts))
	for _, host := range hosts {
		jobs = append(jobs, activities.RefreshRemoteSessionIssuerMetadataHostInput{Host: host, Issuers: byHost[host]})
	}
	return jobs
}

func remoteSessionIssuerMetadataRefreshScheduleID(taskQueue string) string {
	return fmt.Sprintf("v1:%s:%s", remoteSessionIssuerMetadataRefreshScheduleName, taskQueue)
}

func remoteSessionIssuerMetadataRefreshScheduleOptions(taskQueue string) client.ScheduleOptions {
	scheduleID := remoteSessionIssuerMetadataRefreshScheduleID(taskQueue)
	return client.ScheduleOptions{
		ID:      scheduleID,
		Overlap: enums.SCHEDULE_OVERLAP_POLICY_SKIP,
		Spec: client.ScheduleSpec{
			Intervals: []client.ScheduleIntervalSpec{{Every: remoteSessionIssuerMetadataRefreshInterval}},
			Jitter:    remoteSessionIssuerMetadataRefreshScheduleJitter,
		},
		Action: &client.ScheduleWorkflowAction{
			ID:                 scheduleID + "/scheduled",
			Workflow:           RemoteSessionIssuerMetadataRefreshWorkflow,
			TaskQueue:          taskQueue,
			WorkflowRunTimeout: remoteSessionIssuerMetadataRefreshWorkflowTimeout,
		},
	}
}

// AddRemoteSessionIssuerMetadataRefreshSchedule installs the queue-scoped hourly sweep, updating one that already exists.
func AddRemoteSessionIssuerMetadataRefreshSchedule(ctx context.Context, temporalEnv *tenv.Environment) error {
	scheduleClient := temporalEnv.Client().ScheduleClient()
	options := remoteSessionIssuerMetadataRefreshScheduleOptions(string(temporalEnv.Queue()))

	_, err := scheduleClient.Create(ctx, options)
	switch {
	case errors.Is(err, temporal.ErrScheduleAlreadyRunning):
		if err := scheduleClient.GetHandle(ctx, options.ID).Update(ctx, client.ScheduleUpdateOptions{
			DoUpdate: func(input client.ScheduleUpdateInput) (*client.ScheduleUpdate, error) {
				input.Description.Schedule.Spec = &options.Spec
				input.Description.Schedule.Action = options.Action
				return &client.ScheduleUpdate{
					Schedule:              &input.Description.Schedule,
					TypedSearchAttributes: nil,
				}, nil
			},
		}); err != nil {
			return fmt.Errorf("update remote session issuer metadata refresh schedule: %w", err)
		}
	case err != nil:
		return fmt.Errorf("create remote session issuer metadata refresh schedule: %w", err)
	}
	return nil
}
