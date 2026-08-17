package activities

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	auditrepo "github.com/speakeasy-api/gram/server/internal/audit/repo"
	"github.com/speakeasy-api/gram/server/internal/background/activities/repo"
	"github.com/speakeasy-api/gram/server/internal/billing"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/openrouter"
	openrouterrepo "github.com/speakeasy-api/gram/server/internal/thirdparty/openrouter/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
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

	for _, keyType := range openrouter.AllKeyTypes {
		if err := withOpenRouterKeyBillingLock(ctx, r.logger, r.db, args.OrganizationID, keyType, func(queries *repo.Queries) error {
			return r.reconcileLocked(ctx, queries, args, keyType)
		}); err != nil {
			return err
		}
	}
	return nil
}

func (r *ReconcilePaygOpenRouterChatKey) reconcileLocked(ctx context.Context, queries *repo.Queries, args ReconcilePaygOpenRouterChatKeyArgs, keyType openrouter.KeyType) error {
	projection, err := queries.GetPaygOpenRouterChatKeyProjection(ctx, args.OrganizationID)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		// Organization deletion also removes its platform keys. An old outbox
		// delivery has no remaining desired state to apply.
		return nil
	case err != nil:
		return fmt.Errorf("read PAYG inference-key billing projection: %w", err)
	}

	hasSubscription := projection.StripeSubscriptionID.Valid && projection.StripeSubscriptionID.String != ""
	switch {
	case projection.GramAccountType == string(billing.TierPayg) && hasSubscription:
		if args.DesiredState != openrouter.KeyDesiredStateEnabled {
			return nil
		}
		limit, err := r.activationLimit(ctx, args.OrganizationID, keyType)
		if errors.Is(err, pgx.ErrNoRows) {
			// Platform keys are materialized on first use. Activation must not
			// invent a key solely to expose a cap control.
			return nil
		}
		if err != nil {
			return err
		}
		if _, err := r.openRouter.RefreshAPIKeyLimit(ctx, args.OrganizationID, keyType, &limit); errors.Is(err, pgx.ErrNoRows) {
			// Keys are provisioned lazily. Billing activation must not create one.
			return nil
		} else if err != nil {
			return fmt.Errorf("enable PAYG %s inference key: %w", keyType, err)
		}
	case projection.GramAccountType == string(billing.TierBase) && !hasSubscription:
		if args.DesiredState != openrouter.KeyDesiredStateDisabled {
			return nil
		}
		if keyType == openrouter.KeyTypeChat {
			if err := r.openRouter.DisableAPIKey(ctx, args.OrganizationID, keyType); err != nil {
				return fmt.Errorf("disable PAYG Other inference key: %w", err)
			}
			return nil
		}

		key, err := openrouterrepo.New(r.db).GetOpenRouterAPIKey(ctx, openrouterrepo.GetOpenRouterAPIKeyParams{
			OrganizationID: args.OrganizationID,
			KeyType:        string(keyType),
		})
		if errors.Is(err, pgx.ErrNoRows) || (err == nil && key.Disabled) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("read %s inference key before PAYG deactivation: %w", keyType, err)
		}
		limit, ok := openrouter.AccountTypeCreditLimit(billing.TierBase)
		if !ok {
			return errors.New("base-tier OpenRouter credit policy is unavailable")
		}
		if _, err := r.openRouter.RefreshAPIKeyLimit(ctx, args.OrganizationID, keyType, &limit); err != nil {
			return fmt.Errorf("restore base-tier %s inference cap: %w", keyType, err)
		}
	case projection.GramAccountType != string(billing.TierPayg) && projection.GramAccountType != string(billing.TierBase):
		// Enterprise and Polar-managed tiers are outside self-serve Stripe
		// billing. Old PAYG events must not mutate their keys.
		return nil
	default:
		// The billing transition writes both halves in one transaction. A mixed
		// projection means another writer did not honor that invariant; retrying
		// is safer than granting or revoking access from partial state.
		return fmt.Errorf("inconsistent PAYG inference-key billing projection: account_type=%q has_subscription=%t", projection.GramAccountType, hasSubscription)
	}

	return nil
}

func (r *ReconcilePaygOpenRouterChatKey) activationLimit(ctx context.Context, organizationID string, keyType openrouter.KeyType) (int, error) {
	if keyType == openrouter.KeyTypeChat {
		limit, ok := openrouter.AccountTypeCreditLimit(billing.TierPayg)
		if !ok {
			return 0, errors.New("PAYG OpenRouter credit policy is unavailable")
		}
		return limit, nil
	}

	_, err := openrouterrepo.New(r.db).GetOpenRouterAPIKey(ctx, openrouterrepo.GetOpenRouterAPIKeyParams{
		OrganizationID: organizationID,
		KeyType:        string(keyType),
	})
	if err != nil {
		return 0, fmt.Errorf("read %s inference key before PAYG activation: %w", keyType, err)
	}
	chosen, err := auditrepo.New(r.db).GetLatestOpenRouterSpendCapAuditOperation(ctx, auditrepo.GetLatestOpenRouterSpendCapAuditOperationParams{
		OrganizationID: organizationID,
		SubjectID:      urn.NewOpenRouterAPIKey(organizationID, string(keyType)).ID,
	})
	if err != nil {
		return 0, fmt.Errorf("read chosen %s inference cap before PAYG activation: %w", keyType, err)
	}
	if chosen.OperationID != "" && chosen.MonthlyCredits > 0 {
		return int(chosen.MonthlyCredits), nil
	}
	limit, ok := openrouter.AccountTypeCreditLimit(billing.TierPayg)
	if !ok {
		return 0, errors.New("PAYG OpenRouter credit policy is unavailable")
	}
	return limit, nil
}
