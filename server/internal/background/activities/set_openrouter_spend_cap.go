package activities

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/speakeasy-api/gram/server/internal/audit"
	auditrepo "github.com/speakeasy-api/gram/server/internal/audit/repo"
	activitiesrepo "github.com/speakeasy-api/gram/server/internal/background/activities/repo"
	"github.com/speakeasy-api/gram/server/internal/billing"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/openrouter"
	openrouterrepo "github.com/speakeasy-api/gram/server/internal/thirdparty/openrouter/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
	"go.temporal.io/sdk/activity"
)

type openRouterSpendCapAuditLogger interface {
	LogOpenRouterAPIKeySetSpendCap(context.Context, auditrepo.DBTX, audit.LogOpenRouterAPIKeySetSpendCapEvent) error
}

type SetOpenRouterSpendCap struct {
	logger     *slog.Logger
	db         *pgxpool.Pool
	openRouter openrouter.Provisioner
	audit      openRouterSpendCapAuditLogger
}

func NewSetOpenRouterSpendCap(logger *slog.Logger, db *pgxpool.Pool, openRouter openrouter.Provisioner, auditLogger openRouterSpendCapAuditLogger) *SetOpenRouterSpendCap {
	return &SetOpenRouterSpendCap{
		logger:     logger,
		db:         db,
		openRouter: openRouter,
		audit:      auditLogger,
	}
}

type SetOpenRouterSpendCapArgs struct {
	OperationID      string
	OrganizationID   string
	Limit            int
	Actor            urn.Principal
	ActorDisplayName *string
}

type setOpenRouterSpendCapHeartbeat struct {
	BeforeMonthlyCredits int64
}

func (s *SetOpenRouterSpendCap) Do(ctx context.Context, args SetOpenRouterSpendCapArgs) error {
	if args.OperationID == "" {
		return errors.New("spend-cap operation ID is required")
	}
	if args.OrganizationID == "" {
		return errors.New("organization ID is required")
	}
	if args.Limit < 1 || args.Limit > 10000 {
		return fmt.Errorf("spend cap must be between 1 and 10000: %d", args.Limit)
	}

	return withOpenRouterChatKeyBillingLock(ctx, s.logger, s.db, args.OrganizationID, func(queries *activitiesrepo.Queries) error {
		return s.setLocked(ctx, queries, args)
	})
}

func (s *SetOpenRouterSpendCap) setLocked(ctx context.Context, queries *activitiesrepo.Queries, args SetOpenRouterSpendCapArgs) error {
	projection, err := queries.GetPaygOpenRouterChatKeyProjection(ctx, args.OrganizationID)
	if err != nil {
		return fmt.Errorf("read billing state before setting OpenRouter spend cap: %w", err)
	}
	hasSubscription := projection.StripeSubscriptionID.Valid && projection.StripeSubscriptionID.String != ""
	if projection.GramAccountType != string(billing.TierPayg) || !hasSubscription {
		return fmt.Errorf("PAYG subscription required to set OpenRouter spend cap: account_type=%q has_subscription=%t", projection.GramAccountType, hasSubscription)
	}

	subject := urn.NewOpenRouterAPIKey(args.OrganizationID, string(openrouter.KeyTypeChat))
	recorded, err := auditrepo.New(s.db).HasOpenRouterSpendCapAuditOperation(ctx, auditrepo.HasOpenRouterSpendCapAuditOperationParams{
		OrganizationID: args.OrganizationID,
		SubjectID:      subject.ID,
		OperationID:    args.OperationID,
	})
	if err != nil {
		return fmt.Errorf("check spend-cap audit operation: %w", err)
	}
	if recorded {
		return nil
	}

	key, err := openrouterrepo.New(s.db).GetOpenRouterAPIKey(ctx, openrouterrepo.GetOpenRouterAPIKeyParams{
		OrganizationID: args.OrganizationID,
		KeyType:        string(openrouter.KeyTypeChat),
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return errors.New("chat key must be provisioned before setting its spend cap")
	}
	if err != nil {
		return fmt.Errorf("read chat key before setting spend cap: %w", err)
	}
	if key.Disabled {
		return errors.New("cannot set spend cap while the chat key is disabled")
	}
	before := key.MonthlyCredits
	if activity.HasHeartbeatDetails(ctx) {
		var heartbeat setOpenRouterSpendCapHeartbeat
		if err := activity.GetHeartbeatDetails(ctx, &heartbeat); err != nil {
			return fmt.Errorf("restore spend-cap operation heartbeat: %w", err)
		}
		before = heartbeat.BeforeMonthlyCredits
	} else {
		// The heartbeat is durable across activity retries. Recording the value
		// before the upstream PATCH preserves the true before snapshot if the
		// PATCH and local mirror succeed but the audit transaction later fails.
		activity.RecordHeartbeat(ctx, setOpenRouterSpendCapHeartbeat{BeforeMonthlyCredits: before})
	}

	refreshed, err := s.openRouter.RefreshAPIKeyLimit(ctx, args.OrganizationID, openrouter.KeyTypeChat, &args.Limit)
	if err != nil {
		return fmt.Errorf("refresh OpenRouter chat spend cap: %w", err)
	}

	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin spend-cap audit transaction: %w", err)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })

	auditQueries := auditrepo.New(dbtx)
	recorded, err = auditQueries.HasOpenRouterSpendCapAuditOperation(ctx, auditrepo.HasOpenRouterSpendCapAuditOperationParams{
		OrganizationID: args.OrganizationID,
		SubjectID:      subject.ID,
		OperationID:    args.OperationID,
	})
	if err != nil {
		return fmt.Errorf("recheck spend-cap audit operation: %w", err)
	}
	if !recorded {
		if err := s.audit.LogOpenRouterAPIKeySetSpendCap(ctx, dbtx, audit.LogOpenRouterAPIKeySetSpendCapEvent{
			OrganizationID:      args.OrganizationID,
			Actor:               args.Actor,
			ActorDisplayName:    args.ActorDisplayName,
			ActorSlug:           nil,
			OpenRouterAPIKeyURN: subject,
			KeyType:             string(openrouter.KeyTypeChat),
			OperationIdentifier: args.OperationID,
			OpenRouterAPIKeySnapshotBefore: &audit.OpenRouterAPIKeySpendCapSnapshot{
				MonthlyCredits: before,
			},
			OpenRouterAPIKeySnapshotAfter: &audit.OpenRouterAPIKeySpendCapSnapshot{
				MonthlyCredits: int64(refreshed),
			},
		}); err != nil {
			return fmt.Errorf("record spend-cap audit event: %w", err)
		}
	}

	if err := dbtx.Commit(ctx); err != nil {
		return fmt.Errorf("commit spend-cap audit transaction: %w", err)
	}
	return nil
}
