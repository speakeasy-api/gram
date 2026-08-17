package activities

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/jackc/pgx/v5/pgxpool"
	"go.temporal.io/sdk/temporal"

	"github.com/speakeasy-api/gram/server/internal/background/activities/keybillinglock"
	"github.com/speakeasy-api/gram/server/internal/background/activities/repo"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/openrouter"
)

func withOpenRouterKeyBillingLock(
	ctx context.Context,
	logger *slog.Logger,
	db *pgxpool.Pool,
	organizationID string,
	keyType openrouter.KeyType,
	operation func(*repo.Queries) error,
) error {
	if err := keybillinglock.With(ctx, logger, db, organizationID, keyType, func(conn *pgxpool.Conn) error {
		return operation(repo.New(conn))
	}); err != nil {
		var applicationErr *temporal.ApplicationError
		if errors.As(err, &applicationErr) {
			return applicationErr
		}
		return fmt.Errorf("hold OpenRouter %s key billing lock: %w", keyType, err)
	}
	return nil
}
