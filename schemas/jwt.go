// /schemas/jwt.go
//
// Unified SessionClaims: one JWT shape for chat sessions and user sessions.
// See spike.md §4.5 for rationale.
//
// Signing:    HS256 with GRAM_JWT_SIGNING_KEY (existing chat-session manager key).
// Delivery:   Authorization: Bearer <token> on every request. The legacy
//             Gram-Chat-Session header is deprecated.
// Revocation: by JTI in the existing chat_session_revoked:{jti} Redis cache (24h TTL).
//
// The two flows differ only in `sub` and `aud`:
//   - User session: sub = principal URN; aud = toolset slug.
//   - Chat session:   sub = principal URN; aud = embed origin.
//
// Valid `sub` URN shapes (enforced at sign time):
//   - user:<id>
//   - apikey:<uuid>
//   - anonymous:<mcp-session-id>
//
// Any of the above MAY carry an optional `?externalId=<external-id>` suffix
// — e.g., `apikey:abc123?externalId=customer-user-42`. The suffix attaches
// a customer-supplied external user identifier, used today for embedded
// chat sessions where the customer's app authenticates the user and we
// need to track per-user state without minting a Gram user. The URN parser
// strips the suffix, validates the base URN normally, and stamps the
// externalId onto the auth context for downstream consumption. This lets
// one input principal (typically an API key) issue many chat sessions, one
// per external user.
//
// role:<slug> is NOT valid in `sub` — roles are not authentication subjects.
// urn.APIKey remains a parallel URN kind (it is not a PrincipalType); we
// deliberately keep that separation but allow both PrincipalType URNs and
// APIKey URNs to share the `sub` claim.

package schemas

import "github.com/golang-jwt/jwt/v5"

// SessionClaims is the unified JWT claim shape for all Gram-issued session
// tokens. It carries ONLY the standard OIDC registered claims — no
// Gram-specific extras. Org / project / etc. are resolved from the session
// record in Redis (keyed by the refresh-token hash on token exchange,
// or by JTI revocation cache on access-token validation).
type SessionClaims struct {
	// Standard registered claims (jwt.RegisteredClaims):
	//   ID        string         — JTI: UUIDv4, used for revocation tracking
	//   Issuer    string         — Gram issuer URL
	//   Subject   string         — principal URN (see file header)
	//   Audience  []string       — toolset slug (user session) | embed origin (chat session)
	//   IssuedAt  *NumericDate
	//   ExpiresAt *NumericDate
	//   NotBefore *NumericDate   — nil (not set)
	jwt.RegisteredClaims
}

// Notes on what's NOT here:
//
//   - The legacy ChatSessionClaims.APIKeyID claim is gone. When the principal
//     is an API key, the URN `apikey:<uuid>` lives in `sub`.
//
//   - org_id / project_id / org_slug / project_slug are not on the JWT.
//     They are derivable from the session record (which is keyed by sub +
//     aud), so duplicating them in the token bloats every request and
//     creates two sources of truth.
//
//   - ExternalUserID (legacy chat-session JWT) is replaced by the
//     `?externalId=<id>` suffix on `sub`. See the URN-shape section above.
