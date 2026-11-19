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

	"github.com/speakeasy-api/gram/server/internal/access/slackrequests"
	"github.com/speakeasy-api/gram/server/internal/attr"
	tenv "github.com/speakeasy-api/gram/server/internal/temporal"
)

const (
	accessRequestSlackWorkflowIDPrefix = "v1:access-request-slack"

	// accessRequestSlackDecidedSignal wakes a run once its request is decided.
	accessRequestSlackDecidedSignal = "decided"

	// accessRequestSlackRunTimeout outlives the longest request plus the
	// time finishing its cards is retried: a request lapses 24 hours after it
	// is made.
	accessRequestSlackRunTimeout = 24*time.Hour + accessRequestSlackFinishWindow + time.Hour

	// accessRequestSlackExpiryGrace lets a request lapse in the database
	// before its cards are marked expired, so a slow clock cannot show
	// Expired on a request that can still be decided.
	accessRequestSlackExpiryGrace = 30 * time.Second

	// accessRequestSlackActivityAttempts bounds the tries of the send
	// activity.
	accessRequestSlackActivityAttempts = 6

	// accessRequestSlackFinishWindow is how long updating the cards is
	// retried, so a Slack outage of a few hours does not leave them live.
	accessRequestSlackFinishWindow = 6 * time.Hour
)

// AccessRequestSlackWorkflow sends an access request to the organization's
// administrators as Slack DMs, waits until the request is decided, withdrawn
// or lapses, then replaces the DMs with the outcome and tells the requester.
//
// Temporal actions/month ≈ 5R, per namespace, where R is access requests
// whose administrators get a DM: one start, one send activity, one expiry
// timer, one decision signal and one finish activity. A request with nobody to
// DM stops after the send activity (2 actions). Requests come from people
// blocked by a permission and the open-request index dedupes repeats, so the
// rate stays human-paced.
func AccessRequestSlackWorkflow(ctx workflow.Context, target slackrequests.Target) error {
	activityCtx := workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
		StartToCloseTimeout: 2 * time.Minute,
		RetryPolicy:         &temporal.RetryPolicy{InitialInterval: 5 * time.Second, BackoffCoefficient: 2, MaximumInterval: time.Minute, MaximumAttempts: accessRequestSlackActivityAttempts},
	})
	var a *accessRequestSlackActivities
	var sent slackrequests.NotifyResult
	if err := workflow.ExecuteActivity(activityCtx, a.NotifyAccessRequestAdmins, target).Get(activityCtx, &sent); err != nil {
		return fmt.Errorf("send access request DMs: %w", err)
	}
	if sent.Sent == 0 {
		return nil
	}

	timerCtx, cancelTimer := workflow.WithCancel(ctx)
	selector := workflow.NewSelector(ctx)
	selector.AddReceive(workflow.GetSignalChannel(ctx, accessRequestSlackDecidedSignal), func(c workflow.ReceiveChannel, _ bool) {
		c.Receive(ctx, nil)
	})
	if !sent.ExpiresAt.IsZero() {
		wait := max(sent.ExpiresAt.Sub(workflow.Now(ctx))+accessRequestSlackExpiryGrace, 0)
		selector.AddFuture(workflow.NewTimer(timerCtx, wait), func(workflow.Future) {})
	}
	selector.Select(ctx)
	cancelTimer()

	// Finishing retries for longer than sending: until it succeeds the cards
	// keep their buttons. A click on a stale card still refreshes it, since a
	// decided request is refused and the card redrawn.
	finishCtx := workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
		StartToCloseTimeout:    2 * time.Minute,
		ScheduleToCloseTimeout: accessRequestSlackFinishWindow,
		RetryPolicy:            &temporal.RetryPolicy{InitialInterval: 5 * time.Second, BackoffCoefficient: 2, MaximumInterval: 10 * time.Minute, MaximumAttempts: 0},
	})
	if err := workflow.ExecuteActivity(finishCtx, a.FinishAccessRequestCards, target).Get(finishCtx, nil); err != nil {
		return fmt.Errorf("update access request DMs: %w", err)
	}
	return nil
}

type accessRequestSlackActivities struct {
	logger   *slog.Logger
	notifier *slackrequests.Notifier
}

func (a *accessRequestSlackActivities) NotifyAccessRequestAdmins(ctx context.Context, target slackrequests.Target) (slackrequests.NotifyResult, error) {
	result, err := a.notifier.Notify(ctx, target)
	if err == nil {
		return result, nil
	}
	if keepsSentAccessRequestDMs(result, activity.GetInfo(ctx).Attempt) {
		a.logger.WarnContext(ctx, "some administrators got no access request DM", attr.SlogOrganizationID(target.OrganizationID), attr.SlogAccessRequestID(target.RequestID.String()), attr.SlogError(err))
		return result, nil
	}
	return result, fmt.Errorf("notify administrators: %w", err)
}

// keepsSentAccessRequestDMs reports a failed send that ends successfully with
// the DMs it has: on the last attempt, the run still waits and gives those DMs
// the outcome instead of failing and leaving their buttons live. Earlier
// attempts fail, and the retry reaches the administrators still missing a DM.
func keepsSentAccessRequestDMs(result slackrequests.NotifyResult, attempt int32) bool {
	return result.Sent > 0 && attempt >= accessRequestSlackActivityAttempts
}

func (a *accessRequestSlackActivities) FinishAccessRequestCards(ctx context.Context, target slackrequests.Target) error {
	if err := a.notifier.Finish(ctx, target); err != nil {
		return fmt.Errorf("finish access request DMs: %w", err)
	}
	return nil
}

// TemporalAccessRequestSlackScheduler runs one AccessRequestSlackWorkflow per
// access request.
type TemporalAccessRequestSlackScheduler struct {
	TemporalEnv *tenv.Environment
}

var _ slackrequests.Scheduler = (*TemporalAccessRequestSlackScheduler)(nil)

func accessRequestSlackWorkflowID(target slackrequests.Target) string {
	return fmt.Sprintf("%s:%s:%s", accessRequestSlackWorkflowIDPrefix, target.OrganizationID, target.RequestID)
}

// StartNotify starts the request's run. The workflow ID carries the request
// ID, so a redelivered event never sends the DMs twice.
func (s *TemporalAccessRequestSlackScheduler) StartNotify(ctx context.Context, target slackrequests.Target) error {
	if s == nil || s.TemporalEnv == nil {
		return errors.New("temporal environment is not configured")
	}
	_, err := s.TemporalEnv.Client().ExecuteWorkflow(ctx, client.StartWorkflowOptions{
		ID:                                       accessRequestSlackWorkflowID(target),
		TaskQueue:                                string(s.TemporalEnv.Queue()),
		WorkflowIDReusePolicy:                    enums.WORKFLOW_ID_REUSE_POLICY_ALLOW_DUPLICATE_FAILED_ONLY,
		WorkflowExecutionErrorWhenAlreadyStarted: true,
		WorkflowRunTimeout:                       accessRequestSlackRunTimeout,
	}, AccessRequestSlackWorkflow, target)
	if _, ok := errors.AsType[*serviceerror.WorkflowExecutionAlreadyStarted](err); ok {
		return nil
	}
	if err != nil {
		return fmt.Errorf("start access request Slack workflow: %w", err)
	}
	return nil
}

// SignalDecided wakes the request's run. A request whose run already ended,
// or that never had one because nobody could be sent a DM, needs nothing.
func (s *TemporalAccessRequestSlackScheduler) SignalDecided(ctx context.Context, target slackrequests.Target) error {
	if s == nil || s.TemporalEnv == nil {
		return errors.New("temporal environment is not configured")
	}
	err := s.TemporalEnv.Client().SignalWorkflow(ctx, accessRequestSlackWorkflowID(target), "", accessRequestSlackDecidedSignal, nil)
	if _, ok := errors.AsType[*serviceerror.NotFound](err); ok {
		return nil
	}
	if err != nil {
		return fmt.Errorf("signal access request Slack workflow: %w", err)
	}
	return nil
}
