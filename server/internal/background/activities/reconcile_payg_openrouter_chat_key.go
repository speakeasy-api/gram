package activities

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/speakeasy-api/gram/server/internal/background/activities/repo"
	"github.com/speakeasy-api/gram/server/internal/billing"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/openrouter"
)

type ReconcilePaygOpenRouterChatKey struct {
	logger     *slog.Logger
	db         *pgxpool.Pool
	openRouter openrouter.Provisioner
}

func NewReconcilePaygOpenRouterChatKey(logger *slog.Logger, db *pgxpool.Pool, openRouter openrouter.Provisioner) *ReconcilePaygOpenRouterChatKey {
	return &ReconcilePaygOpenRouterChatKey{
		logger:     logger,
		db:         db,
		openRouter: openRouter,
	}
}

type ReconcilePaygOpenRouterChatKeyArgs struct {
	OrganizationID string
	DesiredState   openrouter.KeyDesiredState
}

func (r *ReconcilePaygOpenRouterChatKey) Do(ctx context.Context, args ReconcilePaygOpenRouterChatKeyArgs) error {
	if args.OrganizationID == "" {
		return errors.New("organization ID is required")
	}
	if err := args.DesiredState.Validate(); err != nil {
		return fmt.Errorf("invalid PAYG OpenRouter chat key desired state %q", args.DesiredState)
	}

	return withOpenRouterChatKeyBillingLock(ctx, r.logger, r.db, args.OrganizationID, func(queries *repo.Queries) error {
		return r.reconcileLocked(ctx, queries, args)
	})
}

func (r *ReconcilePaygOpenRouterChatKey) reconcileLocked(ctx context.Context, queries *repo.Queries, args ReconcilePaygOpenRouterChatKeyArgs) error {
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
	case projection.GramAccountType == string(billing.TierPayg) && hasSubscription:
		if args.DesiredState != openrouter.KeyDesiredStateEnabled {
			return nil
		}
		limit, ok := openrouter.AccountTypeCreditLimit(billing.TierPayg)
		if !ok {
			return errors.New("PAYG OpenRouter credit policy is unavailable")
		}
		if _, err := r.openRouter.RefreshAPIKeyLimit(ctx, args.OrganizationID, openrouter.KeyTypeChat, &limit); errors.Is(err, pgx.ErrNoRows) {
			// Keys are provisioned lazily. Billing activation must not create one.
			return nil
		} else if err != nil {
			return fmt.Errorf("enable PAYG OpenRouter chat key: %w", err)
		}
	case projection.GramAccountType == string(billing.TierBase) && !hasSubscription:
		if args.DesiredState != openrouter.KeyDesiredStateDisabled {
			return nil
		}
		if err := r.openRouter.DisableAPIKey(ctx, args.OrganizationID, openrouter.KeyTypeChat); err != nil {
			return fmt.Errorf("disable PAYG OpenRouter chat key: %w", err)
		}
	case projection.GramAccountType != string(billing.TierPayg) && projection.GramAccountType != string(billing.TierBase):
		// Enterprise and Polar-managed tiers are outside self-serve Stripe
		// billing. Old PAYG events must not mutate their keys.
		return nil
	default:
		// The billing transition writes both halves in one transaction. A mixed
		// projection means another writer did not honor that invariant; retrying
		// is safer than granting or revoking access from partial state.
		return fmt.Errorf("inconsistent PAYG chat key billing projection: account_type=%q has_subscription=%t", projection.GramAccountType, hasSubscription)
	}

	return nil
}
