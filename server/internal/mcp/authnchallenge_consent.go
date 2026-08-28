// Consent UI + POST handler for the issuer-gated authn-challenge flow.
// GET renders the consent template; POST persists the user_session_consents
// row, mints a UserSessionGrant, and 302s back to the MCP client.

package mcp

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	_ "embed"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"html/template"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgerrcode"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/mcp/mcpmetrics"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/productfeatures"
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
	"github.com/speakeasy-api/gram/server/internal/urls"
	"github.com/speakeasy-api/gram/server/internal/urn"
	users_repo "github.com/speakeasy-api/gram/server/internal/users/repo"
	"github.com/speakeasy-api/gram/server/internal/usersessions/cimd"
	usersessions_repo "github.com/speakeasy-api/gram/server/internal/usersessions/repo"
)

//go:embed consent_template.html
var consentTemplateHTML string

// consentLogoHTML defines the "speakeasyWordmark" template. Kept in its own
// file because it is generated from the dashboard's GramLogo component and is
// almost entirely path data.
//
//go:embed consent_logo.html
var consentLogoHTML string

var consentTemplate = template.Must(
	template.Must(template.New("consent").Parse(consentTemplateHTML)).Parse(consentLogoHTML),
)

// consentScriptData is the consent page's client-side script. It is served
// as an external file (not inlined into the template) because the ingress
// CSP forbids inline scripts.
//
//go:embed consent_script.js
var consentScriptData []byte

// consentScriptHash is the first 8 hex chars of the SHA-256 of
// consentScriptData, used to cache-bust the immutable script URL. Matches
// the install-page script convention in the mcpmetadata package.
var consentScriptHash = func() string {
	sum := sha256.Sum256(consentScriptData)
	return hex.EncodeToString(sum[:])[:8]
}()

// consentScriptURL is the path the consent template loads the script from.
// Hardcoded to the /mcp surface (like the install-page script) so the
// /x/mcp surface reuses the same route rather than registering its own.
var consentScriptURL = "/mcp/consent-page-" + consentScriptHash + ".js"

// remoteSetHashEmpty is the SHA-256 of an empty remote-set, used by the
// consent record's remote_set_hash column when the issuer has no remote
// session clients (the only case today). The empty case is NOT skipped —
// every consent binds to a specific hash so a later non-empty set
// invalidates prior consents.
var remoteSetHashEmpty = func() string {
	h := sha256.Sum256([]byte("[]"))
	return base64.RawURLEncoding.EncodeToString(h[:])
}()

// consentTemplateData is the field set the consent template renders against.
type consentTemplateData struct {
	ClientName         string
	MCPSlug            string
	MCPRouteBase       string
	State              string
	CSRFToken          string
	SubjectDisplay     string
	RedirectURI        string
	ScriptURL          string
	RemoteSessionCards []remoteSessionCard
	// ConsentEnabled gates the "Give Access" button. True when there are no
	// remote-session challenges, or when at least one challenge has been
	// completed (a card is Connected). Cancel is always available.
	ConsentEnabled bool
	// FirstParty swaps the approve/deny client-grant footer for a terminal
	// completion message: a first-party challenge has no MCP client to grant
	// to, so linking the cards is the whole job.
	FirstParty bool
	// ClientIDOrigin is the host of the client_id URL for CIMD-resolved
	// clients, empty otherwise. Surfaced because a metadata document's
	// client_name (and logo) are attacker-chosen for any accepted document;
	// the origin is the trust anchor a human can actually verify
	// (draft-ietf-oauth-client-id-metadata-document-02 §8.5).
	ClientIDOrigin string
	// LoopbackRedirectWarning is set when a CIMD client will receive the
	// authorization code on a loopback redirect: any process on the user's
	// machine can bind the same port, so the page shows a caution (MCP
	// SHOULD).
	LoopbackRedirectWarning bool
	// AutoClose marks a fully completed first-party connection: every bound
	// remote_session_client is connected. The consent script closes only this
	// terminal state; partially-linked connections (some cards still
	// disconnected) and MCP client consent remain open.
	AutoClose bool
	// SessionDurationOptions is the "Session length" picker rendered as an
	// info row wired to the approve form: presets capped at the issuer's
	// maximum, preselecting the maximum. Empty hides the picker (first-party
	// pages, or a lookup failure — the mint then falls back to the maximum).
	SessionDurationOptions []sessionDurationOption
	// AutoRefreshPolicy controls how the row renders whenever
	// RemoteSessionCards is non-empty: editable for user-controlled refresh,
	// otherwise read-only with the organization's effective value.
	AutoRefreshPolicy autoRefreshPolicy

	// AutoRefreshOn is the row's current value: forced off when the
	// organization disabled refresh, forced on when it requires refresh, and
	// otherwise on only when every card's stored preference is on. Changing an
	// editable value applies to all providers at once.
	AutoRefreshOn bool

	// AutoRefreshHasSessions marks that at least one remote_session row exists
	// (connected or expired), so a change can be persisted immediately rather
	// than only riding the next connect.
	AutoRefreshHasSessions bool
	// ShowToolsIsland renders the "Tool access" React island mount on every
	// non-first-party client-grant page. The island hydrates the picker from
	// ConsentToolsURL and owns enabling the approve button, which the
	// template renders disabled; a missing or failed bundle therefore fails
	// closed.
	ShowToolsIsland bool
	// ConsentToolsURL is the state-authorized inventory action the island
	// POSTs its tools/list request to.
	ConsentToolsURL string
	// ConsentToolsScriptURL is the content-hashed island bundle URL.
	ConsentToolsScriptURL string
	// ConsentToolsPrefill is the subject's stored selection serialized for
	// the island bootstrap; empty when there is no restrictive prefill.
	ConsentToolsPrefill string
	// ConnectedCardCount is the number of RemoteSessionCards already linked,
	// rendered as the "n of m connected" summary above the service list.
	ConnectedCardCount int
	// Styles is the compiled design-system stylesheet inlined into the
	// document head. A build artifact, never user input.
	Styles template.CSS
	// SelectedSessionDuration is the preselected length, surfaced on the
	// summary line so the session's lifetime is visible without opening the
	// configuration disclosure. Empty when there is no picker.
	SelectedSessionDuration string
}

// sessionDurationOption is one <option> of the consent page's session length
// picker.
type sessionDurationOption struct {
	Hours int
	Label string
	// ShortLabel drops the "(maximum)" qualifier so the duration reads as a
	// plain phrase on the summary line ("signing in as … for 2 weeks").
	ShortLabel string
	Selected   bool
}

// remoteSessionCard is the per-remote view rendered by the {{range}} block
// in the consent template. Connect/Disconnect/auto-refresh are POST actions
// against the non-consuming consent action endpoint, so no upstream
// authorize URL is prebuilt here.
//
// Connected and Expired are mutually exclusive and reflect the stored
// remote_session's usability: Connected means the runtime gate will accept
// it; Expired means a stale link exists that must be re-established; both
// false means never connected. Only Connected enables consent — an expired
// link is no better than none until the user reconnects.
type remoteSessionCard struct {
	ClientID   string
	IssuerSlug string

	// IssuerDisplay is the card's identity-provider label: the issuer's
	// operator-set display name when present, otherwise the slug. Issuer
	// branding is Gram-controlled and tenant-set, unlike the
	// attacker-chosen CIMD client_name/logo_uri surfaced via
	// ClientIDOrigin, so the two stay visually separate on the page.
	IssuerDisplay string

	// IssuerLogoURL points at the issuer's logo through the public
	// assets.serveImage endpoint, empty when the issuer has no logo.
	IssuerLogoURL string

	Connected  bool
	Expired    bool
	CanRefresh bool
	// Access expiry describes the current credential. Refresh expiry is kept
	// separate because a renewable one-hour access token is not a connection
	// with "no expiry." Empty values mean the provider omitted that lifetime.
	AccessExpiresAt  string
	AccessExpiresIn  string
	RefreshExpiresAt string
	RefreshExpiresIn string
	// AutoRefreshChecked is the effective auto-refresh value for this card:
	// the stored preference when the organization lets subjects choose,
	// otherwise the organization's own policy value.
	AutoRefreshChecked bool
}

// autoRefreshPolicy is an organization's policy for automatic remote-session
// refresh, resolved from the two product features that back it.
type autoRefreshPolicy int

const (
	// autoRefreshDisabled keeps every connection manual. The consent page
	// shows the state read-only so the subject knows idle connections will
	// lapse, and the keepalive skips the organization even for sessions whose
	// stored preference is on.
	autoRefreshDisabled autoRefreshPolicy = iota

	// autoRefreshUserControlled exposes the opt-in and lets each subject
	// choose per connection. This is the only policy under which a posted
	// form value is trusted.
	autoRefreshUserControlled

	// autoRefreshEnforced pins refresh on for every subject: the consent row
	// is read-only and the keepalive ignores stored preferences.
	autoRefreshEnforced
)

// IsUserControlled reports whether subjects may change auto refresh.
func (p autoRefreshPolicy) IsUserControlled() bool {
	return p == autoRefreshUserControlled
}

// IsEnforced reports whether the organization requires auto refresh.
func (p autoRefreshPolicy) IsEnforced() bool {
	return p == autoRefreshEnforced
}

// consentToolFilteringEnabled reports the organization admin's durable opt-in
// from the consent_tool_filtering product feature managed on MCP Connections.
// An unavailable checker degrades to off.
func (s *Service) consentToolFilteringEnabled(ctx context.Context, _ *slog.Logger, organizationID string) bool {
	if s.platformFeatureChecker == nil {
		return false
	}
	return s.platformFeatureChecker(ctx, organizationID, string(productfeatures.FeatureConsentToolFiltering))
}

// resolveAutoRefreshPolicy reports the organization's automatic-refresh policy.
// Enforcement wins over the opt-in so an organization that turns on both still
// gets the stricter behavior, and an unavailable feature checker degrades to
// disabled rather than silently refreshing connections.
func (s *Service) resolveAutoRefreshPolicy(ctx context.Context, organizationID string) autoRefreshPolicy {
	if s.platformFeatureChecker == nil {
		return autoRefreshDisabled
	}
	if s.platformFeatureChecker(ctx, organizationID, string(productfeatures.FeatureRemoteSessionAutoRefreshEnforced)) {
		return autoRefreshEnforced
	}
	if s.platformFeatureChecker(ctx, organizationID, string(productfeatures.FeatureRemoteSessionAutoRefresh)) {
		return autoRefreshUserControlled
	}
	return autoRefreshDisabled
}

// HandleConsent serves the GET (consent UI) and POST (Give Access /
// Cancel) for the issuer-gated authn-challenge flow. Mounted at
// `GET, POST /mcp/{mcpSlug}/connect`.
//
// On POST + Give Access:
//
//   - Verify the consent CSRF token stored on AuthnChallengeState.
//   - Use the subject that was already resolved into AuthnChallengeState.
//   - Persist a user_session_consents row binding (principal, client,
//     remote_set_hash). Even the empty-remote-set case is bound to a
//     specific hash so consent can't be CSRF'd past on a future issuer
//     change.
//   - Mint a UserSessionGrant in Redis carrying everything HandleToken
//     needs to mint a JWT (sub, client_id, redirect_uri, code_challenge,
//     scope) and 302 the MCP client to its registered redirect_uri with
//     `?code={code}&state={original_state}`.
func (s *Service) HandleConsent(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	mcpSlug := chi.URLParam(r, "mcpSlug")
	if mcpSlug == "" {
		return oops.E(oops.CodeBadRequest, nil, "an mcp slug must be provided").LogError(ctx, s.logger)
	}
	logger := s.logger.With(attr.SlogToolsetMCPSlug(mcpSlug))
	endpoint, err := s.LoadResolvedMcpEndpointBySlug(ctx, logger, mcpSlug, "mcp")
	if err != nil {
		return err
	}
	return s.ServeConsent(w, r, endpoint)
}

// ServeConsentScript serves the consent page's client-side script with
// immutable cache headers. Mounted at `GET /mcp/consent-page-{hash}.js`.
// The hash in the path is content-derived, so a mismatch is a stale URL.
func (s *Service) ServeConsentScript(w http.ResponseWriter, r *http.Request) error {
	if chi.URLParam(r, "hash") != consentScriptHash {
		w.WriteHeader(http.StatusNotFound)
		return nil
	}

	w.Header().Set("Content-Type", "application/javascript; charset=utf-8")
	w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	w.WriteHeader(http.StatusOK)
	if _, err := w.Write(consentScriptData); err != nil {
		return oops.E(oops.CodeUnexpected, err, "write consent script response").LogError(r.Context(), s.logger)
	}
	return nil
}

// ServeConsent is the post-resolution entry point for the consent UI
// (GET) and consent POST handlers, shared by /mcp's HandleConsent
// (toolset-keyed) and /x/mcp's mcp_endpoint-keyed route registration.
func (s *Service) ServeConsent(w http.ResponseWriter, r *http.Request, endpoint *ResolvedMcpEndpoint) error {
	switch r.Method {
	case http.MethodGet:
		return s.serveConsentGet(w, r, endpoint)
	case http.MethodPost:
		return s.serveConsentPost(w, r, endpoint)
	default:
		return oops.E(oops.CodeBadRequest, nil, "method not allowed").LogError(r.Context(), s.logger)
	}
}

func (s *Service) serveConsentGet(w http.ResponseWriter, r *http.Request, endpoint *ResolvedMcpEndpoint) error {
	ctx := r.Context()
	logger := endpoint.LogWith(s.logger)

	stateID := r.URL.Query().Get("state")
	if stateID == "" {
		return oops.E(oops.CodeBadRequest, nil, "state is required").LogError(ctx, logger)
	}

	challengeState, err := s.authnChallengeCache.Get(ctx, "authnChallenge:"+stateID)
	if err != nil {
		return oops.E(oops.CodeUnauthorized, err, "authn challenge state not found or expired").LogError(ctx, logger)
	}
	logger = logger.With(attr.SlogOAuthFlowID(challengeState.FlowID))
	if err := endpoint.ValidateRef(challengeState.Endpoint); err != nil {
		return oops.E(oops.CodeUnauthorized, err, "authn challenge state does not match this MCP server").LogError(ctx, logger)
	}

	// First-party challenges (minted by ServeFirstPartyConnect) have no
	// DCR-registered client; the connect page is the dashboard linking the
	// user's own upstream sessions. Skip the client lookup and label the page
	// generically.
	clientName := "Gram"
	clientIDOrigin := ""
	loopbackRedirectWarning := false
	var clientRowID uuid.UUID
	if !challengeState.FirstParty {
		client, err := s.resolveUserSessionClient(ctx, logger, endpoint, challengeState.ClientID, lookupClientOnly)
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return oops.E(oops.CodeUnauthorized, err, "user session client revoked").LogError(ctx, logger)
			}
			return oops.E(oops.CodeUnexpected, err, "lookup user session client").LogError(ctx, logger)
		}
		clientRowID = client.ID
		clientName = client.ClientName
		if client.ClientIDMetadataUri.Valid {
			if u, err := url.Parse(client.ClientIDMetadataUri.String); err == nil {
				clientIDOrigin = u.Host
			}
			if u, err := url.Parse(challengeState.RedirectURI); err == nil && cimd.IsLoopbackRedirectURI(u) {
				loopbackRedirectWarning = true
			}
		}
	}

	if challengeState.Subject == nil || challengeState.Subject.IsZero() {
		return oops.E(oops.CodeUnauthorized, nil, "authn challenge subject is not resolved").LogError(ctx, logger)
	}

	subjectDisplay := resolveSubjectDisplay(ctx, s.db, *challengeState.Subject)

	autoRefreshPolicy := s.resolveAutoRefreshPolicy(ctx, endpoint.OrganizationID)
	cards, err := s.buildRemoteSessionCards(ctx, endpoint, challengeState, autoRefreshPolicy)
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "build remote session cards").LogError(ctx, logger)
	}

	connectedCardCount := 0
	autoRefreshHasSessions := false
	// Every card already carries the organization's policy applied to its own
	// stored preference, so the page value is on only when none of them is off.
	everyCardAutoRefreshes := true
	for _, c := range cards {
		if c.Connected {
			connectedCardCount++
		}
		if c.Connected || c.Expired {
			autoRefreshHasSessions = true
		}
		everyCardAutoRefreshes = everyCardAutoRefreshes && c.AutoRefreshChecked
	}
	autoRefreshOn := len(cards) > 0 && everyCardAutoRefreshes
	consentEnabled := len(cards) == 0 || connectedCardCount > 0

	// Skip the interstitial when it has nothing to ask. A server fronting a
	// single upstream that the subject has not linked yet leaves the page with
	// exactly one useful control, and the user already expressed intent by
	// starting the authorization — so send them straight to the provider and
	// let them land back here with something to approve. Multi-service pages
	// keep the list: each provider is its own consent decision, and a silent
	// chain of redirects through three login screens is worse than a list that
	// shows what is being asked for.
	if redirected, err := s.maybeAutoConnect(ctx, w, r, logger, endpoint, challengeState, cards); err != nil {
		return err
	} else if redirected {
		return nil
	}

	// First-party pages mint no user session, so there is no length to pick.
	// A lookup failure degrades to no picker rather than a failed render; the
	// mint falls back to the issuer default in that case anyway.
	var durationOptions []sessionDurationOption
	if !challengeState.FirstParty {
		if issuer, ierr := usersessions_repo.New(s.db).GetUserSessionIssuerByID(ctx, usersessions_repo.GetUserSessionIssuerByIDParams{
			ID:        endpoint.UserSessionIssuerID,
			ProjectID: endpoint.ProjectID,
		}); ierr == nil {
			durationOptions = buildSessionDurationOptions(issuer)
		}
	}

	// Only modern endpoints can author per-tool consent: legacy and meta-MCP
	// endpoints remain unrestricted-only, and toolset-fronting servers qualify
	// only when every tool is representable in the island. The island owns
	// approve-button enabling, so unavailable checks must hide it rather than
	// prevent unrestricted approval.
	showToolsIsland := false
	if !challengeState.FirstParty && s.consentToolFilteringEnabled(ctx, logger, endpoint.OrganizationID) {
		var eligibilityErr error
		showToolsIsland, eligibilityErr = s.consentToolPickerEligible(ctx, endpoint)
		if eligibilityErr != nil {
			logger.WarnContext(ctx, "consent tool picker eligibility unavailable", attr.SlogError(eligibilityErr))
		}
	}
	if showToolsIsland {
		lockedDown, lerr := s.customDomainLockdownApplies(ctx, logger, endpoint.ProjectID)
		if lerr != nil {
			return lerr
		}
		// The consent transport enumerates the live upstream inventory and is
		// therefore lockdown-protected like runtime MCP dispatch. On the
		// platform origin, hide the island so the page does not deadlock on a
		// relative transport request that must be rejected; the ordinary
		// unrestricted approval path remains available.
		if lockedDown {
			showToolsIsland = false
		}
	}
	prefillAttr := ""
	if showToolsIsland {
		prefillAttr = consentPrefillAttr(
			s.consentToolSelectionPrefill(ctx, endpoint, *challengeState.Subject, clientRowID),
		)
	}

	data := consentTemplateData{
		ClientName:              clientName,
		MCPSlug:                 endpoint.Slug,
		MCPRouteBase:            endpoint.RouteBase,
		State:                   stateID,
		CSRFToken:               challengeState.CSRFToken,
		SubjectDisplay:          subjectDisplay,
		RedirectURI:             challengeState.RedirectURI,
		ScriptURL:               consentScriptURL,
		RemoteSessionCards:      cards,
		ConsentEnabled:          consentEnabled,
		FirstParty:              challengeState.FirstParty,
		ClientIDOrigin:          clientIDOrigin,
		LoopbackRedirectWarning: loopbackRedirectWarning,
		AutoClose:               shouldAutoCloseFirstParty(challengeState.FirstParty, cards),
		SessionDurationOptions:  durationOptions,
		AutoRefreshPolicy:       autoRefreshPolicy,
		AutoRefreshOn:           autoRefreshOn,
		AutoRefreshHasSessions:  autoRefreshHasSessions,
		ShowToolsIsland:         showToolsIsland,
		ConsentToolsURL:         fmt.Sprintf("/%s/%s/connect/mcp", endpoint.RouteBase, endpoint.Slug),
		ConsentToolsScriptURL:   consentToolsScriptURL,
		ConsentToolsPrefill:     prefillAttr,
		ConnectedCardCount:      connectedCardCount,
		Styles:                  consentPageStyles,
		SelectedSessionDuration: selectedSessionDuration(durationOptions),
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	if err := consentTemplate.Execute(w, data); err != nil {
		return oops.E(oops.CodeUnexpected, err, "render consent template").LogError(ctx, logger)
	}
	return nil
}

func (s *Service) serveConsentPost(w http.ResponseWriter, r *http.Request, endpoint *ResolvedMcpEndpoint) error {
	ctx := r.Context()

	// Cap form body to defend against memory exhaustion (gosec G120). The
	// tool picker can post consentToolNameLimit names of up to
	// consentInventoryMaxNameBytes bytes each, inflated up to 3x by URL
	// encoding; 1 MiB fits that worst case with room for the fixed fields
	// while staying bounded.
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	if err := r.ParseForm(); err != nil {
		return oops.E(oops.CodeBadRequest, err, "failed to parse form").LogError(ctx, s.logger)
	}

	logger := endpoint.LogWith(s.logger)

	stateID := r.PostForm.Get("state")
	if stateID == "" {
		return oops.E(oops.CodeBadRequest, nil, "state is required").LogError(ctx, logger)
	}

	// Preflight on a plain Get: everything that can fail for retryable
	// reasons (validation, inventory snapshot, client lookup, selection
	// parsing) runs BEFORE the challenge is consumed, so a transient failure
	// leaves the page usable. The consuming GetAndDelete below re-validates
	// against the consumed value, which stays the single-use authority.
	challengeState, err := s.authnChallengeCache.Get(ctx, "authnChallenge:"+stateID)
	if err != nil {
		return oops.E(oops.CodeUnauthorized, err, "authn challenge state not found or expired").LogError(ctx, logger)
	}
	logger = logger.With(attr.SlogOAuthFlowID(challengeState.FlowID))
	issuerID := endpoint.UserSessionIssuerID.String()
	mcpSlug := endpoint.Slug

	// The guards below (state-confusion ref check, CSRF, and the unknown-action
	// default) are deliberately NOT counted as flow failures: they are
	// attacker-controllable, so emitting `failed` here would let crafted
	// requests pollute a config's health signal. A legitimate user never
	// trips them; the rare case lands in the started-without-terminal gap.
	if err := validateConsentChallenge(endpoint, &challengeState, r.PostForm.Get("csrf_token")); err != nil {
		return err.LogError(ctx, logger)
	}

	// Explicit action required: fail closed on missing / unknown values so
	// a malformed form post can't trigger the approval path. Checked before
	// any flow-outcome metric is recorded, so a crafted action stays in the
	// attacker-controllable bucket the guards above describe rather than
	// counting against a config's health signal.
	action := r.PostForm.Get("action")
	if action != "approve" && action != "deny" {
		return oops.E(oops.CodeBadRequest, nil, `action must be "approve" or "deny"`).LogError(ctx, logger)
	}

	// The RFC 9207 `iss` both branches below emit, resolved once so the deny
	// and success responses cannot disagree. It hangs off the origin the
	// challenge was minted under, not this request's: the remote-session
	// return leg re-enters consent on the platform origin, so a POST carrying
	// a custom-domain context can still be completing a flow the client
	// recorded under a different origin (or vice versa).
	issuer, err := endpoint.RootURL(challengeState.mintOriginOr(s.BaseURLForRequest(r)))
	if err != nil {
		s.metrics.RecordOAuthFlowFailed(ctx, issuerID, mcpSlug, mcpmetrics.OAuthFlowStageConsent)
		return oops.E(oops.CodeUnexpected, err, "build authorization response issuer").LogError(ctx, logger)
	}

	if action == "deny" {
		// Cancel: consume the challenge, then 303 (POST → GET) the MCP client
		// back to its redirect_uri with access_denied per RFC 6749 §4.1.2.1,
		// preserving the original state. The user reached the consent screen
		// and chose "no" — a decline, not an errant config. A lost consume
		// race (double submit) reads as expired state.
		if _, err := s.authnChallengeCache.GetAndDelete(ctx, "authnChallenge:"+stateID); err != nil {
			return oops.E(oops.CodeUnauthorized, err, "authn challenge state not found or expired").LogError(ctx, logger)
		}
		s.evictConsentToolInventory(ctx, stateID)
		denyURL, err := buildClientRedirect(clientRedirectParams{
			RedirectURI:      challengeState.RedirectURI,
			Issuer:           issuer,
			Code:             "",
			State:            challengeState.State,
			ErrorCode:        "access_denied",
			ErrorDescription: "user denied consent",
		})
		if err != nil {
			// Recorded as failed, not declined: the user's decline never
			// reached the client, so this flow ended on a fault. Exactly one
			// terminal outcome is counted per started flow either way.
			s.metrics.RecordOAuthFlowFailed(ctx, issuerID, mcpSlug, mcpmetrics.OAuthFlowStageConsent)
			return oops.E(oops.CodeUnexpected, err, "build client redirect").LogError(ctx, logger)
		}
		s.metrics.RecordOAuthFlowDeclined(ctx, issuerID, mcpSlug, mcpmetrics.OAuthFlowStageConsent)
		logger.InfoContext(ctx, "oauth flow declined at consent", attr.SlogOAuthError("access_denied"))
		http.Redirect(w, r, denyURL, http.StatusSeeOther)
		return nil
	}

	if challengeState.Subject == nil || challengeState.Subject.IsZero() {
		// Reaching an approved consent POST with no resolved subject is a code
		// invariant break, not a user action — a config/code-class failure.
		s.metrics.RecordOAuthFlowFailed(ctx, issuerID, mcpSlug, mcpmetrics.OAuthFlowStageConsent)
		return oops.E(oops.CodeUnauthorized, nil, "authn challenge subject is not resolved").LogError(ctx, logger)
	}

	// A restrictive approve binds to the exact inventory snapshot the island
	// displayed: the island submits its attempt id only after fetching every
	// page, and only a COMPLETE snapshot satisfies the lookup. A missing,
	// incomplete, or expired snapshot is retryable — reload the page — and
	// must not consume the challenge; a store outage is an operational 503.
	// Approvals without tool_filtering=on (pages rendered before the picker
	// deployed or with the product feature off) skip the binding: they mint the
	// unrestricted grant the pre-picker flow always minted, so stripping the
	// field can only widen a submission to the status quo, never past it.
	var boundInventory *consentToolInventory
	if r.PostForm.Get("tool_filtering") == "on" {
		eligible, eerr := s.consentToolPickerEligible(ctx, endpoint)
		if eerr != nil {
			return oops.E(oops.CodeUnavailable, eerr, "service temporarily unavailable").LogError(ctx, logger)
		}
		if !eligible {
			return oops.E(oops.CodeConflict, nil, "tool filtering is not available for this endpoint").LogWarn(ctx, logger)
		}

		attempt, aerr := consentAttemptID(r.PostForm.Get("tool_inventory_id"))
		if aerr != nil {
			return oops.E(oops.CodeConflict, aerr, "tool inventory is no longer available; reload the page and try again").LogWarn(ctx, logger)
		}
		inventory, found, gerr := s.getCompletedConsentInventory(ctx, stateID, attempt)
		if gerr != nil {
			return oops.E(oops.CodeUnavailable, gerr, "service temporarily unavailable").LogError(ctx, logger)
		}
		if !found {
			return oops.E(oops.CodeConflict, nil, "tool inventory is no longer available; reload the page and try again").LogWarn(ctx, logger)
		}
		boundInventory = &inventory
	}

	toolSelection, err := chosenToolSelection(r.PostForm, boundInventory)
	if err != nil {
		return oops.E(oops.CodeBadRequest, err, "invalid tool selection").LogError(ctx, logger)
	}

	// Resolve the user_session_clients row id for the consent FK.
	clientRow, err := s.resolveUserSessionClient(ctx, logger, endpoint, challengeState.ClientID, lookupClientOnly)
	if err != nil {
		// Client revoked mid-flow (config change) or DB error — either way the
		// approved flow can't complete.
		s.metrics.RecordOAuthFlowFailed(ctx, issuerID, mcpSlug, mcpmetrics.OAuthFlowStageConsent)
		if errors.Is(err, pgx.ErrNoRows) {
			return oops.E(oops.CodeUnauthorized, err, "user session client revoked").LogError(ctx, logger)
		}
		return oops.E(oops.CodeUnexpected, err, "lookup user session client").LogError(ctx, logger)
	}

	// Atomic GETDEL: a consent approval consumes the authn-challenge state
	// single-use. Parallel POSTs (e.g. user double-submits) lose the race
	// and get "not found or expired", so only one grant is ever minted per
	// authorization request. The consumed value is the authority — re-run
	// the guards against it in case the preflighted copy went stale.
	challengeState, err = s.authnChallengeCache.GetAndDelete(ctx, "authnChallenge:"+stateID)
	if err != nil {
		return oops.E(oops.CodeUnauthorized, err, "authn challenge state not found or expired").LogError(ctx, logger)
	}
	if err := validateConsentChallenge(endpoint, &challengeState, r.PostForm.Get("csrf_token")); err != nil {
		return err.LogError(ctx, logger)
	}
	subject := *challengeState.Subject

	// Persist the consent record. The unique index on
	// (principal_urn, user_session_client_id, remote_set_hash) makes this
	// idempotent on re-consent for the same set; we treat the duplicate-key
	// error as a no-op (consent already on file).
	if _, err := usersessions_repo.New(s.db).CreateUserSessionConsent(ctx, usersessions_repo.CreateUserSessionConsentParams{
		SubjectUrn:          subject,
		UserSessionClientID: clientRow.ID,
		RemoteSetHash:       remoteSetHashEmpty,
	}); err != nil && !isUniqueViolation(err) {
		s.metrics.RecordOAuthFlowFailed(ctx, issuerID, mcpSlug, mcpmetrics.OAuthFlowStageConsent)
		return oops.E(oops.CodeUnexpected, err, "record consent").LogError(ctx, logger)
	}

	code, err := generateOpaqueToken()
	if err != nil {
		s.metrics.RecordOAuthFlowFailed(ctx, issuerID, mcpSlug, mcpmetrics.OAuthFlowStageConsent)
		return oops.E(oops.CodeUnexpected, err, "generate authorization code").LogError(ctx, logger)
	}

	grant := UserSessionGrant{
		Code:                        code,
		FlowID:                      challengeState.FlowID,
		UserSessionIssuerID:         endpoint.UserSessionIssuerID,
		UserSessionClientID:         clientRow.ID,
		ClientID:                    challengeState.ClientID,
		RedirectURI:                 challengeState.RedirectURI,
		CodeChallenge:               challengeState.CodeChallenge,
		CodeChallengeMethod:         challengeState.CodeChallengeMethod,
		Subject:                     subject,
		DesiredSessionDurationHours: desiredSessionDurationHours(r.PostForm.Get("session_duration_hours")),
		ToolSelection:               toolSelection,
		CreatedAt:                   time.Now(),
	}
	if err := s.userSessionGrantCache.Store(ctx, grant); err != nil {
		s.metrics.RecordOAuthFlowFailed(ctx, issuerID, mcpSlug, mcpmetrics.OAuthFlowStageConsent)
		return oops.E(oops.CodeUnexpected, err, "store user session grant").LogError(ctx, logger)
	}

	s.evictConsentToolInventory(ctx, stateID)

	clientRedirect, err := buildClientRedirect(clientRedirectParams{
		RedirectURI:      challengeState.RedirectURI,
		Issuer:           issuer,
		Code:             code,
		State:            challengeState.State,
		ErrorCode:        "",
		ErrorDescription: "",
	})
	if err != nil {
		s.metrics.RecordOAuthFlowFailed(ctx, issuerID, mcpSlug, mcpmetrics.OAuthFlowStageConsent)
		return oops.E(oops.CodeUnexpected, err, "build client redirect").LogError(ctx, logger)
	}
	// 303 See Other (POST → GET): the consent submit is a POST; we want
	// the user agent to GET the redirect target with NO body re-submission.
	http.Redirect(w, r, clientRedirect, http.StatusSeeOther)
	return nil
}

// validateConsentChallenge runs the consent POST's state guards: endpoint
// ref, CSRF (constant time), the first-party rejection, and subject
// resolution. Shared by the preflight Get and the post-consume revalidation
// so both read the same rules.
func validateConsentChallenge(endpoint *ResolvedMcpEndpoint, challengeState *AuthnChallengeState, csrfToken string) *oops.ShareableError {
	if err := endpoint.ValidateRef(challengeState.Endpoint); err != nil {
		return oops.E(oops.CodeUnauthorized, err, "authn challenge state does not match this MCP server")
	}
	if challengeState.CSRFToken == "" || subtle.ConstantTimeCompare([]byte(csrfToken), []byte(challengeState.CSRFToken)) != 1 {
		return oops.E(oops.CodeUnauthorized, nil, "invalid consent csrf token")
	}
	// First-party challenges have no MCP client to grant to: linking the
	// cards is terminal, so there is no approve/deny POST. The template
	// omits the form; reject any crafted submission rather than falling into
	// the client-grant path with an empty ClientID.
	if challengeState.FirstParty {
		return oops.E(oops.CodeBadRequest, nil, "first-party connect challenges have no approval step")
	}
	if challengeState.Subject == nil || challengeState.Subject.IsZero() {
		return oops.E(oops.CodeUnauthorized, nil, "authn challenge subject is not resolved")
	}
	return nil
}

// resolveSubjectDisplay picks the friendliest label for the consent page's
// "Signing in as" row. User-kind subjects look up the gram user and prefer
// email then display_name; any miss (anonymous subject, deleted user, lookup
// error) falls back to the URN string so the UI still renders.
func resolveSubjectDisplay(ctx context.Context, db users_repo.DBTX, subject urn.SessionSubject) string {
	fallback := subject.String()
	if subject.Kind != urn.SessionSubjectKindUser {
		return fallback
	}
	user, err := users_repo.New(db).GetUser(ctx, subject.ID)
	if err != nil {
		return fallback
	}
	if user.Email != "" {
		return user.Email
	}
	if user.DisplayName != "" {
		return user.DisplayName
	}
	return fallback
}

// clientRedirectParams is the field set of one client-facing authorization
// response.
type clientRedirectParams struct {
	// Code is the authorization code on a success response, empty on an error.
	Code string

	// ErrorCode carries an RFC 6749 §4.1.2.1 error code. Empty on a success
	// response.
	ErrorCode string

	// ErrorDescription carries an RFC 6749 §4.1.2.1 error description. Empty on
	// a success response.
	ErrorDescription string

	// Issuer is the RFC 9207 `iss` parameter — the endpoint's root URL, byte
	// identical to the `issuer` advertised by the AS metadata document. Required
	// on every authorization response, success and error alike (RFC 9207 §2).
	// Clients compare it without any normalization (no case folding, default-port
	// elision, trailing-slash or percent-encoding fixups), so it must be derived
	// the same way ServeGetAuthorizationServer derives the advertised value.
	Issuer string

	// RedirectURI is the client's redirect_uri. Callers must only reach this
	// helper with a URI already validated against the registered set on the
	// client row; passing an untrusted URI turns the AS into an open redirector.
	RedirectURI string

	// State echoes the client's original `state` when it sent one.
	State string
}

// responseOwnedParams are the query parameters an authorization response
// defines. A registered redirect_uri may carry a query string of the client's
// own, which is preserved per RFC 6749 §3.1.2, but any of these it contains is
// cleared before the response is written: a client that reads `code` before
// `error` would otherwise see a redirect_uri-supplied `code=…` on a decline as
// a grant, and a redirect_uri-supplied `iss` could be chosen to pass the RFC
// 9207 §2.4 comparison the response is meant to fail. `state` is deliberately
// exempt: it is client-owned round-trip data with no spoofing value, and a
// registered redirect_uri that embeds one relies on receiving it back on every
// response. When the client sent a request `state`, the response value
// overwrites any embedded one below.
var responseOwnedParams = []string{"iss", "code", "error", "error_description"}

// buildClientRedirect produces the URL to redirect the MCP client to,
// preserving any prior query string on RedirectURI and adding `iss` plus
// `code` (success) or `error` / `error_description` (failure) and the
// original `state`.
func buildClientRedirect(p clientRedirectParams) (string, error) {
	// The issuer has to be the absolute URL the metadata document advertises.
	// A relative or empty value is one a client validating per RFC 9207 §2.4
	// discards without surfacing anything to the user, so it fails here where
	// it is still visible. url.JoinPath returns no error for an empty base, so
	// a missing origin arrives as a relative path rather than as a failure.
	if !urls.IsAbsoluteHTTP(p.Issuer) {
		return "", fmt.Errorf("authorization response issuer is not an absolute http(s) url: %q", p.Issuer)
	}
	u, err := url.Parse(p.RedirectURI)
	if err != nil {
		// Should never happen — redirect_uri was validated at HandleAuthorize
		// time. An unparseable URI has nowhere to carry the response
		// parameters, so this is terminal for the flow.
		return "", fmt.Errorf("parse client redirect_uri: %w", err)
	}
	q := u.Query()
	for _, param := range responseOwnedParams {
		q.Del(param)
	}
	q.Set("iss", p.Issuer)
	if p.Code != "" {
		q.Set("code", p.Code)
	}
	if p.ErrorCode != "" {
		q.Set("error", p.ErrorCode)
		if p.ErrorDescription != "" {
			q.Set("error_description", p.ErrorDescription)
		}
	}
	if p.State != "" {
		q.Set("state", p.State)
	}
	u.RawQuery = q.Encode()
	return u.String(), nil
}

// isUniqueViolation reports whether err is a Postgres unique-constraint
// violation. Used to detect duplicate consent inserts (idempotent re-consent).
func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == pgerrcode.UniqueViolation
}

// shouldAutoCloseFirstParty reports whether a first-party connect tab is fully
// terminal and safe to auto-close: every bound remote_session_client is
// connected. The runtime gate (remotesessions.ResolveAccessTokens) fails the
// request unless all bound clients have a usable token, so closing after only
// the first of several providers is linked would strand the user mid-flow. A
// challenge with no cards is never auto-closed — there is nothing to complete.
func shouldAutoCloseFirstParty(firstParty bool, cards []remoteSessionCard) bool {
	if !firstParty || len(cards) == 0 {
		return false
	}
	for _, c := range cards {
		if !c.Connected {
			return false
		}
	}
	return true
}

// consentDurationPresets are the session-length choices offered on the
// consent page, largest first. The issuer's maximum is inserted when not
// already present, and anything above the maximum is dropped.
var consentDurationPresets = []sessionDurationOption{
	{Hours: 90 * 24, Label: "90 days", Selected: false},
	{Hours: 60 * 24, Label: "60 days", Selected: false},
	{Hours: 30 * 24, Label: "30 days", Selected: false},
	{Hours: 14 * 24, Label: "2 weeks", Selected: false},
	{Hours: 7 * 24, Label: "1 week", Selected: false},
	{Hours: 3 * 24, Label: "3 days", Selected: false},
	{Hours: 24, Label: "1 day", Selected: false},
	{Hours: 12, Label: "12 hours", Selected: false},
	{Hours: 1, Label: "1 hour", Selected: false},
}

// formatDurationHours renders a whole-hour count the way the presets do.
func formatDurationHours(hours int) string {
	switch {
	case hours%(7*24) == 0:
		if hours == 7*24 {
			return "1 week"
		}
		return fmt.Sprintf("%d weeks", hours/(7*24))
	case hours%24 == 0:
		if hours == 24 {
			return "1 day"
		}
		return fmt.Sprintf("%d days", hours/24)
	case hours == 1:
		return "1 hour"
	default:
		return fmt.Sprintf("%d hours", hours)
	}
}

// buildSessionDurationOptions produces the session-length <select> options:
// presets at or below the issuer's maximum, with the maximum itself
// guaranteed present and preselected.
func buildSessionDurationOptions(issuer usersessions_repo.UserSessionIssuer) []sessionDurationOption {
	if !issuer.SessionDuration.Valid || issuer.SessionDuration.Microseconds <= 0 {
		return nil
	}
	maxHours := int(time.Duration(issuer.SessionDuration.Microseconds) * time.Microsecond / time.Hour)
	if maxHours < 1 {
		return nil
	}

	options := make([]sessionDurationOption, 0, len(consentDurationPresets)+1)
	seen := map[int]bool{}
	add := func(hours int, label string) {
		if hours < 1 || hours > maxHours || seen[hours] {
			return
		}
		seen[hours] = true
		options = append(options, sessionDurationOption{
			Hours:      hours,
			Label:      label,
			ShortLabel: formatDurationHours(hours),
			Selected:   hours == maxHours,
		})
	}
	add(maxHours, formatDurationHours(maxHours)+" (maximum)")
	for _, preset := range consentDurationPresets {
		add(preset.Hours, preset.Label)
	}
	return options
}

// desiredSessionDurationHours parses the approve form's session length.
// Token minting applies the issuer's authoritative maximum.
func desiredSessionDurationHours(raw string) int {
	hours, err := strconv.Atoi(raw)
	if err != nil || hours < 1 {
		return 0
	}
	return hours
}

// issuerCardBranding resolves the branding a consent card renders for its
// identity provider. The display fallback matches
// formatRemoteSessionIssuerDisplay in the dashboard: a trimmed non-empty
// name wins, otherwise the identifier the page always rendered (the slug).
// The logo URL points at the public assets.serveImage endpoint on the
// platform origin, the same construction mcpmetadata uses for MCP server
// logos, and is empty when the issuer has no logo.
func issuerCardBranding(c remotesessions.Client, serverURL *url.URL) (display, logoURL string) {
	display = c.IssuerSlug
	if name := strings.TrimSpace(conv.PtrValOr(c.IssuerName, "")); name != "" {
		display = name
	}
	if c.IssuerLogoAssetID.Valid {
		u := *serverURL
		u.Path = "/rpc/assets.serveImage"
		q := u.Query()
		q.Set("id", c.IssuerLogoAssetID.UUID.String())
		u.RawQuery = q.Encode()
		logoURL = u.String()
	}
	return display, logoURL
}

// buildRemoteSessionCards loads every remote_session_client linked to the
// endpoint's user_session_issuer and materialises a card per client. Each
// card carries a connected/disconnected state (read from remote_sessions
// for the stamped subject) plus the upstream authorize URL minted by the
// ChallengeManager. Mints fresh per-card Redis state on every render —
// the 10-min TTL keeps abandoned states from piling up.
func (s *Service) buildRemoteSessionCards(
	ctx context.Context,
	endpoint *ResolvedMcpEndpoint,
	challengeState AuthnChallengeState,
	policy autoRefreshPolicy,
) ([]remoteSessionCard, error) {
	clients, err := s.remoteChallengeMgr.ListClients(ctx, endpoint.ProjectID, endpoint.OrganizationID, endpoint.UserSessionIssuerID)
	if err != nil {
		return nil, fmt.Errorf("list remote session clients: %w", err)
	}
	if len(clients) == 0 {
		return nil, nil
	}

	// Single round-trip for connection state across all cards. Empty when
	// the subject hasn't been stamped yet (early render before IDP /
	// anonymous late-bind); the per-card check below then resolves to
	// not-connected.
	var statuses map[uuid.UUID]remotesessions.RemoteSessionState
	if challengeState.Subject != nil && !challengeState.Subject.IsZero() {
		statuses, err = s.remoteChallengeMgr.RemoteSessionStatuses(ctx, *challengeState.Subject, endpoint.ProjectID, endpoint.OrganizationID, endpoint.UserSessionIssuerID)
		if err != nil {
			return nil, fmt.Errorf("remote session statuses: %w", err)
		}
	}

	cards := make([]remoteSessionCard, 0, len(clients))
	renderedAt := time.Now()
	for _, c := range clients {
		state, hasSession := statuses[c.ID]
		var checked bool
		switch policy {
		case autoRefreshEnforced:
			checked = true
		case autoRefreshUserControlled:
			// A new connection defaults on; an existing one keeps the choice
			// the subject already made.
			checked = true
			if hasSession {
				checked = state.AutoRefresh
			}
		case autoRefreshDisabled:
			checked = false
		}
		accessExpiresAt := ""
		accessExpiresIn := ""
		if state.AccessExpiresAt != nil {
			accessExpiresAt = state.AccessExpiresAt.UTC().Format(time.RFC3339)
			accessExpiresIn = formatTimeRemaining(renderedAt, *state.AccessExpiresAt)
		}
		refreshExpiresAt := ""
		refreshExpiresIn := ""
		if state.RefreshExpiresAt != nil {
			refreshExpiresAt = state.RefreshExpiresAt.UTC().Format(time.RFC3339)
			refreshExpiresIn = formatTimeRemaining(renderedAt, *state.RefreshExpiresAt)
		}
		issuerDisplay, issuerLogoURL := issuerCardBranding(c, s.serverURL)
		cards = append(cards, remoteSessionCard{
			ClientID:           c.ID.String(),
			IssuerSlug:         c.IssuerSlug,
			IssuerDisplay:      issuerDisplay,
			IssuerLogoURL:      issuerLogoURL,
			Connected:          state.Status == remotesessions.RemoteSessionActive,
			Expired:            state.Status == remotesessions.RemoteSessionExpired,
			CanRefresh:         state.CanRefresh,
			AccessExpiresAt:    accessExpiresAt,
			AccessExpiresIn:    accessExpiresIn,
			RefreshExpiresAt:   refreshExpiresAt,
			RefreshExpiresIn:   refreshExpiresIn,
			AutoRefreshChecked: checked,
		})
	}
	return cards, nil
}

func formatTimeRemaining(now, expiresAt time.Time) string {
	remaining := expiresAt.Sub(now)
	if remaining <= 0 {
		return "Expired"
	}

	totalMinutes := int((remaining + time.Minute - 1) / time.Minute)
	days := totalMinutes / (24 * 60)
	hours := totalMinutes % (24 * 60) / 60
	minutes := totalMinutes % 60

	switch {
	case days > 0 && hours > 0:
		return fmt.Sprintf("%d %s %d %s", days, pluralize(days, "day"), hours, pluralize(hours, "hour"))
	case days > 0:
		return fmt.Sprintf("%d %s", days, pluralize(days, "day"))
	case hours > 0 && minutes > 0:
		return fmt.Sprintf("%d %s %d %s", hours, pluralize(hours, "hour"), minutes, pluralize(minutes, "minute"))
	case hours > 0:
		return fmt.Sprintf("%d %s", hours, pluralize(hours, "hour"))
	default:
		return fmt.Sprintf("%d %s", minutes, pluralize(minutes, "minute"))
	}
}

func pluralize(value int, singular string) string {
	if value == 1 {
		return singular
	}
	return singular + "s"
}

// maybeAutoConnect sends the subject straight to the sole unlinked upstream
// provider, reporting whether it wrote a redirect. It is a no-op unless there
// is exactly one remote-session card, that card is unlinked, and this
// challenge has not auto-connected before.
//
// The latch is persisted BEFORE redirecting and is never cleared, so every
// path back to this page — the user denying consent upstream, the provider
// erroring, an explicit disconnect — renders the page with its manual
// controls instead of bouncing the user out again.
func (s *Service) maybeAutoConnect(
	ctx context.Context,
	w http.ResponseWriter,
	r *http.Request,
	logger *slog.Logger,
	endpoint *ResolvedMcpEndpoint,
	challengeState AuthnChallengeState,
	cards []remoteSessionCard,
) (bool, error) {
	if challengeState.AutoConnectDone || len(cards) != 1 || cards[0].Connected {
		return false, nil
	}

	clients, err := s.remoteChallengeMgr.ListClients(ctx, endpoint.ProjectID, endpoint.OrganizationID, endpoint.UserSessionIssuerID)
	if err != nil {
		return false, oops.E(oops.CodeUnexpected, err, "list remote session clients").LogError(ctx, logger)
	}
	var client *remotesessions.Client
	for i := range clients {
		if clients[i].ID.String() == cards[0].ClientID {
			client = &clients[i]
			break
		}
	}
	if client == nil {
		return false, nil
	}

	// Claim the latch before redirecting: a redirect the latch did not survive
	// is an infinite bounce between this page and the provider.
	//
	// CompareAndSwap rather than Store, for two reasons. Concurrent GETs would
	// both read AutoConnectDone=false and both start an upstream login; only
	// the swap winner may redirect. And a plain Store would recreate a
	// challenge that the approve POST's GetAndDelete had already consumed,
	// handing a replayed approval a live state to mint a second grant against.
	// Losing the race is not an error — it means someone else is driving this
	// challenge, so fall through and render.
	claimed := challengeState
	claimed.AutoConnectDone = true
	swapped, err := s.authnChallengeCache.CompareAndSwap(ctx, challengeState, claimed)
	if err != nil {
		logger.WarnContext(ctx, "claim auto-connect latch; falling back to the consent page", attr.SlogError(err))
		return false, nil
	}
	if !swapped {
		return false, nil
	}
	challengeState = claimed

	// autoRefresh is nil: the subject has not been shown the control yet, so
	// there is no choice to record. The page's own Connect action is what
	// authors a stored preference.
	challengeURL, err := s.buildRemoteConnectURL(ctx, logger, endpoint, challengeState, *client, nil)
	if err != nil {
		// Already logged. Render the page so the user can connect manually
		// rather than seeing an error for a step they did not take.
		return false, nil
	}

	http.Redirect(w, r, challengeURL, http.StatusSeeOther)
	return true, nil
}

// selectedSessionDuration returns the preselected option's short label, for
// the summary line. Empty when there is no picker to describe.
func selectedSessionDuration(options []sessionDurationOption) string {
	for _, o := range options {
		if o.Selected {
			return o.ShortLabel
		}
	}
	return ""
}
