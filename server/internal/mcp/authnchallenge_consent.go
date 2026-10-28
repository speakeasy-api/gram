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
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"log/slog"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgerrcode"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/consentpage"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	customdomainsrepo "github.com/speakeasy-api/gram/server/internal/customdomains/repo"
	"github.com/speakeasy-api/gram/server/internal/mcp/mcpmetrics"
	"github.com/speakeasy-api/gram/server/internal/mcp/mcproute"
	"github.com/speakeasy-api/gram/server/internal/mcp/metamcp"
	"github.com/speakeasy-api/gram/server/internal/mcp/toolfilter"
	"github.com/speakeasy-api/gram/server/internal/mcpaccess"
	metamcprepo "github.com/speakeasy-api/gram/server/internal/metamcp/repo"
	"github.com/speakeasy-api/gram/server/internal/networkingress"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/oauth/wellknown"
	"github.com/speakeasy-api/gram/server/internal/oauthui"
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

var consentTemplate = template.Must(
	template.Must(template.New("consent").Parse(consentTemplateHTML)).Parse(consentpage.LogoTemplate),
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
// Hardcoded to the /mcp surface, like the install-page script.
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

// ConsentPath is the path the consent page's forms post to.
func (d consentTemplateData) ConsentPath() string {
	if d.SharedConsentPath != "" {
		return d.SharedConsentPath
	}
	return "/" + d.MCPRouteBase.String() + "/" + d.MCPSlug + "/connect"
}

// consentFormMaxBytes caps the consent form body to defend against memory
// exhaustion (gosec G120). The tool picker can post consentToolNameLimit names
// of up to consentInventoryMaxNameBytes bytes each, inflated up to 3x by URL
// encoding; 1 MiB fits that worst case with room for the fixed fields while
// staying bounded.
const consentFormMaxBytes = 1 << 20 // 1 MiB

type consentApprovalError struct {
	// Message explains why this origin or capture cannot be approved.
	Message string
	// ActionURL leads to the required surface or access request.
	ActionURL string
	// ActionLabel names the recovery action when one exists.
	ActionLabel string
}

// consentTemplateData is the field set the consent template renders against.
type consentTemplateData struct {
	// GatewayDefaultMode labels the gateway default when choosing discovery.
	GatewayDefaultMode string
	// CodeModeUnavailable explains a stored code choice that is currently served as Progressive.
	CodeModeUnavailable bool
	// ShowCodeMode exposes Python execution when the runtime accepts new selections.
	ShowCodeMode bool
	// ShowDiscoveryMode exposes optional gateway connection settings.
	ShowDiscoveryMode bool
	// PrefillDiscoveryMode is the mode this subject chose on its previous
	// connection; empty selects the gateway default.
	PrefillDiscoveryMode metamcp.DiscoveryMode
	ClientName           string
	MCPSlug              string
	MCPRouteBase         mcproute.Base
	// SharedConsentPath is the consent page path on the issuer's shared
	// authorization server when it is serving the request. Empty when the
	// endpoint's own authorization server is, whose consent page lives under
	// MCPRouteBase and MCPSlug.
	SharedConsentPath  string
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
	// ApprovalError blocks approval while keeping Cancel available.
	ApprovalError *consentApprovalError
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
	// GatewayInventory is the captured gateway inventory used by both consent views.
	GatewayInventory string
	// ConsentToolsURL is the state-authorized inventory action the island
	// POSTs its tools/list request to.
	ConsentToolsURL string
	// ConsentToolsScriptURL is the content-hashed island bundle URL.
	ConsentToolsScriptURL string
	// ConsentToolsPrefill is the subject's stored selection serialized for
	// the island bootstrap; empty when there is no restrictive prefill.
	ConsentToolsPrefill string
	// ConsentToolsDeferred tells the island that a "not connected" inventory
	// answer only means identity chaining has not run yet, so approval
	// proceeds with the unrestricted default.
	ConsentToolsDeferred bool
	// ValidationDeadlineMS is the callback probe's absolute deadline. Only first-party pages poll because reloading interactive consent would discard unsaved tool choices.
	ValidationDeadlineMS int64
	// ConnectedCardCount is the number of RemoteSessionCards already linked or
	// holding a usable chained credential, rendered as the "n of m connected"
	// summary above the service list.
	ConnectedCardCount int
	// Styles is the compiled design-system stylesheet inlined into the
	// document head. A build artifact, never user input.
	Styles template.CSS
	// SelectedSessionDuration is the preselected length, surfaced on the
	// summary line so the session's lifetime is visible without opening the
	// configuration disclosure. Empty when there is no picker.
	SelectedSessionDuration string
	// AgentSelectionEnabled exposes the existing-agent selector only after the
	// human authorizer, rollout gates, and fixed endpoint target are verified.
	AgentSelectionEnabled bool
	AgentOptions          []consentAgentOption
	AgentSetupURL         string
	// Gateway replaces the list of services on a gateway's page with its
	// servers, or, on one server's connect page, narrows RemoteSessionCards
	// to that server's services. Nil for every other endpoint.
	Gateway *gatewayConsentView
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
// Connected, Expired and Unroutable are mutually exclusive and reflect the
// stored remote_session's usability: Connected means the runtime gate will
// accept it and forward it to this endpoint's backend; Expired means a stale
// link exists that must be re-established; Unroutable means a live link
// exists but was minted for another upstream (or before resources were
// recorded), so this endpoint's routing would never forward it; all false
// means never connected. Only Connected enables consent — the other two are
// no better than none until the user reconnects.
type remoteSessionCard struct {
	// consentPermissionsHash fingerprints the effective upstream request.
	consentPermissionsHash string
	// consentReuseReady requires a routable connection with all requested scopes.
	consentReuseReady bool

	ClientID   string
	IssuerSlug string

	// IssuerDisplay is the card's label: the resource's own name when the
	// client carries one, else the issuer's operator-set display name, else the slug. Issuer
	// branding is Speakeasy-controlled and tenant-set, unlike the
	// attacker-chosen CIMD client_name/logo_uri surfaced via
	// ClientIDOrigin, so the two stay visually separate on the page.
	IssuerDisplay string

	// IssuerLogoURL points at the issuer's logo through the public
	// assets.serveImage endpoint, empty when the issuer has no logo.
	IssuerLogoURL string

	// IssuerDocumentationURL is the documentation link (resource's, else issuer's); rendered only when non-empty.
	IssuerDocumentationURL string

	// IssuerPolicyURL is the data-usage policy link (resource's, else issuer's); rendered only when non-empty.
	IssuerPolicyURL string

	// IssuerTosURL is the terms link (resource's, else issuer's); rendered only when non-empty.
	IssuerTosURL string

	Connected  bool
	Expired    bool
	Unroutable bool
	// Chained marks a card without a usable interactive link whose upstream identity chaining is configured to serve; Connect stays a fallback.
	Chained bool
	// ChainCheck is a chained card's exchange check state; the page script settles pending.
	ChainCheck string
	// ChainMessage is the user-safe reason a rejected check shows.
	ChainMessage string
	CanRefresh   bool
	// IdentityReconnect marks a connected grant lacking openid while a reconnect would request it.
	IdentityReconnect bool

	// AttachmentReconnect requires this popup to produce a fresh grant for an agent attachment.
	AttachmentReconnect bool
	// Access expiry describes the current credential. Refresh expiry is kept
	// separate because a renewable one-hour access token is not a connection
	// with "no expiry." Empty values mean the provider omitted that lifetime.
	AccessExpiresAt  string
	AccessExpiresIn  string
	RefreshExpiresAt string
	RefreshExpiresIn string
	// AuthorizationExpiresAt is the absolute end of the grant, which no amount
	// of background refreshing extends — unlike the refresh idle timeout above,
	// which is exactly what auto refresh prevents. The page states the two
	// differently for that reason.
	AuthorizationExpiresAt string
	AuthorizationExpiresIn string
	// AutoRefreshChecked is the effective auto-refresh value for this card:
	// the stored preference when the organization lets subjects choose,
	// otherwise the organization's own policy value.
	AutoRefreshChecked bool

	// ConnectedAs is upstream-supplied text, rendered escaped as a secondary line.
	ConnectedAs string
	// AccountChips is upstream-supplied provider context (workspace, team, login), rendered escaped on its own line.
	AccountChips []string
	// IdentityCaveat qualifies ConnectedAs when it was not recorded as the grant's identity; rendered as a hover note.
	IdentityCaveat string

	// TokenActive is set when the provider's introspection last reported the access token active.
	TokenActive bool
	// TokenExpiresAt and TokenExpiresIn are that token's deadline: introspection's exp, else the
	// stored access expiry; empty when neither is known.
	TokenExpiresAt string
	TokenExpiresIn string

	// Verified, Rejected, Inactive, Unverified: the last probe's verdict; all false when none ran.
	Verified   bool
	Rejected   bool
	Inactive   bool
	Unverified bool
	// ValidatedAt and ValidatedAgo describe when that validation ran.
	ValidatedAt  string
	ValidatedAgo string
	// ValidationReason is the Speakeasy-authored explanation of a non-valid verdict.
	ValidationReason string
	// ValidationNotice is fixed page copy about a verify that did not run.
	ValidationNotice string
	// CanValidate marks a card whose credential this endpoint forwards upstream, so a check has a target.
	CanValidate bool
	// Pending marks a card whose automatic verification is still running, so the page refreshes until its verdict lands.
	Pending bool
	// SharedServerNote names, on a gateway server's connect page, the other
	// servers connecting this service also makes usable. Empty elsewhere.
	SharedServerNote string

	// remoteSessionIssuerID and projectID are the card's client's
	// authorization server and owning project, which match it to the gateway
	// servers it signs in to.
	remoteSessionIssuerID uuid.UUID
	projectID             uuid.NullUUID
}

// cardPanel is which detail lines a card renders. The status line says whether
// the grant is live; the panel describes a live grant, plus the one thing that
// stays useful when it is not: which account it was. The template checks only
// these booleans.
type cardPanel struct {
	// GrantLive is a connected card whose status line does not say reconnect.
	GrantLive bool

	ShowIdentity         bool
	ShowAccountContext   bool
	ShowToken            bool
	ShowValidationReason bool
	ShowLapse            bool
	ShowAutoRefresh      bool
	ShowAccessEnd        bool

	// ShowLinks is the issuer's documentation, policy, and terms links; shown in every state.
	ShowLinks bool

	// ShowDetails is whether the disclosure renders at all: any line above.
	ShowDetails bool
}

// Panel decides the detail lines from the card's state.
func (c remoteSessionCard) Panel() cardPanel {
	reconnect := c.Rejected || c.Inactive || c.Expired || c.Unroutable || c.IdentityReconnect || c.AttachmentReconnect
	hasGrant := c.Connected || c.Expired || c.Unroutable
	p := cardPanel{
		GrantLive:            c.Connected && !reconnect,
		ShowIdentity:         hasGrant && c.ConnectedAs != "",
		ShowAccountContext:   false,
		ShowToken:            false,
		ShowValidationReason: false,
		ShowLapse:            false,
		ShowAutoRefresh:      false,
		ShowAccessEnd:        false,
		ShowLinks:            c.IssuerDocumentationURL != "" || c.IssuerPolicyURL != "" || c.IssuerTosURL != "",
		ShowDetails:          false,
	}
	p.ShowAccountContext = hasGrant && len(c.AccountChips) > 0
	if p.GrantLive {
		p.ShowToken = c.TokenActive
		p.ShowValidationReason = c.Unverified && c.ValidationReason != ""
		// Auto refresh is what defeats the idle lapse, so the two lines are exclusive.
		p.ShowLapse = c.RefreshExpiresIn != "" && !c.AutoRefreshChecked
		p.ShowAutoRefresh = c.AutoRefreshChecked
		p.ShowAccessEnd = c.AuthorizationExpiresIn != ""
	}
	p.ShowDetails = p.ShowIdentity || p.ShowAccountContext || p.ShowToken || p.ShowValidationReason || p.ShowLapse || p.ShowAutoRefresh || p.ShowAccessEnd || p.ShowLinks
	return p
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
	endpoint, err := s.LoadResolvedMcpEndpointBySlug(ctx, logger, mcpSlug, mcproute.MCP)
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
// (GET) and consent POST handlers, called by HandleConsent after slug
// resolution.
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

func (s *Service) serveConsentGet(w http.ResponseWriter, r *http.Request, endpoint *ResolvedMcpEndpoint) (retErr error) {
	ctx := r.Context()
	logger := endpoint.LogWith(s.logger)

	stateID := r.URL.Query().Get("state")
	if stateID == "" {
		return fmt.Errorf("validate consent request: %w", oops.WithBrowserReason(oops.E(oops.CodeBadRequest, nil, "state is required").LogError(ctx, logger), oauthui.ReasonInvalidLink))
	}

	challengeState, err := s.authnChallengeCache.Get(ctx, "authnChallenge:"+stateID)
	if err != nil {
		return consentChallengeCacheError(ctx, logger, err)
	}
	if challengeState.FirstParty {
		defer func() { retErr = s.firstPartyBrowserError(retErr) }()
	}
	if err := validateChallengeBrowser(r, challengeState, false); err != nil {
		return fmt.Errorf("validate consent request: %w", oops.WithBrowserReason(oops.E(oops.CodeBadRequest, err, "invalid consent browser binding"), oauthui.ReasonSessionChanged))
	}
	logger = logger.With(attr.SlogOAuthFlowID(challengeState.FlowID))
	if err := endpoint.ValidateChallenge(ctx, challengeState.Endpoint, challengeState.UserSessionIssuerID); err != nil {
		if errors.Is(err, networkingress.ErrAuthorityUnavailable) {
			s.metrics.RecordOAuthAuthorityUnavailable(ctx, endpoint.UserSessionIssuerID.String(), endpoint.Slug, mcpmetrics.OAuthFlowStageConsent)
		}
		return oauthAuthorityError(err).LogError(ctx, logger)
	}

	if challengeState.Federation != nil {
		return s.completeFederatedBrowserHandoff(w, r, challengeState)
	}

	// First-party challenges (minted by ServeFirstPartyConnect) have no
	// DCR-registered client; the connect page is the dashboard linking the
	// user's own upstream sessions. Skip the client lookup and label the page
	// generically.
	clientName := "Speakeasy"
	clientIDOrigin := ""
	loopbackRedirectWarning := false
	var clientRowID uuid.UUID
	if !challengeState.FirstParty {
		client, err := s.resolveUserSessionClient(ctx, logger, endpoint, challengeState.ClientID, lookupClientOnly)
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return oops.E(oops.CodeBadRequest, err, "user session client revoked").LogWarn(ctx, logger)
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
		return oops.E(oops.CodeBadRequest, nil, "authn challenge subject is not resolved").LogWarn(ctx, logger)
	}

	if endpoint.MetaMcpServerID.Valid {
		if err := s.consentToolInventoryCache.DeleteByPrefix(ctx, "consentToolInventory:"+stateID+":"); err != nil {
			return oops.E(oops.CodeUnavailable, err, "could not invalidate the previous gateway capture")
		}
	}

	subjectDisplay := resolveSubjectDisplay(ctx, s.db, *challengeState.Subject)

	autoRefreshPolicy := s.resolveAutoRefreshPolicy(ctx, endpoint.OrganizationID)
	cards, err := s.buildRemoteSessionCards(ctx, endpoint, challengeState, autoRefreshPolicy)
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "build remote session cards").LogError(ctx, logger)
	}
	validationDeadlineMS := int64(0)
	if deadlineMS, parseErr := strconv.ParseInt(r.URL.Query().Get("verifying_until"), 10, 64); parseErr == nil {
		now := time.Now()
		deadline := time.UnixMilli(deadlineMS)
		maxDeadline := now.Add(s.metaRuntime.ValidationTimeout)
		if deadline.After(now) && !deadline.After(maxDeadline) {
			clientID := r.URL.Query().Get("verifying_client")
			for i := range cards {
				if cards[i].ClientID == clientID && cards[i].Connected && cards[i].ValidatedAt == "" {
					cards[i].Pending = true
					if challengeState.FirstParty {
						validationDeadlineMS = deadlineMS
					}
					break
				}
			}
		}
	}
	if limited := r.URL.Query().Get("validate_limited"); limited != "" {
		for i := range cards {
			if cards[i].ClientID == limited {
				cards[i].ValidationNotice = validationLimitedNotice
			}
		}
	}

	// A gateway's page approves the gateway and lists its servers; the
	// services it shows are those of one server, on that server's connect
	// page. A first-party connect opened for a server lands on its page, and
	// an explicit server parameter (empty for the gateway's own page) wins.
	var gateway *gatewayConsentView
	focusServer := ""
	if endpoint.MetaMcpServerID.Valid {
		focus := challengeState.FocusServer
		if challengeState.CodeReconnect == nil && r.URL.Query().Has("server") {
			focus = r.URL.Query().Get("server")
		}
		if len(focus) > maxFocusServerLength {
			return oops.E(oops.CodeBadRequest, nil, "server is too long").LogError(ctx, logger)
		}
		view, focused, gerr := s.gatewayConsent(ctx, endpoint, challengeState, cards, focus)
		if gerr != nil {
			return oops.E(oops.CodeUnexpected, gerr, "build gateway consent").LogError(ctx, logger)
		}
		gateway = view
		cards = focused
		if view.Focus != nil {
			focusServer = view.Focus.Slug
		}
	}

	connectedCardCount := 0
	chainedCardCount := 0
	autoRefreshHasSessions := false
	// Every card already carries the organization's policy applied to its own
	// stored preference, so the page value is on only when none of them is off.
	everyCardAutoRefreshes := true
	for _, c := range cards {
		// A chained service counts once its exchange is known to work; access rules use chainedCardCount.
		if c.Connected || c.ChainCheck == chainCheckConnected {
			connectedCardCount++
		}
		if c.Chained {
			chainedCardCount++
		}
		if c.Connected || c.Expired || c.Unroutable {
			autoRefreshHasSessions = true
		}
		everyCardAutoRefreshes = everyCardAutoRefreshes && c.AutoRefreshChecked
	}
	autoRefreshOn := len(cards) > 0 && everyCardAutoRefreshes
	// Gateway capture below decides whether any member can be approved.
	consentEnabled := gateway != nil || len(cards) == 0 || connectedCardCount > 0 || chainedCardCount > 0

	agentSelectionEnabled := false
	agentSetupURL := ""
	var agentOptions []consentAgentOption
	if !challengeState.FirstParty && challengeState.AuthorizerUserID != "" {
		if setupURL, serr := s.agentSetupURL(ctx, logger, endpoint.OrganizationID); serr != nil {
			logger.WarnContext(ctx, "agent selection unavailable", attr.SlogError(serr))
		} else if options, aerr := s.eligibleConsentAgents(ctx, challengeState, endpoint); aerr != nil {
			logger.WarnContext(ctx, "eligible agent selection unavailable", attr.SlogError(aerr))
		} else {
			agentSelectionEnabled = true
			agentSetupURL = setupURL
			agentOptions = options
		}
	}

	if r.URL.Query().Get("consent") != "required" && len(agentOptions) == 0 &&
		consentReuseEligible(endpoint, challengeState) && consentBrowserMatches(r, challengeState, time.Now()) &&
		consentRemoteFingerprint(endpoint, cards) != "" {
		eligible, eligibilityErr := s.consentToolPickerEligible(ctx, endpoint)
		lockedDown, lockdownErr := s.customDomainLockdownApplies(ctx, logger, endpoint.ProjectID)
		if eligibilityErr != nil {
			logger.WarnContext(ctx, "saved consent eligibility unavailable", attr.SlogError(eligibilityErr))
		}
		if eligible && eligibilityErr == nil && lockdownErr == nil && !lockedDown {
			if _, lookupErr := usersessions_repo.New(s.db).GetUserSessionConsentResource(ctx, consentResourceParams(endpoint, challengeState, clientRowID)); lookupErr == nil {
				return renderConsentReuse(w, endpoint, challengeState)
			} else if !errors.Is(lookupErr, pgx.ErrNoRows) {
				logger.WarnContext(ctx, "saved consent lookup unavailable", attr.SlogError(lookupErr))
			}
		}
	}

	// Skip the interstitial when it has nothing to ask. A server fronting a
	// single upstream that the subject has not linked yet leaves the page with
	// exactly one useful control, and the user already expressed intent by
	// starting the authorization — so send them straight to the provider and
	// let them land back here with something to approve. Multi-service pages
	// keep the list: each provider is its own consent decision, and a silent
	// chain of redirects through three login screens is worse than a list that
	// shows what is being asked for.
	// When an eligible existing agent is available, render the choice before
	// starting any human remote-credential flow. A user who chooses themselves
	// can still connect the service from the rendered cards.
	// On a gateway, only a connect opened for one server, as a tool call's
	// link opens it, goes straight on to that server's provider. A server
	// picked from the gateway's list shows its page first, which says what
	// connecting does.
	autoConnectCards := cards
	if challengeState.FirstParty && challengeState.Subject != nil {
		pendingClient := challengeState.ReauthenticateClientID
		if s.bindingRefresh != nil && challengeState.BindingRefreshAttemptID != "" {
			client, err := s.bindingRefresh.PendingClient(ctx, challengeState.BindingRefreshAttemptID, *challengeState.Subject)
			if err != nil {
				return fmt.Errorf("read attachment reconnect requirement: %w", err)
			}
			if client != uuid.Nil {
				pendingClient = client
			}
		}
		if pendingClient != uuid.Nil {
			found := false
			for i := range cards {
				if cards[i].ClientID == pendingClient.String() {
					cards[i].AttachmentReconnect = true
					target := cards[i]
					target.Connected = false
					autoConnectCards = []remoteSessionCard{target}
					found = true
					break
				}
			}
			if !found {
				return oops.E(oops.CodeForbidden, nil, "requested reconnect client is not available on this server")
			}
		}
	}
	autoConnect := gateway == nil || (gateway.Focus != nil && challengeState.FirstParty && challengeState.FocusServer == gateway.Focus.Slug)
	if len(agentOptions) == 0 && autoConnect {
		if redirected, err := s.maybeAutoConnect(ctx, w, r, logger, endpoint, challengeState, autoConnectCards, focusServer); err != nil {
			return err
		} else if redirected {
			return nil
		}
	}

	// First-party pages mint no user session, so there is no length to pick.
	// A lookup failure degrades to no picker rather than a failed render; the
	// mint falls back to the issuer default in that case anyway.
	// A gateway server's connect page has no approval for a length to apply to.
	var durationOptions []sessionDurationOption
	if !challengeState.FirstParty && focusServer == "" {
		if issuer, ierr := usersessions_repo.New(s.db).GetUserSessionIssuerByID(ctx, usersessions_repo.GetUserSessionIssuerByIDParams{
			ID:             endpoint.UserSessionIssuerID,
			ProjectID:      endpoint.ProjectID,
			OrganizationID: endpoint.OrganizationID,
		}); ierr == nil {
			durationOptions = buildSessionDurationOptions(issuer)
		}
	}

	// Modern single-server endpoints offer optional tool selection. Gateways
	// require a complete capture before approving any discovery mode.
	showToolsIsland := false
	if !challengeState.FirstParty && focusServer == "" {
		var eligibilityErr error
		showToolsIsland, eligibilityErr = s.consentToolPickerEligible(ctx, endpoint)
		if eligibilityErr != nil {
			logger.WarnContext(ctx, "consent tool picker eligibility unavailable", attr.SlogError(eligibilityErr))
		}
	}
	var gatewayInventory string
	var approvalError *consentApprovalError
	if showToolsIsland && gateway != nil {
		lockedDown, lockErr := s.endpointCustomDomainLockdown(ctx, logger, endpoint)
		if lockErr != nil {
			return lockErr
		}
		if lockedDown {
			domain, domainErr := customdomainsrepo.New(s.db).GetCustomDomainByOrganization(ctx, endpoint.OrganizationID)
			if domainErr != nil {
				return oops.E(oops.CodeUnavailable, domainErr, "could not load the gateway's custom domain")
			}
			approvalError = &consentApprovalError{Message: "This gateway's custom domain is not ready. Ask an administrator to finish setup before reconnecting your client.", ActionURL: "", ActionLabel: ""}
			if domain.Verified && domain.Activated {
				gatewayURL, err := endpoint.RootURL("https://" + domain.Domain)
				if err != nil {
					return oops.E(oops.CodeUnexpected, err, "build custom-domain gateway URL").LogError(ctx, logger)
				}
				approvalError = &consentApprovalError{Message: "Reconnect your AI client using this gateway's MCP URL on its custom domain:", ActionURL: gatewayURL, ActionLabel: gatewayURL}
			}
		} else {
			gatewayInventory, err = s.captureGatewayConsentInventory(r, endpoint, challengeState, gateway)
			if err != nil {
				logger.WarnContext(ctx, "gateway consent capture failed", attr.SlogError(err))
				approvalError = &consentApprovalError{Message: "Tools could not be captured. This gateway cannot be approved. Try again later.", ActionURL: "", ActionLabel: ""}
				if denied, ok := errors.AsType[*oops.ShareableError](err); ok && denied.Code == oops.CodeForbidden {
					approvalError.Message = "You do not have access to this gateway. Request access before approving it."
					approvalError.ActionURL = s.requestAccessURL(ctx, endpoint.connectResourceID().String(), endpoint.Slug)
					approvalError.ActionLabel = "Request access"
					if endpoint.IsOrganizationGateway() {
						if auth, ok := contextvalues.GetAuthContext(ctx); ok && auth != nil {
							approvalError.ActionURL = mcpaccess.AuthorizationChallengesURL(s.siteURL, auth.OrganizationSlug)
						}
					}
				}
				if errors.Is(err, errConnectionCatalogTooLarge) {
					approvalError.Message = "Tool definitions are too large to approve here. Ask an administrator to reduce the tool inventory."
				}
				if errors.Is(err, errConsentCaptureSuperseded) {
					approvalError.Message = "This page was replaced by a newer one. Reload the page to continue."
				}
			} else if !gateway.AnyReady() {
				approvalError = &consentApprovalError{Message: "No servers could be verified. Resolve the server statuses above before approving access.", ActionURL: "", ActionLabel: ""}
			}
		}
	} else if showToolsIsland {
		lockedDown, lerr := s.endpointCustomDomainLockdown(ctx, logger, endpoint)
		if lerr != nil {
			return lerr
		}
		showToolsIsland = !lockedDown
	}
	if approvalError != nil {
		showToolsIsland = false
		consentEnabled = false
		agentSelectionEnabled = false
	}

	prefillAttr := ""
	if showToolsIsland {
		if policy := s.consentStoredSessionPolicy(ctx, endpoint, *challengeState.Subject, clientRowID); policy != nil {
			prefillAttr = consentPrefillAttr(policy.Selection, policy.Catalog)
		}
	}
	inventoryDeferred := gateway == nil && consentInventoryDeferred(cards, prefillAttr != "")
	showDiscoveryMode := !challengeState.FirstParty && focusServer == "" && gatewayDiscoveryOptionsOffered(endpoint)
	var prefillDiscoveryMode metamcp.DiscoveryMode
	if showDiscoveryMode {
		prefillDiscoveryMode = s.consentDiscoveryModePrefill(ctx, endpoint, *challengeState.Subject, clientRowID)
	}

	gatewayDefaultMode := ""
	codeModeAvailable := showDiscoveryMode && s.CodeModeAvailable(ctx, endpoint.OrganizationID)
	codeModeUnavailable := prefillDiscoveryMode == metamcp.DiscoveryModeCode && !codeModeAvailable
	if codeModeUnavailable {
		prefillDiscoveryMode = metamcp.DiscoveryModeProgressive
	}
	if (endpoint.IsOrganizationGateway() || endpoint.MetaMcpServerID.Valid) && focusServer == "" {
		defaultMode, readErr := s.gatewayDefaultDiscoveryMode(ctx, endpoint)
		// Display only: an unreadable default leaves the page without it.
		if readErr != nil {
			logger.WarnContext(ctx, "read gateway discovery mode", attr.SlogError(readErr))
		}
		switch {
		case readErr != nil:
		case defaultMode == metamcp.DiscoveryModeCode:
			gatewayDefaultMode = "Code Mode"
			// admitMetaGate serves Code Mode gateways as Progressive while the
			// runtime is off or the organization is not entitled to Code Mode.
			if !s.CodeModeAvailable(ctx, endpoint.OrganizationID) {
				gatewayDefaultMode = "Progressive"
				codeModeUnavailable = true
			}
		case defaultMode == metamcp.DiscoveryModeDirect:
			gatewayDefaultMode = "Direct"
		default:
			gatewayDefaultMode = "Progressive"
		}
	}
	data := consentTemplateData{
		GatewayDefaultMode:      gatewayDefaultMode,
		CodeModeUnavailable:     codeModeUnavailable,
		ClientName:              clientName,
		MCPSlug:                 endpoint.Slug,
		MCPRouteBase:            endpoint.RouteBase,
		SharedConsentPath:       sharedConsentPath(endpoint),
		State:                   stateID,
		CSRFToken:               challengeState.CSRFToken,
		SubjectDisplay:          subjectDisplay,
		RedirectURI:             challengeState.RedirectURI,
		ScriptURL:               consentScriptURL,
		RemoteSessionCards:      cards,
		ConsentEnabled:          consentEnabled,
		ApprovalError:           approvalError,
		FirstParty:              challengeState.FirstParty,
		ClientIDOrigin:          clientIDOrigin,
		LoopbackRedirectWarning: loopbackRedirectWarning,
		AutoClose:               shouldAutoCloseFirstParty(challengeState.FirstParty, cards),
		SessionDurationOptions:  durationOptions,
		AutoRefreshPolicy:       autoRefreshPolicy,
		AutoRefreshOn:           autoRefreshOn,
		AutoRefreshHasSessions:  autoRefreshHasSessions,
		ShowToolsIsland:         showToolsIsland,
		GatewayInventory:        gatewayInventory,
		ShowDiscoveryMode:       showDiscoveryMode,
		PrefillDiscoveryMode:    prefillDiscoveryMode,
		ShowCodeMode:            codeModeAvailable,
		ConsentToolsURL:         endpoint.consentPath() + "/mcp",
		ConsentToolsScriptURL:   consentToolsScriptURL,
		ConsentToolsPrefill:     prefillAttr,
		ConsentToolsDeferred:    inventoryDeferred,
		ValidationDeadlineMS:    validationDeadlineMS,
		ConnectedCardCount:      connectedCardCount,
		Styles:                  consentpage.Styles,
		SelectedSessionDuration: selectedSessionDuration(durationOptions),
		AgentSelectionEnabled:   agentSelectionEnabled,
		AgentOptions:            agentOptions,
		AgentSetupURL:           agentSetupURL,
		Gateway:                 gateway,
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	// Consent cannot be embedded, even by the same origin. Do not restrict
	// form-action: consent submissions redirect to clients on other origins,
	// including loopback addresses and custom URI schemes.
	w.Header().Set("Content-Security-Policy", "frame-ancestors 'none'")
	w.Header().Set("X-Frame-Options", "DENY")
	if err := consentTemplate.Execute(w, data); err != nil {
		return oops.E(oops.CodeUnexpected, err, "render consent template").LogError(ctx, logger)
	}
	return nil
}

func (s *Service) serveConsentPost(w http.ResponseWriter, r *http.Request, endpoint *ResolvedMcpEndpoint) (retErr error) {
	ctx := r.Context()

	r.Body = http.MaxBytesReader(w, r.Body, consentFormMaxBytes)
	if err := r.ParseForm(); err != nil {
		return oops.E(oops.CodeBadRequest, err, "failed to parse form").LogError(ctx, s.logger)
	}

	logger := endpoint.LogWith(s.logger)

	stateID := r.PostForm.Get("state")
	if stateID == "" {
		return fmt.Errorf("validate consent request: %w", oops.WithBrowserReason(oops.E(oops.CodeBadRequest, nil, "state is required").LogError(ctx, logger), oauthui.ReasonInvalidLink))
	}

	// Preflight on a plain Get: everything that can fail for retryable
	// reasons (validation, inventory snapshot, client lookup, selection
	// parsing) runs BEFORE the challenge is consumed, so a transient failure
	// leaves the page usable. The consuming GetAndDelete below re-validates
	// against the consumed value, which stays the single-use authority.
	challengeState, err := s.authnChallengeCache.Get(ctx, "authnChallenge:"+stateID)
	if err != nil {
		return consentChallengeCacheError(ctx, logger, err)
	}
	if challengeState.FirstParty {
		defer func() { retErr = s.firstPartyBrowserError(retErr) }()
	}
	if err := validateChallengeBrowser(r, challengeState, false); err != nil {
		return fmt.Errorf("validate consent request: %w", oops.WithBrowserReason(oops.E(oops.CodeBadRequest, err, "invalid consent browser binding"), oauthui.ReasonSessionChanged))
	}
	logger = logger.With(attr.SlogOAuthFlowID(challengeState.FlowID))
	issuerID := endpoint.UserSessionIssuerID.String()
	mcpSlug := endpoint.Slug

	// The guards below (state-confusion ref check, CSRF, and the unknown-action
	// default) are deliberately NOT counted as flow failures: they are
	// attacker-controllable, so emitting `failed` here would let crafted
	// requests pollute a config's health signal. A legitimate user never
	// trips them; the rare case lands in the started-without-terminal gap.
	if err := s.validateConsentChallenge(ctx, endpoint, &challengeState, r.PostForm.Get("csrf_token")); err != nil {
		return err.LogError(ctx, logger)
	}

	// Explicit action required: fail closed on missing / unknown values so
	// a malformed form post can't trigger the approval path. Checked before
	// any flow-outcome metric is recorded, so a crafted action stays in the
	// attacker-controllable bucket the guards above describe rather than
	// counting against a config's health signal.
	action := r.PostForm.Get("action")
	if action != "approve" && action != "approve_agent" && action != "deny" && action != "reuse" {
		return oops.E(oops.CodeBadRequest, nil, `action must be "approve", "approve_agent", "reuse", or "deny"`).LogError(ctx, logger)
	}

	reuse := action == "reuse"
	if reuse {
		if !consentReuseEligible(endpoint, challengeState) || !consentBrowserMatches(r, challengeState, time.Now()) {
			http.Redirect(w, r, consentFullPageURL(endpoint, challengeState), http.StatusSeeOther)
			return nil
		}
		agents, err := s.eligibleConsentAgents(ctx, challengeState, endpoint)
		if err != nil || len(agents) != 0 {
			http.Redirect(w, r, consentFullPageURL(endpoint, challengeState), http.StatusSeeOther)
			return nil
		}
	}

	// The RFC 9207 `iss` both branches below emit, resolved once so the deny
	// and success responses cannot disagree. It hangs off the origin the
	// challenge was minted under, not this request's: the remote-session
	// return leg re-enters consent on the platform origin, so a POST carrying
	// a custom-domain context can still be completing a flow the client
	// recorded under a different origin (or vice versa).
	issuer, err := s.issuerURL(endpoint, challengeState.mintOriginOr(s.BaseURLForRequest(r)))
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
			return consentChallengeCacheError(ctx, logger, err)
		}
		s.evictConsentToolInventory(ctx, stateID)
		s.retireDeniedConsentCredentials(ctx, logger, endpoint, challengeState)
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
		return oops.E(oops.CodeBadRequest, nil, "authn challenge subject is not resolved").LogWarn(ctx, logger)
	}
	if endpoint.MetaMcpServerID.Valid && !challengeState.FirstParty {
		lockedDown, err := s.endpointCustomDomainLockdown(ctx, logger, endpoint)
		if err != nil {
			return err
		}
		if lockedDown {
			return oops.E(oops.CodeForbidden, nil, "Start authorization from this gateway's custom domain to approve access.")
		}
	}

	selectedAgentID := strings.TrimSpace(r.PostForm.Get("agent_id"))
	if (action == "approve_agent") != (selectedAgentID != "") {
		// A distinct action makes mixed-version consent POSTs fail closed: an
		// older server rejects approve_agent instead of silently issuing a human
		// grant while ignoring the newly introduced agent_id field.
		return oops.E(oops.CodeBadRequest, nil, "agent approval action and selection do not match").LogError(ctx, logger)
	}
	// The credential the grant mints is the selected agent's for an agent
	// approval, so its upstream client picks are judged by the agent's own
	// connections.
	pickState := challengeState
	if selectedAgentID != "" {
		selectedAgent, err := s.authorizeConsentAgent(ctx, challengeState, endpoint, selectedAgentID)
		if err != nil {
			return consentAgentAuthorizationError(err, "selected agent is not eligible").LogWarn(ctx, logger)
		}
		// Keep the challenge retryable while the human connects and attaches
		// required services. Human-owned tokens alone do not authorize an agent.
		subject := urn.NewAgentSubject(selectedAgent.AgentID)
		pickState.Subject = &subject
		agentCtx, err := s.contextForSessionSubject(ctx, endpoint, subject, "", "")
		if err != nil {
			return oops.E(oops.CodeUnavailable, err, "resolve selected agent identity").LogWarn(ctx, logger)
		}
		if err := s.remoteChallengeMgr.CheckAccessTokens(agentCtx, endpoint.ProjectID, endpoint.OrganizationID, endpoint.UserSessionIssuerID, subject); err != nil {
			if errors.Is(err, remotesessions.ErrRemoteSessionUnavailable) {
				return remoteSessionUnavailableError(w, err).LogWarn(ctx, logger)
			}
			if errors.Is(err, remotesessions.ErrRemoteSessionMisconfigured) {
				return oops.E(oops.CodeFailedPrecondition, err, "%s", remoteSessionMisconfiguredDescription).LogWarn(ctx, logger)
			}
			if !errors.Is(err, remotesessions.ErrNoValidToken) {
				return oops.E(oops.CodeUnavailable, err, "check agent connections").LogError(ctx, logger)
			}
			return oops.E(oops.CodeConflict, err, "connect and attach the required services before authorizing this agent").LogWarn(ctx, logger)
		}
	}

	// Gateway approval always binds to a complete attempt, including live
	// discovery: only successfully discovered members may be authorized.
	materializeGateway := endpoint.MetaMcpServerID.Valid && !challengeState.FirstParty && selectedAgentID == ""
	var boundInventory *consentToolInventory
	if reuse {
		boundInventory, err = s.consentReuseInventory(ctx, endpoint, challengeState, r.PostForm.Get("tool_inventory_id"))
		if err != nil || boundInventory == nil {
			http.Redirect(w, r, consentFullPageURL(endpoint, challengeState), http.StatusSeeOther)
			return nil
		}
	}
	if selectedAgentID == "" && !reuse && (r.PostForm.Get("tool_filtering") == "on" || materializeGateway) {
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
		inventory, found, gerr := s.getCompletedConsentInventory(ctx, endpoint, stateID, attempt)
		if gerr != nil {
			return oops.E(oops.CodeUnavailable, gerr, "service temporarily unavailable").LogError(ctx, logger)
		}
		if !found {
			return oops.E(oops.CodeConflict, nil, "tool inventory is no longer available; reload the page and try again").LogWarn(ctx, logger)
		}
		boundInventory = &inventory
		if materializeGateway && len(inventory.MemberBindings) == 0 {
			return oops.E(oops.CodeFailedPrecondition, nil, "No servers could be verified. Resolve the server statuses before approving access.")
		}
	}

	if selectedAgentID == "" && !reuse && boundInventory == nil && r.PostForm.Get("tool_inventory_id") != "" {
		if attempt, aerr := consentAttemptID(r.PostForm.Get("tool_inventory_id")); aerr == nil {
			if inventory, found, gerr := s.getCompletedConsentInventory(ctx, endpoint, stateID, attempt); gerr == nil && found {
				boundInventory = &inventory
			}
		}
	}

	var toolSelection *toolfilter.SessionSelection
	if selectedAgentID == "" && !reuse {
		toolSelection, err = chosenToolSelection(r.PostForm, boundInventory)
		if err != nil {
			return oops.E(oops.CodeBadRequest, err, "invalid tool selection").LogError(ctx, logger)
		}
	}

	catalogMode := toolfilter.CatalogModeLive
	if materializeGateway {
		if toolSelection != nil {
			catalogMode = toolfilter.CatalogModeCached
		}
		if requested := r.PostForm.Get("tool_catalog_mode"); requested != "" {
			catalogMode = toolfilter.CatalogMode(requested)
		}
		if catalogMode != toolfilter.CatalogModeLive && catalogMode != toolfilter.CatalogModeCached {
			return oops.E(oops.CodeBadRequest, nil, "invalid tool catalog mode").LogWarn(ctx, logger)
		}
		if toolSelection != nil && len(toolSelection.LiveAnnotations()) > 0 {
			return oops.E(oops.CodeBadRequest, nil, "Specific tools must use the tools captured at consent").LogWarn(ctx, logger)
		}
		if toolSelection != nil && catalogMode != toolfilter.CatalogModeCached {
			return oops.E(oops.CodeBadRequest, nil, "Specific tools requires a cached catalog").LogWarn(ctx, logger)
		}
	}
	toolPolicy, err := s.consentGatewayPolicy(ctx, endpoint, challengeState, selectedAgentID, r.PostForm.Get("discovery_mode"))
	if err != nil {
		return err
	}

	// Resolve the user_session_clients row id for the consent FK.
	clientRow, err := s.resolveUserSessionClient(ctx, logger, endpoint, challengeState.ClientID, lookupClientOnly)
	if err != nil {
		// Client revoked mid-flow (config change) or DB error — either way the
		// approved flow can't complete.
		s.metrics.RecordOAuthFlowFailed(ctx, issuerID, mcpSlug, mcpmetrics.OAuthFlowStageConsent)
		if errors.Is(err, pgx.ErrNoRows) {
			return oops.E(oops.CodeBadRequest, err, "user session client revoked").LogWarn(ctx, logger)
		}
		return oops.E(oops.CodeUnexpected, err, "lookup user session client").LogError(ctx, logger)
	}

	// Capture private live authority while the challenge remains retryable. A
	// transient ingress lookup failure must not consume state or persist consent.
	authorityStarted := time.Now()
	grantEndpoint, err := endpoint.EndpointRef(ctx, s.db, challengeState.mintOriginOr(s.BaseURLForRequest(r)))
	if err != nil {
		s.recordPrivateOAuthAuthority(ctx, challengeState.Endpoint.Authority, authorityStarted, err)
		if errors.Is(err, networkingress.ErrAuthorityUnavailable) {
			s.metrics.RecordOAuthAuthorityUnavailable(ctx, issuerID, mcpSlug, mcpmetrics.OAuthFlowStageConsent)
			return oops.E(oops.CodeUnavailable, err, "capture authorization-code endpoint authority").LogError(ctx, logger)
		}
		s.metrics.RecordOAuthFlowFailed(ctx, issuerID, mcpSlug, mcpmetrics.OAuthFlowStageConsent)
		return oops.E(oops.CodeBadRequest, err, "private OAuth authority is no longer valid").LogWarn(ctx, logger)
	}
	s.recordPrivateOAuthAuthority(ctx, challengeState.Endpoint.Authority, authorityStarted, nil)

	duration := desiredSessionDurationHours(r.PostForm.Get("session_duration_hours"))
	var approval *consentApprovalTransaction
	// Both authorization routes share client and resource identities. Every
	// explicit human approval must supersede older evidence, including when
	// the per-endpoint route cannot save a reusable replacement.
	if selectedAgentID == "" && !challengeState.FirstParty && challengeState.Subject.Kind == urn.SessionSubjectKindUser {
		evidence := boundInventory
		if !consentReuseEligible(endpoint, challengeState) {
			evidence = nil
		}
		approval, err = s.beginConsentApproval(ctx, endpoint, challengeState, clientRow.ID, evidence, toolSelection, duration, reuse)
		if err != nil {
			return oops.E(oops.CodeUnavailable, err, "prepare consent approval").LogError(ctx, logger)
		}
		if approval == nil {
			http.Redirect(w, r, consentFullPageURL(endpoint, challengeState), http.StatusSeeOther)
			return nil
		}
		defer o11y.NoLogDefer(func() error { return approval.tx.Rollback(ctx) })
		toolSelection, duration = approval.selection, approval.duration
	}

	// For each server offering several upstream clients, the one usable card
	// the credential goes through is recorded as its choice. Read before the
	// challenge is consumed, so a failure leaves the page retryable.
	upstreamPicks, err := s.consentUpstreamClientPicks(ctx, endpoint, pickState)
	if err != nil {
		return oops.E(oops.CodeUnavailable, err, "read upstream client picks").LogError(ctx, logger)
	}

	if materializeGateway {
		reference, err := s.publishConnectionCatalog(ctx, endpoint, challengeState, boundInventory, toolSelection, catalogMode, r.PostForm.Get("tool_filtering") != "on", upstreamPicks, duration, time.Now())
		if err != nil {
			if errors.Is(err, errConnectionCatalogTooLarge) {
				return oops.E(oops.CodeInvalid, err, "Selected tool definitions are too large to approve. Choose fewer tools or contact an administrator.").LogWarn(ctx, logger)
			}
			if errors.Is(err, errConsentConfigurationChanged) {
				return oops.E(oops.CodeFailedPrecondition, err, "Server configuration changed. Reload consent to review access.").LogWarn(ctx, logger)
			}
			return oops.E(oops.CodeUnavailable, err, "could not save the approved tool catalog; retry consent").LogError(ctx, logger)
		}
		// consentGatewayPolicy records a mode for every connection that
		// materializes a gateway catalog, and the selection rides inside it.
		if toolPolicy == nil {
			return oops.E(oops.CodeUnexpected, nil, "gateway approval carries no connection options").LogError(ctx, logger)
		}
		toolPolicy.Selection = toolSelection
		toolPolicy.Catalog = reference
		if _, err := json.Marshal(toolPolicy); err != nil {
			return oops.E(oops.CodeInvalid, err, "invalid gateway approval; reload consent").LogWarn(ctx, logger)
		}
	}

	// Atomic GETDEL: a consent approval consumes the authn-challenge state
	// single-use. Parallel POSTs (e.g. user double-submits) lose the race
	// and get "not found or expired", so only one grant is ever minted per
	// authorization request. The consumed value is the authority — re-run
	// the guards against it in case the preflighted copy went stale.
	challengeState, err = s.authnChallengeCache.GetAndDelete(ctx, "authnChallenge:"+stateID)
	if err != nil {
		return consentChallengeCacheError(ctx, logger, err)
	}
	if err := s.validateConsentChallenge(ctx, endpoint, &challengeState, r.PostForm.Get("csrf_token")); err != nil {
		return err.LogError(ctx, logger)
	}
	if reuse && !consentBrowserMatches(r, challengeState, time.Now()) {
		return fmt.Errorf("validate consent request: %w", oops.WithBrowserReason(oops.E(oops.CodeBadRequest, nil, "invalid consent browser binding").LogWarn(ctx, logger), oauthui.ReasonSessionChanged))
	}
	subject := *challengeState.Subject

	var agentAuthorization *AgentAuthorizationResult
	if selectedAgentID != "" {
		finalEndpoint, ferr := s.loadResolvedMcpEndpointByRef(ctx, challengeState.Endpoint)
		if ferr != nil {
			s.metrics.RecordOAuthFlowFailed(ctx, issuerID, mcpSlug, mcpmetrics.OAuthFlowStageConsent)
			return oops.E(oops.CodeForbidden, ferr, "selected agent is not eligible").LogWarn(ctx, logger)
		}
		agentAuthorization, ferr = s.authorizeConsentAgent(ctx, challengeState, finalEndpoint, selectedAgentID)
		if ferr != nil {
			s.metrics.RecordOAuthFlowFailed(ctx, issuerID, mcpSlug, mcpmetrics.OAuthFlowStageConsent)
			return consumedConsentAgentAuthorizationError(ferr).LogWarn(ctx, logger)
		}
	}

	// Persist the consent record. The unique index on
	// (principal_urn, user_session_client_id, remote_set_hash) makes this
	// idempotent on re-consent for the same set; we treat the duplicate-key
	// error as a no-op (consent already on file).
	if agentAuthorization == nil && approval == nil {
		if _, err := usersessions_repo.New(s.db).CreateUserSessionConsent(ctx, usersessions_repo.CreateUserSessionConsentParams{
			SubjectUrn:          subject,
			UserSessionClientID: clientRow.ID,
			RemoteSetHash:       remoteSetHashEmpty,
		}); err != nil && !isUniqueViolation(err) {
			s.metrics.RecordOAuthFlowFailed(ctx, issuerID, mcpSlug, mcpmetrics.OAuthFlowStageConsent)
			if errors.Is(err, pgx.ErrNoRows) {
				return oops.E(oops.CodeNotFound, err, "oauth client not found").LogWarn(ctx, logger)
			}
			return oops.E(oops.CodeUnexpected, err, "record consent").LogError(ctx, logger)
		}
	}

	code, err := generateOpaqueToken()
	if err != nil {
		s.metrics.RecordOAuthFlowFailed(ctx, issuerID, mcpSlug, mcpmetrics.OAuthFlowStageConsent)
		return oops.E(oops.CodeUnexpected, err, "generate authorization code").LogError(ctx, logger)
	}
	if agentAuthorization != nil {
		code = agentAuthorizationCodePrefix + code
	} else if toolPolicy != nil && toolPolicy.Catalog != nil {
		code = catalogAuthorizationCodePrefix + code
	} else if toolPolicy != nil {
		code = gatewayAuthorizationCodePrefix + code
	}

	// The POST may arrive through a global continuation surface. Preserve the
	// original mint-time authority after the preflight above proves it is live.
	grantEndpoint.Authority = challengeState.Endpoint.Authority
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
		Endpoint:                    &grantEndpoint,
		AgentAuthorization:          agentAuthorization,
		DesiredSessionDurationHours: duration,
		ToolSelection:               legacyToolSelection(toolSelection, toolPolicy),
		SignInID:                    nil,
		GatewayPolicy:               toolPolicy,
		UpstreamClientPicks:         upstreamPicks,
		CreatedAt:                   time.Now(),
	}
	// Only a human subject's own session is linked to its sign-in.
	if agentAuthorization == nil && subject.Kind == urn.SessionSubjectKindUser && subject.ID == challengeState.AuthorizerUserID {
		grant.SignInID = challengeState.SignInID
	}
	if err := s.userSessionGrantCache.Store(ctx, grant); err != nil {
		s.metrics.RecordOAuthFlowFailed(ctx, issuerID, mcpSlug, mcpmetrics.OAuthFlowStageConsent)
		return oops.E(oops.CodeUnexpected, err, "store user session grant").LogError(ctx, logger)
	}

	if approval != nil {
		if err := approval.tx.Commit(ctx); err != nil {
			_, _ = s.userSessionGrantCache.GetAndDelete(ctx, grant.CacheKey())
			return oops.E(oops.CodeUnexpected, err, "commit consent approval").LogError(ctx, logger)
		}
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
func (s *Service) validateConsentChallenge(ctx context.Context, endpoint *ResolvedMcpEndpoint, challengeState *AuthnChallengeState, csrfToken string) *oops.ShareableError {
	if err := endpoint.ValidateChallenge(ctx, challengeState.Endpoint, challengeState.UserSessionIssuerID); err != nil {
		if errors.Is(err, networkingress.ErrAuthorityUnavailable) {
			s.metrics.RecordOAuthAuthorityUnavailable(ctx, endpoint.UserSessionIssuerID.String(), endpoint.Slug, mcpmetrics.OAuthFlowStageConsent)
		}
		return oauthAuthorityError(err)
	}
	if challengeState.CSRFToken == "" || subtle.ConstantTimeCompare([]byte(csrfToken), []byte(challengeState.CSRFToken)) != 1 {
		return oops.E(oops.CodeBadRequest, nil, "invalid consent csrf token")
	}
	// First-party challenges have no MCP client to grant to: linking the
	// cards is terminal, so there is no approve/deny POST. The template
	// omits the form; reject any crafted submission rather than falling into
	// the client-grant path with an empty ClientID.
	if challengeState.FirstParty {
		return oops.E(oops.CodeBadRequest, nil, "first-party connect challenges have no approval step")
	}
	if challengeState.Subject == nil || challengeState.Subject.IsZero() {
		return oops.E(oops.CodeBadRequest, nil, "authn challenge subject is not resolved")
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
// connected and each probe-capable connection is verified. The runtime gate
// (remotesessions.ResolveAccessTokens) fails the
// request unless all bound clients have a usable token, so closing after only
// the first of several providers is linked would strand the user mid-flow. A
// challenge with no cards is never auto-closed — there is nothing to complete.
func shouldAutoCloseFirstParty(firstParty bool, cards []remoteSessionCard) bool {
	if !firstParty || len(cards) == 0 {
		return false
	}
	for _, c := range cards {
		// Keep actionable reconnect and verification states visible instead of
		// closing the tab before the person can act on them.
		if !c.Connected || c.IdentityReconnect || c.AttachmentReconnect || c.Rejected || c.Inactive || c.Pending || (c.CanValidate && !c.Verified) {
			return false
		}
	}
	return true
}

// consentDurationPresets are the session-length choices offered on the
// consent page, largest first. The issuer's maximum is inserted when not
// already present, and anything above the maximum is dropped.
var consentDurationPresets = []struct {
	Hours int
	Label string
}{
	{Hours: 90 * 24, Label: "90 days"},
	{Hours: 60 * 24, Label: "60 days"},
	{Hours: 30 * 24, Label: "30 days"},
	{Hours: 14 * 24, Label: "2 weeks"},
	{Hours: 7 * 24, Label: "1 week"},
	{Hours: 3 * 24, Label: "3 days"},
	{Hours: 24, Label: "1 day"},
	{Hours: 12, Label: "12 hours"},
	{Hours: 1, Label: "1 hour"},
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
	maxLifetime, ok := conv.PGIntervalToDuration(issuer.SessionDuration)
	if !ok || maxLifetime <= 0 {
		return nil
	}
	maxHours := int(maxLifetime / time.Hour)
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

// tokenLine is the card's "Token active" line: the introspection answer while
// its deadline (else the stored access expiry) is still ahead. An answer about
// a token that has since expired says nothing about the current one.
func tokenLine(renderedAt time.Time, token *remotesessions.IntrospectedToken, accessExpiresAt *time.Time) (active bool, expiresAt, expiresIn string) {
	if token == nil || !token.Active {
		return false, "", ""
	}
	deadline := token.ExpiresAt
	if deadline.IsZero() && accessExpiresAt != nil {
		deadline = *accessExpiresAt
	}
	if deadline.IsZero() {
		return true, "", ""
	}
	if !deadline.After(renderedAt) {
		return false, "", ""
	}
	return true, deadline.UTC().Format(time.RFC3339), formatTimeRemaining(renderedAt, deadline)
}

// issuerCardBranding resolves the branding a consent card renders for its
// identity provider: a trimmed non-empty name wins, otherwise the identifier
// the page always rendered (the slug). Callers run WithCatalogBranding first,
// so the name and logo may be the platform catalog's; the dashboard's
// formatRemoteSessionIssuerDisplay has no such fallback.
// The resource's own name outranks both, but only when the client recorded
// it for a resource this endpoint fronts (ownResource): a client shared with
// another endpoint must not lend that endpoint's name to this one.
// The logo URL points at the public assets.serveImage endpoint on the
// platform origin, the same construction mcpmetadata uses for MCP server
// logos, and is empty when the issuer has no logo.
func issuerCardBranding(c remotesessions.Client, ownResource bool, serverURL *url.URL) (display, logoURL string) {
	display = c.IssuerSlug
	if name := strings.TrimSpace(conv.PtrValOr(c.IssuerName, "")); name != "" {
		display = name
	}
	if ownResource && c.ResourceName != "" {
		display = c.ResourceName
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

// cardLinks picks the documentation, policy, and terms links a card shows:
// the resource's own (RFC 9728) when the client carries any for a resource
// this endpoint fronts (ownResource), else the authorization server's (RFC 8414).
func cardLinks(c remotesessions.Client, ownResource bool) (documentation, policy, tos string) {
	if ownResource && (c.ResourceDocumentationURL != "" || c.ResourcePolicyURL != "" || c.ResourceTosURL != "") {
		return c.ResourceDocumentationURL, c.ResourcePolicyURL, c.ResourceTosURL
	}
	return c.IssuerDocumentationURL, c.IssuerPolicyURL, c.IssuerTosURL
}

// resourceDisplayOwner decides whether a client recorded its RFC 9728
// display members for a resource this endpoint routes the client's grant
// to: the single proxied server's upstream, or a meta member associated with
// the client. A client that recorded none owns none.
// Meta member rows load once, on first use, and serve every card.
type resourceDisplayOwner struct {
	endpoint *ResolvedMcpEndpoint
	load     func(ctx context.Context) ([]metamcprepo.ListMetaMCPProxiedMemberResourcesRow, error)
	loaded   bool
	// upstreams is each client's associated member resources, keyed by
	// remote_session_client_id.
	upstreams map[uuid.UUID][]metamcprepo.ListMetaMCPProxiedMemberResourcesRow
}

func (s *Service) newResourceDisplayOwner(endpoint *ResolvedMcpEndpoint, challengeState AuthnChallengeState) *resourceDisplayOwner {
	return &resourceDisplayOwner{
		endpoint: endpoint,
		load: func(ctx context.Context) ([]metamcprepo.ListMetaMCPProxiedMemberResourcesRow, error) {
			if endpoint.IsOrganizationGateway() {
				rows, err := metamcprepo.New(s.db).ListOrganizationProxiedMCPServerResources(ctx, endpoint.OrganizationID)
				if err != nil {
					return nil, fmt.Errorf("list organization proxied MCP server resources: %w", err)
				}
				// Only servers the subject reaches lend a card their resource;
				// another server's would say it exists.
				members, err := s.consentSubjectMembers(ctx, endpoint, challengeState)
				if err != nil {
					return nil, err
				}
				admitted := make(map[uuid.UUID]bool, len(members))
				for _, member := range members {
					admitted[member.serverID] = true
				}
				converted := make([]metamcprepo.ListMetaMCPProxiedMemberResourcesRow, 0, len(rows))
				for _, row := range rows {
					if admitted[row.McpServerID] {
						converted = append(converted, metamcprepo.ListMetaMCPProxiedMemberResourcesRow(row))
					}
				}
				return converted, nil
			}
			return metamcprepo.New(s.db).ListMetaMCPProxiedMemberResources(ctx, metamcprepo.ListMetaMCPProxiedMemberResourcesParams{
				MetaMcpServerID: endpoint.MetaMcpServerID.UUID,
				ProjectID:       endpoint.ProjectID,
			})
		},
		loaded:    false,
		upstreams: nil,
	}
}

func (o *resourceDisplayOwner) owns(ctx context.Context, c remotesessions.Client) (bool, error) {
	if c.ResourceIdentifier == "" {
		return false, nil
	}
	if !o.endpoint.MetaMcpServerID.Valid {
		return wellknown.SameResource(c.ResourceIdentifier, o.endpoint.UpstreamResource), nil
	}
	if !o.loaded {
		rows, err := o.load(ctx)
		if err != nil {
			return false, fmt.Errorf("list meta MCP member resources for resource display: %w", err)
		}
		o.upstreams = make(map[uuid.UUID][]metamcprepo.ListMetaMCPProxiedMemberResourcesRow, len(rows))
		for _, row := range rows {
			o.upstreams[row.RemoteSessionClientID] = append(o.upstreams[row.RemoteSessionClientID], row)
		}
		o.loaded = true
	}
	// A client a project owns routes only to its own project's servers, so
	// only they can lend it their resource. A stored gateway's members all
	// share its project.
	return slices.ContainsFunc(o.upstreams[c.ID], func(row metamcprepo.ListMetaMCPProxiedMemberResourcesRow) bool {
		if c.ProjectID.Valid && c.ProjectID.UUID != row.ProjectID {
			return false
		}
		return wellknown.SameResource(c.ResourceIdentifier, row.UpstreamUrl)
	}), nil
}

// ownsOrFallsBack is owns as best effort: a lookup fault logs and the card
// keeps the issuer's own branding rather than failing the page or a verdict.
func (o *resourceDisplayOwner) ownsOrFallsBack(ctx context.Context, logger *slog.Logger, c remotesessions.Client) bool {
	own, err := o.owns(ctx, c)
	if err != nil {
		logger.WarnContext(ctx, "resolve resource display ownership; falling back to issuer branding", attr.SlogError(err))
		return false
	}
	return own
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
	clients, err := s.listEndpointClients(ctx, endpoint, challengeState)
	if err != nil {
		return nil, fmt.Errorf("list remote session clients: %w", err)
	}
	clients = subjectConnectedClients(clients)
	if len(clients) == 0 {
		return nil, nil
	}
	clients = s.remoteChallengeMgr.WithCatalogBranding(ctx, clients)
	// Cards read the cached resource row only; the login probes. The row is
	// the one the login resolves (keyed by the endpoint's server URL, or on a
	// gateway the member the client is qualified to) and, as at login, it
	// decides only for a client whose grant is qualified to that resource. A
	// tunneled or hosted server, or a client whose grant goes elsewhere, is
	// decided by its authorization server alone; a client's own claimed
	// resource never stands in, because no login reads it.
	discoverScopes := s.remoteChallengeMgr.ResourceScopeDiscoveryEnabled()
	var serverResource remotesessions.ResourceScopes
	var serverResourceURL string
	hasServerResource := false
	if discoverScopes {
		serverResource, serverResourceURL, hasServerResource = s.remoteChallengeMgr.CachedResourceScopesForServer(ctx, endpoint.ProjectID, endpoint.McpServerID, true)
	}
	// One load decides ownership for every card. A lookup fault logs and
	// the cards keep their issuers' scopes: the reconnect hint this feeds
	// is best effort and must not fail the page.
	var resourceApplies map[uuid.UUID]bool
	if hasServerResource {
		boundIDs := make([]uuid.UUID, 0, len(clients))
		for i := range clients {
			boundIDs = append(boundIDs, clients[i].ID)
		}
		resourceApplies, err = s.remoteChallengeMgr.ResourceAppliesToClients(ctx, endpoint.OrganizationID, boundIDs, serverResourceURL)
		if err != nil {
			s.logger.WarnContext(ctx, "decide resource ownership for consent cards; falling back to issuer scopes", attr.SlogError(err))
			resourceApplies = nil
		}
	}

	statuses, routing, err := s.consentSessionRouting(ctx, endpoint, challengeState, clients)
	if err != nil {
		return nil, err
	}
	// The member's scopes only feed the reconnect hint, which needs a session.
	connectedClients := make([]remotesessions.Client, 0, len(clients))
	for _, c := range clients {
		if state, ok := statuses[c.ID]; ok && state.Status == remotesessions.RemoteSessionActive {
			connectedClients = append(connectedClients, c)
		}
	}
	memberResources := s.gatewayMemberResourceScopes(ctx, endpoint, challengeState, connectedClients, discoverScopes)
	chainRequests := s.consentChainingRequests(ctx, endpoint, challengeState, clients, routing)

	cards := make([]remoteSessionCard, 0, len(clients))
	renderedAt := time.Now()
	owner := s.newResourceDisplayOwner(endpoint, challengeState)
	for _, c := range clients {
		state, hasSession := statuses[c.ID]
		unroutable := hasSession && state.Status == remotesessions.RemoteSessionActive && routing.unroutable(c, state.Resource)
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
		authorizationExpiresAt := ""
		authorizationExpiresIn := ""
		if state.AuthorizationExpiresAt != nil {
			authorizationExpiresAt = state.AuthorizationExpiresAt.UTC().Format(time.RFC3339)
			authorizationExpiresIn = formatTimeRemaining(renderedAt, *state.AuthorizationExpiresAt)
		}
		ownResource := owner.ownsOrFallsBack(ctx, s.logger, c)
		issuerDisplay, issuerLogoURL := issuerCardBranding(c, ownResource, s.serverURL)
		documentationURL, policyURL, tosURL := cardLinks(c, ownResource)
		validatedAt := ""
		validatedAgo := ""
		if state.LastValidatedAt != nil {
			validatedAt = state.LastValidatedAt.UTC().Format(time.RFC3339)
			validatedAgo = formatTimeAgo(renderedAt, *state.LastValidatedAt)
		}
		// The stored inactive reason embeds the display resolved at probe
		// time; recompose it so it always names the service the title does.
		validationReason := state.ValidationReason
		if state.ValidationStatus == remotesessions.ValidationOutcomeInactive {
			validationReason = inactiveReason(issuerDisplay)
		}
		tokenActive, tokenExpiresAt, tokenExpiresIn := tokenLine(renderedAt, state.Token, state.AccessExpiresAt)
		resourceScopes := remotesessions.ResourceScopes{Pin: nil, ChallengeScopes: nil, ScopesSupported: nil, Live: false, UseDiscovered: discoverScopes}
		if resourceApplies[c.ID] {
			resourceScopes = serverResource
		}
		if member, ok := memberResources[c.ID]; ok {
			resourceScopes = member
		}
		requested := c.RequestedScopes(resourceScopes).Scopes
		connected := hasSession && state.Status == remotesessions.RemoteSessionActive && !unroutable
		chained := len(chainRequests[c.ID]) > 0 && !connected && !unroutable
		chainCheck := ""
		if chained {
			chainCheck = s.consentChainCheckAtRender(ctx, chainRequests[c.ID])
		}
		identityReconnect := connected && !slices.Contains(state.Scopes, "openid") && slices.Contains(requested, "openid")
		requested = slices.Clone(requested)
		slices.Sort(requested)
		permissionHash := consentEvidenceHash(struct {
			ClientID uuid.UUID
			IssuerID uuid.UUID
			Issuer   string
			Resource string
			Scopes   []string
		}{ClientID: c.ID, IssuerID: c.RemoteSessionIssuerID, Issuer: c.IssuerIdentifier, Resource: c.ResourceIdentifier, Scopes: requested})
		reuseReady := connected && state.ValidationStatus != remotesessions.ValidationOutcomeInactive && state.ValidationStatus != remotesessions.ValidationOutcomeRejectedByMember
		for _, scope := range requested {
			reuseReady = reuseReady && slices.Contains(state.Scopes, scope)
		}
		cards = append(cards, remoteSessionCard{
			consentPermissionsHash: permissionHash,
			consentReuseReady:      reuseReady,
			ClientID:               c.ID.String(),
			IssuerSlug:             c.IssuerSlug,
			IssuerDisplay:          issuerDisplay,
			IssuerLogoURL:          issuerLogoURL,
			IssuerDocumentationURL: documentationURL,
			IssuerPolicyURL:        policyURL,
			IssuerTosURL:           tosURL,
			Connected:              connected,
			Expired:                state.Status == remotesessions.RemoteSessionExpired,
			Unroutable:             unroutable,
			Chained:                chained,
			ChainCheck:             chainCheck,
			ChainMessage:           "",
			CanRefresh:             state.CanRefresh,
			IdentityReconnect:      identityReconnect,
			AttachmentReconnect:    false,
			AccessExpiresAt:        accessExpiresAt,
			AccessExpiresIn:        accessExpiresIn,
			RefreshExpiresAt:       refreshExpiresAt,
			RefreshExpiresIn:       refreshExpiresIn,
			AuthorizationExpiresAt: authorizationExpiresAt,
			AuthorizationExpiresIn: authorizationExpiresIn,
			AutoRefreshChecked:     checked,
			ConnectedAs:            state.ConnectedAs,
			AccountChips:           state.AccountChips,
			IdentityCaveat:         state.IdentityCaveat,
			TokenActive:            tokenActive,
			TokenExpiresAt:         tokenExpiresAt,
			TokenExpiresIn:         tokenExpiresIn,
			Verified:               state.ValidationStatus == remotesessions.ValidationOutcomeValid,
			Rejected:               state.ValidationStatus == remotesessions.ValidationOutcomeRejectedByMember,
			Inactive:               state.ValidationStatus == remotesessions.ValidationOutcomeInactive,
			Unverified:             state.ValidationStatus == remotesessions.ValidationOutcomeUnknown,
			ValidatedAt:            validatedAt,
			ValidatedAgo:           validatedAgo,
			ValidationReason:       validationReason,
			ValidationNotice:       "",
			CanValidate:            routing.canValidate(c, state.Resource),
			Pending:                false,
			SharedServerNote:       "",
			remoteSessionIssuerID:  c.RemoteSessionIssuerID,
			projectID:              c.ProjectID,
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

// formatTimeAgo renders how long ago at was at minute resolution; a future at (clock skew) reads as just now.
func formatTimeAgo(now, at time.Time) string {
	if !at.Before(now) || now.Sub(at) < time.Minute {
		return "just now"
	}
	return formatTimeRemaining(at, now) + " ago"
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
	focusServer string,
) (bool, error) {
	if challengeState.AutoConnectDone || len(cards) != 1 || cards[0].Connected || cards[0].Chained {
		return false, nil
	}

	bound, err := s.listEndpointClients(ctx, endpoint, challengeState)
	if err != nil {
		return false, oops.E(oops.CodeUnexpected, err, "list remote session clients").LogError(ctx, logger)
	}
	clients := subjectConnectedClients(bound)
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
	challengeURL, hop, err := s.buildRemoteConnectURL(ctx, logger, endpoint, challengeState, *client, bound, nil, focusServer)
	if err != nil {
		// Already logged. Render the page so the user can connect manually
		// rather than seeing an error for a step they did not take.
		return false, nil
	}
	if hop {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Referrer-Policy", "no-referrer")
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

// sharedConsentPath is the consent page path on the shared authorization
// server serving the endpoint's OAuth request, or "" when its own
// authorization server is serving it.
func sharedConsentPath(endpoint *ResolvedMcpEndpoint) string {
	if shared := endpoint.servingSharedAuthorizationServer(); shared != nil {
		return shared.consentPath()
	}
	return ""
}
