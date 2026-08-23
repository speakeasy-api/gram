package gateway

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
)

// leaseTTL is how long an unrefreshed claim survives. The supervisor
// heartbeats every reconcile tick (15s), so a dead replica's ingresses free
// up within three missed ticks.
const leaseTTL = 45 * time.Second

// RedisLease implements per-ingress ownership claims. Values are the owning
// replica id; compare-and-set scripts keep one replica from extending or
// releasing another's claim. MVP runs a single replica, but leases are still
// written so the multi-replica path is exercised from day one.
type RedisLease struct {
	client    *redis.Client
	replicaID string
}

func NewRedisLease(client *redis.Client, replicaID string) *RedisLease {
	return &RedisLease{client: client, replicaID: replicaID}
}

func leaseKey(ingressID uuid.UUID) string {
	return "net_ingress_owner:" + ingressID.String()
}

var heartbeatScript = redis.NewScript(`
if redis.call("GET", KEYS[1]) == ARGV[1] then
  return redis.call("PEXPIRE", KEYS[1], ARGV[2])
end
return 0
`)

var releaseScript = redis.NewScript(`
if redis.call("GET", KEYS[1]) == ARGV[1] then
  return redis.call("DEL", KEYS[1])
end
return 0
`)

// Claim attempts to take ownership of an ingress. Returns false when another
// replica holds a live claim.
func (l *RedisLease) Claim(ctx context.Context, ingressID uuid.UUID) (bool, error) {
	ok, err := l.client.SetNX(ctx, leaseKey(ingressID), l.replicaID, leaseTTL).Result()
	if err != nil {
		return false, fmt.Errorf("claim ingress lease: %w", err)
	}
	return ok, nil
}

// Heartbeat extends this replica's claim. Returns false when the claim was
// lost (expired and taken, or released elsewhere).
func (l *RedisLease) Heartbeat(ctx context.Context, ingressID uuid.UUID) (bool, error) {
	res, err := heartbeatScript.Run(ctx, l.client, []string{leaseKey(ingressID)}, l.replicaID, leaseTTL.Milliseconds()).Int()
	if err != nil {
		return false, fmt.Errorf("heartbeat ingress lease: %w", err)
	}
	return res == 1, nil
}

// Release drops this replica's claim if it still holds it.
func (l *RedisLease) Release(ctx context.Context, ingressID uuid.UUID) error {
	if _, err := releaseScript.Run(ctx, l.client, []string{leaseKey(ingressID)}, l.replicaID).Result(); err != nil {
		return fmt.Errorf("release ingress lease: %w", err)
	}
	return nil
}
