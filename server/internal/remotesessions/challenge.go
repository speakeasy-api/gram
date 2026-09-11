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
	"slices"
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
	customdomainsrepo "github.com/speakeasy-api/gram/server/internal/customdomains/repo"
	"github.com/speakeasy-api/gram/server/internal/encryption"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/networkingress"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/oautherr"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/remotesessions/interceptors"
	"github.com/speakeasy-api/gram/server/internal/remotesessions/remotesessionmetrics"
	remotesessions_repo "github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
	"github.com/speakeasy-api/gram/server/internal/requestorigin"
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
	McpServerID         uuid.NullUUID
	MetaMcpServerID     uuid.NullUUID
	FinalRedirectURI    string
	// Resource is the RFC 8707 resource indicator sent on the authorize
	// redirect and code exchange. Empty omits the parameter.
	Resource string
	// AutoRefresh is the subject's consent-screen auto-refresh choice for the
	// session this leg will mint. Nil means "no explicit choice" — the
	// callback falls back to the client capability's default.
	AutoRefresh *bool
	// Authority carries only provider-neutral mint-time request authority. It
	// never contains advisory network identity or provider credentials.
	Authority networkingress.Authority
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
	OrganizationID        string    `json:"organization_id,omitempty"`
	UserSessionIssuerID   uuid.UUID `json:"user_session_issuer_id"`
	RemoteSessionClientID uuid.UUID `json:"remote_session_client_id"`
	TokenEndpoint         string    `json:"token_endpoint"`
	// AssertionIssuer is the RFC 8414 issuer identifier captured with the
	// authorization request. It is the default private_key_jwt audience. Empty
	// for an in-flight state minted by a server from before AIM-156.
	AssertionIssuer string              `json:"assertion_issuer,omitempty"`
	RedirectURI     string              `json:"redirect_uri"`
	CodeVerifier    string              `json:"code_verifier"`
	Resource        string              `json:"resource,omitempty"`
	Subject         *urn.SessionSubject `json:"subject,omitempty"`
	McpSlug         string              `json:"mcp_slug"`
	// RouteBase is "mcp" or "x/mcp" — drives the post-callback redirect
	// to /<RouteBase>/{slug}/connect. Empty values fall back to "mcp"
	// for in-flight states minted before this field landed.
	RouteBase       string        `json:"route_base,omitempty"`
	McpServerID     uuid.NullUUID `json:"mcp_server_id,omitzero"`
	MetaMcpServerID uuid.NullUUID `json:"meta_mcp_server_id,omitzero"`
	// FinalRedirectURI overrides the default post-callback redirect to
	// /<RouteBase>/{slug}/connect. Set by dashboard-driven flows that
	// own their own popup-close surface (validated against an allow-list
	// before it lands here).
	FinalRedirectURI string `json:"final_redirect_uri,omitempty"`
	// AutoRefresh is the subject's consent-screen auto-refresh choice. Nil
	// (including in-flight states minted before this field) defers to the
	// client capability's default at persist time.
	AutoRefresh *bool                    `json:"auto_refresh,omitempty"`
	Authority   networkingress.Authority `json:"authority,omitzero"`
	// Scopes is the requested set, recorded on the session when the response omits scope (RFC 6749 §5.1).
	Scopes []string `json:"scopes,omitempty"`
	// OmitResource keeps Resource off the wire while still recording it on the session.
	OmitResource bool `json:"omit_resource,omitempty"`
	// ResourceRetried marks the single retry leg the callback mints after an
	// invalid_target answer. A retry leg that is refused again fails the login.
	ResourceRetried bool `json:"resource_retried,omitempty"`
	// ExpectedIssuer is what the RFC 9207 iss parameter must equal; empty skips the check.
	ExpectedIssuer string `json:"expected_issuer,omitempty"`
	// Nonce is echoed by the ID token; empty for states minted before it existed.
	Nonce     string    `json:"nonce,omitempty"`
	CreatedAt time.Time `json:"created_at"`
}

// parent rebuilds the ParentChallenge this state was minted from, so the
// callback can mint a sibling leg for the same login.
func (s RemoteLoginState) parent() ParentChallenge {
	return ParentChallenge{
		ID:                  s.ParentChallengeID,
		ProjectID:           s.ProjectID,
		OrganizationID:      s.OrganizationID,
		UserSessionIssuerID: s.UserSessionIssuerID,
		Subject:             s.Subject,
		McpSlug:             s.McpSlug,
		RouteBase:           s.RouteBase,
		FinalRedirectURI:    s.FinalRedirectURI,
		Resource:            s.Resource,
		McpServerID:         s.McpServerID,
		MetaMcpServerID:     s.MetaMcpServerID,
		AutoRefresh:         s.AutoRefresh,
		Authority:           s.Authority,
	}
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

	// metrics carries the unsampled upstream-authorize census that the PKCE
	// enforcement decision (AIS-566) reads.
	metrics *remotesessionmetrics.Authorize
	// privateAuthorityValidator is injected at construction. The callback package
	// owns state mechanics; the caller owns endpoint resolution.
	privateAuthorityValidator PrivateAuthorityValidator

	// idTokens verifies the ID token a code exchange or refresh returns.
	idTokens IDTokenVerifier

	// assertions signs outbound private_key_jwt client authentication.
	assertions TokenEndpointAssertionSigner

	// issuerMetadata refreshes an issuer's stored metadata when a flow uses it; nil leaves the stored row as is.
	issuerMetadata *IssuerMetadataRefresher
	// enricher asks the issuer's userinfo and introspection endpoints about a grant.
	enricher *SessionEnricher
}

// RemoteGrant is a grant the remote login callback committed, keyed to the
// consent challenge it belongs to.
type RemoteGrant struct {
	// ParentChallengeID is the consent challenge the login was started from.
	ParentChallengeID string
	// UserSessionIssuerID is the issuer that challenge was minted for.
	UserSessionIssuerID uuid.UUID
	// RemoteSessionClientID is the client the grant was stored against.
	RemoteSessionClientID uuid.UUID
	// Subject is who holds the grant.
	Subject urn.SessionSubject
	// RemoteSessionID and RemoteSessionUpdatedAt identify the exact stored credential committed by the callback.
	RemoteSessionID        uuid.UUID
	RemoteSessionUpdatedAt time.Time
}

// RemoteLoginResult is where the callback sends the browser next.
type RemoteLoginResult struct {
	RedirectURL string
	// Grant is the committed grant, nil while the login is being retried upstream.
	Grant *RemoteGrant
}

// PrivateAuthorityValidator revalidates a private endpoint without introducing
// a remotesessions -> mcp dependency.
type PrivateAuthorityValidator func(context.Context, RemoteLoginState) error

type ChallengeManagerOption func(*ChallengeManager)

func WithPrivateAuthorityValidator(validator PrivateAuthorityValidator) ChallengeManagerOption {
	return func(m *ChallengeManager) {
		m.privateAuthorityValidator = validator
	}
}

// WithIDTokenVerifier enables identity capture from ID tokens on the exchange and the manager's refreshes.
func WithIDTokenVerifier(verifier IDTokenVerifier) ChallengeManagerOption {
	return func(m *ChallengeManager) { m.idTokens = verifier }
}

// WithIssuerMetadataRefresher refreshes issuer metadata on use from the consent render and the manager's refreshes.
func WithIssuerMetadataRefresher(refresher *IssuerMetadataRefresher) ChallengeManagerOption {
	return func(m *ChallengeManager) { m.issuerMetadata = refresher }
}

// WithSessionEnricher replaces the default enricher, which is unpaced and
// cannot verify signed introspection responses.
func WithSessionEnricher(enricher *SessionEnricher) ChallengeManagerOption {
	return func(m *ChallengeManager) { m.enricher = enricher }
}

func WithTokenEndpointAssertionSigner(signer TokenEndpointAssertionSigner) ChallengeManagerOption {
	return func(m *ChallengeManager) { m.assertions = signer }
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
	options ...ChallengeManagerOption,
) *ChallengeManager {
	logger = logger.With(attr.SlogComponent("remotesessions_challenge"))
	manager := &ChallengeManager{
		logger: logger,
		db:     db,
		enc:    enc,
		policy: policy,
		cache: cache.NewTypedObjectCache[RemoteLoginState](
			logger.With(attr.SlogCacheNamespace("remote_login")),
			cacheImpl,
			cache.SuffixNone,
		),
		locks:          cacheImpl,
		refresher:      nil,
		issuerMetadata: nil,
		serverURL:      serverURL,
		revoker:        NewUpstreamRevoker(logger, tracerProvider, meterProvider, db, enc, policy),
		authorizeInterceptors: []interceptors.AuthorizeInterceptor{
			interceptors.NewGoogle(logger),
		},
		metrics:                   remotesessionmetrics.NewAuthorize(logger, meterProvider),
		privateAuthorityValidator: nil,
		idTokens:                  NoIDTokenVerifier(),
		enricher:                  nil,
		assertions:                unavailableTokenEndpointAssertionSigner{},
	}
	for _, option := range options {
		option(manager)
	}
	if manager.enricher == nil {
		manager.enricher = NewSessionEnricher(logger, enc, policy, nil, nil)
	}
	// The manager's own refreshes restate identity with the same verifier.
	manager.refresher = NewRefreshService(logger, meterProvider, db, enc, policy, cacheImpl, WithRefreshIDTokenVerifier(manager.idTokens), WithRefreshIssuerMetadataRefresher(manager.issuerMetadata), WithRefreshTokenEndpointAssertionSigner(manager.assertions))
	return manager
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

	// IssuerName is the issuer's operator-set display name, nil when unset.
	IssuerName *string

	// IssuerLogoAssetID references the issuer's logo image in the assets
	// store, invalid when the issuer has no logo.
	IssuerLogoAssetID uuid.NullUUID

	IssuerURL string

	// IssuerIdentifier is the discovery document's issuer, else IssuerURL; what iss must equal.
	IssuerIdentifier string
	// ClientAssertionIssuer preserves the exact RFC 8414 issuer identifier for
	// private_key_jwt aud claims, including a significant trailing slash.
	ClientAssertionIssuer string

	AuthorizationEndpoint string
	TokenEndpoint         string
	// ClientScope is the client's stored scope (PRM scopes_supported at
	// registration, or an operator's); the base of the request when non-empty.
	ClientScope           []string
	IssuerScopesSupported []string

	// IssuerScopeOverride is the operator-pinned scope request on the issuer.
	// Empty when unset; set, it is requested verbatim.
	IssuerScopeOverride []string

	// IssuerResourceIndicatorSupported is an operator's answer to whether the
	// issuer accepts the RFC 8707 resource parameter. Nil sends it.
	IssuerResourceIndicatorSupported *bool

	// IssuerAuthorizationResponseIssParameterSupported makes the callback require and validate iss.
	IssuerAuthorizationResponseIssParameterSupported bool

	// IssuerCodeChallengeMethodsSupported carries the issuer's stored
	// code_challenge_methods_supported for flow-time PKCE telemetry. Nil means
	// the column is NULL (never captured) — distinct from an empty slice
	// (captured; the issuer advertises no methods), so it must not be run
	// through nil-collapsing copy idioms.
	IssuerCodeChallengeMethodsSupported []string

	Audience    string
	Passthrough bool
	// LegacyCallbackUrl flips BuildAuthorizationUrl onto the
	// /oauth/callback redirect_uri (with a JSON state carrying
	// remote_sessions=true) so a client registered against the old
	// oauth_proxy_servers URL keeps working without re-registration.
	LegacyCallbackUrl bool
}

// standardScopes are appended when advertised: openid, email, profile for
// identity; offline_access for a refresh token.
var standardScopes = []string{"openid", "email", "profile", "offline_access"}

// RequestedScopes resolves the authorize scope set: IssuerScopeOverride
// verbatim; else ClientScope (or IssuerScopesSupported when empty) plus each
// standard scope the issuer advertises. widened is what was appended to a
// client scope.
func (c Client) RequestedScopes() (scopes []string, widened []string) {
	if len(c.IssuerScopeOverride) > 0 {
		return slices.Clone(c.IssuerScopeOverride), nil
	}
	base := c.IssuerScopesSupported
	narrowed := len(c.ClientScope) > 0
	if narrowed {
		base = c.ClientScope
	}
	scopes = slices.Clone(base)
	for _, scope := range standardScopes {
		if slices.Contains(scopes, scope) || !slices.Contains(c.IssuerScopesSupported, scope) {
			continue
		}
		scopes = append(scopes, scope)
		if narrowed {
			widened = append(widened, scope)
		}
	}
	return scopes, widened
}

// issuerIdentifier is the document's issuer verbatim, else the stored URL without a trailing slash.
func issuerIdentifier(metadata []byte, issuerURL string) string {
	if doc := rawDocumentIssuer(metadata); doc != "" {
		return doc
	}
	return strings.TrimRight(issuerURL, "/")
}

// clientAssertionIssuer is the RFC 8414 issuer identifier used as the default
// private_key_jwt audience. Preserve the configured URL verbatim when no
// discovery document is stored: a trailing slash is significant to audience
// comparison (notably for Auth0 issuers).
func clientAssertionIssuer(metadata []byte, issuerURL string) string {
	if doc := rawDocumentIssuer(metadata); doc != "" && issuerURLsCanonicallyEqual(doc, issuerURL) {
		return doc
	}
	return strings.TrimSpace(issuerURL)
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
	noted := make(map[uuid.UUID]bool, len(rows))
	for _, r := range rows {
		if !noted[r.RemoteSessionIssuerID] {
			noted[r.RemoteSessionIssuerID] = true
			m.issuerMetadata.NoteUse(ctx, issuerUseFromClientListRow(r))
		}
		out = append(out, Client{
			ID:                               r.ClientID,
			RemoteSessionIssuerID:            r.RemoteSessionIssuerID,
			ExternalClientID:                 r.ExternalClientID,
			ClientSecretEncrypted:            conv.FromPGText[string](r.ClientSecretEncrypted),
			IssuerSlug:                       r.IssuerSlug,
			IssuerName:                       conv.FromPGText[string](r.IssuerName),
			IssuerLogoAssetID:                r.IssuerLogoAssetID,
			IssuerURL:                        r.IssuerUrl,
			IssuerIdentifier:                 issuerIdentifier(r.IssuerMetadata, r.IssuerUrl),
			ClientAssertionIssuer:            clientAssertionIssuer(r.IssuerMetadata, r.IssuerUrl),
			AuthorizationEndpoint:            conv.PtrValOr(conv.FromPGText[string](r.AuthorizationEndpoint), ""),
			TokenEndpoint:                    conv.PtrValOr(conv.FromPGText[string](r.TokenEndpoint), ""),
			ClientScope:                      r.ClientScope,
			IssuerScopesSupported:            r.ScopesSupported,
			IssuerScopeOverride:              r.ScopeOverride,
			IssuerResourceIndicatorSupported: conv.FromPGBool[bool](r.ResourceIndicatorSupported),
			IssuerAuthorizationResponseIssParameterSupported: r.AuthorizationResponseIssParameterSupported.Valid && r.AuthorizationResponseIssParameterSupported.Bool,
			IssuerCodeChallengeMethodsSupported:              r.CodeChallengeMethodsSupported,
			Audience:                                         conv.FromPGTextOrEmpty[string](r.ClientAudience),
			Passthrough:                                      r.Passthrough,
			LegacyCallbackUrl:                                r.LegacyCallbackUrl,
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
	// token. RefreshExpiresAt is the refresh token's own deadline — an idle
	// timeout that using the session postpones. AuthorizationExpiresAt is the
	// absolute end of the grant, which renewing does not move. The two are kept
	// apart rather than reduced to the earliest, because auto refresh defeats
	// the first and is powerless against the second, and a caller that cannot
	// tell them apart cannot say which applies. Any is nil when the provider
	// omitted that lifetime, which is common.
	AccessExpiresAt        *time.Time
	RefreshExpiresAt       *time.Time
	AuthorizationExpiresAt *time.Time
	CanRefresh             bool
	// Resource is the RFC 8707 resource recorded on the grant, if any.
	Resource string
	// Scopes is the scope set the grant carries, as the provider reported it
	// at exchange time or, when it reported none, as it was requested.
	Scopes []string
	// ConnectedAs is the account identity: the upstream display name and email; empty when unknown.
	ConnectedAs string
	// AccountChips is the provider context beside it (workspace, team, login); empty when none.
	AccountChips []string
	// IdentitySource names the interface ConnectedAs came from.
	IdentitySource string
	// Token is the introspection interface's last answer about the access token; nil when never asked or unanswered.
	Token *IntrospectedToken
	// ID is the remote_sessions row a verdict is written against.
	ID uuid.UUID
	// UpdatedAt is the row's CAS token; a verdict only lands while it still holds.
	UpdatedAt time.Time
	// LastValidatedAt is when a probe last presented the credential; nil when never.
	LastValidatedAt *time.Time
	// ValidationStatus is that probe's verdict, empty when never validated.
	ValidationStatus ValidationOutcome
	// ValidationReason is the Gram-authored explanation of a non-valid verdict.
	ValidationReason string
}

// RemoteSessionStatuses returns, per remote_session_client_id, the state of
// `subject`'s remote_session on every client bound to the requesting
// `userSessionIssuerID`. Clients with no non-deleted session are omitted
// (disconnected). Single round-trip; the caller (consent renderer) then does
// O(1) lookups per card. Returns an empty map for zero subjects so
// anonymous-pre-stamp renders are no-ops. The stored user_session_issuer_id
// is provenance from INSERT, not a lookup key, so a grant minted by a
// different issuer — including one since soft-deleted — is still returned.
func (m *ChallengeManager) RemoteSessionStatuses(
	ctx context.Context,
	subject urn.SessionSubject,
	projectID uuid.UUID,
	organizationID string,
	userSessionIssuerID uuid.UUID,
) (map[uuid.UUID]RemoteSessionState, error) {
	if subject.IsZero() {
		return map[uuid.UUID]RemoteSessionState{}, nil
	}
	rows, err := remotesessions_repo.New(m.db).ListRemoteSessionStatusesForSubject(ctx, remotesessions_repo.ListRemoteSessionStatusesForSubjectParams{
		SubjectUrn:          subject,
		UserSessionIssuerID: userSessionIssuerID,
		ProjectID:           projectID,
		OrganizationID:      organizationID,
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
		var authorizationExpiresAt *time.Time
		if row.AuthorizationExpiresAt.Valid {
			expires := row.AuthorizationExpiresAt.Time
			authorizationExpiresAt = &expires
		}
		var lastValidatedAt *time.Time
		if row.LastValidatedAt.Valid {
			validated := row.LastValidatedAt.Time
			lastValidatedAt = &validated
		}
		var token *IntrospectedToken
		if introspected, ok := IntrospectedTokenFromEnrichment(row.Enrichment); ok {
			token = &introspected
		}
		label := RemoteSessionAccountLabel(row.UpstreamEmail.String, row.UpstreamDisplayName.String, row.Enrichment)
		statuses[row.RemoteSessionClientID] = RemoteSessionState{
			Status:                 RemoteSessionStatus(row.Status),
			AutoRefresh:            row.AutoRefresh,
			AccessExpiresAt:        accessExpiresAt,
			RefreshExpiresAt:       refreshExpiresAt,
			AuthorizationExpiresAt: authorizationExpiresAt,
			CanRefresh:             row.CanRefresh,
			Resource:               row.Resource.String,
			Scopes:                 row.Scopes,
			ConnectedAs:            label.Identity(),
			AccountChips:           label.Context(),
			IdentitySource:         row.IdentitySource.String,
			Token:                  token,
			ID:                     row.ID,
			UpdatedAt:              row.UpdatedAt.Time,
			LastValidatedAt:        lastValidatedAt,
			ValidationStatus:       ValidationOutcome(row.ValidationStatus.String),
			ValidationReason:       row.ValidationReason.String,
		}
	}
	return statuses, nil
}

var ErrRemoteSessionNotRefreshable = errors.New("remote session has no usable refresh token")

// RefreshRemoteSession performs an explicit consent-screen refresh through the
// same best-effort single-flight path as lazy and scheduled refreshes.
//
// The credential is shared by every user_session_issuer bound to its client;
// its stored user_session_issuer_id is provenance only. Authorization is
// therefore the requesting issuer's tenant-scoped client binding, not a match
// against the surface that happened to mint the row — any bound surface may
// refresh, and an unbound one fails closed with ErrRemoteSessionNotRefreshable.
func (m *ChallengeManager) RefreshRemoteSession(
	ctx context.Context,
	subject urn.SessionSubject,
	projectID uuid.UUID,
	organizationID string,
	userSessionIssuerID uuid.UUID,
	clientID uuid.UUID,
) (RefreshResult, error) {
	var zero RefreshResult

	bound, err := remotesessions_repo.New(m.db).CheckRemoteSessionClientBindingForUserSessionIssuer(ctx, remotesessions_repo.CheckRemoteSessionClientBindingForUserSessionIssuerParams{
		RemoteSessionClientID: clientID,
		UserSessionIssuerID:   userSessionIssuerID,
		ProjectID:             projectID,
		OrganizationID:        organizationID,
	})
	if err != nil {
		return zero, fmt.Errorf("check remote session client binding: %w", err)
	}
	if !bound {
		return zero, ErrRemoteSessionNotRefreshable
	}

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
	if !session.RefreshTokenEncrypted.Valid || session.RefreshTokenEncrypted.String == "" {
		return zero, ErrRemoteSessionNotRefreshable
	}

	return m.refresher.RefreshNow(ctx, session, "", remotesessionmetrics.RefreshTriggerManual)
}

// FallbackResourceForClient derives one client's RFC 8707 resource from its
// attached MCP servers; ambiguous or absent upstreams derive "".
func (m *ChallengeManager) FallbackResourceForClient(ctx context.Context, clientID uuid.UUID) (string, error) {
	return m.refresher.FallbackResourceForClient(ctx, clientID)
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
func (m *ChallengeManager) DisconnectRemoteSession(ctx context.Context, subject urn.SessionSubject, projectID uuid.UUID, organizationID string, userSessionIssuerID uuid.UUID, clientID uuid.UUID) (int64, error) {
	disconnected, err := remotesessions_repo.New(m.db).SoftDeleteRemoteSessionBySubjectAndClient(ctx, remotesessions_repo.SoftDeleteRemoteSessionBySubjectAndClientParams{
		SubjectUrn:            subject,
		RemoteSessionClientID: clientID,
		UserSessionIssuerID:   userSessionIssuerID,
		ProjectID:             projectID,
		OrganizationID:        organizationID,
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
func (m *ChallengeManager) SetRemoteSessionAutoRefresh(ctx context.Context, subject urn.SessionSubject, projectID uuid.UUID, organizationID string, userSessionIssuerID uuid.UUID, clientID uuid.UUID, enabled bool) (int64, error) {
	n, err := remotesessions_repo.New(m.db).SetRemoteSessionAutoRefresh(ctx, remotesessions_repo.SetRemoteSessionAutoRefreshParams{
		AutoRefresh:           enabled,
		SubjectUrn:            subject,
		RemoteSessionClientID: clientID,
		UserSessionIssuerID:   userSessionIssuerID,
		ProjectID:             projectID,
		OrganizationID:        organizationID,
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
	return m.mintAuthorization(ctx, parent, client, false)
}

// mintAuthorization is BuildAuthorizationUrl; retry marks the single
// resource-less leg minted after invalid_target.
func (m *ChallengeManager) mintAuthorization(
	ctx context.Context,
	parent ParentChallenge,
	client Client,
	retry bool,
) (string, error) {
	// Counted at entry, before any validation or the Redis write, so a flow
	// that dies on an unrelated error here still lands in the census. A retry
	// leg is the same login and is not counted again.
	if !retry {
		m.metrics.Record(ctx, client.IssuerURL, remotesessionmetrics.ClassifyPKCESupport(client.IssuerCodeChallengeMethodsSupported))
	}

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
	nonce, err := randomToken(16)
	if err != nil {
		return "", fmt.Errorf("generate nonce: %w", err)
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

	scopes, widened := client.RequestedScopes()
	if len(widened) > 0 {
		m.logger.DebugContext(ctx, "requested scope widens the client's configured scope",
			attr.SlogProjectID(parent.ProjectID.String()),
			attr.SlogOrganizationID(parent.OrganizationID),
			attr.SlogOAuthIssuer(client.IssuerURL),
			attr.SlogRemoteSessionClientID(client.ID.String()),
			attr.SlogOAuthScope(strings.Join(scopes, " ")),
			attr.SlogOAuthScopeAdded(strings.Join(widened, " ")),
		)
	}
	// The resource stays on the session: a grant without one is unroutable.
	omitResource := parent.Resource != "" && client.IssuerResourceIndicatorSupported != nil && !*client.IssuerResourceIndicatorSupported
	expectedIssuer := ""
	if client.IssuerAuthorizationResponseIssParameterSupported {
		expectedIssuer = client.IssuerIdentifier
	}

	state := RemoteLoginState{
		ID:                    stateID,
		ParentChallengeID:     parent.ID,
		ProjectID:             parent.ProjectID,
		OrganizationID:        parent.OrganizationID,
		UserSessionIssuerID:   parent.UserSessionIssuerID,
		RemoteSessionClientID: client.ID,
		TokenEndpoint:         client.TokenEndpoint,
		AssertionIssuer:       client.ClientAssertionIssuer,
		RedirectURI:           redirectURI,
		CodeVerifier:          verifier,
		Resource:              parent.Resource,
		Subject:               parent.Subject,
		McpSlug:               parent.McpSlug,
		RouteBase:             parent.RouteBase,
		McpServerID:           parent.McpServerID,
		MetaMcpServerID:       parent.MetaMcpServerID,
		FinalRedirectURI:      parent.FinalRedirectURI,
		AutoRefresh:           parent.AutoRefresh,
		Authority:             parent.Authority,
		Scopes:                scopes,
		OmitResource:          omitResource,
		ResourceRetried:       retry,
		ExpectedIssuer:        expectedIssuer,
		Nonce:                 nonce,
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
	// Harmless to a plain OAuth server and required for an OpenID one to
	// bind the ID token to this request.
	q.Set("nonce", nonce)
	if len(scopes) > 0 {
		q.Set("scope", strings.Join(scopes, " "))
	}
	if client.Audience != "" {
		q.Set("audience", client.Audience)
	}
	if parent.Resource != "" && !omitResource {
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
// Completes the login through CompleteRemoteLogin and redirects the browser.
func (m *ChallengeManager) HandleRemoteLoginCallback(w http.ResponseWriter, r *http.Request) error {
	result, err := m.CompleteRemoteLogin(r)
	if err != nil {
		return err
	}
	http.Redirect(w, r, result.RedirectURL, http.StatusSeeOther)
	return nil
}

// CompleteRemoteLogin exchanges the upstream code, persists the result in
// remote_sessions, and returns where the browser goes next: the consent page
// /{route}/{slug}/connect?state={parent_challenge_id}, or the upstream
// authorize URL again when the login is retried without a resource. It writes
// nothing itself.
func (m *ChallengeManager) CompleteRemoteLogin(r *http.Request) (RemoteLoginResult, error) {
	var none RemoteLoginResult
	ctx := r.Context()
	routeMcpSlug := chi.URLParam(r, "mcpSlug")
	logger := m.logger

	q := r.URL.Query()
	errCode := q.Get("error")
	code := q.Get("code")
	stateID := q.Get("state")
	if stateID == "" {
		if errCode != "" {
			return none, denied(ctx, logger, q)
		}
		return none, oops.E(oops.CodeBadRequest, nil, "state is required").LogError(ctx, logger)
	}
	// Checked before the state is consumed so a bare ?state= prefetch does not
	// burn a pending login.
	if code == "" && errCode == "" {
		return none, oops.E(oops.CodeBadRequest, nil, "code is required").LogError(ctx, logger)
	}

	// Single-use state: GETDEL so a duplicate callback can't double-exchange
	// the code. The upstream code itself is also single-use, but defense in
	// depth keeps the failure mode obvious. A denial is read after the state
	// is consumed, so the issuer that answered it is known.
	state, err := m.cache.GetAndDelete(ctx, "remoteLogin:"+stateID)
	if err != nil {
		if errCode != "" {
			return none, denied(ctx, logger, q)
		}
		return none, oops.E(oops.CodeUnauthorized, err, "remote login state not found or expired").LogError(ctx, logger)
	}
	mcpSlug := state.McpSlug
	if mcpSlug == "" {
		mcpSlug = routeMcpSlug
	}
	if mcpSlug == "" {
		return none, oops.E(oops.CodeBadRequest, nil, "mcp slug is missing from remote login state").LogError(ctx, logger)
	}
	if routeMcpSlug != "" && routeMcpSlug != mcpSlug {
		return none, oops.E(oops.CodeUnauthorized, nil, "remote login state does not match this MCP server").LogError(ctx, logger)
	}
	if state.McpSlug != "" && state.McpSlug != mcpSlug {
		return none, oops.E(oops.CodeUnauthorized, nil, "remote login state does not match this MCP server").LogError(ctx, logger)
	}

	logger = logger.With(
		attr.SlogToolsetMCPSlug(mcpSlug),
		attr.SlogProjectID(state.ProjectID.String()),
	)
	if state.Resource != "" {
		logger = logger.With(attr.SlogOAuthResource(state.Resource))
	}

	// RFC 9207 §2.4: iss must be present and match before anything in the
	// response is trusted, denial included.
	if state.ExpectedIssuer != "" && q.Get("iss") != state.ExpectedIssuer {
		return none, oops.E(oops.CodeUnauthorized, nil, "remote login callback did not identify the expected issuer").LogWarn(ctx, logger,
			attr.SlogOAuthIssuer(state.ExpectedIssuer),
		)
	}

	if errCode != "" {
		if errCode == oautherr.CodeInvalidTarget {
			cause := oautherr.RFC6749Error{
				Code:        errCode,
				Description: truncateForMessage(q.Get("error_description")),
				URI:         truncateForMessage(q.Get("error_uri")),
			}
			return m.retryWithoutResource(ctx, logger, state, cause)
		}
		return none, denied(ctx, logger, q)
	}

	// Hoisted above the DB lookup + upstream code exchange so a state with a
	// missing/zero Subject fails fast — otherwise we burn the single-use
	// upstream authorization code on a request that can't produce a
	// remote_sessions row anyway.
	if state.Subject == nil || state.Subject.IsZero() {
		return none, oops.E(oops.CodeUnauthorized, nil, "remote login requires a stamped subject on the parent challenge").LogError(ctx, logger)
	}
	if err := state.Authority.ValidateLive(ctx, m.db); err != nil {
		return none, oops.E(oops.CodeUnauthorized, err, "private OAuth authority is no longer valid").LogError(ctx, logger)
	}
	if state.Authority.IsPrivate() {
		if m.privateAuthorityValidator == nil {
			return none, oops.E(oops.CodeUnauthorized, nil, "private MCP endpoint authority validator is unavailable").LogError(ctx, logger)
		}
		if err := m.privateAuthorityValidator(ctx, state); err != nil {
			return none, oops.E(oops.CodeUnauthorized, err, "private MCP endpoint authority is no longer valid").LogError(ctx, logger)
		}
	}

	queries := remotesessions_repo.New(m.db)
	clientRow, err := queries.GetRemoteSessionClientByID(ctx, remotesessions_repo.GetRemoteSessionClientByIDParams{
		ID:             state.RemoteSessionClientID,
		ProjectID:      state.ProjectID,
		OrganizationID: state.OrganizationID,
	})
	if err != nil {
		return none, oops.E(oops.CodeUnexpected, err, "load remote session client").LogError(ctx, logger)
	}
	client := clientRow.RemoteSessionClient

	var clientSecret string
	if client.ClientSecretEncrypted.Valid {
		decoded, derr := m.enc.Decrypt(client.ClientSecretEncrypted.String)
		if derr != nil {
			return none, oops.E(oops.CodeUnexpected, derr, "decrypt client secret").LogError(ctx, logger)
		}
		clientSecret = decoded
	}

	authMethod, err := ResolveTokenEndpointAuthMethod(client.TokenEndpointAuthMethod.String, clientSecret)
	if err != nil {
		return none, oops.E(oops.CodeUnauthorized, err, "the remote session client is misconfigured").LogError(ctx, logger)
	}
	audience := conv.FromPGTextOrEmpty[string](client.Audience)
	assertionIssuer := state.AssertionIssuer
	if authMethod == TokenEndpointAuthMethodPrivateKeyJWT && assertionIssuer == "" {
		// Rolling-deploy compatibility: the prior binary could mint a login state
		// without AssertionIssuer. The scoped client read above established
		// authority; this id-only joined read only recovers its issuer metadata.
		joined, jerr := queries.GetRemoteSessionClientWithIssuerByID(ctx, client.ID)
		if jerr != nil {
			return none, oops.E(oops.CodeUnexpected, jerr, "load the remote session client's assertion issuer").LogError(ctx, logger)
		}
		assertionIssuer = clientAssertionIssuer(joined.IssuerMetadata, joined.IssuerUrl)
	}
	assertionAudience, err := ResolveTokenEndpointAuthAudience(client.TokenEndpointAuthAudienceFormat.String, assertionIssuer, state.TokenEndpoint)
	if err != nil && authMethod == TokenEndpointAuthMethodPrivateKeyJWT {
		return none, oops.E(oops.CodeUnauthorized, err, "the remote session client's assertion audience is misconfigured").LogError(ctx, logger)
	}
	tok, err := m.exchangeCode(ctx, state, tokenEndpointClientAuth{
		Method:                authMethod,
		RemoteSessionClientID: client.ID,
		OrganizationID:        client.OrganizationID.String,
		JSONWebKeySetID:       client.JsonWebKeySetID.UUID,
		ClientID:              client.ClientID,
		ClientSecret:          clientSecret,
		AssertionAudience:     assertionAudience,
		AssertionSigner:       m.assertions,
	}, audience, code)
	if err != nil {
		var oauthErr oautherr.RFC6749Error
		if errors.As(err, &oauthErr) && oauthErr.Code == oautherr.CodeInvalidTarget {
			return m.retryWithoutResource(ctx, logger, state, err)
		}
		return none, oops.E(oops.CodeUnauthorized, err, "upstream token exchange failed").LogError(ctx, logger)
	}
	// The pair is live upstream from this line on, and every path out of here
	// that does not store it strands it: unreachable through Gram, and outside
	// the reach of every revoke path since no row points at it. So arm the
	// revocation on the exchange rather than on the first thing done with the
	// result — encrypting it can fail too — and disarm it once the row is
	// committed.
	stranded := true
	defer func() {
		if !stranded {
			return
		}
		m.revoker.RevokeUnstoredDetached(ctx, state.RemoteSessionClientID, tok.AccessToken, tok.RefreshToken)
	}()

	identity, interfaces := m.identityFromExchange(ctx, logger, tok, client.ID, client.ClientID, state.Nonce, state.OrganizationID)
	identityCols := identity.columns()
	enrichment, err := buildEnrichment(tok, identity, interfaces)
	if err != nil {
		logIdentityFailure(ctx, logger, "enrichment document dropped; session stored without it", err, attr.SlogRemoteSessionClientID(client.ID.String()))
		// The interface records survive the overflow: the id token rejection marker must outlive it.
		var noTokenResponse tokenResponse
		if enrichment, err = buildEnrichment(noTokenResponse, nil, interfaces); err != nil {
			logIdentityFailure(ctx, logger, "enrichment interfaces dropped; session stored without them", err, attr.SlogRemoteSessionClientID(client.ID.String()))
		}
	}

	accessEnc, err := m.enc.Encrypt([]byte(tok.AccessToken))
	if err != nil {
		return none, oops.E(oops.CodeUnexpected, err, "encrypt access token").LogError(ctx, logger)
	}
	var refreshEnc *string
	if tok.RefreshToken != "" {
		v, eerr := m.enc.Encrypt([]byte(tok.RefreshToken))
		if eerr != nil {
			return none, oops.E(oops.CodeUnexpected, eerr, "encrypt refresh token").LogError(ctx, logger)
		}
		refreshEnc = &v
	}

	// A token with no reported expiry — neither expires_in nor a JWT exp —
	// is stored with NULL access_expires_at, "no known expiry", rather than a
	// deadline the provider never asserted. validateAndRefresh serves that
	// token as-is; a refresh token does not imply that the access token
	// expires.
	now := time.Now()
	accessExpires := tok.AccessExpiresAt(now)

	// A deadline the provider asserts is persisted even when it has already
	// passed: with a refresh grant the first resolution refreshes, and the
	// session recovers on its own. With no refresh grant there is nothing to
	// recover with — the row would report the session as connected while
	// every resolution answered with a reconnect prompt, and a provider that
	// pins exp to its own session rather than to this grant would mint the
	// same dead row again on reconnect. exp is upstream wall-clock time, so a
	// host clock running ahead of the provider reaches this too. The margin
	// is the one the request path refreshes at, and the armed revocation
	// above returns the pair to the provider.
	if tok.RefreshToken == "" && accessExpires != nil && !accessExpires.After(now.Add(AccessTokenExpirySkew)) {
		return none, oops.E(oops.CodeUnauthorized, nil, "the identity provider issued an access token that is expired or about to expire, and no refresh token to renew it").LogWarn(ctx, logger,
			attr.SlogRemoteSessionAccessExpiresAt(*accessExpires),
		)
	}

	refreshTimeout, refreshTimeoutReported := tok.RefreshTokenTimeoutSeconds()
	refreshExpires := expirationDeadline(now, refreshTimeout, refreshTimeoutReported)
	authorizationLifetime, authorizationLifetimeReported := tok.AuthorizationLifetimeSeconds()
	authorizationExpires := expirationDeadline(now, authorizationLifetime, authorizationLifetimeReported)

	// RFC 6749 §5.1: a response that omits scope granted exactly what was
	// requested, so the requested set is what the session records.
	scopes := tok.Scopes()
	if len(scopes) == 0 {
		scopes = state.Scopes
	}
	if scopes == nil {
		scopes = []string{}
	}
	// Only the visible consent control opts a new session into scheduled
	// keepalive. On reconnect, UpsertRemoteSession preserves the stored choice.
	autoRefresh := false
	if state.AutoRefresh != nil {
		autoRefresh = *state.AutoRefresh
	}

	// The upstream exchange has already happened, so the token pair exists
	// either way; this transaction decides whether Gram stores it. The
	// client-row lock serializes the write against the issuer-delete orphan
	// cascade, which locks the same row before sweeping the client's
	// sessions: a callback that acquires the lock after that cascade
	// committed re-reads the binding as dead and is rejected here, instead
	// of resurrecting a grant no live issuer can reach or revoke. A rejected
	// callback leaves the revocation armed above still standing.
	dbtx, err := m.db.Begin(ctx)
	if err != nil {
		return none, oops.E(oops.CodeUnexpected, err, "begin remote session transaction").LogError(ctx, logger)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })
	txQueries := remotesessions_repo.New(dbtx)

	// No row means the client itself is gone, which the binding recheck below
	// rejects on its own — there is nothing left to serialize against.
	if _, err := txQueries.LockRemoteSessionClientForSessionWrite(ctx, state.RemoteSessionClientID); err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return none, oops.E(oops.CodeUnexpected, err, "lock remote session client").LogError(ctx, logger)
	}

	// Deliberately the issuer this login started from, not "any live binding
	// on the client": the consent that produced this code was given on that
	// issuer's surface, so its deletion ends the login rather than quietly
	// re-homing the grant onto a sibling the user was never shown. The user
	// re-authorizes through the surviving surface instead.
	bound, err := txQueries.CheckRemoteSessionClientBindingForUserSessionIssuer(ctx, remotesessions_repo.CheckRemoteSessionClientBindingForUserSessionIssuerParams{
		RemoteSessionClientID: state.RemoteSessionClientID,
		UserSessionIssuerID:   state.UserSessionIssuerID,
		ProjectID:             state.ProjectID,
		OrganizationID:        state.OrganizationID,
	})
	if err != nil {
		return none, oops.E(oops.CodeUnexpected, err, "recheck remote session client binding").LogError(ctx, logger)
	}
	if !bound {
		return none, oops.E(oops.CodeUnauthorized, nil, "the connection this login was started from no longer exists").LogWarn(ctx, logger)
	}

	storedSession, err := txQueries.UpsertRemoteSession(ctx, remotesessions_repo.UpsertRemoteSessionParams{
		SubjectUrn:            *state.Subject,
		UserSessionIssuerID:   state.UserSessionIssuerID,
		RemoteSessionClientID: state.RemoteSessionClientID,
		AccessTokenEncrypted:  accessEnc,
		AccessExpiresAt:       conv.PtrToPGTimestamptz(accessExpires),
		RefreshTokenEncrypted: conv.PtrToPGText(refreshEnc),
		AuthorizationExpiresAt: conv.PtrToPGTimestamptz(
			authorizationExpires,
		),
		RefreshExpiresAt:    conv.PtrToPGTimestamptz(refreshExpires),
		Scopes:              scopes,
		Resource:            conv.ToPGTextEmpty(state.Resource),
		AutoRefresh:         autoRefresh,
		UpstreamSubject:     identityCols.Subject,
		UpstreamEmail:       identityCols.Email,
		UpstreamDisplayName: identityCols.DisplayName,
		IdentitySource:      identityCols.Source,
		Enrichment:          enrichment,
	})
	if err != nil {
		return none, oops.E(oops.CodeUnexpected, err, "store remote session").LogError(ctx, logger)
	}

	if err := dbtx.Commit(ctx); err != nil {
		return none, oops.E(oops.CodeUnexpected, err, "commit remote session").LogError(ctx, logger)
	}
	stranded = false

	routeBase := state.RouteBase
	if routeBase == "" {
		routeBase = "mcp"
	}
	redirectBaseURL := m.serverURL.String()
	if state.Authority.IsPrivate() {
		redirectBaseURL = state.Authority.BaseURL
	} else if state.Authority.Surface == requestorigin.SurfaceCustomDomain && state.Authority.CustomDomainID.Valid {
		domain, derr := customdomainsrepo.New(m.db).GetCustomDomainByIDAndOrganization(ctx, customdomainsrepo.GetCustomDomainByIDAndOrganizationParams{
			ID: state.Authority.CustomDomainID.UUID, OrganizationID: state.Authority.OrganizationID,
		})
		if derr == nil && domain.Verified && domain.Activated && !domain.Deleted && "https://"+strings.ToLower(domain.Domain) == state.Authority.BaseURL {
			redirectBaseURL = state.Authority.BaseURL
		}
	}
	redirect := fmt.Sprintf("%s/%s/%s/connect?state=%s", strings.TrimRight(redirectBaseURL, "/"), routeBase, mcpSlug, url.QueryEscape(state.ParentChallengeID))
	if state.FinalRedirectURI != "" {
		redirect = state.FinalRedirectURI
	}
	return RemoteLoginResult{
		RedirectURL: redirect,
		Grant: &RemoteGrant{
			ParentChallengeID:      state.ParentChallengeID,
			UserSessionIssuerID:    state.UserSessionIssuerID,
			RemoteSessionClientID:  client.ID,
			Subject:                *state.Subject,
			RemoteSessionID:        storedSession.ID,
			RemoteSessionUpdatedAt: storedSession.UpdatedAt.Time,
		},
	}, nil
}

// denied rejects the callback; the public message echoes only IETF-registered error codes.
func denied(ctx context.Context, logger *slog.Logger, q url.Values) error {
	errCode := q.Get("error")
	message := "remote authn challenge denied"
	if oautherr.IsIETFRegisteredCode(errCode) {
		message += ": " + errCode
	}
	return oops.E(oops.CodeUnauthorized, nil, "%s", message).LogWarn(ctx, logger,
		attr.SlogOAuthError(truncateForMessage(errCode)),
		attr.SlogOAuthErrorDescription(truncateForMessage(q.Get("error_description"))),
	)
}

// retryWithoutResource mints one resource-less leg of the same login after
// invalid_target. Refused when the leg already omitted the resource or is the
// retry itself, so an issuer cannot loop the user. Persists nothing: the
// rejection may be of this resource alone, so it stays scoped to this login.
func (m *ChallengeManager) retryWithoutResource(ctx context.Context, logger *slog.Logger, state RemoteLoginState, cause error) (RemoteLoginResult, error) {
	var none RemoteLoginResult
	logger = logger.With(attr.SlogOAuthError(oautherr.CodeInvalidTarget))
	if state.Resource == "" || state.OmitResource || state.ResourceRetried {
		return none, oops.E(oops.CodeUnauthorized, cause, "the identity provider rejected the requested resource").LogWarn(ctx, logger)
	}

	clients, err := m.ListClients(ctx, state.ProjectID, state.OrganizationID, state.UserSessionIssuerID)
	if err != nil {
		return none, oops.E(oops.CodeUnexpected, err, "load remote session client for retry").LogError(ctx, logger)
	}
	idx := slices.IndexFunc(clients, func(c Client) bool { return c.ID == state.RemoteSessionClientID })
	if idx < 0 {
		return none, oops.E(oops.CodeUnauthorized, cause, "the connection this login was started from no longer exists").LogWarn(ctx, logger)
	}
	client := clients[idx]

	logger.WarnContext(ctx, "identity provider rejected the RFC 8707 resource parameter; retrying the login without it",
		attr.SlogOAuthIssuer(client.IssuerURL),
		attr.SlogRemoteSessionIssuerID(client.RemoteSessionIssuerID.String()),
		attr.SlogError(cause),
	)

	unsupported := false
	client.IssuerResourceIndicatorSupported = &unsupported
	authURL, err := m.mintAuthorization(ctx, state.parent(), client, true)
	if err != nil {
		return none, oops.E(oops.CodeUnexpected, err, "build authorization url for retry").LogError(ctx, logger)
	}
	return RemoteLoginResult{RedirectURL: authURL, Grant: nil}, nil
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
	clientAuth tokenEndpointClientAuth,
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
	if state.Resource != "" && !state.OmitResource {
		form.Set("resource", state.Resource)
	}

	req, err := newTokenEndpointRequest(ctx, state.TokenEndpoint, form, clientAuth)
	if err != nil {
		return tokenResponse{}, fmt.Errorf("new token request: %w", err)
	}

	resp, err := noRedirectClient(m.policy.PooledClient()).Do(req)
	if err != nil {
		return tokenResponse{}, fmt.Errorf("post token: %w", err)
	}
	defer o11y.NoLogDefer(func() error { return resp.Body.Close() })

	body, err := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	if err != nil {
		return tokenResponse{}, fmt.Errorf("read token response body: %w", err)
	}
	if resp.StatusCode/100 != 2 {
		if parsed, ok := oautherr.ParseTokenError(body); ok {
			parsed.Description = truncateForMessage(parsed.Description)
			parsed.URI = truncateForMessage(parsed.URI)
			return tokenResponse{}, fmt.Errorf("token endpoint %s: %w", resp.Status, parsed)
		}
		return tokenResponse{}, fmt.Errorf("token endpoint %s: %s", resp.Status, truncateForMessage(string(body)))
	}
	var tok tokenResponse
	if err := json.Unmarshal(body, &tok); err != nil {
		return tokenResponse{}, fmt.Errorf("decode token response: %w", err)
	}
	if tok.AccessToken == "" {
		return tokenResponse{}, errors.New("token endpoint returned no access_token")
	}
	tok.raw = body
	return tok, nil
}

// identityFromExchange decides who the exchanged grant belongs to: the ID
// token when the exchange returned one, else the issuer's userinfo endpoint.
// An ID token the issuer gives no key set to verify falls back to userinfo,
// which the access token itself authenticates; a rejected one yields no
// identity at all, since its nonce is the one signal of token substitution,
// and a disabled verifier captures none. The returned records name the
// enrichment interfaces that were asked.
func (m *ChallengeManager) identityFromExchange(ctx context.Context, logger *slog.Logger, tok tokenResponse, clientRowID uuid.UUID, externalClientID string, nonce string, organizationID string) (*UpstreamIdentity, map[string]interfaceRecord) {
	issuer, err := remotesessions_repo.New(m.db).GetRemoteSessionClientWithIssuerByID(ctx, clientRowID)
	if err != nil {
		logIdentityFailure(ctx, logger, "session stored without identity", fmt.Errorf("load issuer for identity capture: %w", err), attr.SlogRemoteSessionClientID(clientRowID.String()))
		return nil, nil
	}
	// An issuer with no published key set cannot have its tokens verified;
	// that is a configuration state, not an event worth a warning per grant.
	if tok.IDToken != "" && issuer.JwksUri.Valid && issuer.JwksUri.String != "" {
		identity, err := m.idTokens.Verify(ctx, tok.IDToken, IDTokenExpectation{
			issuer:      issuer.IssuerUrl,
			clientID:    externalClientID,
			jwksURI:     issuer.JwksUri.String,
			fetchScope:  issuer.RemoteSessionIssuerID.String(),
			signingAlgs: issuer.IDTokenSigningAlgValuesSupported,
			nonce:       nonce,
			subject:     "",
		})
		switch {
		case err == nil:
			return &identity, nil
		case errors.Is(err, errIDTokenVerificationDisabled):
			return nil, nil
		default:
			logIdentityFailure(ctx, logger, "upstream id token rejected; session stored without identity", err, attr.SlogOAuthIssuer(issuer.IssuerUrl), attr.SlogRemoteSessionClientID(clientRowID.String()))
			// The marker keeps a later verify from taking a weaker identity for this grant.
			return nil, map[string]interfaceRecord{IdentitySourceIDToken: {Status: interfaceStatusRejected, At: time.Now(), HTTPStatus: 0, Reason: "rejected at exchange"}}
		}
	}
	userinfo := m.enricher.userinfo(ctx, enrichmentTargetFromClient(issuer, organizationID), tok.AccessToken)
	if !userinfo.ran {
		return nil, nil
	}
	return userinfo.identity, map[string]interfaceRecord{IdentitySourceUserinfo: userinfo.interfaceRecord}
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
