package cache

import (
	"context"
	"fmt"
	"time"

	"github.com/speakeasy-api/gram/server/internal/redisrole"
)

var (
	_ EvictableCache       = (*EvictableRedisAdapter)(nil)
	_ CompareAndSwapCache  = (*EvictableRedisAdapter)(nil)
	_ ConditionalCache     = (*EvictableRedisAdapter)(nil)
	_ RenewableLeaseCache  = (*EvictableRedisAdapter)(nil)
	_ evictableLeaseClaims = (*EvictableRedisAdapter)(nil)
)

// EvictableCache is a Cache stored on the evictable Redis role. Constructors
// of key families that rebuild on a miss take this type, so they cannot be
// handed the sessions adapter.
type EvictableCache interface {
	Cache

	// EvictableRole marks the implementation as stored on the evictable
	// role. RedisCacheAdapter does not implement it.
	EvictableRole()
}

// evictableLeaseClaims lists the RedisCacheAdapter methods outside the
// optional interfaces that EvictableRedisAdapter also provides.
type evictableLeaseClaims interface {
	ClaimLease(ctx context.Context, key, owner string, ttl time.Duration) (bool, error)
	ReleaseLease(ctx context.Context, key, owner string) error
	ScanKeys(ctx context.Context, prefix string) ([]string, error)
}

// EvictableRedisAdapter is the Redis adapter for the evictable role. Every
// method checks that its keys carry a declared evictable prefix before the
// command is sent; in enforce mode a key of the sessions role returns
// redisrole.ErrWrongRole.
//
// It does not implement MarkingConsumeCache: the markers that operation sets
// are sessions-role keys, so no evictable family can use it.
type EvictableRedisAdapter struct {
	inner *RedisCacheAdapter
	guard *redisrole.Guard
}

// NewEvictableRedisAdapter returns the adapter for the evictable client,
// checking keys with guard.
func NewEvictableRedisAdapter(client redisrole.EvictableClient, guard *redisrole.Guard) *EvictableRedisAdapter {
	return &EvictableRedisAdapter{
		inner: NewRedisCacheAdapter(client.Client),
		guard: guard,
	}
}

// EvictableRole implements [EvictableCache].
func (e *EvictableRedisAdapter) EvictableRole() {}

func (e *EvictableRedisAdapter) check(ctx context.Context, operation string, keys ...string) error {
	for _, key := range keys {
		if err := e.guard.Check(ctx, redisrole.RoleEvictable, operation, key); err != nil {
			return fmt.Errorf("evictable %s: %w", operation, err)
		}
	}
	return nil
}

func (e *EvictableRedisAdapter) Get(ctx context.Context, key string, value any) error {
	if err := e.check(ctx, "get", key); err != nil {
		return err
	}
	return e.inner.Get(ctx, key, value)
}

func (e *EvictableRedisAdapter) GetAndDelete(ctx context.Context, key string, value any) error {
	if err := e.check(ctx, "getdel", key); err != nil {
		return err
	}
	return e.inner.GetAndDelete(ctx, key, value)
}

func (e *EvictableRedisAdapter) Set(ctx context.Context, key string, value any, ttl time.Duration) error {
	if err := e.check(ctx, "set", key); err != nil {
		return err
	}
	return e.inner.Set(ctx, key, value, ttl)
}

func (e *EvictableRedisAdapter) SetIfAbsent(ctx context.Context, key string, value any, ttl time.Duration) (bool, error) {
	if err := e.check(ctx, "setnx", key); err != nil {
		return false, err
	}
	return e.inner.SetIfAbsent(ctx, key, value, ttl)
}

func (e *EvictableRedisAdapter) Add(ctx context.Context, key string, ttl time.Duration) (bool, error) {
	if err := e.check(ctx, "add", key); err != nil {
		return false, err
	}
	return e.inner.Add(ctx, key, ttl)
}

func (e *EvictableRedisAdapter) ClaimLease(ctx context.Context, key, owner string, ttl time.Duration) (bool, error) {
	if err := e.check(ctx, "claim_lease", key); err != nil {
		return false, err
	}
	return e.inner.ClaimLease(ctx, key, owner, ttl)
}

func (e *EvictableRedisAdapter) AcquireLease(ctx context.Context, key, owner string, ttl time.Duration) (bool, error) {
	if err := e.check(ctx, "acquire_lease", key); err != nil {
		return false, err
	}
	return e.inner.AcquireLease(ctx, key, owner, ttl)
}

func (e *EvictableRedisAdapter) RenewLease(ctx context.Context, key, owner string, ttl time.Duration) (bool, error) {
	if err := e.check(ctx, "renew_lease", key); err != nil {
		return false, err
	}
	return e.inner.RenewLease(ctx, key, owner, ttl)
}

func (e *EvictableRedisAdapter) ReleaseLease(ctx context.Context, key, owner string) error {
	if err := e.check(ctx, "release_lease", key); err != nil {
		return err
	}
	return e.inner.ReleaseLease(ctx, key, owner)
}

func (e *EvictableRedisAdapter) ReleaseLeaseIfOwner(ctx context.Context, key, owner string) (bool, error) {
	if err := e.check(ctx, "release_lease", key); err != nil {
		return false, err
	}
	return e.inner.ReleaseLeaseIfOwner(ctx, key, owner)
}

func (e *EvictableRedisAdapter) CompareAndSwap(ctx context.Context, key string, expected, replacement any, ttl time.Duration) (bool, error) {
	if err := e.check(ctx, "compare_and_swap", key); err != nil {
		return false, err
	}
	return e.inner.CompareAndSwap(ctx, key, expected, replacement, ttl)
}

func (e *EvictableRedisAdapter) CompareAndDelete(ctx context.Context, key string, expected any) (bool, error) {
	if err := e.check(ctx, "compare_and_delete", key); err != nil {
		return false, err
	}
	return e.inner.CompareAndDelete(ctx, key, expected)
}

func (e *EvictableRedisAdapter) Mutate(ctx context.Context, key string, value any, ttl time.Duration, fn func(exists bool) error) error {
	if err := e.check(ctx, "mutate", key); err != nil {
		return err
	}
	return e.inner.Mutate(ctx, key, value, ttl, fn)
}

func (e *EvictableRedisAdapter) Update(ctx context.Context, key string, value any) error {
	if err := e.check(ctx, "update", key); err != nil {
		return err
	}
	return e.inner.Update(ctx, key, value)
}

func (e *EvictableRedisAdapter) Expire(ctx context.Context, key string, ttl time.Duration) error {
	if err := e.check(ctx, "expire", key); err != nil {
		return err
	}
	return e.inner.Expire(ctx, key, ttl)
}

func (e *EvictableRedisAdapter) Delete(ctx context.Context, key string) error {
	if err := e.check(ctx, "del", key); err != nil {
		return err
	}
	return e.inner.Delete(ctx, key)
}

func (e *EvictableRedisAdapter) ListAppend(ctx context.Context, key string, value any, ttl time.Duration) error {
	if err := e.check(ctx, "rpush", key); err != nil {
		return err
	}
	return e.inner.ListAppend(ctx, key, value, ttl)
}

func (e *EvictableRedisAdapter) ListRange(ctx context.Context, key string, start, stop int64, value any) error {
	if err := e.check(ctx, "lrange", key); err != nil {
		return err
	}
	return e.inner.ListRange(ctx, key, start, stop, value)
}

func (e *EvictableRedisAdapter) ScanKeys(ctx context.Context, prefix string) ([]string, error) {
	if err := e.check(ctx, "scan", prefix); err != nil {
		return nil, err
	}
	return e.inner.ScanKeys(ctx, prefix)
}

func (e *EvictableRedisAdapter) DeleteByPrefix(ctx context.Context, prefix string) error {
	if err := e.check(ctx, "scan", prefix); err != nil {
		return err
	}
	return e.inner.DeleteByPrefix(ctx, prefix)
}
