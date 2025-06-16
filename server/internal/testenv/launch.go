package testenv

import (
	"context"
	"fmt"
	"io"
	"log"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/testsuite"
	"golang.org/x/sync/errgroup"
)

type Environment struct {
	CloneTestDatabase PostgresDBCloneFunc
	NewRedisClient    RedisClientFunc
	NewTemporalClient func(t *testing.T) client.Client
}

func Launch(ctx context.Context) (*Environment, func() error, error) {
	pgcontainer, cloner, err := NewTestPostgres(ctx)
	if err != nil {
		log.Fatalf("start postgres container: %v", err)
		os.Exit(1)
	}

	rediscontainer, rcFactory, err := NewTestRedis(ctx)
	if err != nil {
		log.Fatalf("start redis container: %v", err)
		os.Exit(1)
	}

	res := &Environment{
		CloneTestDatabase: cloner,
		NewRedisClient:    rcFactory,
		NewTemporalClient: func(t *testing.T) client.Client {
			t.Helper()

			var stdout io.Writer
			var stderr io.Writer
			if !testing.Verbose() {
				stdout = io.Discard
				stderr = io.Discard
			}

			temporal, err := testsuite.StartDevServer(ctx, testsuite.DevServerOptions{
				LogLevel: "error",
				ClientOptions: &client.Options{
					Namespace: fmt.Sprintf("test_%s", nextRandom()),
					Logger:    NewLogger(t),
				},
				Stdout: stdout,
				Stderr: stderr,
			})
			require.NoError(t, err, "start temporal dev server")
			t.Cleanup(func() {
				require.NoError(t, temporal.Stop(), "stop temporal dev server")
			})
			return temporal.Client()
		},
	}

	return res, func() error {
		var eg errgroup.Group
		eg.Go(func() error {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			if err := pgcontainer.Terminate(ctx); err != nil {
				log.Printf("terminate postgres container: %v", err)
			}
			return nil
		})
		eg.Go(func() error {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			if err := rediscontainer.Terminate(ctx); err != nil {
				log.Printf("terminate redis container: %v", err)
			}
			return nil
		})

		return eg.Wait()
	}, nil
}
