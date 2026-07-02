// ChallengeManager drives the per-remote OAuth authn-challenge leg of the
// MCP user-session flow. Two entry points split by direction:
//
//   - BuildAuthorizationUrl is called by the user-session consent renderer
//     (mcp/authnchallenge_consent.go). Given the in-flight parent challenge
//     and the picked remote_session_client, it mints a RemoteLoginState in
//     Redis (carrying PKCE + parent binding) and returns the authorize URL
//     to redirect the user to.
//   - HandleRemoteLoginCallback is bound to `GET /mcp/remote_login_callback`.
//     Reads ?code+?state, validates state, exchanges code for tokens at the
//     upstream token endpoint, encrypts and persists the remote_sessions
//     row, then redirects back to /mcp/{slug}/connect with the parent
//     challenge id so the consent page re-renders with this remote ✓.
//
// AuthnChallengeState reuse: the parent challenge passed to
// BuildAuthorizationUrl is the same Redis-backed state minted at
// /authorize — its ID is the unambiguous binding back to the right /connect
// render after a user round-trips through the upstream provider. mcp/
// builds a ParentChallenge value from its AuthnChallengeState; this package
// never imports mcp/.

package remotesessions

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/encryption"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/remotesessions/interceptors"
	remotesessions_repo "github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// ParentChallenge projects the in-flight user-session AuthnChallengeState
// into the fields the remote leg needs. mcp/ builds this from its
// AuthnChallengeState; remotesessions/ never imports mcp/.
//
// FinalRedirectURI is set by callers that own their own redirect surface
// (e.g. the dashboard's issuer-connect endpoint, which bypasses the consent
// UI). When non-empty, HandleRemoteLoginCallback redirects there after the
// upstream token exchange instead of bouncing back to
// /<RouteBase>/{slug}/connect.
//
// RouteBase is "mcp" or "x/mcp" — the surface the parent challenge was
// minted under. Drives both the upstream provider's redirect_uri
// (/<RouteBase>/remote_login_callback) and the post-callback bounce to
// /<RouteBase>/{slug}/connect. Empty values fall back to "mcp" so
// in-flight states minted before this field landed still resume on the
// original surface.
type ParentChallenge struct {
	ID                  string
	ProjectID           uuid.UUID
	OrganizationID      string
	UserSessionIssuerID uuid.UUID
	Subject             *urn.SessionSubject
	McpSlug             string
	RouteBase           string
	FinalRedirectURI    string
}

// RemoteLoginState is the per-remote-leg Redis state, keyed by the opaque
// `state` parameter sent to the upstream provider. ~10 minute TTL — same
// budget as the parent AuthnChallengeState.
type RemoteLoginState struct {
	ID                string    `json:"id"`
	ParentChallengeID string    `json:"parent_challenge_id"`
	ProjectID         uuid.UUID `json:"project_id"`
	// OrganizationID scopes the callback's client lookup so an organization-level
	// client (project_id NULL) bound to this project's user_session_issuer
	// resolves on the way back. Empty for in-flight states minted before it.
	OrganizationID        string              `json:"organization_id,omitempty"`
	UserSessionIssuerID   uuid.UUID           `json:"user_session_issuer_id"`
	RemoteSessionClientID uuid.UUID           `json:"remote_session_client_id"`
	TokenEndpoint         string              `json:"token_endpoint"`
	RedirectURI           string              `json:"redirect_uri"`
	CodeVerifier          string              `json:"code_verifier"`
	Subject               *urn.SessionSubject `json:"subject,omitempty"`
	McpSlug               string              `json:"mcp_slug"`
	// RouteBase is "mcp" or "x/mcp" — drives the post-callback redirect
	// to /<RouteBase>/{slug}/connect. Empty values fall back to "mcp"
	// for in-flight states minted before this field landed.
	RouteBase string `json:"route_base,omitempty"`
	// FinalRedirectURI overrides the default post-callback redirect to
	// /<RouteBase>/{slug}/connect. Set by dashboard-driven flows that
	// own their own popup-close surface (validated against an allow-list
	// before it lands here).
	FinalRedirectURI string    `json:"final_redirect_uri,omitempty"`
	CreatedAt        time.Time `json:"created_at"`
}

var _ cache.CacheableObject[RemoteLoginState] = (*RemoteLoginState)(nil)

func (s RemoteLoginState) CacheKey() string              { return "remoteLogin:" + s.ID }
func (s RemoteLoginState) AdditionalCacheKeys() []string { return []string{} }
func (s RemoteLoginState) TTL() time.Duration            { return 10 * time.Minute }

// ChallengeManager drives the per-remote OAuth authn-challenge leg.
type ChallengeManager struct {
	logger    *slog.Logger
	db        *pgxpool.Pool
	enc       *encryption.Client
	policy    *guardian.Policy
	cache     cache.TypedCacheObject[RemoteLoginState]
	serverURL *url.URL
	// authorizeInterceptors adapt the outgoing upstream authorize request to
	// per-provider, non-standard requirements (e.g. Google's offline access).
	// Injected here rather than via a package-global registry.
	authorizeInterceptors []interceptors.AuthorizeInterceptor
}

func NewChallengeManager(
	logger *slog.Logger,
	db *pgxpool.Pool,
	enc *encryption.Client,
	policy *guardian.Policy,
	cacheImpl cache.Cache,
	serverURL *url.URL,
) *ChallengeManager {
	logger = logger.With(attr.SlogComponent("remotesessions_challenge"))
	return &ChallengeManager{
		logger: logger,
		db:     db,
		enc:    enc,
		policy: policy,
		cache: cache.NewTypedObjectCache[RemoteLoginState](
			logger.With(attr.SlogCacheNamespace("remote_login")),
			cacheImpl,
			cache.SuffixNone,
		),
		serverURL: serverURL,
		authorizeInterceptors: []interceptors.AuthorizeInterceptor{
			interceptors.NewGoogle(logger),
		},
	}
}

// Client is the joined view of a remote_session_client + its
// remote_session_issuer used by BuildAuthorizationUrl and the consent
// renderer. Kept as a package value type so mcp/ can pass it back to us
// without re-querying.
type Client struct {
	ID                    uuid.UUID
	ExternalClientID      string
	ClientSecretEncrypted *string
	IssuerSlug            string
	IssuerURL             string
	AuthorizationEndpoint string
	TokenEndpoint         string
	// ClientScope, when non-empty, overrides IssuerScopesSupported in the
	// OAuth dance.
	ClientScope           []string
	IssuerScopesSupported []string
	Audience              string
	// Resource, when non-empty, is sent as the RFC 8707 resource parameter
	// on the authorize redirect and code exchange so the upstream AS
	// audience-binds the issued tokens to the MCP server.
	Resource    string
	Passthrough bool
	// LegacyCallbackUrl flips BuildAuthorizationUrl onto the
	// /oauth/callback redirect_uri (with a JSON state carrying
	// remote_sessions=true) so a client registered against the old
	// oauth_proxy_servers URL keeps working without re-registration.
	LegacyCallbackUrl bool
}

func (c Client) resolveScopes() []string {
	if len(c.ClientScope) > 0 {
		return c.ClientScope
	}
	return c.IssuerScopesSupported
}

// ListClients returns the joined client + issuer rows linked to a user
// session issuer. Used by the consent renderer to materialise the
// per-remote cards.
func (m *ChallengeManager) ListClients(
	ctx context.Context,
	projectID uuid.UUID,
	organizationID string,
	userSessionIssuerID uuid.UUID,
) ([]Client, error) {
	rows, err := m.listRemoteSessionClientRowsForUserSessionIssuer(ctx, projectID, organizationID, userSessionIssuerID)
	if err != nil {
		return nil, fmt.Errorf("list remote session clients: %w", err)
	}
	out := make([]Client, 0, len(rows))
	for _, r := range rows {
		out = append(out, Client{
			ID:                    r.ClientID,
			ExternalClientID:      r.ExternalClientID,
			ClientSecretEncrypted: conv.FromPGText[string](r.ClientSecretEncrypted),
			IssuerSlug:            r.IssuerSlug,
			IssuerURL:             r.IssuerUrl,
			AuthorizationEndpoint: conv.PtrValOr(conv.FromPGText[string](r.AuthorizationEndpoint), ""),
			TokenEndpoint:         conv.PtrValOr(conv.FromPGText[string](r.TokenEndpoint), ""),
			ClientScope:           r.ClientScope,
			IssuerScopesSupported: r.ScopesSupported,
			Audience:              conv.FromPGTextOrEmpty[string](r.ClientAudience),
			Resource:              conv.FromPGTextOrEmpty[string](r.ClientResource),
			Passthrough:           r.Passthrough,
			LegacyCallbackUrl:     r.LegacyCallbackUrl,
		})
	}
	return out, nil
}

// RemoteSessionStatus is the usability of a subject's stored remote_session
// for a single client, as surfaced to the consent renderer. A client with no
// non-deleted remote_session is absent from the map entirely (disconnected);
// only present rows carry a status.
type RemoteSessionStatus string

const (
	// RemoteSessionActive: the access token is unexpired, or a refresh token
	// exists to renew it — the runtime gate will accept it.
	RemoteSessionActive RemoteSessionStatus = "active"
	// RemoteSessionExpired: the row exists but the access token has expired
	// with no refresh token, so the runtime gate rejects it
	// (ErrNoValidToken). The user must re-link to recover.
	RemoteSessionExpired RemoteSessionStatus = "expired"
)

// RemoteSessionStatuses returns, per remote_session_client_id, the usability
// status of `subject`'s remote_session under the given `userSessionIssuerID`.
// Clients with no non-deleted session are omitted (disconnected). Single
// round-trip; the caller (consent renderer) then does O(1) lookups per card.
// Returns an empty map for zero subjects so anonymous-pre-stamp renders are
// no-ops.
func (m *ChallengeManager) RemoteSessionStatuses(
	ctx context.Context,
	subject urn.SessionSubject,
	userSessionIssuerID uuid.UUID,
) (map[uuid.UUID]RemoteSessionStatus, error) {
	if subject.IsZero() {
		return map[uuid.UUID]RemoteSessionStatus{}, nil
	}
	rows, err := remotesessions_repo.New(m.db).ListRemoteSessionStatusesForSubject(ctx, remotesessions_repo.ListRemoteSessionStatusesForSubjectParams{
		SubjectUrn:          subject,
		UserSessionIssuerID: userSessionIssuerID,
	})
	if err != nil {
		return nil, fmt.Errorf("list remote session statuses: %w", err)
	}
	statuses := make(map[uuid.UUID]RemoteSessionStatus, len(rows))
	for _, row := range rows {
		statuses[row.RemoteSessionClientID] = RemoteSessionStatus(row.Status)
	}
	return statuses, nil
}

// BuildAuthorizationUrl mints a RemoteLoginState (with PKCE S256) for the
// (parent, client) pair, stores it in Redis, and returns the upstream
// authorize URL with bound `state` + `code_challenge` query params. The
// caller is the consent renderer; this is called once per visible card.
func (m *ChallengeManager) BuildAuthorizationUrl(
	ctx context.Context,
	parent ParentChallenge,
	client Client,
) (string, error) {
	if client.AuthorizationEndpoint == "" {
		return "", fmt.Errorf("remote_session_issuer %s missing authorization_endpoint", client.IssuerSlug)
	}
	if client.TokenEndpoint == "" {
		return "", fmt.Errorf("remote_session_issuer %s missing token_endpoint", client.IssuerSlug)
	}

	stateID, err := randomToken(32)
	if err != nil {
		return "", fmt.Errorf("generate state: %w", err)
	}
	verifier, err := randomToken(32)
	if err != nil {
		return "", fmt.Errorf("generate code verifier: %w", err)
	}
	codeChallenge := s256Challenge(verifier)
	redirectURI := m.callbackURL(canonicalCallbackRouteBase)
	stateParam := stateID
	if client.LegacyCallbackUrl {
		// Upstream was registered against the legacy oauth_proxy_servers
		// callback. Keep that exact redirect_uri so the upstream's
		// strict-match check still passes, and wrap the state in a JSON
		// envelope tagged remote_sessions=true so /oauth/callback can tell
		// this response apart from a true proxy callback and forward to
		// /mcp/remote_login_callback.
		redirectURI = m.legacyCallbackURL()
		envelope, eerr := json.Marshal(map[string]string{
			"remote_sessions": "true",
			"state_id":        stateID,
		})
		if eerr != nil {
			return "", fmt.Errorf("marshal legacy state envelope: %w", eerr)
		}
		stateParam = string(envelope)
	}

	// Parse the upstream authorize URL before the cache write so a malformed
	// endpoint can't leave an orphaned RemoteLoginState in Redis (its key is
	// keyed on the random stateID — nothing else can reach it to clean up,
	// it just expires after TTL).
	u, err := url.Parse(client.AuthorizationEndpoint)
	if err != nil {
		return "", fmt.Errorf("parse authorization_endpoint: %w", err)
	}

	state := RemoteLoginState{
		ID:                    stateID,
		ParentChallengeID:     parent.ID,
		ProjectID:             parent.ProjectID,
		OrganizationID:        parent.OrganizationID,
		UserSessionIssuerID:   parent.UserSessionIssuerID,
		RemoteSessionClientID: client.ID,
		TokenEndpoint:         client.TokenEndpoint,
		RedirectURI:           redirectURI,
		CodeVerifier:          verifier,
		Subject:               parent.Subject,
		McpSlug:               parent.McpSlug,
		RouteBase:             parent.RouteBase,
		FinalRedirectURI:      parent.FinalRedirectURI,
		CreatedAt:             time.Now(),
	}
	if err := m.cache.Store(ctx, state); err != nil {
		return "", fmt.Errorf("store remote login state: %w", err)
	}

	q := u.Query()
	q.Set("response_type", "code")
	q.Set("client_id", client.ExternalClientID)
	q.Set("redirect_uri", redirectURI)
	q.Set("state", stateParam)
	q.Set("code_challenge", codeChallenge)
	q.Set("code_challenge_method", "S256")
	if scopes := client.resolveScopes(); len(scopes) > 0 {
		q.Set("scope", strings.Join(scopes, " "))
	}
	if client.Audience != "" {
		q.Set("audience", client.Audience)
	}
	if client.Resource != "" {
		q.Set("resource", client.Resource)
	}
	for _, ic := range m.authorizeInterceptors {
		if ic.Match(client.IssuerURL) {
			ic.ModifyAuthorize(ctx, q)
		}
	}
	u.RawQuery = q.Encode()
	return u.String(), nil
}

// HandleRemoteLoginCallback is the GET handler for
// /mcp/remote_login_callback. Bound by mcp/ at route-mount time. The legacy
// /mcp/{mcpSlug}/remote_login_callback route is still accepted, but the MCP
// slug is resolved from the stored RemoteLoginState.
// Coordinates code → token exchange at the upstream token endpoint and
// persists the result in remote_sessions; on success redirects back to
// /mcp/{slug}/connect?state={parent_challenge_id}.
func (m *ChallengeManager) HandleRemoteLoginCallback(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	routeMcpSlug := chi.URLParam(r, "mcpSlug")
	logger := m.logger

	q := r.URL.Query()
	if errCode := q.Get("error"); errCode != "" {
		return oops.E(oops.CodeUnauthorized, nil, "remote authn challenge denied: %s", errCode).LogWarn(ctx, logger,
			attr.SlogOAuthError(errCode),
			attr.SlogOAuthErrorDescription(q.Get("error_description")),
		)
	}
	stateID := q.Get("state")
	if stateID == "" {
		return oops.E(oops.CodeBadRequest, nil, "state is required").LogError(ctx, logger)
	}
	code := q.Get("code")
	if code == "" {
		return oops.E(oops.CodeBadRequest, nil, "code is required").LogError(ctx, logger)
	}

	// Single-use state: GETDEL so a duplicate callback can't double-exchange
	// the code. The upstream code itself is also single-use, but defense in
	// depth keeps the failure mode obvious.
	state, err := m.cache.GetAndDelete(ctx, "remoteLogin:"+stateID)
	if err != nil {
		return oops.E(oops.CodeUnauthorized, err, "remote login state not found or expired").LogError(ctx, logger)
	}
	mcpSlug := state.McpSlug
	if mcpSlug == "" {
		mcpSlug = routeMcpSlug
	}
	if mcpSlug == "" {
		return oops.E(oops.CodeBadRequest, nil, "mcp slug is missing from remote login state").LogError(ctx, logger)
	}
	if routeMcpSlug != "" && routeMcpSlug != mcpSlug {
		return oops.E(oops.CodeUnauthorized, nil, "remote login state does not match this MCP server").LogError(ctx, logger)
	}
	if state.McpSlug != "" && state.McpSlug != mcpSlug {
		return oops.E(oops.CodeUnauthorized, nil, "remote login state does not match this MCP server").LogError(ctx, logger)
	}

	logger = logger.With(
		attr.SlogToolsetMCPSlug(mcpSlug),
		attr.SlogProjectID(state.ProjectID.String()),
	)

	// Hoisted above the DB lookup + upstream code exchange so a state with a
	// missing/zero Subject fails fast — otherwise we burn the single-use
	// upstream authorization code on a request that can't produce a
	// remote_sessions row anyway.
	if state.Subject == nil || state.Subject.IsZero() {
		return oops.E(oops.CodeUnauthorized, nil, "remote login requires a stamped subject on the parent challenge").LogError(ctx, logger)
	}

	queries := remotesessions_repo.New(m.db)
	clientRow, err := queries.GetRemoteSessionClientByID(ctx, remotesessions_repo.GetRemoteSessionClientByIDParams{
		ID:             state.RemoteSessionClientID,
		ProjectID:      state.ProjectID,
		OrganizationID: conv.ToPGText(state.OrganizationID),
	})
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "load remote session client").LogError(ctx, logger)
	}
	client := clientRow.RemoteSessionClient

	var clientSecret string
	if client.ClientSecretEncrypted.Valid {
		decoded, derr := m.enc.Decrypt(client.ClientSecretEncrypted.String)
		if derr != nil {
			return oops.E(oops.CodeUnexpected, derr, "decrypt client secret").LogError(ctx, logger)
		}
		clientSecret = decoded
	}

	authMethod := ResolveTokenEndpointAuthMethod(client.TokenEndpointAuthMethod.String)
	audience := conv.FromPGTextOrEmpty[string](client.Audience)
	resource := conv.FromPGTextOrEmpty[string](client.Resource)
	tok, err := m.exchangeCode(ctx, state, client.ClientID, clientSecret, authMethod, audience, resource, code)
	if err != nil {
		return oops.E(oops.CodeUnauthorized, err, "upstream token exchange failed").LogError(ctx, logger)
	}

	accessEnc, err := m.enc.Encrypt([]byte(tok.AccessToken))
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "encrypt access token").LogError(ctx, logger)
	}
	var refreshEnc *string
	if tok.RefreshToken != "" {
		v, eerr := m.enc.Encrypt([]byte(tok.RefreshToken))
		if eerr != nil {
			return oops.E(oops.CodeUnexpected, eerr, "encrypt refresh token").LogError(ctx, logger)
		}
		refreshEnc = &v
	}

	// expires_in is OPTIONAL per RFC 6749 §5.1. When the upstream omits it we
	// store NULL — "no known expiry" — rather than fabricating a deadline the
	// provider never asserted. validateAndRefresh then decides what NULL means
	// from the refresh token: non-expiring when none was issued (e.g. Slack
	// non-rotating xoxp), or an hourly app-layer refresh cadence when one was.
	var accessExpires *time.Time
	if tok.ExpiresIn > 0 {
		v := time.Now().Add(time.Duration(tok.ExpiresIn) * time.Second)
		accessExpires = &v
	}
	var refreshExpires *time.Time
	if tok.RefreshExpiresIn > 0 {
		v := time.Now().Add(time.Duration(tok.RefreshExpiresIn) * time.Second)
		refreshExpires = &v
	}

	scopes := tok.Scopes()
	if scopes == nil {
		scopes = []string{}
	}
	if _, err := queries.UpsertRemoteSession(ctx, remotesessions_repo.UpsertRemoteSessionParams{
		SubjectUrn:            *state.Subject,
		UserSessionIssuerID:   state.UserSessionIssuerID,
		RemoteSessionClientID: state.RemoteSessionClientID,
		AccessTokenEncrypted:  accessEnc,
		AccessExpiresAt:       conv.PtrToPGTimestamptz(accessExpires),
		RefreshTokenEncrypted: conv.PtrToPGText(refreshEnc),
		RefreshExpiresAt:      conv.PtrToPGTimestamptz(refreshExpires),
		Scopes:                scopes,
	}); err != nil {
		return oops.E(oops.CodeUnexpected, err, "store remote session").LogError(ctx, logger)
	}

	routeBase := state.RouteBase
	if routeBase == "" {
		routeBase = "mcp"
	}
	redirect := fmt.Sprintf("%s/%s/%s/connect?state=%s", strings.TrimRight(m.serverURL.String(), "/"), routeBase, mcpSlug, url.QueryEscape(state.ParentChallengeID))
	if state.FinalRedirectURI != "" {
		redirect = state.FinalRedirectURI
	}
	http.Redirect(w, r, redirect, http.StatusSeeOther)
	return nil
}

// canonicalCallbackRouteBase is the route base the outbound remote-login
// redirect_uri uses. remote_login_callback is mounted slug-less under both /mcp
// and /x/mcp and recovers the originating slug from the cached login state, so
// one canonical base serves either surface. A single stable redirect_uri also
// matches the lone redirect_uri a CIMD client publishes in its metadata
// document; the originating surface lives in the login state's RouteBase for
// the post-callback bounce.
const canonicalCallbackRouteBase = "mcp"

// callbackURL is the route-base-scoped path the upstream provider redirects
// back to after the user authenticates. Empty routeBase falls back to "mcp"
// for back-compat with callers that haven't been threaded with a RouteBase
// yet (and for in-flight states minted before this parameter landed).
func (m *ChallengeManager) callbackURL(routeBase string) string {
	if routeBase == "" {
		routeBase = canonicalCallbackRouteBase
	}
	return strings.TrimRight(m.serverURL.String(), "/") + "/" + routeBase + "/remote_login_callback"
}

// legacyCallbackURL is the oauth_proxy_servers-era redirect_uri. Used only
// for clients flagged LegacyCallbackUrl whose upstream registration still
// points at this path; /oauth/callback then forwards them into
// /mcp/remote_login_callback by reading the JSON state envelope.
func (m *ChallengeManager) legacyCallbackURL() string {
	return strings.TrimRight(m.serverURL.String(), "/") + "/oauth/callback"
}

func (m *ChallengeManager) exchangeCode(
	ctx context.Context,
	state RemoteLoginState,
	externalClientID string,
	clientSecret string,
	authMethod TokenEndpointAuthMethod,
	audience string,
	resource string,
	code string,
) (tokenResponse, error) {
	form := url.Values{}
	form.Set("grant_type", "authorization_code")
	form.Set("code", code)
	form.Set("redirect_uri", state.RedirectURI)
	form.Set("client_id", externalClientID)
	form.Set("code_verifier", state.CodeVerifier)
	if audience != "" {
		form.Set("audience", audience)
	}
	// RFC 8707: the resource indicator must be repeated on the token
	// request so the AS binds the issued token to the same audience it
	// authorized. Must match the value sent by BuildAuthorizationUrl.
	if resource != "" {
		form.Set("resource", resource)
	}

	req, err := newTokenEndpointRequest(ctx, state.TokenEndpoint, form, authMethod, externalClientID, clientSecret)
	if err != nil {
		return tokenResponse{}, fmt.Errorf("new token request: %w", err)
	}

	resp, err := m.policy.PooledClient().Do(req)
	if err != nil {
		return tokenResponse{}, fmt.Errorf("post token: %w", err)
	}
	defer o11y.NoLogDefer(func() error { return resp.Body.Close() })

	body, err := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	if err != nil {
		return tokenResponse{}, fmt.Errorf("read token response body: %w", err)
	}
	if resp.StatusCode/100 != 2 {
		return tokenResponse{}, fmt.Errorf("token endpoint %s: %s", resp.Status, string(body))
	}
	var tok tokenResponse
	if err := json.Unmarshal(body, &tok); err != nil {
		return tokenResponse{}, fmt.Errorf("decode token response: %w", err)
	}
	if tok.AccessToken == "" {
		return tokenResponse{}, errors.New("token endpoint returned no access_token")
	}
	return tok, nil
}

func randomToken(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("read random bytes: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

func s256Challenge(verifier string) string {
	sum := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}
