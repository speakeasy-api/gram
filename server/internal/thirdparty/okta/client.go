package okta

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	jose "github.com/go-jose/go-jose/v4"
	"github.com/go-jose/go-jose/v4/jwt"
	"github.com/google/uuid"
	"github.com/hashicorp/go-retryablehttp"
	"golang.org/x/sync/singleflight"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/o11y"
)

const (
	clientAssertionType  = "urn:ietf:params:oauth:client-assertion-type:jwt-bearer"
	assertionLifetime    = 5 * time.Minute
	tokenRefreshSkew     = 60 * time.Second
	maxErrorBodyBytes    = 8 * 1024
	maxResponseBodyBytes = 2 * 1024 * 1024
	dpopNonceHeader      = "DPoP-Nonce"
)

// APIError describes an error response from Okta without retaining request credentials.
type APIError struct {
	// Method is the HTTP method used for the request.
	Method string

	// Path is the Okta API path that rejected the request.
	Path string

	// StatusCode is the HTTP status returned by Okta.
	StatusCode int

	// Code is Okta's machine-readable error code.
	Code string

	// Description is Okta's human-readable error description.
	Description string

	// RateLimit reports the quota headers returned with the error.
	RateLimit RateLimit
}

func (e *APIError) Error() string {
	return fmt.Sprintf("okta api %s %s: status %d: %s: %s", e.Method, e.Path, e.StatusCode, e.Code, e.Description)
}

// ClientOpts configures optional client behavior. Zero values use production defaults.
type ClientOpts struct {
	// Endpoint overrides every tenant origin. It is intended for HTTP tests.
	Endpoint string

	// RetryConfig overrides retries for idempotent collection reads. Token exchanges are never retried.
	RetryConfig *guardian.RetryConfig
}

// TokenRequest contains the material needed to mint an Okta OAuth token.
type TokenRequest struct {
	// ConnectionID scopes the token cache to one identity provider connection.
	ConnectionID uuid.UUID

	// TenantDomain is the normalized Okta tenant hostname.
	TenantDomain string

	// ClientID is the API Services application client ID.
	ClientID string

	// KeyID identifies PrivateKey in the tenant's configured JWKS.
	KeyID string

	// PrivateKey signs the short-lived client assertion.
	PrivateKey *rsa.PrivateKey

	// Scopes are requested from the Okta authorization server.
	Scopes []string
}

// Token is an Okta OAuth access token and its granted scope set.
type Token struct {
	// AccessToken is the bearer credential for Okta API reads.
	AccessToken string

	// ExpiresAt is the token's provider-reported expiry.
	ExpiresAt time.Time

	// GrantedScopes are the scopes Okta granted to this token.
	GrantedScopes []string
}

// PageRequest selects one page from an Okta collection.
type PageRequest struct {
	// Limit is the maximum number of items requested.
	Limit int

	// After is Okta's opaque cursor from a prior page.
	After string
}

// RateLimit reports Okta's current per-endpoint quota state.
type RateLimit struct {
	// Limit is the total request quota Okta reports for the endpoint.
	Limit *int64

	// Remaining is the number of requests Okta reports as available.
	Remaining *int64

	// Reset is the instant at which Okta reports the quota resets.
	Reset *time.Time
}

// Page is one page from an Okta collection.
type Page struct {
	// Items contains the provider objects on this page.
	Items []json.RawMessage

	// NextCursor is the opaque after cursor for the next page, when present.
	NextCursor string

	// RateLimit reports the response's Okta quota headers.
	RateLimit RateLimit
}

// CreateOIDCApplicationInput contains the caller-controlled OIDC application settings.
type CreateOIDCApplicationInput struct {
	// RedirectURIs are the allowed authorization-code callback URLs.
	RedirectURIs []string

	// ClientSecret is written to Okta and must not be retained after the request.
	ClientSecret string
}

// Application is the non-secret subset of an Okta OIDC application used by Gram.
type Application struct {
	// ID is Okta's application instance identifier.
	ID string `json:"id"`

	// Status is the application's Okta lifecycle status.
	Status string `json:"status"`

	// Label is the application's display name.
	Label string `json:"label"`

	// ClientID is the application's public OAuth client identifier.
	ClientID string `json:"client_id"`
}

// Group identifies an Okta group that can be assigned to an application.
type Group struct {
	// ID is Okta's group identifier.
	ID string

	// Name is the group's display name.
	Name string
}

type applicationResponse struct {
	ID          string `json:"id"`
	Status      string `json:"status"`
	Label       string `json:"label"`
	Credentials struct {
		OAuthClient struct {
			ClientID string `json:"client_id"`
		} `json:"oauthClient"`
	} `json:"credentials"`
}

type createOIDCApplicationRequest struct {
	Name        string                        `json:"name"`
	Label       string                        `json:"label"`
	SignOnMode  string                        `json:"signOnMode"`
	Credentials applicationCredentialsRequest `json:"credentials"`
	Settings    applicationSettingsRequest    `json:"settings"`
}

type applicationCredentialsRequest struct {
	OAuthClient oauthClientCredentialsRequest `json:"oauthClient"`
}

type oauthClientCredentialsRequest struct {
	ClientSecret            string `json:"client_secret"`
	TokenEndpointAuthMethod string `json:"token_endpoint_auth_method"`
	AutoKeyRotation         bool   `json:"autoKeyRotation"`
	PKCERequired            bool   `json:"pkce_required"`
}

type applicationSettingsRequest struct {
	OAuthClient oauthClientSettingsRequest `json:"oauthClient"`
}

type oauthClientSettingsRequest struct {
	ApplicationType string   `json:"application_type"`
	GrantTypes      []string `json:"grant_types"`
	ResponseTypes   []string `json:"response_types"`
	RedirectURIs    []string `json:"redirect_uris"`
	ConsentMethod   string   `json:"consent_method"`
}

type cacheKey struct {
	connectionID uuid.UUID
	kid          string
}

type cachedToken struct {
	token        Token
	tenantDomain string
	clientID     string
	scopes       string
	dpop         bool
	dpopNonce    string
}

type dpopProofClaims struct {
	HTTPMethod      string `json:"htm"`
	HTTPURI         string `json:"htu"`
	AccessTokenHash string `json:"ath,omitempty"`
	Nonce           string `json:"nonce,omitempty"`
}

type dpopBoundToken struct {
	key       cacheKey
	nonce     string
	expiresAt time.Time
}

// Client performs authenticated Okta API requests.
type Client struct {
	logger                *slog.Logger
	httpClient            *guardian.HTTPClient
	nonRetryingHTTPClient *guardian.HTTPClient
	endpoint              string
	now                   func() time.Time
	dpopKey               *ecdsa.PrivateKey
	mu                    sync.Mutex
	tokens                map[cacheKey]cachedToken
	dpopModes             map[uuid.UUID]string
	dpopTokens            map[string]dpopBoundToken
	tokenFlight           singleflight.Group
}

// NewClient constructs an Okta client using a shared outbound-request policy.
func NewClient(logger *slog.Logger, guardianPolicy *guardian.Policy, opts ...ClientOpts) *Client {
	if guardianPolicy == nil {
		panic("okta client requires a guardian policy")
	}

	var opt ClientOpts
	if len(opts) > 0 {
		opt = opts[0]
	}
	retryConfig := opt.RetryConfig
	if retryConfig == nil {
		retryConfig = guardian.DefaultRetryConfig()
		retryConfig.WaitMax = 10 * time.Second
		retryConfig.MaxAttempts = 2
		retryConfig.ErrorHandler = retryablehttp.PassthroughErrorHandler
	}

	resilience := guardian.WithResilience("okta", guardian.ResilienceConfig{
		Partition: guardian.PartitionByHost(),
		Limit:     guardian.PerMinute(600),
		Breaker:   guardian.NoBreaker(),
	})
	httpClient := guardianPolicy.PooledClient(guardian.WithRetryConfig(retryConfig), resilience)
	nonRetryingHTTPClient := guardianPolicy.PooledClient(resilience)
	refuseRedirect := func(_ *http.Request, _ []*http.Request) error {
		return http.ErrUseLastResponse
	}
	for _, client := range []*guardian.HTTPClient{httpClient, nonRetryingHTTPClient} {
		client.Timeout = 30 * time.Second
		client.CheckRedirect = refuseRedirect
	}
	dpopKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		panic(fmt.Errorf("generate Okta DPoP key: %w", err))
	}

	return &Client{
		logger:                logger,
		httpClient:            httpClient,
		nonRetryingHTTPClient: nonRetryingHTTPClient,
		endpoint:              strings.TrimRight(opt.Endpoint, "/"),
		now:                   time.Now,
		dpopKey:               dpopKey,
		mu:                    sync.Mutex{},
		tokens:                make(map[cacheKey]cachedToken),
		dpopModes:             make(map[uuid.UUID]string),
		dpopTokens:            make(map[string]dpopBoundToken),
		tokenFlight:           singleflight.Group{},
	}
}

// AcquireToken returns a cached token or exchanges a new private-key assertion.
func (c *Client) AcquireToken(ctx context.Context, request TokenRequest) (Token, error) {
	key := cacheKey{connectionID: request.ConnectionID, kid: request.KeyID}
	if token, ok := c.cachedToken(key, request); ok {
		return token, nil
	}

	scopes := strings.Join(request.Scopes, "\x00")
	flightKey := request.ConnectionID.String() + "\x00" + request.KeyID + "\x00" + request.TenantDomain + "\x00" + request.ClientID + "\x00" + scopes
	value, err, _ := c.tokenFlight.Do(flightKey, func() (any, error) {
		if token, ok := c.cachedToken(key, request); ok {
			return token, nil
		}
		return c.acquireToken(ctx, request)
	})
	if err != nil {
		return Token{AccessToken: "", ExpiresAt: time.Time{}, GrantedScopes: nil}, err
	}
	token, ok := value.(Token)
	if !ok {
		return Token{AccessToken: "", ExpiresAt: time.Time{}, GrantedScopes: nil}, fmt.Errorf("acquire Okta token: unexpected result type %T", value)
	}
	return cloneToken(token), nil
}

func (c *Client) cachedToken(key cacheKey, request TokenRequest) (Token, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	cached, ok := c.tokens[key]
	if !ok || cached.tenantDomain != request.TenantDomain || cached.clientID != request.ClientID || cached.scopes != strings.Join(request.Scopes, "\x00") || !c.now().Before(cached.token.ExpiresAt.Add(-tokenRefreshSkew)) {
		return Token{AccessToken: "", ExpiresAt: time.Time{}, GrantedScopes: nil}, false
	}
	return cloneToken(cached.token), true
}

func (c *Client) acquireToken(ctx context.Context, request TokenRequest) (Token, error) {
	if request.PrivateKey == nil {
		return Token{AccessToken: "", ExpiresAt: time.Time{}, GrantedScopes: nil}, errors.New("acquire Okta token: private key is required")
	}
	tokenEndpoint, err := c.urlFor(request.TenantDomain, "/oauth2/v1/token")
	if err != nil {
		return Token{AccessToken: "", ExpiresAt: time.Time{}, GrantedScopes: nil}, fmt.Errorf("acquire Okta token: %w", err)
	}

	key := cacheKey{connectionID: request.ConnectionID, kid: request.KeyID}
	dpop, nonce := c.dpopMode(key)
	token, responseNonce, err := c.exchangeToken(ctx, request, tokenEndpoint, dpop, nonce)
	if responseNonce != "" {
		nonce = responseNonce
		if dpop {
			c.setDPoPMode(key, nonce)
		}
	}
	if !dpop && isDPoPChallenge(err) {
		dpop = true
		c.setDPoPMode(key, nonce)
		token, responseNonce, err = c.exchangeToken(ctx, request, tokenEndpoint, true, nonce)
		if responseNonce != "" {
			nonce = responseNonce
			c.setDPoPMode(key, nonce)
		}
	}
	if dpop && isDPoPNonceChallenge(err) && responseNonce != "" {
		c.setDPoPMode(key, responseNonce)
		token, responseNonce, err = c.exchangeToken(ctx, request, tokenEndpoint, true, responseNonce)
		if responseNonce != "" {
			nonce = responseNonce
			c.setDPoPMode(key, nonce)
		}
	}
	if err != nil {
		if apiErr, ok := errors.AsType[*APIError](err); ok {
			c.logger.WarnContext(ctx, "Okta token request failed",
				attr.SlogOAuthError(apiErr.Code),
				attr.SlogOAuthErrorDescription(apiErr.Description),
				attr.SlogHTTPResponseStatusCode(apiErr.StatusCode),
				attr.SlogURLDomain(request.TenantDomain),
				attr.SlogError(err),
			)
		}
		return Token{AccessToken: "", ExpiresAt: time.Time{}, GrantedScopes: nil}, err
	}
	c.mu.Lock()
	c.tokens[cacheKey{connectionID: request.ConnectionID, kid: request.KeyID}] = cachedToken{
		token:        cloneToken(token),
		tenantDomain: request.TenantDomain,
		clientID:     request.ClientID,
		scopes:       strings.Join(request.Scopes, "\x00"),
		dpop:         dpop,
		dpopNonce:    "",
	}
	if dpop {
		for accessToken, bound := range c.dpopTokens {
			if !c.now().Before(bound.expiresAt) {
				delete(c.dpopTokens, accessToken)
			}
		}
		c.dpopTokens[token.AccessToken] = dpopBoundToken{key: key, nonce: "", expiresAt: token.ExpiresAt}
	}
	c.mu.Unlock()
	return token, nil
}

func (c *Client) exchangeToken(
	ctx context.Context,
	request TokenRequest,
	tokenEndpoint string,
	dpop bool,
	nonce string,
) (Token, string, error) {
	assertion, err := c.mintClientAssertion(ctx, request, tokenEndpoint)
	if err != nil {
		return Token{AccessToken: "", ExpiresAt: time.Time{}, GrantedScopes: nil}, "", err
	}
	form := url.Values{}
	form.Set("grant_type", "client_credentials")
	form.Set("scope", strings.Join(request.Scopes, " "))
	form.Set("client_assertion_type", clientAssertionType)
	form.Set("client_assertion", assertion)
	httpRequest, err := http.NewRequestWithContext(ctx, http.MethodPost, tokenEndpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return Token{AccessToken: "", ExpiresAt: time.Time{}, GrantedScopes: nil}, "", fmt.Errorf("create Okta token request: %w", err)
	}
	httpRequest.Header.Set("Accept", "application/json")
	httpRequest.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if dpop {
		proof, err := c.dpopProof(http.MethodPost, tokenEndpoint, "", nonce)
		if err != nil {
			return Token{AccessToken: "", ExpiresAt: time.Time{}, GrantedScopes: nil}, "", err
		}
		httpRequest.Header.Set("DPoP", proof)
	}

	var response struct {
		AccessToken string `json:"access_token"`
		TokenType   string `json:"token_type"`
		ExpiresIn   int64  `json:"expires_in"`
		Scope       string `json:"scope"`
	}
	headers, err := c.do(c.nonRetryingHTTPClient, httpRequest, &response)
	responseNonce := headers.Get(dpopNonceHeader)
	if err != nil {
		return Token{AccessToken: "", ExpiresAt: time.Time{}, GrantedScopes: nil}, responseNonce, err
	}
	if response.AccessToken == "" || response.ExpiresIn <= 0 {
		return Token{AccessToken: "", ExpiresAt: time.Time{}, GrantedScopes: nil}, responseNonce, errors.New("decode Okta token response: access_token and positive expires_in are required")
	}
	if dpop && !strings.EqualFold(response.TokenType, "DPoP") {
		return Token{AccessToken: "", ExpiresAt: time.Time{}, GrantedScopes: nil}, responseNonce, errors.New("decode Okta token response: token_type DPoP is required")
	}
	return Token{
		AccessToken:   response.AccessToken,
		ExpiresAt:     c.now().Add(time.Duration(response.ExpiresIn) * time.Second),
		GrantedScopes: strings.Fields(response.Scope),
	}, responseNonce, nil
}

func (c *Client) mintClientAssertion(ctx context.Context, request TokenRequest, tokenEndpoint string) (string, error) {
	signer, err := jose.NewSigner(
		jose.SigningKey{Algorithm: jose.RS256, Key: request.PrivateKey},
		(&jose.SignerOptions{NonceSource: nil, EmbedJWK: false, ExtraHeaders: nil}).WithType("JWT").WithHeader(jose.HeaderKey("kid"), request.KeyID),
	)
	if err != nil {
		return "", fmt.Errorf("create Okta assertion signer: %w", err)
	}
	now := c.now()
	claims := jwt.Claims{
		Issuer:    request.ClientID,
		Subject:   request.ClientID,
		Audience:  jwt.Audience{tokenEndpoint},
		Expiry:    jwt.NewNumericDate(now.Add(assertionLifetime)),
		NotBefore: nil,
		IssuedAt:  jwt.NewNumericDate(now),
		ID:        uuid.NewString(),
	}
	c.logger.DebugContext(ctx, "minting Okta client assertion",
		attr.SlogOAuthAssertionIssuer(claims.Issuer),
		attr.SlogOAuthAssertionSubject(claims.Subject),
		attr.SlogOAuthAssertionAudience(tokenEndpoint),
		attr.SlogOAuthAssertionKeyID(request.KeyID),
		attr.SlogOAuthAssertionAlgorithm(string(jose.RS256)),
		attr.SlogOAuthAssertionExpiresAt(claims.Expiry.Time()),
	)
	assertion, err := jwt.Signed(signer).Claims(claims).Serialize()
	if err != nil {
		return "", fmt.Errorf("sign Okta client assertion: %w", err)
	}
	return assertion, nil
}

func (c *Client) dpopProof(method, targetURL, accessToken, nonce string) (string, error) {
	signer, err := jose.NewSigner(
		jose.SigningKey{Algorithm: jose.ES256, Key: c.dpopKey},
		(&jose.SignerOptions{NonceSource: nil, EmbedJWK: false, ExtraHeaders: nil}).
			WithType("dpop+jwt").
			WithHeader(jose.HeaderKey("jwk"), jose.JSONWebKey{
				Key:                         &c.dpopKey.PublicKey,
				KeyID:                       "",
				Algorithm:                   "",
				Use:                         "",
				Certificates:                nil,
				CertificatesURL:             nil,
				CertificateThumbprintSHA1:   nil,
				CertificateThumbprintSHA256: nil,
			}),
	)
	if err != nil {
		return "", fmt.Errorf("create Okta DPoP signer: %w", err)
	}
	ath := ""
	if accessToken != "" {
		hash := sha256.Sum256([]byte(accessToken))
		ath = base64.RawURLEncoding.EncodeToString(hash[:])
	}
	proof, err := jwt.Signed(signer).
		Claims(jwt.Claims{
			Issuer:    "",
			Subject:   "",
			Audience:  nil,
			Expiry:    nil,
			NotBefore: nil,
			IssuedAt:  jwt.NewNumericDate(c.now()),
			ID:        uuid.NewString(),
		}).
		Claims(dpopProofClaims{HTTPMethod: method, HTTPURI: targetURL, AccessTokenHash: ath, Nonce: nonce}).
		Serialize()
	if err != nil {
		return "", fmt.Errorf("sign Okta DPoP proof: %w", err)
	}
	return proof, nil
}

func (c *Client) dpopMode(key cacheKey) (bool, string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	nonce, ok := c.dpopModes[key.connectionID]
	return ok, nonce
}

func (c *Client) setDPoPMode(key cacheKey, nonce string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.dpopModes[key.connectionID] = nonce
}

func isDPoPChallenge(err error) bool {
	apiErr, ok := errors.AsType[*APIError](err)
	return ok && apiErr.StatusCode == http.StatusBadRequest && (apiErr.Code == "invalid_dpop_proof" || apiErr.Code == "use_dpop_nonce")
}

func isDPoPNonceChallenge(err error) bool {
	apiErr, ok := errors.AsType[*APIError](err)
	return ok && (apiErr.StatusCode == http.StatusBadRequest || apiErr.StatusCode == http.StatusUnauthorized) && apiErr.Code == "use_dpop_nonce"
}

// ListGroups reads one cursor-addressable page of Okta groups.
func (c *Client) ListGroups(ctx context.Context, tenantDomain, accessToken string, page PageRequest) (Page, error) {
	return c.list(ctx, tenantDomain, accessToken, "/api/v1/groups", page)
}

// ListUsers reads one cursor-addressable page of Okta users.
func (c *Client) ListUsers(ctx context.Context, tenantDomain, accessToken string, page PageRequest) (Page, error) {
	return c.list(ctx, tenantDomain, accessToken, "/api/v1/users", page)
}

// ListApplications reads one cursor-addressable page of Okta applications.
func (c *Client) ListApplications(ctx context.Context, tenantDomain, accessToken string, page PageRequest) (Page, error) {
	return c.list(ctx, tenantDomain, accessToken, "/api/v1/apps", page)
}

// CreateOIDCApplication creates the Speakeasy OIDC web application with a caller-minted client secret.
func (c *Client) CreateOIDCApplication(ctx context.Context, tenantDomain, accessToken string, input CreateOIDCApplicationInput) (Application, error) {
	if input.ClientSecret == "" {
		return Application{}, errors.New("create Okta OIDC application: client secret is required")
	}
	payload := createOIDCApplicationRequest{
		Name:       "oidc_client",
		Label:      "Speakeasy",
		SignOnMode: "OPENID_CONNECT",
		Credentials: applicationCredentialsRequest{
			OAuthClient: oauthClientCredentialsRequest{
				ClientSecret:            input.ClientSecret,
				TokenEndpointAuthMethod: "client_secret_post",
				AutoKeyRotation:         true,
				PKCERequired:            true,
			},
		},
		Settings: applicationSettingsRequest{
			OAuthClient: oauthClientSettingsRequest{
				ApplicationType: "web",
				GrantTypes:      []string{"authorization_code", "refresh_token"},
				ResponseTypes:   []string{"code"},
				RedirectURIs:    append([]string(nil), input.RedirectURIs...),
				ConsentMethod:   "TRUSTED",
			},
		},
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return Application{}, fmt.Errorf("encode Okta OIDC application: %w", err)
	}
	requestURL, err := c.urlFor(tenantDomain, "/api/v1/apps")
	if err != nil {
		return Application{}, fmt.Errorf("create Okta OIDC application: %w", err)
	}
	httpRequest, err := http.NewRequestWithContext(ctx, http.MethodPost, requestURL, bytes.NewReader(body))
	if err != nil {
		return Application{}, fmt.Errorf("create Okta OIDC application request: %w", err)
	}
	httpRequest.Header.Set("Accept", "application/json")
	httpRequest.Header.Set("Authorization", "Bearer "+accessToken)
	httpRequest.Header.Set("Content-Type", "application/json")

	var response applicationResponse
	if _, err := c.doAuthorized(c.nonRetryingHTTPClient, httpRequest, accessToken, &response); err != nil {
		return Application{}, err
	}
	return applicationFromResponse(response), nil
}

// GetApplication retrieves an Okta OIDC application by its application instance ID.
func (c *Client) GetApplication(ctx context.Context, tenantDomain, accessToken, applicationID string) (Application, error) {
	if applicationID == "" {
		return Application{}, errors.New("get Okta application: application ID is required")
	}
	requestURL, err := c.urlFor(tenantDomain, "/api/v1/apps/"+url.PathEscape(applicationID))
	if err != nil {
		return Application{}, fmt.Errorf("get Okta application: %w", err)
	}
	httpRequest, err := http.NewRequestWithContext(ctx, http.MethodGet, requestURL, nil)
	if err != nil {
		return Application{}, fmt.Errorf("create Okta get application request: %w", err)
	}
	httpRequest.Header.Set("Accept", "application/json")
	httpRequest.Header.Set("Authorization", "Bearer "+accessToken)

	var response applicationResponse
	if _, err := c.doAuthorized(c.httpClient, httpRequest, accessToken, &response); err != nil {
		return Application{}, err
	}
	return applicationFromResponse(response), nil
}

// ResolveApplicationByClientID finds an OIDC application by its public OAuth client ID.
func (c *Client) ResolveApplicationByClientID(ctx context.Context, tenantDomain, accessToken, clientID string) (Application, error) {
	if clientID == "" {
		return Application{}, errors.New("resolve Okta application: client ID is required")
	}
	filterValue := strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(clientID)
	responses, _, err := c.listApplicationResponses(ctx, tenantDomain, accessToken, `credentials.oauthClient.client_id eq "`+filterValue+`"`, "")
	if err != nil {
		var apiErr *APIError
		if !errors.As(err, &apiErr) || apiErr.StatusCode != http.StatusBadRequest {
			return Application{}, err
		}
	}
	for _, response := range responses {
		if response.Credentials.OAuthClient.ClientID == clientID {
			return applicationFromResponse(response), nil
		}
	}

	seenCursors := make(map[string]struct{})
	after := ""
	for {
		responses, next, err := c.listApplicationResponses(ctx, tenantDomain, accessToken, "", after)
		if err != nil {
			return Application{}, err
		}
		for _, response := range responses {
			if response.Credentials.OAuthClient.ClientID == clientID {
				return applicationFromResponse(response), nil
			}
		}
		if next == "" {
			break
		}
		if _, exists := seenCursors[next]; exists {
			return Application{}, errors.New("resolve Okta application: repeated pagination cursor")
		}
		seenCursors[next] = struct{}{}
		after = next
	}
	return Application{}, errors.New("resolve Okta application: application not found")
}

func (c *Client) listApplicationResponses(ctx context.Context, tenantDomain, accessToken, filter, after string) ([]applicationResponse, string, error) {
	requestURL, err := c.urlFor(tenantDomain, "/api/v1/apps")
	if err != nil {
		return nil, "", fmt.Errorf("resolve Okta application: %w", err)
	}
	parsed, err := url.Parse(requestURL)
	if err != nil {
		return nil, "", fmt.Errorf("parse Okta applications URL: %w", err)
	}
	query := parsed.Query()
	query.Set("limit", "200")
	if filter != "" {
		query.Set("filter", filter)
	}
	if after != "" {
		query.Set("after", after)
	}
	parsed.RawQuery = query.Encode()

	httpRequest, err := http.NewRequestWithContext(ctx, http.MethodGet, parsed.String(), nil)
	if err != nil {
		return nil, "", fmt.Errorf("create Okta applications request: %w", err)
	}
	httpRequest.Header.Set("Accept", "application/json")
	httpRequest.Header.Set("Authorization", "Bearer "+accessToken)

	var responses []applicationResponse
	headers, err := c.doAuthorized(c.httpClient, httpRequest, accessToken, &responses)
	if err != nil {
		return nil, "", err
	}
	return responses, nextCursor(headers.Get("Link"), parsed), nil
}

// FindEveryoneGroup finds Okta's built-in Everyone group.
func (c *Client) FindEveryoneGroup(ctx context.Context, tenantDomain, accessToken string) (Group, error) {
	requestURL, err := c.urlFor(tenantDomain, "/api/v1/groups")
	if err != nil {
		return Group{}, fmt.Errorf("find Okta Everyone group: %w", err)
	}
	parsed, err := url.Parse(requestURL)
	if err != nil {
		return Group{}, fmt.Errorf("parse Okta groups URL: %w", err)
	}
	query := parsed.Query()
	query.Set("filter", `type eq "BUILT_IN"`)
	query.Set("limit", "200")
	parsed.RawQuery = query.Encode()

	httpRequest, err := http.NewRequestWithContext(ctx, http.MethodGet, parsed.String(), nil)
	if err != nil {
		return Group{}, fmt.Errorf("create Okta groups request: %w", err)
	}
	httpRequest.Header.Set("Accept", "application/json")
	httpRequest.Header.Set("Authorization", "Bearer "+accessToken)

	var responses []struct {
		ID      string `json:"id"`
		Type    string `json:"type"`
		Profile struct {
			Name string `json:"name"`
		} `json:"profile"`
	}
	if _, err := c.doAuthorized(c.httpClient, httpRequest, accessToken, &responses); err != nil {
		return Group{}, err
	}
	for _, response := range responses {
		if response.Type == "BUILT_IN" && response.Profile.Name == "Everyone" {
			return Group{ID: response.ID, Name: response.Profile.Name}, nil
		}
	}
	return Group{}, errors.New("find Okta Everyone group: group not found")
}

// AssignGroupToApplication makes an Okta application available to a group.
func (c *Client) AssignGroupToApplication(ctx context.Context, tenantDomain, accessToken, applicationID, groupID string) error {
	if applicationID == "" || groupID == "" {
		return errors.New("assign Okta application group: application ID and group ID are required")
	}
	path := "/api/v1/apps/" + url.PathEscape(applicationID) + "/groups/" + url.PathEscape(groupID)
	requestURL, err := c.urlFor(tenantDomain, path)
	if err != nil {
		return fmt.Errorf("assign Okta application group: %w", err)
	}
	httpRequest, err := http.NewRequestWithContext(ctx, http.MethodPut, requestURL, bytes.NewReader([]byte(`{}`)))
	if err != nil {
		return fmt.Errorf("create Okta application group request: %w", err)
	}
	httpRequest.Header.Set("Accept", "application/json")
	httpRequest.Header.Set("Authorization", "Bearer "+accessToken)
	httpRequest.Header.Set("Content-Type", "application/json")

	var response struct{}
	if _, err := c.doAuthorized(c.nonRetryingHTTPClient, httpRequest, accessToken, &response); err != nil {
		return err
	}
	return nil
}

func (c *Client) list(ctx context.Context, tenantDomain, accessToken, path string, page PageRequest) (Page, error) {
	if page.Limit <= 0 {
		return Page{}, errors.New("list Okta resources: limit must be positive")
	}
	requestURL, err := c.urlFor(tenantDomain, path)
	if err != nil {
		return Page{}, fmt.Errorf("list Okta resources: %w", err)
	}
	parsed, err := url.Parse(requestURL)
	if err != nil {
		return Page{}, fmt.Errorf("parse Okta resource URL: %w", err)
	}
	query := parsed.Query()
	query.Set("limit", strconv.Itoa(page.Limit))
	if page.After != "" {
		query.Set("after", page.After)
	}
	parsed.RawQuery = query.Encode()

	httpRequest, err := http.NewRequestWithContext(ctx, http.MethodGet, parsed.String(), nil)
	if err != nil {
		return Page{}, fmt.Errorf("create Okta resource request: %w", err)
	}
	httpRequest.Header.Set("Accept", "application/json")
	httpRequest.Header.Set("Authorization", "Bearer "+accessToken)

	var items []json.RawMessage
	headers, err := c.doAuthorized(c.httpClient, httpRequest, accessToken, &items)
	if err != nil {
		return Page{}, err
	}
	return Page{
		Items:      items,
		NextCursor: nextCursor(headers.Get("Link"), parsed),
		RateLimit:  parseRateLimit(headers),
	}, nil
}

func (c *Client) doAuthorized(httpClient *guardian.HTTPClient, request *http.Request, accessToken string, output any) (http.Header, error) {
	dpop, nonce, key := c.dpopToken(accessToken)
	if !dpop {
		request.Header.Set("Authorization", "Bearer "+accessToken)
		return c.do(httpClient, request, output)
	}

	proofURL := *request.URL
	proofURL.RawQuery = ""
	proofURL.ForceQuery = false
	proofURL.Fragment = ""
	for attempt := range 2 {
		attemptRequest := request
		if attempt > 0 {
			attemptRequest = request.Clone(request.Context())
			if request.GetBody != nil {
				body, err := request.GetBody()
				if err != nil {
					return nil, fmt.Errorf("recreate Okta DPoP request body: %w", err)
				}
				attemptRequest.Body = body
			}
		}
		proof, err := c.dpopProof(request.Method, proofURL.String(), accessToken, nonce)
		if err != nil {
			return nil, err
		}
		attemptRequest.Header.Set("Authorization", "DPoP "+accessToken)
		attemptRequest.Header.Set("DPoP", proof)
		headers, err := c.do(c.nonRetryingHTTPClient, attemptRequest, output)
		responseNonce := headers.Get(dpopNonceHeader)
		if responseNonce != "" {
			nonce = responseNonce
			c.setDPoPNonce(key, accessToken, nonce)
		}
		if attempt == 0 && responseNonce != "" && isDPoPNonceChallenge(err) {
			continue
		}
		return headers, err
	}
	return nil, errors.New("okta DPoP nonce retry exhausted")
}

func (c *Client) dpopToken(accessToken string) (bool, string, cacheKey) {
	c.mu.Lock()
	defer c.mu.Unlock()
	bound, ok := c.dpopTokens[accessToken]
	if ok {
		return true, bound.nonce, bound.key
	}
	return false, "", cacheKey{connectionID: uuid.Nil, kid: ""}
}

func (c *Client) setDPoPNonce(key cacheKey, accessToken, nonce string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	bound, ok := c.dpopTokens[accessToken]
	if ok && bound.key == key {
		bound.nonce = nonce
		c.dpopTokens[accessToken] = bound
	}
	cached, ok := c.tokens[key]
	if ok && cached.token.AccessToken == accessToken && cached.dpop {
		cached.dpopNonce = nonce
		c.tokens[key] = cached
	}
}

func (c *Client) do(httpClient *guardian.HTTPClient, request *http.Request, output any) (http.Header, error) {
	response, err := httpClient.Do(request)
	if err != nil {
		return nil, fmt.Errorf("send Okta request: %w", err)
	}
	defer o11y.NoLogDefer(func() error { return response.Body.Close() })

	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		body, readErr := io.ReadAll(io.LimitReader(response.Body, maxErrorBodyBytes+1))
		if readErr != nil {
			return nil, fmt.Errorf("read Okta error response: %w", readErr)
		}
		var providerError struct {
			Error            string `json:"error"`
			ErrorDescription string `json:"error_description"`
			ErrorCode        string `json:"errorCode"`
			ErrorSummary     string `json:"errorSummary"`
		}
		_ = json.Unmarshal(body, &providerError)
		code := providerError.Error
		if code == "" {
			code = providerError.ErrorCode
		}
		if code == "" && oauthChallengeHasError(response.Header.Get("WWW-Authenticate"), "use_dpop_nonce") {
			code = "use_dpop_nonce"
		}
		description := providerError.ErrorDescription
		if description == "" {
			description = providerError.ErrorSummary
		}
		if description == "" {
			description = http.StatusText(response.StatusCode)
		}
		code = redactRequestCredentials(request, code)
		description = redactRequestCredentials(request, description)
		return response.Header.Clone(), &APIError{
			Method:      request.Method,
			Path:        request.URL.Path,
			StatusCode:  response.StatusCode,
			Code:        code,
			Description: description,
			RateLimit:   parseRateLimit(response.Header),
		}
	}

	limited := &io.LimitedReader{R: response.Body, N: maxResponseBodyBytes + 1}
	if err := json.NewDecoder(limited).Decode(output); err != nil {
		return nil, fmt.Errorf("decode Okta response: %w", err)
	}
	if limited.N <= 0 {
		return nil, fmt.Errorf("decode Okta response: response exceeds %d bytes", maxResponseBodyBytes)
	}
	return response.Header.Clone(), nil
}

func oauthChallengeHasError(challenge, target string) bool {
	for field := range strings.SplitSeq(challenge, ",") {
		field = strings.TrimSpace(field)
		if _, rest, ok := strings.Cut(field, " "); ok {
			field = rest
		}
		name, value, ok := strings.Cut(field, "=")
		if ok && strings.EqualFold(strings.TrimSpace(name), "error") && strings.Trim(strings.TrimSpace(value), `"`) == target {
			return true
		}
	}
	return false
}

func (c *Client) urlFor(tenantDomain, path string) (string, error) {
	base := c.endpoint
	if base == "" {
		base = "https://" + tenantDomain
	}
	result, err := url.JoinPath(base, path)
	if err != nil {
		return "", fmt.Errorf("build Okta URL: %w", err)
	}
	return result, nil
}

func nextCursor(header string, origin *url.URL) string {
	for link := range strings.SplitSeq(header, ",") {
		parts := strings.Split(link, ";")
		isNext := false
		for _, parameter := range parts[1:] {
			name, value, ok := strings.Cut(strings.TrimSpace(parameter), "=")
			if ok && strings.EqualFold(name, "rel") && strings.EqualFold(strings.Trim(value, `"`), "next") {
				isNext = true
				break
			}
		}
		if len(parts) < 2 || !isNext {
			continue
		}
		target := strings.Trim(strings.TrimSpace(parts[0]), "<>")
		parsed, err := url.Parse(target)
		if err != nil || !strings.EqualFold(parsed.Scheme, origin.Scheme) || !strings.EqualFold(parsed.Host, origin.Host) {
			continue
		}
		return parsed.Query().Get("after")
	}
	return ""
}

func parseRateLimit(headers http.Header) RateLimit {
	var limit *int64
	if value, err := strconv.ParseInt(headers.Get("X-Rate-Limit-Limit"), 10, 64); err == nil {
		limit = &value
	}
	var remaining *int64
	if value, err := strconv.ParseInt(headers.Get("X-Rate-Limit-Remaining"), 10, 64); err == nil {
		remaining = &value
	}
	var reset *time.Time
	if value, err := strconv.ParseInt(headers.Get("X-Rate-Limit-Reset"), 10, 64); err == nil {
		parsed := time.Unix(value, 0)
		reset = &parsed
	}
	return RateLimit{Limit: limit, Remaining: remaining, Reset: reset}
}

func redactRequestCredentials(request *http.Request, value string) string {
	credentials := make([]string, 0, 2)
	if authorization := request.Header.Get("Authorization"); authorization != "" {
		_, credential, found := strings.Cut(authorization, " ")
		if found && credential != "" {
			credentials = append(credentials, credential)
		}
	}
	if proof := request.Header.Get("DPoP"); proof != "" {
		credentials = append(credentials, proof)
	}
	if request.GetBody != nil {
		body, err := request.GetBody()
		if err == nil {
			defer o11y.NoLogDefer(func() error { return body.Close() })
			encoded, readErr := io.ReadAll(io.LimitReader(body, maxErrorBodyBytes))
			if readErr == nil {
				if form, parseErr := url.ParseQuery(string(encoded)); parseErr == nil {
					if assertion := form.Get("client_assertion"); assertion != "" {
						credentials = append(credentials, assertion)
					}
				}
				if strings.Contains(request.Header.Get("Content-Type"), "application/json") {
					var payload any
					if json.Unmarshal(encoded, &payload) == nil {
						credentials = appendJSONCredentials(credentials, payload)
					}
				}
			}
		}
	}
	for _, credential := range credentials {
		value = strings.ReplaceAll(value, credential, "[redacted]")
	}
	return value
}

func appendJSONCredentials(credentials []string, value any) []string {
	switch typed := value.(type) {
	case map[string]any:
		for key, child := range typed {
			if key == "client_secret" {
				if secret, ok := child.(string); ok && secret != "" {
					credentials = append(credentials, secret)
				}
				continue
			}
			credentials = appendJSONCredentials(credentials, child)
		}
	case []any:
		for _, child := range typed {
			credentials = appendJSONCredentials(credentials, child)
		}
	}
	return credentials
}

func cloneToken(token Token) Token {
	return Token{
		AccessToken:   token.AccessToken,
		ExpiresAt:     token.ExpiresAt,
		GrantedScopes: append([]string(nil), token.GrantedScopes...),
	}
}

func applicationFromResponse(response applicationResponse) Application {
	return Application{
		ID:       response.ID,
		Status:   response.Status,
		Label:    response.Label,
		ClientID: response.Credentials.OAuthClient.ClientID,
	}
}
