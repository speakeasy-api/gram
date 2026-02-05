package testenv

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"

	"go.temporal.io/sdk/client"
)

// Environment provides factory functions for creating test database connections
// and clients. It reads connection details from environment variables set by
// `mise test:go`.
type Environment struct {
	CloneTestDatabase   PostgresDBCloneFunc
	NewRedisClient      RedisClientFunc
	NewClickhouseClient ClickhouseClientFunc
	NewTemporalClient   func(t *testing.T) client.Client
}

// requiredEnvVars lists the environment variables that must be set to run tests.
var requiredEnvVars = []string{
	"TEST_RUN_ID",
	"TEST_POSTGRES_URL",
	"TEST_REDIS_HOST",
	"TEST_REDIS_PORT",
	"TEST_CLICKHOUSE_HOST",
	"TEST_CLICKHOUSE_NATIVE_PORT",
	"TEST_TEMPORAL_ADDRESS",
}

// checkEnvVars verifies all required environment variables are set.
// Returns an error with a helpful message if any are missing.
func checkEnvVars() error {
	var missing []string
	for _, env := range requiredEnvVars {
		if os.Getenv(env) == "" {
			missing = append(missing, env)
		}
	}

	if len(missing) > 0 {
		return fmt.Errorf(`missing required environment variables: %s

tests must be run using the mise task which sets up test infrastructure:

    mise test:server ./...

or for a specific package:

    mise test:server ./internal/projects/...

the mise task starts PostgreSQL, Redis, ClickHouse, and Temporal containers
with unique names, allowing multiple test runs in parallel`, strings.Join(missing, ", "))
	}

	return nil
}

// Launch initializes the test environment by reading connection details from
// environment variables. These variables are set by `mise test:go` which starts
// the required infrastructure containers.
//
// Returns an Environment with factory functions for creating test connections,
// and a cleanup function (currently a no-op since containers are managed by mise).
func Launch(ctx context.Context) (*Environment, func() error, error) {
	if err := checkEnvVars(); err != nil {
		return nil, nil, err
	}

	pgCloner, err := newPostgresCloner(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("initialize postgres cloner: %w", err)
	}

	redisFactory, err := newRedisClientFactory()
	if err != nil {
		return nil, nil, fmt.Errorf("initialize redis factory: %w", err)
	}

	clickhouseFactory, err := newClickhouseClientFactory()
	if err != nil {
		return nil, nil, fmt.Errorf("initialize clickhouse factory: %w", err)
	}

	temporalFactory, err := newTemporalClientFactory()
	if err != nil {
		return nil, nil, fmt.Errorf("initialize temporal factory: %w", err)
	}

	res := &Environment{
		CloneTestDatabase:   pgCloner,
		NewRedisClient:      redisFactory,
		NewClickhouseClient: clickhouseFactory,
		NewTemporalClient:   temporalFactory,
	}

	// Cleanup is a no-op since containers are managed by mise test:go
	return res, func() error { return nil }, nil
}
