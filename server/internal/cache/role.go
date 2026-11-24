package cache

import (
	"context"
	"fmt"
	"slices"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/speakeasy-api/gram/server/internal/redisrole"
)

var (
	_ Cache               = (*RoleRedisAdapter)(nil)
	_ CompareAndSwapCache = (*RoleRedisAdapter)(nil)
	_ ConditionalCache    = (*RoleRedisAdapter)(nil)
	_ RenewableLeaseCache = (*RoleRedisAdapter)(nil)
	_ MarkingConsumeCache = (*RoleRedisAdapter)(nil)
	_ leaseClaims         = (*RoleRedisAdapter)(nil)

	_ EvictableCache      = (*EvictableRedisAdapter)(nil)
	_ CompareAndSwapCache = (*EvictableRedisAdapter)(nil)
	_ ConditionalCache    = (*EvictableRedisAdapter)(nil)
	_ RenewableLeaseCache = (*EvictableRedisAdapter)(nil)
	_ leaseClaims         = (*EvictableRedisAdapter)(nil)
)

// EvictableCache is a Cache stored on the evictable Redis role. Constructors
// of key families that rebuild on a miss take this type, so they cannot be
// handed the adapter of another role.
type EvictableCache interface {
	Cache

	// EvictableRole marks the implementation as stored on the evictable
	// role. RedisCacheAdapter and RoleRedisAdapter do not implement it.
	EvictableRole()
}

// leaseClaims lists the RedisCacheAdapter methods outside the optional
// interfaces that the role adapters also provide.
type leaseClaims interface {
	ClaimLease(ctx context.Context, key, owner string, ttl time.Duration) (bool, error)
	ReleaseLease(ctx context.Context, key, owner string) error
	ScanKeys(ctx context.Context, prefix string) ([]string, error)
}

// roleAdapter checks every key against its role before sending the command
// through a RedisCacheAdapter. In enforce mode a key of another role, or one
// the manifest does not declare, returns redisrole.ErrWrongRole; in report
// mode it is counted and sent.
type roleAdapter struct {
	inner *RedisCacheAdapter
	guard *redisrole.Guard
	role  redisrole.Role
}

func (a *roleAdapter) check(ctx context.Context, operation string, keys ...string) error {
	for _, key := range keys {
		if err := a.guard.Check(ctx, a.role, operation, key); err != nil {
			return fmt.Errorf("%s cache %s: %w", a.role, operation, err)
		}
	}
	return nil
}

// RoleRedisAdapter is the Redis adapter for the auth or ops role. Every
// method checks that its keys belong to the adapter's role before the command
// is sent. It works on a client shared with other roles, so a family wired to
// the wrong role is caught even before the roles have their own instances.
type RoleRedisAdapter struct {
	roleAdapter
}

// NewRoleRedisAdapter returns the adapter of role for client, checking keys
// with guard. Use NewEvictableRedisAdapter for the evictable role.
func NewRoleRedisAdapter(client *redis.Client, role redisrole.Role, guard *redisrole.Guard) *RoleRedisAdapter {
	return &RoleRedisAdapter{roleAdapter: roleAdapter{inner: NewRedisCacheAdapter(client), guard: guard, role: role}}
}

// Role reports the role the adapter stores keys on.
func (r *RoleRedisAdapter) Role() redisrole.Role {
	return r.role
}

// GetAndDeleteMarking implements [MarkingConsumeCache]. The consumed key and
// the marker are written by one script, so both must belong to the adapter's
// role.
func (r *RoleRedisAdapter) GetAndDeleteMarking(ctx context.Context, key, marker string, markerTTL time.Duration, value any) error {
	if err := r.check(ctx, "getdel_marking", key, marker); err != nil {
		return err
	}
	return r.inner.GetAndDeleteMarking(ctx, key, marker, markerTTL, value)
}

// EvictableRedisAdapter is the Redis adapter for the evictable role. Every
// method checks that its keys carry a declared evictable prefix before the
// command is sent.
//
// It does not implement MarkingConsumeCache: the markers that operation sets
// are auth-role keys, so no evictable family can use it.
type EvictableRedisAdapter struct {
	roleAdapter
}

// NewEvictableRedisAdapter returns the adapter for the evictable client,
// checking keys with guard.
func NewEvictableRedisAdapter(client redisrole.EvictableClient, guard *redisrole.Guard) *EvictableRedisAdapter {
	return &EvictableRedisAdapter{roleAdapter: roleAdapter{inner: NewRedisCacheAdapter(client.Client), guard: guard, role: redisrole.RoleEvictable}}
}

// EvictableRole implements [EvictableCache].
func (e *EvictableRedisAdapter) EvictableRole() {}

func (a *roleAdapter) Get(ctx context.Context, key string, value any) error {
	if err := a.check(ctx, "get", key); err != nil {
		return err
	}
	return a.inner.Get(ctx, key, value)
}

func (a *roleAdapter) GetAndDelete(ctx context.Context, key string, value any) error {
	if err := a.check(ctx, "getdel", key); err != nil {
		return err
	}
	return a.inner.GetAndDelete(ctx, key, value)
}

func (a *roleAdapter) Set(ctx context.Context, key string, value any, ttl time.Duration) error {
	if err := a.check(ctx, "set", key); err != nil {
		return err
	}
	return a.inner.Set(ctx, key, value, ttl)
}

func (a *roleAdapter) SetIfAbsent(ctx context.Context, key string, value any, ttl time.Duration) (bool, error) {
	if err := a.check(ctx, "setnx", key); err != nil {
		return false, err
	}
	return a.inner.SetIfAbsent(ctx, key, value, ttl)
}

func (a *roleAdapter) Add(ctx context.Context, key string, ttl time.Duration) (bool, error) {
	if err := a.check(ctx, "add", key); err != nil {
		return false, err
	}
	return a.inner.Add(ctx, key, ttl)
}

func (a *roleAdapter) ClaimLease(ctx context.Context, key, owner string, ttl time.Duration) (bool, error) {
	if err := a.check(ctx, "claim_lease", key); err != nil {
		return false, err
	}
	return a.inner.ClaimLease(ctx, key, owner, ttl)
}

func (a *roleAdapter) AcquireLease(ctx context.Context, key, owner string, ttl time.Duration) (bool, error) {
	if err := a.check(ctx, "acquire_lease", key); err != nil {
		return false, err
	}
	return a.inner.AcquireLease(ctx, key, owner, ttl)
}

func (a *roleAdapter) RenewLease(ctx context.Context, key, owner string, ttl time.Duration) (bool, error) {
	if err := a.check(ctx, "renew_lease", key); err != nil {
		return false, err
	}
	return a.inner.RenewLease(ctx, key, owner, ttl)
}

func (a *roleAdapter) ReleaseLease(ctx context.Context, key, owner string) error {
	if err := a.check(ctx, "release_lease", key); err != nil {
		return err
	}
	return a.inner.ReleaseLease(ctx, key, owner)
}

func (a *roleAdapter) ReleaseLeaseIfOwner(ctx context.Context, key, owner string) (bool, error) {
	if err := a.check(ctx, "release_lease", key); err != nil {
		return false, err
	}
	return a.inner.ReleaseLeaseIfOwner(ctx, key, owner)
}

func (a *roleAdapter) CompareAndSwap(ctx context.Context, key string, expected, replacement any, ttl time.Duration) (bool, error) {
	if err := a.check(ctx, "compare_and_swap", key); err != nil {
		return false, err
	}
	return a.inner.CompareAndSwap(ctx, key, expected, replacement, ttl)
}

func (a *roleAdapter) CompareAndDelete(ctx context.Context, key string, expected any) (bool, error) {
	if err := a.check(ctx, "compare_and_delete", key); err != nil {
		return false, err
	}
	return a.inner.CompareAndDelete(ctx, key, expected)
}

func (a *roleAdapter) Mutate(ctx context.Context, key string, value any, ttl time.Duration, fn func(exists bool) error) error {
	if err := a.check(ctx, "mutate", key); err != nil {
		return err
	}
	return a.inner.Mutate(ctx, key, value, ttl, fn)
}

func (a *roleAdapter) Update(ctx context.Context, key string, value any) error {
	if err := a.check(ctx, "update", key); err != nil {
		return err
	}
	return a.inner.Update(ctx, key, value)
}

func (a *roleAdapter) Expire(ctx context.Context, key string, ttl time.Duration) error {
	if err := a.check(ctx, "expire", key); err != nil {
		return err
	}
	return a.inner.Expire(ctx, key, ttl)
}

func (a *roleAdapter) Delete(ctx context.Context, key string) error {
	if err := a.check(ctx, "del", key); err != nil {
		return err
	}
	return a.inner.Delete(ctx, key)
}

func (a *roleAdapter) ListAppend(ctx context.Context, key string, value any, ttl time.Duration) error {
	if err := a.check(ctx, "rpush", key); err != nil {
		return err
	}
	return a.inner.ListAppend(ctx, key, value, ttl)
}

func (a *roleAdapter) ListRange(ctx context.Context, key string, start, stop int64, value any) error {
	if err := a.check(ctx, "lrange", key); err != nil {
		return err
	}
	return a.inner.ListRange(ctx, key, start, stop, value)
}

func (a *roleAdapter) ScanKeys(ctx context.Context, prefix string) ([]string, error) {
	if err := a.check(ctx, "scan", prefix); err != nil {
		return nil, err
	}
	keys, err := a.inner.ScanKeys(ctx, prefix)
	if err != nil {
		return nil, err
	}
	return slices.DeleteFunc(keys, func(key string) bool {
		family := redisrole.Classify(key)
		return family.Control || family.Role != a.role
	}), nil
}

func (a *roleAdapter) DeleteByPrefix(ctx context.Context, prefix string) error {
	keys, err := a.ScanKeys(ctx, prefix)
	if err != nil {
		return err
	}
	for batch := range slices.Chunk(keys, 100) {
		if err := a.inner.client.Del(ctx, batch...).Err(); err != nil {
			return fmt.Errorf("delete keys with prefix %q: %w", prefix, err)
		}
	}
	return nil
}
