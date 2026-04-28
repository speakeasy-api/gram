// /schemas/redis.go
//
// Redis types for the new clientsessions / remotesessions surface.
// See spike.md §4.3 for rationale.
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
// 1. ClientSession — the Redis record /token reads when exchanging a refresh token.
//    Cache key: "clientSession:{refreshTokenHash}"
//    TTL: time.Until(RefreshExpiresAt)
//
// Keyed by refresh-token hash because that's the operation that actually needs
// the lookup. The bookkeeping reverse-index lives on ClientSessionIndex.
// ---------------------------------------------------------------------------

type ClientSession struct {
	ClientSessionIssuerID uuid.UUID `json:"client_session_issuer_id"`
	PrincipalURN          string    `json:"principal_urn"`
	RefreshTokenHash      string    `json:"-"` // SHA-256 of the refresh token; never persisted in the clear
	JTI                   string    `json:"jti"`
	RefreshExpiresAt      time.Time `json:"refresh_expires_at"`
	CreatedAt             time.Time `json:"created_at"`
}

func ClientSessionCacheKey(refreshTokenHash string) string {
	return "clientSession:" + refreshTokenHash
}

func (c ClientSession) CacheKey() string              { return ClientSessionCacheKey(c.RefreshTokenHash) }
func (c ClientSession) AdditionalCacheKeys() []string { return nil }
func (c ClientSession) TTL() time.Duration            { return time.Until(c.RefreshExpiresAt) }

// ---------------------------------------------------------------------------
// 2. ClientSessionIndex — bookkeeping reverse-index by principal.
//    Cache key: "clientSessionByPrincipal:{principalURN}:{clientSessionIssuerID}"
//    TTL: time.Until(LatestRefreshExpiresAt)
//
// Answers "what active sessions does this principal have at this issuer?" —
// the lookup needed for revoke-all, listing, and operational queries.
// Anonymous principals encode their session id in the URN itself
// (anonymous:<mcp-session-id>), so no separate session-id concept is required.
// ---------------------------------------------------------------------------

type ClientSessionIndex struct {
	PrincipalURN           string    `json:"principal_urn"`
	ClientSessionIssuerID  uuid.UUID `json:"client_session_issuer_id"`
	ActiveRefreshHashes    []string  `json:"active_refresh_hashes"` // pointers into ClientSession docs
	LatestRefreshExpiresAt time.Time `json:"latest_refresh_expires_at"`
	UpdatedAt              time.Time `json:"updated_at"`
}

func ClientSessionIndexCacheKey(principalURN string, clientSessionIssuerID uuid.UUID) string {
	return "clientSessionByPrincipal:" + principalURN + ":" + clientSessionIssuerID.String()
}

func (c ClientSessionIndex) CacheKey() string {
	return ClientSessionIndexCacheKey(c.PrincipalURN, c.ClientSessionIssuerID)
}
func (c ClientSessionIndex) AdditionalCacheKeys() []string { return nil }
func (c ClientSessionIndex) TTL() time.Duration            { return time.Until(c.LatestRefreshExpiresAt) }

// ---------------------------------------------------------------------------
// 3. RemoteSession — one per remote_oauth_issuer attached to a client session.
//    Cache key: "remoteSession:{principalURN}:{clientSessionIssuerID}:{remoteOAuthIssuerID}"
//    TTL: time.Until(RefreshExpiresAt)
//
// Holds upstream access and refresh tokens with INDEPENDENT expiries —
// access can lapse without invalidating the refresh path. The TTL on the
// document itself is governed by the (longer) refresh expiry.
// ---------------------------------------------------------------------------

type RemoteSession struct {
	PrincipalURN          string    `json:"principal_urn"`
	ClientSessionIssuerID uuid.UUID `json:"client_session_issuer_id"`
	RemoteOAuthIssuerID   uuid.UUID `json:"remote_oauth_issuer_id"`
	RemoteOAuthClientID   uuid.UUID `json:"remote_oauth_client_id"`

	AccessTokenEncrypted  string    `json:"access_token_encrypted"`
	AccessExpiresAt       time.Time `json:"access_expires_at"` // independent of refresh expiry
	RefreshTokenEncrypted string    `json:"refresh_token_encrypted,omitempty"`
	RefreshExpiresAt      time.Time `json:"refresh_expires_at"` // controls Redis TTL

	Scopes    []string  `json:"scopes,omitempty"`
	CreatedAt time.Time `json:"created_at"`
}

func RemoteSessionCacheKey(principalURN string, clientSessionIssuerID, remoteOAuthIssuerID uuid.UUID) string {
	return "remoteSession:" + principalURN + ":" + clientSessionIssuerID.String() + ":" + remoteOAuthIssuerID.String()
}

func (r RemoteSession) CacheKey() string {
	return RemoteSessionCacheKey(r.PrincipalURN, r.ClientSessionIssuerID, r.RemoteOAuthIssuerID)
}
func (r RemoteSession) AdditionalCacheKeys() []string { return nil }
func (r RemoteSession) TTL() time.Duration            { return time.Until(r.RefreshExpiresAt) }

// ---------------------------------------------------------------------------
// 4. ClientSessionGrant — short-lived authorization-code grant on the AS path.
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
func (g ClientSessionGrant) AdditionalCacheKeys() []string { return nil }
func (g ClientSessionGrant) TTL() time.Duration            { return time.Until(g.ExpiresAt) }

// ---------------------------------------------------------------------------
// 5. RemoteSessionAuthState — in-flight remote OAuth authorization state.
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
func (s RemoteSessionAuthState) AdditionalCacheKeys() []string { return nil }
func (s RemoteSessionAuthState) TTL() time.Duration            { return time.Until(s.ExpiresAt) }

// ---------------------------------------------------------------------------
// 6. RemoteSessionPKCE — verifier storage during a remote authorize redirect.
//    Cache key: "remoteSessionPKCE:{nonce}"
//    TTL: 10 minutes fixed
//
// Successor to legacy UpstreamPKCEVerifier.
// ---------------------------------------------------------------------------

type RemoteSessionPKCE struct {
	Nonce    string `json:"nonce"`
	Verifier string `json:"verifier"`
}

func (v RemoteSessionPKCE) CacheKey() string              { return "remoteSessionPKCE:" + v.Nonce }
func (v RemoteSessionPKCE) AdditionalCacheKeys() []string { return nil }
func (v RemoteSessionPKCE) TTL() time.Duration            { return 10 * time.Minute }
