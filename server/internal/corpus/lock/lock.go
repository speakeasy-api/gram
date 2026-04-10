// Package lock provides a Redis-based distributed lock with renewable leases
// and fencing tokens for multi-pod publish serialization.
package lock

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"time"

	"github.com/redis/go-redis/v9"
)

// ErrNotHeld is returned when attempting to release or renew a lock that is
// not held by the caller.
var ErrNotHeld = errors.New("lock not held")

// ErrStaleFencingToken is returned when a fencing token is older than the
// current token for the lock, indicating the caller's lease has been
// superseded.
var ErrStaleFencingToken = errors.New("stale fencing token")

// fencingTokenKey returns the Redis key used to store the monotonically
// increasing fencing token counter for a given lock key.
func fencingTokenKey(key string) string {
	return key + ":fencing"
}

// Lock represents a held distributed lock with a fencing token.
type Lock struct {
	key          string
	value        string
	fencingToken int64
}

// FencingToken returns the monotonically increasing fencing token assigned
// when this lock was acquired.
func (l *Lock) FencingToken() int64 {
	return l.fencingToken
}

// Locker provides distributed locking backed by Redis.
type Locker struct {
	client *redis.Client
	logger *slog.Logger
}

// NewLocker creates a new Locker using the given Redis client.
func NewLocker(client *redis.Client, logger *slog.Logger) *Locker {
	return &Locker{
		client: client,
		logger: logger,
	}
}

// randomValue generates a cryptographically random hex string to uniquely
// identify the lock holder.
func randomValue() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generate lock value: %w", err)
	}
	return hex.EncodeToString(b), nil
}

// Acquire attempts to acquire a distributed lock for the given key with the
// specified TTL. Returns a Lock on success or an error if the lock is already
// held. Each successful acquisition increments the fencing token counter.
func (l *Locker) Acquire(ctx context.Context, key string, ttl time.Duration) (*Lock, error) {
	value, err := randomValue()
	if err != nil {
		return nil, err
	}

	ok, err := l.client.SetNX(ctx, key, value, ttl).Result()
	if err != nil {
		return nil, fmt.Errorf("acquire lock %q: %w", key, err)
	}
	if !ok {
		return nil, fmt.Errorf("acquire lock %q: %w", key, ErrNotHeld)
	}

	// Increment fencing token atomically. The fencing token key has no TTL;
	// it persists so that tokens are monotonically increasing across lock
	// generations.
	token, err := l.client.Incr(ctx, fencingTokenKey(key)).Result()
	if err != nil {
		// Best-effort release if we fail to get a fencing token.
		_ = l.client.Del(ctx, key).Err()
		return nil, fmt.Errorf("increment fencing token for %q: %w", key, err)
	}

	return &Lock{
		key:          key,
		value:        value,
		fencingToken: token,
	}, nil
}

// releaseScript atomically releases the lock only if the caller still holds it
// (value matches). This prevents releasing a lock that was already expired and
// re-acquired by another holder.
var releaseScript = redis.NewScript(`
if redis.call("GET", KEYS[1]) == ARGV[1] then
	return redis.call("DEL", KEYS[1])
else
	return 0
end
`)

// Release releases a previously acquired lock. Returns ErrNotHeld if the lock
// is no longer held by the caller (e.g., it expired).
func (l *Locker) Release(ctx context.Context, lock *Lock) error {
	result, err := releaseScript.Run(ctx, l.client, []string{lock.key}, lock.value).Int64()
	if err != nil {
		return fmt.Errorf("release lock %q: %w", lock.key, err)
	}
	if result == 0 {
		return ErrNotHeld
	}
	return nil
}

// renewScript atomically extends the TTL only if the caller still holds the
// lock (value matches).
var renewScript = redis.NewScript(`
if redis.call("GET", KEYS[1]) == ARGV[1] then
	return redis.call("PEXPIRE", KEYS[1], ARGV[2])
else
	return 0
end
`)

// Renew extends the TTL of a held lock. Returns ErrNotHeld if the lock is no
// longer held by the caller.
func (l *Locker) Renew(ctx context.Context, lock *Lock, ttl time.Duration) error {
	result, err := renewScript.Run(ctx, l.client, []string{lock.key}, lock.value, strconv.FormatInt(ttl.Milliseconds(), 10)).Int64()
	if err != nil {
		return fmt.Errorf("renew lock %q: %w", lock.key, err)
	}
	if result == 0 {
		return ErrNotHeld
	}
	return nil
}

// ValidateFencingToken checks that the given fencing token is still the
// current token for the lock key. Returns ErrStaleFencingToken if a newer
// token exists.
func (l *Locker) ValidateFencingToken(ctx context.Context, key string, token int64) error {
	current, err := l.client.Get(ctx, fencingTokenKey(key)).Int64()
	if err != nil {
		return fmt.Errorf("validate fencing token for %q: %w", key, err)
	}
	if token < current {
		return ErrStaleFencingToken
	}
	return nil
}
