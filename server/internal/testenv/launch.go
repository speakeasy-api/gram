package testenv

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
	"github.com/speakeasy-api/gram/server/internal/temporal"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"go.temporal.io/sdk/testsuite"
	"golang.org/x/sync/errgroup"
)

var errCapabilityNotEnabled = errors.New("testenv capability not enabled")

type LaunchOptions struct {
	Postgres   bool
	Redis      bool
	ClickHouse bool
	Temporal   bool
}

type Environment struct {
	CloneTestDatabase   PostgresDBCloneFunc
	NewRedisClient      RedisClientFunc
	NewClickhouseClient ClickhouseClientFunc
	NewTemporalEnv      func(t *testing.T) (env *temporal.Environment)
}

func Launch(ctx context.Context, opts LaunchOptions) (*Environment, func() error, error) {
	if !opts.Postgres && !opts.Redis && !opts.ClickHouse && !opts.Temporal {
		return nil, nil, fmt.Errorf("launch options: %w", errCapabilityNotEnabled)
	}

	var pgcontainer terminateable
	var rediscontainer terminateable
	var clickhousecontainer terminateable
	var temporalserver *testsuite.DevServer
	var temporalserverErr error
	var temporalserverOnce sync.Once

	res := &Environment{
		CloneTestDatabase:   unsupportedPostgresCloneFunc(),
		NewRedisClient:      unsupportedRedisClientFunc(),
		NewClickhouseClient: unsupportedClickhouseClientFunc(),
		NewTemporalEnv:      unsupportedTemporalEnvFunc(),
	}

	if opts.Postgres {
		existing := os.Getenv("TESTENV_POSTGRES_URI")
		if existing == "" {
			container, cloner, err := NewPostgresContainer(ctx)
			if err != nil {
				return nil, nil, fmt.Errorf("start postgres container: %w", err)
			}
			pgcontainer = container
			res.CloneTestDatabase = cloner
		} else {
			res.CloneTestDatabase = newPostgresCloneFunc(existing)
		}
	}

	if opts.Redis {
		existing := os.Getenv("TESTENV_REDIS_URI")
		if existing == "" {
			container, rcFactory, err := NewRedisContainer(ctx)
			if err != nil {
				return nil, nil, fmt.Errorf("start redis container: %w", err)
			}
			rediscontainer = container
			res.NewRedisClient = rcFactory
		} else {
			res.NewRedisClient = newRedisClientFunc(existing)
		}
	}

	if opts.ClickHouse {
		existing := os.Getenv("TESTENV_CLICKHOUSE_URI")
		if existing == "" {
			container, chFactory, err := NewClickhouseContainer(ctx)
			if err != nil {
				return nil, nil, fmt.Errorf("start clickhouse container: %w", err)
			}
			clickhousecontainer = container
			res.NewClickhouseClient = chFactory
		} else {
			res.NewClickhouseClient = newClickhouseClientFunc(existing)
		}
	}

	if opts.Temporal {
		existing := os.Getenv("TESTENV_TEMPORAL_URI")
		if existing == "" {
			res.NewTemporalEnv = func(t *testing.T) *temporal.Environment {
				t.Helper()

				var uri string

				temporalserverOnce.Do(func() {
					temporalserver, uri, temporalserverErr = NewTemporalDevServer(ctx)
				})
				require.NoError(t, temporalserverErr, "start temporal dev server")

				env, err := NewTemporalEnvironment(t, uri)
				require.NoError(t, err, "create temporal environment")

				return env
			}
		} else {
			res.NewTemporalEnv = func(t *testing.T) *temporal.Environment {
				env, err := NewTemporalEnvironment(t, existing)
				require.NoError(t, err, "create temporal environment")

				return env
			}
		}
	}

	return res, func() error {
		var eg errgroup.Group
		if pgcontainer != nil {
			eg.Go(func() error {
				ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
				defer cancel()
				if err := pgcontainer.Terminate(ctx); err != nil {
					log.Printf("terminate postgres container: %v", err)
				}
				return nil
			})
		}
		if rediscontainer != nil {
			eg.Go(func() error {
				ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
				defer cancel()
				if err := rediscontainer.Terminate(ctx); err != nil {
					log.Printf("terminate redis container: %v", err)
				}
				return nil
			})
		}
		if clickhousecontainer != nil {
			eg.Go(func() error {
				ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
				defer cancel()
				if err := clickhousecontainer.Terminate(ctx); err != nil {
					log.Printf("terminate clickhouse container: %v", err)
				}
				return nil
			})
		}
		if temporalserver != nil {
			eg.Go(func() error {
				temporalserver.Client().Close()
				if err := temporalserver.Stop(); err != nil {
					log.Printf("terminate temporal dev server: %v", err)
				}
				return nil
			})
		}

		return eg.Wait()
	}, nil
}

type terminateable interface {
	Terminate(context.Context, ...testcontainers.TerminateOption) error
}

func unsupportedPostgresCloneFunc() PostgresDBCloneFunc {
	return func(_ *testing.T, _ string) (*pgxpool.Pool, error) {
		return nil, fmt.Errorf("clone postgres database: %w", errCapabilityNotEnabled)
	}
}

func unsupportedRedisClientFunc() RedisClientFunc {
	return func(_ *testing.T, _ int) (*redis.Client, error) {
		return nil, fmt.Errorf("new redis client: %w", errCapabilityNotEnabled)
	}
}

func unsupportedClickhouseClientFunc() ClickhouseClientFunc {
	return func(_ *testing.T) (clickhouse.Conn, error) {
		return nil, fmt.Errorf("new clickhouse client: %w", errCapabilityNotEnabled)
	}
}

func unsupportedTemporalEnvFunc() func(t *testing.T) (env *temporal.Environment) {
	return func(t *testing.T) *temporal.Environment {
		t.Helper()
		require.FailNow(t, fmt.Errorf("new temporal environment: %w", errCapabilityNotEnabled).Error())
		return nil
	}
}
