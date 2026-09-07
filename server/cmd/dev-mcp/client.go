package main

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"sync"
)

const (
	sessionCookieName = "gram_refresh"
	accessCookieName  = "gram_session"
)

// apiClient is a session-authenticated client for the local server's
// management API. Sessions are minted lazily by walking the dashboard's OIDC
// login flow — the local dev-idp auto-approves, so the whole redirect chain
// completes without interaction — and the resulting cookie lives in the
// client's jar for the life of the process. Short-lived access credentials are
// exchanged from that cookie and sent only in the Gram-Session header.
type apiClient struct {
	base   *url.URL
	origin string
	hc     *http.Client
	logger *slog.Logger

	mu          sync.Mutex
	sessionOK   bool
	accessToken string
}

func newAPIClient(base *url.URL, origin string, insecure bool, logger *slog.Logger) *apiClient {
	var transport http.RoundTripper
	if insecure {
		transport = &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, //nolint:gosec // opt-in flag for local self-signed certs
		}
	}

	return &apiClient{
		base:   base,
		origin: origin,
		logger: logger,
		hc: &http.Client{
			Jar:       &authCookieJar{},
			Transport: transport,
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				// Cookies are host-scoped, not port-scoped. Stop before the jar
				// can attach credentials to a different origin's request.
				if !sameOrigin(req.URL, base) {
					return fmt.Errorf("cross-origin API redirect blocked")
				}
				if len(via) >= 10 {
					return fmt.Errorf("stopped after 10 redirects")
				}
				return nil
			},
		},
	}
}

func (c *apiClient) hasSessionCookie() bool {
	for _, ck := range c.hc.Jar.Cookies(c.base.ResolveReference(&url.URL{Path: "/rpc/auth.refresh"})) {
		if ck.Name == sessionCookieName && ck.Value != "" {
			return true
		}
	}
	return false
}

// login walks GET /rpc/auth.login through the IDP and back to the auth
// callback, leaving a session cookie in the jar.
func (c *apiClient) login(ctx context.Context) error {
	loginURL := c.base.JoinPath("/rpc/auth.login").String()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, loginURL, nil)
	if err != nil {
		return fmt.Errorf("build login request: %w", err)
	}

	// Only login may cross origins to visit the IDP. Its shared jar still
	// confines dashboard credentials to the API origin, including the port.
	loginClient := *c.hc
	loginClient.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		// Re-populate cookies from the origin-aware jar, rather than copying
		// the initial request's Cookie header across a same-host port change.
		req.Header.Del("Cookie")
		if len(via) > 0 && sameOrigin(via[len(via)-1].URL, c.base) && via[len(via)-1].URL.Path == "/rpc/auth.callback" {
			// The callback has stored the cookie; the dashboard need not run.
			return http.ErrUseLastResponse
		}
		if len(via) >= 10 {
			return fmt.Errorf("stopped after 10 redirects")
		}
		return nil
	}
	resp, err := loginClient.Do(req)
	if err != nil {
		return fmt.Errorf("walk login flow: %w", err)
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	_ = resp.Body.Close()

	if !c.hasSessionCookie() {
		return fmt.Errorf("login flow finished with status %d but no %s cookie; is the local stack (server + dev-idp) running?", resp.StatusCode, sessionCookieName)
	}

	c.logger.InfoContext(ctx, "logged in to local server")
	return nil
}

func (c *apiClient) ensureSession(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.sessionOK {
		return nil
	}
	if !c.hasSessionCookie() {
		if err := c.login(ctx); err != nil {
			return err
		}
	}
	token, status, err := c.refresh(ctx)
	if status == http.StatusUnauthorized {
		if err := c.login(ctx); err != nil {
			return err
		}
		token, _, err = c.refresh(ctx)
	}
	if err != nil {
		return err
	}
	c.accessToken = token
	c.sessionOK = true
	return nil
}

// refresh exchanges the path-scoped HttpOnly cookie without exposing its value.
// The configured dashboard origin must match the API's CSRF allowlist.
func (c *apiClient) refresh(ctx context.Context) (string, int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.base.JoinPath("/rpc/auth.refresh").String(), nil)
	if err != nil {
		return "", 0, fmt.Errorf("build refresh request: %w", err)
	}
	req.Header.Set("Origin", c.origin)
	resp, err := c.hc.Do(req)
	if err != nil {
		return "", 0, fmt.Errorf("refresh session: %w", err)
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		return "", resp.StatusCode, fmt.Errorf("refresh session: status %d", resp.StatusCode)
	}
	token := resp.Header.Get("Gram-Session")
	if token == "" {
		return "", resp.StatusCode, fmt.Errorf("refresh session: missing Gram-Session header")
	}
	return token, resp.StatusCode, nil
}

// call performs an authenticated JSON API call and returns the raw response
// body. A 401 invalidates the cached access credential and retries once after
// refresh (or a fresh local login when the refresh session has expired).
// project sets the Gram-Project header when non-empty; when omitted
// the server falls back to the organization's only project.
func (c *apiClient) call(ctx context.Context, method, path string, query url.Values, project string, body any) (json.RawMessage, error) {
	if err := c.ensureSession(ctx); err != nil {
		return nil, err
	}

	raw, status, err := c.doOnce(ctx, method, path, query, project, body)
	if status == http.StatusUnauthorized {
		c.mu.Lock()
		c.sessionOK = false
		c.mu.Unlock()
		if err := c.ensureSession(ctx); err != nil {
			return nil, err
		}
		raw, status, err = c.doOnce(ctx, method, path, query, project, body)
	}
	if err != nil {
		return nil, err
	}
	if status < 200 || status > 299 {
		return nil, fmt.Errorf("%s %s: status %d: %s", method, path, status, string(raw))
	}
	return raw, nil
}

func (c *apiClient) doOnce(ctx context.Context, method, path string, query url.Values, project string, body any) (json.RawMessage, int, error) {
	u := c.base.JoinPath(path)
	if query != nil {
		u.RawQuery = query.Encode()
	}

	var reqBody io.Reader
	if body != nil {
		buf, err := json.Marshal(body)
		if err != nil {
			return nil, 0, fmt.Errorf("encode request body: %w", err)
		}
		reqBody = bytes.NewReader(buf)
	}

	req, err := http.NewRequestWithContext(ctx, method, u.String(), reqBody)
	if err != nil {
		return nil, 0, fmt.Errorf("build request: %w", err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if project != "" {
		req.Header.Set("Gram-Project", project)
	}

	c.mu.Lock()
	req.Header.Set("Gram-Session", c.accessToken)
	c.mu.Unlock()

	resp, err := c.hc.Do(req)
	if err != nil {
		return nil, 0, fmt.Errorf("%s %s: %w", method, path, err)
	}
	defer func() { _ = resp.Body.Close() }()

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, resp.StatusCode, fmt.Errorf("read response body: %w", err)
	}
	return raw, resp.StatusCode, nil
}

func sameOrigin(a, b *url.URL) bool {
	aOrigin, aErr := canonicalOrigin(a)
	bOrigin, bErr := canonicalOrigin(b)
	return aErr == nil && bErr == nil && aOrigin == bOrigin
}

// Go's cookie jar ignores ports. Give each canonical origin its own jar so
// login can visit the IDP without sharing any dashboard cookie state. Within
// an origin, the standard jar still enforces cookie path, domain and expiry.
// Like http.CookieJar, this wrapper is safe for concurrent use.
type authCookieJar struct {
	mu   sync.Mutex
	jars map[string]*cookiejar.Jar
}

func (j *authCookieJar) Cookies(u *url.URL) []*http.Cookie {
	jar := j.jarForOrigin(u)
	if jar == nil {
		return nil
	}
	return jar.Cookies(u)
}

func (j *authCookieJar) SetCookies(u *url.URL, cookies []*http.Cookie) {
	if jar := j.jarForOrigin(u); jar != nil {
		jar.SetCookies(u, cookies)
	}
}

func (j *authCookieJar) jarForOrigin(u *url.URL) *cookiejar.Jar {
	origin, err := canonicalOrigin(u)
	if err != nil {
		return nil
	}
	j.mu.Lock()
	defer j.mu.Unlock()
	if jar := j.jars[origin]; jar != nil {
		return jar
	}
	jar, err := cookiejar.New(nil)
	if err != nil {
		// cookiejar.New with nil options never errors.
		panic(err)
	}
	if j.jars == nil {
		j.jars = make(map[string]*cookiejar.Jar)
	}
	j.jars[origin] = jar
	return jar
}
