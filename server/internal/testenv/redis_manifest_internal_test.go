package testenv

import (
	"sync"
	"testing"

	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

func TestUnclassifiedKeyCheckRecordsOnlyUndeclaredFamilies(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	check := &unclassifiedKeyCheck{mu: sync.Mutex{}, seen: map[string]string{}}

	check.record(redis.NewStringCmd(ctx, "get", "sessions:v2:sid:"))
	check.record(redis.NewStringCmd(ctx, "set", "toolset:a:b:1:", "v"))
	check.record(redis.NewStringCmd(ctx, "set", TestRedisKeyPrefix+"anything", "v"))
	check.record(redis.NewStringCmd(ctx, "ping"))
	require.Empty(t, check.heads())

	check.record(redis.NewStringCmd(ctx, "set", "undeclared:family:id-1", "v"))
	check.record(redis.NewStringCmd(ctx, "set", "undeclared:family:id-2", "v"))
	check.record(redis.NewIntCmd(ctx, "del", "sessions:v2:sid:", "other"))
	require.Equal(t, []string{"del other", "set undeclared:family:"}, check.heads())
}
