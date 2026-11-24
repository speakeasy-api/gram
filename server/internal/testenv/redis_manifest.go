package testenv

import (
	"context"
	"net"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/redis/go-redis/v9"

	"github.com/speakeasy-api/gram/server/internal/redisrole"
)

// TestRedisKeyPrefix starts keys a test writes for its own purposes (for
// example to exercise a generic store). The manifest check below lets them
// through; every other key a test's Redis client sees must belong to a family
// in redisrole's manifest.
const TestRedisKeyPrefix = "test:"

// unclassifiedKeyCheck records keys that no family in redisrole's manifest
// declares and fails the test that sent them. Every package test that talks
// to the test Redis container goes through it, so a new key family that was
// not added to the manifest fails CI instead of silently defaulting to the
// ops role in production.
type unclassifiedKeyCheck struct {
	mu   sync.Mutex
	seen map[string]string
}

func newUnclassifiedKeyCheck(t *testing.T) *unclassifiedKeyCheck {
	t.Helper()

	check := &unclassifiedKeyCheck{mu: sync.Mutex{}, seen: map[string]string{}}
	t.Cleanup(func() {
		heads := check.heads()
		if len(heads) == 0 {
			return
		}
		t.Errorf("redis keys outside redisrole's manifest (declare their families in server/internal/redisrole/roles.go, or start test-only keys with %q): %s",
			TestRedisKeyPrefix, strings.Join(heads, ", "))
	})
	return check
}

func (c *unclassifiedKeyCheck) record(cmd redis.Cmder) {
	for _, key := range redisrole.CommandKeys(cmd) {
		if strings.HasPrefix(key, TestRedisKeyPrefix) || redisrole.Classify(key).Classified {
			continue
		}
		// The first two segments name the family without the ids after it.
		head := key
		if parts := strings.SplitAfterN(key, ":", 3); len(parts) == 3 {
			head = parts[0] + parts[1]
		}
		c.mu.Lock()
		c.seen[head] = cmd.Name()
		c.mu.Unlock()
	}
}

func (c *unclassifiedKeyCheck) heads() []string {
	c.mu.Lock()
	defer c.mu.Unlock()

	heads := make([]string, 0, len(c.seen))
	for head, command := range c.seen {
		heads = append(heads, command+" "+head)
	}
	slices.Sort(heads)
	return heads
}

func (c *unclassifiedKeyCheck) DialHook(next redis.DialHook) redis.DialHook {
	return func(ctx context.Context, network, addr string) (net.Conn, error) {
		return next(ctx, network, addr)
	}
}

func (c *unclassifiedKeyCheck) ProcessHook(next redis.ProcessHook) redis.ProcessHook {
	return func(ctx context.Context, cmd redis.Cmder) error {
		c.record(cmd)
		return next(ctx, cmd)
	}
}

func (c *unclassifiedKeyCheck) ProcessPipelineHook(next redis.ProcessPipelineHook) redis.ProcessPipelineHook {
	return func(ctx context.Context, cmds []redis.Cmder) error {
		for _, cmd := range cmds {
			c.record(cmd)
		}
		return next(ctx, cmds)
	}
}
