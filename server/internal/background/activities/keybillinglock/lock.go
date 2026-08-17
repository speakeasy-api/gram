package keybillinglock

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/speakeasy-api/gram/server/internal/attr"
	activitiesrepo "github.com/speakeasy-api/gram/server/internal/background/activities/repo"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/openrouter"
)

// ErrAcquireTimeout reports that a bounded caller did not obtain the lock.
var ErrAcquireTimeout = errors.New("OpenRouter key billing lock wait timed out")

// WithAcquireTimeout bounds only the wait for the session lock. The operation
// keeps using its caller's context after the lock is acquired.
func WithAcquireTimeout(
	ctx context.Context,
	logger *slog.Logger,
	db *pgxpool.Pool,
	organizationID string,
	keyType openrouter.KeyType,
	timeout time.Duration,
	operation func(*pgxpool.Conn) error,
) error {
	lockCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	started := false
	err := With(lockCtx, logger, db, organizationID, keyType, func(conn *pgxpool.Conn) error {
		started = true
		return operation(conn)
	})
	if !started && errors.Is(lockCtx.Err(), context.DeadlineExceeded) {
		return fmt.Errorf("%w: %w", ErrAcquireTimeout, err)
	}
	return err
}

// With serializes a platform-key billing read and its upstream mutation.
func With(
	ctx context.Context,
	logger *slog.Logger,
	db *pgxpool.Pool,
	organizationID string,
	keyType openrouter.KeyType,
	operation func(*pgxpool.Conn) error,
) error {
	conn, err := db.Acquire(ctx)
	if err != nil {
		return fmt.Errorf("acquire connection for OpenRouter %s key billing lock: %w", keyType, err)
	}

	queries := activitiesrepo.New(conn)
	if err := queries.AcquireOpenRouterKeyBillingLock(ctx, activitiesrepo.AcquireOpenRouterKeyBillingLockParams{
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
	defer release(ctx, logger, conn, queries, organizationID, keyType)

	return operation(conn)
}

func release(ctx context.Context, logger *slog.Logger, conn *pgxpool.Conn, queries *activitiesrepo.Queries, organizationID string, keyType openrouter.KeyType) {
	cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()

	unlocked, err := queries.ReleaseOpenRouterKeyBillingLock(cleanupCtx, activitiesrepo.ReleaseOpenRouterKeyBillingLockParams{
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
	// session advisory lock. Closing it lets Postgres release all session locks.
	hijacked := conn.Hijack()
	if closeErr := hijacked.Close(cleanupCtx); closeErr != nil {
		logger.ErrorContext(ctx, "close connection with unreleased OpenRouter key billing lock", attr.SlogError(closeErr))
	}
}
