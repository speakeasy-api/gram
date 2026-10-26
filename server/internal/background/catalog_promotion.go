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
	// catalogPromotionInterval bounds discovery cost to 720 runs per month.
	catalogPromotionInterval = time.Hour

	// catalogPromotionDiscoverActivity names the bounded candidate scan.
	catalogPromotionDiscoverActivity = "CatalogPromotionDiscover"

	// catalogPromotionDispatchActivity names delivery of durable pending jobs.
	catalogPromotionDispatchActivity = "CatalogPromotionDispatch"
)

// CatalogPromotionJobs discovers candidates and retries independently recorded deliveries.
type CatalogPromotionJobs interface {
	// Discover checks a bounded page of remote sources for promotion eligibility.
	Discover(context.Context) error

	// Dispatch submits due setup-guide requests for approved promotions.
	Dispatch(context.Context) error

	// Status summarizes persisted progress without including source metadata.
	Status(context.Context) (catalogpromotion.JobStatus, error)
}

// CatalogPromotionWorkflow runs two batch activities per hourly tick. Delivery
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
	queue := string(env.Queue())
	id := fmt.Sprintf("v1:catalog-promotion:%s", queue)
	_, err := env.Client().ScheduleClient().Create(ctx, client.ScheduleOptions{
		ID:            id,
		Overlap:       enums.SCHEDULE_OVERLAP_POLICY_SKIP,
		CatchupWindow: catalogPromotionInterval - time.Second,
		Spec:          client.ScheduleSpec{Intervals: []client.ScheduleIntervalSpec{{Every: catalogPromotionInterval}}},
		Action: &client.ScheduleWorkflowAction{
			ID: id + "/scheduled", Workflow: CatalogPromotionWorkflow,
			TaskQueue: queue, WorkflowRunTimeout: 25 * time.Minute,
		},
	})
	if err != nil && !errors.Is(err, temporal.ErrScheduleAlreadyRunning) {
		return fmt.Errorf("create catalogue promotion schedule: %w", err)
	}
	return nil
}
