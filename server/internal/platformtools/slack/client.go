package slack

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/platformtools/core"
	slackapi "github.com/speakeasy-api/gram/server/internal/thirdparty/slack/api"
	"github.com/speakeasy-api/gram/server/internal/toolconfig"
)

// The platform Slack tools route every Slack call through the shared
// slackapi.Client so transport behavior (form-encoding, token resolution,
// envelope handling) lives in one place. The aliases below keep the tool-side
// surface stable while delegating to that shared client.
const (
	defaultSlackAPIBaseURL = slackapi.DefaultBaseURL
	slackBotTokenEnvVar    = slackapi.BotTokenEnvVar
	slackUserTokenEnvVar   = slackapi.UserTokenEnvVar
	slackTokenEnvVar       = slackapi.TokenEnvVar
	sourceSlack            = "slack"
)

type (
	apiClient             = slackapi.Client
	slackResponseEnvelope = slackapi.ResponseEnvelope
)

const (
	tokenPreferBot   = slackapi.TokenPreferBot
	tokenRequireUser = slackapi.TokenRequireUser
)

func newAPIClient(baseURL string, httpClient *guardian.HTTPClient) *apiClient {
	return slackapi.NewClient(baseURL, httpClient)
}

type slackTool struct {
	descriptor core.ToolDescriptor
	client     *apiClient
	callFn     func(context.Context, *apiClient, toolconfig.ToolCallEnv, io.Reader, io.Writer) error
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
