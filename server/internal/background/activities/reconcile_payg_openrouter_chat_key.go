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
	"github.com/speakeasy-api/gram/server/internal/urn"
)

type ReconcilePaygOpenRouterChatKey struct {
	logger     *slog.Logger
	db         *pgxpool.Pool
	openRouter openrouter.Provisioner
}

func NewReconcilePaygOpenRouterChatKey(logger *slog.Logger, db *pgxpool.Pool, openRouter openrouter.Provisioner) *ReconcilePaygOpenRouterChatKey {
	return &ReconcilePaygOpenRouterChatKey{logger: logger, db: db, openRouter: openRouter}
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
		return r.reconcileChatLocked(ctx, conn, queries, args)
	}); err != nil {
		return err
	}
	if args.DesiredState != openrouter.KeyDesiredStateEnabled {
		return nil
	}

	return withOpenRouterKeyBillingConnectionLock(ctx, r.logger, r.db, args.OrganizationID, openrouter.KeyTypeInternal, func(conn *pgxpool.Conn, queries *repo.Queries) error {
		return r.reconcileConvertedTrialInternalLocked(ctx, conn, queries, args.OrganizationID)
	})
}

func (r *ReconcilePaygOpenRouterChatKey) reconcileChatLocked(ctx context.Context, conn *pgxpool.Conn, queries *repo.Queries, args ReconcilePaygOpenRouterChatKeyArgs) error {
	projection, err := queries.GetPaygOpenRouterChatKeyProjection(ctx, args.OrganizationID)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return nil
	case err != nil:
		return fmt.Errorf("read PAYG Other inference key billing projection: %w", err)
	}

	hasSubscription := projection.StripeSubscriptionID.Valid && projection.StripeSubscriptionID.String != ""
	dbProvisioner, ok := r.openRouter.(openRouterKeyBillingDBProvisioner)
	if !ok {
		return errors.New("OpenRouter key provisioner cannot use the locked database session")
	}

	switch {
	case projection.GramAccountType == string(billing.TierPayg) && hasSubscription:
		if args.DesiredState != openrouter.KeyDesiredStateEnabled {
			return nil
		}
		limit, err := paygKeyLimitLocked(ctx, conn, args.OrganizationID, openrouter.KeyTypeChat)
		if err != nil {
			return err
		}
		if _, _, err := dbProvisioner.RemoveAPIKeyDisableCauseWithDB(ctx, conn, args.OrganizationID, openrouter.KeyTypeChat, openrouter.DisableCauseBillingInactive, &limit); err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("restore PAYG Other inference key billing access: %w", err)
		}
		if projection.TrialDemotedAt.Valid && projection.TrialConvertedAt.Valid {
			if _, _, err := dbProvisioner.RemoveAPIKeyDisableCauseWithDB(ctx, conn, args.OrganizationID, openrouter.KeyTypeChat, openrouter.DisableCauseTrialDemotion, &limit); err != nil && !errors.Is(err, pgx.ErrNoRows) {
				return fmt.Errorf("restore converted trial Other inference key: %w", err)
			}
		}
	case projection.GramAccountType == string(billing.TierBase) && !hasSubscription:
		if args.DesiredState != openrouter.KeyDesiredStateDisabled {
			return nil
		}
		if _, err := dbProvisioner.AddAPIKeyDisableCauseWithDB(ctx, conn, args.OrganizationID, openrouter.KeyTypeChat, openrouter.DisableCauseBillingInactive); err != nil {
			return fmt.Errorf("disable PAYG Other inference key for inactive billing: %w", err)
		}
	case projection.GramAccountType != string(billing.TierPayg) && projection.GramAccountType != string(billing.TierBase):
		return nil
	default:
		return fmt.Errorf("inconsistent PAYG Other inference key billing projection: account_type=%q has_subscription=%t", projection.GramAccountType, hasSubscription)
	}

	return nil
}

func (r *ReconcilePaygOpenRouterChatKey) reconcileConvertedTrialInternalLocked(ctx context.Context, conn *pgxpool.Conn, queries *repo.Queries, organizationID string) error {
	projection, err := queries.GetPaygOpenRouterChatKeyProjection(ctx, organizationID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read PAYG Security inference key billing projection: %w", err)
	}
	hasSubscription := projection.StripeSubscriptionID.Valid && projection.StripeSubscriptionID.String != ""
	if projection.GramAccountType != string(billing.TierPayg) || !hasSubscription || !projection.TrialDemotedAt.Valid || !projection.TrialConvertedAt.Valid {
		return nil
	}

	limit, err := paygKeyLimitLocked(ctx, conn, organizationID, openrouter.KeyTypeInternal)
	if err != nil {
		return err
	}
	dbProvisioner, ok := r.openRouter.(openRouterKeyBillingDBProvisioner)
	if !ok {
		return errors.New("OpenRouter key provisioner cannot use the locked database session")
	}
	if _, _, err := dbProvisioner.RemoveAPIKeyDisableCauseWithDB(ctx, conn, organizationID, openrouter.KeyTypeInternal, openrouter.DisableCauseTrialDemotion, &limit); err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("restore converted trial Security inference key: %w", err)
	}
	return nil
}

func paygKeyLimitLocked(ctx context.Context, conn *pgxpool.Conn, organizationID string, keyType openrouter.KeyType) (int, error) {
	limit, ok := openrouter.AccountTypeCreditLimit(billing.TierPayg)
	if !ok {
		return 0, errors.New("PAYG OpenRouter credit policy is unavailable")
	}
	chosen, err := auditrepo.New(conn).GetLatestOpenRouterSpendCapAuditOperation(ctx, auditrepo.GetLatestOpenRouterSpendCapAuditOperationParams{
		OrganizationID: organizationID,
		SubjectID:      urn.NewOpenRouterAPIKey(organizationID, string(keyType)).ID,
	})
	if err != nil {
		return 0, fmt.Errorf("read latest %s inference cap selection: %w", keyType, err)
	}
	if chosen.OperationID != "" && chosen.MonthlyCredits > 0 {
		limit = int(chosen.MonthlyCredits)
	}
	return limit, nil
}
