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

const sessionCookieName = "gram_session"

// apiClient is a session-authenticated client for the local server's
// management API. Sessions are minted lazily by walking the dashboard's OIDC
// login flow — the local dev-idp auto-approves, so the whole redirect chain
// completes without interaction — and the resulting cookie lives in the
// client's jar for the life of the process.
type apiClient struct {
	base   *url.URL
	hc     *http.Client
	logger *slog.Logger

	mu        sync.Mutex
	sessionOK bool
}

func newAPIClient(base *url.URL, insecure bool, logger *slog.Logger) *apiClient {
	jar, err := cookiejar.New(nil)
	if err != nil {
		// cookiejar.New with nil options never errors; keep the compiler honest.
		panic(err)
	}

	var transport http.RoundTripper
	if insecure {
		transport = &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, //nolint:gosec // opt-in flag for local self-signed certs
		}
	}

	return &apiClient{
		base:   base,
		logger: logger,
		hc: &http.Client{
			Jar:       jar,
			Transport: transport,
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				// The login chain ends at /rpc/auth.callback, which sets the
				// session cookie and then redirects to the dashboard. The
				// dashboard may not be running, so stop there.
				if len(via) > 0 && via[len(via)-1].URL.Path == "/rpc/auth.callback" {
					return http.ErrUseLastResponse
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
	for _, ck := range c.hc.Jar.Cookies(c.base) {
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

	resp, err := c.hc.Do(req)
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
	if err := c.login(ctx); err != nil {
		return err
	}
	c.sessionOK = true
	return nil
}

// call performs an authenticated JSON API call and returns the raw response
// body. A 401 invalidates the cached session and retries once with a fresh
// login. project sets the Gram-Project header when non-empty; when omitted
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
