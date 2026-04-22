package slack

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/platformtools/core"
	"github.com/speakeasy-api/gram/server/internal/toolconfig"
)

const (
	defaultSlackAPIBaseURL = "https://slack.com/api"
	//nolint:gosec // environment variable name, not a credential
	slackBotTokenEnvVar  = "SLACK_BOT_TOKEN"
	slackUserTokenEnvVar = "SLACK_USER_TOKEN"
	slackTokenEnvVar     = "SLACK_TOKEN"
	sourceSlack          = "slack"
)

type slackTokenKind int

const (
	tokenPreferBot slackTokenKind = iota
	tokenRequireUser
)

type apiClient struct {
	baseURL    string
	httpClient *guardian.HTTPClient
}

type slackTool struct {
	descriptor core.ToolDescriptor
	client     *apiClient
	callFn     func(context.Context, *apiClient, toolconfig.ToolCallEnv, io.Reader, io.Writer) error
}

type slackResponseEnvelope struct {
	Ok               bool   `json:"ok"`
	Error            string `json:"error,omitempty"`
	Warning          string `json:"warning,omitempty"`
	ResponseMetadata *struct {
		Messages []string `json:"messages,omitempty"`
	} `json:"response_metadata,omitempty"`
}

func (t *slackTool) Descriptor() core.ToolDescriptor {
	return t.descriptor
}

func (t *slackTool) Call(ctx context.Context, env toolconfig.ToolCallEnv, payload io.Reader, wr io.Writer) error {
	if t.client == nil {
		return fmt.Errorf("slack client not configured")
	}
	return t.callFn(ctx, t.client, env, payload, wr)
}

func newAPIClient(baseURL string, httpClient *guardian.HTTPClient) *apiClient {
	if strings.TrimSpace(baseURL) == "" {
		baseURL = defaultSlackAPIBaseURL
	}
	return &apiClient{
		baseURL:    strings.TrimRight(baseURL, "/"),
		httpClient: httpClient,
	}
}

func (c *apiClient) call(ctx context.Context, method string, payload map[string]any, kind slackTokenKind, env toolconfig.ToolCallEnv) ([]byte, error) {
	token, err := c.token(kind, env)
	if err != nil {
		return nil, err
	}
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

	var envelope slackResponseEnvelope
	if err := json.Unmarshal(bodyBytes, &envelope); err != nil {
		return nil, fmt.Errorf("decode slack response for %s: %w", method, err)
	}
	if !envelope.Ok {
		return nil, fmt.Errorf("slack %s: %s", method, slackErrorDetails(envelope))
	}

	return bodyBytes, nil
}

func (c *apiClient) token(kind slackTokenKind, env toolconfig.ToolCallEnv) (string, error) {
	var candidates []string
	switch kind {
	case tokenRequireUser:
		candidates = []string{slackUserTokenEnvVar, slackTokenEnvVar}
	default:
		candidates = []string{slackBotTokenEnvVar, slackUserTokenEnvVar, slackTokenEnvVar}
	}
	merged := env.Merged()
	for _, key := range candidates {
		if value := strings.TrimSpace(merged.Get(key)); value != "" {
			return value, nil
		}
	}
	if kind == tokenRequireUser {
		return "", fmt.Errorf("slack user token not configured: expected %s or %s with search:read scope", slackUserTokenEnvVar, slackTokenEnvVar)
	}
	return "", fmt.Errorf("slack token not configured: expected %s, %s, or %s", slackBotTokenEnvVar, slackUserTokenEnvVar, slackTokenEnvVar)
}

func slackErrorDetails(resp slackResponseEnvelope) string {
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

func decodePayload(payload io.Reader, target any) error {
	bodyBytes, err := io.ReadAll(payload)
	if err != nil {
		return fmt.Errorf("read request body: %w", err)
	}
	if len(bodyBytes) == 0 {
		return nil
	}
	if err := json.Unmarshal(bodyBytes, target); err != nil {
		return fmt.Errorf("decode request body: %w", err)
	}
	return nil
}

func writeResponse(wr io.Writer, body []byte) error {
	if _, err := wr.Write(body); err != nil {
		return fmt.Errorf("write response body: %w", err)
	}
	return nil
}

func requireString(name string, value string) (string, error) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return "", fmt.Errorf("%s is required", name)
	}
	return trimmed, nil
}

func filterListResponse(body []byte, field string, predicate func(map[string]any) bool) ([]byte, error) {
	if predicate == nil {
		return body, nil
	}
	var response map[string]any
	if err := json.Unmarshal(body, &response); err != nil {
		return nil, fmt.Errorf("decode slack list response: %w", err)
	}
	raw, ok := response[field]
	if !ok {
		return body, nil
	}
	items, ok := raw.([]any)
	if !ok {
		return body, nil
	}
	filtered := make([]any, 0, len(items))
	for _, item := range items {
		entry, ok := item.(map[string]any)
		if !ok {
			continue
		}
		if predicate(entry) {
			filtered = append(filtered, entry)
		}
	}
	response[field] = filtered
	out, err := json.Marshal(response)
	if err != nil {
		return nil, fmt.Errorf("encode filtered slack list response: %w", err)
	}
	return out, nil
}

func stringFieldContains(entry map[string]any, needle string, keys ...string) bool {
	for _, key := range keys {
		if value, ok := entry[key].(string); ok && value != "" {
			if strings.Contains(strings.ToLower(value), needle) {
				return true
			}
		}
	}
	return false
}

func derefString(value *string) string {
	if value == nil {
		return ""
	}
	return *value
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

func setOptionalString(target map[string]any, key string, value *string) {
	if value != nil && strings.TrimSpace(*value) != "" {
		target[key] = strings.TrimSpace(*value)
	}
}

func setOptionalBool(target map[string]any, key string, value *bool) {
	if value != nil {
		target[key] = *value
	}
}

func setOptionalInt(target map[string]any, key string, value *int) {
	if value != nil {
		target[key] = *value
	}
}

func slackToolAnnotations(readOnly, destructive, idempotent, openWorld bool) *types.ToolAnnotations {
	return &types.ToolAnnotations{
		Title:           nil,
		ReadOnlyHint:    &readOnly,
		DestructiveHint: &destructive,
		IdempotentHint:  &idempotent,
		OpenWorldHint:   &openWorld,
	}
}
