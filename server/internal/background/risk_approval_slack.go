package background

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"go.temporal.io/api/enums/v1"
	"go.temporal.io/api/serviceerror"
	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/risk/approval/slackapproval"
	tenv "github.com/speakeasy-api/gram/server/internal/temporal"
)

const (
	riskApprovalSlackWorkflowIDPrefix = "v1:risk-approval-slack"

	// riskApprovalSlackDecidedSignal wakes a run once its request is decided.
	riskApprovalSlackDecidedSignal = "decided"

	// riskApprovalSlackRunTimeout outlives the longest request: agents can
	// wait up to 24 hours for a decision.
	riskApprovalSlackRunTimeout = 25 * time.Hour

	// riskApprovalSlackExpiryGrace lets a request lapse in the database
	// before its cards are marked expired, so a slow clock cannot show
	// Expired on a request that can still be decided.
	riskApprovalSlackExpiryGrace = 30 * time.Second

	// riskApprovalSlackActivityAttempts bounds the tries of each activity.
	riskApprovalSlackActivityAttempts = 6
)

// RiskApprovalSlackWorkflow sends an approval request to its approvers as
// Slack DMs, waits until the request is decided or lapses, then replaces the
// DMs with the outcome.
//
// Temporal actions/month ≈ 5R, per namespace, where R is approval requests
// whose approvers get a DM: one start, one send activity, one expiry timer,
// one decision signal and one update activity. A request with nobody to DM
// stops after the send activity (2 actions). Scales with approval requests,
// and each one waits on a person, so the rate stays human-paced.
func RiskApprovalSlackWorkflow(ctx workflow.Context, target slackapproval.Target) error {
	activityCtx := workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
		StartToCloseTimeout: 2 * time.Minute,
		RetryPolicy:         &temporal.RetryPolicy{InitialInterval: 5 * time.Second, BackoffCoefficient: 2, MaximumInterval: time.Minute, MaximumAttempts: riskApprovalSlackActivityAttempts},
	})
	var a *riskApprovalSlackActivities
	var sent slackapproval.NotifyResult
	if err := workflow.ExecuteActivity(activityCtx, a.NotifyRiskApprovalApprovers, target).Get(activityCtx, &sent); err != nil {
		return fmt.Errorf("send approval DMs: %w", err)
	}
	if sent.Sent == 0 {
		return nil
	}

	timerCtx, cancelTimer := workflow.WithCancel(ctx)
	selector := workflow.NewSelector(ctx)
	selector.AddReceive(workflow.GetSignalChannel(ctx, riskApprovalSlackDecidedSignal), func(c workflow.ReceiveChannel, _ bool) {
		c.Receive(ctx, nil)
	})
	if !sent.ExpiresAt.IsZero() {
		wait := max(sent.ExpiresAt.Sub(workflow.Now(ctx))+riskApprovalSlackExpiryGrace, 0)
		selector.AddFuture(workflow.NewTimer(timerCtx, wait), func(workflow.Future) {})
	}
	selector.Select(ctx)
	cancelTimer()

	if err := workflow.ExecuteActivity(activityCtx, a.FinishRiskApprovalCards, target).Get(activityCtx, nil); err != nil {
		return fmt.Errorf("update approval DMs: %w", err)
	}
	return nil
}

type riskApprovalSlackActivities struct {
	logger   *slog.Logger
	notifier *slackapproval.Notifier
}

func (a *riskApprovalSlackActivities) NotifyRiskApprovalApprovers(ctx context.Context, target slackapproval.Target) (slackapproval.NotifyResult, error) {
	result, err := a.notifier.Notify(ctx, target)
	if err == nil {
		return result, nil
	}
	if keepsSentDMs(result, activity.GetInfo(ctx).Attempt) {
		a.logger.WarnContext(ctx, "some approvers got no approval DM", attr.SlogOrganizationID(target.OrganizationID), attr.SlogRiskPolicyApprovalID(target.ApprovalID.String()), attr.SlogError(err))
		return result, nil
	}
	return result, fmt.Errorf("notify approvers: %w", err)
}

// keepsSentDMs reports a failed send that ends successfully with the DMs it
// has: on the last attempt, the run still waits and gives those DMs the
// outcome instead of failing and leaving their buttons live. Earlier attempts
// fail, and the retry reaches the approvers still missing a DM.
func keepsSentDMs(result slackapproval.NotifyResult, attempt int32) bool {
	return result.Sent > 0 && attempt >= riskApprovalSlackActivityAttempts
}

func (a *riskApprovalSlackActivities) FinishRiskApprovalCards(ctx context.Context, target slackapproval.Target) error {
	if err := a.notifier.Finish(ctx, target); err != nil {
		return fmt.Errorf("finish approval DMs: %w", err)
	}
	return nil
}

// TemporalRiskApprovalSlackScheduler runs one RiskApprovalSlackWorkflow per
// approval request.
type TemporalRiskApprovalSlackScheduler struct {
	TemporalEnv *tenv.Environment
}

var _ slackapproval.Scheduler = (*TemporalRiskApprovalSlackScheduler)(nil)

func riskApprovalSlackWorkflowID(target slackapproval.Target) string {
	return fmt.Sprintf("%s:%s", riskApprovalSlackWorkflowIDPrefix, target.ApprovalID)
}

// StartNotify starts the request's run. The workflow ID carries the request
// ID, so a redelivered event never sends the DMs twice.
func (s *TemporalRiskApprovalSlackScheduler) StartNotify(ctx context.Context, target slackapproval.Target) error {
	if s == nil || s.TemporalEnv == nil {
		return errors.New("temporal environment is not configured")
	}
	_, err := s.TemporalEnv.Client().ExecuteWorkflow(ctx, client.StartWorkflowOptions{
		ID:                                       riskApprovalSlackWorkflowID(target),
		TaskQueue:                                string(s.TemporalEnv.Queue()),
		WorkflowIDReusePolicy:                    enums.WORKFLOW_ID_REUSE_POLICY_ALLOW_DUPLICATE_FAILED_ONLY,
		WorkflowExecutionErrorWhenAlreadyStarted: true,
		WorkflowRunTimeout:                       riskApprovalSlackRunTimeout,
	}, RiskApprovalSlackWorkflow, target)
	if _, ok := errors.AsType[*serviceerror.WorkflowExecutionAlreadyStarted](err); ok {
		return nil
	}
	if err != nil {
		return fmt.Errorf("start approval Slack workflow: %w", err)
	}
	return nil
}

// SignalDecided wakes the request's run. A request whose run already ended,
// or that never had one because nobody could be sent a DM, needs nothing.
func (s *TemporalRiskApprovalSlackScheduler) SignalDecided(ctx context.Context, target slackapproval.Target) error {
	if s == nil || s.TemporalEnv == nil {
		return errors.New("temporal environment is not configured")
	}
	err := s.TemporalEnv.Client().SignalWorkflow(ctx, riskApprovalSlackWorkflowID(target), "", riskApprovalSlackDecidedSignal, nil)
	if _, ok := errors.AsType[*serviceerror.NotFound](err); ok {
		return nil
	}
	if err != nil {
		return fmt.Errorf("signal approval Slack workflow: %w", err)
	}
	return nil
}
