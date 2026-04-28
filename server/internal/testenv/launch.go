package testenv

import (
	"context"
	"errors"
	"fmt"
	"log"
	"sync"
	"testing"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
	risk_analysis "github.com/speakeasy-api/gram/server/internal/background/activities/risk_analysis"
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
	Presidio   bool
}

type Environment struct {
	CloneTestDatabase   PostgresDBCloneFunc
	NewRedisClient      RedisClientFunc
	NewClickhouseClient ClickhouseClientFunc
	NewTemporalEnv      func(t *testing.T) (env *temporal.Environment, server *testsuite.DevServer)
	NewPresidioClient   PresidioClientFunc
}

func Launch(ctx context.Context, opts LaunchOptions) (*Environment, func() error, error) {
	if !opts.Postgres && !opts.Redis && !opts.ClickHouse && !opts.Temporal && !opts.Presidio {
		return nil, nil, fmt.Errorf("launch options: %w", errCapabilityNotEnabled)
	}

	var pgcontainer terminateable
	var rediscontainer terminateable
	var clickhousecontainer terminateable
	var presidiocontainer terminateable
	var temporalserver *testsuite.DevServer
	var temporalserverErr error
	var temporalserverOnce sync.Once

	res := &Environment{
		CloneTestDatabase:   unsupportedPostgresCloneFunc(),
		NewRedisClient:      unsupportedRedisClientFunc(),
		NewClickhouseClient: unsupportedClickhouseClientFunc(),
		NewTemporalEnv:      unsupportedTemporalEnvFunc(),
		NewPresidioClient:   unsupportedPresidioClientFunc(),
	}

	var launchEg errgroup.Group

	if opts.Postgres {
		launchEg.Go(func() error {
			container, cloner, err := NewTestPostgres(ctx)
			if err != nil {
				return fmt.Errorf("start postgres container: %w", err)
			}
			pgcontainer = container
			res.CloneTestDatabase = cloner
			return nil
		})
	}

	if opts.Redis {
		launchEg.Go(func() error {
			container, rcFactory, err := NewTestRedis(ctx)
			if err != nil {
				return fmt.Errorf("start redis container: %w", err)
			}
			rediscontainer = container
			res.NewRedisClient = rcFactory
			return nil
		})
	}

	if opts.ClickHouse {
		launchEg.Go(func() error {
			container, chFactory, err := NewTestClickhouse(ctx)
			if err != nil {
				return fmt.Errorf("start clickhouse container: %w", err)
			}
			clickhousecontainer = container
			res.NewClickhouseClient = chFactory
			return nil
		})
	}

	if opts.Presidio {
		launchEg.Go(func() error {
			container, pcFactory, err := NewTestPresidio(ctx)
			if err != nil {
				return fmt.Errorf("start presidio container: %w", err)
			}
			presidiocontainer = container
			res.NewPresidioClient = pcFactory
			return nil
		})
	}

	if err := launchEg.Wait(); err != nil {
		for _, c := range []terminateable{pgcontainer, rediscontainer, clickhousecontainer, presidiocontainer} {
			if c != nil {
				ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
				_ = c.Terminate(ctx)
				cancel()
			}
		}
		return nil, nil, fmt.Errorf("start containers: %w", err)
	}

	if opts.Temporal {
		res.NewTemporalEnv = func(t *testing.T) (*temporal.Environment, *testsuite.DevServer) {
			t.Helper()

			temporalserverOnce.Do(func() {
				temporalserver, temporalserverErr = NewTemporalDevServer(ctx)
			})
			require.NoError(t, temporalserverErr, "start temporal dev server")

			env, err := NewTemporalEnvironment(t, temporalserver)
			require.NoError(t, err, "create temporal environment")

			return env, temporalserver
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
		if presidiocontainer != nil {
			eg.Go(func() error {
				ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
				defer cancel()
				if err := presidiocontainer.Terminate(ctx); err != nil {
					log.Printf("terminate presidio container: %v", err)
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

func unsupportedPresidioClientFunc() PresidioClientFunc {
	return func(t *testing.T) *risk_analysis.PresidioClient {
		t.Helper()
		t.Fatal(fmt.Errorf("new presidio client: %w", errCapabilityNotEnabled))
		return nil
	}
}

func unsupportedTemporalEnvFunc() func(t *testing.T) (env *temporal.Environment, server *testsuite.DevServer) {
	return func(t *testing.T) (*temporal.Environment, *testsuite.DevServer) {
		t.Helper()
		require.FailNow(t, fmt.Errorf("new temporal environment: %w", errCapabilityNotEnabled).Error())
		return nil, nil
	}
}
