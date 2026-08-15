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
)

func withOpenRouterChatKeyBillingLock(
	ctx context.Context,
	logger *slog.Logger,
	db *pgxpool.Pool,
	organizationID string,
	operation func(*repo.Queries) error,
) error {
	conn, err := db.Acquire(ctx)
	if err != nil {
		return fmt.Errorf("acquire connection for OpenRouter chat key billing lock: %w", err)
	}

	queries := repo.New(conn)
	if err := queries.AcquirePaygOpenRouterChatKeyLock(ctx, organizationID); err != nil {
		// The statement may have acquired the session lock before context
		// cancellation surfaced. Do not return an uncertain session to the pool.
		cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		closeErr := conn.Hijack().Close(cleanupCtx)
		cancel()
		if closeErr != nil {
			logger.ErrorContext(ctx, "close connection after OpenRouter chat key billing lock failure", attr.SlogError(closeErr))
		}
		return fmt.Errorf("acquire OpenRouter chat key billing lock: %w", err)
	}
	defer releaseOpenRouterChatKeyBillingLock(ctx, logger, conn, queries, organizationID)

	return operation(queries)
}

func releaseOpenRouterChatKeyBillingLock(ctx context.Context, logger *slog.Logger, conn *pgxpool.Conn, queries *repo.Queries, organizationID string) {
	cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()

	unlocked, err := queries.ReleasePaygOpenRouterChatKeyLock(cleanupCtx, organizationID)
	if err == nil && unlocked {
		conn.Release()
		return
	}

	if err == nil {
		err = errors.New("lock was not held by this session")
	}
	logger.ErrorContext(ctx, "release OpenRouter chat key billing lock", attr.SlogError(err))

	// A pooled connection must never be returned while it might still own a
	// session advisory lock. Closing the hijacked connection lets Postgres
	// release all of its session locks.
	hijacked := conn.Hijack()
	if closeErr := hijacked.Close(cleanupCtx); closeErr != nil {
		logger.ErrorContext(ctx, "close connection with unreleased OpenRouter chat key billing lock", attr.SlogError(closeErr))
	}
}
