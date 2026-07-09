package activities

import (
	"context"
	"log/slog"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/openrouter"
)

type RefreshOpenRouterKey struct {
	openRouter openrouter.Provisioner
	logger     *slog.Logger
}

func NewRefreshOpenRouterKey(logger *slog.Logger, db *pgxpool.Pool, openrouter openrouter.Provisioner) *RefreshOpenRouterKey {
	return &RefreshOpenRouterKey{
		openRouter: openrouter,
		logger:     logger,
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
