package client

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/speakeasy-api/gram/server/internal/guardian"
	slackapi "github.com/speakeasy-api/gram/server/internal/thirdparty/slack/api"
)

type SlackClient struct {
	api *slackapi.Client
}

func NewSlackClient(guardianPolicy *guardian.Policy) *SlackClient {
	return &SlackClient{
		api: slackapi.NewClient(slackapi.DefaultBaseURL, guardianPolicy.PooledClient()),
	}
}

type SlackSetThreadStatusInput struct {
	ChannelID       string
	ThreadTS        string
	Status          string
	LoadingMessages []string
}

// SetThreadStatus shows Slack's native AI loading indicator on a thread via
// assistant.threads.setStatus. Slack rotates through LoadingMessages beneath the
// status and clears it automatically once the app posts a reply, or after a
// ~2-minute timeout. The method accepts the chat:write scope (Slack changelog
// 2026-03-05), so it works on plain channel/DM threads without the assistant
// split-view.
func (s *SlackClient) SetThreadStatus(ctx context.Context, accessToken string, input SlackSetThreadStatusInput) error {
	payload := map[string]any{
		"channel_id": input.ChannelID,
		"thread_ts":  input.ThreadTS,
		"status":     input.Status,
	}
	if len(input.LoadingMessages) > 0 {
		// Slack expects array params as JSON-encoded strings in a
		// form-encoded request; pre-marshal so the shared client doesn't
		// comma-join the slice.
		encoded, err := json.Marshal(input.LoadingMessages)
		if err != nil {
			return fmt.Errorf("encode loading_messages: %w", err)
		}
		payload["loading_messages"] = string(encoded)
	}

	if _, err := s.api.CallWithToken(ctx, "assistant.threads.setStatus", payload, accessToken); err != nil {
		return fmt.Errorf("set slack thread status: %w", err)
	}

	return nil
}
