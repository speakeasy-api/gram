package testenv

import (
	"context"
	"fmt"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"
)

type PostgresDBCloneFunc func(t *testing.T, name string) (*pgxpool.Pool, error)

func nextPostgresCloneSuffix() string {
	return fmt.Sprintf("%d", uuid.New().ID())
}

// NewTestPostgres creates a new Postgres container with a template database built
// from a SQL init script. A reference to the container is returned as well as
// a function to create test databases from the template. All clone databases
// are automatically dropped when the test ends using t.Cleanup hooks.
func NewTestPostgres(ctx context.Context) (_ *postgres.PostgresContainer, _ PostgresDBCloneFunc, err error) {
	root, err := FindRepoRoot(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("locate Postgres test schema: %w", err)
	}
	if err := ensureDockerReady(ctx); err != nil {
		return nil, nil, fmt.Errorf("wait for docker: %w", err)
	}

	// The maintenance pool is shared by every clone and drop for this
	// container, so each test pays for a SCRAM handshake only on its own
	// pool's connections. It is closed before the container stops.
	var maintenance *pgxpool.Pool
	closeMaintenance := testcontainers.ContainerLifecycleHooks{
		PreStops: []testcontainers.ContainerHook{
			func(context.Context, testcontainers.Container) error {
				if maintenance != nil {
					maintenance.Close()
				}
				return nil
			},
		},
		PreBuilds:      nil,
		PostBuilds:     nil,
		PreCreates:     nil,
		PostCreates:    nil,
		PreStarts:      nil,
		PostStarts:     nil,
		PostReadies:    nil,
		PostStops:      nil,
		PreTerminates:  nil,
		PostTerminates: nil,
	}

	container, err := postgres.Run(
		ctx,
		"pgvector/pgvector:0.8.7-pg18-bookworm@sha256:2358fcba361ed2233a5ed81b5fe4ca779ccb304120ce531a3bf51c0ed7e2bc11",
		postgres.WithUsername("gotest"),
		postgres.WithPassword("gotest"),
		postgres.WithDatabase("gotestdb"),
		postgres.WithInitScripts(filepath.Join(root, "server", "database", "schema.sql")),
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
		testcontainers.WithAdditionalLifecycleHooks(closeMaintenance),
	)
	if err != nil {
		return nil, nil, fmt.Errorf("start postgres container: %w", err)
	}
	// The caller receives the container only on success, so terminate it if
	// any later step fails. Its PreStop hook closes the maintenance pool.
	defer func() {
		if err != nil {
			_ = container.Terminate(context.WithoutCancel(ctx))
		}
	}()

	uri, err := postgresURI(ctx, container)
	if err != nil {
		return nil, nil, fmt.Errorf("read connection string: %w", err)
	}

	maintenance, err = pgxpool.New(context.WithoutCancel(ctx), uri)
	if err != nil {
		return nil, nil, fmt.Errorf("create maintenance pool: %w", err)
	}

	if _, err := maintenance.Exec(ctx, "ALTER DATABASE gotestdb WITH is_template = true;"); err != nil {
		return nil, nil, fmt.Errorf("mark template database: %w", err)
	}

	return container, newPostgresCloneFunc(uri, maintenance), nil
}

func postgresURI(ctx context.Context, container *postgres.PostgresContainer) (string, error) {
	addr, err := ContainerAddr(ctx, container, "5432/tcp")
	if err != nil {
		return "", fmt.Errorf("resolve postgres address: %w", err)
	}
	// Keep clone creation and cleanup connections off the template: PostgreSQL
	// cannot clone it while another session (including cleanup) is connected.
	return fmt.Sprintf("postgres://gotest:gotest@%s/postgres?sslmode=disable", addr), nil
}

func newPostgresCloneFunc(uri string, maintenance *pgxpool.Pool) PostgresDBCloneFunc {
	// Clones of one template run one at a time as a precaution. Clones never
	// connect to the template and CREATE DATABASE takes only a share lock on
	// it, so the lock may be unnecessary. It covers only the CREATE statement.
	var cloneMu sync.Mutex

	return func(t *testing.T, name string) (*pgxpool.Pool, error) {
		t.Helper()

		ctx := t.Context()
		cloneName := fmt.Sprintf("%s_%s", name, nextPostgresCloneSuffix())

		cloneMu.Lock()
		_, err := maintenance.Exec(ctx, fmt.Sprintf("CREATE DATABASE %s WITH TEMPLATE gotestdb;", cloneName))
		cloneMu.Unlock()
		if err != nil {
			return nil, fmt.Errorf("create test database: %w", err)
		}

		cloneURI := uri[:len(uri)-len("postgres?sslmode=disable")] + cloneName + "?sslmode=disable"
		pool, err := pgxpool.New(ctx, cloneURI)
		if err != nil {
			return nil, fmt.Errorf("create pgx pool: %w", err)
		}

		t.Cleanup(func() {
			timeoutCtx, cancel := context.WithTimeout(context.WithoutCancel(t.Context()), 60*time.Second)
			defer cancel()

			pool.Close()

			if _, err := maintenance.Exec(timeoutCtx, fmt.Sprintf("DROP DATABASE %s WITH (FORCE);", cloneName)); err != nil {
				panic(fmt.Errorf("drop test database: exec: %w", err))
			}
		})

		return pool, nil
	}
}
