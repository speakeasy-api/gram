// Package tunnelsessions holds the Redis-only session state for anonymous
// (public) tunneled MCP traffic. Gram terminates MCP sessions for these
// endpoints: it mints a Gram-owned session id (gram_sid) on a successful
// initialize, maps it to the backend's own Mcp-Session-Id plus the exact
// tunnel target that owns it, and resolves that mapping on every subsequent
// session-bearing request. There is deliberately no Postgres record — these
// sessions are transport/routing state with a TTL, not identity.
package tunnelsessions

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"time"

	"github.com/redis/go-redis/v9"
)

// SessionIDPrefix namespaces Gram-minted anonymous tunnel session ids so they
// are recognizable in logs (hashed) and cannot be confused with backend ids.
const SessionIDPrefix = "gsid_"

var sessionIDPattern = regexp.MustCompile(`^gsid_[0-9a-f]{32}$`)

// MaxBackendSessionIDLength bounds the upstream Mcp-Session-Id value stored
// in a mapping. The MCP spec only requires visible ASCII; an unbounded value
// would let a misbehaving backend bloat Redis.
const MaxBackendSessionIDLength = 512

// ErrNotFound is returned when a session id has no live mapping — expired,
// deleted, or never minted. Callers translate it to HTTP 404 so MCP clients
// re-initialize.
var ErrNotFound = errors.New("tunnel session not found")

// CapacityError is returned by Reserve when the tunnel is at its live
// anonymous-session cap. RetryAfter is the time until the earliest tracked
// session expires — the soonest a slot could free without a DELETE.
type CapacityError struct {
	RetryAfter time.Duration
}

func (e *CapacityError) Error() string {
	return fmt.Sprintf("tunnel is at its anonymous session capacity (retry in %s)", e.RetryAfter)
}

// Session is the Redis-stored mapping value for one Gram-owned anonymous MCP
// session.
type Session struct {
	// BackendSessionID is the Mcp-Session-Id the customer's MCP server minted
	// at initialize. Forwarded upstream in place of the Gram-owned id.
	BackendSessionID string `json:"backend_session_id"`
	// GatewayAddr is the tunnel gateway advertise address that served the
	// initialize. Session-bearing requests dial it directly instead of
	// re-running rendezvous selection.
	GatewayAddr string `json:"gateway_addr"`
	// AgentSessionID is the exact gateway agent session that owns the backend
	// session. Forwarded as the exact-target header so the gateway never
	// spills the request to a sibling agent.
	AgentSessionID string `json:"agent_session_id"`
}

// Store reads and writes anonymous tunnel session state in Redis.
type Store struct {
	redis *redis.Client
	// ttl is the sliding session lifetime: set at reserve, refreshed on every
	// session-bearing POST/GET.
	ttl time.Duration
	// liveCap bounds concurrently tracked sessions per tunnel.
	liveCap int
}

func NewStore(redisClient *redis.Client, ttl time.Duration, liveCap int) *Store {
	return &Store{redis: redisClient, ttl: ttl, liveCap: liveCap}
}

// TTL exposes the configured session lifetime for logging/Retry-After math.
func (s *Store) TTL() time.Duration { return s.ttl }

// MintSessionID returns a fresh Gram-owned session id: gsid_ + 128 bits of
// crypto/rand hex. The id doubles as a bearer credential for the anonymous
// session, so it must be unguessable and must never be logged raw.
func MintSessionID() (string, error) {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("generate tunnel session id: %w", err)
	}
	return SessionIDPrefix + hex.EncodeToString(buf), nil
}

// IsSessionID reports whether value is a well-formed Gram-owned tunnel
// session id. Callers must check this before using client-supplied values as
// Redis key material.
func IsSessionID(value string) bool {
	return sessionIDPattern.MatchString(value)
}

func mappingKey(tunnelID, mcpServerID, sid string) string {
	return "tunnelsess:map:" + tunnelID + ":" + mcpServerID + ":" + sid
}

func liveSetKey(tunnelID string) string {
	return "tunnelsess:live:" + tunnelID
}

func liveMember(mcpServerID, sid string) string {
	return mcpServerID + ":" + sid
}

// reserveScript prunes expired members, enforces the live cap, and admits the
// new member in one atomic step. Returns {1} on admit or {0, earliest_expiry_ms}
// on capacity rejection.
var reserveScript = redis.NewScript(`
redis.call('ZREMRANGEBYSCORE', KEYS[1], '-inf', ARGV[1])
local count = redis.call('ZCARD', KEYS[1])
if count >= tonumber(ARGV[2]) then
  local first = redis.call('ZRANGE', KEYS[1], 0, 0, 'WITHSCORES')
  return {0, first[2]}
end
redis.call('ZADD', KEYS[1], ARGV[3], ARGV[4])
redis.call('PEXPIRE', KEYS[1], ARGV[5])
return {1, '0'}
`)

// resolveRefreshScript loads a mapping and, when present, slides both the
// mapping TTL and the live-set score in the same atomic step. A missing
// mapping also drops the (stale) live-set member so capacity accounting
// converges.
var resolveRefreshScript = redis.NewScript(`
local value = redis.call('GET', KEYS[1])
if not value then
  redis.call('ZREM', KEYS[2], ARGV[1])
  return false
end
redis.call('PEXPIRE', KEYS[1], ARGV[3])
redis.call('ZADD', KEYS[2], 'XX', ARGV[2], ARGV[1])
redis.call('PEXPIRE', KEYS[2], ARGV[3])
return value
`)

// Reserve admits sid into the tunnel's live-session set ahead of the
// initialize forward, enforcing the per-tunnel cap atomically. The
// reservation holds a capacity slot only; the mapping is written by Commit
// once the initialize succeeds. Callers must Rollback on every non-commit
// path.
func (s *Store) Reserve(ctx context.Context, tunnelID, mcpServerID, sid string) error {
	now := time.Now()
	res, err := reserveScript.Run(ctx, s.redis,
		[]string{liveSetKey(tunnelID)},
		now.UnixMilli(),
		s.liveCap,
		now.Add(s.ttl).UnixMilli(),
		liveMember(mcpServerID, sid),
		s.ttl.Milliseconds(),
	).Int64Slice()
	if err != nil {
		return fmt.Errorf("reserve tunnel session slot: %w", err)
	}
	if len(res) != 2 {
		return fmt.Errorf("reserve tunnel session slot: unexpected script result length %d", len(res))
	}
	if res[0] == 1 {
		return nil
	}
	retryAfter := max(time.Duration(res[1]-now.UnixMilli())*time.Millisecond, time.Second)
	return &CapacityError{RetryAfter: retryAfter}
}

// Commit stores the session mapping after a successful initialize. The
// live-set member was already admitted by Reserve.
func (s *Store) Commit(ctx context.Context, tunnelID, mcpServerID, sid string, session Session) error {
	payload, err := json.Marshal(session)
	if err != nil {
		return fmt.Errorf("encode tunnel session mapping: %w", err)
	}
	if err := s.redis.Set(ctx, mappingKey(tunnelID, mcpServerID, sid), payload, s.ttl).Err(); err != nil {
		return fmt.Errorf("store tunnel session mapping: %w", err)
	}
	return nil
}

// Rollback releases a reservation (and any partially written mapping) after a
// failed initialize.
func (s *Store) Rollback(ctx context.Context, tunnelID, mcpServerID, sid string) error {
	pipe := s.redis.Pipeline()
	pipe.ZRem(ctx, liveSetKey(tunnelID), liveMember(mcpServerID, sid))
	pipe.Del(ctx, mappingKey(tunnelID, mcpServerID, sid))
	if _, err := pipe.Exec(ctx); err != nil {
		return fmt.Errorf("release tunnel session reservation: %w", err)
	}
	return nil
}

// Resolve loads the mapping for sid. When refresh is true (session-bearing
// POST/GET) the mapping TTL and live-set score slide forward atomically;
// DELETE resolves without extending the session's life. Returns ErrNotFound
// when no mapping exists.
func (s *Store) Resolve(ctx context.Context, tunnelID, mcpServerID, sid string, refresh bool) (*Session, error) {
	var raw string
	if refresh {
		now := time.Now()
		res, err := resolveRefreshScript.Run(ctx, s.redis,
			[]string{mappingKey(tunnelID, mcpServerID, sid), liveSetKey(tunnelID)},
			liveMember(mcpServerID, sid),
			now.Add(s.ttl).UnixMilli(),
			s.ttl.Milliseconds(),
		).Text()
		switch {
		case errors.Is(err, redis.Nil):
			return nil, ErrNotFound
		case err != nil:
			return nil, fmt.Errorf("resolve tunnel session mapping: %w", err)
		}
		raw = res
	} else {
		res, err := s.redis.Get(ctx, mappingKey(tunnelID, mcpServerID, sid)).Result()
		switch {
		case errors.Is(err, redis.Nil):
			return nil, ErrNotFound
		case err != nil:
			return nil, fmt.Errorf("resolve tunnel session mapping: %w", err)
		}
		raw = res
	}

	var session Session
	if err := json.Unmarshal([]byte(raw), &session); err != nil {
		return nil, fmt.Errorf("decode tunnel session mapping: %w", err)
	}
	return &session, nil
}

// Delete drops the mapping and its capacity slot — used when the client
// terminates the session (DELETE) or the backend reports it gone (404).
func (s *Store) Delete(ctx context.Context, tunnelID, mcpServerID, sid string) error {
	pipe := s.redis.Pipeline()
	pipe.Del(ctx, mappingKey(tunnelID, mcpServerID, sid))
	pipe.ZRem(ctx, liveSetKey(tunnelID), liveMember(mcpServerID, sid))
	if _, err := pipe.Exec(ctx); err != nil {
		return fmt.Errorf("delete tunnel session mapping: %w", err)
	}
	return nil
}

// Purge drops every tracked anonymous session for a tunnel. Called when the
// owner withdraws public consent so a later re-enable does not resurrect old
// sessions. Best-effort: the serve-path consent guard rejects per-request
// regardless.
func (s *Store) Purge(ctx context.Context, tunnelID string) error {
	return Purge(ctx, s.redis, tunnelID)
}

// Purge is the package-level variant of [Store.Purge] for callers (e.g. the
// tunneledmcp management service) that only revoke sessions and have no use
// for a fully configured store.
func Purge(ctx context.Context, redisClient *redis.Client, tunnelID string) error {
	members, err := redisClient.ZRange(ctx, liveSetKey(tunnelID), 0, -1).Result()
	if err != nil {
		return fmt.Errorf("list tunnel sessions for purge: %w", err)
	}
	pipe := redisClient.Pipeline()
	for _, member := range members {
		// member is "<mcp_server_id>:<sid>"; the mapping key appends it to
		// the tunnel prefix verbatim.
		pipe.Del(ctx, "tunnelsess:map:"+tunnelID+":"+member)
	}
	pipe.Del(ctx, liveSetKey(tunnelID))
	if _, err := pipe.Exec(ctx); err != nil {
		return fmt.Errorf("purge tunnel sessions: %w", err)
	}
	return nil
}

// ActiveCount reports the number of live (unexpired) tracked sessions for a
// tunnel after pruning expired members.
func (s *Store) ActiveCount(ctx context.Context, tunnelID string) (int64, error) {
	now := time.Now().UnixMilli()
	pipe := s.redis.Pipeline()
	pipe.ZRemRangeByScore(ctx, liveSetKey(tunnelID), "-inf", strconv.FormatInt(now, 10))
	card := pipe.ZCard(ctx, liveSetKey(tunnelID))
	if _, err := pipe.Exec(ctx); err != nil {
		return 0, fmt.Errorf("count tunnel sessions: %w", err)
	}
	return card.Val(), nil
}
