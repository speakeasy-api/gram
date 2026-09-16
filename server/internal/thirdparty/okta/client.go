package okta

import (
	"context"
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

	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/oautherr"
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
)

const (
	clientAssertionType = "urn:ietf:params:oauth:client-assertion-type:jwt-bearer"
	tokenEndpointPath   = "/oauth2/v1/token" //nolint:gosec // G101 false positive: a URL path, not a credential.

	maxResponseBytes        = 1 << 20
	tokenExpirySafetyMargin = 60 * time.Second
	maxRateLimitRetries     = 3
	maxRateLimitWait        = 2 * time.Minute
	rateLimitSlowdownRatio  = 0.2

	defaultMaxPages          = 50
	defaultRequestsPerMinute = 300
)

// defaultScopes are requested on every normal token mint.
var defaultScopes = []string{"okta.apps.read", "okta.users.read", "okta.groups.read"}

// Config identifies the Okta org and the remote session client whose
// private_key_jwt credential authenticates against it.
type Config struct {
	// OrgURL is the Okta org base URL, for example https://example.okta.com.
	OrgURL string

	// ClientID is the Okta API Services application client id.
	ClientID string

	// AudienceFormat is the remote_session_clients
	// token_endpoint_auth_audience_format value and must be "token_endpoint";
	// provisioning writes that value on the row.
	AudienceFormat string

	// RemoteSessionClientID is the remote_session_clients row id.
	RemoteSessionClientID uuid.UUID

	// OrganizationID is the Gram organization that owns the credential.
	OrganizationID string

	// JSONWebKeySetID is the organization key set holding the signing key.
	JSONWebKeySetID uuid.UUID

	// MaxPages caps how many pages a list call follows; zero uses 50.
	MaxPages int
}

type cachedToken struct {
	accessToken string
	tokenType   string
	scope       string
	expiresAt   time.Time
}

var noToken = cachedToken{accessToken: "", tokenType: "", scope: "", expiresAt: time.Time{}}

type httpClient struct {
	logger     *slog.Logger
	cfg        Config
	httpClient *guardian.HTTPClient
	signer     remotesessions.TokenEndpointAssertionSigner
	key        *dpopKey
	orgURL     *url.URL
	tokenURL   *url.URL
	audience   string
	maxPages   int
	now        func() time.Time
	sleep      func(ctx context.Context, d time.Duration) error

	// mintMu serializes token minting so concurrent callers share one handshake.
	mintMu sync.Mutex

	mu        sync.Mutex
	cached    cachedToken
	nonce     string
	slowUntil time.Time
}

var _ Client = (*httpClient)(nil)

// NewClient builds an Okta Management API client for one org with its own
// ephemeral DPoP key and token cache. The client never follows redirects:
// a 3xx from Okta surfaces as an APIError so credentials are only ever sent
// to the configured org.
func NewClient(logger *slog.Logger, client *guardian.HTTPClient, signer remotesessions.TokenEndpointAssertionSigner, cfg Config) (Client, error) {
	orgURL, err := parseOrgURL(cfg.OrgURL)
	if err != nil {
		return nil, err
	}
	if cfg.ClientID == "" {
		return nil, errors.New("okta: client id is required")
	}
	if cfg.AudienceFormat != string(remotesessions.TokenEndpointAuthAudienceTokenEndpoint) {
		return nil, fmt.Errorf("okta: audience format %q must be %q", cfg.AudienceFormat, remotesessions.TokenEndpointAuthAudienceTokenEndpoint)
	}

	tokenURL := orgURL.JoinPath(tokenEndpointPath)
	audience, err := remotesessions.ResolveTokenEndpointAuthAudience(cfg.AudienceFormat, orgURL.String(), tokenURL.String())
	if err != nil {
		return nil, fmt.Errorf("okta: resolve client assertion audience: %w", err)
	}

	key, err := newDPoPKey()
	if err != nil {
		return nil, fmt.Errorf("okta: %w", err)
	}

	maxPages := cfg.MaxPages
	if maxPages <= 0 {
		maxPages = defaultMaxPages
	}

	// A shallow copy shares the pooled transport but pins the redirect policy.
	noRedirect := *client
	noRedirect.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }

	return &httpClient{
		logger:     logger.With(attr.SlogComponent("okta_client")),
		cfg:        cfg,
		httpClient: &noRedirect,
		signer:     signer,
		key:        key,
		orgURL:     orgURL,
		tokenURL:   tokenURL,
		audience:   audience,
		maxPages:   maxPages,
		now:        time.Now,
		sleep:      sleepContext,
		mintMu:     sync.Mutex{},
		mu:         sync.Mutex{},
		cached:     noToken,
		nonce:      "",
		slowUntil:  time.Time{},
	}, nil
}

// parseOrgURL accepts an https origin, or an http origin on a loopback host
// for local stubs, with no path, query, fragment, or userinfo.
func parseOrgURL(raw string) (*url.URL, error) {
	orgURL, err := url.Parse(strings.TrimRight(raw, "/"))
	if err != nil || orgURL.Host == "" || orgURL.Opaque != "" {
		return nil, fmt.Errorf("okta: invalid org url %q", raw)
	}
	if orgURL.Path != "" || orgURL.RawQuery != "" || orgURL.Fragment != "" || orgURL.User != nil {
		return nil, fmt.Errorf("okta: org url %q must be an origin without path, query, fragment, or userinfo", raw)
	}
	switch orgURL.Scheme {
	case "https":
	case "http":
		if !isLoopbackHost(orgURL.Hostname()) {
			return nil, fmt.Errorf("okta: org url %q must use https", raw)
		}
	default:
		return nil, fmt.Errorf("okta: org url %q must use https", raw)
	}
	return orgURL, nil
}

func isLoopbackHost(host string) bool {
	switch host {
	case "localhost", "127.0.0.1", "::1":
		return true
	default:
		return false
	}
}

func sleepContext(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return fmt.Errorf("wait: %w", ctx.Err())
	case <-timer.C:
		return nil
	}
}

func (c *httpClient) ListApps(ctx context.Context, req ListAppsRequest) ([]App, error) {
	q := url.Values{}
	setQuery(q, "q", req.Query)
	if req.Status != "" {
		q.Set("filter", fmt.Sprintf("status eq %q", req.Status))
	}
	setLimit(q, req.Limit)

	raw, err := listAll[appJSON](ctx, c, "/api/v1/apps", q)
	if err != nil {
		return nil, err
	}
	out := make([]App, 0, len(raw))
	for _, a := range raw {
		out = append(out, a.toApp())
	}
	return out, nil
}

func (c *httpClient) GetApp(ctx context.Context, appID string) (*App, error) {
	if appID == "" {
		return nil, errors.New("okta: app id is required")
	}
	raw, _, err := getJSON[appJSON](ctx, c, c.apiURL("/api/v1/apps", appID, nil))
	if err != nil {
		return nil, err
	}
	app := raw.toApp()
	return &app, nil
}

func (c *httpClient) ListAppUsers(ctx context.Context, req ListAppUsersRequest) ([]AppUser, error) {
	if req.AppID == "" {
		return nil, errors.New("okta: app id is required")
	}
	q := url.Values{}
	setLimit(q, req.Limit)

	raw, err := listAll[appUserJSON](ctx, c, "/api/v1/apps/"+url.PathEscape(req.AppID)+"/users", q)
	if err != nil {
		return nil, err
	}
	out := make([]AppUser, 0, len(raw))
	for _, u := range raw {
		out = append(out, AppUser{
			ID:          u.ID,
			Scope:       u.Scope,
			Status:      u.Status,
			UserName:    u.Credentials.UserName,
			Created:     u.Created,
			LastUpdated: u.LastUpdated,
		})
	}
	return out, nil
}

func (c *httpClient) ListAppGroups(ctx context.Context, req ListAppGroupsRequest) ([]AppGroup, error) {
	if req.AppID == "" {
		return nil, errors.New("okta: app id is required")
	}
	q := url.Values{}
	setLimit(q, req.Limit)

	raw, err := listAll[appGroupJSON](ctx, c, "/api/v1/apps/"+url.PathEscape(req.AppID)+"/groups", q)
	if err != nil {
		return nil, err
	}
	out := make([]AppGroup, 0, len(raw))
	for _, g := range raw {
		out = append(out, AppGroup(g))
	}
	return out, nil
}

func (c *httpClient) ListGroups(ctx context.Context, req ListGroupsRequest) ([]Group, error) {
	q := url.Values{}
	setQuery(q, "q", req.Search)
	setLimit(q, req.Limit)

	raw, err := listAll[groupJSON](ctx, c, "/api/v1/groups", q)
	if err != nil {
		return nil, err
	}
	out := make([]Group, 0, len(raw))
	for _, g := range raw {
		out = append(out, Group{
			ID:          g.ID,
			Type:        g.Type,
			Name:        g.Profile.Name,
			Description: g.Profile.Description,
			Created:     g.Created,
			LastUpdated: g.LastUpdated,
		})
	}
	return out, nil
}

// VerifyScopes always mints a fresh token for the required scopes; the
// verification token is never cached because it may lack default scopes.
func (c *httpClient) VerifyScopes(ctx context.Context, required []string) (*ScopeVerification, error) {
	c.mintMu.Lock()
	defer c.mintMu.Unlock()
	c.evictToken()

	tok, err := c.mint(ctx, required)
	var apiErr *APIError
	if errors.As(err, &apiErr) && apiErr.StatusCode == http.StatusBadRequest && apiErr.ErrorCode == oautherr.CodeInvalidScope {
		return &ScopeVerification{
			Granted:   make([]string, 0),
			Missing:   append([]string(nil), required...),
			DPoPBound: false,
			ExpiresAt: time.Time{},
		}, nil
	}
	if err != nil {
		return nil, err
	}

	granted := make([]string, 0)
	have := make(map[string]struct{})
	for s := range strings.FieldsSeq(tok.scope) {
		granted = append(granted, s)
		have[s] = struct{}{}
	}
	missing := make([]string, 0)
	for _, s := range required {
		if _, ok := have[s]; !ok {
			missing = append(missing, s)
		}
	}
	return &ScopeVerification{
		Granted:   granted,
		Missing:   missing,
		DPoPBound: strings.EqualFold(tok.tokenType, "DPoP"),
		ExpiresAt: tok.expiresAt,
	}, nil
}

func setQuery(q url.Values, key, value string) {
	if value != "" {
		q.Set(key, value)
	}
}

func setLimit(q url.Values, limit int) {
	if limit > 0 {
		q.Set("limit", strconv.Itoa(limit))
	}
}

func (c *httpClient) apiURL(base, id string, q url.Values) *url.URL {
	target := c.orgURL.JoinPath(base)
	if id != "" {
		target = target.JoinPath(id)
	}
	if len(q) > 0 {
		target.RawQuery = q.Encode()
	}
	return target
}

func listAll[T any](ctx context.Context, c *httpClient, path string, q url.Values) ([]T, error) {
	target := c.apiURL(path, "", q)
	items := make([]T, 0)
	pages := 0
	for target != nil {
		if pages >= c.maxPages {
			return nil, fmt.Errorf("%w: %s after %d pages", ErrTooManyPages, path, pages)
		}
		pages++

		page, next, err := getJSON[[]T](ctx, c, target)
		if err != nil {
			return nil, err
		}
		items = append(items, page...)
		target = next
	}
	return items, nil
}

func getJSON[T any](ctx context.Context, c *httpClient, target *url.URL) (T, *url.URL, error) {
	var out T
	body, header, err := c.do(ctx, http.MethodGet, target)
	if err != nil {
		return out, nil, err
	}
	if err := json.Unmarshal(body, &out); err != nil {
		return out, nil, fmt.Errorf("decode okta %s response: %w", target.Path, err)
	}
	next, err := nextLink(header, target)
	if err != nil {
		return out, nil, fmt.Errorf("parse okta %s next link: %w", target.Path, err)
	}
	return out, next, nil
}

// do retries once for a nonce challenge, once for a rejected token, and up to
// maxRateLimitRetries times for 429.
func (c *httpClient) do(ctx context.Context, method string, target *url.URL) ([]byte, http.Header, error) {
	nonceRetried := false
	tokenRetried := false
	rateLimitRetries := 0

	for {
		if err := c.waitForSlowdown(ctx); err != nil {
			return nil, nil, err
		}
		tok, err := c.token(ctx)
		if err != nil {
			return nil, nil, err
		}

		c.mu.Lock()
		nonce := c.nonce
		c.mu.Unlock()

		proof, err := c.key.proof(method, target, nonce, tok.accessToken, c.now())
		if err != nil {
			return nil, nil, err
		}

		req, err := http.NewRequestWithContext(ctx, method, target.String(), nil)
		if err != nil {
			return nil, nil, fmt.Errorf("build okta request: %w", err)
		}
		req.Header.Set("Accept", "application/json")
		req.Header.Set("Authorization", "DPoP "+tok.accessToken)
		req.Header.Set("DPoP", proof)

		status, header, body, err := c.send(req)
		if err != nil {
			return nil, nil, fmt.Errorf("okta %s %s: %w", method, target.Path, err)
		}
		c.rememberNonce(header)
		if status != http.StatusTooManyRequests {
			c.observeRateLimit(header)
		}

		switch {
		case status >= 200 && status < 300:
			return body, header, nil

		case status == http.StatusTooManyRequests && rateLimitRetries < maxRateLimitRetries:
			rateLimitRetries++
			wait := c.rateLimitWait(header)
			c.logger.WarnContext(ctx, "okta rate limited, backing off",
				attr.SlogHTTPRequestMethod(method),
				attr.SlogHTTPRoute(target.Path),
				attr.SlogHTTPResponseStatusCode(status),
			)
			if err := c.sleep(ctx, wait); err != nil {
				return nil, nil, err
			}
			continue

		case status == http.StatusUnauthorized:
			if isUseDPoPNonce(header, body) {
				if nonceRetried {
					return nil, nil, newAPIError(method, target.Path, status, body)
				}
				nonceRetried = true
				continue
			}
			if tokenRetried {
				return nil, nil, newAPIError(method, target.Path, status, body)
			}
			tokenRetried = true
			c.evictToken()
			continue

		default:
			return nil, nil, newAPIError(method, target.Path, status, body)
		}
	}
}

// send reads at most maxResponseBytes and never logs headers.
func (c *httpClient) send(req *http.Request) (int, http.Header, []byte, error) {
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return 0, nil, nil, fmt.Errorf("send: %w", err)
	}
	defer o11y.NoLogDefer(func() error { return resp.Body.Close() })

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes+1))
	if err != nil {
		return 0, nil, nil, fmt.Errorf("read response: %w", err)
	}
	if len(body) > maxResponseBytes {
		return 0, nil, nil, ErrResponseTooLarge
	}
	return resp.StatusCode, resp.Header, body, nil
}

func (c *httpClient) cachedToken() (cachedToken, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.cached.accessToken == "" || !c.now().Before(c.cached.expiresAt.Add(-tokenExpirySafetyMargin)) {
		return noToken, false
	}
	return c.cached, true
}

func (c *httpClient) evictToken() {
	c.mu.Lock()
	c.cached = noToken
	c.mu.Unlock()
}

// token returns the cached DPoP token or mints one under mintMu.
func (c *httpClient) token(ctx context.Context) (cachedToken, error) {
	if tok, ok := c.cachedToken(); ok {
		return tok, nil
	}

	c.mintMu.Lock()
	defer c.mintMu.Unlock()
	if tok, ok := c.cachedToken(); ok {
		return tok, nil
	}

	tok, err := c.mint(ctx, defaultScopes)
	if err != nil {
		return cachedToken{}, err
	}
	if !strings.EqualFold(tok.tokenType, "DPoP") {
		return cachedToken{}, fmt.Errorf("okta token response type %q is not DPoP", tok.tokenType)
	}

	c.mu.Lock()
	c.cached = tok
	c.mu.Unlock()
	return tok, nil
}

// mint runs a client-credentials grant, retrying once for use_dpop_nonce and
// up to maxRateLimitRetries times for 429. Every attempt signs a fresh
// assertion because Okta burns the jti on first use.
func (c *httpClient) mint(ctx context.Context, scopes []string) (cachedToken, error) {
	c.mu.Lock()
	nonce := c.nonce
	c.mu.Unlock()

	nonceRetried := false
	rateLimitRetries := 0
	for {
		status, header, body, err := c.requestToken(ctx, nonce, scopes)
		if err != nil {
			return cachedToken{}, err
		}

		switch {
		case status == http.StatusOK:
			return parseTokenResponse(body, c.now())

		case status == http.StatusTooManyRequests && rateLimitRetries < maxRateLimitRetries:
			rateLimitRetries++
			c.logger.WarnContext(ctx, "okta token endpoint rate limited, backing off",
				attr.SlogHTTPRequestMethod(http.MethodPost),
				attr.SlogHTTPRoute(tokenEndpointPath),
				attr.SlogHTTPResponseStatusCode(status),
			)
			if err := c.sleep(ctx, c.rateLimitWait(header)); err != nil {
				return cachedToken{}, err
			}
			continue

		case status == http.StatusBadRequest && isUseDPoPNonce(header, body):
			if nonceRetried {
				return cachedToken{}, errors.New("okta token endpoint rejected dpop nonce twice")
			}
			nonceRetried = true
			nonce = header.Get("DPoP-Nonce")
			continue

		default:
			return cachedToken{}, newAPIError(http.MethodPost, tokenEndpointPath, status, body)
		}
	}
}

func parseTokenResponse(body []byte, now time.Time) (cachedToken, error) {
	var parsed tokenResponse
	if err := json.Unmarshal(body, &parsed); err != nil {
		return cachedToken{}, fmt.Errorf("decode okta token response: %w", err)
	}
	if parsed.AccessToken == "" {
		return cachedToken{}, errors.New("okta token response has no access token")
	}
	if parsed.ExpiresIn <= 0 {
		return cachedToken{}, errors.New("okta token response has no expiry")
	}
	return cachedToken{
		accessToken: parsed.AccessToken,
		tokenType:   parsed.TokenType,
		scope:       parsed.Scope,
		expiresAt:   now.Add(time.Duration(parsed.ExpiresIn) * time.Second),
	}, nil
}

func (c *httpClient) rememberNonce(header http.Header) {
	nonce := header.Get("DPoP-Nonce")
	if nonce == "" {
		return
	}
	c.mu.Lock()
	c.nonce = nonce
	c.mu.Unlock()
}

// requestToken signs one assertion and posts it to the token endpoint,
// returning the raw response for mint to interpret.
func (c *httpClient) requestToken(ctx context.Context, nonce string, scopes []string) (int, http.Header, []byte, error) {
	assertion, err := c.signer.SignClientAssertion(ctx, remotesessions.ClientAssertionRequest{
		RemoteSessionClientID: c.cfg.RemoteSessionClientID,
		OrganizationID:        c.cfg.OrganizationID,
		JSONWebKeySetID:       c.cfg.JSONWebKeySetID,
		ClientID:              c.cfg.ClientID,
		Audience:              c.audience,
	})
	if err != nil {
		return 0, nil, nil, fmt.Errorf("sign okta client assertion: %w", err)
	}

	proof, err := c.key.proof(http.MethodPost, c.tokenURL, nonce, "", c.now())
	if err != nil {
		return 0, nil, nil, err
	}

	form := url.Values{
		"grant_type":            {"client_credentials"},
		"client_id":             {c.cfg.ClientID},
		"client_assertion_type": {clientAssertionType},
		"client_assertion":      {assertion},
	}
	if len(scopes) > 0 {
		form.Set("scope", strings.Join(scopes, " "))
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.tokenURL.String(), strings.NewReader(form.Encode()))
	if err != nil {
		return 0, nil, nil, fmt.Errorf("build okta token request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("DPoP", proof)

	status, header, body, err := c.send(req)
	if err != nil {
		return 0, nil, nil, fmt.Errorf("okta token request: %w", err)
	}
	c.rememberNonce(header)
	if status != http.StatusTooManyRequests {
		c.observeRateLimit(header)
	}
	return status, header, body, nil
}

func isUseDPoPNonce(header http.Header, body []byte) bool {
	if header.Get("DPoP-Nonce") == "" {
		return false
	}
	if strings.Contains(header.Get("WWW-Authenticate"), oautherr.CodeUseDPoPNonce) {
		return true
	}
	var parsed struct {
		Error string `json:"error"`
	}
	return json.Unmarshal(body, &parsed) == nil && parsed.Error == oautherr.CodeUseDPoPNonce
}

// observeRateLimit pauses until reset once fewer than rateLimitSlowdownRatio of quota remains.
func (c *httpClient) observeRateLimit(header http.Header) {
	limit, err := strconv.ParseFloat(header.Get("X-Rate-Limit-Limit"), 64)
	if err != nil || limit <= 0 {
		return
	}
	remaining, err := strconv.ParseFloat(header.Get("X-Rate-Limit-Remaining"), 64)
	if err != nil {
		return
	}
	if remaining >= limit*rateLimitSlowdownRatio {
		return
	}
	reset := c.resetTime(header)
	if latest := c.now().Add(maxRateLimitWait); reset.After(latest) {
		reset = latest
	}
	c.mu.Lock()
	if reset.After(c.slowUntil) {
		c.slowUntil = reset
	}
	c.mu.Unlock()
}

// waitForSlowdown sleeps until slowUntil and clears it only if no concurrent
// observer moved the deadline while this waiter slept.
func (c *httpClient) waitForSlowdown(ctx context.Context) error {
	c.mu.Lock()
	deadline := c.slowUntil
	wait := deadline.Sub(c.now())
	c.mu.Unlock()
	if wait <= 0 {
		return nil
	}
	if err := c.sleep(ctx, min(wait, maxRateLimitWait)); err != nil {
		return err
	}
	c.mu.Lock()
	if c.slowUntil.Equal(deadline) {
		c.slowUntil = time.Time{}
	}
	c.mu.Unlock()
	return nil
}

func (c *httpClient) rateLimitWait(header http.Header) time.Duration {
	wait := c.resetTime(header).Sub(c.now())
	if wait <= 0 {
		wait = time.Second
	}
	return min(wait, maxRateLimitWait)
}

func (c *httpClient) resetTime(header http.Header) time.Time {
	epoch, err := strconv.ParseInt(header.Get("X-Rate-Limit-Reset"), 10, 64)
	if err != nil {
		return c.now().Add(time.Second)
	}
	return time.Unix(epoch, 0)
}

func nextLink(header http.Header, base *url.URL) (*url.URL, error) {
	for _, link := range header.Values("Link") {
		for part := range strings.SplitSeq(link, ",") {
			fields := strings.Split(part, ";")
			target := strings.TrimSpace(fields[0])
			if !strings.HasPrefix(target, "<") || !strings.HasSuffix(target, ">") {
				continue
			}
			if !hasNextRel(fields[1:]) {
				continue
			}
			next, err := base.Parse(strings.Trim(target, "<>"))
			if err != nil {
				return nil, fmt.Errorf("parse link: %w", err)
			}
			if next.Host != base.Host || next.Scheme != base.Scheme {
				return nil, fmt.Errorf("next link host %q does not match %q", next.Host, base.Host)
			}
			return next, nil
		}
	}
	return nil, nil
}

func hasNextRel(params []string) bool {
	for _, p := range params {
		k, v, ok := strings.Cut(strings.TrimSpace(p), "=")
		if !ok || !strings.EqualFold(strings.TrimSpace(k), "rel") {
			continue
		}
		if strings.EqualFold(strings.Trim(strings.TrimSpace(v), `"`), "next") {
			return true
		}
	}
	return false
}

func newAPIError(method, path string, status int, body []byte) *APIError {
	var parsed struct {
		ErrorCode        string `json:"errorCode"`
		ErrorSummary     string `json:"errorSummary"`
		Error            string `json:"error"`
		ErrorDescription string `json:"error_description"`
	}
	_ = json.Unmarshal(body, &parsed)
	code := parsed.ErrorCode
	if code == "" {
		code = parsed.Error
	}
	summary := parsed.ErrorSummary
	if summary == "" {
		summary = parsed.ErrorDescription
	}
	return &APIError{Method: method, Path: path, StatusCode: status, ErrorCode: code, Summary: summary}
}
