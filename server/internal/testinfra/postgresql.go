package testinfra

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"

	"github.com/speakeasy-api/gram/server/internal/o11y"
)

type PostgresDBCloneFunc func(t *testing.T, name string) (*pgxpool.Pool, error)

var pgCloneMutex sync.Mutex

func nextRandom() string {
	return fmt.Sprintf("%d", uuid.New().ID())
}

func rootPath(elem ...string) string {
	_, thisFile, _, _ := runtime.Caller(0)
	serverDir := filepath.Join(filepath.Dir(thisFile), "..", "..")
	return filepath.Join(append([]string{serverDir}, elem...)...)
}

func NewTestPostgres(ctx context.Context) (*postgres.PostgresContainer, PostgresDBCloneFunc, error) {
	container, err := postgres.Run(
		ctx,
		"pgvector/pgvector:pg17",
		postgres.WithUsername("gotest"),
		postgres.WithPassword("gotest"),
		postgres.WithDatabase("gotestdb"),
		postgres.WithInitScripts(rootPath("database", "schema.sql")),
		postgres.BasicWaitStrategies(),
		testcontainers.WithTmpfs(map[string]string{"/var/lib/postgresql/data": "rw"}),
		testcontainers.WithEnv(map[string]string{"PGDATA": "/var/lib/postgresql/data"}),
		testcontainers.WithLogger(NewTestcontainersLogger(os.Getenv("LOG_LEVEL"))),
	)
	if err != nil {
		return nil, nil, fmt.Errorf("start postgres container: %w", err)
	}

	uri, err := container.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		return nil, nil, fmt.Errorf("read connection string: %w", err)
	}

	conn, err := pgx.Connect(ctx, uri)
	if err != nil {
		return nil, nil, fmt.Errorf("connect to template database: %w", err)
	}
	defer o11y.NoLogDefer(func() error { return conn.Close(ctx) })

	if _, err := conn.Exec(ctx, "ALTER DATABASE gotestdb WITH is_template = true;"); err != nil {
		return nil, nil, fmt.Errorf("mark template database: %w", err)
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

		pgCloneMutex.Lock()
		defer pgCloneMutex.Unlock()

		conn, err := pgx.Connect(ctx, uri)
		if err != nil {
			return nil, fmt.Errorf("connect to template database: %w", err)
		}
		defer o11y.NoLogDefer(func() error { return conn.Close(ctx) })

		clonename := fmt.Sprintf("%s_%s", name, nextRandom())
		if _, err := conn.Exec(ctx, fmt.Sprintf("CREATE DATABASE %s WITH TEMPLATE gotestdb;", clonename)); err != nil {
			return nil, fmt.Errorf("create test database: %w", err)
		}

		cloneURI := uri[:len(uri)-len("gotestdb?sslmode=disable")] + clonename + "?sslmode=disable"
		pool, err := pgxpool.New(ctx, cloneURI)
		if err != nil {
			return nil, fmt.Errorf("create pgx pool: %w", err)
		}

		t.Cleanup(func() {
			timeoutCtx, cancel := context.WithTimeout(context.WithoutCancel(t.Context()), 60*time.Second)
			defer cancel()

			pool.Close()

			conn, err := pgx.Connect(timeoutCtx, uri)
			if err != nil {
				panic(fmt.Errorf("drop test database: connect: %w", err))
			}
			defer o11y.NoLogDefer(func() error { return conn.Close(timeoutCtx) })

			if _, err := conn.Exec(timeoutCtx, fmt.Sprintf("DROP DATABASE %s;", clonename)); err != nil {
				panic(fmt.Errorf("drop test database: exec: %w", err))
			}
		})

		return pool, nil
	}
}
