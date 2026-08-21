package background

import (
	"context"
	"errors"
	"fmt"
	"time"

	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"

	"github.com/speakeasy-api/gram/server/internal/background/activities"
	tenv "github.com/speakeasy-api/gram/server/internal/temporal"
)

const (
	trialDemotionScheduleID          = "v1:demote-expired-trials-schedule"
	trialDemotionScheduledWorkflowID = "v1:demote-expired-trials/scheduled"

	trialDemotionActivityMaxRetries = 3

	// One demotion is two OpenRouter round trips and one transaction, so 30s is
	// a generous ceiling that surfaces a stalled provider rather than masking
	// it behind retries.
	trialDemotionActivityTimeout = 30 * time.Second

	// A trial expires 14 days after signup, so an hour of extra access costs
	// little and the tick is cheap: the table holds one row per trial signup
	// ever, and the scan returns nothing on almost every run.
	trialDemotionScheduleInterval = time.Hour

	// The sweep walks organizations one at a time, so this bounds a burst of
	// trials that all expire in the same hour. A run that overruns it leaves
	// the organizations it did not reach unstamped, and the next tick picks
	// them up.
	trialDemotionWorkflowRunTimeout = 30 * time.Minute
)

// DemoteExpiredTrialsWorkflow locks out every trial organization
// whose trial ended without a conversion.
func DemoteExpiredTrialsWorkflow(ctx workflow.Context) error {
	ctx = workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
		StartToCloseTimeout: trialDemotionActivityTimeout,
		RetryPolicy: &temporal.RetryPolicy{
			MaximumAttempts: trialDemotionActivityMaxRetries,
		},
	})

	var a *Activities
	var organizationIDs []string
	if err := workflow.ExecuteActivity(ctx, a.ListExpiredTrials).Get(ctx, &organizationIDs); err != nil {
		return fmt.Errorf("list expired trials: %w", err)
	}

	failed := 0
	for _, organizationID := range organizationIDs {
		if err := workflow.ExecuteActivity(ctx, a.DemoteExpiredTrial, activities.DemoteExpiredTrialArgs{
			OrganizationID: organizationID,
		}).Get(ctx, nil); err != nil {
			// Sweep the rest of the batch before reporting: one organization
			// whose provider calls keep failing must not hold back the others,
			// and its row stays unstamped for the next tick.
			workflow.GetLogger(ctx).Error("demote expired trial",
				"organization_id", organizationID, "error", err.Error())
			failed++
		}
	}

	if failed > 0 {
		return fmt.Errorf("demote expired trials: %d of %d organizations failed", failed, len(organizationIDs))
	}

	return nil
}

func AddTrialDemotionSchedule(ctx context.Context, temporalEnv *tenv.Environment) error {
	sc := temporalEnv.Client().ScheduleClient()

	spec := client.ScheduleSpec{
		Intervals: []client.ScheduleIntervalSpec{
			{Every: trialDemotionScheduleInterval},
		},
	}
	action := &client.ScheduleWorkflowAction{
		ID:                 trialDemotionScheduledWorkflowID,
		Workflow:           DemoteExpiredTrialsWorkflow,
		TaskQueue:          string(temporalEnv.Queue()),
		WorkflowRunTimeout: trialDemotionWorkflowRunTimeout,
	}

	_, err := sc.Create(ctx, client.ScheduleOptions{
		ID:     trialDemotionScheduleID,
		Spec:   spec,
		Action: action,
	})
	switch {
	case errors.Is(err, temporal.ErrScheduleAlreadyRunning):
		// Push spec and action changes into the already-created schedule:
		// Create alone would leave deployed environments on the old values.
		if err := sc.GetHandle(ctx, trialDemotionScheduleID).Update(ctx, client.ScheduleUpdateOptions{
			DoUpdate: func(input client.ScheduleUpdateInput) (*client.ScheduleUpdate, error) {
				input.Description.Schedule.Spec = &spec
				input.Description.Schedule.Action = action
				return &client.ScheduleUpdate{
					Schedule:              &input.Description.Schedule,
					TypedSearchAttributes: nil,
				}, nil
			},
		}); err != nil {
			return fmt.Errorf("update existing trial demotion schedule: %w", err)
		}
	case err != nil:
		return fmt.Errorf("create trial demotion schedule: %w", err)
	}

	return nil
}
