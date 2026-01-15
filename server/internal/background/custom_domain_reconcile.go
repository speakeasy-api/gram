package background

import (
	"context"
	"errors"
	"fmt"
	"time"

	"go.temporal.io/api/enums/v1"
	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"

	"github.com/speakeasy-api/gram/server/internal/background/activities"
)

const maxDomainsPerExecution = 100 // ContinueAsNew threshold to avoid history bloat

type CustomDomainReconcileParams struct {
	OrganizationID    string
	ContinuationState *ReconcileContinuationState
}

type ReconcileContinuationState struct {
	ProcessedDomains []string
	Reconciled       int
	Failed           int
}

func CustomDomainReconcileWorkflow(ctx workflow.Context, params CustomDomainReconcileParams) error {
	logger := workflow.GetLogger(ctx)
	logger.Info("starting custom domain reconciliation")

	ao := workflow.ActivityOptions{
		StartToCloseTimeout: 5 * time.Minute,
		RetryPolicy: &temporal.RetryPolicy{
			InitialInterval:    time.Second,
			BackoffCoefficient: 2.0,
			MaximumInterval:    time.Minute,
			MaximumAttempts:    3,
		},
	}
	ctx = workflow.WithActivityOptions(ctx, ao)

	var a *Activities // nil receiver; Temporal uses method selector for activity registration

	var domains []string
	listArgs := activities.ListActiveCustomDomainsArgs{
		OrganizationID: params.OrganizationID,
	}
	if err := workflow.ExecuteActivity(ctx, a.ListActiveCustomDomains, listArgs).Get(ctx, &domains); err != nil {
		return fmt.Errorf("failed to list active domains: %w", err)
	}

	logger.Info("found activated domains to reconcile", "count", len(domains))

	if len(domains) == 0 {
		return nil
	}

	state := params.ContinuationState
	if state == nil {
		state = &ReconcileContinuationState{
			ProcessedDomains: make([]string, 0),
			Reconciled:       0,
			Failed:           0,
		}
	}

	processedSet := make(map[string]struct{}, len(state.ProcessedDomains))
	for _, d := range state.ProcessedDomains {
		processedSet[d] = struct{}{}
	}

	processedThisRun := 0
	for _, domain := range domains {
		if _, processed := processedSet[domain]; processed {
			continue
		}

		ensureArgs := activities.EnsureCustomDomainIngressArgs{
			Domain: domain,
		}
		if err := workflow.ExecuteActivity(ctx, a.EnsureCustomDomainIngress, ensureArgs).Get(ctx, nil); err != nil {
			logger.Error("failed to reconcile domain", "domain", domain, "error", err)
			state.Failed++
		} else {
			state.Reconciled++
		}

		state.ProcessedDomains = append(state.ProcessedDomains, domain)
		processedThisRun++

		_ = workflow.Sleep(ctx, 100*time.Millisecond) // rate limit K8s API

		remaining := len(domains) - len(state.ProcessedDomains)
		if processedThisRun >= maxDomainsPerExecution && remaining > 0 {
			logger.Info("continuing workflow to process remaining domains",
				"processed_this_run", processedThisRun,
				"remaining", remaining)

			return workflow.NewContinueAsNewError(ctx, CustomDomainReconcileWorkflow, CustomDomainReconcileParams{
				OrganizationID:    params.OrganizationID,
				ContinuationState: state,
			})
		}
	}

	logger.Info("custom domain reconciliation complete",
		"reconciled", state.Reconciled,
		"failed", state.Failed,
		"total", len(domains))

	if state.Failed > 0 {
		logger.Warn("some domains failed to reconcile - ingresses may be missing",
			"failed_count", state.Failed)
	}

	return nil
}

// AddCustomDomainReconcileSchedule creates the hourly reconciliation schedule. Idempotent.
func AddCustomDomainReconcileSchedule(ctx context.Context, temporalClient client.Client) error {
	_, err := temporalClient.ScheduleClient().Create(ctx, client.ScheduleOptions{
		ID: "v1:custom-domain-reconcile-schedule",
		Spec: client.ScheduleSpec{
			Intervals: []client.ScheduleIntervalSpec{{Every: 1 * time.Hour}},
		},
		Action: &client.ScheduleWorkflowAction{
			ID:        "v1:custom-domain-reconcile/scheduled",
			Workflow:  CustomDomainReconcileWorkflow,
			TaskQueue: string(TaskQueueMain),
			Args:      []interface{}{CustomDomainReconcileParams{OrganizationID: "", ContinuationState: nil}},
		},
		Overlap: enums.SCHEDULE_OVERLAP_POLICY_SKIP,
	})
	if err != nil && !errors.Is(err, temporal.ErrScheduleAlreadyRunning) {
		return fmt.Errorf("create schedule: %w", err)
	}
	return nil
}

// TriggerCustomDomainReconcile runs reconciliation immediately. Multi-replica safe.
func TriggerCustomDomainReconcile(ctx context.Context, temporalClient client.Client) error {
	_, err := temporalClient.ExecuteWorkflow(ctx, client.StartWorkflowOptions{
		ID:                    "v1:custom-domain-reconcile/startup",
		TaskQueue:             string(TaskQueueMain),
		WorkflowIDReusePolicy: enums.WORKFLOW_ID_REUSE_POLICY_ALLOW_DUPLICATE,
	}, CustomDomainReconcileWorkflow, CustomDomainReconcileParams{OrganizationID: "", ContinuationState: nil})
	if err != nil && !temporal.IsWorkflowExecutionAlreadyStartedError(err) {
		return fmt.Errorf("execute workflow: %w", err)
	}
	return nil
}
