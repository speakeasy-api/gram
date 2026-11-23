// Serve-path loading of consent-screen tool selection and refreshability.
// The session JWT stays standard-claims-only, so policy lives on user_sessions
// and is pulled per request through a short-TTL Redis cache keyed by
// (issuer, jti). Failures reject the request (fail closed) — a policy store
// outage must never widen a restrictive session to all tools.
//
// The Redis cache holds policy, never liveness. Whether the session row is
// still live is decided by Postgres and memoized per process for
// sessionLivenessTTL, because revocation soft-deletes the row first and its
// Redis denylist entry can be lost to eviction, a restart, or a failed push.

package mcp

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/hashicorp/golang-lru/v2/expirable"
	"github.com/jackc/pgx/v5"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/mcp/toolfilter"
	usersessions_repo "github.com/speakeasy-api/gram/server/internal/usersessions/repo"
)

// sessionPolicyTTL bounds staleness of the cached policy. The jti in
// the key rotates on every refresh-token slide, so a rotated session never
// reads a predecessor's entry; the TTL only bounds how long a same-jti
// revision (there are none today — rows are written once) could lag.
const sessionPolicyTTL = 5 * time.Minute

const (
	// sessionLivenessTTL is how long a process trusts its last Postgres answer
	// on whether a session row is live. It is the longest a revoked access
	// token keeps working once its Redis denylist entry is missing. Five
	// seconds collapses one client's burst of MCP calls into one lookup.
	sessionLivenessTTL = 5 * time.Second

	// sessionLivenessMemoSize bounds the per-process liveness memo. An entry
	// is a few hundred bytes, so 10,000 entries cost a few MiB; overflow only
	// evicts entries early and costs an extra indexed lookup.
	sessionLivenessMemoSize = 10_000
)

// sessionLivenessKey identifies a session row the way the serve path
// addresses it.
type sessionLivenessKey struct {
	// issuerID is the user session issuer that minted the token.
	issuerID uuid.UUID

	// jti is the access token's identifier.
	jti string
}

func newSessionLivenessMemo() *expirable.LRU[sessionLivenessKey, bool] {
	return expirable.NewLRU[sessionLivenessKey, bool](sessionLivenessMemoSize, nil, sessionLivenessTTL)
}

// errToolSelectionResourceMismatch marks a restrictive selection presented
// against an endpoint other than the one it was consented on. Session tokens
// are issuer-scoped and portable across endpoints sharing the issuer, so the
// caller must 401 into reauth rather than reinterpret the policy.
var errToolSelectionResourceMismatch = errors.New("session tool selection is bound to a different endpoint")

// errSessionPolicyGone marks a bearer whose session row is gone: the token is no good, nothing is down.
var errSessionPolicyGone = errors.New("session policy gone")

// errToolSelectionLoad marks a post-validation policy-store failure. The
// bearer token itself is valid; the request still fails closed, but logs must
// distinguish this operational or stored-data failure from bad credentials.
var errToolSelectionLoad = errors.New("load session tool selection")

// errSessionLivenessLoad marks a session liveness lookup that never returned.
// The credential was not judged, so the client is told to retry rather than
// to discard it.
var errSessionLivenessLoad = errors.New("check session liveness")

// sessionPolicyEntry caches immutable issuance policy by issuer and JTI.
type sessionPolicyEntry struct {
	// IssuerID scopes this entry to the authorization server.
	IssuerID string `json:"issuer_id"`

	// JTI identifies the immutable session issuance.
	JTI string `json:"jti"`

	// Selection is the raw tool_selection document; empty means NULL (all
	// tools). Kept raw so the cache round-trip cannot normalize away a parse
	// failure — parsing (and its fail-closed error) happens on every read.
	Selection []byte `json:"selection,omitempty"`

	// Refreshable records whether issuance included an actual refresh token.
	Refreshable bool `json:"refreshable"`

	// CredentialID is the credential the session row belongs to, which
	// refresh rotation keeps.
	CredentialID uuid.UUID `json:"credential_id"`
}

// sessionPolicy is a validated session's immutable issuance policy.
type sessionPolicy struct {
	// selection is the consent-screen tool policy, nil for all tools.
	selection *toolfilter.SessionPolicy

	// refreshable reports that issuance included an actual refresh token.
	refreshable bool

	// credentialID is the credential the session row belongs to.
	credentialID uuid.UUID

	// rowConfirmed reports that the policy was read from Postgres on this
	// call, which proves the row is live.
	rowConfirmed bool
}

func sessionPolicyCacheKey(issuerID, jti string) string {
	// The version excludes entries from older servers that lack fields this
	// one reads.
	return "sessionPolicy:v2:" + issuerID + ":" + jti
}

// CacheKey implements cache.CacheableObject.
func (e sessionPolicyEntry) CacheKey() string {
	return sessionPolicyCacheKey(e.IssuerID, e.JTI)
}

// AdditionalCacheKeys implements cache.CacheableObject.
func (e sessionPolicyEntry) AdditionalCacheKeys() []string { return []string{} }

// TTL implements cache.CacheableObject.
func (e sessionPolicyEntry) TTL() time.Duration { return sessionPolicyTTL }

// endpointToolSelectionResource is the server-authored identity a consent
// selection binds to: the fronting mcp_server when there is one, else the
// toolset or gateway. Empty when the endpoint has no stored resource.
func endpointToolSelectionResource(endpoint *ResolvedMcpEndpoint) string {
	switch {
	case endpoint.MetaMcpServerID.Valid:
		return "meta_mcp_server:" + endpoint.MetaMcpServerID.UUID.String()
	case endpoint.McpServerID.Valid:
		return "mcp_server:" + endpoint.McpServerID.UUID.String()
	case endpoint.ToolsetID.Valid:
		return "toolset:" + endpoint.ToolsetID.UUID.String()
	default:
		return ""
	}
}

// endpointAcceptsToolSelectionResource reports whether a stored consent
// selection belongs to this endpoint. A toolset-backed server also accepts the
// "toolset:<id>" form of its toolset, which selections consented before servers
// owned their endpoints still carry, so those sessions keep serving and
// refreshing.
func endpointAcceptsToolSelectionResource(endpoint *ResolvedMcpEndpoint, resource string) bool {
	if resource == endpointToolSelectionResource(endpoint) {
		return true
	}
	return endpoint.McpServerID.Valid && endpoint.ToolsetID.Valid && resource == "toolset:"+endpoint.ToolsetID.UUID.String()
}

// liveSessionPolicyKey bypasses the short-lived HTTP policy cache during code callbacks.
type liveSessionPolicyKey struct{}

// loadSessionPolicy resolves tool selection and refreshability for a validated
// session JTI. The selection is nil for an all-tools session. Any error means the request
// must be rejected: missing row (a live jti always has one — refresh
// rotation revokes the old jti before soft-deleting its row), database
// failure, or a malformed stored policy.
func (s *Service) loadSessionPolicy(ctx context.Context, endpoint *ResolvedMcpEndpoint, jti string) (sessionPolicy, error) {
	issuerID := endpoint.UserSessionIssuerID.String()

	var entry sessionPolicyEntry
	live, _ := ctx.Value(liveSessionPolicyKey{}).(bool)
	cachedPolicy := false
	rowConfirmed := false
	if !live {
		if cached, err := s.sessionPolicyCache.Get(ctx, sessionPolicyCacheKey(issuerID, jti)); err == nil {
			entry = cached
			cachedPolicy = true
		}
	}
	if !cachedPolicy {
		row, derr := usersessions_repo.New(s.db).GetUserSessionPolicyByJTI(ctx, usersessions_repo.GetUserSessionPolicyByJTIParams{
			UserSessionIssuerID: endpoint.UserSessionIssuerID,
			Jti:                 jti,
		})
		if derr != nil {
			if errors.Is(derr, pgx.ErrNoRows) {
				return sessionPolicy{}, fmt.Errorf("user session row not found for jti: %w", errSessionPolicyGone)
			}
			return sessionPolicy{}, fmt.Errorf("load session policy: %w", derr)
		}
		rowConfirmed = true
		entry = sessionPolicyEntry{
			IssuerID:     issuerID,
			JTI:          jti,
			Selection:    row.ToolSelection,
			Refreshable:  row.Refreshable,
			CredentialID: row.CredentialID,
		}
		if !live {
			if cerr := s.sessionPolicyCache.Store(ctx, entry); cerr != nil {
				s.logger.WarnContext(ctx, "cache session policy", attr.SlogError(cerr))
			}
		}
	}

	sel, err := toolfilter.ParseSessionPolicy(entry.Selection)
	if err != nil {
		return sessionPolicy{}, fmt.Errorf("parse stored tool selection: %w", err)
	}
	return sessionPolicy{selection: sel, refreshable: entry.Refreshable, credentialID: entry.CredentialID, rowConfirmed: rowConfirmed}, nil
}

// requireLiveSession rejects a bearer whose user_sessions row is no longer
// live. A missing row is a verdict and wraps errSessionPolicyGone; a failed
// lookup is returned as an error without a verdict.
func (s *Service) requireLiveSession(ctx context.Context, endpoint *ResolvedMcpEndpoint, jti string) error {
	key := sessionLivenessKey{issuerID: endpoint.UserSessionIssuerID, jti: jti}
	live, ok := s.sessionLiveness.Get(key)
	if !ok {
		_, err := usersessions_repo.New(s.db).GetUserSessionPolicyByJTI(ctx, usersessions_repo.GetUserSessionPolicyByJTIParams{
			UserSessionIssuerID: endpoint.UserSessionIssuerID,
			Jti:                 jti,
		})
		switch {
		case errors.Is(err, pgx.ErrNoRows):
			live = false
		case err != nil:
			return fmt.Errorf("check session liveness: %w", err)
		default:
			live = true
		}
		s.sessionLiveness.Add(key, live)
	}
	if !live {
		return fmt.Errorf("user session row not found for jti: %w", errSessionPolicyGone)
	}
	return nil
}
