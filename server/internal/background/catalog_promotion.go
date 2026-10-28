package background

import (
	"context"
	"errors"
	"fmt"
	"time"

	"go.temporal.io/api/enums/v1"
	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"

	"github.com/speakeasy-api/gram/server/internal/catalogpromotion"
	tenv "github.com/speakeasy-api/gram/server/internal/temporal"
)

const (
	// catalogPromotionInterval schedules staff candidate discovery once a week.
	catalogPromotionInterval = 7 * 24 * time.Hour

	// catalogPromotionDeliveryInterval keeps published guide requests moving between reviews.
	catalogPromotionDeliveryInterval = time.Hour

	// catalogPromotionDiscoverActivity names the bounded candidate scan.
	catalogPromotionDiscoverActivity = "CatalogPromotionDiscover"

	// catalogPromotionReviewBatchActivity names one checkpointed part of the weekly review.
	catalogPromotionReviewBatchActivity = "CatalogPromotionReviewBatch"

	// catalogPromotionDispatchActivity names delivery of durable pending jobs.
	catalogPromotionDispatchActivity = "CatalogPromotionDispatch"
)

// CatalogPromotionJobs discovers candidates and retries independently recorded deliveries.
type CatalogPromotionJobs interface {
	// Discover checks a bounded page of remote sources for promotion eligibility.
	Discover(context.Context) error

	// DiscoverBatch advances one bounded part of the weekly fleet review.
	DiscoverBatch(context.Context) (catalogpromotion.DiscoveryBatch, error)

	// Dispatch submits due setup-guide requests for approved promotions.
	Dispatch(context.Context) error

	// Status summarizes persisted progress without including source metadata.
	Status(context.Context) (catalogpromotion.JobStatus, error)
}

// CatalogPromotionWorkflow retains the original command order for in-flight executions. Delivery
// still runs when discovery fails so existing promotions are not stranded.
func CatalogPromotionWorkflow(ctx workflow.Context) error {
	ctx = workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
		StartToCloseTimeout: 10 * time.Minute,
		HeartbeatTimeout:    time.Minute,
		RetryPolicy:         &temporal.RetryPolicy{MaximumAttempts: 1},
	})
	discoveryErr := workflow.ExecuteActivity(ctx, catalogPromotionDiscoverActivity).Get(ctx, nil)
	deliveryErr := workflow.ExecuteActivity(ctx, catalogPromotionDispatchActivity).Get(ctx, nil)
	return errors.Join(discoveryErr, deliveryErr)
}

// CatalogPromotionReviewWorkflow drains bounded batches without growing an unbounded history.
func CatalogPromotionReviewWorkflow(ctx workflow.Context) error {
	ctx = workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
		StartToCloseTimeout: 10 * time.Minute,
		HeartbeatTimeout:    time.Minute,
		RetryPolicy:         &temporal.RetryPolicy{InitialInterval: 10 * time.Second, BackoffCoefficient: 2, MaximumInterval: time.Minute, MaximumAttempts: 3},
	})
	for range 10 {
		var batch catalogpromotion.DiscoveryBatch
		if err := workflow.ExecuteActivity(ctx, catalogPromotionReviewBatchActivity).Get(ctx, &batch); err != nil {
			return err
		}
		if !batch.More {
			return nil
		}
	}
	return workflow.NewContinueAsNewError(ctx, CatalogPromotionReviewWorkflow)
}

func (a *catalogPromotionActivities) reviewBatch(ctx context.Context) (catalogpromotion.DiscoveryBatch, error) {
	var batch catalogpromotion.DiscoveryBatch
	err := a.run(ctx, "discovery", func(ctx context.Context) error {
		var err error
		batch, err = a.jobs.DiscoverBatch(ctx)
		if err != nil {
			return fmt.Errorf("discover catalogue review batch: %w", err)
		}
		return nil
	})
	return batch, err
}

// CatalogPromotionDeliveryWorkflow drains approved guide requests between weekly reviews.
func CatalogPromotionDeliveryWorkflow(ctx workflow.Context) error {
	ctx = workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
		StartToCloseTimeout: 10 * time.Minute,
		HeartbeatTimeout:    time.Minute,
		RetryPolicy:         &temporal.RetryPolicy{MaximumAttempts: 1},
	})
	return workflow.ExecuteActivity(ctx, catalogPromotionDispatchActivity).Get(ctx, nil)
}

type catalogPromotionActivities struct {
	jobs           CatalogPromotionJobs
	deliveryPaused bool
	now            func() time.Time
}

func (a *catalogPromotionActivities) discover(ctx context.Context) error {
	return a.run(ctx, "discovery", a.jobs.Discover)
}

func (a *catalogPromotionActivities) dispatch(ctx context.Context) error {
	paused := 0.0
	if a.deliveryPaused {
		paused = 1
	}
	activity.GetMetricsHandler(ctx).Gauge("catalog_promotion_delivery_paused").Update(paused)
	var err error
	if !a.deliveryPaused {
		err = a.run(ctx, "docs", a.jobs.Dispatch)
	}
	if ctx.Err() != nil {
		return fmt.Errorf("catalogue promotion canceled: %w", ctx.Err())
	}
	statusCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	status, statusErr := a.jobs.Status(statusCtx)
	if statusErr != nil {
		return errors.Join(err, fmt.Errorf("read catalogue promotion progress: %w", statusErr))
	}
	metrics := activity.GetMetricsHandler(ctx)
	metrics.Gauge("catalog_promotion_pending_docs").Update(float64(status.PendingDocs))
	metrics.Gauge("catalog_promotion_needs_attention").Update(float64(status.NeedsAttention))
	metrics.Gauge("catalog_promotion_guide_in_review").Update(float64(status.GuideInReview))
	metrics.Gauge("catalog_promotion_discovery_failures").Update(float64(status.DiscoveryFailures))
	oldestAge := 0.0
	if !status.OldestPendingAt.IsZero() {
		oldestAge = max(0, a.now().Sub(status.OldestPendingAt).Seconds())
	}
	metrics.Gauge("catalog_promotion_oldest_pending_seconds").Update(oldestAge)
	lastDiscovery := 0.0
	if !status.LastDiscoveryAt.IsZero() {
		lastDiscovery = float64(status.LastDiscoveryAt.Unix())
	}
	metrics.Gauge("catalog_promotion_last_discovery_timestamp_seconds").Update(lastDiscovery)
	return err
}

// Heartbeats deliver cancellation while a provider call is blocked. Database
// receipts remain the checkpoint; heartbeats never carry tenant information.
func (a *catalogPromotionActivities) run(ctx context.Context, phase string, operation func(context.Context) error) error {
	beatCtx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	activity.RecordHeartbeat(beatCtx)
	go func() {
		defer close(done)
		ticker := time.NewTicker(20 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-beatCtx.Done():
				return
			case <-ticker.C:
				activity.RecordHeartbeat(beatCtx)
			}
		}
	}()
	defer func() { cancel(); <-done }()
	err := operation(ctx)
	if err != nil {
		activity.GetMetricsHandler(ctx).WithTags(map[string]string{"phase": phase}).Counter("catalog_promotion_batch_failures").Inc(1)
		return fmt.Errorf("catalogue promotion %s: %w", phase, err)
	}
	return nil
}

func addCatalogPromotionSchedule(ctx context.Context, env *tenv.Environment) error {
	return configureCatalogPromotionSchedules(ctx, env.Client().ScheduleClient(), string(env.Queue()))
}

func configureCatalogPromotionSchedules(ctx context.Context, sc client.ScheduleClient, queue string) error {
	id := fmt.Sprintf("v1:catalog-promotion:%s", queue)
	options := client.ScheduleOptions{
		ID: id, Overlap: enums.SCHEDULE_OVERLAP_POLICY_SKIP,
		CatchupWindow: catalogPromotionInterval - time.Second,
		Spec:          client.ScheduleSpec{Intervals: []client.ScheduleIntervalSpec{{Every: catalogPromotionInterval}}},
		Action:        &client.ScheduleWorkflowAction{ID: id + "/scheduled", Workflow: CatalogPromotionReviewWorkflow, TaskQueue: queue, WorkflowRunTimeout: time.Hour, WorkflowExecutionTimeout: 24 * time.Hour},
	}
	_, err := sc.Create(ctx, options)
	paused := false
	if errors.Is(err, temporal.ErrScheduleAlreadyRunning) {
		err = sc.GetHandle(ctx, id).Update(ctx, client.ScheduleUpdateOptions{
			DoUpdate: func(input client.ScheduleUpdateInput) (*client.ScheduleUpdate, error) {
				schedule := input.Description.Schedule
				action, ok := schedule.Action.(*client.ScheduleWorkflowAction)
				if !ok || action.TaskQueue != queue {
					return nil, fmt.Errorf("catalogue promotion schedule is owned by another task queue")
				}
				paused = schedule.State != nil && schedule.State.Paused
				if schedule.Spec != nil && len(schedule.Spec.Intervals) == 1 && schedule.Spec.Intervals[0].Every == catalogPromotionInterval && action.Workflow == "CatalogPromotionReviewWorkflow" && schedule.Policy != nil && schedule.Policy.CatchupWindow == options.CatchupWindow {
					return nil, temporal.ErrSkipScheduleUpdate
				}
				// Existing executions retain their original workflow; only future starts use the batched review.
				setScheduleCatchup(&schedule, options.CatchupWindow)
				schedule.Spec = &options.Spec
				action.Workflow = "CatalogPromotionReviewWorkflow"
				action.WorkflowRunTimeout = time.Hour
				action.WorkflowExecutionTimeout = 24 * time.Hour
				return &client.ScheduleUpdate{Schedule: &schedule, TypedSearchAttributes: nil}, nil
			},
		})
	}
	if err != nil {
		return fmt.Errorf("configure catalogue promotion schedule: %w", err)
	}
	docsID := fmt.Sprintf("v1:catalog-promotion-docs:%s", queue)
	_, err = sc.Create(ctx, client.ScheduleOptions{
		ID: docsID, Overlap: enums.SCHEDULE_OVERLAP_POLICY_SKIP,
		CatchupWindow: catalogPromotionDeliveryInterval - time.Second,
		Paused:        paused,
		Spec:          client.ScheduleSpec{Intervals: []client.ScheduleIntervalSpec{{Every: catalogPromotionDeliveryInterval}}},
		Action:        &client.ScheduleWorkflowAction{ID: docsID + "/scheduled", Workflow: CatalogPromotionDeliveryWorkflow, TaskQueue: queue, WorkflowRunTimeout: 15 * time.Minute},
	})
	if err != nil && !errors.Is(err, temporal.ErrScheduleAlreadyRunning) {
		return fmt.Errorf("configure catalogue documentation schedule: %w", err)
	}
	return nil
}
