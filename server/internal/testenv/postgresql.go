package testenv

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/speakeasy-api/gram/server/internal/o11y"
)

func nextRandom() string {
	return fmt.Sprintf("%d", uuid.New().ID())
}

// PostgresDBCloneFunc creates a new test database cloned from the template.
// The database is automatically dropped when the test ends.
type PostgresDBCloneFunc func(t *testing.T, name string) (*pgxpool.Pool, error)

// newPostgresCloner creates a PostgresDBCloneFunc that clones the template
// database for each test. The template database URL is read from TEST_POSTGRES_URL.
// The database is expected to already be marked as a template by the mise test:server task.
//
// Uses PostgreSQL advisory locks to serialize cloning across test processes,
// allowing parallel test execution while respecting PostgreSQL's limitation
// that template databases can't be cloned while other connections exist.
//
// Connects to the "postgres" database for DDL operations (clone/drop) rather than
// gotestdb, so that these connections don't count as "other users" of the template.
func newPostgresCloner(_ context.Context) (PostgresDBCloneFunc, error) {
	baseURL := os.Getenv("TEST_POSTGRES_URL")
	if baseURL == "" {
		return nil, fmt.Errorf("TEST_POSTGRES_URL environment variable not set")
	}

	// Derive advisory lock ID from TEST_RUN_ID (hex string from first UUID segment).
	// Each test run has its own containers, so only tests within the same run need to coordinate.
	runID := os.Getenv("TEST_RUN_ID")
	if runID == "" {
		return nil, fmt.Errorf("TEST_RUN_ID environment variable not set")
	}
	lockID, err := strconv.ParseInt(runID, 16, 64)
	if err != nil {
		return nil, fmt.Errorf("parse TEST_RUN_ID as hex: %w", err)
	}

	// Use "postgres" database for DDL operations to avoid holding connections to the template
	adminURL := strings.Replace(baseURL, "gotestdb", "postgres", 1)

	return func(t *testing.T, name string) (*pgxpool.Pool, error) {
		t.Helper()
		ctx := t.Context()

		clonename := fmt.Sprintf("%s_%s", name, nextRandom())

		conn, err := pgx.Connect(ctx, adminURL)
		if err != nil {
			return nil, fmt.Errorf("connect to admin database: %w", err)
		}

		// Acquire advisory lock - blocks until no other process holds it.
		// This serializes the clone operation across all test processes in this run.
		_, err = conn.Exec(ctx, "SELECT pg_advisory_lock($1)", lockID)
		if err != nil {
			_ = conn.Close(ctx)
			return nil, fmt.Errorf("acquire advisory lock: %w", err)
		}

		// Clone the template database while holding the lock
		_, err = conn.Exec(ctx, fmt.Sprintf("CREATE DATABASE %s WITH TEMPLATE gotestdb;", clonename))

		// Always release the lock, even if clone failed
		_, unlockErr := conn.Exec(ctx, "SELECT pg_advisory_unlock($1)", lockID)
		_ = conn.Close(ctx)

		if err != nil {
			return nil, fmt.Errorf("create test database: %w", err)
		}
		if unlockErr != nil {
			return nil, fmt.Errorf("release advisory lock: %w", unlockErr)
		}

		cloneURL := strings.Replace(baseURL, "gotestdb", clonename, 1)
		pool, err := pgxpool.New(ctx, cloneURL)
		if err != nil {
			return nil, fmt.Errorf("create pgx pool: %w", err)
		}

		t.Cleanup(func() {
			timeoutCtx, cancel := context.WithTimeout(context.WithoutCancel(t.Context()), 60*time.Second)
			defer cancel()

			pool.Close()

			// Connect to postgres database for cleanup to avoid blocking other clones
			cleanupConn, err := pgx.Connect(timeoutCtx, adminURL)
			if err != nil {
				t.Logf("drop test database: connect: %v", err)
				return
			}
			defer o11y.NoLogDefer(func() error {
				return cleanupConn.Close(timeoutCtx)
			})

			_, err = cleanupConn.Exec(timeoutCtx, fmt.Sprintf("DROP DATABASE %s;", clonename))
			if err != nil {
				t.Logf("drop test database: exec: %v", err)
			}
		})

		return pool, nil
	}, nil
}
