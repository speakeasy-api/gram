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

// NewSlackClientWithBaseURL builds a client against a custom Slack API root
// with a caller-supplied HTTP client. Used by tests to point the client at a
// fake Slack server.
func NewSlackClientWithBaseURL(baseURL string, httpClient *guardian.HTTPClient) *SlackClient {
	return &SlackClient{
		api: slackapi.NewClient(baseURL, httpClient),
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

type SlackUnfurlInput struct {
	ChannelID string
	MessageTS string
	// UnfurlID and Source, when both set, address the link via Slack's unfurl
	// handle (required for composer-sourced link_shared events) instead of
	// channel + message ts.
	UnfurlID string
	Source   string
	// Unfurls maps each shared URL to its unfurl payload (e.g. a "blocks"
	// object), as accepted by chat.unfurl.
	Unfurls map[string]any
}

// Unfurl attaches app-provided previews to links in a message via
// chat.unfurl. Requires a bot token with the links:write scope and only works
// for domains registered as unfurl domains in the Slack app manifest.
func (s *SlackClient) Unfurl(ctx context.Context, accessToken string, input SlackUnfurlInput) error {
	encoded, err := json.Marshal(input.Unfurls)
	if err != nil {
		return fmt.Errorf("encode unfurls: %w", err)
	}

	payload := map[string]any{
		"unfurls": string(encoded),
	}
	if input.UnfurlID != "" && input.Source != "" {
		payload["unfurl_id"] = input.UnfurlID
		payload["source"] = input.Source
	} else {
		payload["channel"] = input.ChannelID
		payload["ts"] = input.MessageTS
	}

	if _, err := s.api.CallWithToken(ctx, "chat.unfurl", payload, accessToken); err != nil {
		return fmt.Errorf("unfurl slack links: %w", err)
	}

	return nil
}
