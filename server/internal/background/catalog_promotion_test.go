package background

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/speakeasy-api/gram/server/internal/catalogpromotion"
	"github.com/stretchr/testify/require"
	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/converter"
	"go.temporal.io/sdk/testsuite"
	"go.temporal.io/sdk/worker"
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
