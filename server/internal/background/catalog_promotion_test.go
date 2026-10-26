package background

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/speakeasy-api/gram/server/internal/catalogpromotion"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"go.temporal.io/api/enums/v1"
	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/converter"
	temporalmocks "go.temporal.io/sdk/mocks"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/testsuite"
	"go.temporal.io/sdk/worker"
	"go.temporal.io/sdk/workflow"
)

func TestCatalogPromotionDeliveryRunsAfterDiscoveryFailure(t *testing.T) {
	t.Parallel()
	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()
	delivered := false
	env.RegisterActivityWithOptions(func(context.Context) error { return errors.New("telemetry unavailable") }, activity.RegisterOptions{Name: catalogPromotionDiscoverActivity})
	env.RegisterActivityWithOptions(func(context.Context) error { delivered = true; return nil }, activity.RegisterOptions{Name: catalogPromotionDispatchActivity})
	env.ExecuteWorkflow(CatalogPromotionWorkflow)
	require.True(t, env.IsWorkflowCompleted())
	require.Error(t, env.GetWorkflowError())
	require.True(t, delivered, "pending deliveries cannot depend on new candidate discovery")
}

type promotionJobsStub struct {
	discover func(context.Context) error
	dispatch func(context.Context) error
	status   func(context.Context) (catalogpromotion.JobStatus, error)
}

func (s promotionJobsStub) Discover(ctx context.Context) error { return s.discover(ctx) }
func (s promotionJobsStub) DiscoverBatch(ctx context.Context) (catalogpromotion.DiscoveryBatch, error) {
	return catalogpromotion.DiscoveryBatch{}, s.discover(ctx)
}
func (s promotionJobsStub) Dispatch(ctx context.Context) error { return s.dispatch(ctx) }
func (s promotionJobsStub) Status(ctx context.Context) (catalogpromotion.JobStatus, error) {
	return s.status(ctx)
}

func TestCatalogPromotionPausePreservesDiscoveryAndStatus(t *testing.T) {
	t.Parallel()
	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestActivityEnvironment()
	discovered, dispatched, inspected := false, false, false
	jobs := promotionJobsStub{
		discover: func(context.Context) error { discovered = true; return nil },
		dispatch: func(context.Context) error { dispatched = true; return nil },
		status: func(context.Context) (catalogpromotion.JobStatus, error) {
			inspected = true
			var status catalogpromotion.JobStatus
			return status, nil
		},
	}
	a := &catalogPromotionActivities{jobs: jobs, deliveryPaused: true, now: time.Now}
	env.RegisterActivity(a.discover)
	env.RegisterActivity(a.dispatch)
	_, err := env.ExecuteActivity(a.discover)
	require.NoError(t, err)
	_, err = env.ExecuteActivity(a.dispatch)
	require.NoError(t, err)
	require.True(t, discovered)
	require.True(t, inspected)
	require.False(t, dispatched, "pausing must leave pending deliveries untouched")
}

func TestCatalogPromotionStatusFailureIsVisible(t *testing.T) {
	t.Parallel()
	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestActivityEnvironment()
	jobs := promotionJobsStub{
		discover: func(context.Context) error { return nil },
		dispatch: func(context.Context) error { return nil },
		status: func(context.Context) (catalogpromotion.JobStatus, error) {
			var status catalogpromotion.JobStatus
			return status, errors.New("status unavailable")
		},
	}
	a := &catalogPromotionActivities{jobs: jobs, deliveryPaused: false, now: time.Now}
	env.RegisterActivity(a.dispatch)
	_, err := env.ExecuteActivity(a.dispatch)
	require.ErrorContains(t, err, "status unavailable")
}

func TestCatalogPromotionCancellationReachesBlockedDelivery(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestActivityEnvironment()
	env.SetWorkerOptions(worker.Options{BackgroundActivityContext: ctx})
	started := make(chan struct{})
	var once sync.Once
	env.SetOnActivityHeartbeatListener(func(*activity.Info, converter.EncodedValues) {
		once.Do(func() { close(started) })
	})
	jobs := promotionJobsStub{
		discover: func(context.Context) error { return nil },
		dispatch: func(ctx context.Context) error {
			<-ctx.Done()
			return fmt.Errorf("delivery canceled: %w", ctx.Err())
		},
		status: func(context.Context) (catalogpromotion.JobStatus, error) {
			var status catalogpromotion.JobStatus
			return status, errors.New("status must not run after cancellation")
		},
	}
	a := &catalogPromotionActivities{jobs: jobs, deliveryPaused: false, now: time.Now}
	env.RegisterActivity(a.dispatch)
	finished := make(chan error, 1)
	go func() {
		_, err := env.ExecuteActivity(a.dispatch)
		finished <- err
	}()
	<-started
	cancel()
	err := <-finished
	require.ErrorContains(t, err, "catalogue promotion canceled")
	require.NotContains(t, err.Error(), "status must not run")
}

func TestCatalogPromotionHourlyDeliveryDoesNotDiscover(t *testing.T) {
	t.Parallel()
	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()
	delivered := false
	env.RegisterActivityWithOptions(func(context.Context) error { t.Fatal("hourly delivery must not discover candidates"); return nil }, activity.RegisterOptions{Name: catalogPromotionDiscoverActivity})
	env.RegisterActivityWithOptions(func(context.Context) error { delivered = true; return nil }, activity.RegisterOptions{Name: catalogPromotionDispatchActivity})
	env.ExecuteWorkflow(CatalogPromotionDeliveryWorkflow)
	require.NoError(t, env.GetWorkflowError())
	require.True(t, delivered)
}

func TestCatalogPromotionScheduleMigratesWeeklyAndPreservesPause(t *testing.T) {
	t.Parallel()
	sc := &temporalmocks.ScheduleClient{}
	handle := &temporalmocks.ScheduleHandle{}
	sc.On("Create", mock.Anything, mock.MatchedBy(func(options client.ScheduleOptions) bool {
		return options.ID == "v1:catalog-promotion:example" && options.Spec.Intervals[0].Every == 7*24*time.Hour
	})).Return(nil, temporal.ErrScheduleAlreadyRunning).Once()
	sc.On("GetHandle", mock.Anything, "v1:catalog-promotion:example").Return(handle).Once()
	action := &client.ScheduleWorkflowAction{TaskQueue: "example", ID: "preserved"}
	state := &client.ScheduleState{Paused: true, Note: "maintenance", LimitedActions: true, RemainingActions: 7}
	handle.On("Update", mock.Anything, mock.MatchedBy(func(options client.ScheduleUpdateOptions) bool {
		result, err := options.DoUpdate(client.ScheduleUpdateInput{Description: client.ScheduleDescription{Schedule: client.Schedule{
			Spec: &client.ScheduleSpec{Intervals: []client.ScheduleIntervalSpec{{Every: time.Hour}}}, Action: action, State: state,
		}}})
		require.NoError(t, err)
		require.Equal(t, 7*24*time.Hour, result.Schedule.Spec.Intervals[0].Every)
		require.Same(t, state, result.Schedule.State)
		require.Same(t, action, result.Schedule.Action)
		return true
	})).Return(nil).Once()
	sc.On("Create", mock.Anything, mock.MatchedBy(func(options client.ScheduleOptions) bool {
		return options.ID == "v1:catalog-promotion-docs:example" && options.Spec.Intervals[0].Every == time.Hour && options.Paused
	})).Return(nil, nil).Once()
	require.NoError(t, configureCatalogPromotionSchedules(t.Context(), sc, "example"))
	sc.AssertExpectations(t)
	handle.AssertExpectations(t)
}

func TestCatalogPromotionScheduleRejectsForeignQueue(t *testing.T) {
	t.Parallel()
	sc := &temporalmocks.ScheduleClient{}
	handle := &temporalmocks.ScheduleHandle{}
	sc.On("Create", mock.Anything, mock.Anything).Return(nil, temporal.ErrScheduleAlreadyRunning).Once()
	sc.On("GetHandle", mock.Anything, "v1:catalog-promotion:preview").Return(handle).Once()
	handle.On("Update", mock.Anything, mock.MatchedBy(func(options client.ScheduleUpdateOptions) bool {
		result, err := options.DoUpdate(client.ScheduleUpdateInput{Description: client.ScheduleDescription{Schedule: client.Schedule{Action: &client.ScheduleWorkflowAction{TaskQueue: "main"}}}})
		require.ErrorContains(t, err, "another task queue")
		require.Nil(t, result)
		return true
	})).Return(errors.New("another task queue")).Once()
	require.Error(t, configureCatalogPromotionSchedules(t.Context(), sc, "preview"))
	sc.AssertExpectations(t)
	handle.AssertExpectations(t)
}

func TestCatalogPromotionWeeklyReviewDrainsBatches(t *testing.T) {
	t.Parallel()
	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()
	calls := 0
	env.RegisterActivityWithOptions(func(context.Context) (catalogpromotion.DiscoveryBatch, error) {
		calls++
		return catalogpromotion.DiscoveryBatch{More: calls < 3, Failures: 1}, nil
	}, activity.RegisterOptions{Name: catalogPromotionReviewBatchActivity})
	env.ExecuteWorkflow(CatalogPromotionReviewWorkflow)
	require.NoError(t, env.GetWorkflowError())
	require.Equal(t, 3, calls, "unavailable observations must not strand the remaining fleet")
}

func TestCatalogPromotionWeeklyReviewBoundsHistory(t *testing.T) {
	t.Parallel()
	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()
	calls := 0
	env.RegisterActivityWithOptions(func(context.Context) (catalogpromotion.DiscoveryBatch, error) {
		calls++
		return catalogpromotion.DiscoveryBatch{More: true}, nil
	}, activity.RegisterOptions{Name: catalogPromotionReviewBatchActivity})
	env.ExecuteWorkflow(CatalogPromotionReviewWorkflow)
	require.True(t, workflow.IsContinueAsNewError(env.GetWorkflowError()))
	require.Equal(t, 10, calls)
}

func TestCatalogPromotionWeeklyReviewRetriesTransientFailure(t *testing.T) {
	t.Parallel()
	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()
	attempts := 0
	env.RegisterActivityWithOptions(func(context.Context) (catalogpromotion.DiscoveryBatch, error) {
		attempts++
		if attempts == 1 {
			return catalogpromotion.DiscoveryBatch{}, errors.New("database unavailable")
		}
		return catalogpromotion.DiscoveryBatch{More: attempts < 3}, nil
	}, activity.RegisterOptions{Name: catalogPromotionReviewBatchActivity})
	env.ExecuteWorkflow(CatalogPromotionReviewWorkflow)
	require.NoError(t, env.GetWorkflowError())
	require.Equal(t, 3, attempts)
}

func TestCatalogPromotionWeeklyReviewBoundsFailedClaims(t *testing.T) {
	t.Parallel()
	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()
	attempts := 0
	env.RegisterActivityWithOptions(func(context.Context) (catalogpromotion.DiscoveryBatch, error) {
		attempts++
		return catalogpromotion.DiscoveryBatch{}, errors.New("claim promotion discovery: database unavailable")
	}, activity.RegisterOptions{Name: catalogPromotionReviewBatchActivity})
	env.ExecuteWorkflow(CatalogPromotionReviewWorkflow)
	require.Error(t, env.GetWorkflowError())
	require.False(t, workflow.IsContinueAsNewError(env.GetWorkflowError()))
	require.Equal(t, 3, attempts)
}

func TestCatalogPromotionWeeklyScheduleUpgradesCatchupPolicy(t *testing.T) {
	t.Parallel()
	sc := &temporalmocks.ScheduleClient{}
	handle := &temporalmocks.ScheduleHandle{}
	sc.On("Create", mock.Anything, mock.MatchedBy(func(options client.ScheduleOptions) bool {
		return options.ID == "v1:catalog-promotion:example" && options.CatchupWindow == catalogPromotionInterval-time.Second
	})).Return(nil, temporal.ErrScheduleAlreadyRunning).Once()
	sc.On("GetHandle", mock.Anything, "v1:catalog-promotion:example").Return(handle).Once()
	state := &client.ScheduleState{Paused: true, Note: "maintenance", LimitedActions: true, RemainingActions: 7}
	handle.On("Update", mock.Anything, mock.MatchedBy(func(options client.ScheduleUpdateOptions) bool {
		policy := &client.SchedulePolicies{Overlap: enums.SCHEDULE_OVERLAP_POLICY_BUFFER_ONE, CatchupWindow: time.Hour - time.Second, PauseOnFailure: true}
		schedule := client.Schedule{Spec: &client.ScheduleSpec{Intervals: []client.ScheduleIntervalSpec{{Every: catalogPromotionInterval}}}, Action: &client.ScheduleWorkflowAction{TaskQueue: "example", Workflow: "CatalogPromotionReviewWorkflow"}, State: state, Policy: policy}
		result, err := options.DoUpdate(client.ScheduleUpdateInput{Description: client.ScheduleDescription{Schedule: schedule}})
		require.NoError(t, err)
		require.Equal(t, catalogPromotionInterval-time.Second, result.Schedule.Policy.CatchupWindow)
		require.Equal(t, enums.SCHEDULE_OVERLAP_POLICY_BUFFER_ONE, result.Schedule.Policy.Overlap)
		require.True(t, result.Schedule.Policy.PauseOnFailure)
		require.Same(t, state, result.Schedule.State)
		require.Same(t, policy, result.Schedule.Policy)
		result, err = options.DoUpdate(client.ScheduleUpdateInput{Description: client.ScheduleDescription{Schedule: *result.Schedule}})
		require.ErrorIs(t, err, temporal.ErrSkipScheduleUpdate)
		require.Nil(t, result)
		return true
	})).Return(nil).Once()
	sc.On("Create", mock.Anything, mock.MatchedBy(func(options client.ScheduleOptions) bool {
		return options.ID == "v1:catalog-promotion-docs:example" && options.Paused
	})).Return(nil, nil).Once()
	require.NoError(t, configureCatalogPromotionSchedules(t.Context(), sc, "example"))
	sc.AssertExpectations(t)
	handle.AssertExpectations(t)
}
