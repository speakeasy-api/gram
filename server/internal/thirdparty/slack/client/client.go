package client

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/speakeasy-api/gram/server/internal/guardian"
)

const slackServer = "https://slack.com/api"

type SlackClient struct {
	client *guardian.HTTPClient
}

func NewSlackClient(guardianPolicy *guardian.Policy) *SlackClient {
	return &SlackClient{
		client: guardianPolicy.PooledClient(),
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
		payload["loading_messages"] = input.LoadingMessages
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal setStatus body: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, slackServer+"/assistant.threads.setStatus", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("create setStatus request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json; charset=utf-8")
	req.Header.Set("Authorization", "Bearer "+accessToken)

	resp, err := s.client.Do(req)
	if err != nil {
		return fmt.Errorf("send setStatus request to Slack: %w", err)
	}
	defer func() {
		if cerr := resp.Body.Close(); cerr != nil {
			fmt.Printf("failed to close response body: %v\n", cerr)
		}
	}()

	if resp.StatusCode != http.StatusOK {
		respBody, err := io.ReadAll(resp.Body)
		if err != nil {
			return fmt.Errorf("slack setStatus non-200 response: %d, read body: %w", resp.StatusCode, err)
		}
		return fmt.Errorf("slack setStatus non-200 response: %d, body: %s", resp.StatusCode, string(respBody))
	}

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("read setStatus response body: %w", err)
	}

	var result struct {
		Ok    bool   `json:"ok"`
		Error string `json:"error"`
	}
	if err := json.Unmarshal(respBody, &result); err != nil {
		return fmt.Errorf("unmarshal setStatus response: %w", err)
	}
	if !result.Ok {
		return fmt.Errorf("slack setStatus failed: %s", result.Error)
	}

	return nil
}
