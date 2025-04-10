package dbtest

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/speakeasy-api/gram/internal/must"
	"github.com/speakeasy-api/gram/internal/o11y"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"
)

type PostgresDBCloneFunc func(t *testing.T, name string) (*pgxpool.Pool, error)

// NewPostgres creates a new Postgres container with a template database built
// from a SQL init script. A reference to the container is returned as well as
// a function to create test databases from the template. All "clone" databases
// are automatically dropped when the test ends using t.Cleanup() hooks.
func NewPostgres(ctx context.Context) (*postgres.PostgresContainer, PostgresDBCloneFunc, error) {
	container, err := postgres.Run(
		ctx,
		"postgres:17",
		postgres.WithUsername("gotest"),
		postgres.WithPassword("gotest"),
		postgres.WithDatabase("gotestdb"),
		postgres.WithInitScripts(filepath.Join("..", "..", "database", "schema.sql")),
		testcontainers.WithWaitStrategy(
			wait.ForLog("database system is ready to accept connections").
				WithOccurrence(2).
				WithStartupTimeout(30*time.Second)),
	)
	if err != nil {
		return nil, nil, err
	}

	uri, err := container.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		return nil, nil, err
	}

	conn, err := pgx.Connect(ctx, uri)
	if err != nil {
		return nil, nil, err
	}
	defer o11y.NoLogDefer(conn.Close(ctx))

	_, err = conn.Exec(ctx, "ALTER DATABASE gotestdb WITH is_template = true")
	if err != nil {
		return nil, nil, err
	}

	return container, newPostgresCloneFunc(container), nil
}

func newPostgresCloneFunc(container *postgres.PostgresContainer) PostgresDBCloneFunc {
	return func(t *testing.T, name string) (*pgxpool.Pool, error) {
		t.Helper()
		ctx := t.Context()
		uri, err := container.ConnectionString(ctx, "sslmode=disable")
		if err != nil {
			return nil, fmt.Errorf("read connection string: %w", err)
		}

		conn, err := pgx.Connect(ctx, uri)
		if err != nil {
			return nil, fmt.Errorf("connect to template database: %w", err)
		}
		defer o11y.NoLogDefer(conn.Close(ctx))

		clonename := fmt.Sprintf("%s_%s", name, must.Value(uuid.NewV7()))
		_, err = conn.Exec(ctx, fmt.Sprintf("CREATE DATABASE %s WITH TEMPLATE gotestdb;", clonename))
		if err != nil {
			return nil, fmt.Errorf("create test database: %w", err)
		}

		cloneuri := strings.Replace(uri, "gotestdb", clonename, 1)
		pool, err := pgxpool.New(ctx, cloneuri)
		if err != nil {
			return nil, fmt.Errorf("create pgx pool: %w", err)
		}

		t.Cleanup(func() {
			timeoutCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			pool.Close()

			conn, err := pgx.Connect(timeoutCtx, uri)
			if err != nil {
				panic(fmt.Errorf("drop test database: connect: %w", err))
			}
			defer o11y.NoLogDefer(conn.Close(timeoutCtx))

			_, err = conn.Exec(timeoutCtx, fmt.Sprintf("DROP DATABASE %s;", clonename))
			if err != nil {
				panic(fmt.Errorf("drop test database: exec: %w", err))
			}
		})

		return pool, nil
	}
}
