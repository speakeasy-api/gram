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

	if err := withOpenRouterKeyBillingConnectionLock(ctx, r.logger, r.db, args.OrganizationID, openrouter.KeyTypeChat, func(conn *pgxpool.Conn, queries *repo.Queries) error {
		return r.reconcileLocked(ctx, conn, queries, args)
	}); err != nil {
		return err
	}
	if args.DesiredState != openrouter.KeyDesiredStateEnabled {
		return nil
	}

	// Trial demotion disables both platform keys, while subscription loss only
	// disables Other inference. Re-enable Security inference only when it is
	// actually disabled; an ordinary recheckout therefore leaves its independent
	// cap and enabled state untouched.
	return withOpenRouterKeyBillingConnectionLock(ctx, r.logger, r.db, args.OrganizationID, openrouter.KeyTypeInternal, func(conn *pgxpool.Conn, queries *repo.Queries) error {
		return r.reenableSecurityLocked(ctx, conn, queries, args.OrganizationID)
	})
}

func (r *ReconcilePaygOpenRouterChatKey) reconcileLocked(ctx context.Context, conn *pgxpool.Conn, queries *repo.Queries, args ReconcilePaygOpenRouterChatKeyArgs) error {
	projection, err := queries.GetPaygOpenRouterChatKeyProjection(ctx, args.OrganizationID)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		// Organization deletion also removes its platform keys. An old outbox
		// delivery has no remaining desired state to apply.
		return nil
	case err != nil:
		return fmt.Errorf("read PAYG Other inference key billing projection: %w", err)
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
		key, err := openrouterrepo.New(conn).GetOpenRouterAPIKey(ctx, openrouterrepo.GetOpenRouterAPIKeyParams{
			OrganizationID: args.OrganizationID,
			KeyType:        string(openrouter.KeyTypeChat),
		})
		if errors.Is(err, pgx.ErrNoRows) {
			// Keys are provisioned lazily. Billing activation must not create one.
			return nil
		}
		if err != nil {
			return fmt.Errorf("read PAYG Other inference key: %w", err)
		}
		if !key.Disabled {
			if key.MonthlyCredits == int64(limit) {
				return nil
			}
			chosen, err := auditrepo.New(conn).GetLatestOpenRouterSpendCapAuditOperation(ctx, auditrepo.GetLatestOpenRouterSpendCapAuditOperationParams{
				OrganizationID: args.OrganizationID,
				SubjectID:      urn.NewOpenRouterAPIKey(args.OrganizationID, string(openrouter.KeyTypeChat)).ID,
			})
			if err != nil {
				return fmt.Errorf("read latest Other inference cap selection: %w", err)
			}
			if chosen.OperationID != "" {
				// A customer cap completed after this activation wake-up began. Do
				// not let a retry overwrite that newer intent with the default.
				return nil
			}
		}
		dbProvisioner, ok := r.openRouter.(openRouterSpendCapDBProvisioner)
		if !ok {
			return errors.New("OpenRouter key provisioner cannot use the locked database session")
		}
		if _, err := dbProvisioner.ReinstateAPIKeyLimitWithDB(ctx, conn, args.OrganizationID, openrouter.KeyTypeChat, &limit); errors.Is(err, pgx.ErrNoRows) {
			return nil
		} else if err != nil {
			return fmt.Errorf("enable PAYG Other inference key: %w", err)
		}
	case projection.GramAccountType == string(billing.TierBase) && !hasSubscription:
		if args.DesiredState != openrouter.KeyDesiredStateDisabled {
			return nil
		}
		dbProvisioner, ok := r.openRouter.(openRouterKeyBillingDBProvisioner)
		if !ok {
			return errors.New("OpenRouter key provisioner cannot use the locked database session")
		}
		if err := dbProvisioner.DisableAPIKeyWithDB(ctx, conn, args.OrganizationID, openrouter.KeyTypeChat); err != nil {
			return fmt.Errorf("disable PAYG Other inference key: %w", err)
		}
	case projection.GramAccountType != string(billing.TierPayg) && projection.GramAccountType != string(billing.TierBase):
		// Enterprise and Polar-managed tiers are outside self-serve Stripe
		// billing. Old PAYG events must not mutate their keys.
		return nil
	default:
		// The billing transition writes both halves in one transaction. A mixed
		// projection means another writer did not honor that invariant; retrying
		// is safer than granting or revoking access from partial state.
		return fmt.Errorf("inconsistent PAYG Other inference key billing projection: account_type=%q has_subscription=%t", projection.GramAccountType, hasSubscription)
	}

	return nil
}

func (r *ReconcilePaygOpenRouterChatKey) reenableSecurityLocked(ctx context.Context, conn *pgxpool.Conn, queries *repo.Queries, organizationID string) error {
	projection, err := queries.GetPaygOpenRouterChatKeyProjection(ctx, organizationID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read PAYG Security inference key billing projection: %w", err)
	}
	if projection.GramAccountType != string(billing.TierPayg) || !projection.StripeSubscriptionID.Valid || projection.StripeSubscriptionID.String == "" {
		return nil
	}

	key, err := openrouterrepo.New(conn).GetOpenRouterAPIKey(ctx, openrouterrepo.GetOpenRouterAPIKeyParams{
		OrganizationID: organizationID,
		KeyType:        string(openrouter.KeyTypeInternal),
	})
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && !key.Disabled) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read PAYG Security inference key: %w", err)
	}

	limit, ok := openrouter.AccountTypeCreditLimit(billing.TierPayg)
	if !ok {
		return errors.New("PAYG OpenRouter credit policy is unavailable")
	}
	chosen, err := auditrepo.New(conn).GetLatestOpenRouterSpendCapAuditOperation(ctx, auditrepo.GetLatestOpenRouterSpendCapAuditOperationParams{
		OrganizationID: organizationID,
		SubjectID:      urn.NewOpenRouterAPIKey(organizationID, string(openrouter.KeyTypeInternal)).ID,
	})
	if err != nil {
		return fmt.Errorf("read latest Security inference cap selection: %w", err)
	}
	if chosen.OperationID != "" && chosen.MonthlyCredits > 0 {
		limit = int(chosen.MonthlyCredits)
	}
	dbProvisioner, ok := r.openRouter.(openRouterSpendCapDBProvisioner)
	if !ok {
		return errors.New("OpenRouter key provisioner cannot use the locked database session")
	}
	if _, err := dbProvisioner.ReinstateAPIKeyLimitWithDB(ctx, conn, organizationID, openrouter.KeyTypeInternal, &limit); errors.Is(err, pgx.ErrNoRows) {
		return nil
	} else if err != nil {
		return fmt.Errorf("enable PAYG Security inference key: %w", err)
	}

	return nil
}
