package background

import (
	"context"
	"errors"
	"fmt"
	"time"

	"go.temporal.io/sdk/workflow"

	"github.com/speakeasy-api/gram/server/internal/channels"
	tenv "github.com/speakeasy-api/gram/server/internal/temporal"
)

// channelsRetention is the hourly sweep's schedule and retry policy.
var channelsRetention = hourlyCleanup{
	name:               "channels-retention",
	activityTimeout:    10 * time.Minute,
	maxAttempts:        3,
	retryInterval:      time.Minute,
	backoffCoefficient: 2,
	schedulingMargin:   7 * time.Minute,
}

// channelsRetentionMaxPassesPerAttempt bounds how much backlog one attempt may
// drain. Each pass visits at most channels.DefaultRetention() organizations
// and removes at most its batch per organization.
const channelsRetentionMaxPassesPerAttempt = 20

// ChannelsRetentionWorkflow retires Channels history and stale instances past
// their organization's retention window.
//
// One global hourly sweep, one activity per tick, no per-organization
// schedules and nothing in the request path. The activity's first query
// returns only the organizations that actually have something to retire, so a
// deployment where nobody uses Channels costs one indexed query per tick.
//
// Temporal actions/month per namespace: 720 starts + 720 activities = 1,440
// normally, 2,880 if every tick exhausts all three attempts. Fixed, not scaled
// by organizations, projects or messages. Each preview task queue registers
// its own copy of this schedule and adds the same fixed 1,440.
func ChannelsRetentionWorkflow(ctx workflow.Context) error {
	ctx = workflow.WithActivityOptions(ctx, channelsRetention.activityOptions())
	var a *Activities
	return workflow.ExecuteActivity(ctx, a.CleanupChannelsHistory).Get(ctx, nil)
}

// CleanupChannelsHistory drains the retention backlog in bounded passes.
//
// Every pass is independently committed and idempotent: the selecting queries
// only return rows that are still eligible, so a retry neither double-counts
// nor re-removes. Each attempt starts with a fresh pass budget; exhausting it
// is retryable rather than fatal, and the next hourly tick picks up whatever
// is left.
func (a *Activities) CleanupChannelsHistory(ctx context.Context) error {
	retention := channels.DefaultRetention()
	return drainChannelsRetention(ctx, func(ctx context.Context, after string) (channels.RetentionResult, error) {
		return channels.RunRetention(ctx, a.db, a.logger, retention, time.Now(), after)
	})
}

// drainChannelsRetention repeats a bounded pass until the backlog is clear or
// the attempt's budget runs out. Each pass resumes after the organization the
// previous one stopped at, so a deep backlog in the first organizations cannot
// spend the whole budget while later ones wait.
func drainChannelsRetention(ctx context.Context, sweep func(ctx context.Context, after string) (channels.RetentionResult, error)) error {
	after := ""
	for range channelsRetentionMaxPassesPerAttempt {
		result, err := sweep(ctx, after)
		if err != nil {
			return fmt.Errorf("sweep channels retention: %w", err)
		}
		if !result.Truncated {
			return nil
		}
		after = result.Next
	}
	return errors.New("channels retention per-attempt pass budget exhausted")
}

// AddChannelsRetentionSchedule installs the hourly sweep for this task queue.
func AddChannelsRetentionSchedule(ctx context.Context, temporalEnv *tenv.Environment) error {
	return channelsRetention.ensureSchedule(ctx, temporalEnv, ChannelsRetentionWorkflow)
}
