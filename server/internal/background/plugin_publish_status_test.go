package background

import (
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	enums "go.temporal.io/api/enums/v1"
	failurepb "go.temporal.io/api/failure/v1"
	historypb "go.temporal.io/api/history/v1"
	workflowpb "go.temporal.io/api/workflow/v1"
	"go.temporal.io/api/workflowservice/v1"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/speakeasy-api/gram/server/internal/background/activities"
	"github.com/speakeasy-api/gram/server/internal/plugins/publishstatus"
)

// secretPublishFailureText stands in for the raw error a publish can fail
// with. None of it may surface in a status.
const secretPublishFailureText = "push https://x-access-token:ghs_secret@github.com/private-owner/private-repo.git: 401 Bad credentials"

var (
	publishStatusStartedAt = time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	publishStatusClosedAt  = publishStatusStartedAt.Add(2 * time.Minute)
)

func publishDescribe(status enums.WorkflowExecutionStatus, pending ...*workflowpb.PendingActivityInfo) *workflowservice.DescribeWorkflowExecutionResponse {
	info := &workflowpb.WorkflowExecutionInfo{Status: status, StartTime: timestamppb.New(publishStatusStartedAt)}
	if status != enums.WORKFLOW_EXECUTION_STATUS_RUNNING {
		info.CloseTime = timestamppb.New(publishStatusClosedAt)
	}
	return &workflowservice.DescribeWorkflowExecutionResponse{WorkflowExecutionInfo: info, PendingActivities: pending}
}

func applicationFailure(errType string) *failurepb.Failure {
	return &failurepb.Failure{
		Message: secretPublishFailureText,
		FailureInfo: &failurepb.Failure_ApplicationFailureInfo{
			ApplicationFailureInfo: &failurepb.ApplicationFailureInfo{Type: errType},
		},
	}
}

// wrappedFailure nests cause the way a workflow failure wraps the activity
// failure that caused it.
func wrappedFailure(cause *failurepb.Failure) *failurepb.Failure {
	return &failurepb.Failure{
		Message: "publish plugin project: activity error",
		Cause: &failurepb.Failure{
			Message:     "activity error",
			FailureInfo: &failurepb.Failure_ActivityFailureInfo{ActivityFailureInfo: &failurepb.ActivityFailureInfo{}},
			Cause:       cause,
		},
	}
}

func TestPluginPublishStatusFromDescribe(t *testing.T) {
	t.Parallel()

	started := publishStatusStartedAt
	closed := publishStatusClosedAt
	cases := []struct {
		name         string
		resp         *workflowservice.DescribeWorkflowExecutionResponse
		closeFailure *failurepb.Failure
		want         publishstatus.Status
	}{
		{
			name: "no run on record",
			resp: nil,
			want: publishstatus.Status{State: publishstatus.StateNone},
		},
		{
			name: "debounce window before any activity",
			resp: publishDescribe(enums.WORKFLOW_EXECUTION_STATUS_RUNNING),
			want: publishstatus.Status{State: publishstatus.StateQueued, RequestedAt: &started},
		},
		{
			name: "activity scheduled but not picked up",
			resp: publishDescribe(enums.WORKFLOW_EXECUTION_STATUS_RUNNING, &workflowpb.PendingActivityInfo{State: enums.PENDING_ACTIVITY_STATE_SCHEDULED, Attempt: 1}),
			want: publishstatus.Status{State: publishstatus.StateQueued, RequestedAt: &started},
		},
		{
			name: "first attempt running",
			resp: publishDescribe(enums.WORKFLOW_EXECUTION_STATUS_RUNNING, &workflowpb.PendingActivityInfo{State: enums.PENDING_ACTIVITY_STATE_STARTED, Attempt: 1}),
			want: publishstatus.Status{State: publishstatus.StateRunning, RequestedAt: &started, Attempt: 1},
		},
		{
			name: "retrying after a generic failure",
			resp: publishDescribe(enums.WORKFLOW_EXECUTION_STATUS_RUNNING, &workflowpb.PendingActivityInfo{
				State: enums.PENDING_ACTIVITY_STATE_SCHEDULED, Attempt: 2, LastFailure: applicationFailure("*fmt.wrapError"),
			}),
			want: publishstatus.Status{State: publishstatus.StateRetrying, RequestedAt: &started, Attempt: 2, FailureCategory: publishstatus.FailurePublishFailed},
		},
		{
			name: "succeeded",
			resp: publishDescribe(enums.WORKFLOW_EXECUTION_STATUS_COMPLETED),
			want: publishstatus.Status{State: publishstatus.StateSucceeded, RequestedAt: &started, FinishedAt: &closed},
		},
		{
			name:         "failed on a repository conflict",
			resp:         publishDescribe(enums.WORKFLOW_EXECUTION_STATUS_FAILED),
			closeFailure: wrappedFailure(applicationFailure(activities.ErrTypeGitHubRepoConflict)),
			want:         publishstatus.Status{State: publishstatus.StateFailed, RequestedAt: &started, FinishedAt: &closed, FailureCategory: publishstatus.FailureRepositoryConflict},
		},
		{
			name:         "failed after retries were exhausted",
			resp:         publishDescribe(enums.WORKFLOW_EXECUTION_STATUS_FAILED),
			closeFailure: wrappedFailure(applicationFailure("*fmt.wrapError")),
			want:         publishstatus.Status{State: publishstatus.StateFailed, RequestedAt: &started, FinishedAt: &closed, FailureCategory: publishstatus.FailurePublishFailed},
		},
		{
			name:         "failed on an activity timeout",
			resp:         publishDescribe(enums.WORKFLOW_EXECUTION_STATUS_FAILED),
			closeFailure: wrappedFailure(&failurepb.Failure{FailureInfo: &failurepb.Failure_TimeoutFailureInfo{TimeoutFailureInfo: &failurepb.TimeoutFailureInfo{}}}),
			want:         publishstatus.Status{State: publishstatus.StateFailed, RequestedAt: &started, FinishedAt: &closed, FailureCategory: publishstatus.FailureTimedOut},
		},
		{
			name: "workflow run timed out",
			resp: publishDescribe(enums.WORKFLOW_EXECUTION_STATUS_TIMED_OUT),
			want: publishstatus.Status{State: publishstatus.StateFailed, RequestedAt: &started, FinishedAt: &closed, FailureCategory: publishstatus.FailureTimedOut},
		},
		{
			name: "terminated",
			resp: publishDescribe(enums.WORKFLOW_EXECUTION_STATUS_TERMINATED),
			want: publishstatus.Status{State: publishstatus.StateFailed, RequestedAt: &started, FinishedAt: &closed, FailureCategory: publishstatus.FailureCanceled},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := pluginPublishStatusFromDescribe(tc.resp, tc.closeFailure)
			require.Equal(t, tc.want, got)

			rendered := fmt.Sprintf("%+v", got)
			for _, secret := range []string{"ghs_secret", "x-access-token", "private-owner", "private-repo", "Bad credentials"} {
				require.NotContains(t, rendered, secret)
			}
		})
	}
}

type fakeHistoryIterator struct {
	events []*historypb.HistoryEvent
	err    error
}

func (f *fakeHistoryIterator) HasNext() bool { return len(f.events) > 0 || f.err != nil }

func (f *fakeHistoryIterator) Next() (*historypb.HistoryEvent, error) {
	if f.err != nil {
		return nil, f.err
	}
	event := f.events[0]
	f.events = f.events[1:]
	return event, nil
}

func TestPublishCloseFailureRefusesUnreadFailures(t *testing.T) {
	t.Parallel()

	failure := wrappedFailure(applicationFailure(activities.ErrTypeGitHubRepoConflict))
	got, err := publishCloseFailure(&fakeHistoryIterator{events: []*historypb.HistoryEvent{{
		Attributes: &historypb.HistoryEvent_WorkflowExecutionFailedEventAttributes{
			WorkflowExecutionFailedEventAttributes: &historypb.WorkflowExecutionFailedEventAttributes{Failure: failure},
		},
	}}})
	require.NoError(t, err)
	require.Same(t, failure, got)

	_, err = publishCloseFailure(&fakeHistoryIterator{})
	require.Error(t, err, "a missing close event must not read as a failure")

	_, err = publishCloseFailure(&fakeHistoryIterator{events: []*historypb.HistoryEvent{{}}})
	require.Error(t, err, "a close event without a failure must not read as a failure")

	_, err = publishCloseFailure(&fakeHistoryIterator{err: errors.New("history unavailable")})
	require.Error(t, err)
}
