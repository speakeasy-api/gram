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
	"net/http"
	"net/url"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
	"github.com/speakeasy-api/gram/server/internal/urn"
	users_repo "github.com/speakeasy-api/gram/server/internal/users/repo"
	usersessions_repo "github.com/speakeasy-api/gram/server/internal/usersessions/repo"
)

//go:embed consent_template.html
var consentTemplateHTML string

var consentTemplate = template.Must(template.New("consent").Parse(consentTemplateHTML))

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
	// AutoClose marks a fully completed first-party connection: every bound
	// remote_session_client is connected. The consent script closes only this
	// terminal state; partially-linked connections (some cards still
	// disconnected) and MCP client consent remain open.
	AutoClose bool
}

// remoteSessionCard is the per-remote view rendered by the {{range}} block
// in the consent template. ChallengeURL is the upstream provider's
// authorize URL with PKCE + state bound for this consent session.
//
// Connected and Expired are mutually exclusive and reflect the stored
// remote_session's usability: Connected means the runtime gate will accept
// it; Expired means a stale link exists that must be re-established; both
// false means never connected. Only Connected enables consent — an expired
// link is no better than none until the user reconnects.
type remoteSessionCard struct {
	ClientID     string
	IssuerSlug   string
	Connected    bool
	Expired      bool
	ChallengeURL string
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
	if !challengeState.FirstParty {
		client, err := usersessions_repo.New(s.db).GetUserSessionClientByClientID(ctx, usersessions_repo.GetUserSessionClientByClientIDParams{
			UserSessionIssuerID: endpoint.UserSessionIssuerID,
			ClientID:            challengeState.ClientID,
		})
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return oops.E(oops.CodeUnauthorized, err, "user session client revoked").LogError(ctx, logger)
			}
			return oops.E(oops.CodeUnexpected, err, "lookup user session client").LogError(ctx, logger)
		}
		clientName = client.ClientName
	}

	if challengeState.Subject == nil || challengeState.Subject.IsZero() {
		return oops.E(oops.CodeUnauthorized, nil, "authn challenge subject is not resolved").LogError(ctx, logger)
	}

	subjectDisplay := resolveSubjectDisplay(ctx, s.db, *challengeState.Subject)

	cards, err := s.buildRemoteSessionCards(ctx, endpoint, challengeState)
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "build remote session cards").LogError(ctx, logger)
	}

	hasConnectedCard := false
	for _, c := range cards {
		if c.Connected {
			hasConnectedCard = true
			break
		}
	}
	consentEnabled := len(cards) == 0 || hasConnectedCard

	data := consentTemplateData{
		ClientName:         clientName,
		MCPSlug:            endpoint.Slug,
		MCPRouteBase:       endpoint.RouteBase,
		State:              stateID,
		CSRFToken:          challengeState.CSRFToken,
		SubjectDisplay:     subjectDisplay,
		RedirectURI:        challengeState.RedirectURI,
		ScriptURL:          consentScriptURL,
		RemoteSessionCards: cards,
		ConsentEnabled:     consentEnabled,
		FirstParty:         challengeState.FirstParty,
		AutoClose:          shouldAutoCloseFirstParty(challengeState.FirstParty, cards),
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
	// consent form has a few short fields; 16 KiB is generous.
	r.Body = http.MaxBytesReader(w, r.Body, 16<<10)
	if err := r.ParseForm(); err != nil {
		return oops.E(oops.CodeBadRequest, err, "failed to parse form").LogError(ctx, s.logger)
	}

	logger := endpoint.LogWith(s.logger)

	stateID := r.PostForm.Get("state")
	if stateID == "" {
		return oops.E(oops.CodeBadRequest, nil, "state is required").LogError(ctx, logger)
	}

	// Atomic GETDEL: a consent POST consumes the authn-challenge state
	// single-use. Parallel POSTs (e.g. user double-submits) lose the race
	// and get "not found or expired", so only one grant is ever minted per
	// authorization request.
	challengeState, err := s.authnChallengeCache.GetAndDelete(ctx, "authnChallenge:"+stateID)
	if err != nil {
		return oops.E(oops.CodeUnauthorized, err, "authn challenge state not found or expired").LogError(ctx, logger)
	}
	logger = logger.With(attr.SlogOAuthFlowID(challengeState.FlowID))
	issuerID := endpoint.UserSessionIssuerID.String()
	mcpSlug := endpoint.Slug

	// The guards below (state-confusion ref check, CSRF, and the unknown-action
	// default) consume the challenge but are deliberately NOT counted as flow
	// failures: they are attacker-controllable, so emitting `failed` here would
	// let crafted requests pollute a config's health signal. A legitimate user
	// never trips them; the rare case lands in the started-without-terminal gap.
	if err := endpoint.ValidateRef(challengeState.Endpoint); err != nil {
		return oops.E(oops.CodeUnauthorized, err, "authn challenge state does not match this MCP server").LogError(ctx, logger)
	}

	if challengeState.CSRFToken == "" || subtle.ConstantTimeCompare([]byte(r.PostForm.Get("csrf_token")), []byte(challengeState.CSRFToken)) != 1 {
		return oops.E(oops.CodeUnauthorized, nil, "invalid consent csrf token").LogError(ctx, logger)
	}

	// First-party challenges have no MCP client to grant to: linking the cards
	// is terminal, so there is no approve/deny POST. The template omits the
	// form; reject any crafted submission rather than falling into the
	// client-grant path with an empty ClientID.
	if challengeState.FirstParty {
		return oops.E(oops.CodeBadRequest, nil, "first-party connect challenges have no approval step").LogError(ctx, logger)
	}

	// Explicit action required: fail closed on missing / unknown values so
	// a malformed form post can't trigger the approval path.
	action := r.PostForm.Get("action")
	switch action {
	case "approve":
		// fall through
	case "deny":
		// Cancel: 303 (POST → GET) the MCP client back to its redirect_uri
		// with access_denied per RFC 6749 §4.1.2.1, preserving the original
		// state. The user reached the consent screen and chose "no" — a
		// decline, not an errant config.
		s.metrics.RecordOAuthFlowDeclined(ctx, issuerID, mcpSlug, oauthFlowStageConsent)
		logger.InfoContext(ctx, "oauth flow declined at consent", attr.SlogOAuthError("access_denied"))
		denyURL := buildClientRedirect(challengeState.RedirectURI, "", challengeState.State, "access_denied", "user denied consent")
		http.Redirect(w, r, denyURL, http.StatusSeeOther)
		return nil
	default:
		return oops.E(oops.CodeBadRequest, nil, `action must be "approve" or "deny"`).LogError(ctx, logger)
	}

	if challengeState.Subject == nil || challengeState.Subject.IsZero() {
		// Reaching an approved consent POST with no resolved subject is a code
		// invariant break, not a user action — a config/code-class failure.
		s.metrics.RecordOAuthFlowFailed(ctx, issuerID, mcpSlug, oauthFlowStageConsent)
		return oops.E(oops.CodeUnauthorized, nil, "authn challenge subject is not resolved").LogError(ctx, logger)
	}
	subject := *challengeState.Subject

	// Resolve the user_session_clients row id for the consent FK.
	clientRow, err := usersessions_repo.New(s.db).GetUserSessionClientByClientID(ctx, usersessions_repo.GetUserSessionClientByClientIDParams{
		UserSessionIssuerID: endpoint.UserSessionIssuerID,
		ClientID:            challengeState.ClientID,
	})
	if err != nil {
		// Client revoked mid-flow (config change) or DB error — either way the
		// approved flow can't complete.
		s.metrics.RecordOAuthFlowFailed(ctx, issuerID, mcpSlug, oauthFlowStageConsent)
		if errors.Is(err, pgx.ErrNoRows) {
			return oops.E(oops.CodeUnauthorized, err, "user session client revoked").LogError(ctx, logger)
		}
		return oops.E(oops.CodeUnexpected, err, "lookup user session client").LogError(ctx, logger)
	}

	// Persist the consent record. The unique index on
	// (principal_urn, user_session_client_id, remote_set_hash) makes this
	// idempotent on re-consent for the same set; we treat the duplicate-key
	// error as a no-op (consent already on file).
	if _, err := usersessions_repo.New(s.db).CreateUserSessionConsent(ctx, usersessions_repo.CreateUserSessionConsentParams{
		SubjectUrn:          subject,
		UserSessionClientID: clientRow.ID,
		RemoteSetHash:       remoteSetHashEmpty,
	}); err != nil && !isUniqueViolation(err) {
		s.metrics.RecordOAuthFlowFailed(ctx, issuerID, mcpSlug, oauthFlowStageConsent)
		return oops.E(oops.CodeUnexpected, err, "record consent").LogError(ctx, logger)
	}

	code, err := generateOpaqueToken()
	if err != nil {
		s.metrics.RecordOAuthFlowFailed(ctx, issuerID, mcpSlug, oauthFlowStageConsent)
		return oops.E(oops.CodeUnexpected, err, "generate authorization code").LogError(ctx, logger)
	}

	grant := UserSessionGrant{
		Code:                code,
		FlowID:              challengeState.FlowID,
		UserSessionIssuerID: endpoint.UserSessionIssuerID,
		UserSessionClientID: clientRow.ID,
		ClientID:            challengeState.ClientID,
		RedirectURI:         challengeState.RedirectURI,
		CodeChallenge:       challengeState.CodeChallenge,
		CodeChallengeMethod: challengeState.CodeChallengeMethod,
		Subject:             subject,
		CreatedAt:           time.Now(),
	}
	if err := s.userSessionGrantCache.Store(ctx, grant); err != nil {
		s.metrics.RecordOAuthFlowFailed(ctx, issuerID, mcpSlug, oauthFlowStageConsent)
		return oops.E(oops.CodeUnexpected, err, "store user session grant").LogError(ctx, logger)
	}

	clientRedirect := buildClientRedirect(challengeState.RedirectURI, code, challengeState.State, "", "")
	// 303 See Other (POST → GET): the consent submit is a POST; we want
	// the user agent to GET the redirect target with NO body re-submission.
	http.Redirect(w, r, clientRedirect, http.StatusSeeOther)
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

// buildClientRedirect produces the URL to redirect the MCP client to,
// preserving any prior query string on redirectURI and adding `code` (success)
// or `error` / `error_description` (failure) plus the original `state`.
func buildClientRedirect(redirectURI, code, originalState, errCode, errDescription string) string {
	u, err := url.Parse(redirectURI)
	if err != nil {
		// Should never happen — redirect_uri was validated at HandleAuthorize
		// time. Fall back to a best-effort string concatenation.
		return redirectURI
	}
	q := u.Query()
	if code != "" {
		q.Set("code", code)
	}
	if errCode != "" {
		q.Set("error", errCode)
		if errDescription != "" {
			q.Set("error_description", errDescription)
		}
	}
	if originalState != "" {
		q.Set("state", originalState)
	}
	u.RawQuery = q.Encode()
	return u.String()
}

// isUniqueViolation reports whether err is a Postgres unique-constraint
// violation. Used to detect duplicate consent inserts (idempotent re-consent).
func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
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
	var statuses map[uuid.UUID]remotesessions.RemoteSessionStatus
	if challengeState.Subject != nil && !challengeState.Subject.IsZero() {
		statuses, err = s.remoteChallengeMgr.RemoteSessionStatuses(ctx, *challengeState.Subject, endpoint.UserSessionIssuerID)
		if err != nil {
			return nil, fmt.Errorf("remote session statuses: %w", err)
		}
	}

	parent := remotesessions.ParentChallenge{
		ID:                  challengeState.ID,
		ProjectID:           endpoint.ProjectID,
		OrganizationID:      endpoint.OrganizationID,
		UserSessionIssuerID: endpoint.UserSessionIssuerID,
		Subject:             challengeState.Subject,
		McpSlug:             endpoint.Slug,
		RouteBase:           endpoint.RouteBase,
		FinalRedirectURI:    "",
		Resource:            endpoint.UpstreamResource,
	}

	cards := make([]remoteSessionCard, 0, len(clients))
	for _, c := range clients {
		challengeURL, berr := s.remoteChallengeMgr.BuildAuthorizationUrl(ctx, parent, c)
		if berr != nil {
			return nil, fmt.Errorf("build authorization url for %s: %w", c.IssuerSlug, berr)
		}
		status := statuses[c.ID]
		cards = append(cards, remoteSessionCard{
			ClientID:     c.ID.String(),
			IssuerSlug:   c.IssuerSlug,
			Connected:    status == remotesessions.RemoteSessionActive,
			Expired:      status == remotesessions.RemoteSessionExpired,
			ChallengeURL: challengeURL,
		})
	}
	return cards, nil
}
