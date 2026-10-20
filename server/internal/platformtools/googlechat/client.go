// Package googlechat implements customer-managed Google Chat app tools.
package googlechat

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/jwt"

	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/toolconfig"
)

const (
	ServiceAccountEnvVar = "GOOGLE_CHAT_SERVICE_ACCOUNT_JSON"
	botScope             = "https://www.googleapis.com/auth/chat.bot"
	historyScope         = "https://www.googleapis.com/auth/chat.app.messages.readonly"
	tokenURL             = "https://oauth2.googleapis.com/token" // #nosec G101 -- Public fixed OAuth endpoint, not a credential.
	apiURL               = "https://chat.googleapis.com/v1/"
	maxResponseBytes     = 4 << 20
)

type client struct{ http *guardian.HTTPClient }

func newClient(httpClient *guardian.HTTPClient) *client {
	if httpClient == nil {
		return &client{http: nil}
	}
	// Never forward signed assertions or bearer tokens through redirects, even
	// when the caller's shared client permits them.
	safe := *httpClient
	safe.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	if safe.Timeout == 0 {
		safe.Timeout = 30 * time.Second
	}
	return &client{http: &safe}
}

func (c *client) call(ctx context.Context, env toolconfig.ToolCallEnv, scope, method, resource string, query url.Values, body any) ([]byte, error) {
	if c.http == nil {
		return nil, errors.New("google chat HTTP client not configured")
	}
	raw := env.Merged().Get(ServiceAccountEnvVar)
	if raw == "" {
		return nil, fmt.Errorf("google chat requires %s", ServiceAccountEnvVar)
	}
	var credentials struct {
		Type       string `json:"type"`
		Email      string `json:"client_email"`
		PrivateKey string `json:"private_key"`
	}
	if len(raw) > 64<<10 || json.Unmarshal([]byte(raw), &credentials) != nil || credentials.Type != "service_account" || !strings.HasSuffix(credentials.Email, ".iam.gserviceaccount.com") || credentials.PrivateKey == "" {
		return nil, errors.New("invalid Google Chat service account JSON")
	}
	// Construct the configuration ourselves: token_uri and other credential-file
	// endpoints must never control where private signed assertions are sent.
	config := jwt.Config{Email: credentials.Email, PrivateKey: []byte(credentials.PrivateKey), Scopes: []string{scope}, TokenURL: tokenURL, PrivateKeyID: "", Subject: "", Expires: 0, Audience: "", PrivateClaims: nil, UseIDToken: false}
	ctx = context.WithValue(ctx, oauth2.HTTPClient, c.http)
	token, err := config.TokenSource(ctx).Token()
	if err != nil {
		return nil, errors.New("google chat authentication failed; verify the service account and required app scope approval")
	}
	var payload []byte
	if body != nil {
		payload, err = json.Marshal(body)
		if err != nil {
			return nil, errors.New("cannot encode Google Chat request")
		}
	}
	endpoint := apiURL + resource
	if len(query) > 0 {
		endpoint += "?" + query.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, method, endpoint, bytes.NewReader(payload))
	if err != nil {
		return nil, errors.New("cannot build Google Chat request")
	}
	req.Header.Set("Authorization", "Bearer "+token.AccessToken)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, errors.New("google chat request failed")
	}
	defer o11y.NoLogDefer(func() error { return resp.Body.Close() })
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		// Google's error details and OAuth responses can echo input. Never surface
		// upstream payloads, transport errors, assertions, or credential material.
		return nil, fmt.Errorf("google chat API returned HTTP %d; verify app membership, scopes, and message ownership", resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes+1))
	if err != nil || len(data) > maxResponseBytes {
		return nil, errors.New("cannot read Google Chat response within size limit")
	}
	if len(data) == 0 {
		return []byte("{}"), nil
	}
	if !json.Valid(data) {
		return nil, errors.New("invalid Google Chat response")
	}
	return data, nil
}
