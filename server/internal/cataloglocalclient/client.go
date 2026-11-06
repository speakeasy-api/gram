// Package cataloglocalclient syncs a local catalog through authenticated Admin HTTP.
package cataloglocalclient

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strings"
	"time"

	"github.com/speakeasy-api/gram/server/internal/mcpregistry"
	"golang.org/x/net/html"
)

// DefaultEmail is an existing identity in mock-oidc/mock-oidc.example.yaml.
const DefaultEmail = "eng@speakeasyapi.dev"

// Sync uses only the configured loopback Admin server and mock OIDC provider.
// It never follows redirects automatically, persists credentials, or accesses storage.
func Sync(ctx context.Context, adminURL, providerURL, email string) (*mcpregistry.CatalogImportResult, error) {
	admin, err := localOrigin(adminURL)
	if err != nil {
		return nil, fmt.Errorf("local Admin URL: %w", err)
	}
	provider, err := localOrigin(providerURL)
	if err != nil {
		return nil, fmt.Errorf("local mock OIDC URL: %w", err)
	}
	if email == "" {
		email = DefaultEmail
	}
	// Separate jars prevent cookies crossing origins that share a host but not a port.
	ac, err := localClient()
	if err != nil {
		return nil, err
	}
	pc, err := localClient()
	if err != nil {
		return nil, err
	}
	defer ac.CloseIdleConnections()
	defer pc.CloseIdleConnections()
	resp, err := request(ctx, ac, http.MethodGet, admin.JoinPath("/admin/auth.login"), "", "")
	if err != nil {
		return nil, err
	}
	challenge, err := redirect(resp, provider, "/authorize")
	if err != nil {
		return nil, err
	}
	callback, err := url.Parse(challenge.Query().Get("redirect_uri"))
	if err != nil || !allowed(callback, admin, "/admin/auth.callback") || callback.RawQuery != "" {
		return nil, fmt.Errorf("mock OIDC callback must be the configured local Admin callback")
	}
	state := challenge.Query().Get("state")
	if state == "" {
		return nil, fmt.Errorf("admin login omitted OAuth state")
	}
	resp, err = request(ctx, pc, http.MethodGet, challenge, "", "")
	if err != nil {
		return nil, err
	}
	token, err := challengeToken(resp, email)
	if err != nil {
		return nil, err
	}
	form := url.Values{"user_email": {email}, "state_token": {token}}
	resp, err = request(ctx, pc, http.MethodPost, provider.JoinPath("/authorize"), form.Encode(), "application/x-www-form-urlencoded")
	if err != nil {
		return nil, err
	}
	callback, err = redirect(resp, admin, "/admin/auth.callback")
	if err != nil {
		return nil, err
	}
	if callback.Query().Get("state") != state || callback.Query().Get("code") == "" || callback.Query().Get("error") != "" {
		return nil, fmt.Errorf("mock OIDC callback returned invalid state or code")
	}
	resp, err = request(ctx, ac, http.MethodGet, callback, "", "")
	if err != nil {
		return nil, err
	}
	// Login's default return_to is /. Do not visit the dashboard or arbitrary targets.
	if _, err := redirect(resp, admin, "/"); err != nil {
		return nil, err
	}
	resp, err = request(ctx, ac, http.MethodPost, admin.JoinPath("/admin/catalog.sync"), "{}", "application/json")
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("catalog sync returned HTTP %d", resp.StatusCode)
	}
	var result mcpregistry.CatalogImportResult
	if err := json.NewDecoder(io.LimitReader(resp.Body, 4<<20)).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode catalog sync result: %w", err)
	}
	return &result, nil
}

func localOrigin(raw string) (*url.URL, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return nil, fmt.Errorf("invalid loopback origin")
	}
	if (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.User != nil || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || (u.Path != "" && u.Path != "/") || u.RawPath != "" {
		return nil, fmt.Errorf("expected a loopback HTTP origin without credentials, path, query, or fragment")
	}
	ip := net.ParseIP(u.Hostname())
	if u.Hostname() != "localhost" && (ip == nil || !ip.IsLoopback()) {
		return nil, fmt.Errorf("automatic login requires loopback endpoints")
	}
	u.Path = ""
	return u, nil
}

func allowed(u, origin *url.URL, path string) bool {
	return u != nil && u.Scheme == origin.Scheme && u.Host == origin.Host && u.User == nil && u.Path == path && u.RawPath == "" && u.Fragment == ""
}

//nolint:forbidigo // Local CLI intentionally contacts loopback; explicit origin checks and loopback-only dialer replace outbound SSRF policy.
func localClient() (*http.Client, error) {
	jar, err := cookiejar.New(nil)
	if err != nil {
		return nil, fmt.Errorf("create local session jar: %w", err)
	}
	base, ok := http.DefaultTransport.(*http.Transport)
	if !ok {
		return nil, fmt.Errorf("unexpected default HTTP transport")
	}
	transport := base.Clone()
	transport.Proxy = nil
	// Pin localhost to a loopback address instead of trusting DNS or proxy settings.
	transport.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(address)
		if err != nil {
			return nil, fmt.Errorf("parse local address: %w", err)
		}
		if host == "localhost" {
			host = "127.0.0.1"
		}
		ip := net.ParseIP(host)
		if ip == nil || !ip.IsLoopback() {
			return nil, fmt.Errorf("refusing nonloopback connection")
		}
		var d net.Dialer
		conn, err := d.DialContext(ctx, network, net.JoinHostPort(host, port))
		if err != nil {
			return nil, fmt.Errorf("connect local endpoint: %w", err)
		}
		return conn, nil
	}
	return &http.Client{ //nolint:forbidigo // Explicitly loopback-only local CLI transport; other client settings use defaults.
		Jar: jar, Transport: transport, Timeout: 2 * time.Minute,
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse },
	}, nil
}

//nolint:forbidigo // Client is constructed with the loopback-only transport and redirects disabled.
func request(ctx context.Context, client *http.Client, method string, target *url.URL, body, contentType string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, method, target.String(), strings.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("build local Admin request: %w", err)
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
		req.Header.Set("Origin", target.Scheme+"://"+target.Host)
	}
	resp, err := client.Do(req)
	if err != nil {
		// url.Error includes the callback query (authorization code); never print it.
		if ue, ok := errors.AsType[*url.Error](err); ok {
			err = ue.Err
		}
		return nil, fmt.Errorf("local authentication/sync request failed: %w", err)
	}
	return resp, nil
}

func redirect(resp *http.Response, origin *url.URL, path string) (*url.URL, error) {
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusFound && resp.StatusCode != http.StatusSeeOther && resp.StatusCode != http.StatusTemporaryRedirect {
		return nil, fmt.Errorf("local login expected redirect, received HTTP %d", resp.StatusCode)
	}
	target, err := resp.Location()
	if err != nil || !allowed(target, origin, path) {
		return nil, fmt.Errorf("local login refused redirect outside the expected origin and path")
	}
	return target, nil
}

func challengeToken(resp *http.Response, email string) (string, error) {
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("mock OIDC challenge returned HTTP %d", resp.StatusCode)
	}
	doc, err := html.Parse(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return "", fmt.Errorf("parse mock OIDC challenge: %w", err)
	}
	var tokens []string
	var visit func(*html.Node)
	visit = func(n *html.Node) {
		if n.Type == html.ElementNode && n.Data == "form" && attribute(n, "method") == "POST" && attribute(n, "action") == "/authorize" {
			values := make(url.Values)
			var inputs func(*html.Node)
			inputs = func(child *html.Node) {
				if child.Type == html.ElementNode && child.Data == "input" {
					values.Add(attribute(child, "name"), attribute(child, "value"))
				}
				for c := child.FirstChild; c != nil; c = c.NextSibling {
					inputs(c)
				}
			}
			inputs(n)
			if len(values["user_email"]) == 1 && values.Get("user_email") == email && len(values["state_token"]) == 1 && values.Get("state_token") != "" {
				tokens = append(tokens, values.Get("state_token"))
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			visit(c)
		}
	}
	visit(doc)
	if len(tokens) != 1 {
		return "", fmt.Errorf("mock OIDC challenge must offer exactly one matching configured user")
	}
	return tokens[0], nil
}

func attribute(n *html.Node, name string) string {
	for _, a := range n.Attr {
		if a.Key == name {
			return a.Val
		}
	}
	return ""
}
