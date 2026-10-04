package background

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	enums "go.temporal.io/api/enums/v1"
	failurepb "go.temporal.io/api/failure/v1"
	"go.temporal.io/api/serviceerror"
	"go.temporal.io/api/workflowservice/v1"

	"github.com/speakeasy-api/gram/server/internal/background/activities"
	"github.com/speakeasy-api/gram/server/internal/plugins/publishstatus"
)

// maxPublishFailureCauseDepth bounds the walk down a Temporal failure's cause
// chain. A publish failure nests workflow -> activity -> application error, so
// a handful of levels covers every real chain while a malformed or cyclic one
// cannot spin.
const maxPublishFailureCauseDepth = 8

var _ publishstatus.Describer = (*TemporalPluginPublisher)(nil)

// Describe reports the latest change-driven publish for a project by describing
// the latest run of its debounced publish workflow. An empty run id asks
// Temporal for the most recent run, which is what "last publish" means for a
// ContinueAsNew chain. NotFound maps to StateNone rather than an error. A failed
// run costs one more bounded read: the close event, which carries the failure
// that gets classified. The hourly rollout sweep and the initial publish run
// under other workflow ids and are not described here.
func (p *TemporalPluginPublisher) Describe(ctx context.Context, projectID uuid.UUID) (publishstatus.Status, error) {
	if p.TemporalEnv == nil {
		return publishstatus.Status{}, errors.New("plugin publisher has no temporal environment")
	}
	id := pluginPublishWorkflowID(PluginPublishParams{ProjectID: projectID, CreatedByUserID: "", CommitMessage: "", SkipIfUnchanged: false})
	resp, err := p.TemporalEnv.Client().DescribeWorkflowExecution(ctx, id, "")
	if err != nil {
		if _, ok := errors.AsType[*serviceerror.NotFound](err); ok {
			return pluginPublishStatusFromDescribe(nil, nil), nil
		}
		return publishstatus.Status{}, fmt.Errorf("describe plugin publish %s: %w", id, err)
	}

	var closeFailure *failurepb.Failure
	info := resp.GetWorkflowExecutionInfo()
	if info.GetStatus() == enums.WORKFLOW_EXECUTION_STATUS_FAILED {
		iter := p.TemporalEnv.Client().GetWorkflowHistory(ctx, id, info.GetExecution().GetRunId(), false, enums.HISTORY_EVENT_FILTER_TYPE_CLOSE_EVENT)
		if iter.HasNext() {
			event, err := iter.Next()
			if err != nil {
				return publishstatus.Status{}, fmt.Errorf("read plugin publish close event %s: %w", id, err)
			}
			closeFailure = event.GetWorkflowExecutionFailedEventAttributes().GetFailure()
		}
	}
	return pluginPublishStatusFromDescribe(resp, closeFailure), nil
}

// pluginPublishStatusFromDescribe maps a describe result, plus the close
// failure of a failed run, to a publish status. A nil response means no run is
// on record.
func pluginPublishStatusFromDescribe(resp *workflowservice.DescribeWorkflowExecutionResponse, closeFailure *failurepb.Failure) publishstatus.Status {
	status := publishstatus.Status{State: publishstatus.StateNone, RequestedAt: nil, FinishedAt: nil, Attempt: 0, FailureCategory: publishstatus.FailureNone}
	info := resp.GetWorkflowExecutionInfo()
	if info == nil {
		return status
	}
	if ts := info.GetStartTime(); ts != nil {
		t := ts.AsTime()
		status.RequestedAt = &t
	}

	switch info.GetStatus() {
	case enums.WORKFLOW_EXECUTION_STATUS_RUNNING:
		status.State = publishstatus.StateQueued
		// The publish workflow runs one activity at a time, so the first pending
		// activity is the attempt in flight.
		pending := resp.GetPendingActivities()
		if len(pending) == 0 {
			return status
		}
		activity := pending[0]
		switch {
		case activity.GetAttempt() > 1:
			status.State = publishstatus.StateRetrying
			status.Attempt = activity.GetAttempt()
			status.FailureCategory = classifyPublishFailure(activity.GetLastFailure())
		case activity.GetState() == enums.PENDING_ACTIVITY_STATE_STARTED:
			status.State = publishstatus.StateRunning
			status.Attempt = 1
		}
		return status
	case enums.WORKFLOW_EXECUTION_STATUS_CONTINUED_AS_NEW:
		// The latest run of a chain is never itself continued, but if Temporal
		// reports one, a follow-up run is what comes next.
		status.State = publishstatus.StateQueued
		return status
	case enums.WORKFLOW_EXECUTION_STATUS_COMPLETED:
		status.State = publishstatus.StateSucceeded
	case enums.WORKFLOW_EXECUTION_STATUS_FAILED:
		status.State = publishstatus.StateFailed
		status.FailureCategory = classifyPublishFailure(closeFailure)
	case enums.WORKFLOW_EXECUTION_STATUS_TIMED_OUT:
		status.State = publishstatus.StateFailed
		status.FailureCategory = publishstatus.FailureTimedOut
	case enums.WORKFLOW_EXECUTION_STATUS_CANCELED, enums.WORKFLOW_EXECUTION_STATUS_TERMINATED:
		status.State = publishstatus.StateFailed
		status.FailureCategory = publishstatus.FailureCanceled
	case enums.WORKFLOW_EXECUTION_STATUS_UNSPECIFIED:
		status.RequestedAt = nil
		return status
	default:
		status.RequestedAt = nil
		return status
	}
	if ts := info.GetCloseTime(); ts != nil {
		t := ts.AsTime()
		status.FinishedAt = &t
	}
	return status
}

// classifyPublishFailure maps a Temporal failure to a bounded category by its
// structured type information only. The failure message is never read: it can
// carry repository names and upstream provider payloads.
func classifyPublishFailure(failure *failurepb.Failure) publishstatus.FailureCategory {
	for depth := 0; failure != nil && depth < maxPublishFailureCauseDepth; depth++ {
		switch {
		case failure.GetApplicationFailureInfo().GetType() == activities.ErrTypeGitHubRepoConflict:
			return publishstatus.FailureRepositoryConflict
		case failure.GetTimeoutFailureInfo() != nil:
			return publishstatus.FailureTimedOut
		case failure.GetCanceledFailureInfo() != nil, failure.GetTerminatedFailureInfo() != nil:
			return publishstatus.FailureCanceled
		}
		failure = failure.GetCause()
	}
	return publishstatus.FailurePublishFailed
}
