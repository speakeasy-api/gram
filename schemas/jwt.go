// /schemas/jwt.go
//
// Unified SessionClaims: one JWT shape for chat sessions and client sessions.
// See spike.md §4.5 for rationale.
//
// Signing:    HS256 with GRAM_JWT_SIGNING_KEY (existing chat-session manager key).
// Delivery:   Authorization: Bearer <token> on every request. The legacy
//             Gram-Chat-Session header is deprecated.
// Revocation: by JTI in the existing chat_session_revoked:{jti} Redis cache (24h TTL).
//
// The two flows differ only in `sub` and `aud`:
//   - Client session: sub = principal URN; aud = toolset slug.
//   - Chat session:   sub = principal URN; aud = embed origin.
//
// Valid `sub` URN shapes (enforced at sign time):
//   - user:<id>
//   - apikey:<uuid>
//   - anonymous:<mcp-session-id>
//
// role:<slug> is NOT valid in `sub` — roles are not authentication subjects.
// urn.APIKey remains a parallel URN kind (it is not a PrincipalType); we
// deliberately keep that separation but allow both PrincipalType URNs and
// APIKey URNs to share the `sub` claim.

package schemas

import "github.com/golang-jwt/jwt/v5"

// SessionClaims is the unified JWT claim shape for all Gram-issued session tokens.
type SessionClaims struct {
	// Standard registered claims (jwt.RegisteredClaims):
	//   ID        string         — JTI: UUIDv4, used for revocation tracking
	//   Issuer    string         — Gram issuer URL
	//   Subject   string         — principal URN (see file header)
	//   Audience  []string       — toolset slug (client session) | embed origin (chat session)
	//   IssuedAt  *NumericDate
	//   ExpiresAt *NumericDate
	//   NotBefore *NumericDate   — nil (not set)
	jwt.RegisteredClaims

	// Gram-specific
	OrgID            string `json:"org_id"`
	ProjectID        string `json:"project_id"`
	OrganizationSlug string `json:"organization_slug"`
	ProjectSlug      string `json:"project_slug"`

	// Optional: only set on chat sessions issued for an embedded chat user.
	// Absent on client-session tokens.
	ExternalUserID *string `json:"external_user_id,omitempty"`
}

// Note: the legacy ChatSessionClaims.APIKeyID claim is gone. When the principal
// is an API key, the URN `apikey:<uuid>` lives in `sub` and is the source of truth.
