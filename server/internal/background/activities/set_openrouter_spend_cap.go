package activities

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/speakeasy-api/gram/server/internal/audit"
	auditrepo "github.com/speakeasy-api/gram/server/internal/audit/repo"
	activitiesrepo "github.com/speakeasy-api/gram/server/internal/background/activities/repo"
	"github.com/speakeasy-api/gram/server/internal/billing"
	"github.com/speakeasy-api/gram/server/internal/constants"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/openrouter"
	openrouterrepo "github.com/speakeasy-api/gram/server/internal/thirdparty/openrouter/repo"
	trialsrepo "github.com/speakeasy-api/gram/server/internal/trials/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/temporal"
)

type openRouterSpendCapAuditLogger interface {
	LogOpenRouterAPIKeySetSpendCap(context.Context, auditrepo.DBTX, audit.LogOpenRouterAPIKeySetSpendCapEvent) error
}

type openRouterSpendCapDBProvisioner interface {
	ReinstateAPIKeyLimitWithDB(context.Context, openrouter.DBTX, string, openrouter.KeyType, *int) (int, error)
}

const spendCapKeyBillingLockWaitTimeout = 10 * time.Second

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
	OperationID    string
	OrganizationID string
	// KeyType is empty only for pre-deployment workflow payloads and defaults
	// to the other-inference (chat) key for compatibility.
	KeyType          string
	Limit            int
	Actor            urn.Principal
	ActorDisplayName *string
}

type setOpenRouterSpendCapHeartbeat struct {
	BeforeMonthlyCredits int64
	ObservedKeyUpdatedAt time.Time
	AppliedKeyUpdatedAt  time.Time
	Applied              bool
}

func (s *SetOpenRouterSpendCap) Do(ctx context.Context, args SetOpenRouterSpendCapArgs) error {
	if args.OperationID == "" {
		return errors.New("spend-cap operation ID is required")
	}
	if args.OrganizationID == "" {
		return errors.New("organization ID is required")
	}
	keyType := openrouter.KeyType(args.KeyType).OrDefault()
	if err := keyType.Validate(); err != nil {
		return fmt.Errorf("invalid OpenRouter key type: %w", err)
	}
	if args.Limit < constants.MinimumPaygSpendCapUSD || args.Limit > constants.MaximumPaygSpendCapUSD {
		return fmt.Errorf("spend cap must be between %d and %d: %d", constants.MinimumPaygSpendCapUSD, constants.MaximumPaygSpendCapUSD, args.Limit)
	}

	return withOpenRouterKeyBillingConnectionLockTimeout(ctx, s.logger, s.db, args.OrganizationID, keyType, spendCapKeyBillingLockWaitTimeout, func(conn *pgxpool.Conn, queries *activitiesrepo.Queries) error {
		return s.setLocked(ctx, conn, queries, args, keyType)
	})
}

func (s *SetOpenRouterSpendCap) setLocked(ctx context.Context, conn *pgxpool.Conn, queries *activitiesrepo.Queries, args SetOpenRouterSpendCapArgs, keyType openrouter.KeyType) error {
	subject := urn.NewOpenRouterAPIKey(args.OrganizationID, string(keyType))
	recorded, err := auditrepo.New(conn).HasOpenRouterSpendCapAuditOperation(ctx, auditrepo.HasOpenRouterSpendCapAuditOperationParams{
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

	key, err := openrouterrepo.New(conn).GetOpenRouterAPIKey(ctx, openrouterrepo.GetOpenRouterAPIKeyParams{
		OrganizationID: args.OrganizationID,
		KeyType:        string(keyType),
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("%s key must be provisioned before setting its inference cap", keyType)
	}
	if err != nil {
		return fmt.Errorf("read %s key before setting inference cap: %w", keyType, err)
	}
	before := key.MonthlyCredits
	applied := false
	if activity.HasHeartbeatDetails(ctx) {
		var heartbeat setOpenRouterSpendCapHeartbeat
		if err := activity.GetHeartbeatDetails(ctx, &heartbeat); err != nil {
			return fmt.Errorf("restore spend-cap operation heartbeat: %w", err)
		}
		before = heartbeat.BeforeMonthlyCredits
		if heartbeat.Applied {
			if heartbeat.AppliedKeyUpdatedAt.IsZero() {
				return temporal.NewNonRetryableApplicationError(
					"restore spend-cap operation heartbeat: missing applied key generation",
					"malformed-spend-cap-heartbeat",
					nil,
				)
			}
			if !key.UpdatedAt.Time.Equal(heartbeat.AppliedKeyUpdatedAt) && key.MonthlyCredits != int64(args.Limit) {
				// Every cap application advances the mirrored key generation. A
				// different generation and value means another operation won after
				// this attempt applied the key, so this retry must not overwrite it.
				s.logger.WarnContext(ctx, "skipping superseded spend-cap retry")
				return nil
			}
			// Credits polling may advance the mirror generation while reconciling
			// the same value. This operation still owns its missing audit record.
			applied = true
		} else if heartbeat.ObservedKeyUpdatedAt.IsZero() {
			return errors.New("restore spend-cap operation heartbeat: missing observed key generation")
		} else if !key.UpdatedAt.Time.Equal(heartbeat.ObservedKeyUpdatedAt) {
			if key.MonthlyCredits != int64(args.Limit) {
				// The mirror advanced to another cap while this attempt was down.
				s.logger.WarnContext(ctx, "skipping superseded spend-cap retry")
				return nil
			}
			// The upstream PATCH and mirror write completed, but the applied
			// heartbeat was lost. The advanced generation distinguishes this from
			// an initial mirror that already happened to equal the requested cap.
			applied = true
		}
	} else {
		// The heartbeat is durable across activity retries. Recording the value
		// before the upstream PATCH preserves the true before snapshot if the
		// PATCH and local mirror succeed but the audit transaction later fails.
		// An attempt with no heartbeat has not made an external change, so it
		// remains eligible and concurrent requests stay completion-ordered.
		activity.RecordHeartbeat(ctx, setOpenRouterSpendCapHeartbeat{
			BeforeMonthlyCredits: before,
			ObservedKeyUpdatedAt: key.UpdatedAt.Time,
			AppliedKeyUpdatedAt:  time.Time{},
			Applied:              false,
		})
	}

	refreshed := args.Limit
	if !applied {
		projection, err := queries.GetPaygOpenRouterChatKeyProjection(ctx, args.OrganizationID)
		if err != nil {
			return fmt.Errorf("read billing state before setting OpenRouter spend cap: %w", err)
		}
		hasSubscription := projection.StripeSubscriptionID.Valid && projection.StripeSubscriptionID.String != ""
		if projection.GramAccountType != string(billing.TierPayg) || !hasSubscription {
			return fmt.Errorf("PAYG subscription required to set OpenRouter spend cap: account_type=%q has_subscription=%t", projection.GramAccountType, hasSubscription)
		}
		_, err = trialsrepo.New(conn).GetActiveTrial(ctx, args.OrganizationID)
		switch {
		case err == nil:
			return temporal.NewNonRetryableApplicationError("inference caps cannot be changed during an active trial", "active-trial", nil)
		case !errors.Is(err, pgx.ErrNoRows):
			return fmt.Errorf("check active trial before setting inference cap: %w", err)
		}
		if key.Disabled {
			return fmt.Errorf("cannot set inference cap while the %s key is disabled", keyType)
		}

		dbProvisioner, ok := s.openRouter.(openRouterSpendCapDBProvisioner)
		if !ok {
			return temporal.NewNonRetryableApplicationError("OpenRouter spend-cap provisioner cannot use the locked database session", "invalid-spend-cap-provisioner", nil)
		}
		refreshed, err = dbProvisioner.ReinstateAPIKeyLimitWithDB(ctx, conn, args.OrganizationID, keyType, &args.Limit)
		if err != nil {
			return fmt.Errorf("refresh OpenRouter %s inference cap: %w", keyType, err)
		}
		appliedKey, err := openrouterrepo.New(conn).GetOpenRouterAPIKey(ctx, openrouterrepo.GetOpenRouterAPIKeyParams{
			OrganizationID: args.OrganizationID,
			KeyType:        string(keyType),
		})
		if err != nil {
			return fmt.Errorf("read %s key after setting inference cap: %w", keyType, err)
		}
		activity.RecordHeartbeat(ctx, setOpenRouterSpendCapHeartbeat{
			BeforeMonthlyCredits: before,
			ObservedKeyUpdatedAt: key.UpdatedAt.Time,
			AppliedKeyUpdatedAt:  appliedKey.UpdatedAt.Time,
			Applied:              true,
		})
	}

	dbtx, err := conn.Begin(ctx)
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
			KeyType:             string(keyType),
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
