package testenv

import (
	"context"
	"fmt"
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
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/speakeasy-api/gram/server/internal/o11y"
)

type PostgresDBCloneFunc func(t *testing.T, name string) (*pgxpool.Pool, error)

var pgCloneMutex sync.Mutex

func nextPostgresCloneSuffix() string {
	return fmt.Sprintf("%d", uuid.New().ID())
}

func rootPath(elem ...string) string {
	_, thisFile, _, _ := runtime.Caller(0)
	serverDir := filepath.Join(filepath.Dir(thisFile), "..", "..")
	return filepath.Join(append([]string{serverDir}, elem...)...)
}

// NewTestPostgres creates a new Postgres container with a template database built
// from a SQL init script. A reference to the container is returned as well as
// a function to create test databases from the template. All clone databases
// are automatically dropped when the test ends using t.Cleanup hooks.
func NewTestPostgres(ctx context.Context) (*postgres.PostgresContainer, PostgresDBCloneFunc, error) {
	container, err := postgres.Run(
		ctx,
		"pgvector/pgvector:pg17",
		postgres.WithUsername("gotest"),
		postgres.WithPassword("gotest"),
		postgres.WithDatabase("gotestdb"),
		postgres.WithInitScripts(rootPath("database", "schema.sql")),
		testcontainers.WithWaitStrategy(
			// The log appears twice because postgres restarts itself after
			// the init-script bootstrap run.
			wait.ForLog("database system is ready to accept connections").WithOccurrence(2),
		),
		WithPublishedPortWait("5432/tcp"),
		WithoutPublishedPorts(),
		testcontainers.WithTmpfs(map[string]string{"/var/lib/postgresql/data": "rw"}),
		testcontainers.WithEnv(map[string]string{"PGDATA": "/var/lib/postgresql/data"}),
		testcontainers.WithLogger(NewTestcontainersLogger()),
	)
	if err != nil {
		return nil, nil, fmt.Errorf("start postgres container: %w", err)
	}

	uri, err := postgresURI(ctx, container)
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

func postgresURI(ctx context.Context, container *postgres.PostgresContainer) (string, error) {
	addr, err := ContainerAddr(ctx, container, "5432/tcp")
	if err != nil {
		return "", fmt.Errorf("resolve postgres address: %w", err)
	}
	return fmt.Sprintf("postgres://gotest:gotest@%s/gotestdb?sslmode=disable", addr), nil
}

func newPostgresCloneFunc(container *postgres.PostgresContainer) PostgresDBCloneFunc {
	return func(t *testing.T, name string) (*pgxpool.Pool, error) {
		t.Helper()

		ctx := t.Context()
		uri, err := postgresURI(ctx, container)
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

		cloneName := fmt.Sprintf("%s_%s", name, nextPostgresCloneSuffix())
		if _, err := conn.Exec(ctx, fmt.Sprintf("CREATE DATABASE %s WITH TEMPLATE gotestdb;", cloneName)); err != nil {
			return nil, fmt.Errorf("create test database: %w", err)
		}

		cloneURI := uri[:len(uri)-len("gotestdb?sslmode=disable")] + cloneName + "?sslmode=disable"
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

			if _, err := conn.Exec(timeoutCtx, fmt.Sprintf("DROP DATABASE %s;", cloneName)); err != nil {
				panic(fmt.Errorf("drop test database: exec: %w", err))
			}
		})

		return pool, nil
	}
}
