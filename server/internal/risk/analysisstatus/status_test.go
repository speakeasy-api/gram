package analysisstatus

import (
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	enums "go.temporal.io/api/enums/v1"
	"go.temporal.io/api/serviceerror"
	"go.temporal.io/api/workflow/v1"
	"go.temporal.io/api/workflowservice/v1"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func describeResp(status enums.WorkflowExecutionStatus, start, closeAt *time.Time) *workflowservice.DescribeWorkflowExecutionResponse {
	info := &workflow.WorkflowExecutionInfo{Status: status}
	if start != nil {
		info.StartTime = timestamppb.New(*start)
	}
	if closeAt != nil {
		info.CloseTime = timestamppb.New(*closeAt)
	}
	return &workflowservice.DescribeWorkflowExecutionResponse{WorkflowExecutionInfo: info}
}

func TestFromDescribe_NotFoundIsNever(t *testing.T) {
	t.Parallel()

	got, err := FromDescribe(nil, serviceerror.NewNotFound("no such workflow"))
	require.NoError(t, err)
	require.Equal(t, StateNever, got.State)
	require.Nil(t, got.LastRunAt)
	require.Nil(t, got.RunningSince)
}

func TestFromDescribe_OtherErrorsPropagate(t *testing.T) {
	t.Parallel()

	boom := errors.New("temporal unavailable")
	_, err := FromDescribe(nil, boom)
	require.ErrorIs(t, err, boom)
}

func TestFromDescribe_Running(t *testing.T) {
	t.Parallel()

	start := time.Date(2026, 9, 15, 10, 0, 0, 0, time.UTC)
	got, err := FromDescribe(describeResp(enums.WORKFLOW_EXECUTION_STATUS_RUNNING, &start, nil), nil)
	require.NoError(t, err)
	require.Equal(t, StateRunning, got.State)
	require.NotNil(t, got.RunningSince)
	require.True(t, got.RunningSince.Equal(start))
	require.Nil(t, got.LastRunAt)
	require.Empty(t, got.LastRunOutcome)
}

func TestFromDescribe_ClosedIsIdleWithOutcome(t *testing.T) {
	t.Parallel()

	start := time.Date(2026, 9, 15, 10, 0, 0, 0, time.UTC)
	closeAt := start.Add(90 * time.Second)

	cases := []struct {
		status  enums.WorkflowExecutionStatus
		outcome string
	}{
		{enums.WORKFLOW_EXECUTION_STATUS_COMPLETED, "completed"},
		{enums.WORKFLOW_EXECUTION_STATUS_FAILED, "failed"},
		{enums.WORKFLOW_EXECUTION_STATUS_CONTINUED_AS_NEW, "continued_as_new"},
		{enums.WORKFLOW_EXECUTION_STATUS_TIMED_OUT, "timed_out"},
	}
	for _, tc := range cases {
		got, err := FromDescribe(describeResp(tc.status, &start, &closeAt), nil)
		require.NoError(t, err)
		require.Equal(t, StateIdle, got.State, tc.outcome)
		require.Equal(t, tc.outcome, got.LastRunOutcome)
		require.NotNil(t, got.LastRunAt)
		require.True(t, got.LastRunAt.Equal(closeAt))
		require.NotNil(t, got.LastRunStartedAt)
		require.True(t, got.LastRunStartedAt.Equal(start))
		require.Nil(t, got.RunningSince)
	}
}

func TestFromDescribe_NilInfoIsNever(t *testing.T) {
	t.Parallel()

	got, err := FromDescribe(&workflowservice.DescribeWorkflowExecutionResponse{}, nil)
	require.NoError(t, err)
	require.Equal(t, StateNever, got.State)
}
