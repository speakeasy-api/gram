package background

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/temporal"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/networkingress"
	"github.com/speakeasy-api/gram/server/internal/networkingress/repo"
	"github.com/speakeasy-api/gram/server/internal/o11y"
)

type networkIngressActivities struct {
	executor *networkingress.Executor
	db       *pgxpool.Pool
	queue    string
}

func (a *networkIngressActivities) reconcile(ctx context.Context, id uuid.UUID) (NetworkIngressReconcileResult, error) {
	// Activity cancellation must reach blocked provider calls before a retry
	// can acquire the database serialization lock.
	stop := make(chan struct{})
	defer close(stop)
	go func() {
		ticker := time.NewTicker(10 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-stop:
				return
			case <-ctx.Done():
				return
			case <-ticker.C:
				activity.RecordHeartbeat(ctx)
			}
		}
	}()
	result, err := a.executor.Reconcile(ctx, id)
	if err != nil {
		if failure, ok := errors.AsType[*networkingress.ReconcileError](err); ok {
			if !failure.Retryable {
				return NetworkIngressReconcileResult{Requeue: false}, temporal.NewNonRetryableApplicationError(failure.Code, "network_ingress", nil)
			}
			return NetworkIngressReconcileResult{Requeue: false}, fmt.Errorf("reconcile: %w", temporal.NewApplicationError(failure.Code, "network_ingress"))
		}
		return NetworkIngressReconcileResult{Requeue: false}, fmt.Errorf("reconcile: %w", temporal.NewApplicationError("reconciliation_failed", "network_ingress"))
	}
	return NetworkIngressReconcileResult{Requeue: result.Requeue}, nil
}

func (a *networkIngressActivities) sweep(ctx context.Context) error {
	requester := networkingress.NewOutboxRequester(a.queue)
	after := uuid.Nil
	cutoff := pgtype.Timestamptz{Time: time.Now().Add(-time.Hour), Valid: true, InfinityModifier: pgtype.Finite}
	for range 10 {
		rows, err := repo.New(a.db).ListDueNetworkIngresses(ctx, repo.ListDueNetworkIngressesParams{AfterID: after, StaleBefore: cutoff, PageSize: 100})
		if err != nil {
			return fmt.Errorf("list due network ingresses")
		}
		if len(rows) == 0 {
			return nil
		}
		tx, err := a.db.Begin(ctx)
		if err != nil {
			return fmt.Errorf("begin network ingress redrive")
		}
		defer o11y.NoLogDefer(func() error {
			rollbackCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
			defer cancel()
			return tx.Rollback(rollbackCtx)
		})
		for _, row := range rows {
			if err := requester.Enqueue(ctx, tx, row.OrganizationID, row.ID); err != nil {

				return fmt.Errorf("enqueue network ingress redrive")
			}
			if row.DeletedAt.Valid && time.Since(row.DeletedAt.Time) > 24*time.Hour {
				activity.GetLogger(ctx).Warn("network ingress cleanup overdue", string(attr.NetworkIngressIDKey), row.ID.String())
			}
		}
		if err := tx.Commit(ctx); err != nil {
			return fmt.Errorf("commit network ingress redrive")
		}
		after = rows[len(rows)-1].ID
		if len(rows) < 100 {
			return nil
		}
	}
	return nil
}

func (a *networkIngressActivities) findOrphans(ctx context.Context) error {
	orphans, err := a.executor.FindOrphans(ctx)
	if err != nil {
		return fmt.Errorf("inventory: %w", temporal.NewApplicationError("orphan_inventory_unavailable", "network_ingress"))
	}
	for _, orphan := range orphans {
		activity.GetLogger(ctx).Warn("orphan network ingress resource", string(attr.NetworkIngressIDKey), orphan.OwnerID.String(), "resource_kind", orphan.Kind)
	}
	return nil
}

// RegisterNetworkIngress adds lifecycle work only on the authoritative queue.
// Disabled mutation is not a reason to skip registration: cleanup must continue.
func (w *Workers) RegisterNetworkIngress(executor *networkingress.Executor, queue string) {
	if queue == "" || queue != string(w.env.Queue()) {
		return
	}
	a := &networkIngressActivities{executor: executor, db: w.opts.DB, queue: queue}
	w.main.RegisterActivityWithOptions(a.reconcile, activity.RegisterOptions{Name: NetworkIngressReconcileActivityName})
	w.main.RegisterActivityWithOptions(a.sweep, activity.RegisterOptions{Name: "SweepNetworkIngresses"})
	w.main.RegisterActivityWithOptions(a.findOrphans, activity.RegisterOptions{Name: "FindNetworkIngressOrphans"})
	w.main.RegisterWorkflow(NetworkIngressReconcileWorkflow)
	w.main.RegisterWorkflow(NetworkIngressSweepWorkflow)
	w.networkIngressQueue = queue
}
