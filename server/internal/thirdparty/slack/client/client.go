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

type SlackThreadRepliesInput struct {
	ChannelID string
	ThreadTS  string

	// Oldest, when set, limits the result to messages after this timestamp.
	Oldest string

	// Latest, when set, limits the result to messages before this timestamp.
	Latest string

	// Limit caps the number of messages returned.
	Limit int
}

// SlackThreadMessage is the subset of a conversations.replies message that
// thread-context consumers read.
type SlackThreadMessage struct {
	Ts     string `json:"ts"`
	User   string `json:"user,omitempty"`
	BotID  string `json:"bot_id,omitempty"`
	Text   string `json:"text,omitempty"`
	Hidden bool   `json:"hidden,omitempty"`
}

type SlackThreadReplies struct {
	Messages []SlackThreadMessage

	// HasMore reports that messages matching the bounds were left out because
	// of Limit.
	HasMore bool
}

// ThreadReplies reads messages in a thread via conversations.replies. Slack
// returns the earliest Limit messages inside the bounds, oldest first, and
// excludes messages at the bound timestamps.
func (s *SlackClient) ThreadReplies(ctx context.Context, accessToken string, input SlackThreadRepliesInput) (SlackThreadReplies, error) {
	payload := map[string]any{
		"channel": input.ChannelID,
		"ts":      input.ThreadTS,
		"limit":   input.Limit,
	}
	if input.Oldest != "" {
		payload["oldest"] = input.Oldest
	}
	if input.Latest != "" {
		payload["latest"] = input.Latest
	}

	body, err := s.api.CallWithToken(ctx, "conversations.replies", payload, accessToken)
	if err != nil {
		return SlackThreadReplies{}, fmt.Errorf("read slack thread replies: %w", err)
	}

	var decoded struct {
		Messages []SlackThreadMessage `json:"messages"`
		HasMore  bool                 `json:"has_more"`
	}
	if err := json.Unmarshal(body, &decoded); err != nil {
		return SlackThreadReplies{}, fmt.Errorf("decode slack thread replies: %w", err)
	}
	return SlackThreadReplies{Messages: decoded.Messages, HasMore: decoded.HasMore}, nil
}

// SlackBotIdentity is the bot user and bot a bot token acts as.
type SlackBotIdentity struct {
	UserID string `json:"user_id"`
	BotID  string `json:"bot_id"`
}

// BotIdentity resolves the bot behind a bot token via auth.test.
func (s *SlackClient) BotIdentity(ctx context.Context, accessToken string) (SlackBotIdentity, error) {
	body, err := s.api.CallWithToken(ctx, "auth.test", map[string]any{}, accessToken)
	if err != nil {
		return SlackBotIdentity{}, fmt.Errorf("slack auth test: %w", err)
	}
	var identity SlackBotIdentity
	if err := json.Unmarshal(body, &identity); err != nil {
		return SlackBotIdentity{}, fmt.Errorf("decode slack auth test: %w", err)
	}
	return identity, nil
}
