package activities

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/background/activities/repo"
	"github.com/speakeasy-api/gram/server/internal/billing"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/openrouter"
)

type RefreshOpenRouterKey struct {
	openRouter openrouter.Provisioner
	logger     *slog.Logger
	db         *pgxpool.Pool
}

func NewRefreshOpenRouterKey(logger *slog.Logger, db *pgxpool.Pool, openrouter openrouter.Provisioner) *RefreshOpenRouterKey {
	return &RefreshOpenRouterKey{
		openRouter: openrouter,
		logger:     logger,
		db:         db,
	}
}

type RefreshOpenRouterKeyArgs struct {
	OrgID string
	Limit *int
	// KeyType names which of the org's OpenRouter keys to refresh. Empty
	// resolves to the chat key, keeping in-flight workflow payloads from
	// before the field existed valid.
	KeyType string
}

func (o *RefreshOpenRouterKey) Do(ctx context.Context, args RefreshOpenRouterKeyArgs) error {
	keyType := openrouter.KeyType(args.KeyType).OrDefault()
	// Workflows started directly in Temporal bypass the entry-point check, so
	// a bad payload must fail fast here instead of erroring against the DB.
	if err := keyType.Validate(); err != nil {
		return oops.E(oops.CodeInvalid, err, "invalid openrouter key type").LogError(ctx, o.logger)
	}
	return withOpenRouterKeyBillingLock(ctx, o.logger, o.db, args.OrgID, keyType, func(queries *repo.Queries) error {
		if keyType == openrouter.KeyTypeChat {
			projection, err := queries.GetPaygOpenRouterChatKeyProjection(ctx, args.OrgID)
			if err != nil {
				return fmt.Errorf("read billing state before OpenRouter chat key refresh: %w", err)
			}

			hasSubscription := projection.StripeSubscriptionID.Valid && projection.StripeSubscriptionID.String != ""
			if (projection.GramAccountType == string(billing.TierPayg) && !hasSubscription) ||
				(projection.GramAccountType == string(billing.TierBase) && hasSubscription) {
				return fmt.Errorf("inconsistent billing state before OpenRouter chat key refresh: account_type=%q has_subscription=%t", projection.GramAccountType, hasSubscription)
			}
			if projection.GramAccountType == string(billing.TierBase) && !hasSubscription && projection.ChatKeyDisabled.Valid && projection.ChatKeyDisabled.Bool {
				// Subscription loss marks the existing chat key disabled locally
				// before publishing the deactivation event. An older refresh must
				// not reinstate it after that committed transition.
				return nil
			}
		}

		return o.refresh(ctx, args, keyType)
	})
}

func (o *RefreshOpenRouterKey) refresh(ctx context.Context, args RefreshOpenRouterKeyArgs, keyType openrouter.KeyType) error {
	limit, err := o.openRouter.RefreshAPIKeyLimit(ctx, args.OrgID, keyType, args.Limit)
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "error updating openrouter key").LogError(ctx, o.logger)
	}

	o.logger.InfoContext(ctx, "refreshed openrouter key limit",
		attr.SlogOpenRouterKeyLimit(limit),
		attr.SlogOpenRouterKeyType(string(keyType)),
	)

	return nil
}
