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
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"

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
	// Resource is the RFC 8707 resource indicator sent on the authorize
	// redirect and code exchange. Empty omits the parameter.
	Resource string
	// AutoRefresh is the subject's consent-screen auto-refresh choice for the
	// session this leg will mint. Nil means "no explicit choice" — the
	// callback falls back to the client capability's default.
	AutoRefresh *bool
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
	Resource              string              `json:"resource,omitempty"`
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
	FinalRedirectURI string `json:"final_redirect_uri,omitempty"`
	// AutoRefresh is the subject's consent-screen auto-refresh choice. Nil
	// (including in-flight states minted before this field) defers to the
	// client capability's default at persist time.
	AutoRefresh *bool     `json:"auto_refresh,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
}

var _ cache.CacheableObject[RemoteLoginState] = (*RemoteLoginState)(nil)

func (s RemoteLoginState) CacheKey() string   { return "remoteLogin:" + s.ID }
func (s RemoteLoginState) TTL() time.Duration { return 10 * time.Minute }

// ChallengeManager drives the per-remote OAuth authn-challenge leg.
type ChallengeManager struct {
	logger    *slog.Logger
	db        *pgxpool.Pool
	enc       *encryption.Client
	policy    *guardian.Policy
	cache     cache.TypedCacheObject[RemoteLoginState]
	locks     cache.Cache
	refresher *RefreshService
	serverURL *url.URL

	// revoker pushes RFC 7009 revocations upstream when the consent screen
	// disconnects a remote session, so the provider drops the tokens rather
	// than only Gram forgetting them.
	revoker *UpstreamRevoker

	// authorizeInterceptors adapt the outgoing upstream authorize request to
	// per-provider, non-standard requirements (e.g. Google's offline access).
	// Injected here rather than via a package-global registry.
	authorizeInterceptors []interceptors.AuthorizeInterceptor
}

func NewChallengeManager(
	logger *slog.Logger,
	tracerProvider trace.TracerProvider,
	meterProvider metric.MeterProvider,
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
		locks:     cacheImpl,
		refresher: NewRefreshService(logger, db, enc, policy, cacheImpl),
		serverURL: serverURL,
		revoker:   NewUpstreamRevoker(logger, tracerProvider, meterProvider, db, enc, policy),
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
	RemoteSessionIssuerID uuid.UUID
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
			RemoteSessionIssuerID: r.RemoteSessionIssuerID,
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

// RemoteSessionState is the consent renderer's per-client view of a stored
// remote_session: its usability plus the subject's stored auto-refresh
// preference.
type RemoteSessionState struct {
	Status      RemoteSessionStatus
	AutoRefresh bool
	// AccessExpiresAt is the upstream-reported deadline for the current access
	// token. RefreshExpiresAt is the earliest known deadline for renewal,
	// combining the refresh-token idle timeout and absolute authorization
	// lifetime. Either is nil when the provider omitted that lifetime.
	AccessExpiresAt  *time.Time
	RefreshExpiresAt *time.Time
	CanRefresh       bool
}

// RemoteSessionStatuses returns, per remote_session_client_id, the state of
// `subject`'s remote_session under the given `userSessionIssuerID`. Clients
// with no non-deleted session are omitted (disconnected). Single round-trip;
// the caller (consent renderer) then does O(1) lookups per card. Returns an
// empty map for zero subjects so anonymous-pre-stamp renders are no-ops.
func (m *ChallengeManager) RemoteSessionStatuses(
	ctx context.Context,
	subject urn.SessionSubject,
	projectID uuid.UUID,
	userSessionIssuerID uuid.UUID,
) (map[uuid.UUID]RemoteSessionState, error) {
	if subject.IsZero() {
		return map[uuid.UUID]RemoteSessionState{}, nil
	}
	rows, err := remotesessions_repo.New(m.db).ListRemoteSessionStatusesForSubject(ctx, remotesessions_repo.ListRemoteSessionStatusesForSubjectParams{
		SubjectUrn:          subject,
		UserSessionIssuerID: userSessionIssuerID,
		ProjectID:           projectID,
	})
	if err != nil {
		return nil, fmt.Errorf("list remote session statuses: %w", err)
	}
	statuses := make(map[uuid.UUID]RemoteSessionState, len(rows))
	for _, row := range rows {
		var accessExpiresAt *time.Time
		if row.AccessExpiresAt.Valid {
			expires := row.AccessExpiresAt.Time
			accessExpiresAt = &expires
		}
		var refreshExpiresAt *time.Time
		if row.RefreshExpiresAt.Valid {
			expires := row.RefreshExpiresAt.Time
			refreshExpiresAt = &expires
		}
		if row.AuthorizationExpiresAt.Valid &&
			(refreshExpiresAt == nil || row.AuthorizationExpiresAt.Time.Before(*refreshExpiresAt)) {
			expires := row.AuthorizationExpiresAt.Time
			refreshExpiresAt = &expires
		}
		statuses[row.RemoteSessionClientID] = RemoteSessionState{
			Status:           RemoteSessionStatus(row.Status),
			AutoRefresh:      row.AutoRefresh,
			AccessExpiresAt:  accessExpiresAt,
			RefreshExpiresAt: refreshExpiresAt,
			CanRefresh:       row.CanRefresh,
		}
	}
	return statuses, nil
}

var ErrRemoteSessionNotRefreshable = errors.New("remote session has no usable refresh token")

// RefreshRemoteSession performs an explicit consent-screen refresh through the
// same best-effort single-flight path as lazy and scheduled refreshes.
func (m *ChallengeManager) RefreshRemoteSession(
	ctx context.Context,
	subject urn.SessionSubject,
	userSessionIssuerID uuid.UUID,
	clientID uuid.UUID,
	fallbackResource string,
) (RefreshResult, error) {
	var zero RefreshResult

	session, err := remotesessions_repo.New(m.db).GetActiveRemoteSession(ctx, remotesessions_repo.GetActiveRemoteSessionParams{
		SubjectUrn:            subject,
		RemoteSessionClientID: clientID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return zero, ErrRemoteSessionNotRefreshable
	}
	if err != nil {
		return zero, fmt.Errorf("get consent remote session: %w", err)
	}
	if session.UserSessionIssuerID != userSessionIssuerID ||
		!session.RefreshTokenEncrypted.Valid ||
		session.RefreshTokenEncrypted.String == "" {
		return zero, ErrRemoteSessionNotRefreshable
	}

	return m.refresher.RefreshNow(ctx, session, fallbackResource)
}

// DisconnectRemoteSession soft-deletes the subject's remote_session for one
// client — the consent screen's per-card "Disconnect" — and then asks the
// upstream authorization server to drop the tokens it still holds.
//
// The user asked to disconnect a provider, so leaving a live refresh token at
// that provider would defeat the action; the upstream revocation is what makes
// the disconnect mean something outside Gram. It is best-effort in exactly the
// way the other revoke paths are: the soft delete has already committed by the
// time it runs, and a provider that is unreachable or refuses is recorded
// rather than surfaced, because the local disconnect succeeded either way.
//
// Returns the number of rows affected; zero means there was nothing to
// disconnect and nothing is sent upstream.
func (m *ChallengeManager) DisconnectRemoteSession(ctx context.Context, subject urn.SessionSubject, projectID uuid.UUID, userSessionIssuerID uuid.UUID, clientID uuid.UUID) (int64, error) {
	disconnected, err := remotesessions_repo.New(m.db).SoftDeleteRemoteSessionBySubjectAndClient(ctx, remotesessions_repo.SoftDeleteRemoteSessionBySubjectAndClientParams{
		SubjectUrn:            subject,
		RemoteSessionClientID: clientID,
		UserSessionIssuerID:   userSessionIssuerID,
		ProjectID:             projectID,
	})
	if err != nil {
		return 0, fmt.Errorf("disconnect remote session: %w", err)
	}

	for _, row := range disconnected {
		m.revoker.RevokeDetached(ctx, RevokedCredentials{
			RemoteSessionClientID: row.RemoteSessionClientID,
			AccessTokenEncrypted:  row.AccessTokenEncrypted,
			RefreshTokenEncrypted: row.RefreshTokenEncrypted,
		})
	}

	return int64(len(disconnected)), nil
}

// SetRemoteSessionAutoRefresh records the subject's consent-screen
// auto-refresh choice for one client. Returns rows affected; zero means no
// active session exists for the binding (e.g. disconnected in another tab).
func (m *ChallengeManager) SetRemoteSessionAutoRefresh(ctx context.Context, subject urn.SessionSubject, projectID uuid.UUID, userSessionIssuerID uuid.UUID, clientID uuid.UUID, enabled bool) (int64, error) {
	n, err := remotesessions_repo.New(m.db).SetRemoteSessionAutoRefresh(ctx, remotesessions_repo.SetRemoteSessionAutoRefreshParams{
		AutoRefresh:           enabled,
		SubjectUrn:            subject,
		RemoteSessionClientID: clientID,
		UserSessionIssuerID:   userSessionIssuerID,
		ProjectID:             projectID,
	})
	if err != nil {
		return 0, fmt.Errorf("set remote session auto refresh: %w", err)
	}
	return n, nil
}

// BuildAuthorizationUrl mints a RemoteLoginState (with PKCE S256) for the
// (parent, client) pair, stores it in Redis, and returns the upstream
// authorize URL with bound `state` + `code_challenge` query params. The
// caller is the consent-screen connect action; this is called once per
// connect click.
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
		// callback, so keep that exact redirect_uri — the upstream's
		// strict-match check still requires it. /oauth/callback forwards the
		// response into the canonical remote-login callback. The state is the
		// bare stateID, same as the non-legacy path: with the proxy gone,
		// /oauth/callback serves only these forwards, so there is nothing to
		// tell them apart from and no envelope is needed.
		redirectURI = m.legacyCallbackURL()
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
		Resource:              parent.Resource,
		Subject:               parent.Subject,
		McpSlug:               parent.McpSlug,
		RouteBase:             parent.RouteBase,
		FinalRedirectURI:      parent.FinalRedirectURI,
		AutoRefresh:           parent.AutoRefresh,
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
	if parent.Resource != "" {
		q.Set("resource", parent.Resource)
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
	if state.Resource != "" {
		logger = logger.With(attr.SlogOAuthResource(state.Resource))
	}

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

	authMethod, err := ResolveTokenEndpointAuthMethod(client.TokenEndpointAuthMethod.String, clientSecret)
	if err != nil {
		return oops.E(oops.CodeUnauthorized, err, "the remote session client is misconfigured").LogError(ctx, logger)
	}
	audience := conv.FromPGTextOrEmpty[string](client.Audience)
	tok, err := m.exchangeCode(ctx, state, client.ClientID, clientSecret, authMethod, audience, code)
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
	// provider never asserted. validateAndRefresh serves that token as-is; a
	// refresh token does not imply that the access token expires.
	now := time.Now()
	var accessExpires *time.Time
	if tok.ExpiresIn > 0 {
		v := now.Add(time.Duration(tok.ExpiresIn) * time.Second)
		accessExpires = &v
	}
	refreshTimeout, refreshTimeoutReported := tok.RefreshTokenTimeoutSeconds()
	refreshExpires := expirationDeadline(now, refreshTimeout, refreshTimeoutReported)
	authorizationLifetime, authorizationLifetimeReported := tok.AuthorizationLifetimeSeconds()
	authorizationExpires := expirationDeadline(now, authorizationLifetime, authorizationLifetimeReported)

	scopes := tok.Scopes()
	if scopes == nil {
		scopes = []string{}
	}
	// Only the visible consent control opts a new session into scheduled
	// keepalive. On reconnect, UpsertRemoteSession preserves the stored choice.
	autoRefresh := false
	if state.AutoRefresh != nil {
		autoRefresh = *state.AutoRefresh
	}

	if _, err := queries.UpsertRemoteSession(ctx, remotesessions_repo.UpsertRemoteSessionParams{
		SubjectUrn:            *state.Subject,
		UserSessionIssuerID:   state.UserSessionIssuerID,
		RemoteSessionClientID: state.RemoteSessionClientID,
		AccessTokenEncrypted:  accessEnc,
		AccessExpiresAt:       conv.PtrToPGTimestamptz(accessExpires),
		RefreshTokenEncrypted: conv.PtrToPGText(refreshEnc),
		AuthorizationExpiresAt: conv.PtrToPGTimestamptz(
			authorizationExpires,
		),
		RefreshExpiresAt: conv.PtrToPGTimestamptz(refreshExpires),
		Scopes:           scopes,
		Resource:         conv.ToPGTextEmpty(state.Resource),
		AutoRefresh:      autoRefresh,
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

// legacyCallbackURL is the oauth_proxy_servers-era redirect_uri. Used only for
// clients flagged LegacyCallbackUrl whose upstream registration still points at
// this path; HandleLegacyProxyCallback forwards them into
// /mcp/remote_login_callback.
func (m *ChallengeManager) legacyCallbackURL() string {
	return strings.TrimRight(m.serverURL.String(), "/") + "/oauth/callback"
}

// HandleLegacyProxyCallback is the shim behind `GET /oauth/callback`, the
// oauth_proxy_servers-era redirect_uri that clients flagged LegacyCallbackUrl
// still send upstream. The proxy that once shared this path is gone, so every
// response here is a remote-session callback: forward the query string (state,
// code, error) unchanged to the canonical /mcp/remote_login_callback, where the
// remote-session flow finishes the exchange.
func (m *ChallengeManager) HandleLegacyProxyCallback(w http.ResponseWriter, r *http.Request) error {
	target := strings.TrimRight(m.serverURL.String(), "/") + "/" + canonicalCallbackRouteBase + "/remote_login_callback"
	if raw := r.URL.RawQuery; raw != "" {
		target += "?" + raw
	}
	http.Redirect(w, r, target, http.StatusFound)
	return nil
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
	form.Set("code_verifier", state.CodeVerifier)
	if audience != "" {
		form.Set("audience", audience)
	}
	if state.Resource != "" {
		form.Set("resource", state.Resource)
	}

	req, err := newTokenEndpointRequest(ctx, state.TokenEndpoint, form, authMethod, externalClientID, clientSecret)
	if err != nil {
		return tokenResponse{}, fmt.Errorf("new token request: %w", err)
	}

	resp, err := m.policy.PooledClient(guardian.WithAllowedSchemes("http")).Do(req)
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
