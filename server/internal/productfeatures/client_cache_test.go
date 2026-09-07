package productfeatures_test

import (
	"context"
	"errors"
	"net"
	"sync"
	"testing"

	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/productfeatures"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

type cancelOnRedisSetHook struct {
	cancel context.CancelFunc
	once   sync.Once
}

func (h *cancelOnRedisSetHook) DialHook(next redis.DialHook) redis.DialHook {
	return next
}

func (h *cancelOnRedisSetHook) ProcessHook(next redis.ProcessHook) redis.ProcessHook {
	return func(ctx context.Context, cmd redis.Cmder) error {
		if cmd.Name() == "set" {
			h.once.Do(h.cancel)
		}
		return next(ctx, cmd)
	}
}

func (h *cancelOnRedisSetHook) ProcessPipelineHook(next redis.ProcessPipelineHook) redis.ProcessPipelineHook {
	return next
}

type failRedisSetHook struct{}

func (failRedisSetHook) DialHook(next redis.DialHook) redis.DialHook {
	return func(ctx context.Context, network, addr string) (net.Conn, error) {
		return next(ctx, network, addr)
	}
}

func (failRedisSetHook) ProcessHook(next redis.ProcessHook) redis.ProcessHook {
	return func(ctx context.Context, cmd redis.Cmder) error {
		if cmd.Name() == "set" {
			return errors.New("injected redis set failure")
		}
		return next(ctx, cmd)
	}
}

func (failRedisSetHook) ProcessPipelineHook(next redis.ProcessPipelineHook) redis.ProcessPipelineHook {
	return func(ctx context.Context, cmds []redis.Cmder) error {
		return next(ctx, cmds)
	}
}

func TestUpdateFeatureCacheUnderLockReturnsStoreFailure(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	conn, err := infra.CloneTestDatabase(t, "featurecachefailure")
	require.NoError(t, err)
	redisClient, err := infra.NewRedisClient(t, 0)
	require.NoError(t, err)
	redisClient.AddHook(failRedisSetHook{})
	client := productfeatures.NewClient(testenv.NewLogger(t), testenv.NewTracerProvider(t), conn, redisClient)

	lockConn, release, err := client.AcquireFeatureCacheLocks(ctx, "org_cache_store_failure", []productfeatures.Feature{productfeatures.FeatureLogs})
	require.NoError(t, err)
	defer release()

	err = client.UpdateFeatureCacheUnderLock(ctx, lockConn, "org_cache_store_failure", productfeatures.FeatureLogs)
	require.ErrorContains(t, err, "store feature cache state")
}
