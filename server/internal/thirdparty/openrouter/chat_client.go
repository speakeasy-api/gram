package openrouter

// import (
// 	"bytes"
// 	"context"
// 	"encoding/json"
// 	"fmt"
// 	"io"
// 	"log/slog"
// 	"net/http"
// 	"strings"

// 	"github.com/hashicorp/go-cleanhttp"

// 	or_base "github.com/OpenRouterTeam/go-sdk"
// 	or "github.com/OpenRouterTeam/go-sdk/models/components"
// 	or_operations "github.com/OpenRouterTeam/go-sdk/models/operations"
// 	"github.com/speakeasy-api/gram/server/internal/attr"
// 	"github.com/speakeasy-api/gram/server/internal/billing"
// )

// type ChatClient struct {
// 	openRouter Provisioner
// 	chatClient *http.Client
// 	logger     *slog.Logger
// }

// const (
// 	DefaultChatModel = "openai/gpt-4o"
// )

// func NewChatClient(logger *slog.Logger, openRouter Provisioner) *ChatClient {
// 	return &ChatClient{
// 		openRouter: openRouter,
// 		chatClient: cleanhttp.DefaultPooledClient(),
// 		logger:     logger,
// 	}
// }

// func (c *ChatClient) GetCompletion(ctx context.Context, orgID string, systemPrompt, prompt string, tools []Tool, usageSource billing.ModelUsageSource) (*or.Message, error) {
// 	var messages []or.Message

// 	// Optional system prompt
// 	if systemPrompt != "" {
// 		messages = append(messages, or.CreateMessageSystem(or.SystemMessage{
// 			Content: or.CreateSystemMessageContentStr(systemPrompt),
// 			Name:    nil,
// 		}))
// 	}

// 	// User message
// 	messages = append(messages, or.CreateMessageUser(or.UserMessage{
// 		Content: or.CreateUserMessageContentStr(prompt),
// 		Name:    nil,
// 	}))

// 	return c.GetCompletionFromMessages(ctx, orgID, "", messages, tools, nil, "", usageSource)
// }

// func (c *ChatClient) GetObjectCompletion(ctx context.Context, orgID string, projectID string, model string, systemPrompt string, prompt string, jsonSchema or.JSONSchemaConfig, usageSource billing.ModelUsageSource) (*or.Message, error) {
// 	var messages []or.Message

// 	// Optional system prompt
// 	if systemPrompt != "" {
// 		messages = append(messages, or.CreateMessageSystem(or.SystemMessage{
// 			Content: or.CreateSystemMessageContentStr(systemPrompt),
// 			Name:    nil,
// 		}))
// 	}

// 	// User message
// 	messages = append(messages, or.CreateMessageUser(or.UserMessage{
// 		Content: or.CreateUserMessageContentStr(prompt),
// 		Name:    nil,
// 	}))

// 	return c.getCompletionFromMessages(ctx, orgID, projectID, messages, nil, nil, model, &jsonSchema, usageSource)
// }

// func (c *ChatClient) GetCompletionFromMessages(ctx context.Context, orgID string, projectID string, messages []or.Message, tools []Tool, temperature *float64, model string, usageSource billing.ModelUsageSource) (*or.Message, error) {
// 	return c.getCompletionFromMessages(ctx, orgID, projectID, messages, tools, temperature, model, nil, usageSource)
// }

// func (c *ChatClient) getCompletionFromMessages(
// 	ctx context.Context,
// 	orgID string,
// 	projectID string,
// 	messages []or.Message,
// 	tools []Tool,
// 	temperature *float64,
// 	model string,
// 	jsonSchema *or.JSONSchemaConfig,
// 	usageSource billing.ModelUsageSource,
// ) (*or.Message, error) {
// 	openrouterKey, err := c.openRouter.ProvisionAPIKey(ctx, orgID)
// 	if err != nil {
// 		return nil, fmt.Errorf("provisioning OpenRouter key: %w", err)
// 	}

// 	// Default temperature to 1.0 if not provided
// 	temp := float32(1.0)
// 	if temperature != nil {
// 		temp = float32(*temperature)
// 	}

// 	// Default model if not provided
// 	modelToUse := model
// 	if modelToUse == "" {
// 		modelToUse = DefaultChatModel
// 	}

// 	reqBody := OpenAIChatRequest{
// 		Model:          modelToUse,
// 		Messages:       messages,
// 		Stream:         false,
// 		Tools:          tools,
// 		Temperature:    temp,
// 		ResponseFormat: nil,
// 	}

// 	if jsonSchema != nil {
// 		jsonSchemaConfig := or.ResponseFormatJSONSchema{
// 			JSONSchema: *jsonSchema,
// 		}
// 		responseFormat := or.CreateResponseFormatJSONSchema(jsonSchemaConfig)
// 		reqBody.ResponseFormat = &responseFormat
// 	}

// 	data, err := json.Marshal(reqBody)
// 	if err != nil {
// 		return nil, fmt.Errorf("marshal request error: %w", err)
// 	}

// 	req, err := http.NewRequestWithContext(ctx, "POST", fmt.Sprintf("%s/v1/chat/completions", OpenRouterBaseURL), bytes.NewReader(data))
// 	if err != nil {
// 		return nil, fmt.Errorf("create request error: %w", err)
// 	}
// 	req.Header.Set("Content-Type", "application/json")
// 	req.Header.Set("Authorization", "Bearer "+openrouterKey)

// 	resp, err := c.chatClient.Do(req)
// 	if err != nil {
// 		return nil, fmt.Errorf("chat request failed: %w", err)
// 	}
// 	defer func() {
// 		if err := resp.Body.Close(); err != nil {
// 			c.logger.ErrorContext(ctx, "failed to close response body", attr.SlogError(err))
// 		}
// 	}()

// 	body, err := io.ReadAll(resp.Body)
// 	if err != nil {
// 		return nil, fmt.Errorf("read response error: %w", err)
// 	}

// 	if resp.StatusCode != http.StatusOK {
// 		return nil, fmt.Errorf("OpenRouter API error: %s", strings.TrimSpace(string(body)))
// 	}

// 	var chatResp OpenAIChatResponse
// 	if err := json.Unmarshal(body, &chatResp); err != nil {
// 		return nil, fmt.Errorf("unmarshal response error: %w", err)
// 	}

// 	// Track model usage for billing
// 	go func() {
// 		err = c.openRouter.TriggerModelUsageTracking(context.WithoutCancel(ctx), chatResp.ID, orgID, projectID, usageSource, "")
// 		if err != nil {
// 			c.logger.ErrorContext(ctx, "failed to track model usage", attr.SlogError(err))
// 		}
// 	}()

// 	msg := chatResp.Choices[0].Message
// 	return &msg, nil
// }
