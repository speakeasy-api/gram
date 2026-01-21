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

	or "github.com/speakeasy-api/gram/openrouter/models/components"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/billing"
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

func (c *ChatClient) CreateEmbeddings(ctx context.Context, orgID string, model string, inputs []string) ([][]float32, error) {
	openrouterKey, err := c.openRouter.ProvisionAPIKey(ctx, orgID)
	if err != nil {
		return nil, fmt.Errorf("provisioning OpenRouter key: %w", err)
	}

	if model == "" {
		return nil, fmt.Errorf("model is required")
	}

	if len(inputs) == 0 {
		return nil, fmt.Errorf("at least one input is required")
	}

	reqBody := AIEmbeddingRequest{Model: model, Input: inputs}

	data, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("marshal request error: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", fmt.Sprintf("%s/v1/embeddings", OpenRouterBaseURL), bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("create request error: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+openrouterKey)

	resp, err := c.chatClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("embedding request failed: %w", err)
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

	var embeddingResp AIEmbeddingResponse
	if err := json.Unmarshal(body, &embeddingResp); err != nil {
		return nil, fmt.Errorf("unmarshal response error: %w", err)
	}

	if len(embeddingResp.Data) == 0 {
		return nil, fmt.Errorf("embedding data missing in response")
	}

	results := make([][]float32, len(inputs))
	for _, data := range embeddingResp.Data {
		if data.Index < 0 || data.Index >= len(results) {
			return nil, fmt.Errorf("embedding index out of range: %d", data.Index)
		}

		vector := make([]float32, len(data.Embedding))
		for i, v := range data.Embedding {
			vector[i] = float32(v)
		}
		results[data.Index] = vector
	}

	for i, vector := range results {
		if vector == nil {
			return nil, fmt.Errorf("missing embedding for input index %d", i)
		}
	}

	return results, nil
}

func (c *ChatClient) GetCompletion(ctx context.Context, orgID string, systemPrompt, prompt string, tools []Tool) (*or.Message, error) {
	var messages []or.Message

	// Optional system prompt
	if systemPrompt != "" {
		messages = append(messages, or.CreateMessageSystem(or.SystemMessage{
			Content: or.CreateSystemMessageContentStr(systemPrompt),
			Name:    nil,
		}))
	}

	// User message
	messages = append(messages, or.CreateMessageUser(or.UserMessage{
		Content: or.CreateUserMessageContentStr(prompt),
		Name:    nil,
	}))

	return c.GetCompletionFromMessages(ctx, orgID, "", messages, tools, nil, "")
}

func (c *ChatClient) GetCompletionFromMessages(ctx context.Context, orgID string, projectID string, messages []or.Message, tools []Tool, temperature *float64, model string) (*or.Message, error) {
	openrouterKey, err := c.openRouter.ProvisionAPIKey(ctx, orgID)
	if err != nil {
		return nil, fmt.Errorf("provisioning OpenRouter key: %w", err)
	}

	// Default temperature to 1.0 if not provided
	temp := float32(1.0)
	if temperature != nil {
		temp = float32(*temperature)
	}

	// Default model if not provided
	modelToUse := model
	if modelToUse == "" {
		modelToUse = DefaultChatModel
	}

	reqBody := OpenAIChatRequest{
		Model:       modelToUse,
		Messages:    messages,
		Stream:      false,
		Tools:       tools,
		Temperature: temp,
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

	// Track model usage for billing
	go func() {
		err = c.openRouter.TriggerModelUsageTracking(context.WithoutCancel(ctx), chatResp.ID, orgID, projectID, billing.ModelUsageSourceAgents, "")
		if err != nil {
			c.logger.ErrorContext(ctx, "failed to track model usage", attr.SlogError(err))
		}
	}()

	msg := chatResp.Choices[0].Message
	return &msg, nil
}
