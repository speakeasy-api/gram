package signins

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"slices"
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
		// Claimed and pushed in chunks that fit the lease, so no claimed
		// delivery outlives its lease while earlier ones are pushed.
		for chunk := range slices.Chunk(ids, SweepBatchSize) {
			d.deliverChunk(ctx, organizationIDs, chunk)
		}
	})
}

func (d *Deliverer) deliverChunk(ctx context.Context, organizationIDs []string, ids []uuid.UUID) {
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
}

// Drain waits for background deliveries started by DeliverNow, until ctx
// ends; whatever is cut off stays pending for the sweep.
func (d *Deliverer) Drain(ctx context.Context) {
	if d == nil {
		return
	}
	done := make(chan struct{})
	go func() {
		d.pending.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-ctx.Done():
	}
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

// Push outcomes.
const (
	pushDelivered = "ok"
	pushFailed    = "failed"
	// pushSkipped is a WorkOS session another organization still uses.
	pushSkipped = "skipped"
	// pushRejected is a WorkOS session WorkOS refuses to end for good.
	pushRejected = "rejected"
)

// deliver pushes each claimed delivery outside any transaction and
// acknowledges it on its own. An acknowledgement that finds the delivery
// superseded (its effective time moved) only drops this attempt's lease.
func (d *Deliverer) deliver(ctx context.Context, rows []repo.ClaimPendingRevocationDeliveriesRow) {
	q := repo.New(d.db)
	failed := 0
	for _, row := range rows {
		outcome, pushErr := d.push(ctx, row)
		d.metrics.recordAttempt(ctx, row.Kind, outcome)
		var acked int64
		var err error
		if pushErr != nil {
			failed++
			d.logger.WarnContext(ctx, "sign-in revocation delivery failed",
				attr.SlogSignInDeliveryKind(row.Kind),
				attr.SlogValueInt(int(row.Attempts)+1),
				attr.SlogHTTPResponseStatusCode(statusCode(pushErr)),
			)
			acked, err = q.MarkRevocationDeliveryFailed(ctx, repo.MarkRevocationDeliveryFailedParams{
				LastError:      conv.ToPGText(pushErr.Error()),
				ID:             row.ID,
				OrganizationID: row.OrganizationID,
				EffectiveAt:    row.EffectiveAt,
			})
		} else {
			acked, err = q.MarkRevocationDelivered(ctx, repo.MarkRevocationDeliveredParams{ID: row.ID, OrganizationID: row.OrganizationID, EffectiveAt: row.EffectiveAt})
		}
		if err == nil && acked == 0 {
			err = q.ReleaseRevocationDelivery(ctx, repo.ReleaseRevocationDeliveryParams{ID: row.ID, OrganizationID: row.OrganizationID})
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

func (d *Deliverer) push(ctx context.Context, row repo.ClaimPendingRevocationDeliveriesRow) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, pushTimeout)
	defer cancel()
	switch DeliveryKind(row.Kind) {
	case DeliveryJTI:
		if d.tokens == nil {
			return pushFailed, fmt.Errorf("no token revoker")
		}
		if err := d.tokens.RevokeToken(ctx, row.Target); err != nil {
			return pushFailed, fmt.Errorf("push revoked jti: %w", err)
		}
		return pushDelivered, nil
	case DeliveryWorkOSSession:
		if d.idp == nil {
			return pushFailed, fmt.Errorf("no identity provider revoker")
		}
		// Rechecked at delivery: the session may have been recorded in
		// another organization since this was queued, and organization-scoped
		// revocations must not end it there. A recording that lands between
		// this check and the call below is not serialized with it: the worst
		// case ends that WorkOS session, which signs the user out of WorkOS
		// there; their sign-in and grants stay valid (neither depends on the
		// WorkOS session) and the next login is fresh.
		live, err := repo.New(d.db).WorkOSSessionLiveOutsideOrganization(ctx, repo.WorkOSSessionLiveOutsideOrganizationParams{WorkosSessionID: row.Target, OrganizationID: row.OrganizationID})
		if err != nil {
			return pushFailed, fmt.Errorf("check workos session use elsewhere: %w", err)
		}
		if live {
			return pushSkipped, nil
		}
		err = d.idp.RevokeSession(ctx, row.Target)
		switch {
		case err == nil, workosSessionGone(err):
			return pushDelivered, nil
		case workosSessionRejected(err):
			return pushRejected, nil
		default:
			return pushFailed, fmt.Errorf("revoke workos session: %w", err)
		}
	case DeliveryUserEpoch:
		if d.epochs == nil {
			return pushFailed, fmt.Errorf("no epoch raiser")
		}
		if !row.EffectiveAt.Valid {
			return pushFailed, fmt.Errorf("user epoch delivery has no effective time")
		}
		if err := d.epochs.RaiseUserEpoch(ctx, row.Target, row.EffectiveAt.Time); err != nil {
			return pushFailed, fmt.Errorf("raise user epoch: %w", err)
		}
		return pushDelivered, nil
	default:
		return pushFailed, fmt.Errorf("unknown revocation delivery kind %q", row.Kind)
	}
}

// statusCode is the HTTP status behind a WorkOS error, or 0.
func statusCode(err error) int {
	if apiErr, ok := errors.AsType[*workos.APIError](err); ok {
		return apiErr.StatusCode
	}
	if httpErr, ok := errors.AsType[workos_errors.HTTPError](err); ok {
		return httpErr.Code
	}
	return 0
}

// workosSessionRejected reports WorkOS refusing the session id itself
// (malformed or foreign), which no retry fixes.
func workosSessionRejected(err error) bool {
	code := statusCode(err)
	return code == http.StatusBadRequest || code == http.StatusUnprocessableEntity
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
