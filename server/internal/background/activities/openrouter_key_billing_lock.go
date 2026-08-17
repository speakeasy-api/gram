package activities

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/speakeasy-api/gram/server/internal/attr"
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
	conn, err := db.Acquire(ctx)
	if err != nil {
		return fmt.Errorf("acquire connection for OpenRouter %s key billing lock: %w", keyType, err)
	}

	queries := repo.New(conn)
	if err := queries.AcquireOpenRouterKeyBillingLock(ctx, repo.AcquireOpenRouterKeyBillingLockParams{
		KeyType:        string(keyType),
		OrganizationID: organizationID,
	}); err != nil {
		// The statement may have acquired the session lock before context
		// cancellation surfaced. Do not return an uncertain session to the pool.
		cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		closeErr := conn.Hijack().Close(cleanupCtx)
		cancel()
		if closeErr != nil {
			logger.ErrorContext(ctx, "close connection after OpenRouter key billing lock failure", attr.SlogError(closeErr))
		}
		return fmt.Errorf("acquire OpenRouter %s key billing lock: %w", keyType, err)
	}
	defer releaseOpenRouterKeyBillingLock(ctx, logger, conn, queries, organizationID, keyType)

	return operation(queries)
}

func releaseOpenRouterKeyBillingLock(ctx context.Context, logger *slog.Logger, conn *pgxpool.Conn, queries *repo.Queries, organizationID string, keyType openrouter.KeyType) {
	cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()

	unlocked, err := queries.ReleaseOpenRouterKeyBillingLock(cleanupCtx, repo.ReleaseOpenRouterKeyBillingLockParams{
		KeyType:        string(keyType),
		OrganizationID: organizationID,
	})
	if err == nil && unlocked {
		conn.Release()
		return
	}

	if err == nil {
		err = errors.New("lock was not held by this session")
	}
	logger.ErrorContext(ctx, "release OpenRouter key billing lock", attr.SlogError(err))

	// A pooled connection must never be returned while it might still own a
	// session advisory lock. Closing the hijacked connection lets Postgres
	// release all of its session locks.
	hijacked := conn.Hijack()
	if closeErr := hijacked.Close(cleanupCtx); closeErr != nil {
		logger.ErrorContext(ctx, "close connection with unreleased OpenRouter key billing lock", attr.SlogError(closeErr))
	}
}
