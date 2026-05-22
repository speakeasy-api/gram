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
	"github.com/speakeasy-api/gram/server/internal/customdomains"
	"github.com/speakeasy-api/gram/server/internal/mv"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
	"github.com/speakeasy-api/gram/server/internal/urn"
	usersessions_repo "github.com/speakeasy-api/gram/server/internal/usersessions/repo"
)

// EndpointRef is the cached-state addressing reference for an
// in-flight Gram-as-AS authn challenge. It captures only what's needed
// to re-resolve the originating endpoint when a handler resumes a
// challenge from Redis (e.g. HandleIDPCallback after the IDP round-trip,
// or HandleConsent on POST). Keeping this as a reference rather than a
// snapshot is deliberate: downstream handlers re-resolve the endpoint
// on each entry so mutations to the underlying row (issuer change,
// visibility flip) take effect inside the 10-min challenge window.
type EndpointRef struct {
	// Set when the endpoint belongs to a custom domain, otherwise null.
	CustomDomainID uuid.NullUUID `json:"custom_domain_id"`

	// BaseURL is the public base URL the challenge was minted under,
	// stamped at mint time. For custom-domain challenges this is
	// "https://<custom-domain>"; otherwise it is the server's default
	// URL (s.serverURL.String()). Always populated by new mints so
	// HandleIDPCallback can rebuild the consent redirect from cache
	// alone — the IDP callback is registered at a global URL and loses
	// the request's customdomains.Context. In-flight states minted
	// before this field landed will have BaseURL="" and fall back to
	// the server default origin until the 10-min challenge TTL elapses.
	BaseURL string `json:"base_url,omitempty"`

	// McpServerID, when valid, identifies the mcp_servers row that owns
	// this challenge. Populated by /x/mcp callers whose endpoint
	// addresses resolve through mcp_endpoints → mcp_servers; zero for
	// /mcp callers.
	McpServerID uuid.NullUUID `json:"mcp_server_id"`

	// Path of a toolset-backed endpoint. Set for /mcp and toolset-backed
	// /x/mcp challenges.
	McpSlug string `json:"mcp_slug"`

	// RouteBase is the URL path prefix the challenge was minted under
	// ("mcp" or "x/mcp"). Empty value is treated as "mcp" by callers for
	// backward compatibility with states minted before this field was
	// added.
	RouteBase string `json:"route_base,omitempty"`
}

// AuthnChallengeState is the in-flight context of a single Gram-as-AS authn
// challenge — the OAuth client's request, the issuer it's against, and the
// subject once it has been resolved. Stored in Redis under
// `authnChallenge:{ID}` for ~10 minutes — long enough for the user to
// round-trip through the IDP and land on /connect, short enough that
// abandoned flows don't pile up.
type AuthnChallengeState struct {
	ID                  string      `json:"id"`
	UserSessionIssuerID uuid.UUID   `json:"user_session_issuer_id"`
	Endpoint            EndpointRef `json:"endpoint"`
	ClientID            string      `json:"client_id"`
	RedirectURI         string      `json:"redirect_uri"`
	State               string      `json:"state,omitempty"`
	CodeChallenge       string      `json:"code_challenge"`
	CodeChallengeMethod string      `json:"code_challenge_method"`
	CSRFToken           string      `json:"csrf_token"`
	// Subject is stamped exactly once before consent is rendered:
	// HandleAuthorize stamps `anonymous:<uuid>` for public toolsets, and
	// HandleIDPCallback stamps `user:<id>` for private toolsets. Pointer so
	// the Redis JSON can round-trip the private pre-IDP state (the URN's
	// MarshalJSON refuses to serialise a zero-value SessionSubject).
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
// stamps a contextvalues.AuthContext scoped to the endpoint's org/project.
// Returns ok=false on any of: missing token, bad signature, expired/
// notBefore, audience mismatch, jti revoked, unparseable subject URN.
//
// Anonymous subjects deliberately leave the context untouched (ok=true,
// no AuthContext set). The request belongs to no known principal, so
// stamping the endpoint's org as ActiveOrganizationID would misrepresent
// the caller as a member of that org. Downstream code on the public
// path reads org/project off the resolved endpoint directly, the same
// way it does for unauthenticated public-endpoint traffic.
//
// SessionID and AccountType are populated for non-anonymous subjects so
// authz.Engine.ShouldEnforce / PrepareContext treat the request as a
// real authenticated session — without them the mcp:connect RBAC check
// silently bypasses on enterprise endpoints (ShouldEnforce returns false
// when AccountType != "enterprise"; PrepareContext skips when SessionID
// is nil).
func (s *Service) validateUserSessionToken(ctx context.Context, token string, endpoint *ResolvedMcpEndpoint) (context.Context, *urn.SessionSubject, bool) {
	if token == "" {
		return ctx, nil, false
	}
	subject, jti, err := s.userSessionSigner.ValidateBearer(ctx, token, endpoint.AudienceURN, s.chatSessionsManager)
	if err != nil {
		return ctx, nil, false
	}

	if subject.Kind == urn.SessionSubjectKindAnonymous {
		return ctx, &subject, true
	}

	orgMetadata, err := mv.DescribeOrganization(ctx, s.logger, s.orgsRepo, s.billingRepository, endpoint.OrganizationID)
	if err != nil {
		return ctx, nil, false
	}
	projectID := endpoint.ProjectID
	authCtx := &contextvalues.AuthContext{
		ActiveOrganizationID:  endpoint.OrganizationID,
		ProjectID:             &projectID,
		UserID:                "",
		ExternalUserID:        "",
		APIKeyID:              "",
		SessionID:             &jti,
		OrganizationSlug:      orgMetadata.Slug,
		Email:                 nil,
		AccountType:           orgMetadata.GramAccountType,
		HasActiveSubscription: orgMetadata.HasActiveSubscription,
		Whitelisted:           orgMetadata.Whitelisted,
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
	return contextvalues.SetAuthContext(ctx, authCtx), &subject, true
}

// WriteAuthenticateChallenge sets the WWW-Authenticate header and returns an
// oops.CodeUnauthorized error. The 401 status and response body come from
// the oops error middleware; the helper owns only the header.
//
// Header shape (RFC 9728 §5.3):
//
//	Bearer resource_metadata="<protectedResourceURL>"
//
// Callers build the URL — the canonical RFC 9728 path is
// `<base>/.well-known/oauth-protected-resource/<routeBase>/<slug>`, which is
// exactly what a spec-compliant client constructs from a resource URL of
// `<base>/<routeBase>/<slug>`.
func WriteAuthenticateChallenge(w http.ResponseWriter, protectedResourceURL, message string) error {
	w.Header().Set(
		"WWW-Authenticate",
		fmt.Sprintf(`Bearer resource_metadata="%s"`, protectedResourceURL),
	)
	if message == "" {
		return oops.C(oops.CodeUnauthorized)
	}
	return oops.E(oops.CodeUnauthorized, nil, "%s", message)
}

// BaseURLForRequest returns the public base URL the runtime request was
// addressed at — the custom domain when one is bound to the request
// context, the server's default origin otherwise. Exposed so /x/mcp
// callers building post-resolution OAuth URLs see the same origin /mcp
// callers do.
func (s *Service) BaseURLForRequest(r *http.Request) string {
	if domainCtx := customdomains.FromContext(r.Context()); domainCtx != nil {
		return fmt.Sprintf("https://%s", domainCtx.Domain)
	}
	return s.serverURL.String()
}

// ApplyIssuerGate runs the issuer-gated authentication branch shared by
// the toolset-keyed (/mcp) and mcp_server-keyed (/x/mcp) MCP runtime
// paths. It validates the bearer token as a user-session JWT, falls back
// to an assistant-runtime JWT scoped to the endpoint's project, and on
// success resolves the upstream remote-session access token configured
// for the issuer.
//
// On success: returns the request context stamped with the resolved
// principal plus the upstream access token. The token is "" when the
// issuer has no remote_session_clients bound; today the inv.Check in
// remotesessions.ResolveOneAccessToken caps the upstream to exactly 0 or
// 1, so the return type is a single string rather than a slice. Callers
// wrap the non-empty value into an oauthTokenInputs as needed for
// downstream tool-dispatch chains.
//
// On failure: writes a 401 + WWW-Authenticate to w and returns the
// CodeUnauthorized error from WriteAuthenticateChallenge. The
// resource_metadata URL is built from baseURL + endpoint.RouteBase +
// endpoint.Slug so a /x/mcp request gets pointed at /x/mcp's
// protected-resource metadata, not /mcp's.
//
// /x/mcp uses this to gate requests on mcp_servers.user_session_issuer_id
// before dispatching to its remote backend or delegating to
// ServeToolsetResolved with the gate skipped.
func (s *Service) ApplyIssuerGate(
	ctx context.Context,
	w http.ResponseWriter,
	authToken, baseURL string,
	endpoint *ResolvedMcpEndpoint,
) (context.Context, string, error) {
	protectedResourceURL, err := endpoint.ProtectedResourceURL(baseURL)
	if err != nil {
		return ctx, "", oops.E(oops.CodeUnexpected, err, "build protected-resource URL").Log(ctx, s.logger)
	}

	newCtx, subject, ok := s.validateUserSessionToken(ctx, authToken, endpoint)
	if !ok {
		// Accept an assistant-runtime JWT, but only when the assistant
		// belongs to the endpoint's project — otherwise a token minted
		// in project A could resolve a remote_session linked under
		// the same user in project B.
		if assistCtx, claims, aerr := s.assistantTokens.Authorize(ctx, authToken); aerr == nil && claims.ProjectID == endpoint.ProjectID.String() {
			ssubj := urn.NewUserSubject(claims.UserID)
			newCtx, subject, ok = assistCtx, &ssubj, true
		}
	}
	if !ok {
		return ctx, "", WriteAuthenticateChallenge(w, protectedResourceURL, "expired or invalid access token")
	}

	// Resolve the upstream remote_session for this subject before
	// running the legacy auth chain. The resolver short-circuits to
	// no-op when the issuer has no remote_session_clients bound;
	// otherwise it either supplies the upstream access token (fed
	// into tokenInputs so it satisfies the endpoint's oauth2 scheme
	// downstream) or fails with ErrNoValidToken — which the user
	// resolves by re-linking via {routeBase}/{slug}/connect.
	var upstreamToken string
	if subject != nil {
		upstream, rerr := s.remoteChallengeMgr.ResolveOneAccessToken(newCtx, endpoint.ProjectID, endpoint.UserSessionIssuerID, *subject)
		switch {
		case errors.Is(rerr, remotesessions.ErrNoValidToken):
			return ctx, "", WriteAuthenticateChallenge(w, protectedResourceURL, "")
		case rerr != nil:
			return ctx, "", oops.E(oops.CodeUnexpected, rerr, "resolve remote session").Log(newCtx, s.logger)
		}
		upstreamToken = upstream
	}
	return newCtx, upstreamToken, nil
}

var errToolsetEndpointMismatch = errors.New("authn challenge endpoint does not match toolset")

// RequireUserSessionIssuer verifies the endpoint's user_session_issuer_id
// FK still resolves to a live row. Returns CodeNotFound when the issuer
// was deleted out from under the endpoint, CodeUnexpected on lookup
// failure. Callers are responsible for first checking that the endpoint
// is issuer-gated.
//
// Exported so /x/mcp's [Service.buildResolvedMcpEndpoint] can include
// the live-FK check in the same chokepoint as the
// NewResolvedMcpEndpointFromMcpServer construction.
func (s *Service) RequireUserSessionIssuer(ctx context.Context, endpoint *ResolvedMcpEndpoint) error {
	if _, err := usersessions_repo.New(s.db).GetUserSessionIssuerByID(ctx, usersessions_repo.GetUserSessionIssuerByIDParams{
		ID:        endpoint.UserSessionIssuerID,
		ProjectID: endpoint.ProjectID,
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
