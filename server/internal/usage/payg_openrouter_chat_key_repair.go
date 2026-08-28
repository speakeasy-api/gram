package usage

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/billing"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/openrouter"
	openrouterrepo "github.com/speakeasy-api/gram/server/internal/thirdparty/openrouter/repo"
	"github.com/speakeasy-api/gram/server/internal/usage/repo"
)

type paygOpenRouterChatKeyRepairer interface {
	AddAPIKeyDisableCauseWithDB(context.Context, openrouter.DBTX, string, openrouter.KeyType, openrouter.DisableCause) (openrouter.DisableCauseChange, error)
	RemoveAPIKeyDisableCauseWithDB(context.Context, openrouter.DBTX, string, openrouter.KeyType, openrouter.DisableCause, *int) (int, openrouter.DisableCauseChange, error)
	ReconcileAPIKeyDisabledWithDB(context.Context, openrouter.DBTX, string, openrouter.KeyType) error
}

// RepairPaygOpenRouterChatKey projects committed self-serve billing state onto
// the existing chat key. intent only fences stale asynchronous deliveries.
func RepairPaygOpenRouterChatKey(ctx context.Context, logger *slog.Logger, db *pgxpool.Pool, provisioner openrouter.Provisioner, organizationID string, intent openrouter.KeyDesiredState) error {
	if organizationID == "" {
		return errors.New("organization ID is required")
	}
	if err := intent.Validate(); err != nil {
		return fmt.Errorf("invalid PAYG OpenRouter chat key desired state %q", intent)
	}

	conn, err := db.Acquire(ctx)
	if err != nil {
		return fmt.Errorf("acquire connection for PAYG OpenRouter chat key repair: %w", err)
	}
	defer conn.Release()

	queries := repo.New(conn)
	lock := repo.AcquireOpenRouterBillingSessionLockParams{
		OrganizationID: organizationID,
		KeyType:        string(openrouter.KeyTypeChat),
	}
	if err := queries.AcquireOpenRouterBillingSessionLock(ctx, lock); err != nil {
		return fmt.Errorf("lock PAYG OpenRouter chat key repair: %w", err)
	}
	defer func() {
		cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		unlocked, unlockErr := queries.ReleaseOpenRouterBillingSessionLock(cleanupCtx, repo.ReleaseOpenRouterBillingSessionLockParams(lock))
		if unlockErr != nil {
			logger.ErrorContext(cleanupCtx, "failed to unlock PAYG OpenRouter chat key repair", attr.SlogError(unlockErr))
		} else if !unlocked {
			logger.ErrorContext(cleanupCtx, "PAYG OpenRouter chat key repair lock was not held")
		}
	}()

	projection, err := queries.GetPaygOpenRouterChatLifecycleProjection(ctx, organizationID)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return nil
	case err != nil:
		return fmt.Errorf("read PAYG OpenRouter chat key lifecycle projection: %w", err)
	}
	hasSubscription := projection.StripeSubscriptionID.Valid && projection.StripeSubscriptionID.String != ""

	switch {
	case projection.GramAccountType == string(billing.TierPayg) && hasSubscription:
		if intent != openrouter.KeyDesiredStateEnabled {
			return nil
		}
		return repairPaidOpenRouterChatKey(ctx, conn, queries, provisioner, organizationID)
	case projection.GramAccountType == string(billing.TierBase) && !hasSubscription:
		if intent != openrouter.KeyDesiredStateDisabled {
			return nil
		}
		return repairUnpaidOpenRouterChatKey(ctx, conn, provisioner, organizationID)
	case projection.GramAccountType != string(billing.TierPayg) && projection.GramAccountType != string(billing.TierBase):
		return nil
	default:
		return fmt.Errorf("inconsistent PAYG OpenRouter chat key billing projection: account_type=%q has_subscription=%t", projection.GramAccountType, hasSubscription)
	}
}

func repairPaidOpenRouterChatKey(ctx context.Context, conn *pgxpool.Conn, queries *repo.Queries, provisioner openrouter.Provisioner, organizationID string) error {
	key, err := openrouterrepo.New(conn).GetOpenRouterAPIKey(ctx, openrouterrepo.GetOpenRouterAPIKeyParams{OrganizationID: organizationID, KeyType: string(openrouter.KeyTypeChat)})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return nil
	case err != nil:
		return fmt.Errorf("read PAYG OpenRouter chat key: %w", err)
	case key.DisableCauses == nil:
		return errors.New("PAYG OpenRouter chat key disable causes are unclassified")
	}
	limit, ok := openrouter.AccountTypeCreditLimit(billing.TierPayg)
	if !ok || limit <= 0 {
		return errors.New("PAYG OpenRouter chat key credit policy is unavailable")
	}
	repairer, ok := provisioner.(paygOpenRouterChatKeyRepairer)
	if !ok {
		if reconciler, supported := provisioner.(openrouter.DisableStateReconciler); supported {
			if err := reconciler.ReconcileAPIKeyDisabled(ctx, organizationID, openrouter.KeyTypeChat); err != nil {
				return fmt.Errorf("reconcile committed PAYG OpenRouter chat key: %w", err)
			}
			return nil
		}
		return errors.New("OpenRouter key provisioner cannot repair committed PAYG lifecycle state")
	}
	if _, _, err := repairer.RemoveAPIKeyDisableCauseWithDB(ctx, conn, organizationID, openrouter.KeyTypeChat, openrouter.DisableCauseBillingInactive, &limit); err != nil {
		return fmt.Errorf("remove inactive PAYG billing cause: %w", err)
	}
	rows, err := queries.RecoverPaygOpenRouterChatKey(ctx, repo.RecoverPaygOpenRouterChatKeyParams{MonthlyCredits: int64(limit), OrganizationID: organizationID, KeyHash: key.KeyHash})
	if err != nil {
		return fmt.Errorf("refresh committed PAYG OpenRouter chat key cap: %w", err)
	}
	if rows != 1 {
		return fmt.Errorf("refresh committed PAYG OpenRouter chat key cap: updated %d rows", rows)
	}
	if err := repairer.ReconcileAPIKeyDisabledWithDB(ctx, conn, organizationID, openrouter.KeyTypeChat); err != nil {
		return fmt.Errorf("reconcile committed PAYG OpenRouter chat key: %w", err)
	}
	return nil
}

func repairUnpaidOpenRouterChatKey(ctx context.Context, conn *pgxpool.Conn, provisioner openrouter.Provisioner, organizationID string) error {
	key, err := openrouterrepo.New(conn).GetOpenRouterAPIKey(ctx, openrouterrepo.GetOpenRouterAPIKeyParams{OrganizationID: organizationID, KeyType: string(openrouter.KeyTypeChat)})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return nil
	case err != nil:
		return fmt.Errorf("read unpaid OpenRouter chat key: %w", err)
	case key.DisableCauses == nil:
		return errors.New("unpaid OpenRouter chat key disable causes are unclassified")
	}
	repairer, ok := provisioner.(paygOpenRouterChatKeyRepairer)
	if !ok {
		if reconciler, supported := provisioner.(openrouter.DisableStateReconciler); supported {
			if err := reconciler.ReconcileAPIKeyDisabled(ctx, organizationID, openrouter.KeyTypeChat); err != nil {
				return fmt.Errorf("reconcile committed unpaid OpenRouter chat key: %w", err)
			}
			return nil
		}
		return errors.New("OpenRouter key provisioner cannot repair committed PAYG lifecycle state")
	}
	if _, err := repairer.AddAPIKeyDisableCauseWithDB(ctx, conn, organizationID, openrouter.KeyTypeChat, openrouter.DisableCauseBillingInactive); err != nil {
		return fmt.Errorf("add inactive PAYG billing cause: %w", err)
	}
	if err := repairer.ReconcileAPIKeyDisabledWithDB(ctx, conn, organizationID, openrouter.KeyTypeChat); err != nil {
		return fmt.Errorf("reconcile committed unpaid OpenRouter chat key: %w", err)
	}
	return nil
}
