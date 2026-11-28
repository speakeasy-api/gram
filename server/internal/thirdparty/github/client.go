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
	"path"
	"strings"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"go.opentelemetry.io/otel/trace"
	"golang.org/x/sync/singleflight"

	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/o11y"
)

// Client authenticates as a GitHub App and performs API operations
// on behalf of installations.
type Client struct {
	appID       int64
	privateKey  *rsa.PrivateKey
	tracer      trace.Tracer
	httpClient  *guardian.HTTPClient
	mu          sync.Mutex
	tokens      map[int64]cachedToken
	tokenFlight singleflight.Group
	repository  *repositoryScope
}

type repositoryScope struct {
	installationID int64
	path           string
	token          string
}

type cachedToken struct {
	token     string
	expiresAt time.Time
}

// NewClient creates a GitHub App client from the app ID and PEM-encoded private key.
func NewClient(appID int64, privateKeyPEM []byte, httpClient *guardian.HTTPClient, tracerProvider trace.TracerProvider) (*Client, error) {
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
		appID:       appID,
		privateKey:  key,
		httpClient:  httpClient,
		tracer:      tracerProvider.Tracer("github.com/speakeasy-api/gram/server/internal/thirdparty/github"),
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
	if scope := c.repository; scope != nil {
		if installationID != scope.installationID {
			return "", fmt.Errorf("installation token: client is scoped to a different installation")
		}
		return scope.token, nil
	}

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

		ct, err := c.requestInstallationToken(ctx, installationID, 0)
		if err != nil {
			return "", err
		}
		c.mu.Lock()
		c.tokens[installationID] = ct
		c.mu.Unlock()
		return ct.token, nil
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

// requestInstallationToken mints a token without accessing either client's cache.
// A positive repositoryID restricts it to that repository; zero is for the
// installation-wide token used by the unscoped client.
func (c *Client) requestInstallationToken(ctx context.Context, installationID, repositoryID int64) (cachedToken, error) {
	appJWT, err := c.appJWT()
	if err != nil {
		return cachedToken{}, fmt.Errorf("create app JWT: %w", err)
	}
	url := fmt.Sprintf("%s/app/installations/%d/access_tokens", apiBase, installationID)
	var body io.Reader
	if repositoryID > 0 {
		body = strings.NewReader(fmt.Sprintf(`{"repository_ids":[%d]}`, repositoryID))
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, body)
	if err != nil {
		return cachedToken{}, fmt.Errorf("create token request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+appJWT)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := withoutRedirects(c.httpClient).Do(req)
	if err != nil {
		return cachedToken{}, fmt.Errorf("request installation token: %w", err)
	}
	defer o11y.NoLogDefer(func() error { return resp.Body.Close() })

	if resp.StatusCode != http.StatusCreated {
		return cachedToken{}, fmt.Errorf("request installation token: status %d", resp.StatusCode)
	}

	var result struct {
		Token     string    `json:"token"`
		ExpiresAt time.Time `json:"expires_at"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return cachedToken{}, fmt.Errorf("decode installation token: %w", err)
	}
	if result.Token == "" {
		return cachedToken{}, fmt.Errorf("request installation token: empty token")
	}
	return cachedToken{
		token:     result.Token,
		expiresAt: result.ExpiresAt.Add(-5 * time.Minute),
	}, nil
}

func withoutRedirects(httpClient *http.Client) *http.Client {
	client := *httpClient
	client.CheckRedirect = func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}
	return &client
}

func isRedirect(status int) bool {
	switch status {
	case http.StatusMovedPermanently, http.StatusFound, http.StatusSeeOther,
		http.StatusTemporaryRedirect, http.StatusPermanentRedirect:
		return true
	default:
		return false
	}
}

func (c *Client) doAPI(ctx context.Context, installationID int64, method, url string, body io.Reader) (*http.Response, error) {
	return c.doAPIWithClient(ctx, c.httpClient, installationID, method, url, body)
}

func (c *Client) doAPIWithClient(ctx context.Context, httpClient *http.Client, installationID int64, method, url string, body io.Reader) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, method, url, body)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	if scope := c.repository; scope != nil {
		// Match a whole path segment and reject traversal in the decoded path.
		repoPath := strings.ToLower(req.URL.Path)
		if req.URL.Scheme != "https" || req.URL.Host != "api.github.com" || req.URL.User != nil ||
			repoPath != path.Clean(repoPath) ||
			(repoPath != scope.path && !strings.HasPrefix(repoPath, scope.path+"/")) {
			return nil, fmt.Errorf("execute API request: client is scoped to a different repository")
		}
	}
	token, err := c.installationToken(ctx, installationID)
	if err != nil {
		return nil, err
	}

	req.Header.Set("Authorization", "token "+token)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("execute API request: %w", err)
	}
	if c.repository != nil && isRedirect(resp.StatusCode) {
		defer o11y.NoLogDefer(func() error { return resp.Body.Close() })
		return nil, fmt.Errorf("execute API request: %w: unexpected repository redirect (status %d)", ErrRepositoryConflict, resp.StatusCode)
	}
	return resp, nil
}
