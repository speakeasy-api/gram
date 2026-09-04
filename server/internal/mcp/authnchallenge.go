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
	"log/slog"
	"net/http"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/customdomains"
	"github.com/speakeasy-api/gram/server/internal/mcp/mcpmetrics"
	"github.com/speakeasy-api/gram/server/internal/mcp/toolfilter"
	"github.com/speakeasy-api/gram/server/internal/mv"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
	"github.com/speakeasy-api/gram/server/internal/sessiontokens"
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

	// MetaMcpServerID, when valid, identifies the meta_mcp_servers row that
	// owns this challenge. Populated for meta-MCP-backed endpoints; zero
	// everywhere else. In-flight states minted before this field landed
	// simply lack it, which is safe: no meta endpoint could mint a
	// challenge before it existed.
	MetaMcpServerID uuid.NullUUID `json:"meta_mcp_server_id,omitzero"`

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
	ID string `json:"id"`
	// FlowID is the stable correlation identifier for the whole OAuth flow,
	// minted once at /authorize. Unlike ID — which idp_callback rotates to
	// rotate the Redis cache key — FlowID is preserved across the rotation
	// and copied into the UserSessionGrant so /token can log it too. Logged
	// as attr.OAuthFlowID on every handler in the flow. Empty for in-flight
	// states minted before this field landed (rolling deploy); callers treat
	// empty as "unknown" and never depend on its presence.
	FlowID              string      `json:"flow_id,omitempty"`
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
	// FirstParty marks a challenge minted by the dashboard for its own user
	// (via ServeFirstPartyConnect) rather than by a DCR-registered MCP client's
	// /authorize. First-party challenges carry no ClientID/RedirectURI: the
	// consent page renders the remote-session cards so the user can link
	// upstream providers, but there is no client to approve or redirect back to
	// — completing the connections is terminal.
	FirstParty bool `json:"first_party,omitempty"`
	// AutoConnectDone records that the consent page has already sent this
	// challenge straight to an upstream provider without the user clicking
	// Connect (see maybeAutoConnect). It is a latch, not a success flag: it is
	// set before the redirect and also by an explicit disconnect, so a denied
	// or failed upstream leg — and a deliberate disconnect — return the user to
	// a page they can act on instead of bouncing them out again.
	AutoConnectDone bool `json:"auto_connect_done,omitempty"`
}

var _ cache.CacheableObject[AuthnChallengeState] = (*AuthnChallengeState)(nil)

// CacheKey implements cache.CacheableObject.
func (a AuthnChallengeState) CacheKey() string { return "authnChallenge:" + a.ID }

// TTL implements cache.CacheableObject.
func (a AuthnChallengeState) TTL() time.Duration { return 10 * time.Minute }

// mintOriginOr returns the public origin this challenge was minted under: the
// mint-time snapshot when present, the supplied fallback otherwise.
//
// Every URL a resuming handler builds — the consent redirect and the RFC 9207
// `iss` on the authorization response — hangs off this origin rather than off
// the resuming request, because a challenge can be resumed on an origin other
// than the one it was minted under. HandleIDPCallback is mounted at the global
// server URL and carries no customdomains.Context at all, and the upstream
// remote-session login returns the user to the platform origin, so even the
// consent POST — which does carry a custom-domain context — can be serving a
// challenge minted under a different one. A client that recorded
// https://<custom-domain>/mcp/<slug> as the issuer rejects a response carrying
// the platform origin and is forbidden from displaying the error it discarded,
// so a wrong origin here surfaces as nothing at all.
//
// The fallback is per-caller because the right one differs: a handler holding
// the request's custom-domain context falls back to that origin, while
// HandleIDPCallback can only fall back to the server default. It covers states
// carrying no snapshot at all, the one case where the true mint origin is
// unrecoverable.
func (a AuthnChallengeState) mintOriginOr(fallback string) string {
	if a.Endpoint.BaseURL == "" {
		return fallback
	}
	return a.Endpoint.BaseURL
}

// UserSessionGrant is the short-lived OAuth authorization grant minted by
// HandleConsent's POST and consumed by HandleToken's authorization_code
// grant. Stored in Redis under
// `userSessionGrant:{user_session_issuer_id}:{code}` for ~10 minutes.
type UserSessionGrant struct {
	Code string `json:"code"`
	// FlowID carries the OAuth flow correlation identifier from the
	// AuthnChallengeState into the grant so /token can stamp it on its logs,
	// completing end-to-end correlation. Empty for grants minted before this
	// field landed (rolling deploy).
	FlowID              string             `json:"flow_id,omitempty"`
	UserSessionIssuerID uuid.UUID          `json:"user_session_issuer_id"`
	UserSessionClientID uuid.UUID          `json:"user_session_client_id"`
	ClientID            string             `json:"client_id"`
	RedirectURI         string             `json:"redirect_uri"`
	CodeChallenge       string             `json:"code_challenge"`
	CodeChallengeMethod string             `json:"code_challenge_method"`
	Subject             urn.SessionSubject `json:"subject"`
	// DesiredSessionDurationHours is the subject's consent-screen session
	// length choice. Token minting clamps it to the issuer maximum. Zero means
	// "no explicit choice" and the mint uses that maximum. Keep the JSON key
	// stable so grants survive rolling deploys.
	DesiredSessionDurationHours int `json:"session_duration_hours,omitempty"`
	// ToolSelection is the subject's consent-screen tool policy, already
	// validated against the endpoint's live tool inventory and resource-bound.
	// Nil means all tools.
	ToolSelection *toolfilter.SessionSelection `json:"tool_selection,omitempty"`
	CreatedAt     time.Time                    `json:"created_at"`
}

var _ cache.CacheableObject[UserSessionGrant] = (*UserSessionGrant)(nil)

// CacheKey implements cache.CacheableObject.
func (g UserSessionGrant) CacheKey() string {
	return "userSessionGrant:" + g.UserSessionIssuerID.String() + ":" + g.Code
}

// TTL implements cache.CacheableObject. 10 minutes is the standard OAuth code
// lifetime — enough for a slow round trip from the MCP client to /token, short
// enough to limit exposure if the code leaks.
func (g UserSessionGrant) TTL() time.Duration { return 10 * time.Minute }

// errIssuerGateOrgLookup marks the post-validation operational path in
// validateUserSessionToken: the bearer token itself was accepted but the
// endpoint's organization could not be described, so the resulting 401 is
// not a credential rejection.
var errIssuerGateOrgLookup = errors.New("describe organization for issuer-gated endpoint")

// The gram.oauth.failure_reason values the issuer gate emits on its rejection
// logs and on the mcp.request.rejected counter, beyond the bearer-token
// classification issuerGateFailureReason produces. Together they are a closed
// set, so the metric dimension stays bounded.
const (
	// issuerGateReasonNoCredentials: no bearer token was presented at all,
	// which is every client's first unauthenticated handshake probe and every
	// scanner hit on a gated endpoint. Delineated from bad-credential
	// rejections because it is by far the largest 401 population and would
	// otherwise swamp the rejected share.
	issuerGateReasonNoCredentials = "no_credentials"

	// issuerGateReasonInvalidRemoteSession: the bearer token was accepted but
	// a required upstream remote session for the issuer is missing or
	// unusable, so the runtime challenged the client to reconnect.
	issuerGateReasonInvalidRemoteSession = "invalid_remote_session"
)

func issuerGateFailureReason(err error) string {
	switch {
	case errors.Is(err, errIssuerGateOrgLookup):
		return "org_lookup_failed"
	case errors.Is(err, errToolSelectionResourceMismatch):
		return "tool_selection_resource_mismatch"
	case errors.Is(err, errToolSelectionLoad):
		return "tool_selection_load_failed"
	default:
		return "invalid_bearer_token"
	}
}

// userSessionLastUsedCutoff coalesces the last_used_at stamp: a session records
// at most one write per window regardless of request volume. Every other
// request matches no rows and costs one index probe. The window is therefore
// also the resolution of the liveness readout — "used 4m ago" is accurate to
// within this much.
const userSessionLastUsedCutoff = 5 * time.Minute

// touchUserSessionLastUsed records that a validated session just carried a
// request. Best-effort by design: this runs on the per-request MCP auth path,
// where a bookkeeping write must never turn a good credential into a failed
// call, so a failure is logged and swallowed.
func (s *Service) touchUserSessionLastUsed(ctx context.Context, endpoint *ResolvedMcpEndpoint, jti string) {
	if jti == "" {
		return
	}

	now := time.Now()
	err := usersessions_repo.New(s.db).TouchUserSessionLastUsed(ctx, usersessions_repo.TouchUserSessionLastUsedParams{
		NowTs:               pgtype.Timestamptz{Time: now, Valid: true, InfinityModifier: pgtype.Finite},
		ProjectID:           endpoint.ProjectID,
		OrganizationID:      endpoint.OrganizationID,
		UserSessionIssuerID: endpoint.UserSessionIssuerID,
		Jti:                 jti,
		UsedCutoff:          pgtype.Timestamptz{Time: now.Add(-userSessionLastUsedCutoff), Valid: true, InfinityModifier: pgtype.Finite},
	})
	if err != nil {
		s.logger.WarnContext(ctx, "failed to stamp user session last_used_at", attr.SlogError(err))
	}
}

// validateUserSessionToken delegates the JWT verify + revocation check to
// usersessions.Signer.ValidateBearer, then — for user / API-key subjects —
// stamps a contextvalues.AuthContext scoped to the endpoint's org/project.
// A nil subject means "not authenticated as a user session"; the returned
// error carries the reason (bad signature, expired/notBefore, audience
// mismatch, jti revoked, unparseable subject URN) when a token was presented
// and rejected, and is nil when no token was presented at all — so the caller
// can log a real rejection without logging the no-credentials handshake probe.
// One non-rejection error shares this return: a token that validated fine but
// whose org lookup failed wraps errIssuerGateOrgLookup, letting the caller
// label it as an operational failure rather than a bad credential.
//
// Anonymous subjects deliberately leave the AuthContext unset (non-nil
// subject, no AuthContext). The request belongs to no known principal, so
// stamping the endpoint's org as ActiveOrganizationID would misrepresent
// the caller as a member of that org. Downstream code on the public
// path reads org/project off the resolved endpoint directly, the same
// way it does for unauthenticated public-endpoint traffic. The OAuth client
// id is still stamped for them — an anonymous session is anonymous in its
// principal, not in the client that registered for it.
//
// SessionID is populated for non-anonymous subjects so
// authz.Engine.ShouldEnforce / PrepareContext treat the request as a real
// authenticated session. AccountType is retained as session metadata but does
// not control RBAC enforcement.
func (s *Service) validateUserSessionToken(ctx context.Context, token string, endpoint *ResolvedMcpEndpoint) (context.Context, *urn.SessionSubject, *toolfilter.SessionSelection, error) {
	if token == "" {
		return ctx, nil, nil, nil
	}
	session, err := s.userSessionSigner.ValidateBearer(ctx, token, endpoint.AudienceURN, s.chatSessionsManager)
	if err != nil {
		legacySession, ok := s.validateLegacyToolsetAudience(ctx, token, endpoint, err)
		if !ok {
			return ctx, nil, nil, fmt.Errorf("validate user-session bearer: %w", err)
		}
		session = legacySession
	}

	// The consent-screen tool selection loads for every subject kind —
	// including anonymous, which early-returns below before AuthContext is
	// stamped. Load failures fail closed: a policy-store outage must never
	// widen a restrictive session to all tools.
	toolSelection, err := s.loadSessionToolSelection(ctx, endpoint, session.JTI())
	if err != nil {
		return ctx, nil, nil, fmt.Errorf("%w: %w", errToolSelectionLoad, err)
	}
	if toolSelection != nil && !endpointAcceptsToolSelectionResource(endpoint, toolSelection.Resource) {
		// Issuer-scoped tokens are portable across endpoints sharing the
		// issuer; a selection consented on endpoint A must not authorize
		// same-named tools on endpoint B. Reject into reauth.
		return ctx, nil, nil, errToolSelectionResourceMismatch
	}

	subject := session.Subject()
	newCtx, err := s.contextForSessionSubject(ctx, endpoint, subject, session.JTI(), session.ClientID())
	if err != nil {
		return ctx, nil, nil, err
	}
	newCtx = s.identityValidator.StampValidatedSession(newCtx, session)
	return newCtx, &subject, toolSelection, nil
}

// validateLegacyToolsetAudience re-validates a bearer that failed the primary
// audience check against the pre-migration toolset-URN audience (AIS-633;
// counted acceptance, deleted by AIS-646). ok is false when inapplicable or
// the legacy validation fails too — callers surface the original error.
func (s *Service) validateLegacyToolsetAudience(ctx context.Context, token string, endpoint *ResolvedMcpEndpoint, primaryErr error) (sessiontokens.ValidatedSession, bool) {
	legacyAudience, ok := endpoint.legacyToolsetAudienceURN()
	if !ok || !errors.Is(primaryErr, jwt.ErrTokenInvalidAudience) {
		return sessiontokens.ValidatedSession{}, false
	}
	session, err := s.userSessionSigner.ValidateBearer(ctx, token, legacyAudience, s.chatSessionsManager)
	if err != nil {
		return sessiontokens.ValidatedSession{}, false
	}
	s.metrics.RecordLegacyAudienceAccepted(ctx, endpoint.UserSessionIssuerID.String())
	return session, true
}

// contextForSessionSubject stamps the request context for a resolved session
// subject: the OAuth client id when known, and — for non-anonymous subjects —
// the endpoint-org AuthContext that downstream RBAC and telemetry read.
// Anonymous subjects deliberately get no AuthContext: the request belongs to
// no known principal, so stamping the endpoint's org would misrepresent the
// caller as a member.
//
// sessionID feeds AuthContext.SessionID so authz.Engine.ShouldEnforce /
// PrepareContext treat the request as a real authenticated session; the
// issuer gate passes the JWT's JTI, consent-time enumeration passes a
// challenge-derived pseudo id. An org lookup failure wraps
// errIssuerGateOrgLookup so callers can label it operational rather than a
// bad credential.
func (s *Service) contextForSessionSubject(
	ctx context.Context,
	endpoint *ResolvedMcpEndpoint,
	subject urn.SessionSubject,
	sessionID string,
	oauthClientID string,
) (context.Context, error) {
	if oauthClientID != "" {
		ctx = contextvalues.SetOAuthClientID(ctx, oauthClientID)
	}

	// Stamped for every subject kind, anonymous included: liveness describes
	// the connection, and an anonymous session is a real connection whose
	// principal happens to be unknown.
	s.touchUserSessionLastUsed(ctx, endpoint, sessionID)

	if subject.Kind == urn.SessionSubjectKindAnonymous {
		return ctx, nil
	}

	orgMetadata, err := mv.DescribeOrganization(ctx, s.logger, s.orgsRepo, s.billingRepository, endpoint.OrganizationID)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", errIssuerGateOrgLookup, err)
	}
	projectID := endpoint.ProjectID
	authCtx := &contextvalues.AuthContext{
		ActiveOrganizationID:  endpoint.OrganizationID,
		ProjectID:             &projectID,
		UserID:                "",
		ExternalUserID:        "",
		APIKeyID:              "",
		APIKeyName:            "",
		OrgWidePluginHooksKey: false,
		SessionID:             &sessionID,
		OrganizationSlug:      orgMetadata.Slug,
		Email:                 nil,
		AccountType:           orgMetadata.GramAccountType,
		HasActiveSubscription: orgMetadata.HasActiveSubscription,
		Whitelisted:           orgMetadata.Whitelisted,
		ProjectSlug:           nil,
		APIKeyScopes:          nil,
		IsAdmin:               false,
		SupportOrganizationID: "",
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
	return contextvalues.SetAuthContext(ctx, authCtx), nil
}

// AuthenticateChallengeHeader builds the WWW-Authenticate value (RFC 9728
// §5.3): `Bearer resource_metadata="<protectedResourceURL>"`. The remote-MCP
// proxy also uses it to replace upstream challenges on relayed 401/403
// responses.
func AuthenticateChallengeHeader(protectedResourceURL string) string {
	return fmt.Sprintf(`Bearer resource_metadata="%s"`, protectedResourceURL)
}

// WriteAuthenticateChallenge sets the WWW-Authenticate header and returns an
// oops.CodeUnauthorized error. The 401 status and response body come from
// the oops error middleware; the helper owns only the header.
//
// Callers build the URL — the canonical RFC 9728 path is
// `<base>/.well-known/oauth-protected-resource/<routeBase>/<slug>`, which is
// exactly what a spec-compliant client constructs from a resource URL of
// `<base>/<routeBase>/<slug>`.
func WriteAuthenticateChallenge(w http.ResponseWriter, protectedResourceURL, message string) error {
	w.Header().Set("WWW-Authenticate", AuthenticateChallengeHeader(protectedResourceURL))
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

type issuerGateAuthentication struct {
	endpoint             *ResolvedMcpEndpoint
	protectedResourceURL string
	mcpURL               string
	surface              mcpmetrics.Surface
	subject              urn.SessionSubject
}

// authenticateIssuerGate runs the issuer-gated authentication branch shared by
// the toolset-keyed (/mcp) and mcp_server-keyed (/x/mcp) MCP runtime
// paths. It validates the bearer token as a user-session JWT and falls back
// to an assistant-runtime JWT scoped to the endpoint's project. Upstream
// remote-session credentials are deliberately resolved by a separate step so
// hosted tool calls can evaluate kill switches first.
//
// On success it returns the stamped request context, the authenticated subject
// needed for deferred credential resolution, and the caller's tool selection.
// On failure it writes a 401 + WWW-Authenticate and returns the CodeUnauthorized
// error from WriteAuthenticateChallenge. The resource_metadata URL is built
// from baseURL + endpoint.RouteBase +
// endpoint.Slug so a /x/mcp request gets pointed at /x/mcp's
// protected-resource metadata, not /mcp's.
//
// /x/mcp uses this to gate requests on mcp_servers.user_session_issuer_id
// before dispatching to its remote backend or delegating to
// ServeToolsetResolved with the gate skipped.
func (s *Service) authenticateIssuerGate(
	ctx context.Context,
	w http.ResponseWriter,
	authToken, baseURL string,
	endpoint *ResolvedMcpEndpoint,
) (context.Context, *issuerGateAuthentication, *toolfilter.SessionSelection, error) {
	protectedResourceURL, err := endpoint.ProtectedResourceURL(baseURL)
	if err != nil {
		return ctx, nil, nil, oops.E(oops.CodeUnexpected, err, "build protected-resource URL").LogError(ctx, s.logger)
	}

	// The gram.mcp.url value for a rejection, rebuilt from the resolved
	// endpoint rather than taken from the request. The post-authentication
	// metrics use the raw request URL, query string included; here the caller
	// is unauthenticated, and a query string any caller can vary freely would
	// let them mint metric series. The two agree for every query-less request.
	host := ""
	if requestContext, _ := contextvalues.GetRequestContext(ctx); requestContext != nil {
		host = requestContext.Host
	}
	mcpURL := host + "/" + endpoint.RouteBase + "/" + endpoint.Slug
	surface := mcpmetrics.SurfaceHosting
	if endpoint.MetaMcpServerID.Valid {
		surface = mcpmetrics.SurfaceMeta
	}

	newCtx, subject, toolSelection, valErr := s.validateUserSessionToken(ctx, authToken, endpoint)
	if subject == nil {
		// Accept an assistant-runtime JWT, but only when the assistant
		// belongs to the endpoint's project — otherwise a token minted
		// in project A could resolve a remote_session linked under
		// the same user in project B.
		if assistCtx, claims, aerr := s.assistantTokens.Authorize(ctx, authToken); aerr == nil && claims.ProjectID == endpoint.ProjectID.String() {
			ssubj := urn.NewUserSubject(claims.UserID)
			// The subject reads as a user so downstream session plumbing
			// works, but the credential was an assistant-runtime token: its
			// provenance stays KindAssistant and must never be treated as an
			// authoritative acting user.
			newCtx, subject = s.identityValidator.StampAssistant(assistCtx), &ssubj
		}
	}
	if subject == nil {
		// Both the user-session and assistant-runtime paths rejected the
		// token. valErr is nil for the no-credentials handshake probe and
		// never set for a token the assistant path just accepted. It usually
		// carries a credential rejection (audience mismatch / expiry / bad
		// signature / revoked jti), but the errIssuerGateOrgLookup wrap means
		// the token validated and the org lookup failed — an operational
		// error, labeled distinctly so nobody chases a phantom bad token.
		//
		// The no-credentials probe is counted but not logged: it fires on
		// every client's first handshake, so a warning per probe is noise.
		reason := issuerGateReasonNoCredentials
		if valErr != nil {
			reason = issuerGateFailureReason(valErr)
			endpoint.LogWith(s.logger).WarnContext(ctx, "mcp issuer gate rejected bearer token",
				attr.SlogUserSessionIssuerID(endpoint.UserSessionIssuerID.String()),
				attr.SlogToolsetMCPSlug(endpoint.Slug),
				attr.SlogMcpURL(mcpURL),
				attr.SlogOAuthFailureReason(reason),
				attr.SlogError(valErr),
			)
		}
		s.metrics.RecordMCPRequestRejected(ctx, reason, mcpURL, surface)
		return ctx, nil, nil, WriteAuthenticateChallenge(w, protectedResourceURL, "expired or invalid access token")
	}

	return newCtx, &issuerGateAuthentication{
		endpoint:             endpoint,
		protectedResourceURL: protectedResourceURL,
		mcpURL:               mcpURL,
		surface:              surface,
		subject:              *subject,
	}, toolSelection, nil
}

func (s *Service) resolveIssuerGateAccessTokens(ctx context.Context, w http.ResponseWriter, authentication *issuerGateAuthentication) (map[uuid.UUID]remotesessions.UpstreamToken, error) {
	endpoint := authentication.endpoint

	// Meta MCP endpoints resolve partially: their member dispatch routes
	// each credential by its recorded resource, so an unconnected provider
	// degrades that one member while the rest of the session serves. The
	// all-or-nothing ErrNoValidToken challenge below stays for direct
	// endpoints, whose toolset dispatch has no per-upstream routing (AIS-152).
	if endpoint.MetaMcpServerID.Valid {
		tokens, err := s.remoteChallengeMgr.ResolveAvailableAccessTokens(ctx, endpoint.ProjectID, endpoint.OrganizationID, endpoint.UserSessionIssuerID, authentication.subject)
		if err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "resolve remote session").LogError(ctx, s.logger)
		}
		return tokens, nil
	}

	tokens, err := s.remoteChallengeMgr.ResolveAccessTokens(ctx, endpoint.ProjectID, endpoint.OrganizationID, endpoint.UserSessionIssuerID, authentication.subject)
	switch {
	case errors.Is(err, remotesessions.ErrNoValidToken):
		// The Gram user-session token is valid, but a required upstream
		// remote session for this issuer is missing or unusable, so the
		// runtime issues a re-auth challenge pointing the user at
		// {routeBase}/{slug}/connect. This 401 is byte-identical to an
		// invalid-token rejection (both are CodeUnauthorized), so without
		// this line the two are indistinguishable in production. The
		// specific broken upstream (and its refresh reason) is logged by
		// remotesessions.ResolveAccessToken.
		endpoint.LogWith(s.logger).WarnContext(ctx, "mcp issuer gate rejected: upstream remote session missing or unusable",
			attr.SlogUserSessionIssuerID(endpoint.UserSessionIssuerID.String()),
			attr.SlogToolsetMCPSlug(endpoint.Slug),
			attr.SlogMcpURL(authentication.mcpURL),
			attr.SlogOAuthFailureReason(issuerGateReasonInvalidRemoteSession),
		)
		s.metrics.RecordMCPRequestRejected(ctx, issuerGateReasonInvalidRemoteSession, authentication.mcpURL, authentication.surface)
		return nil, WriteAuthenticateChallenge(w, authentication.protectedResourceURL, "")
	case err != nil:
		return nil, oops.E(oops.CodeUnexpected, err, "resolve remote session").LogError(ctx, s.logger)
	default:
		return tokens, nil
	}
}

// ApplyIssuerGate authenticates and immediately resolves upstream credentials.
// Hosted toolset dispatch uses the split operations so kill-switch evaluation
// can run between authentication and protected credential work.
func (s *Service) ApplyIssuerGate(
	ctx context.Context,
	w http.ResponseWriter,
	authToken, baseURL string,
	endpoint *ResolvedMcpEndpoint,
) (context.Context, map[uuid.UUID]remotesessions.UpstreamToken, *toolfilter.SessionSelection, error) {
	newCtx, authentication, toolSelection, err := s.authenticateIssuerGate(ctx, w, authToken, baseURL, endpoint)
	if err != nil {
		return ctx, nil, nil, err
	}
	tokens, err := s.resolveIssuerGateAccessTokens(newCtx, w, authentication)
	if err != nil {
		return ctx, nil, nil, err
	}
	return newCtx, tokens, toolSelection, nil
}

var errToolsetEndpointMismatch = errors.New("authn challenge endpoint does not match toolset")

// RequireUserSessionIssuer verifies the endpoint's user_session_issuer_id
// FK still resolves to a live row, and stamps the issuer configuration the
// OAuth handlers need onto the endpoint. Returns CodeNotFound when the
// issuer was deleted out from under the endpoint, CodeUnexpected on lookup
// failure. Callers are responsible for first checking that the endpoint
// is issuer-gated.
//
// This is where issuer config reaches an OAuth-facing
// ResolvedMcpEndpoint, and it already had to load the row for the FK check,
// so carrying config out of it costs no additional query.
//
// It is NOT run by every construction path: the runtime issuer-gate in
// impl.go builds an endpoint without it. Nothing on that path reads the
// config today, but any future consumer must either route through here or
// tolerate an unstamped endpoint, which reads as an unset mode.
//
// Exported so /x/mcp's [Service.buildResolvedMcpEndpoint] can include
// the live-FK check in the same place as the
// NewResolvedMcpEndpointFromMcpServer construction.
func (s *Service) RequireUserSessionIssuer(ctx context.Context, endpoint *ResolvedMcpEndpoint) error {
	issuer, err := usersessions_repo.New(s.db).GetUserSessionIssuerByID(ctx, usersessions_repo.GetUserSessionIssuerByIDParams{
		ID:             endpoint.UserSessionIssuerID,
		ProjectID:      endpoint.ProjectID,
		OrganizationID: endpoint.OrganizationID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return oops.E(oops.CodeNotFound, err, "user_session_issuer not found")
		}
		return oops.E(oops.CodeUnexpected, err, "load user_session_issuer").LogError(ctx, s.logger)
	}
	// Carried verbatim, NULL included; admission.ResolveMode is the one
	// place that decides what an absent or unrecognized value means.
	endpoint.CIMDAdmissionModeRaw = issuer.ClientIDMetadataAdmissionMode
	return nil
}

func logOAuthClientCredentialEvent(ctx context.Context, logger *slog.Logger, r *http.Request, message, clientID, presentedMethod, grantType, failureReason string) {
	args := []any{
		attr.SlogURLOriginal(r.URL.Path),
		attr.SlogHTTPRequestHeaderUserAgent(r.UserAgent()),
	}
	if clientID != "" {
		args = append(args, attr.SlogOAuthClientID(clientID))
	}
	if presentedMethod != "" {
		args = append(args, attr.SlogOAuthPresentedAuthMethod(presentedMethod))
	}
	if grantType != "" {
		args = append(args, attr.SlogOAuthGrant(grantType))
	}
	if failureReason != "" {
		args = append(args, attr.SlogOAuthFailureReason(failureReason))
	}
	logger.InfoContext(ctx, message, args...)
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
