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
	enterpriseTrialDemotionScheduleID          = "v1:demote-expired-enterprise-trials-schedule"
	enterpriseTrialDemotionScheduledWorkflowID = "v1:demote-expired-enterprise-trials/scheduled"

	// One demotion is two OpenRouter round trips and one transaction, so 30s is
	// a generous ceiling that surfaces a stalled provider rather than masking
	// it behind retries.
	enterpriseTrialDemotionActivityMaxRetries = 3
	enterpriseTrialDemotionActivityTimeout    = 30 * time.Second

	// A trial expires 14 days after signup, so an hour of extra access costs
	// little and the tick is cheap: the table holds one row per trial signup
	// ever, and the scan returns nothing on almost every run.
	enterpriseTrialDemotionScheduleInterval = time.Hour

	// The sweep walks organizations one at a time, so this bounds a burst of
	// trials that all expire in the same hour. A run that overruns it leaves
	// the organizations it did not reach unstamped, and the next tick picks
	// them up.
	enterpriseTrialDemotionWorkflowRunTimeout = 30 * time.Minute
)

// DemoteExpiredEnterpriseTrialsWorkflow locks out every trial organization
// whose trial ended without a conversion.
func DemoteExpiredEnterpriseTrialsWorkflow(ctx workflow.Context) error {
	ctx = workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
		StartToCloseTimeout: enterpriseTrialDemotionActivityTimeout,
		RetryPolicy: &temporal.RetryPolicy{
			MaximumAttempts: enterpriseTrialDemotionActivityMaxRetries,
		},
	})

	var a *Activities
	var organizationIDs []string
	if err := workflow.ExecuteActivity(ctx, a.ListExpiredEnterpriseTrials).Get(ctx, &organizationIDs); err != nil {
		return fmt.Errorf("list expired enterprise trials: %w", err)
	}

	failed := 0
	for _, organizationID := range organizationIDs {
		if err := workflow.ExecuteActivity(ctx, a.DemoteExpiredEnterpriseTrial, activities.DemoteExpiredEnterpriseTrialArgs{
			OrganizationID: organizationID,
		}).Get(ctx, nil); err != nil {
			// Sweep the rest of the batch before reporting: one organization
			// whose provider calls keep failing must not hold back the others,
			// and its row stays unstamped for the next tick.
			workflow.GetLogger(ctx).Error("demote expired enterprise trial",
				"organization_id", organizationID, "error", err.Error())
			failed++
		}
	}

	if failed > 0 {
		return fmt.Errorf("demote expired enterprise trials: %d of %d organizations failed", failed, len(organizationIDs))
	}

	return nil
}

func AddEnterpriseTrialDemotionSchedule(ctx context.Context, temporalEnv *tenv.Environment) error {
	sc := temporalEnv.Client().ScheduleClient()

	spec := client.ScheduleSpec{
		Intervals: []client.ScheduleIntervalSpec{
			{Every: enterpriseTrialDemotionScheduleInterval},
		},
	}
	action := &client.ScheduleWorkflowAction{
		ID:                 enterpriseTrialDemotionScheduledWorkflowID,
		Workflow:           DemoteExpiredEnterpriseTrialsWorkflow,
		TaskQueue:          string(temporalEnv.Queue()),
		WorkflowRunTimeout: enterpriseTrialDemotionWorkflowRunTimeout,
	}

	_, err := sc.Create(ctx, client.ScheduleOptions{
		ID:     enterpriseTrialDemotionScheduleID,
		Spec:   spec,
		Action: action,
	})
	switch {
	case errors.Is(err, temporal.ErrScheduleAlreadyRunning):
		// Push spec and action changes into the already-created schedule:
		// Create alone would leave deployed environments on the old values.
		if err := sc.GetHandle(ctx, enterpriseTrialDemotionScheduleID).Update(ctx, client.ScheduleUpdateOptions{
			DoUpdate: func(input client.ScheduleUpdateInput) (*client.ScheduleUpdate, error) {
				input.Description.Schedule.Spec = &spec
				input.Description.Schedule.Action = action
				return &client.ScheduleUpdate{
					Schedule:              &input.Description.Schedule,
					TypedSearchAttributes: nil,
				}, nil
			},
		}); err != nil {
			return fmt.Errorf("update existing enterprise trial demotion schedule: %w", err)
		}
	case err != nil:
		return fmt.Errorf("create enterprise trial demotion schedule: %w", err)
	}

	return nil
}
