package ssosignins

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/ssosignins/repo"
)

// DeliveryKind is what a durable revocation delivery pushes.
type DeliveryKind string

const (
	// DeliveryJTI pushes an access-token JTI into the revocation cache.
	DeliveryJTI DeliveryKind = "jti"
	// DeliveryWorkOSSession ends a WorkOS session.
	DeliveryWorkOSSession DeliveryKind = "workos_session"
)

const (
	// maxDeliveryAttempts stops retrying a delivery that keeps failing.
	maxDeliveryAttempts = 20
	// sweepBatchSize bounds one sweep transaction.
	sweepBatchSize = 200
)

// Deliverer pushes durable revocation deliveries. Both pushes are
// idempotent, so a delivery that ran but failed to mark is safely retried.
type Deliverer struct {
	logger *slog.Logger
	db     *pgxpool.Pool
	tokens TokenRevoker
	idp    IDPSessionRevoker
}

func NewDeliverer(logger *slog.Logger, db *pgxpool.Pool, tokens TokenRevoker, idp IDPSessionRevoker) *Deliverer {
	return &Deliverer{logger: logger, db: db, tokens: tokens, idp: idp}
}

// DeliverNow pushes the given deliveries right after their revocation
// committed, detached from the caller's cancellation. What fails stays
// pending for the sweep.
func (d *Deliverer) DeliverNow(ctx context.Context, organizationIDs []string, ids []uuid.UUID) {
	if d == nil || len(ids) == 0 || len(organizationIDs) == 0 {
		return
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), postCommitTimeout)
	defer cancel()
	if _, err := d.run(ctx, func(q *repo.Queries) ([]repo.ClaimPendingRevocationDeliveriesRow, error) {
		rows, err := q.ClaimRevocationDeliveries(ctx, repo.ClaimRevocationDeliveriesParams{OrganizationIds: organizationIDs, Ids: ids})
		if err != nil {
			return nil, fmt.Errorf("claim deliveries by id: %w", err)
		}
		out := make([]repo.ClaimPendingRevocationDeliveriesRow, len(rows))
		for i, r := range rows {
			out[i] = repo.ClaimPendingRevocationDeliveriesRow(r)
		}
		return out, nil
	}); err != nil {
		d.logger.ErrorContext(ctx, "deliver sso sign-in revocations", attr.SlogError(err))
	}
}

// Sweep retries pending deliveries across every organization, one bounded
// batch, and reports how many it claimed.
func (d *Deliverer) Sweep(ctx context.Context) (int, error) {
	return d.run(ctx, func(q *repo.Queries) ([]repo.ClaimPendingRevocationDeliveriesRow, error) {
		rows, err := q.ClaimPendingRevocationDeliveries(ctx, repo.ClaimPendingRevocationDeliveriesParams{MaxAttempts: maxDeliveryAttempts, BatchSize: sweepBatchSize})
		if err != nil {
			return nil, fmt.Errorf("claim pending deliveries: %w", err)
		}
		return rows, nil
	})
}

func (d *Deliverer) run(ctx context.Context, claim func(*repo.Queries) ([]repo.ClaimPendingRevocationDeliveriesRow, error)) (int, error) {
	tx, err := d.db.Begin(ctx)
	if err != nil {
		return 0, fmt.Errorf("begin: %w", err)
	}
	defer o11y.NoLogDefer(func() error { return tx.Rollback(ctx) })
	q := repo.New(tx)

	rows, err := claim(q)
	if err != nil {
		return 0, fmt.Errorf("claim revocation deliveries: %w", err)
	}
	failed := 0
	for _, row := range rows {
		if pushErr := d.push(ctx, DeliveryKind(row.Kind), row.Target); pushErr != nil {
			failed++
			if err := q.MarkRevocationDeliveryFailed(ctx, repo.MarkRevocationDeliveryFailedParams{
				LastError:      conv.ToPGText(pushErr.Error()),
				ID:             row.ID,
				OrganizationID: row.OrganizationID,
			}); err != nil {
				return 0, fmt.Errorf("mark revocation delivery failed: %w", err)
			}
			continue
		}
		if err := q.MarkRevocationDelivered(ctx, repo.MarkRevocationDeliveredParams{ID: row.ID, OrganizationID: row.OrganizationID}); err != nil {
			return 0, fmt.Errorf("mark revocation delivered: %w", err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, fmt.Errorf("commit: %w", err)
	}
	if failed > 0 {
		d.logger.WarnContext(ctx, "sso sign-in revocation deliveries left pending", attr.SlogValueInt(failed))
	}
	return len(rows), nil
}

func (d *Deliverer) push(ctx context.Context, kind DeliveryKind, target string) error {
	switch kind {
	case DeliveryJTI:
		if d.tokens == nil {
			return fmt.Errorf("no token revoker")
		}
		if err := d.tokens.RevokeToken(ctx, target); err != nil {
			return fmt.Errorf("push revoked jti: %w", err)
		}
		return nil
	case DeliveryWorkOSSession:
		if d.idp == nil {
			return fmt.Errorf("no identity provider revoker")
		}
		if err := d.idp.RevokeSession(ctx, target); err != nil {
			return fmt.Errorf("revoke workos session: %w", err)
		}
		return nil
	default:
		return fmt.Errorf("unknown revocation delivery kind %q", kind)
	}
}
