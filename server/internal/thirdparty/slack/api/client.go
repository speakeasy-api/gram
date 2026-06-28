// Package api provides the shared low-level Slack Web API client used across
// Gram. It owns the wire-level concerns of talking to slack.com/api:
// form-encoding request payloads, Bearer-token resolution from the tool-call
// environment, and parsing the standard Slack response envelope. Higher-level
// callers (the platform Slack tools and the third-party Slack integration)
// build their request/response models on top of this client so there is a
// single, consistently-behaving Slack transport.
package api

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/toolconfig"
)

const (
	// DefaultBaseURL is the Slack Web API root.
	DefaultBaseURL = "https://slack.com/api"

	//nolint:gosec // environment variable name, not a credential
	BotTokenEnvVar  = "SLACK_BOT_TOKEN"
	UserTokenEnvVar = "SLACK_USER_TOKEN"
	TokenEnvVar     = "SLACK_TOKEN"
)

// TokenKind selects which token Slack expects for a given method.
type TokenKind int

const (
	// TokenPreferBot resolves a bot token first, falling back to a user token.
	TokenPreferBot TokenKind = iota
	// TokenRequireUser resolves a user token only (e.g. search methods that
	// require a user scope).
	TokenRequireUser
)

// Client is the shared Slack Web API transport. Construct it with NewClient.
type Client struct {
	baseURL    string
	httpClient *guardian.HTTPClient
}

// ResponseEnvelope is the common envelope returned by every Slack Web API
// method. Callers embed it in their response structs to surface ok/error.
type ResponseEnvelope struct {
	Ok               bool   `json:"ok"`
	Error            string `json:"error,omitempty"`
	Warning          string `json:"warning,omitempty"`
	ResponseMetadata *struct {
		Messages []string `json:"messages,omitempty"`
	} `json:"response_metadata,omitempty"`
}

// NewClient builds a Slack API client. An empty baseURL falls back to
// DefaultBaseURL. The constructor is infallible; validate the HTTP client at
// the call site if a nil client is unacceptable.
func NewClient(baseURL string, httpClient *guardian.HTTPClient) *Client {
	if strings.TrimSpace(baseURL) == "" {
		baseURL = DefaultBaseURL
	}
	return &Client{
		baseURL:    strings.TrimRight(baseURL, "/"),
		httpClient: httpClient,
	}
}

// HTTPClient returns the underlying HTTP client, allowing callers to issue
// out-of-band requests (e.g. the binary upload step of the file upload flow)
// against the same guardian-policed transport.
func (c *Client) HTTPClient() *guardian.HTTPClient {
	return c.httpClient
}

// BaseURL returns the configured Slack API root.
func (c *Client) BaseURL() string {
	return c.baseURL
}

// Call invokes a Slack Web API method with a form-encoded payload, resolving
// the token from env according to kind, and returns the raw response body once
// the Slack envelope reports ok=true.
func (c *Client) Call(ctx context.Context, method string, payload map[string]any, kind TokenKind, env toolconfig.ToolCallEnv) ([]byte, error) {
	token, err := c.Token(kind, env)
	if err != nil {
		return nil, err
	}

	return c.CallWithToken(ctx, method, payload, token)
}

// CallWithToken invokes a Slack Web API method with a caller-supplied Bearer
// token. It applies the same form-encoding and envelope handling as Call, and
// is used by callers that resolve tokens out of band (e.g. decrypted
// per-installation bot tokens).
func (c *Client) CallWithToken(ctx context.Context, method string, payload map[string]any, token string) ([]byte, error) {
	if c.httpClient == nil {
		return nil, fmt.Errorf("slack HTTP client not configured")
	}

	form, err := encodeFormPayload(payload)
	if err != nil {
		return nil, fmt.Errorf("encode slack payload for %s: %w", method, err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/"+method, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, fmt.Errorf("build slack request for %s: %w", method, err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("call slack %s: %w", method, err)
	}
	defer o11y.NoLogDefer(func() error { return resp.Body.Close() })

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read slack response for %s: %w", method, err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("slack %s returned %d: %s", method, resp.StatusCode, string(bodyBytes))
	}

	var envelope ResponseEnvelope
	if err := json.Unmarshal(bodyBytes, &envelope); err != nil {
		return nil, fmt.Errorf("decode slack response for %s: %w", method, err)
	}
	if !envelope.Ok {
		return nil, fmt.Errorf("slack %s: %s", method, errorDetails(envelope))
	}

	return bodyBytes, nil
}

// Token resolves the Slack token to use for a request from the tool-call
// environment, following the platform-tool preference order: bot token first
// for general methods, user token only for user-scoped methods.
func (c *Client) Token(kind TokenKind, env toolconfig.ToolCallEnv) (string, error) {
	var candidates []string
	switch kind {
	case TokenRequireUser:
		candidates = []string{UserTokenEnvVar, TokenEnvVar}
	default:
		candidates = []string{BotTokenEnvVar, UserTokenEnvVar, TokenEnvVar}
	}
	merged := env.Merged()
	for _, key := range candidates {
		if value := strings.TrimSpace(merged.Get(key)); value != "" {
			return value, nil
		}
	}
	if kind == TokenRequireUser {
		return "", fmt.Errorf("slack user token not configured: expected %s or %s with search:read scope", UserTokenEnvVar, TokenEnvVar)
	}
	return "", fmt.Errorf("slack token not configured: expected %s, %s, or %s", BotTokenEnvVar, UserTokenEnvVar, TokenEnvVar)
}

func errorDetails(resp ResponseEnvelope) string {
	parts := make([]string, 0, 3)
	if resp.Error != "" {
		parts = append(parts, resp.Error)
	}
	if resp.Warning != "" {
		parts = append(parts, "warning="+resp.Warning)
	}
	if resp.ResponseMetadata != nil && len(resp.ResponseMetadata.Messages) > 0 {
		parts = append(parts, strings.Join(resp.ResponseMetadata.Messages, "; "))
	}
	if len(parts) == 0 {
		return "request failed"
	}
	return strings.Join(parts, " | ")
}

func encodeFormPayload(payload map[string]any) (url.Values, error) {
	form := url.Values{}
	for key, value := range payload {
		encoded, err := encodeFormValue(value)
		if err != nil {
			return nil, fmt.Errorf("encode %s: %w", key, err)
		}
		form.Set(key, encoded)
	}
	return form, nil
}

func encodeFormValue(value any) (string, error) {
	switch v := value.(type) {
	case string:
		return v, nil
	case bool:
		return strconv.FormatBool(v), nil
	case int:
		return strconv.Itoa(v), nil
	case int64:
		return strconv.FormatInt(v, 10), nil
	case float64:
		return strconv.FormatFloat(v, 'f', -1, 64), nil
	case []string:
		return strings.Join(v, ","), nil
	default:
		data, err := json.Marshal(v)
		if err != nil {
			return "", fmt.Errorf("marshal value: %w", err)
		}
		return string(data), nil
	}
}
