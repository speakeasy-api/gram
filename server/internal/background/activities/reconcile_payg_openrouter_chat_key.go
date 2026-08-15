package activities

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/background/activities/repo"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/openrouter"
)

const paygOpenRouterChatCreditLimit = 100

type ReconcilePaygOpenRouterChatKey struct {
	logger     *slog.Logger
	db         *pgxpool.Pool
	openRouter openrouter.Provisioner
}

func NewReconcilePaygOpenRouterChatKey(logger *slog.Logger, db *pgxpool.Pool, openRouter openrouter.Provisioner) *ReconcilePaygOpenRouterChatKey {
	return &ReconcilePaygOpenRouterChatKey{
		logger:     logger.With(attr.SlogComponent("payg-openrouter-chat-key-reconciler")),
		db:         db,
		openRouter: openRouter,
	}
}

type ReconcilePaygOpenRouterChatKeyArgs struct {
	OrganizationID string
}

func (r *ReconcilePaygOpenRouterChatKey) Do(ctx context.Context, args ReconcilePaygOpenRouterChatKeyArgs) error {
	if args.OrganizationID == "" {
		return errors.New("organization ID is required")
	}

	conn, err := r.db.Acquire(ctx)
	if err != nil {
		return fmt.Errorf("acquire connection for PAYG chat key reconciliation: %w", err)
	}

	queries := repo.New(conn)
	if err := queries.AcquirePaygOpenRouterChatKeyLock(ctx, args.OrganizationID); err != nil {
		// The statement may have acquired the session lock before context
		// cancellation surfaced. Do not return an uncertain session to the pool.
		cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		closeErr := conn.Hijack().Close(cleanupCtx)
		cancel()
		if closeErr != nil {
			r.logger.ErrorContext(ctx, "close connection after PAYG chat key billing lock failure", attr.SlogError(closeErr))
		}
		return fmt.Errorf("acquire PAYG chat key billing lock: %w", err)
	}
	defer r.releaseLockAndConnection(ctx, conn, queries, args.OrganizationID)

	projection, err := queries.GetPaygOpenRouterChatKeyProjection(ctx, args.OrganizationID)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		// Organization deletion also removes its platform keys. An old outbox
		// delivery has no remaining desired state to apply.
		return nil
	case err != nil:
		return fmt.Errorf("read PAYG chat key billing projection: %w", err)
	}

	hasSubscription := projection.StripeSubscriptionID.Valid && projection.StripeSubscriptionID.String != ""
	switch {
	case projection.GramAccountType == "payg" && hasSubscription:
		limit := paygOpenRouterChatCreditLimit
		if _, err := r.openRouter.RefreshAPIKeyLimit(ctx, args.OrganizationID, openrouter.KeyTypeChat, &limit); errors.Is(err, pgx.ErrNoRows) {
			// Keys are provisioned lazily. Billing activation must not create one.
			return nil
		} else if err != nil {
			return fmt.Errorf("enable PAYG OpenRouter chat key: %w", err)
		}
	case projection.GramAccountType != "payg" && !hasSubscription:
		if err := r.openRouter.DisableAPIKey(ctx, args.OrganizationID, openrouter.KeyTypeChat); err != nil {
			return fmt.Errorf("disable PAYG OpenRouter chat key: %w", err)
		}
	default:
		// The billing transition writes both halves in one transaction. A mixed
		// projection means another writer did not honor that invariant; retrying
		// is safer than granting or revoking access from partial state.
		return fmt.Errorf("inconsistent PAYG chat key billing projection: account_type=%q has_subscription=%t", projection.GramAccountType, hasSubscription)
	}

	return nil
}

func (r *ReconcilePaygOpenRouterChatKey) releaseLockAndConnection(ctx context.Context, conn *pgxpool.Conn, queries *repo.Queries, organizationID string) {
	cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()

	unlocked, err := queries.ReleasePaygOpenRouterChatKeyLock(cleanupCtx, organizationID)
	if err == nil && unlocked {
		conn.Release()
		return
	}

	if err == nil {
		err = errors.New("lock was not held by this session")
	}
	r.logger.ErrorContext(ctx, "release PAYG chat key billing lock", attr.SlogError(err))

	// A pooled connection must never be returned while it might still own a
	// session advisory lock. Closing the hijacked connection lets Postgres
	// release all of its session locks.
	hijacked := conn.Hijack()
	if closeErr := hijacked.Close(cleanupCtx); closeErr != nil {
		r.logger.ErrorContext(ctx, "close connection with unreleased PAYG chat key billing lock", attr.SlogError(closeErr))
	}
}
