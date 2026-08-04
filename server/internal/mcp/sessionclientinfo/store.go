// Package sessionclientinfo holds the Redis-only record of who is on the other
// end of an MCP session. A client reports its identity once, during the
// initialize handshake; Gram's hosted MCP path is otherwise stateless, so
// without this record the identity is gone by the time the client calls a tool.
//
// There is deliberately no Postgres row and no expiry. MCP sessions can run for
// a month or longer, and a TTL long enough to cover that would put no bound on
// the keyspace at all — while a short one would quietly drop the identity of a
// session that is merely idle. Instead each MCP server gets a fixed budget of
// records and the least recently used ones fall out when it is exceeded.
//
// That bounds abuse to the server being abused: initialize is unauthenticated
// on a public MCP server, so anyone can mint records, but they can only churn
// that one server's budget rather than compete for Redis with the OAuth
// challenges and session grants sharing it.
package sessionclientinfo

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
)

// DefaultLiveRecordCap bounds the identities tracked per MCP server. Matches
// the anonymous session cap the tunneled path uses; at roughly 360 bytes a
// record that is a few megabytes per server.
const DefaultLiveRecordCap = 10000

// ErrNotFound is returned when a session has no recorded identity — never
// handshaked with one, or evicted since. Callers treat it as "caller unknown",
// which is an ordinary outcome rather than a failure.
var ErrNotFound = errors.New("session client info not found")

// Info is the identity a client reported for itself at initialize. Untrusted
// and self-reported: it is carried for attribution, never authorization.
type Info struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

// Store reads and writes session identities in Redis.
type Store struct {
	redis *redis.Client
	cap   int
}

// NewStore builds a store bounded at cap records per MCP server. A cap of zero
// or less takes DefaultLiveRecordCap.
//
// A nil client yields a store that records nothing and finds nothing, so
// callers never have to test for one — an environment without Redis simply
// reports every caller as unknown.
func NewStore(redisClient *redis.Client, recordCap int) *Store {
	if recordCap <= 0 {
		recordCap = DefaultLiveRecordCap
	}
	return &Store{redis: redisClient, cap: recordCap}
}

// scope is the key fragment shared by an MCP server's record keys and its live
// set. Wrapped in a Redis hash tag so both land in the same cluster slot —
// admitRecord derives one from the other and must be able to touch both.
//
// Session ids arrive on a client-supplied header, so scoping by project and
// toolset stops one tenant's sessions from addressing another's.
func scope(projectID uuid.UUID, toolsetSlug string) string {
	return "{" + projectID.String() + ":" + toolsetSlug + "}"
}

func liveSetKey(projectID uuid.UUID, toolsetSlug string) string {
	return "mcpClientInfoLive:" + scope(projectID, toolsetSlug)
}

func recordKeyPrefix(projectID uuid.UUID, toolsetSlug string) string {
	return "mcpClientInfo:" + scope(projectID, toolsetSlug) + ":"
}

// member hashes the session id used as key material. The id arrives on an
// unbounded client-supplied header and doubles as the bearer of the session,
// so hashing gives a fixed-length key that neither bloats Redis nor writes the
// raw value into a keyspace that gets logged and enumerated. Truncating
// instead would collide every id sharing a prefix, letting one session read
// another's identity.
func member(sessionID string) string {
	digest := sha256.Sum256([]byte(sessionID))
	return hex.EncodeToString(digest[:])
}

// admitScript writes a record, admits it to the live set, and evicts the
// excess in one atomic step.
//
// Eviction is by least recent use, not by age: the score is refreshed every
// time a record is read, so a session still in use survives no matter how old
// it is, and what falls out is genuinely dormant. Evicting oldest-first would
// drop exactly the long-lived sessions this record exists to serve.
var admitScript = redis.NewScript(`
redis.call('SET', KEYS[2], ARGV[3])
redis.call('ZADD', KEYS[1], ARGV[2], ARGV[1])
local excess = redis.call('ZCARD', KEYS[1]) - tonumber(ARGV[4])
if excess > 0 then
  local evicted = redis.call('ZRANGE', KEYS[1], 0, excess - 1)
  for _, victim in ipairs(evicted) do
    redis.call('DEL', ARGV[5] .. victim)
  end
  redis.call('ZREMRANGEBYRANK', KEYS[1], 0, excess - 1)
end
return 1
`)

// loadScript reads a record and, when present, slides its live-set score so
// the entry counts as recently used. A record that is gone drops its (stale)
// live-set member too, so the budget accounting converges.
var loadScript = redis.NewScript(`
local value = redis.call('GET', KEYS[1])
if not value then
  redis.call('ZREM', KEYS[2], ARGV[1])
  return false
end
redis.call('ZADD', KEYS[2], 'XX', ARGV[2], ARGV[1])
return value
`)

// Store records the identity a client reported at initialize, evicting the
// least recently used records of the same MCP server once the budget is full.
//
// nowMillis is supplied by the caller so ordering is testable.
func (s *Store) Store(ctx context.Context, projectID uuid.UUID, toolsetSlug, sessionID string, info Info, nowMillis int64) error {
	if s.redis == nil {
		return nil
	}

	payload, err := json.Marshal(info)
	if err != nil {
		return fmt.Errorf("marshal session client info: %w", err)
	}

	key := member(sessionID)
	err = admitScript.Run(ctx, s.redis,
		[]string{liveSetKey(projectID, toolsetSlug), recordKeyPrefix(projectID, toolsetSlug) + key},
		key, nowMillis, string(payload), s.cap, recordKeyPrefix(projectID, toolsetSlug),
	).Err()
	if err != nil {
		return fmt.Errorf("admit session client info: %w", err)
	}

	return nil
}

// Load returns the identity recorded for a session and marks it recently used.
// Returns ErrNotFound when the session has none.
func (s *Store) Load(ctx context.Context, projectID uuid.UUID, toolsetSlug, sessionID string, nowMillis int64) (Info, error) {
	if s.redis == nil {
		return Info{}, ErrNotFound
	}

	key := member(sessionID)
	raw, err := loadScript.Run(ctx, s.redis,
		[]string{recordKeyPrefix(projectID, toolsetSlug) + key, liveSetKey(projectID, toolsetSlug)},
		key, nowMillis,
	).Text()
	switch {
	case errors.Is(err, redis.Nil):
		return Info{}, ErrNotFound
	case err != nil:
		return Info{}, fmt.Errorf("load session client info: %w", err)
	}

	var info Info
	if err := json.Unmarshal([]byte(raw), &info); err != nil {
		return Info{}, fmt.Errorf("unmarshal session client info: %w", err)
	}
	if info.Name == "" {
		return Info{}, ErrNotFound
	}

	return info, nil
}
