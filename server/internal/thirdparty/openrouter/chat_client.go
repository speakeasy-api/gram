package openrouter

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"

	"github.com/hashicorp/go-cleanhttp"

	"github.com/speakeasy-api/gram/server/internal/attr"
)

type ChatClient struct {
	openRouter Provisioner
	chatClient *http.Client
	logger     *slog.Logger
}

const (
	DefaultChatModel = "openai/gpt-4o"
)

func NewChatClient(logger *slog.Logger, openRouter Provisioner) *ChatClient {
	return &ChatClient{
		openRouter: openRouter,
		chatClient: cleanhttp.DefaultPooledClient(),
		logger:     logger,
	}
}

func (c *ChatClient) GetCompletion(ctx context.Context, orgID string, systemPrompt, prompt string, tools []Tool) (*OpenAIChatMessage, error) {
	var messages []OpenAIChatMessage

	// Optional system prompt
	if systemPrompt != "" {
		messages = append(messages, OpenAIChatMessage{
			Role:       "system",
			Content:    systemPrompt,
			ToolCalls:  nil,
			ToolCallID: "",
			Name:       "",
		})
	}

	// User message
	messages = append(messages, OpenAIChatMessage{
		Role:       "user",
		Content:    prompt,
		ToolCalls:  nil,
		ToolCallID: "",
		Name:       "",
	})

	return c.GetCompletionFromMessages(ctx, orgID, messages, tools)
}

func (c *ChatClient) GetCompletionFromMessages(ctx context.Context, orgID string, messages []OpenAIChatMessage, tools []Tool) (*OpenAIChatMessage, error) {
	openrouterKey, err := c.openRouter.ProvisionAPIKey(ctx, orgID)
	if err != nil {
		return nil, fmt.Errorf("provisioning OpenRouter key: %w", err)
	}

	reqBody := OpenAIChatRequest{
		Model:       DefaultChatModel,
		Messages:    messages,
		Stream:      false,
		Tools:       tools,
		Temperature: 0.5,
	}

	data, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("marshal request error: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", fmt.Sprintf("%s/v1/chat/completions", OpenRouterBaseURL), bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("create request error: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+openrouterKey)

	resp, err := c.chatClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("chat request failed: %w", err)
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			c.logger.ErrorContext(ctx, "failed to close response body", attr.SlogError(err))
		}
	}()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response error: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("OpenRouter API error: %s", strings.TrimSpace(string(body)))
	}

	var chatResp OpenAIChatResponse
	if err := json.Unmarshal(body, &chatResp); err != nil {
		return nil, fmt.Errorf("unmarshal response error: %w", err)
	}

	msg := chatResp.Choices[0].Message
	return &msg, nil
}
