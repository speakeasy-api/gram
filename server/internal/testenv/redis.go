package testenv

import (
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
)

// RedisClientFunc creates a new Redis client connected to the test Redis instance.
type RedisClientFunc func(t *testing.T, db int) (*redis.Client, error)

// newRedisClientFactory creates a RedisClientFunc that connects to the test Redis
// instance. Connection details are read from TEST_REDIS_HOST and TEST_REDIS_PORT.
func newRedisClientFactory() (RedisClientFunc, error) {
	host := os.Getenv("TEST_REDIS_HOST")
	port := os.Getenv("TEST_REDIS_PORT")

	if host == "" || port == "" {
		return nil, fmt.Errorf("TEST_REDIS_HOST and TEST_REDIS_PORT environment variables must be set")
	}

	addr := fmt.Sprintf("%s:%s", host, port)

	return func(t *testing.T, db int) (*redis.Client, error) {
		t.Helper()

		client := redis.NewClient(&redis.Options{
			Addr:         addr,
			DB:           db,
			DialTimeout:  1 * time.Second,
			ReadTimeout:  300 * time.Millisecond,
			WriteTimeout: 1 * time.Second,
		})

		t.Cleanup(func() {
			if err := client.Close(); err != nil {
				t.Logf("failed to close redis client: %v", err)
			}
		})

		return client, nil
	}, nil
}
