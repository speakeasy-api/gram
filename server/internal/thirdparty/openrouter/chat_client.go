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

	or_base "github.com/OpenRouterTeam/go-sdk"
	or "github.com/OpenRouterTeam/go-sdk/models/components"
	or_operations "github.com/OpenRouterTeam/go-sdk/models/operations"
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

	// Truncate inputs that exceed token limits
	// Embedding models have 8192 token limit, using ~4 chars/token as conservative estimate
	const maxChars = 30_000
	truncatedInputs := make([]string, len(inputs))
	for i, input := range inputs {
		if len(input) > maxChars {
			c.logger.WarnContext(ctx, fmt.Sprintf("truncating input for embedding, orgID: %s, model: %s, input length: %d", orgID, model, len(input)))
			truncatedInputs[i] = input[:maxChars]
		} else {
			truncatedInputs[i] = input
		}
	}
	inputs = truncatedInputs

	orClient := or_base.New(or_base.WithSecurity(openrouterKey))
	result, err := orClient.Embeddings.Generate(ctx, or_operations.CreateEmbeddingsRequest{
		Model:          model,
		Input:          or_operations.CreateInputUnionArrayOfStr(inputs),
		EncodingFormat: nil,
		Dimensions:     nil,
		User:           nil,
		Provider:       nil,
		InputType:      nil,
	})
	if err != nil {
		return nil, fmt.Errorf("create embeddings error: %w", err)
	}

	// The new SDK returns errors via err, not via HTTPMeta
	// Check if we got a response body
	if result == nil || result.CreateEmbeddingsResponseBody == nil {
		return nil, fmt.Errorf("embedding response body missing")
	}

	embeddingsData := result.CreateEmbeddingsResponseBody.Data

	if len(embeddingsData) == 0 {
		return nil, fmt.Errorf("embedding data missing in response")
	}

	results := make([][]float32, len(inputs))
	for _, data := range embeddingsData {
		if data.Index == nil {
			return nil, fmt.Errorf("embedding data missing index")
		}
		index := int(*data.Index)
		if index < 0 || index >= len(results) {
			return nil, fmt.Errorf("embedding index out of range: %d", index)
		}

		embedding := data.GetEmbedding()
		if embedding.ArrayOfNumber == nil {
			return nil, fmt.Errorf("embedding vector missing for index %d", index)
		}

		vector := make([]float32, len(embedding.ArrayOfNumber))
		for i, v := range embedding.ArrayOfNumber {
			vector[i] = float32(v)
		}
		results[index] = vector
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
