package signins

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/workos/workos-go/v6/pkg/workos_errors"
	"go.opentelemetry.io/otel/metric"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/signins/repo"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/workos"
)

// DeliveryKind is what a durable revocation delivery pushes.
type DeliveryKind string

const (
	// DeliveryJTI pushes an access-token JTI into the revocation cache.
	DeliveryJTI DeliveryKind = "jti"
	// DeliveryWorkOSSession ends a WorkOS session.
	DeliveryWorkOSSession DeliveryKind = "workos_session"
	// DeliveryUserEpoch ends a user's dashboard sessions issued before the
	// delivery's effective time; the target is the user id.
	DeliveryUserEpoch DeliveryKind = "user_epoch"
)

// Delivered rows are pruned by the retention sweep tracked in AIM-539.
const (
	// SweepBatchSize bounds one sweep claim so every push in it finishes
	// well inside the lease: SweepBatchSize * pushTimeout < deliveryLease.
	SweepBatchSize = 20
	// deliveryLease is how long a claim keeps other workers off a delivery.
	deliveryLease = 2 * time.Minute
	// pushTimeout bounds one external push.
	pushTimeout = 5 * time.Second
	// stuckAttempts marks a delivery as stuck for alerting; it still retries.
	stuckAttempts = 10
)

// EpochRaiser ends a user's dashboard sessions issued before a time.
type EpochRaiser interface {
	RaiseUserEpoch(ctx context.Context, userID string, at time.Time) error
}

// Deliverer pushes durable revocation deliveries. Every push is idempotent,
// so a delivery that ran but failed to mark is safely retried.
type Deliverer struct {
	logger  *slog.Logger
	db      *pgxpool.Pool
	tokens  TokenRevoker
	idp     IDPSessionRevoker
	epochs  EpochRaiser
	metrics deliveryMetrics
	pending sync.WaitGroup
}

func NewDeliverer(logger *slog.Logger, meterProvider metric.MeterProvider, db *pgxpool.Pool, tokens TokenRevoker, idp IDPSessionRevoker, epochs EpochRaiser) *Deliverer {
	return &Deliverer{
		logger:  logger,
		db:      db,
		tokens:  tokens,
		idp:     idp,
		epochs:  epochs,
		metrics: newDeliveryMetrics(logger, meterProvider),
		pending: sync.WaitGroup{},
	}
}

// DeliverNow attempts the given deliveries in the background right after
// their revocation committed, so callers never wait on external calls. What
// fails stays pending for the sweep.
func (d *Deliverer) DeliverNow(ctx context.Context, organizationIDs []string, ids []uuid.UUID) {
	if d == nil || len(ids) == 0 || len(organizationIDs) == 0 {
		return
	}
	ctx = context.WithoutCancel(ctx)
	d.pending.Go(func() {
		ctx, cancel := context.WithTimeout(ctx, deliveryLease)
		defer cancel()
		rows, err := repo.New(d.db).ClaimRevocationDeliveries(ctx, repo.ClaimRevocationDeliveriesParams{
			LeaseSeconds:    deliveryLease.Seconds(),
			OrganizationIds: organizationIDs,
			Ids:             ids,
		})
		if err != nil {
			d.logger.ErrorContext(ctx, "claim sign-in revocation deliveries", attr.SlogError(err))
			return
		}
		claimed := make([]repo.ClaimPendingRevocationDeliveriesRow, len(rows))
		for i, r := range rows {
			claimed[i] = repo.ClaimPendingRevocationDeliveriesRow(r)
		}
		d.deliver(ctx, claimed)
	})
}

// Wait blocks until background deliveries started by DeliverNow finish.
func (d *Deliverer) Wait() {
	if d != nil {
		d.pending.Wait()
	}
}

// Sweep retries due deliveries across every organization, one bounded
// batch, and reports how many it claimed.
func (d *Deliverer) Sweep(ctx context.Context) (int, error) {
	q := repo.New(d.db)
	rows, err := q.ClaimPendingRevocationDeliveries(ctx, repo.ClaimPendingRevocationDeliveriesParams{
		LeaseSeconds: deliveryLease.Seconds(),
		BatchSize:    SweepBatchSize,
	})
	if err != nil {
		return 0, fmt.Errorf("claim pending revocation deliveries: %w", err)
	}
	d.deliver(ctx, rows)
	backlog, err := q.RevocationDeliveryBacklog(ctx, stuckAttempts)
	if err != nil {
		d.logger.WarnContext(ctx, "read sign-in revocation backlog", attr.SlogError(err))
	} else {
		d.metrics.recordBacklog(ctx, backlog)
	}
	return len(rows), nil
}

// deliver pushes each claimed delivery outside any transaction and
// acknowledges it on its own.
func (d *Deliverer) deliver(ctx context.Context, rows []repo.ClaimPendingRevocationDeliveriesRow) {
	q := repo.New(d.db)
	failed := 0
	for _, row := range rows {
		pushErr := d.push(ctx, row)
		d.metrics.recordAttempt(ctx, row.Kind, pushErr == nil)
		var err error
		if pushErr != nil {
			failed++
			err = q.MarkRevocationDeliveryFailed(ctx, repo.MarkRevocationDeliveryFailedParams{
				LastError:      conv.ToPGText(pushErr.Error()),
				ID:             row.ID,
				OrganizationID: row.OrganizationID,
			})
		} else {
			err = q.MarkRevocationDelivered(ctx, repo.MarkRevocationDeliveredParams{ID: row.ID, OrganizationID: row.OrganizationID, EffectiveAt: row.EffectiveAt})
		}
		if err != nil {
			// The lease expires and the sweep retries the idempotent push.
			d.logger.ErrorContext(ctx, "acknowledge sign-in revocation delivery", attr.SlogError(err))
		}
	}
	if failed > 0 {
		d.logger.WarnContext(ctx, "sign-in revocation deliveries left pending", attr.SlogValueInt(failed))
	}
}

func (d *Deliverer) push(ctx context.Context, row repo.ClaimPendingRevocationDeliveriesRow) error {
	ctx, cancel := context.WithTimeout(ctx, pushTimeout)
	defer cancel()
	switch DeliveryKind(row.Kind) {
	case DeliveryJTI:
		if d.tokens == nil {
			return fmt.Errorf("no token revoker")
		}
		if err := d.tokens.RevokeToken(ctx, row.Target); err != nil {
			return fmt.Errorf("push revoked jti: %w", err)
		}
		return nil
	case DeliveryWorkOSSession:
		if d.idp == nil {
			return fmt.Errorf("no identity provider revoker")
		}
		if err := d.idp.RevokeSession(ctx, row.Target); err != nil && !workosSessionGone(err) {
			return fmt.Errorf("revoke workos session: %w", err)
		}
		return nil
	case DeliveryUserEpoch:
		if d.epochs == nil {
			return fmt.Errorf("no epoch raiser")
		}
		if !row.EffectiveAt.Valid {
			return fmt.Errorf("user epoch delivery has no effective time")
		}
		if err := d.epochs.RaiseUserEpoch(ctx, row.Target, row.EffectiveAt.Time); err != nil {
			return fmt.Errorf("raise user epoch: %w", err)
		}
		return nil
	default:
		return fmt.Errorf("unknown revocation delivery kind %q", row.Kind)
	}
}

// workosSessionGone reports a WorkOS session that no longer exists, which is
// what ending it wanted.
func workosSessionGone(err error) bool {
	if workos.IsNotFound(err) {
		return true
	}
	httpErr, ok := errors.AsType[workos_errors.HTTPError](err)
	return ok && httpErr.Code == http.StatusNotFound
}
