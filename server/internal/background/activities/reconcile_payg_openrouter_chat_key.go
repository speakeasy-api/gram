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
	"github.com/speakeasy-api/gram/server/internal/usage"
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

	if err := usage.RepairPaygOpenRouterChatKey(ctx, r.logger, r.db, r.openRouter, args.OrganizationID, args.DesiredState); err != nil {
		return fmt.Errorf("repair PAYG OpenRouter chat key: %w", err)
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
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && !openrouter.EffectiveDisabled(key.Disabled, key.DisableCauses)) {
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
