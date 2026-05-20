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
// upstream token exchange instead of bouncing back to /mcp/{slug}/connect.
type ParentChallenge struct {
	ID                  string
	ProjectID           uuid.UUID
	UserSessionIssuerID uuid.UUID
	Subject             *urn.SessionSubject
	McpSlug             string
	FinalRedirectURI    string
}

// RemoteLoginState is the per-remote-leg Redis state, keyed by the opaque
// `state` parameter sent to the upstream provider. ~10 minute TTL — same
// budget as the parent AuthnChallengeState.
type RemoteLoginState struct {
	ID                    string              `json:"id"`
	ParentChallengeID     string              `json:"parent_challenge_id"`
	ProjectID             uuid.UUID           `json:"project_id"`
	UserSessionIssuerID   uuid.UUID           `json:"user_session_issuer_id"`
	RemoteSessionClientID uuid.UUID           `json:"remote_session_client_id"`
	TokenEndpoint         string              `json:"token_endpoint"`
	RedirectURI           string              `json:"redirect_uri"`
	CodeVerifier          string              `json:"code_verifier"`
	Subject               *urn.SessionSubject `json:"subject,omitempty"`
	McpSlug               string              `json:"mcp_slug"`
	// FinalRedirectURI overrides the default post-callback redirect to
	// /mcp/{slug}/connect. Set by dashboard-driven flows that own their
	// own popup-close surface (validated against an allow-list before
	// it lands here).
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
	Passthrough           bool
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
	userSessionIssuerID uuid.UUID,
) ([]Client, error) {
	rows, err := remotesessions_repo.New(m.db).ListRemoteSessionClientsForUserSessionIssuer(ctx, remotesessions_repo.ListRemoteSessionClientsForUserSessionIssuerParams{
		UserSessionIssuerID: userSessionIssuerID,
		ProjectID:           projectID,
	})
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
			Passthrough:           r.Passthrough,
		})
	}
	return out, nil
}

// ConnectedClientIDs returns the set of remote_session_client_ids that
// have an active remote_sessions row for `subject` under the given
// `userSessionIssuerID`. Single round-trip; the caller (consent
// renderer) then does O(1) membership checks per card. Returns an empty
// set for zero subjects so anonymous-pre-stamp renders are no-ops.
func (m *ChallengeManager) ConnectedClientIDs(
	ctx context.Context,
	subject urn.SessionSubject,
	userSessionIssuerID uuid.UUID,
) (map[uuid.UUID]struct{}, error) {
	if subject.IsZero() {
		return map[uuid.UUID]struct{}{}, nil
	}
	ids, err := remotesessions_repo.New(m.db).ListConnectedClientIDsForSubject(ctx, remotesessions_repo.ListConnectedClientIDsForSubjectParams{
		SubjectUrn:          subject,
		UserSessionIssuerID: userSessionIssuerID,
	})
	if err != nil {
		return nil, fmt.Errorf("list connected client ids: %w", err)
	}
	set := make(map[uuid.UUID]struct{}, len(ids))
	for _, id := range ids {
		set[id] = struct{}{}
	}
	return set, nil
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
	redirectURI := m.callbackURL()

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
		UserSessionIssuerID:   parent.UserSessionIssuerID,
		RemoteSessionClientID: client.ID,
		TokenEndpoint:         client.TokenEndpoint,
		RedirectURI:           redirectURI,
		CodeVerifier:          verifier,
		Subject:               parent.Subject,
		McpSlug:               parent.McpSlug,
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
	q.Set("state", stateID)
	q.Set("code_challenge", codeChallenge)
	q.Set("code_challenge_method", "S256")
	if scopes := client.resolveScopes(); len(scopes) > 0 {
		q.Set("scope", strings.Join(scopes, " "))
	}
	if client.Audience != "" {
		q.Set("audience", client.Audience)
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
		logger.WarnContext(ctx, "remote authn challenge returned error",
			attr.SlogOAuthError(errCode),
			attr.SlogOAuthErrorDescription(q.Get("error_description")),
		)
		return oops.E(oops.CodeUnauthorized, nil, "remote authn challenge denied: %s", errCode)
	}
	stateID := q.Get("state")
	if stateID == "" {
		return oops.E(oops.CodeBadRequest, nil, "state is required").Log(ctx, logger)
	}
	code := q.Get("code")
	if code == "" {
		return oops.E(oops.CodeBadRequest, nil, "code is required").Log(ctx, logger)
	}

	// Single-use state: GETDEL so a duplicate callback can't double-exchange
	// the code. The upstream code itself is also single-use, but defense in
	// depth keeps the failure mode obvious.
	state, err := m.cache.GetAndDelete(ctx, "remoteLogin:"+stateID)
	if err != nil {
		return oops.E(oops.CodeUnauthorized, err, "remote login state not found or expired").Log(ctx, logger)
	}
	mcpSlug := state.McpSlug
	if mcpSlug == "" {
		mcpSlug = routeMcpSlug
	}
	if mcpSlug == "" {
		return oops.E(oops.CodeBadRequest, nil, "mcp slug is missing from remote login state").Log(ctx, logger)
	}
	if routeMcpSlug != "" && routeMcpSlug != mcpSlug {
		return oops.E(oops.CodeUnauthorized, nil, "remote login state does not match this MCP server").Log(ctx, logger)
	}
	if state.McpSlug != "" && state.McpSlug != mcpSlug {
		return oops.E(oops.CodeUnauthorized, nil, "remote login state does not match this MCP server").Log(ctx, logger)
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
		return oops.E(oops.CodeUnauthorized, nil, "remote login requires a stamped subject on the parent challenge").Log(ctx, logger)
	}

	queries := remotesessions_repo.New(m.db)
	clientRow, err := queries.GetRemoteSessionClientByID(ctx, remotesessions_repo.GetRemoteSessionClientByIDParams{
		ID:        state.RemoteSessionClientID,
		ProjectID: state.ProjectID,
	})
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "load remote session client").Log(ctx, logger)
	}

	var clientSecret string
	if clientRow.ClientSecretEncrypted.Valid {
		decoded, derr := m.enc.Decrypt(clientRow.ClientSecretEncrypted.String)
		if derr != nil {
			return oops.E(oops.CodeUnexpected, derr, "decrypt client secret").Log(ctx, logger)
		}
		clientSecret = decoded
	}

	authMethod := ResolveTokenEndpointAuthMethod(clientRow.TokenEndpointAuthMethod.String)
	audience := conv.FromPGTextOrEmpty[string](clientRow.Audience)
	tok, err := m.exchangeCode(ctx, state, clientRow.ClientID, clientSecret, authMethod, audience, code)
	if err != nil {
		return oops.E(oops.CodeUnauthorized, err, "upstream token exchange failed").Log(ctx, logger)
	}

	accessEnc, err := m.enc.Encrypt([]byte(tok.AccessToken))
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "encrypt access token").Log(ctx, logger)
	}
	var refreshEnc *string
	if tok.RefreshToken != "" {
		v, eerr := m.enc.Encrypt([]byte(tok.RefreshToken))
		if eerr != nil {
			return oops.E(oops.CodeUnexpected, eerr, "encrypt refresh token").Log(ctx, logger)
		}
		refreshEnc = &v
	}

	// expires_in is OPTIONAL per RFC 6749 §5.1. Fall back to a 1h default
	// so we always have a positive access_expires_at to compare against —
	// silent-refresh logic in a later milestone will treat the row as
	// "needs refresh" if the upstream didn't tell us.
	accessExpires := time.Now().Add(1 * time.Hour)
	if tok.ExpiresIn > 0 {
		accessExpires = time.Now().Add(time.Duration(tok.ExpiresIn) * time.Second)
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
		AccessExpiresAt:       conv.ToPGTimestamptz(accessExpires),
		RefreshTokenEncrypted: conv.PtrToPGText(refreshEnc),
		RefreshExpiresAt:      conv.PtrToPGTimestamptz(refreshExpires),
		Scopes:                scopes,
	}); err != nil {
		return oops.E(oops.CodeUnexpected, err, "store remote session").Log(ctx, logger)
	}

	redirect := fmt.Sprintf("%s/mcp/%s/connect?state=%s", strings.TrimRight(m.serverURL.String(), "/"), mcpSlug, url.QueryEscape(state.ParentChallengeID))
	if state.FinalRedirectURI != "" {
		redirect = state.FinalRedirectURI
	}
	http.Redirect(w, r, redirect, http.StatusSeeOther)
	return nil
}

func (m *ChallengeManager) callbackURL() string {
	return strings.TrimRight(m.serverURL.String(), "/") + "/mcp/remote_login_callback"
}

// tokenResponse is the slice of the upstream /token reply we care about.
// RFC 6749 fields plus the optional refresh_expires_in some providers
// (e.g. Keycloak) include.
type tokenResponse struct {
	AccessToken      string `json:"access_token"`
	RefreshToken     string `json:"refresh_token"`
	TokenType        string `json:"token_type"`
	ExpiresIn        int    `json:"expires_in"`
	RefreshExpiresIn int    `json:"refresh_expires_in"`
	Scope            string `json:"scope"`
}

func (t tokenResponse) Scopes() []string {
	if t.Scope == "" {
		return nil
	}
	return strings.Split(t.Scope, " ")
}

func (m *ChallengeManager) exchangeCode(
	ctx context.Context,
	state RemoteLoginState,
	externalClientID string,
	clientSecret string,
	authMethod TokenEndpointAuthMethod,
	audience string,
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
