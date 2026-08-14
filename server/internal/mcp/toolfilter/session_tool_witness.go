// Listing-witnessed live enforcement for proxied backends. tools/call
// requests carry no annotation hints, so live annotation grants against
// remote/tunneled servers are authorized by WITNESS: the tools/list rows
// this exact MCP session was shown (post-RBAC, post-consent filtering),
// recorded before relay and consulted at call time. Anything unwitnessed
// falls back to the frozen name grant — fail narrow.
//
// Freshness contract, deliberately weaker than "the most recent listing":
// a tool this session was witnessed carrying a matching hint within the
// witness TTL may remain callable even if a newer listing dropped it —
// cursor-mismatched pages drop the record rather than merging, and a fresh
// first page replaces it wholesale, but there is no cross-page atomicity.
// Live grants inherently trust the upstream's declared hints (the labels
// are author-provided and the freeze toggle is the pinning mechanism);
// within that boundary the residual is a tool the upstream itself listed
// with matching hints inside the TTL window.

package toolfilter

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"log/slog"
	"slices"
	"time"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/cache"
)

const (
	// sessionToolWitnessTTL bounds how long a witnessed row may authorize
	// calls. Live sessions re-list on every client connection and on
	// list_changed notifications, each rebuilding the record wholesale.
	sessionToolWitnessTTL = 15 * time.Minute

	// sessionToolWitnessMaxTools mirrors the consent snapshot cap.
	sessionToolWitnessMaxTools = 1000

	// sessionToolWitnessMaxNameBytes mirrors the consent snapshot per-name cap.
	sessionToolWitnessMaxNameBytes = 200

	// sessionToolWitnessMaxCursorBytes bounds the upstream-controlled cursor
	// stored on the witness so a hostile upstream cannot inflate Redis
	// documents through pagination cursors.
	sessionToolWitnessMaxCursorBytes = 2048
)

// WitnessedTool is one listing row this session was shown, with the
// vocabulary values whose hints the upstream declared explicitly true.
type WitnessedTool struct {
	Name        string   `json:"name"`
	Annotations []string `json:"annotations"`
}

// sessionToolWitness is the listing witness for one (grant, MCP session)
// pair: rows accumulate in cursor order from a first page onward and the
// record is replaced wholesale by the next first page.
type sessionToolWitness struct {
	// GrantID is the consent grant identity, stable across refresh-token
	// rotations.
	GrantID string `json:"grant_id"`

	// SessionKey is the hashed client-facing MCP session id.
	SessionKey string `json:"session_key"`

	Tools []WitnessedTool `json:"tools"`

	// ExpectedCursor is the cursor the next witnessed page must carry;
	// empty means only a first page may extend the record. A mismatched
	// page drops the record rather than merging generations.
	ExpectedCursor string `json:"expected_cursor"`

	Complete bool `json:"complete"`
}

// sessionKey hashes the client-supplied MCP session id into a bounded key
// segment so unbounded client input never lands in Redis keys verbatim.
func sessionKey(sessionID string) string {
	sum := sha256.Sum256([]byte(sessionID))
	return base64.RawURLEncoding.EncodeToString(sum[:16])
}

func sessionToolWitnessCacheKey(grantID, sessionKeyHash string) string {
	return fmt.Sprintf("sessionToolWitness:%s:%s", grantID, sessionKeyHash)
}

// CacheKey implements cache.CacheableObject.
func (w sessionToolWitness) CacheKey() string {
	return sessionToolWitnessCacheKey(w.GrantID, w.SessionKey)
}

// AdditionalCacheKeys implements cache.CacheableObject.
func (w sessionToolWitness) AdditionalCacheKeys() []string { return nil }

// TTL implements cache.CacheableObject.
func (w sessionToolWitness) TTL() time.Duration { return sessionToolWitnessTTL }

// SessionToolWitnessStore records the listing rows relayed to each session
// and answers call-time live-annotation matches against them. Every failure
// path narrows: a store outage or malformed page can only reduce what a
// call may reach, never extend it.
type SessionToolWitnessStore struct {
	logger *slog.Logger
	cache  cache.TypedCacheObject[sessionToolWitness]
}

func NewSessionToolWitnessStore(logger *slog.Logger, adapter cache.Cache) *SessionToolWitnessStore {
	return &SessionToolWitnessStore{
		logger: logger,
		cache:  cache.NewTypedObjectCache[sessionToolWitness](logger, adapter, cache.SuffixNone),
	}
}

// WitnessPage records one relayed tools/list page for the grant/session
// pair. requestCursor is the cursor the client requested (empty for a first
// page, which replaces the record wholesale); nextCursor is the upstream's
// continuation (empty completes the listing). Callers commit the filtered
// result first and witness after, so a page that failed filtering never
// leaves authorization behind. Errors are logged, never surfaced — a
// witness failure narrows live matching, it must not fail the relay.
func (s *SessionToolWitnessStore) WitnessPage(ctx context.Context, grantID, sessionID, requestCursor string, tools []WitnessedTool, nextCursor string) {
	if grantID == "" || sessionID == "" {
		return
	}
	key := sessionKey(sessionID)

	var witness sessionToolWitness
	if requestCursor == "" {
		witness = sessionToolWitness{
			GrantID:        grantID,
			SessionKey:     key,
			Tools:          nil,
			ExpectedCursor: "",
			Complete:       false,
		}
	} else {
		cached, err := s.cache.Get(ctx, sessionToolWitnessCacheKey(grantID, key))
		if err != nil || cached.ExpectedCursor != requestCursor || cached.Complete {
			s.drop(ctx, grantID, key)
			return
		}
		witness = cached
	}

	seen := make(map[string]bool, len(witness.Tools)+len(tools))
	for _, tool := range witness.Tools {
		seen[tool.Name] = true
	}
	for _, tool := range tools {
		if tool.Name == "" || len(tool.Name) > sessionToolWitnessMaxNameBytes || seen[tool.Name] {
			s.drop(ctx, grantID, key)
			return
		}
		seen[tool.Name] = true
		witness.Tools = append(witness.Tools, tool)
	}
	if len(witness.Tools) > sessionToolWitnessMaxTools {
		s.drop(ctx, grantID, key)
		return
	}

	if len(nextCursor) > sessionToolWitnessMaxCursorBytes {
		s.drop(ctx, grantID, key)
		return
	}
	witness.ExpectedCursor = nextCursor
	witness.Complete = nextCursor == ""

	if err := s.cache.Store(ctx, witness); err != nil {
		s.logger.WarnContext(ctx, "store session tool witness", attr.SlogError(err))
	}
}

func (s *SessionToolWitnessStore) drop(ctx context.Context, grantID, sessionKeyHash string) {
	if err := s.cache.DeleteByKey(ctx, sessionToolWitnessCacheKey(grantID, sessionKeyHash)); err != nil {
		s.logger.DebugContext(ctx, "drop session tool witness", attr.SlogError(err))
	}
}

// MatchesWitnessed reports whether this session was witnessed a listing row
// for the named tool carrying a hint matching any of the given annotation
// values. Any miss or store failure reads as no match.
func (s *SessionToolWitnessStore) MatchesWitnessed(ctx context.Context, grantID, sessionID, name string, values []string) bool {
	if grantID == "" || sessionID == "" || name == "" || len(values) == 0 {
		return false
	}
	witness, err := s.cache.Get(ctx, sessionToolWitnessCacheKey(grantID, sessionKey(sessionID)))
	if err != nil {
		return false
	}
	for _, tool := range witness.Tools {
		if tool.Name != name {
			continue
		}
		for _, value := range tool.Annotations {
			if slices.Contains(values, value) {
				return true
			}
		}
	}
	return false
}
