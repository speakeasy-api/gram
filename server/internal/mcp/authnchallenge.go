// OAuth authorization code exchange handlers for MCP clients. Issuer-gated
// toolsets (toolsets.user_session_issuer_id set) flow through the OAuth 2.1
// + RFC 7591 / RFC 9728 handlers in this package; toolsets without an
// issuer fall through to the legacy paths in wellknown.Resolve*.
//
// This file holds the shared types, helpers, and the WWW-Authenticate
// challenge writer. Each handler lives in its own file:
//
//   - authnchallenge_well_known.go — RFC 9728 protected-resource +
//     RFC 8414 authorization-server metadata.
//   - authnchallenge_register.go    — RFC 7591 Dynamic Client Registration.
//   - authnchallenge_authorize.go   — RFC 6749 §4.1.1 authorization endpoint.
//   - authnchallenge_idp_callback.go — Speakeasy IDP callback (private path).
//   - authnchallenge_consent.go     — consent UI + POST.
//   - authnchallenge_token.go       — RFC 6749 §4.1.3 / §6 token endpoint.
//   - authnchallenge_revoke.go      — RFC 7009 token revocation.

package mcp

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oops"
	toolsets_repo "github.com/speakeasy-api/gram/server/internal/toolsets/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
	usersessions_repo "github.com/speakeasy-api/gram/server/internal/usersessions/repo"
)

// AuthnChallengeState is the in-flight context of a single Gram-as-AS authn
// challenge — the OAuth client's request, the issuer it's against, and the
// resolved subject (stamped later in the flow). Stored in Redis under
// `authnChallenge:{ID}` for ~10 minutes — long enough for the user to
// round-trip through the IDP and land on /connect, short enough that
// abandoned flows don't pile up.
type AuthnChallengeState struct {
	ID                  string    `json:"id"`
	UserSessionIssuerID uuid.UUID `json:"user_session_issuer_id"`
	ToolsetID           uuid.UUID `json:"toolset_id"`
	ClientID            string    `json:"client_id"`
	RedirectURI         string    `json:"redirect_uri"`
	State               string    `json:"state,omitempty"`
	CodeChallenge       string    `json:"code_challenge"`
	CodeChallengeMethod string    `json:"code_challenge_method"`
	// Subject is nil on creation; HandleIDPCallback (private path)
	// stamps `user:<id>`, HandleConsent's POST mints a fresh
	// `anonymous:<uuid>` on the public path. Pointer so the Redis JSON
	// can round-trip an unstamped state (the URN's MarshalJSON refuses
	// to serialise a zero-value SessionSubject).
	Subject   *urn.SessionSubject `json:"subject,omitempty"`
	CreatedAt time.Time           `json:"created_at"`
}

var _ cache.CacheableObject[AuthnChallengeState] = (*AuthnChallengeState)(nil)

// CacheKey implements cache.CacheableObject.
func (a AuthnChallengeState) CacheKey() string { return "authnChallenge:" + a.ID }

// AdditionalCacheKeys implements cache.CacheableObject. Single-key entry; no
// fan-out. (Per the Cleanup ticket in project.md, AdditionalCacheKeys is
// itself slated for removal from the interface.)
func (a AuthnChallengeState) AdditionalCacheKeys() []string { return []string{} }

// TTL implements cache.CacheableObject.
func (a AuthnChallengeState) TTL() time.Duration { return 10 * time.Minute }

// UserSessionGrant is the short-lived OAuth authorization grant minted by
// HandleConsent's POST and consumed by HandleToken's authorization_code
// grant. Stored in Redis under
// `userSessionGrant:{user_session_issuer_id}:{code}` for ~10 minutes.
type UserSessionGrant struct {
	Code                string             `json:"code"`
	UserSessionIssuerID uuid.UUID          `json:"user_session_issuer_id"`
	UserSessionClientID uuid.UUID          `json:"user_session_client_id"`
	ClientID            string             `json:"client_id"`
	RedirectURI         string             `json:"redirect_uri"`
	CodeChallenge       string             `json:"code_challenge"`
	CodeChallengeMethod string             `json:"code_challenge_method"`
	Subject             urn.SessionSubject `json:"subject"`
	CreatedAt           time.Time          `json:"created_at"`
}

var _ cache.CacheableObject[UserSessionGrant] = (*UserSessionGrant)(nil)

// CacheKey implements cache.CacheableObject.
func (g UserSessionGrant) CacheKey() string {
	return "userSessionGrant:" + g.UserSessionIssuerID.String() + ":" + g.Code
}

// AdditionalCacheKeys implements cache.CacheableObject. Single-key entry; no
// fan-out.
func (g UserSessionGrant) AdditionalCacheKeys() []string { return []string{} }

// TTL implements cache.CacheableObject. 10 minutes is the standard OAuth code
// lifetime — enough for a slow round trip from the MCP client to /token, short
// enough to limit blast radius if the code leaks.
func (g UserSessionGrant) TTL() time.Duration { return 10 * time.Minute }

// validateUserSessionToken delegates the JWT verify + revocation check to
// usersessions.Signer.ValidateBearer, then — for user / API-key subjects —
// stamps a contextvalues.AuthContext scoped to the toolset's org/project.
// Returns ok=false on any of: missing token, bad signature, expired/
// notBefore, audience mismatch, jti revoked, unparseable subject URN.
//
// Anonymous subjects deliberately leave the context untouched (ok=true,
// no AuthContext set). The request belongs to no known principal, so
// stamping the toolset's org as ActiveOrganizationID would misrepresent
// the caller as a member of that org. Downstream code on the public
// path reads org/project off the toolset row directly, the same way it
// does for unauthenticated public-toolset traffic.
func (s *Service) validateUserSessionToken(ctx context.Context, token string, toolset *toolsets_repo.Toolset) (context.Context, bool) {
	if token == "" {
		return ctx, false
	}
	subject, _, err := s.userSessionSigner.ValidateBearer(ctx, token, urn.NewToolset(toolset.ID).String(), s.chatSessionsManager)
	if err != nil {
		return ctx, false
	}

	if subject.Kind == urn.SessionSubjectKindAnonymous {
		return ctx, true
	}

	authCtx := &contextvalues.AuthContext{
		ActiveOrganizationID:  toolset.OrganizationID,
		ProjectID:             &toolset.ProjectID,
		UserID:                "",
		ExternalUserID:        "",
		APIKeyID:              "",
		SessionID:             nil,
		OrganizationSlug:      "",
		Email:                 nil,
		AccountType:           "",
		HasActiveSubscription: false,
		Whitelisted:           false,
		ProjectSlug:           nil,
		APIKeyScopes:          nil,
		IsAdmin:               false,
	}
	switch subject.Kind {
	case urn.SessionSubjectKindUser:
		authCtx.UserID = subject.ID
	case urn.SessionSubjectKindAPIKey:
		authCtx.APIKeyID = subject.ID
	case urn.SessionSubjectKindAnonymous:
		// Unreachable: anonymous subjects return ctx untouched above. Listed
		// for exhaustiveness so the linter doesn't flag the switch.
	}
	return contextvalues.SetAuthContext(ctx, authCtx), true
}

// WriteAuthenticateChallenge sets the WWW-Authenticate header and returns an
// oops.CodeUnauthorized error. The 401 status and response body come from
// the oops error middleware; the helper owns only the header.
//
// Header shape (RFC 9728 §5.3):
//
//	Bearer resource_metadata="<base>/.well-known/oauth-protected-resource/mcp/<slug>"
//
// The path is the canonical RFC 9728 prefix path — exactly what a
// spec-compliant client constructs from a resource URL of `<base>/mcp/<slug>`.
func WriteAuthenticateChallenge(w http.ResponseWriter, baseURL, mcpSlug, message string) error {
	w.Header().Set(
		"WWW-Authenticate",
		fmt.Sprintf(`Bearer resource_metadata="%s"`, baseURL+"/.well-known/oauth-protected-resource/mcp/"+mcpSlug),
	)
	if message == "" {
		return oops.C(oops.CodeUnauthorized)
	}
	return oops.E(oops.CodeUnauthorized, nil, "%s", message)
}

// requireUserSessionIssuer verifies the toolset's user_session_issuer_id FK
// still resolves to a live row. Returns CodeNotFound when the issuer was
// deleted out from under the toolset, CodeUnexpected on lookup failure.
// Callers are responsible for first checking toolset.UserSessionIssuerID.Valid.
func (s *Service) requireUserSessionIssuer(ctx context.Context, toolset *toolsets_repo.Toolset) error {
	if _, err := usersessions_repo.New(s.db).GetUserSessionIssuerByID(ctx, usersessions_repo.GetUserSessionIssuerByIDParams{
		ID:        toolset.UserSessionIssuerID.UUID,
		ProjectID: toolset.ProjectID,
	}); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return oops.E(oops.CodeNotFound, err, "user_session_issuer not found")
		}
		return oops.E(oops.CodeUnexpected, err, "load user_session_issuer").Log(ctx, s.logger)
	}
	return nil
}

// extractClientCredentials returns the client_id + client_secret + ok from
// either the Authorization header (client_secret_basic) or the form body
// (client_secret_post). HTTP Basic wins when both are present, per RFC 6749
// §2.3.1 ("the client MAY use only one authentication method").
func extractClientCredentials(r *http.Request) (string, string, bool) {
	if id, secret, ok := r.BasicAuth(); ok && id != "" {
		return id, secret, true
	}
	id := r.PostForm.Get("client_id")
	secret := r.PostForm.Get("client_secret")
	if id == "" {
		return "", "", false
	}
	return id, secret, true
}

// sha256Hex returns the base64url-encoded SHA-256 of the input. (The name
// is historical — the encoding is base64url, not hex.)
func sha256Hex(s string) string {
	sum := sha256.Sum256([]byte(s))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

// generateOpaqueToken produces a cryptographically random 32-byte URL-safe
// token. Used as both the OAuth authorization code (HandleConsent's POST) and
// the refresh token (HandleToken). 32 bytes of entropy from crypto/rand far
// exceeds RFC 6749 §10.10's 128-bit minimum; base64url makes the value safe
// to drop in a URL query string or HTTP header without further encoding.
func generateOpaqueToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("read random bytes: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}
