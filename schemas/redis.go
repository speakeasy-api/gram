// /schemas/redis.go
//
// Redis types — short-TTL in-flight records only. Durable session state
// (client_sessions, remote_sessions) lives in Postgres — see schemas/postgres.sql
// and spike.md §4.1.
//
// All types implement cache.CacheableObject[T]; values are JSON-serialised by
// cache.TypedCacheObject[T]. Encrypted fields use encryption.Client before persist.
//
// Where a key segment shows {principalURN}, the URN is one of:
//   - user:<id>
//   - apikey:<uuid>
//   - anonymous:<mcp-session-id>
// (role:<slug> is NOT a valid principal URN — roles are not authentication subjects.)

package schemas

import (
	"time"

	"github.com/google/uuid"
)

// ---------------------------------------------------------------------------
// 1. ChallengeState — thin re-entry handle for the challenge flow.
//    Cache key: "challengeState:{id}"
//    TTL: time.Until(ExpiresAt) (~10 min)
//
// Holds the MCP Client's OAuth request context plus the resolved principal
// (set after Phase 2). Each callback in the challenge flow re-loads it by id
// and re-runs buildRequiredChallenge to decide the next 302.
// ---------------------------------------------------------------------------

type ChallengeState struct {
	ID                    string    `json:"id"`
	ClientSessionIssuerID uuid.UUID `json:"client_session_issuer_id"`
	PrincipalURN          string    `json:"principal_urn,omitempty"` // resolved after Phase 2

	// MCP Client's OAuth request context — needed to complete the code grant in Phase 4.
	MCPClientID            string `json:"mcp_client_id"`
	MCPClientRedirectURI   string `json:"mcp_client_redirect_uri"`
	MCPClientCodeChallenge string `json:"mcp_client_code_challenge"`
	MCPClientState         string `json:"mcp_client_state"`
	Scope                  string `json:"scope"`

	CreatedAt time.Time `json:"created_at"`
	ExpiresAt time.Time `json:"expires_at"`
}

func ChallengeStateCacheKey(id string) string {
	return "challengeState:" + id
}

func (c ChallengeState) CacheKey() string   { return ChallengeStateCacheKey(c.ID) }
func (c ChallengeState) TTL() time.Duration { return time.Until(c.ExpiresAt) }

// ---------------------------------------------------------------------------
// 2. ClientSessionGrant — short-lived authorization-code grant on the AS path.
//    Cache key: "clientSessionGrant:{clientSessionIssuerID}:{code}"
//    TTL: time.Until(ExpiresAt) (~10 min)
// ---------------------------------------------------------------------------

type ClientSessionGrant struct {
	ClientSessionIssuerID uuid.UUID `json:"client_session_issuer_id"`
	Code                  string    `json:"code"`
	ClientID              string    `json:"client_id"`
	RedirectURI           string    `json:"redirect_uri"`
	Scope                 string    `json:"scope"`
	State                 string    `json:"state"`
	CodeChallenge         string    `json:"code_challenge"`
	CodeChallengeMethod   string    `json:"code_challenge_method"`
	PrincipalURN          string    `json:"principal_urn"`
	CreatedAt             time.Time `json:"created_at"`
	ExpiresAt             time.Time `json:"expires_at"`
}

func ClientSessionGrantCacheKey(clientSessionIssuerID uuid.UUID, code string) string {
	return "clientSessionGrant:" + clientSessionIssuerID.String() + ":" + code
}

func (g ClientSessionGrant) CacheKey() string {
	return ClientSessionGrantCacheKey(g.ClientSessionIssuerID, g.Code)
}
func (g ClientSessionGrant) TTL() time.Duration { return time.Until(g.ExpiresAt) }

// ---------------------------------------------------------------------------
// 3. RemoteSessionAuthState — in-flight remote OAuth authorization state.
//    Cache key: "remoteSessionAuthState:{stateID}"
//    TTL: time.Until(ExpiresAt) (~10 min)
//
// Successor to legacy ExternalOAuthState. Consumed on the OAuth callback to
// rebuild context (which principal, which issuer, which client) and complete
// the code exchange.
// ---------------------------------------------------------------------------

type RemoteSessionAuthState struct {
	StateID               string    `json:"state_id"`
	PrincipalURN          string    `json:"principal_urn"`
	ClientSessionIssuerID uuid.UUID `json:"client_session_issuer_id"`
	RemoteOAuthIssuerID   uuid.UUID `json:"remote_oauth_issuer_id"`
	RemoteOAuthClientID   uuid.UUID `json:"remote_oauth_client_id"`
	CodeVerifier          string    `json:"code_verifier"`
	RedirectURI           string    `json:"redirect_uri"`
	CreatedAt             time.Time `json:"created_at"`
	ExpiresAt             time.Time `json:"expires_at"`
}

func RemoteSessionAuthStateCacheKey(stateID string) string {
	return "remoteSessionAuthState:" + stateID
}

func (s RemoteSessionAuthState) CacheKey() string {
	return RemoteSessionAuthStateCacheKey(s.StateID)
}
func (s RemoteSessionAuthState) TTL() time.Duration { return time.Until(s.ExpiresAt) }

// ---------------------------------------------------------------------------
// 4. RemoteSessionPKCE — verifier storage during a remote authorize redirect.
//    Cache key: "remoteSessionPKCE:{nonce}"
//    TTL: 10 minutes fixed
//
// Successor to legacy UpstreamPKCEVerifier.
// ---------------------------------------------------------------------------

type RemoteSessionPKCE struct {
	Nonce    string `json:"nonce"`
	Verifier string `json:"verifier"`
}

func (v RemoteSessionPKCE) CacheKey() string   { return "remoteSessionPKCE:" + v.Nonce }
func (v RemoteSessionPKCE) TTL() time.Duration { return 10 * time.Minute }
