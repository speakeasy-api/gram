package github

import (
	"context"
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/sync/singleflight"

	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/o11y"
)

// Client authenticates as a GitHub App and performs API operations
// on behalf of installations.
type Client struct {
	apiBase     string
	appID       int64
	privateKey  *rsa.PrivateKey
	httpClient  *guardian.HTTPClient
	mu          sync.Mutex
	tokens      map[int64]cachedToken
	tokenFlight singleflight.Group
}

type cachedToken struct {
	token     string
	expiresAt time.Time
}

// NewClient creates a GitHub App client from the app ID and PEM-encoded private key.
func NewClient(appID int64, privateKeyPEM []byte, httpClient *guardian.HTTPClient) (*Client, error) {
	block, _ := pem.Decode(privateKeyPEM)
	if block == nil {
		return nil, fmt.Errorf("decode PEM block: no valid PEM data found")
	}

	key, err := x509.ParsePKCS1PrivateKey(block.Bytes)
	if err != nil {
		k, err2 := x509.ParsePKCS8PrivateKey(block.Bytes)
		if err2 != nil {
			return nil, fmt.Errorf("parse private key (tried PKCS1 and PKCS8): %w", err2)
		}
		var ok bool
		key, ok = k.(*rsa.PrivateKey)
		if !ok {
			return nil, fmt.Errorf("parse private key: not an RSA key")
		}
	}

	return &Client{
		apiBase:     "https://api.github.com",
		appID:       appID,
		privateKey:  key,
		httpClient:  httpClient,
		mu:          sync.Mutex{},
		tokens:      make(map[int64]cachedToken),
		tokenFlight: singleflight.Group{},
	}, nil
}

func (c *Client) appJWT() (string, error) {
	now := time.Now()
	claims := jwt.RegisteredClaims{
		Issuer:    fmt.Sprintf("%d", c.appID),
		Subject:   "",
		Audience:  nil,
		ExpiresAt: jwt.NewNumericDate(now.Add(10 * time.Minute)),
		NotBefore: nil,
		IssuedAt:  jwt.NewNumericDate(now.Add(-60 * time.Second)),
		ID:        "",
	}
	token := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	signed, err := token.SignedString(c.privateKey)
	if err != nil {
		return "", fmt.Errorf("sign app JWT: %w", err)
	}
	return signed, nil
}

// InstallationToken returns a short-lived GitHub App installation access token
// for the given installation. Cached and refreshed automatically — each call is
// cheap. Suitable for use as the password slot of a basic-auth credential when
// fetching from GitHub over HTTPS (with username "x-access-token").
func (c *Client) InstallationToken(ctx context.Context, installationID int64) (string, error) {
	return c.installationToken(ctx, installationID)
}

func (c *Client) installationToken(ctx context.Context, installationID int64) (string, error) {
	// Fast path: return cached token if still valid.
	c.mu.Lock()
	if ct, ok := c.tokens[installationID]; ok && time.Now().Before(ct.expiresAt) {
		c.mu.Unlock()
		return ct.token, nil
	}
	c.mu.Unlock()

	// Deduplicate concurrent token refreshes for the same installation.
	key := fmt.Sprintf("%d", installationID)
	val, err, _ := c.tokenFlight.Do(key, func() (any, error) {
		// Re-check cache inside singleflight — another goroutine may have just refreshed.
		c.mu.Lock()
		if ct, ok := c.tokens[installationID]; ok && time.Now().Before(ct.expiresAt) {
			c.mu.Unlock()
			return ct.token, nil
		}
		c.mu.Unlock()

		return c.mintInstallationToken(ctx, installationID)
	})
	if err != nil {
		return "", fmt.Errorf("installation token: %w", err)
	}
	token, ok := val.(string)
	if !ok {
		return "", fmt.Errorf("installation token: unexpected type %T", val)
	}
	return token, nil
}

// APIBase validates a GitHub instance origin and derives its REST API base.
func APIBase(instanceURL string) (string, error) {
	if instanceURL == "" {
		instanceURL = "https://github.com"
	}
	u, err := url.Parse(instanceURL)
	if err != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || (u.Path != "" && u.Path != "/") || u.Opaque != "" {
		return "", fmt.Errorf("GitHub instance must be an HTTPS origin without credentials, path, query, or fragment")
	}
	u.Host = strings.TrimSuffix(strings.ToLower(u.Host), ":443")
	u.Path = ""
	if u.Host == "github.com" {
		return "https://api.github.com", nil
	}
	return u.String() + "/api/v3", nil
}

// NewInstanceClient uses a customer-owned app on GitHub.com or Enterprise Server.
// The supplied transport must enforce outbound network and redirect policy.
func NewInstanceClient(appID int64, privateKeyPEM []byte, instanceURL string, httpClient *guardian.HTTPClient) (*Client, error) {
	if appID <= 0 {
		return nil, fmt.Errorf("app ID must be positive")
	}
	base, err := APIBase(instanceURL)
	if err != nil {
		return nil, err
	}
	c, err := NewClient(appID, privateKeyPEM, httpClient)
	if err != nil {
		return nil, err
	}
	if c.privateKey.N.BitLen() < 2048 {
		return nil, fmt.Errorf("GitHub App key must be at least 2048 bits")
	}
	c.apiBase = base
	return c, nil
}

func (c *Client) mintInstallationToken(ctx context.Context, installationID int64) (string, error) {
	token, expiresAt, err := c.MintInstallationToken(ctx, installationID)
	if err != nil {
		return "", err
	}
	c.mu.Lock()
	c.tokens[installationID] = cachedToken{token: token, expiresAt: expiresAt.Add(-5 * time.Minute)}
	c.mu.Unlock()
	return token, nil
}

// MintInstallationToken performs one exchange. The caller owns caching and
// invalidation; the returned expiry is the upstream expiry, not a cache TTL.
func (c *Client) MintInstallationToken(ctx context.Context, installationID int64) (string, time.Time, error) {
	if installationID <= 0 {
		return "", time.Time{}, fmt.Errorf("installation ID must be positive")
	}
	appJWT, err := c.appJWT()
	if err != nil {
		return "", time.Time{}, fmt.Errorf("create app JWT: %w", err)
	}
	endpoint := fmt.Sprintf("%s/app/installations/%d/access_tokens", c.apiBase, installationID)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, nil)
	if err != nil {
		return "", time.Time{}, fmt.Errorf("create token request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+appJWT)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", time.Time{}, fmt.Errorf("request installation token: %w", err)
	}
	defer o11y.NoLogDefer(func() error { return resp.Body.Close() })
	if resp.StatusCode != http.StatusCreated {
		// The upstream body is untrusted and may echo credentials.
		return "", time.Time{}, fmt.Errorf("GitHub installation token request returned status %d", resp.StatusCode)
	}
	var result struct {
		Token     string    `json:"token"`
		ExpiresAt time.Time `json:"expires_at"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&result); err != nil {
		return "", time.Time{}, fmt.Errorf("decode installation token response")
	}
	if result.Token == "" || !result.ExpiresAt.After(time.Now().Add(time.Minute)) {
		return "", time.Time{}, fmt.Errorf("GitHub returned an empty or expired installation token")
	}
	return result.Token, result.ExpiresAt, nil
}

func (c *Client) doAPI(ctx context.Context, installationID int64, method, url string, body io.Reader) (*http.Response, error) {
	token, err := c.installationToken(ctx, installationID)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, method, url, body)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Authorization", "token "+token)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("execute API request: %w", err)
	}
	return resp, nil
}
