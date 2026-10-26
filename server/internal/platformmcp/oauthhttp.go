//nolint:exhaustruct // OAuth handlers intentionally use zero values for non-wire Platform MCP contract fields and third-party request structs.
package platformmcp

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	_ "embed"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"log/slog"
	"mime"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/metric/noop"
	"golang.org/x/crypto/bcrypt"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/auth"
	"github.com/speakeasy-api/gram/server/internal/auth/identity"
	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/consentpage"
	"github.com/speakeasy-api/gram/server/internal/encryption"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/oautherr"
	"github.com/speakeasy-api/gram/server/internal/oauthui"
	"github.com/speakeasy-api/gram/server/internal/oauthwire"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/organizations/orgprovision"
	platformoauth "github.com/speakeasy-api/gram/server/internal/platformmcp/oauth"
	"github.com/speakeasy-api/gram/server/internal/sessiontokens"
	"github.com/speakeasy-api/gram/server/internal/signins"
	"github.com/speakeasy-api/gram/server/internal/urn"
	"github.com/speakeasy-api/gram/server/internal/usersessions"
	"github.com/speakeasy-api/gram/server/internal/usersessions/cimd"
	"github.com/speakeasy-api/gram/server/internal/usersessions/cimd/admission"
)

const (
	platformOAuthChallengePrefix = "platformMCPChallenge:"
	platformOAuthCodeLifetime    = 10 * time.Minute
	platformAccessTokenLifetime  = time.Hour
	platformRefreshTokenLifetime = 30 * 24 * time.Hour
)

//go:embed oauth_page.html
var oauthPageHTML string

var (
	oauthPageTemplate      = template.Must(template.Must(template.New("platform-mcp-oauth-page").Parse(oauthPageHTML)).Parse(consentpage.LogoTemplate))
	errPlatformMCPDisabled = fmt.Errorf("platform mcp disabled: %w", ErrForbidden)
	supportedGrantTypes    = []string{oauthwire.GrantTypeAuthorizationCode, oauthwire.GrantTypeRefreshToken}

	// supportedAuthMethods is the token_endpoint_auth_method set this
	// authorization server accepts, advertised in its RFC 8414 metadata and
	// enforced by RegisterHandler. Declared here rather than borrowed from
	// usersessions, whose list belongs to the separate user-session
	// authorization server: the two share RegistrationRequest but not a
	// client store, a token endpoint, or a set of supported methods, so a
	// method added there must be an explicit decision here.
	//
	// Every entry is either symmetric or public, which is what makes
	// RegisterHandler's "mint a secret unless the method is none" rule
	// correct. An asymmetric method would need a key source instead, and
	// this endpoint has nowhere to record one.
	supportedAuthMethods = []string{oauthwire.AuthMethodClientSecretBasic, oauthwire.AuthMethodClientSecretPost, oauthwire.AuthMethodNone}
)

type oauthPageData struct {
	Title string
	Kind  string
	// Styles is the shared authorization page stylesheet. renderOAuthPage
	// sets it.
	Styles template.CSS
	// ClientName is attacker-chosen for any CIMD-resolved client: it comes
	// from a document served at a URL the client picked. ClientIDOrigin is
	// the trust anchor that name lacks — the host the document was fetched
	// from, which the client cannot fake without controlling it — so the
	// consent page shows both whenever a client arrived by CIMD. It is
	// empty for dynamically registered clients, which have no origin to
	// vouch for them at all.
	ClientName       string
	ClientIDOrigin   string
	OrganizationName string
	Organizations    []OrganizationOption
	RedirectURI      string
	State            string
	CSRFToken        string
	ScriptNonce      string
	AutoClose        bool
	// WorkspaceName prefills the workspace form, and WorkspaceError explains
	// why a submitted name was refused.
	WorkspaceName  string
	WorkspaceError string
	// Invitations are the organizations a user with none may join instead
	// of creating a workspace.
	Invitations []WorkspaceInvitation
}

// BrowserIdentity resolves a real Speakeasy user from the product identity provider.
type BrowserIdentity interface {
	BuildAuthorizationURL(ctx context.Context, params identity.AuthorizationURLParams) (*url.URL, error)
	ExchangeCodeForTokens(ctx context.Context, code string) (*identity.IDPUserInfo, error)
	CompleteIDPLogin(ctx context.Context, idpUser *identity.IDPUserInfo, opts identity.IDPLoginOptions) (identity.IDPLoginResult, error)
}

type OrganizationOption struct {
	ID   string
	Name string
}

type OrganizationSelector interface {
	EligibleOrganizations(ctx context.Context, userID string) ([]OrganizationOption, error)
}

// WorkspaceInvitation is a live invitation to an existing organization.
type WorkspaceInvitation struct {
	ID               string
	OrganizationName string
	// RequiresSSO marks an organization whose SSO connection covers the
	// invitee's email when this sign-in did not go through it. It cannot be
	// joined until the user signs in with that SSO.
	RequiresSSO bool
}

// ErrInvitationRequiresSSO refuses to join an organization whose SSO
// connection covers the invitee's email when the user did not sign in
// through it.
var ErrInvitationRequiresSSO = errors.New("invitation requires signing in with SSO")

// WorkspaceProvisioner creates the first organization for a user who signs in
// with no organization, so a new user can connect without visiting the
// dashboard first. A user with a pending invitation to their verified email
// joins that organization instead. ProvisionWorkspace must refuse a user who
// already belongs to an organization or has a pending invitation. A nil provisioner in
// OAuthHTTPConfig refuses a user with no organization instead.
type WorkspaceProvisioner interface {
	SuggestedWorkspaceName(ctx context.Context, userID string) (string, error)
	ProvisionWorkspace(ctx context.Context, userID, name string) (OrganizationOption, error)
	PendingInvitations(ctx context.Context, verifiedEmail, ssoOrganizationID string) ([]WorkspaceInvitation, error)
	JoinInvitedWorkspace(ctx context.Context, userID, verifiedEmail, ssoOrganizationID, invitationID string) (OrganizationOption, error)
}

type oauthChallenge struct {
	ID             string    `json:"id"`
	ClientID       string    `json:"client_id"`
	RedirectURI    string    `json:"redirect_uri"`
	State          string    `json:"state,omitempty"`
	CodeChallenge  string    `json:"code_challenge"`
	CSRFToken      string    `json:"csrf_token"`
	OrganizationID string    `json:"organization_id"`
	Subject        string    `json:"subject,omitempty"`
	CreatedAt      time.Time `json:"created_at"`

	// BaseURL is the platform origin the authorization started on. The
	// browser legs after the identity provider return there, and the
	// authorization code is bound to that origin's resource. Empty means the
	// configured base URL.
	BaseURL string `json:"base_url,omitempty"`

	// VerifiedEmail is the signed-in user's email when the identity provider
	// reports it verified. Only it may claim an organization invitation, as
	// following the emailed invite link would.
	VerifiedEmail string `json:"verified_email,omitempty"`
	// SSOOrganizationID is the WorkOS organization whose SSO connection the
	// user signed in through, empty for any other method. An invitation that
	// organization's SSO covers can be joined only from such a sign-in.
	SSOOrganizationID string `json:"sso_organization_id,omitempty"`
}

func (c oauthChallenge) CacheKey() string              { return platformOAuthChallengePrefix + c.ID }
func (c oauthChallenge) AdditionalCacheKeys() []string { return []string{} }
func (c oauthChallenge) TTL() time.Duration            { return platformOAuthCodeLifetime }

var _ cache.CacheableObject[oauthChallenge] = (*oauthChallenge)(nil)

// OAuthHTTP serves the Platform MCP-owned authorization server. It deliberately does
// not import hosted MCP runtime or persistence packages.
//
// The resource, issuer, and access token audience follow the platform origin
// a request arrives on (the server URL or an extra platform host), so each
// platform host is its own RFC 8707 resource with its own authorization
// server metadata. Requests from any other surface use baseURL.
type OAuthHTTP struct {
	// baseURL is the configured server URL. It is the origin for requests not
	// stamped as a platform host.
	baseURL       *url.URL
	environment   string
	cache         cache.TypedCacheObject[oauthChallenge]
	store         platformoauth.Store
	identity      BrowserIdentity
	gate          Gate
	authorizer    Authorizer
	organizations OrganizationSelector
	workspaces    WorkspaceProvisioner
	signer        *sessiontokens.Signer
	credentials   *CredentialCodec
	telemetry     OAuthTelemetry
	now           func() time.Time
	logger        *slog.Logger
	// cimd resolves URL-shaped client_ids against their Client ID Metadata
	// Documents. Nil disables inbound CIMD entirely: such a client_id then
	// falls through to an ordinary registry lookup and is rejected as
	// unknown, and the AS metadata stops advertising support.
	cimd          clientMetadataResolver
	cimdAdmission *admission.Metrics
	// idpCallbackBaseURL hosts the identity provider callback. The identity
	// provider stores the callback URL, so it stays pinned when the server
	// URL moves.
	idpCallbackBaseURL *url.URL
}

type OAuthHTTPConfig struct {
	BaseURL       *url.URL
	Environment   string
	Cache         cache.Cache
	Store         platformoauth.Store
	Identity      BrowserIdentity
	Gate          Gate
	Authorizer    Authorizer
	Organizations OrganizationSelector
	Workspaces    WorkspaceProvisioner
	Signer        *sessiontokens.Signer
	Encryption    *encryption.Client
	Telemetry     OAuthTelemetry
	Logger        *slog.Logger
	// GuardianPolicy backs the CIMD document fetcher's SSRF protection.
	// Nil leaves inbound CIMD disabled.
	GuardianPolicy *guardian.Policy
	MeterProvider  metric.MeterProvider
	// IDPCallbackBaseURL is the pinned origin of the identity provider
	// callback. Nil uses BaseURL.
	IDPCallbackBaseURL *url.URL
}

func NewOAuthHTTP(config OAuthHTTPConfig) (*OAuthHTTP, error) {
	if config.BaseURL == nil || config.BaseURL.Scheme == "" || config.BaseURL.Host == "" || config.Cache == nil || config.Store == nil || config.Identity == nil || config.Gate == nil || config.Authorizer == nil || config.Organizations == nil || config.Signer == nil || config.Encryption == nil {
		return nil, errors.New("platform oauth http configuration is incomplete")
	}
	credentials, err := NewCredentialCodec(config.Encryption)
	if err != nil {
		return nil, err
	}
	logger := config.Logger
	if logger == nil {
		logger = slog.New(slog.DiscardHandler)
	}
	meterProvider := config.MeterProvider
	if meterProvider == nil {
		meterProvider = noop.NewMeterProvider()
	}
	// A nil guardian policy leaves resolver nil, which disables inbound
	// CIMD rather than fetching documents through an unguarded client.
	var resolver clientMetadataResolver
	if config.GuardianPolicy != nil {
		resolver = cimd.NewResolver(config.GuardianPolicy, meterProvider, logger)
	}
	baseURL := *config.BaseURL
	idpCallbackBaseURL := baseURL
	if config.IDPCallbackBaseURL != nil {
		idpCallbackBaseURL = *config.IDPCallbackBaseURL
	}
	return &OAuthHTTP{
		baseURL:       &baseURL,
		environment:   config.Environment,
		cache:         cache.NewTypedObjectCache[oauthChallenge](nil, config.Cache, cache.SuffixNone),
		store:         config.Store,
		identity:      config.Identity,
		gate:          config.Gate,
		authorizer:    config.Authorizer,
		organizations: config.Organizations,
		workspaces:    config.Workspaces,
		signer:        config.Signer,
		credentials:   credentials,
		telemetry:     config.Telemetry,
		now:           time.Now,
		logger:        logger,
		cimd:          resolver,
		cimdAdmission: admission.NewMetrics(meterProvider, logger),

		idpCallbackBaseURL: &idpCallbackBaseURL,
	}, nil
}

func (s *OAuthHTTP) oauthTelemetry() OAuthTelemetry {
	if s.telemetry == nil {
		return noopOAuthTelemetry{}
	}
	return s.telemetry
}

func (s *OAuthHTTP) Attach(mux interface {
	Handle(string, string, http.HandlerFunc)
}) {
	mux.Handle("GET", "/.well-known/oauth-protected-resource/platform-mcp", handlerFunc(s.ProtectedResourceHandler()))
	mux.Handle("GET", "/.well-known/oauth-authorization-server/platform-mcp", handlerFunc(s.AuthorizationServerHandler()))
	mux.Handle("POST", "/platform-mcp/register", handlerFunc(s.RegisterHandler()))
	mux.Handle("GET", "/platform-mcp/authorize", handlerFunc(s.AuthorizeHandler()))
	mux.Handle("GET", "/platform-mcp/idp_callback", handlerFunc(s.IDPCallbackHandler()))
	mux.Handle("GET", "/platform-mcp/select-organization", handlerFunc(s.OrganizationSelectionHandler()))
	mux.Handle("POST", "/platform-mcp/select-organization", handlerFunc(s.OrganizationSelectionHandler()))
	mux.Handle("POST", "/platform-mcp/create-workspace", handlerFunc(s.CreateWorkspaceHandler()))
	mux.Handle("POST", "/platform-mcp/join-workspace", handlerFunc(s.JoinWorkspaceHandler()))
	mux.Handle("GET", "/platform-mcp/connect", handlerFunc(s.ConnectHandler()))
	mux.Handle("POST", "/platform-mcp/connect", handlerFunc(s.ConnectHandler()))
	mux.Handle("GET", "/platform-mcp/provider-setup-complete", handlerFunc(s.ProviderSetupCompleteHandler()))
	mux.Handle("POST", "/platform-mcp/token", handlerFunc(s.TokenHandler()))
	mux.Handle("POST", "/platform-mcp/revoke", handlerFunc(s.RevokeHandler()))
}

func handlerFunc(handler http.Handler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) { handler.ServeHTTP(w, r) }
}

func (s *OAuthHTTP) ProtectedResourceHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resource := s.resource(r.Context())
		writeJSON(w, http.StatusOK, map[string]any{"resource": resource, "authorization_servers": []string{resource}, "bearer_methods_supported": []string{"header"}})
	})
}

func (s *OAuthHTTP) AuthorizationServerHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		base := platformBaseURL(r.Context(), s.baseURL)
		metadata := map[string]any{
			"issuer":                                platformResource(base),
			"authorization_endpoint":                endpointURL(base, "authorize"),
			"token_endpoint":                        endpointURL(base, "token"),
			"registration_endpoint":                 endpointURL(base, "register"),
			"revocation_endpoint":                   endpointURL(base, "revoke"),
			"response_types_supported":              usersessions.SupportedResponseTypes,
			"grant_types_supported":                 supportedGrantTypes,
			"token_endpoint_auth_methods_supported": supportedAuthMethods,
			"code_challenge_methods_supported":      usersessions.SupportedCodeChallengeMethods,
		}
		// Advertised only when a document can actually be resolved and the
		// admission mode admits something, and omitted rather than sent as
		// false otherwise: an absent member is how RFC 8414 metadata says
		// "unsupported", and it is what the hosted authorization server
		// emits. Advertising support while admitting nothing would route
		// spec-compliant clients into a guaranteed-failure flow instead of
		// letting them fall back to dynamic client registration.
		if s.cimd != nil && platformCIMDAdmissionMode != admission.ModeDisabled {
			metadata["client_id_metadata_document_supported"] = true
		}
		writeJSON(w, http.StatusOK, metadata)
	})
}

func (s *OAuthHTTP) RegisterHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !requireJSON(w, r, 64<<10) {
			return
		}
		var request usersessions.RegistrationRequest
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			writeOAuthError(w, http.StatusBadRequest, "invalid_client_metadata", "request body is not valid JSON")
			return
		}
		request.SetDefaults()
		if err := request.Validate(supportedGrantTypes, supportedAuthMethods); err != nil {
			writeRequestOAuthError(w, http.StatusBadRequest, err)
			return
		}
		clientID := "client_" + uuid.NewString()
		var secret, secretHash string
		if request.TokenEndpointAuthMethod != oauthwire.AuthMethodNone {
			var err error
			secret, err = opaqueToken()
			if err != nil {
				writeOAuthError(w, http.StatusInternalServerError, "server_error", "could not register client")
				return
			}
			hash, err := bcrypt.GenerateFromPassword([]byte(secret), bcrypt.DefaultCost)
			if err != nil {
				writeOAuthError(w, http.StatusInternalServerError, "server_error", "could not register client")
				return
			}
			secretHash = string(hash)
		}
		if err := s.store.RegisterClient(r.Context(), platformoauth.Client{ID: clientID, SecretHash: secretHash, Name: request.ClientName, RedirectURIs: request.RedirectURIs, SecretExpiresAt: nil, RevokedAt: nil}); err != nil {
			writeOAuthError(w, http.StatusInternalServerError, "server_error", "could not register client")
			return
		}
		response := map[string]any{"client_id": clientID, "client_id_issued_at": time.Now().Unix(), "client_name": request.ClientName, "redirect_uris": request.RedirectURIs, "grant_types": request.GrantTypes, "response_types": request.ResponseTypes, "token_endpoint_auth_method": request.TokenEndpointAuthMethod}
		if secret != "" {
			response["client_secret"] = secret
			response["client_secret_expires_at"] = 0
		}
		writeJSON(w, http.StatusCreated, response)
	})
}

func (s *OAuthHTTP) AuthorizeHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		request := usersessions.AuthorizationRequestFromQuery(r.URL.Query())
		request.SetDefaults()
		if err := request.ValidateRedirectableFields(); err != nil {
			writeBrowserRequestOAuthError(r.Context(), w, http.StatusBadRequest, err)
			return
		}
		if err := oauthwire.ValidateRedirectURI(request.RedirectURI); err != nil {
			writeBrowserRequestOAuthError(r.Context(), w, http.StatusBadRequest, err)
			return
		}
		// URL-shaped client_ids resolve via CIMD here (admission-gated,
		// inside the resolver). Every resolution failure renders inline per
		// RFC 6749 §4.1.2.1 — the redirect_uri of an unresolved client
		// cannot be trusted — and a document fetch failure aborts the
		// request per draft-ietf-oauth-client-id-metadata-document-02 §5.1
		// (fail closed, no stale fallback).
		client, err := s.resolveClient(r.Context(), request.ClientID, resolveClientCIMD)
		if err != nil {
			s.writeClientResolutionError(w, r.Context(), request.ClientID, err)
			return
		}
		if !clientRedirectAllowed(client, request.RedirectURI) {
			writeBrowserOAuthError(r.Context(), w, http.StatusBadRequest, oautherr.CodeInvalidClient, "unknown client_id or redirect_uri", oauthui.ReasonInvalidClient)
			return
		}
		if err := request.ValidatePostRedirect(); err != nil {
			writePreAuthenticationOAuthError(w, r, client, request.ClientID, request.RedirectURI, request.State, err)
			return
		}

		csrfToken, err := opaqueToken()
		if err != nil {
			writeBrowserOAuthError(r.Context(), w, http.StatusInternalServerError, "server_error", "could not start authorization", oauthui.ReasonDefault)
			return
		}
		challenge := oauthChallenge{ID: uuid.NewString(), ClientID: request.ClientID, RedirectURI: request.RedirectURI, State: request.State, CodeChallenge: request.CodeChallenge, CSRFToken: csrfToken, OrganizationID: "", Subject: "", CreatedAt: time.Now(), BaseURL: platformBaseURL(r.Context(), s.baseURL).String()}
		if err := s.cache.Store(r.Context(), challenge); err != nil {
			writeBrowserOAuthError(r.Context(), w, http.StatusServiceUnavailable, "temporarily_unavailable", "could not start authorization", oauthui.ReasonDefault)
			return
		}
		// The identity provider only accepts registered redirect URIs, so the
		// callback stays on the pinned callback origin for every platform host;
		// the challenge carries the origin to return to afterwards.
		callback := endpointURL(s.idpCallbackBaseURL, "idp_callback")
		idpURL, err := s.identity.BuildAuthorizationURL(r.Context(), identity.AuthorizationURLParams{CallbackURL: callback, State: challenge.ID, Scope: "", ScopesSupported: nil})
		if err != nil {
			writeBrowserOAuthError(r.Context(), w, http.StatusInternalServerError, "server_error", "could not start login", oauthui.ReasonDefault)
			return
		}
		http.Redirect(w, r, idpURL.String(), http.StatusFound)
	})
}

func (s *OAuthHTTP) IDPCallbackHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		challenge, err := s.cache.GetAndDelete(r.Context(), platformOAuthChallengePrefix+r.URL.Query().Get("state"))
		if err != nil {
			writeBrowserOAuthError(r.Context(), w, http.StatusBadRequest, oautherr.CodeInvalidRequest, "authorization state is invalid or expired", oauthui.ReasonExpired)
			return
		}
		if idpError := r.URL.Query().Get("error"); idpError != "" {
			redirectOAuthError(w, r, challenge.RedirectURI, challenge.State, &oauthwire.Error{Code: idpError, Description: r.URL.Query().Get("error_description")})
			return
		}
		idpCode := r.URL.Query().Get("code")
		if idpCode == "" {
			writeBrowserOAuthError(r.Context(), w, http.StatusBadRequest, "invalid_request", "code is required", oauthui.ReasonDefault)
			return
		}
		idpUser, err := s.identity.ExchangeCodeForTokens(r.Context(), idpCode)
		if err != nil {
			writeBrowserOAuthError(r.Context(), w, http.StatusBadRequest, oautherr.CodeAccessDenied, "login could not be completed", oauthui.ReasonDefault)
			return
		}
		login, err := s.identity.CompleteIDPLogin(r.Context(), idpUser, identity.IDPLoginOptions{SkipMembershipSync: false})
		if err != nil {
			writeBrowserOAuthError(r.Context(), w, http.StatusInternalServerError, "server_error", "login could not be completed", oauthui.ReasonDefault)
			return
		}
		challenge.ID = uuid.NewString()
		challenge.Subject = urn.NewUserSubject(login.UserID).String()
		challenge.VerifiedEmail = idpUser.Evidence.VerifiedEmail
		if idpUser.AuthenticationMethod == signins.MethodSSO {
			challenge.SSOOrganizationID = idpUser.OrganizationID
		}
		if err := s.cache.Store(r.Context(), challenge); err != nil {
			writeBrowserOAuthError(r.Context(), w, http.StatusServiceUnavailable, "temporarily_unavailable", "could not continue authorization", oauthui.ReasonDefault)
			return
		}
		http.Redirect(w, r, endpointURL(s.challengeBaseURL(challenge), "select-organization")+"?state="+url.QueryEscape(challenge.ID), http.StatusFound)
	})
}

func (s *OAuthHTTP) OrganizationSelectionHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			s.organizationSelectionGet(w, r)
		case http.MethodPost:
			s.organizationSelectionPost(w, r)
		default:
			w.Header().Set("Allow", "GET, POST")
			writeBrowserOAuthError(r.Context(), w, http.StatusMethodNotAllowed, oautherr.CodeInvalidRequest, "method not allowed", oauthui.ReasonInvalidLink)
		}
	})
}

func (s *OAuthHTTP) organizationSelectionGet(w http.ResponseWriter, r *http.Request) {
	challenge, err := s.cache.Get(r.Context(), platformOAuthChallengePrefix+r.URL.Query().Get("state"))
	if err != nil || challenge.Subject == "" {
		writeBrowserOAuthError(r.Context(), w, http.StatusBadRequest, oautherr.CodeInvalidRequest, "authorization state is invalid or expired", oauthui.ReasonExpired)
		return
	}
	subject, err := urn.ParseSessionSubject(challenge.Subject)
	if err != nil || subject.Kind != urn.SessionSubjectKindUser {
		writeBrowserOAuthError(r.Context(), w, http.StatusBadRequest, oautherr.CodeAccessDenied, "authorization identity is invalid", oauthui.ReasonSessionChanged)
		return
	}
	organizations, err := s.organizations.EligibleOrganizations(r.Context(), subject.ID)
	if err != nil {
		writeBrowserOAuthError(r.Context(), w, http.StatusInternalServerError, "server_error", "could not load eligible organizations", oauthui.ReasonDefault)
		return
	}
	if len(organizations) == 0 && s.workspaces == nil {
		writeBrowserOAuthError(r.Context(), w, http.StatusForbidden, "access_denied", "no eligible organizations are available", oauthui.ReasonDefault)
		return
	}

	// Lookup only: authorize already resolved and persisted any CIMD row,
	// and a mid-flow leg must keep working even if the document host has
	// since become unreachable.
	client, err := s.resolveClient(r.Context(), challenge.ClientID, lookupClientOnly)
	if err != nil {
		writeBrowserOAuthError(r.Context(), w, http.StatusBadRequest, oautherr.CodeInvalidClient, "authorization client is unavailable", oauthui.ReasonInvalidClient)
		return
	}
	if len(organizations) == 0 {
		if s.renderPendingInvitations(w, r, client.Name, challenge) {
			return
		}
		// A suggestion is a convenience. Without one the user types a name.
		suggestion, err := s.workspaces.SuggestedWorkspaceName(r.Context(), subject.ID)
		if err != nil {
			s.logger.WarnContext(r.Context(), "could not suggest a Platform MCP workspace name", attr.SlogError(err))
		}
		s.renderWorkspacePage(r.Context(), w, client.Name, challenge, suggestion, "")
		return
	}
	s.renderOAuthPage(r.Context(), w, oauthPageData{
		Title:         "Choose an organization · Speakeasy",
		Kind:          "organization",
		ClientName:    client.Name,
		Organizations: organizations,
		RedirectURI:   challenge.RedirectURI,
		State:         challenge.ID,
		CSRFToken:     challenge.CSRFToken,
	})
}

func (s *OAuthHTTP) organizationSelectionPost(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 16<<10)
	if err := r.ParseForm(); err != nil {
		writeBrowserOAuthError(r.Context(), w, http.StatusBadRequest, "invalid_request", "could not parse organization selection", oauthui.ReasonDefault)
		return
	}
	challenge, err := s.cache.GetAndDelete(r.Context(), platformOAuthChallengePrefix+r.PostForm.Get("state"))
	if err != nil || challenge.Subject == "" {
		writeBrowserOAuthError(r.Context(), w, http.StatusBadRequest, oautherr.CodeInvalidRequest, "authorization state is invalid or expired", oauthui.ReasonExpired)
		return
	}
	if challenge.CSRFToken == "" || subtle.ConstantTimeCompare([]byte(r.PostForm.Get("csrf_token")), []byte(challenge.CSRFToken)) != 1 {
		writeBrowserOAuthError(r.Context(), w, http.StatusBadRequest, oautherr.CodeInvalidRequest, "organization selection is invalid", oauthui.ReasonSessionChanged)
		return
	}
	subject, err := urn.ParseSessionSubject(challenge.Subject)
	if err != nil || subject.Kind != urn.SessionSubjectKindUser {
		writeBrowserOAuthError(r.Context(), w, http.StatusBadRequest, oautherr.CodeAccessDenied, "authorization identity is invalid", oauthui.ReasonSessionChanged)
		return
	}
	organizations, err := s.organizations.EligibleOrganizations(r.Context(), subject.ID)
	if err != nil {
		writeBrowserOAuthError(r.Context(), w, http.StatusInternalServerError, "server_error", "could not load eligible organizations", oauthui.ReasonDefault)
		return
	}
	selectedID := r.PostForm.Get("organization_id")
	for _, organization := range organizations {
		if organization.ID == selectedID {
			challenge.OrganizationID = selectedID
			if err := s.cache.Store(r.Context(), challenge); err != nil {
				writeBrowserOAuthError(r.Context(), w, http.StatusServiceUnavailable, "temporarily_unavailable", "could not continue authorization", oauthui.ReasonDefault)
				return
			}
			http.Redirect(w, r, endpointURL(s.challengeBaseURL(challenge), "connect")+"?state="+url.QueryEscape(challenge.ID), http.StatusSeeOther)
			return
		}
	}
	writeBrowserOAuthError(r.Context(), w, http.StatusForbidden, "access_denied", "selected organization is not available", oauthui.ReasonDefault)
}

func (s *OAuthHTTP) renderWorkspacePage(ctx context.Context, w http.ResponseWriter, clientName string, challenge oauthChallenge, name, problem string) {
	s.renderOAuthPage(ctx, w, oauthPageData{
		Title:          "Name your organization · Speakeasy",
		Kind:           "workspace",
		ClientName:     clientName,
		RedirectURI:    challenge.RedirectURI,
		State:          challenge.ID,
		CSRFToken:      challenge.CSRFToken,
		WorkspaceName:  name,
		WorkspaceError: problem,
	})
}

func (s *OAuthHTTP) renderInvitationPage(ctx context.Context, w http.ResponseWriter, clientName string, challenge oauthChallenge, invitations []WorkspaceInvitation) {
	s.renderOAuthPage(ctx, w, oauthPageData{
		Title:       "Join your team · Speakeasy",
		Kind:        "invitation",
		ClientName:  clientName,
		RedirectURI: challenge.RedirectURI,
		State:       challenge.ID,
		CSRFToken:   challenge.CSRFToken,
		Invitations: invitations,
	})
}

const ssoRequiredDescription = "Your invitation requires your company's single sign-on. Sign in with SSO, then connect again."

// renderPendingInvitations shows a user with no organization the
// invitations waiting for their verified email, so an invited user joins
// their team rather than starting a second organization beside it. An
// invitation that requires SSO is offered only after an SSO sign-in; when
// every invitation needs one, the user is told to sign in with SSO instead.
// It reports whether it wrote a response.
func (s *OAuthHTTP) renderPendingInvitations(w http.ResponseWriter, r *http.Request, clientName string, challenge oauthChallenge) bool {
	if challenge.VerifiedEmail == "" {
		return false
	}
	invitations, err := s.workspaces.PendingInvitations(r.Context(), challenge.VerifiedEmail, challenge.SSOOrganizationID)
	if err != nil {
		s.logger.ErrorContext(r.Context(), "could not load Platform MCP workspace invitations", attr.SlogError(err))
		writeBrowserOAuthError(r.Context(), w, http.StatusInternalServerError, "server_error", "could not load pending invitations", oauthui.ReasonDefault)
		return true
	}
	if len(invitations) == 0 {
		return false
	}
	if !slices.ContainsFunc(invitations, func(invitation WorkspaceInvitation) bool { return !invitation.RequiresSSO }) {
		s.renderOAuthPage(r.Context(), w, oauthPageData{
			Title:       "Sign in with SSO · Speakeasy",
			Kind:        "sso-required",
			ClientName:  clientName,
			Invitations: invitations,
		})
		return true
	}
	s.renderInvitationPage(r.Context(), w, clientName, challenge, invitations)
	return true
}

// workspaceFormChallenge reads the challenge a workspace form posts back. It
// accepts only the state and CSRF token of a challenge that already carries
// the user and no organization, so the form cannot be driven from outside the
// flow. The challenge is not consumed. ok is false once an error is written.
func (s *OAuthHTTP) workspaceFormChallenge(w http.ResponseWriter, r *http.Request) (challenge oauthChallenge, userID string, ok bool) {
	if s.workspaces == nil {
		writeBrowserOAuthError(r.Context(), w, http.StatusNotFound, "invalid_request", "organization creation is not available", oauthui.ReasonDefault)
		return challenge, "", false
	}
	r.Body = http.MaxBytesReader(w, r.Body, 16<<10)
	if err := r.ParseForm(); err != nil {
		writeBrowserOAuthError(r.Context(), w, http.StatusBadRequest, "invalid_request", "could not parse organization form", oauthui.ReasonDefault)
		return challenge, "", false
	}
	challenge, err := s.cache.Get(r.Context(), platformOAuthChallengePrefix+r.PostForm.Get("state"))
	if err != nil || challenge.Subject == "" || challenge.OrganizationID != "" {
		writeBrowserOAuthError(r.Context(), w, http.StatusUnauthorized, "invalid_request", "authorization state is invalid or expired", oauthui.ReasonExpired)
		return challenge, "", false
	}
	if challenge.CSRFToken == "" || subtle.ConstantTimeCompare([]byte(r.PostForm.Get("csrf_token")), []byte(challenge.CSRFToken)) != 1 {
		writeBrowserOAuthError(r.Context(), w, http.StatusUnauthorized, "invalid_request", "organization request is invalid", oauthui.ReasonSessionChanged)
		return challenge, "", false
	}
	subject, err := urn.ParseSessionSubject(challenge.Subject)
	if err != nil || subject.Kind != urn.SessionSubjectKindUser {
		writeBrowserOAuthError(r.Context(), w, http.StatusUnauthorized, "access_denied", "authorization identity is invalid", oauthui.ReasonSessionChanged)
		return challenge, "", false
	}
	return challenge, subject.ID, true
}

// consumeWorkspaceChallenge deletes the challenge before anything is created
// or joined, so a replayed or doubled submit cannot act twice. It checks the
// challenge again, because another submit can change it between the read and
// the delete. ok is false once an error is written.
func (s *OAuthHTTP) consumeWorkspaceChallenge(w http.ResponseWriter, r *http.Request) (oauthChallenge, bool) {
	challenge, err := s.cache.GetAndDelete(r.Context(), platformOAuthChallengePrefix+r.PostForm.Get("state"))
	if err != nil || challenge.Subject == "" || challenge.OrganizationID != "" {
		writeBrowserOAuthError(r.Context(), w, http.StatusUnauthorized, "invalid_request", "authorization state is invalid or expired", oauthui.ReasonExpired)
		return challenge, false
	}
	return challenge, true
}

// continueWithOrganization moves a consumed challenge on to the consent page
// for the organization the user just created or joined. A fresh state means a
// stale submit that still holds the old one cannot consume the challenge this
// request continues with.
func (s *OAuthHTTP) continueWithOrganization(w http.ResponseWriter, r *http.Request, challenge oauthChallenge, organization OrganizationOption) {
	challenge.ID = uuid.NewString()
	challenge.OrganizationID = organization.ID
	if err := s.cache.Store(r.Context(), challenge); err != nil {
		writeBrowserOAuthError(r.Context(), w, http.StatusServiceUnavailable, "temporarily_unavailable", "could not continue authorization", oauthui.ReasonDefault)
		return
	}
	http.Redirect(w, r, endpointURL(s.challengeBaseURL(challenge), "connect")+"?state="+url.QueryEscape(challenge.ID), http.StatusSeeOther)
}

// CreateWorkspaceHandler creates the first organization for a signed-in user
// who has none, then continues to the consent page for it. A user with a
// pending invitation is shown the invitation instead.
func (s *OAuthHTTP) CreateWorkspaceHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		challenge, userID, ok := s.workspaceFormChallenge(w, r)
		if !ok {
			return
		}

		client, err := s.resolveClient(r.Context(), challenge.ClientID, lookupClientOnly)
		if err != nil {
			writeBrowserOAuthError(r.Context(), w, http.StatusUnauthorized, "invalid_client", "authorization client is unavailable", oauthui.ReasonDefault)
			return
		}
		if s.renderPendingInvitations(w, r, client.Name, challenge) {
			return
		}

		// A refused name keeps the challenge, so the user can correct it.
		submitted := r.PostForm.Get("workspace_name")
		if _, err := orgprovision.ValidateName(submitted); err != nil {
			// An empty name gets the page's own wording rather than the
			// validator's "org name is required".
			problem := "Enter an organization name."
			if shareable, ok := errors.AsType[*oops.ShareableError](err); ok && strings.TrimSpace(submitted) != "" {
				problem = shareable.Error()
			}
			s.renderWorkspacePage(r.Context(), w, client.Name, challenge, submitted, problem)
			return
		}

		challenge, ok = s.consumeWorkspaceChallenge(w, r)
		if !ok {
			return
		}
		organization, err := s.workspaces.ProvisionWorkspace(r.Context(), userID, submitted)
		switch {
		case errors.Is(err, auth.ErrUserHasPendingInvitation):
			redirectOAuthError(w, r, challenge.RedirectURI, challenge.State, &oauthwire.Error{Code: "access_denied", Description: "You have a pending invitation to a Speakeasy organization. Accept it from the invitation email, then connect again."})
			return
		case errors.Is(err, auth.ErrUserHasOrganization), errors.Is(err, auth.ErrWorkspaceCreationInProgress):
			s.logger.WarnContext(r.Context(), "Platform MCP workspace creation refused", attr.SlogError(err))
			redirectOAuthError(w, r, challenge.RedirectURI, challenge.State, &oauthwire.Error{Code: "access_denied", Description: "Your account already has a Speakeasy organization, or one is being created. Connect again to choose it."})
			return
		}
		if err != nil {
			s.logger.ErrorContext(r.Context(), "could not create Platform MCP workspace", attr.SlogError(err))
			redirectOAuthError(w, r, challenge.RedirectURI, challenge.State, &oauthwire.Error{Code: "access_denied", Description: "Your organization could not be created. Sign in to the Speakeasy dashboard to create one, then connect again."})
			return
		}
		s.continueWithOrganization(w, r, challenge, organization)
	})
}

// JoinWorkspaceHandler accepts a pending invitation for a signed-in user who
// has no organization, then continues to the consent page for the
// organization they joined.
func (s *OAuthHTTP) JoinWorkspaceHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, userID, ok := s.workspaceFormChallenge(w, r)
		if !ok {
			return
		}
		challenge, ok := s.consumeWorkspaceChallenge(w, r)
		if !ok {
			return
		}
		organization, err := s.workspaces.JoinInvitedWorkspace(r.Context(), userID, challenge.VerifiedEmail, challenge.SSOOrganizationID, r.PostForm.Get("invitation_id"))
		if errors.Is(err, ErrInvitationRequiresSSO) {
			redirectOAuthError(w, r, challenge.RedirectURI, challenge.State, &oauthwire.Error{Code: "access_denied", Description: ssoRequiredDescription})
			return
		}
		if err != nil {
			s.logger.ErrorContext(r.Context(), "could not join Platform MCP workspace by invitation", attr.SlogError(err))
			redirectOAuthError(w, r, challenge.RedirectURI, challenge.State, &oauthwire.Error{Code: "access_denied", Description: "Your invitation could not be accepted. Ask your teammate for a new invitation, then connect again."})
			return
		}
		s.continueWithOrganization(w, r, challenge, organization)
	})
}

func (s *OAuthHTTP) selectedOrganization(ctx context.Context, challenge oauthChallenge) (OrganizationOption, error) {
	subject, err := urn.ParseSessionSubject(challenge.Subject)
	if err != nil || subject.Kind != urn.SessionSubjectKindUser || challenge.OrganizationID == "" {
		return OrganizationOption{}, ErrForbidden
	}
	organizations, err := s.organizations.EligibleOrganizations(ctx, subject.ID)
	if err != nil {
		return OrganizationOption{}, fmt.Errorf("list organizations eligible for Platform MCP: %w", err)
	}
	for _, organization := range organizations {
		if organization.ID == challenge.OrganizationID {
			return organization, nil
		}
	}
	return OrganizationOption{}, ErrForbidden
}

// ProviderSetupCompleteHandler is the fixed server-owned landing page after a
// reviewed remote-session provider callback persists its tokens. It carries no
// provider identity, tokens, state, or handoff values.
func (s *OAuthHTTP) ProviderSetupCompleteHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.renderOAuthPage(r.Context(), w, oauthPageData{
			Title:     "Sign-in complete · Speakeasy",
			Kind:      "provider-complete",
			AutoClose: true,
		})
	})
}

func (s *OAuthHTTP) ConnectHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			s.connectGet(w, r)
		case http.MethodPost:
			s.connectPost(w, r)
		default:
			w.Header().Set("Allow", "GET, POST")
			writeBrowserOAuthError(r.Context(), w, http.StatusMethodNotAllowed, oautherr.CodeInvalidRequest, "method not allowed", oauthui.ReasonInvalidLink)
		}
	})
}

func (s *OAuthHTTP) connectGet(w http.ResponseWriter, r *http.Request) {
	challenge, err := s.cache.Get(r.Context(), platformOAuthChallengePrefix+r.URL.Query().Get("state"))
	if err != nil || challenge.Subject == "" || challenge.OrganizationID == "" {
		writeBrowserOAuthError(r.Context(), w, http.StatusBadRequest, oautherr.CodeInvalidRequest, "authorization state is invalid or incomplete", oauthui.ReasonExpired)
		return
	}
	// Lookup only: authorize already resolved and persisted any CIMD row,
	// and a mid-flow leg must keep working even if the document host has
	// since become unreachable.
	client, err := s.resolveClient(r.Context(), challenge.ClientID, lookupClientOnly)
	if err != nil {
		writeBrowserOAuthError(r.Context(), w, http.StatusBadRequest, oautherr.CodeInvalidClient, "authorization client is unavailable", oauthui.ReasonInvalidClient)
		return
	}
	organization, err := s.selectedOrganization(r.Context(), challenge)
	if err != nil {
		writeBrowserOAuthError(r.Context(), w, http.StatusForbidden, "access_denied", "selected organization is not available", oauthui.ReasonDefault)
		return
	}
	s.renderOAuthPage(r.Context(), w, oauthPageData{
		Title:            "Connect to Speakeasy",
		Kind:             "connect",
		ClientName:       client.Name,
		ClientIDOrigin:   clientIDOrigin(client),
		OrganizationName: organization.Name,
		RedirectURI:      challenge.RedirectURI,
		State:            challenge.ID,
		CSRFToken:        challenge.CSRFToken,
	})
}

func (s *OAuthHTTP) connectPost(w http.ResponseWriter, r *http.Request) {
	recorded := &oauthResponseRecorder{ResponseWriter: w}
	w = recorded
	defer func() {
		s.oauthTelemetry().Record(r.Context(), OAuthEvent{
			Operation: "interactive_authorization",
			Outcome:   recorded.outcome(),
			Reason:    recorded.reason,
		})
	}()

	r.Body = http.MaxBytesReader(w, r.Body, 16<<10)
	if err := r.ParseForm(); err != nil {
		writeBrowserOAuthError(r.Context(), w, http.StatusBadRequest, "invalid_request", "could not parse authorization", oauthui.ReasonDefault)
		return
	}
	challenge, err := s.cache.GetAndDelete(r.Context(), platformOAuthChallengePrefix+r.PostForm.Get("state"))
	if err != nil || challenge.Subject == "" || challenge.OrganizationID == "" {
		writeBrowserOAuthError(r.Context(), w, http.StatusBadRequest, oautherr.CodeInvalidRequest, "authorization state is invalid or incomplete", oauthui.ReasonExpired)
		return
	}
	if challenge.CSRFToken == "" || subtle.ConstantTimeCompare([]byte(r.PostForm.Get("csrf_token")), []byte(challenge.CSRFToken)) != 1 {
		writeBrowserOAuthError(r.Context(), w, http.StatusBadRequest, oautherr.CodeInvalidRequest, "authorization confirmation is invalid", oauthui.ReasonSessionChanged)
		return
	}
	if r.PostForm.Get("action") != "approve" {
		setOAuthTelemetryReason(w, "authorization_denied")
		redirectOAuthError(w, r, challenge.RedirectURI, challenge.State, &oauthwire.Error{Code: "access_denied", Description: "user denied authorization"})
		return
	}
	subject, err := urn.ParseSessionSubject(challenge.Subject)
	if err != nil || subject.Kind != urn.SessionSubjectKindUser {
		writeBrowserOAuthError(r.Context(), w, http.StatusBadRequest, oautherr.CodeAccessDenied, "authorization identity is invalid", oauthui.ReasonSessionChanged)
		return
	}
	principal := Principal{UserID: subject.ID, OrganizationID: challenge.OrganizationID, ConnectionID: "pending", Generation: "pending", ClientID: challenge.ClientID}
	if err := s.gateAndAuthorize(r.Context(), principal); err != nil {
		writeAuthorizationGateError(w, r, challenge, err)
		return
	}
	code, err := s.credentials.Issue(authorizationCodeCredential, challenge.OrganizationID, platformResource(s.challengeBaseURL(challenge)))
	if err != nil {
		writeBrowserOAuthError(r.Context(), w, http.StatusInternalServerError, "server_error", "could not complete authorization", oauthui.ReasonDefault)
		return
	}
	now := s.now()
	_, err = s.store.AuthorizeConnection(r.Context(), platformoauth.AuthorizeConnectionInput{
		Connection: platformoauth.Connection{
			ID:                     uuid.NewString(),
			ClientID:               challenge.ClientID,
			Subject:                challenge.Subject,
			OrganizationID:         challenge.OrganizationID,
			Generation:             uuid.NewString(),
			AuthorizationExpiresAt: now.Add(platformoauth.AuthorizationLifetime),
			RevokedAt:              nil,
		},
		Grant: platformoauth.Grant{
			Code:          code,
			ClientID:      challenge.ClientID,
			Connection:    platformoauth.Connection{},
			RedirectURI:   challenge.RedirectURI,
			CodeChallenge: challenge.CodeChallenge,
			ExpiresAt:     now.Add(platformOAuthCodeLifetime),
		},
		Now: now,
	})
	if err != nil {
		writeBrowserOAuthError(r.Context(), w, http.StatusInternalServerError, "server_error", "could not complete authorization", oauthui.ReasonDefault)
		return
	}
	recorded.setOAuthOutcome("succeeded")
	http.Redirect(w, r, redirectURL(challenge.RedirectURI, code, challenge.State, "", ""), http.StatusSeeOther)
}

func (s *OAuthHTTP) TokenHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		recorded := &oauthResponseRecorder{ResponseWriter: w}
		w = recorded
		r.Body = http.MaxBytesReader(w, r.Body, 16<<10)
		if err := r.ParseForm(); err != nil {
			writeOAuthError(w, http.StatusBadRequest, "invalid_request", "could not parse token request")
			return
		}
		if r.PostForm.Get(oauthwire.ParamGrantType) == "" {
			writeOAuthError(w, http.StatusBadRequest, oautherr.CodeInvalidRequest, "grant_type is required")
			return
		}
		operation, ok := tokenOperation(r.PostForm.Get("grant_type"))
		if ok {
			defer func() {
				s.oauthTelemetry().Record(r.Context(), OAuthEvent{Operation: operation, Outcome: recorded.outcome(), Reason: recorded.reason})
			}()
		}
		clientID, clientSecret, code := clientCredentials(r)
		if code != "" {
			writeClientAuthenticationError(w, r, code)
			return
		}
		now := s.now()
		resource := s.resource(r.Context())
		client, err := s.authenticateClient(r.Context(), clientID, clientSecret, now)
		if err != nil {
			s.writeClientAuthenticationFailure(w, r, err)
			return
		}
		switch r.PostForm.Get("grant_type") {
		case "authorization_code":
			request := usersessions.AuthCodeTokenRequestFromForm(r.PostForm)
			if err := request.Validate(); err != nil {
				writeRequestOAuthError(w, http.StatusBadRequest, err)
				return
			}
			code, err := s.credentials.decode(authorizationCodeCredential, request.Code)
			if err != nil || s.boundResource(code) != resource {
				writeOAuthError(w, http.StatusBadRequest, "invalid_grant", "authorization code is invalid")
				return
			}
			input := platformoauth.ConsumeGrantInput{OrganizationID: code.OrganizationID, Code: request.Code, ClientID: client.ID, RedirectURI: request.RedirectURI, CodeVerifier: request.CodeVerifier, Now: now}
			grant, err := s.store.ValidateGrant(r.Context(), input)
			if err != nil {
				writeTokenStateError(w, err, "authorization code")
				return
			}
			s.mintAndExchangeGrant(w, r, input, grant, client.ID, resource, now)
		case "refresh_token":
			request := usersessions.RefreshTokenRequestFromForm(r.PostForm)
			if err := request.Validate(); err != nil {
				writeRequestOAuthError(w, http.StatusBadRequest, err)
				return
			}
			refresh, err := s.credentials.decode(refreshTokenCredential, request.RefreshToken)
			if err != nil || s.boundResource(refresh) != resource {
				writeOAuthError(w, http.StatusBadRequest, "invalid_grant", "refresh token is invalid")
				return
			}
			refreshHash := opaqueHash(request.RefreshToken)
			old, err := s.store.PrepareRefresh(r.Context(), platformoauth.PrepareRefreshInput{OrganizationID: refresh.OrganizationID, RefreshHash: refreshHash, ClientID: client.ID, Now: now})
			if err != nil {
				writeTokenStateError(w, err, "refresh token")
				return
			}
			subject, err := urn.ParseSessionSubject(old.Connection.Subject)
			if err != nil || subject.Kind != urn.SessionSubjectKindUser {
				writeOAuthError(w, http.StatusBadRequest, "invalid_grant", "refresh token is invalid")
				return
			}
			if err := s.gateAndAuthorize(r.Context(), Principal{UserID: subject.ID, OrganizationID: old.Connection.OrganizationID, ConnectionID: old.Connection.ID, Generation: old.Connection.Generation, ClientID: client.ID}); err != nil {
				if errors.Is(err, ErrForbidden) && !errors.Is(err, errPlatformMCPDisabled) {
					if markErr := s.store.MarkAuthorizationLost(r.Context(), old.Connection.OrganizationID, old.Connection.ID, old.Connection.Generation, now); markErr != nil {
						writeOAuthError(w, http.StatusServiceUnavailable, "temporarily_unavailable", "organization access change could not be recorded")
						return
					}
				}
				writeTokenGateError(w, err)
				return
			}
			s.mintReplacementAndRespond(w, r, old, client.ID, resource, now)
		default:
			writeOAuthError(w, http.StatusBadRequest, "unsupported_grant_type", "unsupported grant_type")
		}
	})
}

func (s *OAuthHTTP) RevokeHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.Body = http.MaxBytesReader(w, r.Body, 16<<10)
		if err := r.ParseForm(); err != nil {
			writeOAuthError(w, http.StatusBadRequest, oautherr.CodeInvalidRequest, "could not parse revocation request")
			return
		}
		clientID, secret, code := clientCredentials(r)
		if code != "" {
			writeClientAuthenticationError(w, r, code)
			return
		}
		now := s.now()
		client, err := s.authenticateClient(r.Context(), clientID, secret, now)
		if err != nil {
			s.writeClientAuthenticationFailure(w, r, err)
			return
		}
		token := r.PostForm.Get(oauthwire.ParamToken)
		var revokeErr error
		switch r.PostForm.Get(oauthwire.ParamTokenTypeHint) {
		case "access_token":
			revokeErr = s.revokeAccessToken(r.Context(), token, client.ID, now)
		default:
			if organizationID, decodeErr := s.credentials.OrganizationID(refreshTokenCredential, token); decodeErr == nil {
				_, revokeErr = s.store.RevokeSession(r.Context(), organizationID, opaqueHash(token), client.ID, now)
				if !errors.Is(revokeErr, platformoauth.ErrNotFound) {
					break
				}
			}
			revokeErr = s.revokeAccessToken(r.Context(), token, client.ID, now)
		}
		if revokeErr != nil && !errors.Is(revokeErr, platformoauth.ErrNotFound) {
			s.logger.ErrorContext(r.Context(), "revoke OAuth token", attr.SlogError(revokeErr))
			writeOAuthError(w, http.StatusServiceUnavailable, oautherr.CodeTemporarilyUnavailable, "token revocation is temporarily unavailable")
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Pragma", "no-cache")
		w.WriteHeader(http.StatusOK)
	})
}

func (s *OAuthHTTP) revokeAccessToken(ctx context.Context, token, clientID string, now time.Time) error {
	jti, err := s.signer.VerifiedJTI(token)
	if err != nil {
		return nil
	}
	organizationID, err := s.credentials.OrganizationID(accessJTICredential, jti)
	if err != nil {
		return nil
	}
	if _, err := s.store.RevokeAccessSession(ctx, organizationID, jti, clientID, now); err != nil {
		return fmt.Errorf("revoke access token session: %w", err)
	}
	return nil
}

func (s *OAuthHTTP) mintAndExchangeGrant(w http.ResponseWriter, r *http.Request, input platformoauth.ConsumeGrantInput, grant platformoauth.Grant, clientID, resource string, now time.Time) {
	connection := grant.Connection
	subject, err := urn.ParseSessionSubject(connection.Subject)
	if err != nil || subject.Kind != urn.SessionSubjectKindUser {
		writeOAuthError(w, http.StatusInternalServerError, "server_error", "could not mint token")
		return
	}
	principal := Principal{UserID: subject.ID, OrganizationID: connection.OrganizationID, ConnectionID: connection.ID, Generation: connection.Generation, ClientID: clientID}
	if err := s.gateAndAuthorize(r.Context(), principal); err != nil {
		writeTokenGateError(w, err)
		return
	}
	jti, err := s.credentials.Issue(accessJTICredential, connection.OrganizationID, resource)
	if err != nil {
		writeOAuthError(w, http.StatusInternalServerError, "server_error", "could not mint token")
		return
	}
	accessExpiresAt := minTime(now.Add(platformAccessTokenLifetime), connection.AuthorizationExpiresAt)
	refreshExpiresAt := minTime(now.Add(platformRefreshTokenLifetime), connection.AuthorizationExpiresAt)
	accessToken, jti, err := s.signer.Mint(sessiontokens.MintParams{Subject: subject, Audience: resource, Issuer: resource, ExpiresAt: &accessExpiresAt, ClientID: clientID, JTI: jti})
	if err != nil {
		writeOAuthError(w, http.StatusInternalServerError, "server_error", "could not mint token")
		return
	}
	refreshToken, err := s.credentials.Issue(refreshTokenCredential, connection.OrganizationID, resource)
	if err != nil {
		writeOAuthError(w, http.StatusInternalServerError, "server_error", "could not mint token")
		return
	}
	session := platformoauth.Session{ID: uuid.NewString(), ClientID: clientID, Connection: connection, JTI: jti, RefreshHash: opaqueHash(refreshToken), ExpiresAt: accessExpiresAt, RefreshExpiresAt: refreshExpiresAt, RevokedAt: nil}
	if _, err := s.store.ExchangeGrant(r.Context(), platformoauth.ExchangeGrantInput{ConsumeGrantInput: input, Session: session}); err != nil {
		writeTokenStateError(w, err, "authorization code")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"access_token": accessToken, "token_type": "Bearer", "expires_in": int64(accessExpiresAt.Sub(now).Seconds()), "refresh_token": refreshToken})
}

func (s *OAuthHTTP) mintReplacementAndRespond(w http.ResponseWriter, r *http.Request, old platformoauth.Session, clientID, resource string, now time.Time) {
	subject, err := urn.ParseSessionSubject(old.Connection.Subject)
	if err != nil {
		writeOAuthError(w, http.StatusInternalServerError, "server_error", "could not mint token")
		return
	}
	jti, err := s.credentials.Issue(accessJTICredential, old.Connection.OrganizationID, resource)
	if err != nil {
		writeOAuthError(w, http.StatusInternalServerError, "server_error", "could not mint token")
		return
	}
	accessExpiresAt := minTime(now.Add(platformAccessTokenLifetime), old.Connection.AuthorizationExpiresAt)
	refreshExpiresAt := minTime(now.Add(platformRefreshTokenLifetime), old.Connection.AuthorizationExpiresAt)
	accessToken, jti, err := s.signer.Mint(sessiontokens.MintParams{Subject: subject, Audience: resource, Issuer: resource, ExpiresAt: &accessExpiresAt, ClientID: clientID, JTI: jti})
	if err != nil {
		writeOAuthError(w, http.StatusInternalServerError, "server_error", "could not mint token")
		return
	}
	refreshToken, err := s.credentials.Issue(refreshTokenCredential, old.Connection.OrganizationID, resource)
	if err != nil {
		writeOAuthError(w, http.StatusInternalServerError, "server_error", "could not mint token")
		return
	}
	replacement := platformoauth.Session{ID: uuid.NewString(), ClientID: clientID, Connection: old.Connection, JTI: jti, RefreshHash: opaqueHash(refreshToken), ExpiresAt: accessExpiresAt, RefreshExpiresAt: refreshExpiresAt, RevokedAt: nil}
	if _, err := s.store.RotateSession(r.Context(), platformoauth.RotateSessionInput{OrganizationID: old.Connection.OrganizationID, RefreshHash: old.RefreshHash, ClientID: clientID, Generation: old.Connection.Generation, Now: now, Replacement: replacement}); err != nil {
		writeTokenStateError(w, err, "refresh token")
		return
	}
	generationAuthorizedAt := old.Connection.AuthorizationExpiresAt.Add(-platformoauth.AuthorizationLifetime)
	s.oauthTelemetry().RecordRefreshSuccess(r.Context(), s.now().Sub(now), now.Sub(generationAuthorizedAt))
	writeJSON(w, http.StatusOK, map[string]any{"access_token": accessToken, "token_type": "Bearer", "expires_in": int64(accessExpiresAt.Sub(now).Seconds()), "refresh_token": refreshToken})
}

func (s *OAuthHTTP) authenticateClient(ctx context.Context, clientID, secret string, now time.Time) (platformoauth.Client, error) {
	if clientID == "" {
		return platformoauth.Client{}, platformoauth.ErrNotFound
	}
	client, err := s.store.GetClient(ctx, clientID)
	if err != nil {
		return platformoauth.Client{}, fmt.Errorf("get platform oauth client: %w", err)
	}
	if client.SecretExpiresAt != nil && !now.Before(*client.SecretExpiresAt) {
		return platformoauth.Client{}, platformoauth.ErrExpired
	}
	if client.SecretHash == "" {
		if secret != "" {
			return platformoauth.Client{}, platformoauth.ErrClientMismatch
		}
		return client, nil
	}
	if bcrypt.CompareHashAndPassword([]byte(client.SecretHash), []byte(secret)) != nil {
		return platformoauth.Client{}, platformoauth.ErrClientMismatch
	}
	return client, nil
}

func minTime(a, b time.Time) time.Time {
	if a.Before(b) {
		return a
	}
	return b
}

func (s *OAuthHTTP) gateAndAuthorize(ctx context.Context, principal Principal) error {
	enabled, err := s.gate.Enabled(ctx, principal.OrganizationID)
	if err != nil {
		return fmt.Errorf("check platform mcp feature gate: %w: %w", ErrUnavailable, err)
	}
	if !enabled {
		return errPlatformMCPDisabled
	}
	if err := s.authorizer.RequireLiveMembership(ctx, principal); err != nil {
		if isAuthorizationDenied(err) {
			return ErrForbidden
		}
		return fmt.Errorf("require live organization membership: %w: %w", ErrUnavailable, err)
	}
	return nil
}

func writeAuthorizationGateError(w http.ResponseWriter, r *http.Request, challenge oauthChallenge, err error) {
	setOAuthTelemetryReason(w, oauthGateReason(err))
	if errors.Is(err, ErrUnavailable) {
		redirectOAuthError(w, r, challenge.RedirectURI, challenge.State, &oauthwire.Error{Code: "temporarily_unavailable", Description: "organization access could not be verified"})
		return
	}
	// The gate and the live membership check both deny with ErrForbidden, and
	// they need different advice: naming the entitlement to someone whose
	// membership ended sends them to a setting that is already on.
	if errors.Is(err, errPlatformMCPDisabled) {
		redirectOAuthError(w, r, challenge.RedirectURI, challenge.State, &oauthwire.Error{Code: "access_denied", Description: "Platform MCP is not enabled for this organization. An organization admin can enable it in the Speakeasy dashboard."})
		return
	}
	redirectOAuthError(w, r, challenge.RedirectURI, challenge.State, &oauthwire.Error{Code: "access_denied", Description: "Your account is not an active member of this organization."})
}

func writeTokenStateError(w http.ResponseWriter, err error, credential string) {
	setOAuthTelemetryReason(w, oauthStateFailureReason(err))
	switch {
	case errors.Is(err, platformoauth.ErrNotFound), errors.Is(err, platformoauth.ErrRevoked), errors.Is(err, platformoauth.ErrExpired), errors.Is(err, platformoauth.ErrAlreadyUsed), errors.Is(err, platformoauth.ErrClientMismatch), errors.Is(err, platformoauth.ErrGeneration), errors.Is(err, platformoauth.ErrRedirectURI), errors.Is(err, platformoauth.ErrPKCE):
		writeOAuthError(w, http.StatusBadRequest, "invalid_grant", credential+" is invalid")
	default:
		writeOAuthError(w, http.StatusServiceUnavailable, "temporarily_unavailable", "token exchange could not be completed")
	}
}

func tokenOperation(grantType string) (string, bool) {
	switch grantType {
	case "authorization_code":
		return "code_exchange", true
	case "refresh_token":
		return "refresh", true
	default:
		return "", false
	}
}

type oauthResponseRecorder struct {
	http.ResponseWriter
	status       int
	oauthOutcome string
	reason       string
}

func (w *oauthResponseRecorder) WriteHeader(status int) {
	if w.status == 0 {
		w.status = status
	}
	w.ResponseWriter.WriteHeader(status)
}

func (w *oauthResponseRecorder) Write(body []byte) (int, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	written, err := w.ResponseWriter.Write(body)
	if err != nil {
		return written, fmt.Errorf("write OAuth response: %w", err)
	}
	return written, nil
}

func (w *oauthResponseRecorder) outcome() string {
	if validOAuthOutcome(w.oauthOutcome) {
		return w.oauthOutcome
	}
	if w.status >= http.StatusOK && w.status < http.StatusMultipleChoices {
		return "succeeded"
	}
	return "server_error"
}

func (w *oauthResponseRecorder) setOAuthOutcome(outcome string) {
	if validOAuthOutcome(outcome) {
		w.oauthOutcome = outcome
	}
}

func (w *oauthResponseRecorder) setOAuthReason(reason string) {
	if validOAuthReason(reason) {
		w.reason = reason
	}
}

func setOAuthTelemetryOutcome(w http.ResponseWriter, outcome string) {
	if recorder, ok := w.(*oauthResponseRecorder); ok {
		recorder.setOAuthOutcome(outcome)
	}
}

func setOAuthTelemetryReason(w http.ResponseWriter, reason string) {
	if recorder, ok := w.(*oauthResponseRecorder); ok {
		recorder.setOAuthReason(reason)
	}
}

func validOAuthOutcome(outcome string) bool {
	switch outcome {
	case "succeeded", "invalid_grant", "access_denied", "temporarily_unavailable", "invalid_client", "invalid_request", "server_error", "unsupported_grant_type":
		return true
	default:
		return false
	}
}

func oauthGateReason(err error) string {
	if errors.Is(err, errPlatformMCPDisabled) {
		return "platform_disabled"
	}
	if errors.Is(err, ErrForbidden) {
		return "authorization_denied"
	}
	if errors.Is(err, ErrUnavailable) {
		return "authorization_unavailable"
	}
	return ""
}

func writeTokenGateError(w http.ResponseWriter, err error) {
	setOAuthTelemetryReason(w, oauthGateReason(err))
	if errors.Is(err, ErrUnavailable) {
		writeOAuthError(w, http.StatusServiceUnavailable, "temporarily_unavailable", "organization access could not be verified")
		return
	}
	writeOAuthError(w, http.StatusBadRequest, oautherr.CodeInvalidGrant, "organization access is not available")
}

// ProviderSetupCompletionURL is the only callback landing page accepted for
// Platform MCP provider authorization. It is server-owned and carries no state.
func (s *OAuthHTTP) ProviderSetupCompletionURL() string {
	return endpointURL(s.baseURL, "provider-setup-complete")
}

// ProtectedResourceURL is the RFC 9728 metadata URL on the configured base
// URL, used when a request carries no platform origin.
func (s *OAuthHTTP) ProtectedResourceURL() string {
	return platformProtectedResourceMetadataURL(s.baseURL)
}

// resource is the Platform MCP resource, issuer, and audience for the
// platform origin the request arrived on.
func (s *OAuthHTTP) resource(ctx context.Context) string {
	return platformResource(platformBaseURL(ctx, s.baseURL))
}

// challengeBaseURL is the platform origin an authorization started on.
// BaseURL is only ever written from the middleware-stamped origin.
func (s *OAuthHTTP) challengeBaseURL(challenge oauthChallenge) *url.URL {
	if challenge.BaseURL == "" {
		return s.baseURL
	}
	base, err := url.Parse(challenge.BaseURL)
	if err != nil || base.Scheme == "" || base.Host == "" {
		return s.baseURL
	}
	return base
}

// boundResource is the resource an authorization code or refresh token was
// issued for. Credentials that predate resource binding were all issued for
// the configured base URL's resource, so they keep working there and only
// there.
func (s *OAuthHTTP) boundResource(credential credentialPayload) string {
	if credential.Resource == "" {
		return platformResource(s.baseURL)
	}
	return credential.Resource
}

func endpointURL(base *url.URL, segment string) string {
	return platformResource(base) + "/" + segment
}

// writeClientResolutionError maps the resolveClient error contract onto the
// wire. Called before any redirect_uri is trusted, so every case renders
// inline rather than redirecting (RFC 6749 §4.1.2.1).
func (s *OAuthHTTP) writeClientResolutionError(w http.ResponseWriter, ctx context.Context, clientID string, err error) {
	if admissionErr, ok := errors.AsType[*admission.DenialError](err); ok {
		// Policy, not a spec violation, and it carries its own actionable
		// description. Already logged inside admitCIMDClient.
		writeBrowserOAuthError(ctx, w, http.StatusBadRequest, oautherr.CodeInvalidClient, admissionErr.Description(), oauthui.ReasonOrganizationBlocked)
		return
	}
	if oauthErr, ok := errors.AsType[*oauthwire.Error](err); ok {
		reason := oauthui.ReasonDefault
		switch oauthErr.Code {
		case oautherr.CodeInvalidClient, oautherr.CodeInvalidClientMetadata, oautherr.CodeInvalidRedirectURI:
			reason = oauthui.ReasonInvalidClient
		}
		writeBrowserOAuthError(ctx, w, http.StatusBadRequest, oauthErr.Code, oauthErr.Description, reason)
		return
	}
	if errors.Is(err, errCIMDFetchFailed) {
		// The cause may name internal network conditions (SSRF denials, DNS
		// failures); log it and keep the wire response generic. A fetch
		// failure is transient from the client's perspective, so signal
		// retry-later rather than a permanent invalid_client that would make
		// SDKs stop retrying.
		s.logger.InfoContext(ctx, "cimd document fetch failed",
			attr.SlogOAuthClientID(truncateClientIDForLog(clientID)),
			attr.SlogError(err),
		)
		writeBrowserOAuthError(ctx, w, http.StatusServiceUnavailable, "temporarily_unavailable", "failed to fetch client metadata document", oauthui.ReasonDefault)
		return
	}
	if !errors.Is(err, platformoauth.ErrNotFound) && !errors.Is(err, platformoauth.ErrRevoked) {
		s.logger.ErrorContext(ctx, "resolve OAuth client", attr.SlogError(err))
		writeBrowserOAuthError(ctx, w, http.StatusServiceUnavailable, oautherr.CodeTemporarilyUnavailable, "client lookup is temporarily unavailable", oauthui.ReasonDefault)
		return
	}
	writeBrowserOAuthError(ctx, w, http.StatusBadRequest, oautherr.CodeInvalidClient, "unknown client_id", oauthui.ReasonInvalidClient)
}

func (s *OAuthHTTP) writeClientAuthenticationFailure(w http.ResponseWriter, r *http.Request, err error) {
	if errors.Is(err, platformoauth.ErrNotFound) || errors.Is(err, platformoauth.ErrExpired) || errors.Is(err, platformoauth.ErrRevoked) || errors.Is(err, platformoauth.ErrClientMismatch) {
		writeClientAuthenticationError(w, r, oautherr.CodeInvalidClient)
		return
	}
	s.logger.ErrorContext(r.Context(), "authenticate OAuth client", attr.SlogError(err))
	writeOAuthError(w, http.StatusServiceUnavailable, oautherr.CodeTemporarilyUnavailable, "client lookup is temporarily unavailable")
}

func writeClientAuthenticationError(w http.ResponseWriter, r *http.Request, code string) {
	status := http.StatusBadRequest
	description := "invalid client authentication request"
	if code == oautherr.CodeInvalidClient {
		var challenge string
		status, challenge = oauthwire.InvalidClientResponse(r.Header)
		if challenge != "" {
			w.Header().Set("WWW-Authenticate", challenge)
		}
		description = "client authentication failed"
	}
	writeOAuthError(w, status, code, description)
}

func clientCredentials(r *http.Request) (id, secret, code string) {
	if code := oauthwire.ClientAuthenticationError(r); code != "" {
		return "", "", code
	}
	// Platform clients support secrets or public authentication, not assertions.
	if r.PostForm.Get(oauthwire.ParamClientAssertion) != "" || r.PostForm.Get(oauthwire.ParamClientAssertionType) != "" {
		return "", "", oautherr.CodeInvalidClient
	}
	if id, secret, ok := r.BasicAuth(); ok {
		return id, secret, ""
	}
	return r.PostForm.Get(oauthwire.ParamClientID), r.PostForm.Get(oauthwire.ParamClientSecret), ""
}

func requireJSON(w http.ResponseWriter, r *http.Request, maxBytes int64) bool {
	contentType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || contentType != "application/json" {
		writeOAuthError(w, http.StatusBadRequest, "invalid_client_metadata", "Content-Type must be application/json")
		return false
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxBytes)
	return true
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	encoded, err := json.Marshal(body)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Pragma", "no-cache")
	w.WriteHeader(status)
	_, _ = w.Write(encoded)
}

func (s *OAuthHTTP) renderOAuthPage(ctx context.Context, w http.ResponseWriter, data oauthPageData) {
	scriptNonce, err := opaqueToken()
	if err != nil {
		writeBrowserOAuthError(ctx, w, http.StatusInternalServerError, "server_error", "could not render authorization page", oauthui.ReasonDefault)
		return
	}
	data.ScriptNonce = scriptNonce
	data.Styles = consentpage.Styles

	var page strings.Builder
	if err := oauthPageTemplate.Execute(&page, data); err != nil {
		writeBrowserOAuthError(ctx, w, http.StatusInternalServerError, "server_error", "could not render authorization page", oauthui.ReasonDefault)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Pragma", "no-cache")
	w.Header().Set("Content-Security-Policy", oauthPageContentSecurityPolicy(data.RedirectURI, scriptNonce))
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write([]byte(page.String()))
}

func oauthPageContentSecurityPolicy(redirectURI, scriptNonce string) string {
	formAction := "'self'"
	if uri, err := url.Parse(redirectURI); err == nil && (uri.Scheme == "http" || uri.Scheme == "https") && uri.Host != "" {
		formAction += " " + uri.Scheme + "://" + uri.Host
	}
	return "default-src 'none'; style-src 'unsafe-inline'; font-src 'self'; img-src data:; script-src 'nonce-" + scriptNonce + "'; base-uri 'none'; form-action " + formAction + "; frame-ancestors 'none'"
}

func writeOAuthError(w http.ResponseWriter, status int, code, description string) {
	setOAuthTelemetryOutcome(w, code)
	writeJSON(w, status, map[string]string{"error": code, "error_description": description})
}

func writeRequestOAuthError(w http.ResponseWriter, status int, err error) {
	if oauthError, ok := errors.AsType[*oauthwire.Error](err); ok {
		writeOAuthError(w, status, oauthError.Code, oauthError.Description)
		return
	}
	writeOAuthError(w, status, "invalid_request", "invalid request")
}

// writePreAuthenticationOAuthError requires a user click for registered callbacks
// outside loopback; later browser legs retain their existing redirect behavior.
// clientID is the value the request presented.
func writePreAuthenticationOAuthError(w http.ResponseWriter, r *http.Request, client platformoauth.Client, clientID, redirectURI, state string, err error) {
	if oauthwire.MayAutoRedirectBeforeAuthentication(redirectURI) {
		redirectOAuthError(w, r, redirectURI, state, err)
		return
	}
	oauthError, ok := errors.AsType[*oauthwire.Error](err)
	if !ok {
		oauthError = &oauthwire.Error{Code: oautherr.CodeInvalidRequest, Description: "invalid request"}
	}
	setOAuthTelemetryOutcome(w, oauthError.Code)
	callback := redirectURL(redirectURI, "", state, oauthError.Code, oauthError.Description)
	page := oauthui.AuthorizationError{
		Status:         http.StatusBadRequest,
		Reason:         oauthui.ReasonDefault,
		Approval:       false,
		ErrorID:        "",
		CallbackURL:    callback,
		Code:           oauthError.Code,
		Description:    oauthError.Description,
		ClientName:     client.Name,
		ClientID:       clientID,
		ClientIDOrigin: clientIDOrigin(client),
		ServerName:     "Speakeasy",
	}
	_ = oauthui.WriteAuthorizationError(r.Context(), w, page)
}

func redirectOAuthError(w http.ResponseWriter, r *http.Request, redirectURI, state string, err error) {
	var oauthError *oauthwire.Error
	if !errors.As(err, &oauthError) {
		oauthError = &oauthwire.Error{Code: "invalid_request", Description: "invalid request"}
	}
	setOAuthTelemetryOutcome(w, oauthError.Code)
	http.Redirect(w, r, redirectURL(redirectURI, "", state, oauthError.Code, oauthError.Description), http.StatusFound)
}

func redirectURL(raw, code, state, errorCode, description string) string {
	uri, err := url.Parse(raw)
	if err != nil {
		return raw
	}
	query := uri.Query()
	if code != "" {
		query.Set("code", code)
	}
	if state != "" {
		query.Set("state", state)
	}
	if errorCode != "" {
		query.Set("error", errorCode)
		query.Set("error_description", description)
	}
	uri.RawQuery = query.Encode()
	return uri.String()
}

func opaqueToken() (string, error) {
	bytes := make([]byte, 32)
	if _, err := rand.Read(bytes); err != nil {
		return "", fmt.Errorf("generate opaque token: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(bytes), nil
}

// writeBrowserOAuthError preserves OAuth telemetry and wire semantics while
// rendering a terminal navigation response without a callback.
func writeBrowserOAuthError(ctx context.Context, w http.ResponseWriter, status int, code, description string, reason oauthui.Reason) {
	setOAuthTelemetryOutcome(w, code)
	_ = oauthui.WriteAuthorizationError(ctx, w, oauthui.AuthorizationError{
		Status:         status,
		Reason:         reason,
		Approval:       false,
		ErrorID:        "",
		Code:           code,
		Description:    description,
		CallbackURL:    "",
		ClientName:     "",
		ClientID:       "",
		ClientIDOrigin: "",
		ServerName:     "Speakeasy",
	})
}

func writeBrowserRequestOAuthError(ctx context.Context, w http.ResponseWriter, status int, err error) {
	if oauthError, ok := errors.AsType[*oauthwire.Error](err); ok {
		writeBrowserOAuthError(ctx, w, status, oauthError.Code, oauthError.Description, oauthui.ReasonInvalidLink)
		return
	}
	writeBrowserOAuthError(ctx, w, status, oautherr.CodeInvalidRequest, "invalid request", oauthui.ReasonInvalidLink)
}
