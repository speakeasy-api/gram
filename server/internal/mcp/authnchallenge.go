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
	"strconv"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/auth/assistanttokens"
	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/mcp/mcpmetrics"
	"github.com/speakeasy-api/gram/server/internal/mcp/toolfilter"
	"github.com/speakeasy-api/gram/server/internal/mcpservers"
	mcpservers_repo "github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
	"github.com/speakeasy-api/gram/server/internal/mv"
	"github.com/speakeasy-api/gram/server/internal/networkingress"
	"github.com/speakeasy-api/gram/server/internal/oautherr"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
	"github.com/speakeasy-api/gram/server/internal/requestorigin"
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

	// Authority pins the provider-neutral request surface, ingress, organization,
	// namespace, and origin used to mint new challenges. Zero value denotes a
	// TTL-bounded state minted before private ingress OAuth existed.
	Authority networkingress.Authority `json:"authority,omitzero"`

	// BaseURL is the externally visible base URL the challenge was minted under,
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

	// IsPublic snapshots whether the endpoint admitted an anonymous subject at
	// mint time. New states always set it; nil preserves compatibility only for
	// states minted before this field existed, which expire within the challenge
	// TTL. Re-entry rejects a visibility change before consent or token minting.
	IsPublic *bool `json:"is_public,omitempty"`

	// ToolsetID pins direct-toolset endpoints. Server/meta IDs remain the primary
	// backend identity for their endpoints. Missing alongside both server IDs
	// denotes a pre-field legacy cached state.
	ToolsetID uuid.NullUUID `json:"toolset_id,omitzero"`

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
	Browser    *ChallengeBrowserBinding `json:"browser,omitempty"`
	Federation *FederatedChallenge      `json:"federation,omitempty"`
	// Preserve only non-secret login binding and retry budget through consent.
	FederatedBinding    *FederatedConsentBinding `json:"federated_binding,omitempty"`
	DelegationRetryUsed bool                     `json:"delegation_retry_used,omitempty"`
	ID                  string                   `json:"id"`
	// FlowID is the stable correlation identifier for the whole OAuth flow,
	// minted once at /authorize. Unlike ID — which idp_callback rotates to
	// rotate the Redis cache key — FlowID is preserved across the rotation
	// and copied into the UserSessionGrant so /token can log it too. Logged
	// as attr.OAuthFlowID on every handler in the flow. Empty for in-flight
	// states minted before this field landed (rolling deploy); callers treat
	// empty as "unknown" and never depend on its presence.
	FlowID              string    `json:"flow_id,omitempty"`
	UserSessionIssuerID uuid.UUID `json:"user_session_issuer_id"`
	// AuthorizerUserID is stamped only from the successful IDP callback. It is
	// kept separate from the eventual credential subject so selecting an agent
	// never makes the agent appear to have consented for itself.
	AuthorizerUserID string `json:"authorizer_user_id,omitempty"`
	// AuthorizerImpersonated preserves WorkOS support-session provenance across
	// the redirect to consent. A nil value marks legacy or unresolved state and
	// fails agent authorization closed; false is serialized after an ordinary
	// IDP callback. Impersonated humans may authorize themselves but must never
	// authorize an agent.
	AuthorizerImpersonated *bool `json:"authorizer_impersonated,omitempty"`
	// AgentAuthorizationTarget fixes the only policy an agent selection may
	// authorize. Older in-flight states omit it and remain self-only.
	AgentAuthorizationTarget *AgentAuthorizationTarget `json:"agent_authorization_target,omitempty"`
	Endpoint                 EndpointRef               `json:"endpoint"`
	ClientID                 string                    `json:"client_id"`
	RedirectURI              string                    `json:"redirect_uri"`
	State                    string                    `json:"state,omitempty"`
	CodeChallenge            string                    `json:"code_challenge"`
	CodeChallengeMethod      string                    `json:"code_challenge_method"`
	CSRFToken                string                    `json:"csrf_token"`
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
// grant. Human grants are stored in Redis under
// `userSessionGrant:{user_session_issuer_id}:{code}`; agent authorization
// grants use `agentUserSessionGrant:{user_session_issuer_id}:{code}` so older
// binaries cannot redeem them. Both expire after ~10 minutes.
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
	// Endpoint pins new authorization codes to the exact endpoint authority
	// consented by the subject. Nil is accepted only for grants minted before
	// this field landed; authorization-code TTL bounds that compatibility window.
	Endpoint *EndpointRef `json:"endpoint,omitempty"`
	// AgentAuthorization is the final, human-approved handoff consumed by the
	// existing session lane to mint an agent-subject session.
	AgentAuthorization *AgentAuthorizationResult `json:"agent_authorization,omitempty"`
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

const agentAuthorizationCodePrefix = "agent-v1."

// CacheKey implements cache.CacheableObject. Agent authorization grants use a
// namespace that older binaries do not read, so a mixed-version deployment
// cannot silently redeem one as a human session.
func (g UserSessionGrant) CacheKey() string {
	return userSessionGrantCacheKey(g.UserSessionIssuerID, g.Code, g.AgentAuthorization != nil)
}

func userSessionGrantCacheKey(issuerID uuid.UUID, code string, agentAuthorization bool) string {
	prefix := "userSessionGrant:"
	if agentAuthorization {
		prefix = "agentUserSessionGrant:"
	}
	return prefix + issuerID.String() + ":" + code
}

// TTL implements cache.CacheableObject. 10 minutes is the standard OAuth code
// lifetime — enough for a slow round trip from the MCP client to /token, short
// enough to limit exposure if the code leaks.
func (g UserSessionGrant) TTL() time.Duration { return 10 * time.Minute }

// errIssuerGateOrgLookup marks the post-validation operational path in
// validateUserSessionToken: the bearer token itself was accepted but the
// endpoint's organization could not be described, so the resulting 401 is
// not a credential rejection.
var (
	errIssuerGateOrgLookup        = errors.New("describe organization for issuer-gated endpoint")
	errAgentSessionCredentialLoad = errors.New("load agent session credential")
)

// errIssuerGateCallerProfile marks an operational lookup failure while preparing
// a caller assertion, after the session subject has already been validated.
var errIssuerGateCallerProfile = errors.New("resolve caller assertion user profile")

// errCredentialRejected marks a rejection the presented credential itself
// earned: a bad signature, the wrong audience, an expired or revoked token, a
// session row that is gone, or a principal whose admission has been withdrawn.
// It is what makes the invalid_token challenge opt-in. A failure on Gram's side
// — an unreachable revocation store, a policy read that never returned, a
// rollout gate that hides the endpoint — leaves it unset, so the client is told
// to retry rather than to throw a live credential away.
var errCredentialRejected = errors.New("credential rejected")

// errWorkloadRolloutDisabled marks a workload session hidden by the agent
// authorization rollout rather than by anything wrong with its token. Exchanging
// a fresh token would hit the same gate, so this must not earn invalid_token.
var errWorkloadRolloutDisabled = errors.New("workload session hidden by agent authorization rollout")

// errWorkloadRolloutUnavailable marks a workload session refused because the
// rollout state could not be read at all. The endpoint is hidden either way,
// but an outage is not a rollout decision and is not reported as one.
var errWorkloadRolloutUnavailable = errors.New("agent authorization rollout state unavailable")

// errUnsupportedSessionSubject marks a session subject kind that parses but
// that this path cannot describe as a caller. It is an error rather than a
// fallback because a context with no actor reads to authz.Engine as an
// authenticated session belonging to nobody.
var errUnsupportedSessionSubject = errors.New("session subject kind cannot be described as a caller")

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

	// issuerGateReasonRevocationUnavailable: the revocation store could not
	// answer, so the request failed closed without judging the credential.
	// Labeled apart from a bad token so an outage does not read as a spike of
	// bad credentials.
	issuerGateReasonRevocationUnavailable = "revocation_check_unavailable"

	// issuerGateReasonWorkloadRolloutDisabled: a workload session reached an
	// endpoint whose organization has agent authorization switched off.
	issuerGateReasonWorkloadRolloutDisabled = "workload_rollout_disabled"

	// issuerGateReasonWorkloadRolloutUnavailable: the rollout state could not
	// be read, so the endpoint stayed hidden without the feature being off.
	// Separate from the line above so an outage cannot be mistaken for
	// deliberate rollout state.
	issuerGateReasonWorkloadRolloutUnavailable = "workload_rollout_unavailable"

	// issuerGateReasonInvalidRemoteSession: the bearer token was accepted but
	// a required upstream remote session for the issuer is missing or
	// unusable, so the runtime challenged the client to reconnect.
	issuerGateReasonInvalidRemoteSession = "invalid_remote_session"

	// issuerGateReasonRemoteSessionUnavailable: the bearer token was accepted
	// but a required upstream token could not be refreshed for a reason that
	// clears on its own, so the runtime answered 503 for the client to retry.
	// Separate from invalid_remote_session so an upstream outage does not read
	// as a wave of users needing to reconnect.
	issuerGateReasonRemoteSessionUnavailable = "remote_session_unavailable"

	// issuerGateReasonRemoteSessionMisconfigured: the bearer token was accepted
	// but a required upstream token could not be refreshed because the issuer
	// or client configuration is broken, which only an administrator repairs.
	issuerGateReasonRemoteSessionMisconfigured = "remote_session_misconfigured"
)

// The texts the issuer gate returns when the bearer token was accepted but a
// required upstream remote session cannot be used. Each doubles as an RFC 6750
// error_description, so it must stay printable ASCII without '"' or '\'.
const (
	// remoteSessionReconnectDescription tells the user the upstream grant is
	// gone and that authorizing the MCP server again reconnects it.
	remoteSessionReconnectDescription = "The upstream connection for this MCP server is missing or expired. Reauthorize the MCP server to reconnect it."

	// remoteSessionMisconfiguredDescription tells the user that reconnecting
	// cannot help and who can.
	remoteSessionMisconfiguredDescription = "The upstream connection for this MCP server is misconfigured. Contact the MCP server administrator."

	// remoteSessionUnavailableMessage tells the client the failure is
	// temporary and the request can be retried unchanged.
	remoteSessionUnavailableMessage = "The upstream authorization server for this MCP server is temporarily unavailable. Retry shortly."
)

// remoteSessionUnavailableRetryAfter is the Retry-After on a 503 for a
// temporarily unavailable upstream token endpoint. Refresh attempts for one
// session are single-flighted, so retrying sooner mostly queues behind the
// same failing attempt; 30 seconds lets a brief upstream outage pass without
// holding a user's request for long.
const remoteSessionUnavailableRetryAfter = 30 * time.Second

func issuerGateFailureReason(err error) string {
	switch {
	case errors.Is(err, errTokenHostMismatch):
		return issuerGateReasonIssuerMismatch
	case errors.Is(err, errIssuerGateOrgLookup):
		return "org_lookup_failed"
	case errors.Is(err, errIssuerGateCallerProfile):
		return "caller_profile_unavailable"
	case errors.Is(err, errToolSelectionResourceMismatch):
		return "tool_selection_resource_mismatch"
	case errors.Is(err, errToolSelectionLoad):
		return "tool_selection_load_failed"
	case errors.Is(err, errAgentSessionCredentialLoad):
		return "agent_session_load_failed"
	case errors.Is(err, errWorkloadSessionCredentialLoad):
		return "workload_session_load_failed"
	case errors.Is(err, errWorkloadSessionAdmissionLoad):
		return "workload_admission_load_failed"
	case errors.Is(err, sessiontokens.ErrRevocationUnavailable):
		return issuerGateReasonRevocationUnavailable
	case errors.Is(err, errWorkloadRolloutDisabled):
		return issuerGateReasonWorkloadRolloutDisabled
	case errors.Is(err, errWorkloadRolloutUnavailable):
		return issuerGateReasonWorkloadRolloutUnavailable
	default:
		return issuerGateReasonInvalidBearerToken
	}
}

// issuerGateReasonInvalidBearerToken: the presented bearer token was judged
// unusable — bad signature, expired, revoked, wrong audience, or its principal
// is no longer admitted.
const issuerGateReasonInvalidBearerToken = "invalid_bearer_token"

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
//
// The bool reports whether the session came from a refreshable grant, which
// is what lets a client recover from invalid_token through its refresh token
// and, failing that, a new authorization.
func (s *Service) validateUserSessionToken(ctx context.Context, token, baseURL string, endpoint *ResolvedMcpEndpoint) (context.Context, *urn.SessionSubject, *toolfilter.SessionSelection, bool, error) {
	if token == "" {
		return ctx, nil, nil, false, nil
	}
	resource, err := endpoint.RootURL(baseURL)
	if err != nil {
		return ctx, nil, nil, false, fmt.Errorf("build user-session resource audience: %w", err)
	}
	legacyAudience, _ := endpoint.legacyToolsetAudienceURN()
	session, acceptedAudience, err := validateUserSessionBearerAudiences(ctx, s.userSessionSigner, s.chatSessionsManager, token, userSessionBearerAudiences{
		Resource: resource,
		Current:  endpoint.AudienceURN,
		Legacy:   legacyAudience,
	})
	if err != nil {
		// A revocation store that could not answer judged nothing; everything
		// else here is the token failing on its own merits.
		if errors.Is(err, sessiontokens.ErrRevocationUnavailable) {
			return ctx, nil, nil, false, fmt.Errorf("validate user-session bearer: %w", err)
		}
		return ctx, nil, nil, false, fmt.Errorf("%w: validate user-session bearer: %w", errCredentialRejected, err)
	}
	// Only the issuer-scoped audiences are shared across hosts; a token on the
	// exact resource audience is already bound to this host.
	if acceptedAudience != userSessionAudienceResource {
		if err := s.checkPerEndpointTokenHost(ctx, session, endpoint, baseURL); err != nil {
			return ctx, nil, nil, false, fmt.Errorf("%w: %w", errCredentialRejected, err)
		}
	}
	if acceptedAudience == userSessionAudienceLegacy {
		s.metrics.RecordLegacyAudienceAccepted(ctx, endpoint.UserSessionIssuerID.String())
	}

	// The consent-screen tool selection loads for every subject kind —
	// including anonymous, which early-returns below before AuthContext is
	// stamped. Load failures fail closed: a policy-store outage must never
	// widen a restrictive session to all tools.
	toolSelection, err := s.loadSessionToolSelection(ctx, endpoint, session.JTI())
	if err != nil {
		return ctx, nil, nil, false, fmt.Errorf("%w: %w", errToolSelectionLoad, err)
	}
	if toolSelection != nil && !endpointAcceptsToolSelectionResource(endpoint, toolSelection.Resource) {
		// Issuer-scoped tokens are portable across endpoints sharing the
		// issuer; a selection consented on endpoint A must not authorize
		// same-named tools on endpoint B. Reject into reauth.
		return ctx, nil, nil, false, errToolSelectionResourceMismatch
	}

	subject := session.Subject()
	newCtx, err := s.contextForSessionSubject(ctx, endpoint, subject, session.JTI(), session.ClientID())
	if err != nil {
		return ctx, nil, nil, false, err
	}
	if subject.Kind == urn.SessionSubjectKindAgent {
		row, qerr := usersessions_repo.New(s.db).GetUserSessionPrincipalCredentialByJTI(ctx, usersessions_repo.GetUserSessionPrincipalCredentialByJTIParams{
			UserSessionIssuerID: endpoint.UserSessionIssuerID,
			Jti:                 session.JTI(),
		})
		if qerr != nil {
			if errors.Is(qerr, pgx.ErrNoRows) {
				return ctx, nil, nil, false, oops.C(oops.CodeUnauthorized)
			}
			return ctx, nil, nil, false, fmt.Errorf("%w: %w", errAgentSessionCredentialLoad, qerr)
		}
		credential, cerr := loadAgentSessionCredential(endpoint, subject, row.SubjectUrn, row.OrganizationID, row.AuthorizerUserID, row.DelegatedGrants, row.DelegatedGrantsVersion)
		if cerr != nil {
			return ctx, nil, nil, false, cerr
		}
		newCtx, err = s.admitAgentSession(newCtx, endpoint, subject, credential)
		if err != nil {
			return ctx, nil, nil, false, err
		}
	}
	if subject.Kind == urn.SessionSubjectKindWorkload {
		row, qerr := usersessions_repo.New(s.db).GetUserSessionPrincipalCredentialByJTI(ctx, usersessions_repo.GetUserSessionPrincipalCredentialByJTIParams{
			UserSessionIssuerID: endpoint.UserSessionIssuerID,
			Jti:                 session.JTI(),
		})
		if qerr != nil {
			// The session row is gone: revoked, or never ours.
			if errors.Is(qerr, pgx.ErrNoRows) {
				return ctx, nil, nil, false, fmt.Errorf("%w: %w", errCredentialRejected, oops.C(oops.CodeUnauthorized))
			}
			return ctx, nil, nil, false, fmt.Errorf("%w: %w", errWorkloadSessionCredentialLoad, qerr)
		}
		credential, cerr := loadWorkloadSessionCredential(endpoint, subject, row.SubjectUrn, row.OrganizationID, row.DelegatedGrants, row.DelegatedGrantsVersion)
		if cerr != nil {
			return ctx, nil, nil, false, fmt.Errorf("%w: %w", errCredentialRejected, cerr)
		}
		newCtx, err = s.admitWorkloadSession(newCtx, endpoint, subject, credential)
		if err != nil {
			return ctx, nil, nil, false, err
		}
	}
	newCtx = s.identityValidator.StampValidatedSession(newCtx, session)
	// Only the token endpoint's issuer-scoped grants carry a refresh token.
	// They are the sessions that validate against the issuer audience; ID-JAG
	// and workload sessions are minted for the exact resource and have none.
	refreshable := acceptedAudience != userSessionAudienceResource
	return newCtx, &subject, toolSelection, refreshable, nil
}

type userSessionBearerAudiences struct {
	Resource string
	Current  string
	Legacy   string
}

type userSessionAcceptedAudience uint8

const (
	userSessionAudienceResource userSessionAcceptedAudience = iota
	userSessionAudienceCurrent
	userSessionAudienceLegacy
)

// validateUserSessionBearerAudiences applies the rollout-safe audience order:
// exact endpoint resource first, current issuer-scoped audience second, then
// the pre-migration toolset audience. It falls through only on an audience
// mismatch; every other validation failure is final.
func validateUserSessionBearerAudiences(
	ctx context.Context,
	signer *sessiontokens.Signer,
	revocation sessiontokens.RevocationChecker,
	token string,
	audiences userSessionBearerAudiences,
) (sessiontokens.ValidatedSession, userSessionAcceptedAudience, error) {
	session, err := signer.ValidateExactAudienceBearer(ctx, token, audiences.Resource, revocation)
	if err == nil {
		return session, userSessionAudienceResource, nil
	}
	if !errors.Is(err, jwt.ErrTokenInvalidAudience) {
		return sessiontokens.ValidatedSession{}, userSessionAudienceResource, fmt.Errorf("validate resource audience: %w", err)
	}

	session, currentErr := signer.ValidateBearer(ctx, token, audiences.Current, revocation)
	if currentErr == nil {
		return session, userSessionAudienceCurrent, nil
	}
	if audiences.Legacy == "" || !errors.Is(currentErr, jwt.ErrTokenInvalidAudience) {
		return sessiontokens.ValidatedSession{}, userSessionAudienceCurrent, fmt.Errorf("validate current audience: %w", currentErr)
	}

	session, legacyErr := signer.ValidateBearer(ctx, token, audiences.Legacy, revocation)
	if legacyErr != nil {
		return sessiontokens.ValidatedSession{}, userSessionAudienceLegacy, fmt.Errorf("validate legacy audience: %w", legacyErr)
	}
	return session, userSessionAudienceLegacy, nil
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

	// Stamped for every persisted subject kind, anonymous included: liveness
	// describes the connection. Authorization-code completion passes no session
	// id because no row exists yet.
	if sessionID != "" {
		s.touchUserSessionLastUsed(ctx, endpoint, sessionID)
	}

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
		SessionID:             nil,
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
	if sessionID != "" {
		authCtx.SessionID = &sessionID
	}
	switch subject.Kind {
	case urn.SessionSubjectKindUser:
		authCtx.UserID = subject.ID
		// Resolve the validated Gram subject through the session manager's
		// cached database profile. Request-authentication profile values may
		// belong to a different user.
		needsProfile, err := s.endpointNeedsCallerProfile(ctx, endpoint)
		if err != nil {
			return nil, err
		}
		if needsProfile {
			profile, _, err := s.sessions.GetUserInfo(ctx, subject.ID)
			if err != nil {
				return nil, fmt.Errorf("%w: %w", errIssuerGateCallerProfile, err)
			}
			if profile == nil || profile.UserID != subject.ID {
				return nil, fmt.Errorf("%w: profile does not match session subject", errIssuerGateCallerProfile)
			}
			authCtx.Email = &profile.Email
		}
		return contextvalues.WithAuthenticatedActor(
			ctx, authCtx, urn.NewPrincipal(urn.PrincipalTypeUser, subject.ID),
		), nil
	case urn.SessionSubjectKindAPIKey:
		authCtx.APIKeyID = subject.ID
		return contextvalues.WithLegacyAPIKeyAuthorization(ctx, authCtx), nil
	case urn.SessionSubjectKindAgent:
		return contextvalues.WithAuthenticatedActor(
			ctx, authCtx, urn.NewPrincipal(urn.PrincipalTypeAgent, subject.ID),
		), nil
	case urn.SessionSubjectKindAnonymous:
		// Unreachable: anonymous subjects return ctx untouched above. Listed
		// for exhaustiveness so the linter doesn't flag the switch.
		return ctx, nil
	case urn.SessionSubjectKindWorkload:
		workloadIssuerID, externalSubject, workloadErr := subject.Workload()
		if workloadErr != nil {
			return nil, fmt.Errorf("%w: %w", errUnsupportedSessionSubject, workloadErr)
		}
		// The actor carries the whole identity, as for an agent. What the
		// machine may do comes from the agent assigned to it, resolved during
		// admission rather than here.
		return contextvalues.WithAuthenticatedActor(
			ctx, authCtx, urn.NewWorkloadPrincipal(workloadIssuerID, externalSubject),
		), nil
	}
	return ctx, oops.C(oops.CodeUnauthorized)
}

func (s *Service) endpointNeedsCallerProfile(ctx context.Context, endpoint *ResolvedMcpEndpoint) (bool, error) {
	// Meta dispatch can select a private tunneled member after authentication.
	if endpoint.MetaMcpServerID.Valid {
		return true, nil
	}
	if endpoint.IsPublic || !endpoint.McpServerID.Valid || endpoint.ToolsetID.Valid {
		return false, nil
	}
	server, err := mcpservers_repo.New(s.db).GetMCPServerByIDAndProjectID(ctx, mcpservers_repo.GetMCPServerByIDAndProjectIDParams{
		ID: endpoint.McpServerID.UUID, ProjectID: endpoint.ProjectID,
	})
	if err != nil {
		return false, fmt.Errorf("%w: resolve destination: %w", errIssuerGateCallerProfile, err)
	}
	return server.TunneledMcpServerID.Valid && server.Visibility == mcpservers.VisibilityPrivate, nil
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
	return writeChallenge(w, AuthenticateChallengeHeader(protectedResourceURL), message)
}

// writeInvalidTokenChallenge is WriteAuthenticateChallenge with the RFC 6750
// §3.1 invalid_token error code, telling the client to drop the token it holds
// and obtain a new one rather than retry with it.
func writeInvalidTokenChallenge(w http.ResponseWriter, protectedResourceURL, message string) error {
	return writeChallenge(w, bearerErrorChallengeHeader(protectedResourceURL, oautherr.CodeInvalidToken, ""), message)
}

// bearerErrorChallengeHeader is AuthenticateChallengeHeader with the RFC 6750
// §3 error and error_description attributes, each omitted when empty. RFC 6750
// limits both to printable ASCII without '"' or '\', so callers pass fixed
// strings rather than anything derived from a request or an upstream.
func bearerErrorChallengeHeader(protectedResourceURL, errorCode, errorDescription string) string {
	header := AuthenticateChallengeHeader(protectedResourceURL)
	if errorCode != "" {
		header += fmt.Sprintf(`, error="%s"`, errorCode)
	}
	if errorDescription != "" {
		header += fmt.Sprintf(`, error_description="%s"`, errorDescription)
	}
	return header
}

func writeChallenge(w http.ResponseWriter, header, message string) error {
	w.Header().Set("WWW-Authenticate", header)
	if message == "" {
		return oops.C(oops.CodeUnauthorized)
	}
	return oops.E(oops.CodeUnauthorized, nil, "%s", message)
}

// BaseURLForRequest returns the externally visible origin stamped by request
// middleware. The configured server URL is retained only for direct/internal
// callers that do not pass through the HTTP middleware.
func (s *Service) BaseURLForRequest(r *http.Request) string {
	return requestorigin.BaseURL(r.Context(), s.serverURL.String())
}

type issuerGateAuthentication struct {
	endpoint             *ResolvedMcpEndpoint
	protectedResourceURL string
	mcpURL               string
	surface              mcpmetrics.Surface
	subject              urn.SessionSubject

	// refreshableUserSession reports that the bearer was a Gram-minted user
	// session from a refreshable grant, not an assistant-runtime token, an
	// agent API key, or a resource-scoped session. Only such a client can
	// act on invalid_token by refreshing and then reauthorizing.
	refreshableUserSession bool
}

// authenticateIssuerGate runs the issuer-gated authentication branch shared by
// the toolset-keyed (/mcp) and mcp_server-keyed (/x/mcp) MCP runtime
// paths. It validates the bearer token as a user-session JWT and falls back
// to an assistant-runtime JWT scoped to the endpoint's project or an admitted
// agent principal API key scoped to the endpoint's tenant. Upstream
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

	newCtx, subject, toolSelection, refreshable, valErr := s.validateUserSessionToken(ctx, authToken, baseURL, endpoint)
	refreshableUserSession := subject != nil && refreshable
	if subject == nil && assistanttokens.IsExecutionToken(authToken) {
		rejectExecution := func(err error) error {
			var failure *oops.ShareableError
			if !errors.As(err, &failure) || (failure.Code != oops.CodeForbidden && failure.Code != oops.CodeUnauthorized) {
				endpoint.LogWith(s.logger).ErrorContext(ctx, "mcp execution admission unavailable", attr.SlogError(err))
				return err
			}
			reason := issuerGateFailureReason(err)
			var denied *oops.ShareableError
			if errors.As(err, &denied) && denied.Code == oops.CodeForbidden {
				reason = "execution_policy_denied"
			}
			s.metrics.RecordMCPRequestRejected(ctx, reason, mcpURL, surface)
			endpoint.LogWith(s.logger).WarnContext(ctx, "mcp issuer gate rejected execution credential", attr.SlogOAuthFailureReason(reason), attr.SlogError(err))
			_ = WriteAuthenticateChallenge(w, protectedResourceURL, "expired or invalid access token")
			return err
		}
		if endpoint.MetaMcpServerID.Valid {
			return ctx, nil, nil, rejectExecution(oops.C(oops.CodeUnauthorized))
		}
		// First-party runtime admission is distinct from an MCP resource JWT.
		// The server-resolved resource, live agent policy and saved ceiling all
		// constrain it. Remote credential resolution receives the real workload
		// subject, never a fabricated user or an assistant-owner fallback.
		executionCtx, err := s.assistantTokens.AuthorizeBusiness(ctx, authToken, endpoint.connectResourceID(), nil)
		if err != nil {
			return ctx, nil, nil, rejectExecution(fmt.Errorf("admit assistant business execution: %w", err))
		}
		ac, ok := contextvalues.GetAuthContext(executionCtx)
		if !ok || ac == nil || ac.ProjectID == nil || *ac.ProjectID != endpoint.ProjectID || ac.ActiveOrganizationID != endpoint.OrganizationID {
			return ctx, nil, nil, rejectExecution(oops.C(oops.CodeUnauthorized))
		}
		actor, ok := contextvalues.AuthenticatedActor(executionCtx)
		if !ok {
			return ctx, nil, nil, rejectExecution(oops.C(oops.CodeUnauthorized))
		}
		workloadIssuer, workloadSubject, err := actor.Workload()
		if err != nil {
			return ctx, nil, nil, rejectExecution(oops.C(oops.CodeUnauthorized))
		}
		selected := urn.NewWorkloadSubject(workloadIssuer, workloadSubject)
		if execution, ok := assistanttokens.BusinessExecution(executionCtx); ok && execution.HumanUserID != "" {
			selected = urn.NewUserSubject(execution.HumanUserID)
		}
		executionCtx = contextvalues.WithAssistantBusinessResource(executionCtx, endpoint.UpstreamResource)
		newCtx, subject = s.identityValidator.StampAssistant(executionCtx), &selected
	}
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
	if subject == nil && strings.HasPrefix(authToken, "gram_") {
		newCtx, subject, valErr = s.authenticateIssuerGateAgentKey(ctx, authToken, endpoint)
		var denied *oops.ShareableError
		if errors.As(valErr, &denied) && denied.Code == oops.CodeNotFound {
			return ctx, nil, nil, valErr
		}

	}
	if subject == nil {
		// All supported credential paths rejected the
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
		const message = "expired or invalid access token"
		if errors.Is(valErr, errCredentialRejected) && s.isWorkloadSessionBearer(authToken) {
			return ctx, nil, nil, writeInvalidTokenChallenge(w, protectedResourceURL, message)
		}
		return ctx, nil, nil, WriteAuthenticateChallenge(w, protectedResourceURL, message)
	}

	return newCtx, &issuerGateAuthentication{
		endpoint:             endpoint,
		protectedResourceURL: protectedResourceURL,
		mcpURL:               mcpURL,
		surface:              surface,
		subject:              *subject,

		refreshableUserSession: refreshableUserSession,
	}, toolSelection, nil
}

// isWorkloadSessionBearer reports whether a rejected bearer was minted by Gram
// for a workload principal. A workload holds no refresh token, so its only way
// back is a fresh grant, and some clients keep replaying a token until the
// challenge names it invalid_token.
func (s *Service) isWorkloadSessionBearer(token string) bool {
	subject, err := s.userSessionSigner.VerifiedSubject(token)
	return err == nil && subject.Kind == urn.SessionSubjectKindWorkload
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

	// The Gram credential is valid in every rejection below; only the upstream
	// remote session behind it is not. The specific broken upstream and the
	// answer its token endpoint gave are logged by remotesessions.
	tokens, err := s.remoteChallengeMgr.ResolveAccessTokens(ctx, endpoint.ProjectID, endpoint.OrganizationID, endpoint.UserSessionIssuerID, authentication.subject)
	if err != nil {
		return nil, s.rejectRemoteSession(ctx, w, authentication, err)
	}
	return tokens, nil
}

// rejectRemoteSession answers a ResolveAccessTokens failure. The Gram
// credential is valid in every rejection; only an upstream remote session
// behind it is not.
func (s *Service) rejectRemoteSession(ctx context.Context, w http.ResponseWriter, authentication *issuerGateAuthentication, err error) error {
	switch {
	case errors.Is(err, remotesessions.ErrRemoteSessionUnavailable):
		// The upstream token endpoint failed in a way that clears on its own.
		// A 401 would send the user through reauthorization over an outage, so
		// the client is told to retry the same request instead.
		s.recordRemoteSessionRejection(ctx, authentication, "mcp issuer gate deferred: upstream remote session temporarily unavailable", issuerGateReasonRemoteSessionUnavailable)
		return remoteSessionUnavailableError(w, err)
	case errors.Is(err, remotesessions.ErrRemoteSessionMisconfigured):
		// Reauthorizing goes through the same broken issuer or client
		// configuration, so the challenge omits invalid_token and names who
		// can repair it instead.
		s.recordRemoteSessionRejection(ctx, authentication, "mcp issuer gate rejected: upstream remote session misconfigured", issuerGateReasonRemoteSessionMisconfigured)
		header := bearerErrorChallengeHeader(authentication.protectedResourceURL, "", remoteSessionMisconfiguredDescription)
		return writeChallenge(w, header, remoteSessionMisconfiguredDescription)
	case errors.Is(err, remotesessions.ErrNoValidToken):
		s.recordRemoteSessionRejection(ctx, authentication, "mcp issuer gate rejected: upstream remote session missing or unusable", issuerGateReasonInvalidRemoteSession)
		return writeRemoteSessionReconnectChallenge(w, authentication)
	default:
		return oops.E(oops.CodeUnexpected, err, "resolve remote session").LogError(ctx, s.logger)
	}
}

// remoteSessionUnavailableError sets Retry-After on w and returns the 503 for a
// required upstream token endpoint that is temporarily unavailable, so every
// path that hits one tells the client the same thing and paces its retries.
func remoteSessionUnavailableError(w http.ResponseWriter, err error) *oops.ShareableError {
	w.Header().Set("Retry-After", strconv.Itoa(int(remoteSessionUnavailableRetryAfter.Seconds())))
	return oops.E(oops.CodeUnavailable, err, "%s", remoteSessionUnavailableMessage)
}

// recordRemoteSessionRejection logs and counts an issuer-gate rejection whose
// bearer token was accepted but whose upstream remote session was not. The
// response itself is byte-identical to other rejections of the same status,
// so this line is what tells them apart in production.
func (s *Service) recordRemoteSessionRejection(ctx context.Context, authentication *issuerGateAuthentication, message, reason string) {
	endpoint := authentication.endpoint
	endpoint.LogWith(s.logger).WarnContext(ctx, message,
		attr.SlogUserSessionIssuerID(endpoint.UserSessionIssuerID.String()),
		attr.SlogToolsetMCPSlug(endpoint.Slug),
		attr.SlogMcpURL(authentication.mcpURL),
		attr.SlogOAuthFailureReason(reason),
	)
	s.metrics.RecordMCPRequestRejected(ctx, reason, authentication.mcpURL, authentication.surface)
}

// writeRemoteSessionReconnectChallenge answers a request whose required
// upstream remote session is missing or its grant is gone, which only the user
// reconnecting it at {routeBase}/{slug}/connect repairs. RFC 6750 reads a 401
// without an error code as "no credentials presented", so clients replay the
// token they hold indefinitely; invalid_token makes them refresh, which the
// token endpoint refuses for the same reason, and then reauthorize through the
// consent page that reconnects the upstream. Only a user session from a
// refreshable grant can follow that path. Every other caller keeps the bare
// challenge, since telling it to discard a credential it cannot replace, or an
// agent session whose attached credential belongs to someone else, would
// strand it.
func writeRemoteSessionReconnectChallenge(w http.ResponseWriter, authentication *issuerGateAuthentication) error {
	errorCode := ""
	if authentication.refreshableUserSession && authentication.subject.Kind == urn.SessionSubjectKindUser {
		errorCode = oautherr.CodeInvalidToken
	}
	header := bearerErrorChallengeHeader(authentication.protectedResourceURL, errorCode, remoteSessionReconnectDescription)
	return writeChallenge(w, header, remoteSessionReconnectDescription)
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
	newCtx, err = assistanttokens.RefreshBusinessExecution(newCtx)
	if err != nil {
		return ctx, nil, nil, fmt.Errorf("refresh assistant execution policy: %w", err)
	}
	return newCtx, tokens, toolSelection, nil
}

var errToolsetEndpointMismatch = errors.New("authn challenge endpoint does not match toolset")

func oauthAuthorityError(err error) *oops.ShareableError {
	if errors.Is(err, networkingress.ErrAuthorityUnavailable) {
		return oops.E(oops.CodeUnavailable, err, "private OAuth authority lookup is unavailable")
	}
	return oops.E(oops.CodeUnauthorized, err, "authn challenge state does not match this MCP server")
}

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
	endpoint.idJAGConfigured = !issuer.ProjectID.Valid && issuer.OrganizationID.Valid && issuer.TrustedRemoteSessionIssuerID.Valid
	endpoint.useAuthenticationHost = issuer.UseAuthenticationHost
	// The authentication host serves only issuers that opt in to it. To any
	// other issuer it is a host that serves nothing.
	if OnAuthenticationHost(ctx) && !issuer.UseAuthenticationHost {
		return oops.E(oops.CodeNotFound, nil, "mcp server not found")
	}
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
