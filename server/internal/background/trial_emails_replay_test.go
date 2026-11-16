package background

import (
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	commonpb "go.temporal.io/api/common/v1"
	enumspb "go.temporal.io/api/enums/v1"
	historypb "go.temporal.io/api/history/v1"
	taskqueuepb "go.temporal.io/api/taskqueue/v1"
	"go.temporal.io/sdk/converter"
	"go.temporal.io/sdk/worker"
	"go.temporal.io/sdk/workflow"
	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// trialHistoryBuilder assembles the event history the trial lifecycle workflow
// recorded before trialEndingSoonRetiredChangeID existed, so the replayer can
// check that runs started on that code still replay on the current code.
type trialHistoryBuilder struct {
	t      *testing.T
	events []*historypb.HistoryEvent
	now    time.Time
}

func (b *trialHistoryBuilder) add(eventType enumspb.EventType, set func(*historypb.HistoryEvent)) int64 {
	id := int64(len(b.events) + 1)
	event := &historypb.HistoryEvent{EventId: id, EventType: eventType, EventTime: timestamppb.New(b.now)}
	set(event)
	b.events = append(b.events, event)
	return id
}

func (b *trialHistoryBuilder) payloads(values ...any) *commonpb.Payloads {
	b.t.Helper()
	p, err := converter.GetDefaultDataConverter().ToPayloads(values...)
	require.NoError(b.t, err)
	return p
}

// workflowTask records one completed workflow task and returns its completed
// event ID, which the commands it produced point back to.
func (b *trialHistoryBuilder) workflowTask() int64 {
	scheduled := b.add(enumspb.EVENT_TYPE_WORKFLOW_TASK_SCHEDULED, func(e *historypb.HistoryEvent) {
		e.Attributes = &historypb.HistoryEvent_WorkflowTaskScheduledEventAttributes{WorkflowTaskScheduledEventAttributes: &historypb.WorkflowTaskScheduledEventAttributes{
			TaskQueue:           &taskqueuepb.TaskQueue{Name: "replay"},
			StartToCloseTimeout: durationpb.New(10 * time.Second),
		}}
	})
	started := b.add(enumspb.EVENT_TYPE_WORKFLOW_TASK_STARTED, func(e *historypb.HistoryEvent) {
		e.Attributes = &historypb.HistoryEvent_WorkflowTaskStartedEventAttributes{WorkflowTaskStartedEventAttributes: &historypb.WorkflowTaskStartedEventAttributes{ScheduledEventId: scheduled}}
	})
	return b.add(enumspb.EVENT_TYPE_WORKFLOW_TASK_COMPLETED, func(e *historypb.HistoryEvent) {
		e.Attributes = &historypb.HistoryEvent_WorkflowTaskCompletedEventAttributes{WorkflowTaskCompletedEventAttributes: &historypb.WorkflowTaskCompletedEventAttributes{ScheduledEventId: scheduled, StartedEventId: started}}
	})
}

func (b *trialHistoryBuilder) activity(name string, workflowTaskCompleted int64, result *commonpb.Payloads) {
	scheduled := int64(len(b.events) + 1)
	b.add(enumspb.EVENT_TYPE_ACTIVITY_TASK_SCHEDULED, func(e *historypb.HistoryEvent) {
		e.Attributes = &historypb.HistoryEvent_ActivityTaskScheduledEventAttributes{ActivityTaskScheduledEventAttributes: &historypb.ActivityTaskScheduledEventAttributes{
			ActivityId:                   strconv.FormatInt(scheduled, 10),
			ActivityType:                 &commonpb.ActivityType{Name: name},
			TaskQueue:                    &taskqueuepb.TaskQueue{Name: "replay"},
			WorkflowTaskCompletedEventId: workflowTaskCompleted,
		}}
	})
	started := b.add(enumspb.EVENT_TYPE_ACTIVITY_TASK_STARTED, func(e *historypb.HistoryEvent) {
		e.Attributes = &historypb.HistoryEvent_ActivityTaskStartedEventAttributes{ActivityTaskStartedEventAttributes: &historypb.ActivityTaskStartedEventAttributes{ScheduledEventId: scheduled}}
	})
	b.add(enumspb.EVENT_TYPE_ACTIVITY_TASK_COMPLETED, func(e *historypb.HistoryEvent) {
		e.Attributes = &historypb.HistoryEvent_ActivityTaskCompletedEventAttributes{ActivityTaskCompletedEventAttributes: &historypb.ActivityTaskCompletedEventAttributes{ScheduledEventId: scheduled, StartedEventId: started, Result: result}}
	})
}

// preRetirementSleepingHistory is a trial-started run, or its reminder-only
// continuation, recorded by the code before the reminder was retired and now
// waiting on the timer until three days before the trial ends.
func preRetirementSleepingHistory(t *testing.T, reminderOnly bool) *historypb.History {
	t.Helper()
	start := time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)
	b := &trialHistoryBuilder{t: t, events: nil, now: start}
	input := TrialLifecycleEmailInput{Kind: TrialStartedEmailKind, OrganizationID: "<ORGANIZATION_ID>", UserID: "", ReminderOnly: reminderOnly}
	b.add(enumspb.EVENT_TYPE_WORKFLOW_EXECUTION_STARTED, func(e *historypb.HistoryEvent) {
		e.Attributes = &historypb.HistoryEvent_WorkflowExecutionStartedEventAttributes{WorkflowExecutionStartedEventAttributes: &historypb.WorkflowExecutionStartedEventAttributes{
			WorkflowType:        &commonpb.WorkflowType{Name: "TrialLifecycleEmailWorkflow"},
			TaskQueue:           &taskqueuepb.TaskQueue{Name: "replay"},
			Input:               b.payloads(input),
			WorkflowRunTimeout:  durationpb.New(trialLifecycleEmailWorkflowRunTimeout),
			WorkflowTaskTimeout: durationpb.New(10 * time.Second),
		}}
	})
	completed := b.workflowTask()
	if !reminderOnly {
		b.activity("SendTrialLifecycleEmail", completed, nil)
		completed = b.workflowTask()
	}
	endsAt := start.Add(10 * 24 * time.Hour)
	b.activity("ResolveTrialEndingReminder", completed, b.payloads(legacyTrialEndingReminderState{
		Active:         true,
		TrialCreatedAt: start.Add(-time.Hour),
		TrialEndsAt:    endsAt,
		SendAt:         endsAt.Add(-72 * time.Hour),
	}))
	completed = b.workflowTask()
	b.add(enumspb.EVENT_TYPE_TIMER_STARTED, func(e *historypb.HistoryEvent) {
		e.Attributes = &historypb.HistoryEvent_TimerStartedEventAttributes{TimerStartedEventAttributes: &historypb.TimerStartedEventAttributes{
			TimerId:                      strconv.Itoa(len(b.events) + 1),
			StartToFireTimeout:           durationpb.New(7 * 24 * time.Hour),
			WorkflowTaskCompletedEventId: completed,
		}}
	})
	return &historypb.History{Events: b.events}
}

func TestTrialLifecycleEmailWorkflowReplaysPreRetirementRuns(t *testing.T) {
	t.Parallel()

	for _, reminderOnly := range []bool{false, true} {
		replayer := worker.NewWorkflowReplayer()
		replayer.RegisterWorkflow(TrialLifecycleEmailWorkflow)
		require.NoError(t, replayer.ReplayWorkflowHistory(nil, preRetirementSleepingHistory(t, reminderOnly)), "reminderOnly=%v", reminderOnly)
	}
}

// Guards the test above: without the version gate, the same history must fail
// to replay, which proves the replayer is checking the activity sequence.
func TestTrialLifecycleEmailReplayDetectsMissingVersionGate(t *testing.T) {
	t.Parallel()

	replayer := worker.NewWorkflowReplayer()
	replayer.RegisterWorkflowWithOptions(func(ctx workflow.Context, input TrialLifecycleEmailInput) error {
		ctx = workflow.WithActivityOptions(ctx, workflow.ActivityOptions{StartToCloseTimeout: time.Minute})
		var a *Activities
		return workflow.ExecuteActivity(ctx, a.SendTrialLifecycleEmail, input).Get(ctx, nil)
	}, workflow.RegisterOptions{Name: "TrialLifecycleEmailWorkflow"})
	require.Error(t, replayer.ReplayWorkflowHistory(nil, preRetirementSleepingHistory(t, false)))
}
