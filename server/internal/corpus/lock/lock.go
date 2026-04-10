// Package lock provides a Redis-based distributed lock with renewable leases
// and fencing tokens for multi-pod publish serialization.
package lock

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/redis/go-redis/v9"
)

// ErrNotHeld is returned when attempting to release or renew a lock that is
// not held by the caller.
var ErrNotHeld = fmt.Errorf("lock not held")

// ErrStaleFencingToken is returned when a fencing token is older than the
// current token for the lock, indicating the caller's lease has been
// superseded.
var ErrStaleFencingToken = fmt.Errorf("stale fencing token")

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

// Acquire attempts to acquire a distributed lock for the given key with the
// specified TTL. Returns a Lock on success or an error if the lock is already
// held.
func (l *Locker) Acquire(ctx context.Context, key string, ttl time.Duration) (*Lock, error) {
	return nil, fmt.Errorf("not implemented")
}

// Release releases a previously acquired lock.
func (l *Locker) Release(ctx context.Context, lock *Lock) error {
	return fmt.Errorf("not implemented")
}

// Renew extends the TTL of a held lock. Returns ErrNotHeld if the lock is no
// longer held by the caller.
func (l *Locker) Renew(ctx context.Context, lock *Lock, ttl time.Duration) error {
	return fmt.Errorf("not implemented")
}

// ValidateFencingToken checks that the given fencing token is still the
// current token for the lock key. Returns ErrStaleFencingToken if a newer
// token exists.
func (l *Locker) ValidateFencingToken(ctx context.Context, key string, token int64) error {
	return fmt.Errorf("not implemented")
}
