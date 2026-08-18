package activities

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"go.temporal.io/sdk/temporal"

	"github.com/speakeasy-api/gram/server/internal/background/activities/keybillinglock"
	"github.com/speakeasy-api/gram/server/internal/background/activities/repo"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/openrouter"
)

type openRouterKeyBillingDBProvisioner interface {
	RefreshAPIKeyLimitWithDB(context.Context, openrouter.DBTX, string, openrouter.KeyType, *int) (int, error)
	DisableAPIKeyWithDB(context.Context, openrouter.DBTX, string, openrouter.KeyType) error
	ReconcileMonthlyCreditsWithDB(context.Context, openrouter.DBTX, string, openrouter.KeyType, int64, *int64) (int64, error)
}

func withOpenRouterKeyBillingConnectionLock(
	ctx context.Context,
	logger *slog.Logger,
	db *pgxpool.Pool,
	organizationID string,
	keyType openrouter.KeyType,
	operation func(*pgxpool.Conn, *repo.Queries) error,
) error {
	err := keybillinglock.With(ctx, logger, db, organizationID, keyType, func(conn *pgxpool.Conn) error {
		return operation(conn, repo.New(conn))
	})
	return wrapOpenRouterKeyBillingLockError(err, keyType)
}

func withOpenRouterKeyBillingConnectionLockTimeout(
	ctx context.Context,
	logger *slog.Logger,
	db *pgxpool.Pool,
	organizationID string,
	keyType openrouter.KeyType,
	timeout time.Duration,
	operation func(*pgxpool.Conn, *repo.Queries) error,
) error {
	err := keybillinglock.WithAcquireTimeout(ctx, logger, db, organizationID, keyType, timeout, func(conn *pgxpool.Conn) error {
		return operation(conn, repo.New(conn))
	})
	return wrapOpenRouterKeyBillingLockError(err, keyType)
}

func wrapOpenRouterKeyBillingLockError(err error, keyType openrouter.KeyType) error {
	if err == nil {
		return nil
	}
	var applicationErr *temporal.ApplicationError
	if errors.As(err, &applicationErr) {
		return applicationErr
	}
	return fmt.Errorf("hold OpenRouter %s key billing lock: %w", keyType, err)
}
