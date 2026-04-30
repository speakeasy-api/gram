package openrouter

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	"go.opentelemetry.io/otel/trace"

	or_base "github.com/OpenRouterTeam/go-sdk"
	or "github.com/OpenRouterTeam/go-sdk/models/components"
	or_operations "github.com/OpenRouterTeam/go-sdk/models/operations"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/telemetry"
)

// ChatTitleGenerator schedules async chat title generation.
type ChatTitleGenerator interface {
	ScheduleChatTitleGeneration(ctx context.Context, chatID, orgID, projectID string) error
}

// ChatResolutionAnalyzer schedules async chat resolution analysis.
type ChatResolutionAnalyzer interface {
	ScheduleChatResolutionAnalysis(ctx context.Context, chatID, projectID uuid.UUID, orgID, apiKeyID string) error
}

// TelemetryLogger emits telemetry events for observability.
type TelemetryLogger interface {
	Log(ctx context.Context, params telemetry.LogParams)
}

const (
	DefaultChatModel = "openai/gpt-5.4"
)

// ChatClient is the single HTTP client for all OpenRouter communication.
// It applies pluggable strategies for message capture and usage tracking.
type ChatClient struct {
	logger                 *slog.Logger
	httpClient             *guardian.HTTPClient
	provisioner            Provisioner
	messageCaptureStrategy MessageCaptureStrategy
	usageTrackingStrategy  UsageTrackingStrategy
	chatTitleGenerator     ChatTitleGenerator
	chatResolutionAnalyzer ChatResolutionAnalyzer
	telemetryLogger        TelemetryLogger
}

// NewUnifiedClient creates a new UnifiedClient with the given strategies.
func NewUnifiedClient(
	logger *slog.Logger,
	guardianPolicy *guardian.Policy,
	provisioner Provisioner,
	captureStrategy MessageCaptureStrategy,
	trackingStrategy UsageTrackingStrategy,
	chatTitleGenerator ChatTitleGenerator,
	chatResolutionAnalyzer ChatResolutionAnalyzer,
	telemetryLogger TelemetryLogger,
) *ChatClient {
	return &ChatClient{
		logger:                 logger.With(attr.SlogComponent("openrouter_completions")),
		httpClient:             guardianPolicy.PooledClient(),
		provisioner:            provisioner,
		messageCaptureStrategy: captureStrategy,
		usageTrackingStrategy:  trackingStrategy,
		chatTitleGenerator:     chatTitleGenerator,
		chatResolutionAnalyzer: chatResolutionAnalyzer,
		telemetryLogger:        telemetryLogger,
	}
}

type initializeRequestResult struct {
	apiKey         string
	requestBody    OpenAIChatRequest
	captureSession CaptureSession
}

// initializeRequest creates the OpenAI-compatible request body with defaults applied.
func (c *ChatClient) initializeRequest(ctx context.Context, req CompletionRequest) (*initializeRequestResult, error) {
	var captureSession CaptureSession
	if c.messageCaptureStrategy != nil {
		sess, err := c.messageCaptureStrategy.StartOrResumeChat(ctx, req)
		if err != nil {
			return nil, fmt.Errorf("start or resume chat: %w", err)
		}
		captureSession = sess
	}

	// Provision API key
	apiKey, err := c.provisioner.ProvisionAPIKey(ctx, req.OrgID)
	if err != nil {
		return nil, fmt.Errorf("provision OpenRouter key: %w", err)
	}

	if _, err := uuid.Parse(req.ProjectID); err != nil {
		return nil, fmt.Errorf("invalid project ID: %w", err)
	}

	// Set defaults
	temp := float32(1.0)
	if req.Temperature != nil {
		temp = float32(*req.Temperature)
	}

	model := req.Model
	if model == "" {
		model = DefaultChatModel
	}

	// Resolve unsupported models to a supported model from the same provider
	if resolved := ResolveModel(model); resolved != "" {
		if resolved != model {
			c.logger.WarnContext(ctx, "requested model not in allowlist, falling back to supported model",
				attr.SlogGenAIRequestModel(model),
				attr.SlogGenAIResponseModel(resolved),
			)
			model = resolved
		}
	} else {
		return nil, fmt.Errorf("model %s is not allowed and no fallback is available for its provider", model)
	}

	outboundMessages := req.Messages
	if req.NormalizeOutboundMessages {
		outboundMessages = slices.Collect(NormalizeAssistantMessages(req.Messages))
	}

	// Build request body
	reqBody := OpenAIChatRequest{
		Model:          model,
		Messages:       outboundMessages,
		Stream:         req.Stream,
		Tools:          req.Tools,
		Temperature:    temp,
		ResponseFormat: nil,
	}

	// Add JSON schema if provided
	if req.JSONSchema != nil {
		jsonSchemaConfig := or.ChatFormatJSONSchemaConfig{
			Type:       or.ChatFormatJSONSchemaConfigTypeJSONSchema,
			JSONSchema: *req.JSONSchema,
		}
		responseFormat := or.CreateResponseFormatJSONSchema(jsonSchemaConfig)
		reqBody.ResponseFormat = &responseFormat
	}

	return &initializeRequestResult{
		apiKey:         apiKey,
		requestBody:    reqBody,
		captureSession: captureSession,
	}, nil
}

// makeHTTPRequest creates and executes an HTTP request to OpenRouter.
func (c *ChatClient) makeHTTPRequest(ctx context.Context, apiKey string, reqBody OpenAIChatRequest) (*http.Response, error) {
	if !IsModelAllowed(reqBody.Model) {
		return nil, fmt.Errorf("model %s is not allowed", reqBody.Model)
	}

	// Marshal request
	data, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	// Create HTTP request
	httpReq, err := http.NewRequestWithContext(
		ctx,
		"POST",
		fmt.Sprintf("%s/v1/chat/completions", OpenRouterBaseURL),
		bytes.NewReader(data),
	)
	if err != nil {
		return nil, fmt.Errorf("create HTTP request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+apiKey)

	// Make HTTP call
	httpResp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("HTTP request failed: %w", err)
	}

	return httpResp, nil
}

// onMessageComplete applies message capture and usage tracking strategies.
func (c *ChatClient) onMessageComplete(ctx context.Context, session CaptureSession, req CompletionRequest, response CompletionResponse) {
	// Apply message capture strategy
	if c.messageCaptureStrategy != nil {
		if err := c.messageCaptureStrategy.CaptureMessage(ctx, session, req, response); err != nil {
			// Log error but don't fail the request
			c.logger.ErrorContext(ctx, "failed to capture message", attr.SlogError(err))
		}
	}

	// Apply usage tracking strategy (async)
	if c.usageTrackingStrategy != nil {
		go func() {
			if err := c.usageTrackingStrategy.TrackUsage(
				context.WithoutCancel(ctx),
				response.MessageID,
				req.OrgID,
				req.ProjectID,
				req.UsageSource,
				req.ChatID.String(),
			); err != nil {
				c.logger.ErrorContext(ctx, "failed to track usage", attr.SlogError(err))
			}
		}()
	}

	projectID, err := uuid.Parse(req.ProjectID)
	if err != nil {
		c.logger.WarnContext(ctx, "failed to parse project ID for chat title generation", attr.SlogError(err))
		return
	}

	// Schedule chat title generation.
	// Use WithoutCancel to ensure the workflow is scheduled even if the HTTP request is cancelled.
	if c.chatTitleGenerator != nil && req.ChatID != uuid.Nil {
		if err := c.chatTitleGenerator.ScheduleChatTitleGeneration(
			context.WithoutCancel(ctx),
			req.ChatID.String(),
			req.OrgID,
			projectID.String(),
		); err != nil {
			c.logger.WarnContext(ctx, "failed to schedule chat title generation", attr.SlogError(err))
		}
	}

	// Schedule chat resolution analysis (will reset timer if already scheduled)
	if c.chatResolutionAnalyzer != nil && req.ChatID != uuid.Nil {
		if err := c.chatResolutionAnalyzer.ScheduleChatResolutionAnalysis(
			context.WithoutCancel(ctx),
			req.ChatID,
			projectID,
			req.OrgID,
			req.APIKeyID,
		); err != nil {
			c.logger.WarnContext(ctx, "failed to schedule chat resolution analysis", attr.SlogError(err))
		}
	}

	// Emit telemetry
	c.emitGenAITelemetry(
		ctx,
		response.ToolCalls,
		req.OrgID,
		req.ProjectID,
		req.ChatID.String(),
		req.UserID,
		req.ExternalUserID,
		req.UserEmail,
		req.APIKeyID,
		response,
	)
}

// GetCompletion makes a non-streaming completion request to OpenRouter and applies capture/tracking strategies.
func (c *ChatClient) GetCompletion(ctx context.Context, req CompletionRequest) (*CompletionResponse, error) {
	start := time.Now()

	// Build request body (non-streaming)
	initResult, err := c.initializeRequest(ctx, req)
	if err != nil {
		return nil, err
	}
	reqBody := initResult.requestBody
	reqBody.Stream = false

	// Make HTTP request
	httpResp, err := c.makeHTTPRequest(ctx, initResult.apiKey, reqBody)
	if err != nil {
		return nil, err
	}
	defer o11y.NoLogDefer(func() error { return httpResp.Body.Close() })

	// Read response body
	body, err := io.ReadAll(httpResp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response body: %w", err)
	}

	// Handle non-200 responses
	if httpResp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("OpenRouter API error (status %d): %s", httpResp.StatusCode, strings.TrimSpace(string(body)))
	}

	// Parse response
	var chatResp OpenAIChatResponse
	if err := json.Unmarshal(body, &chatResp); err != nil {
		return nil, fmt.Errorf("unmarshal response: %w", err)
	}

	// Check for choices
	if len(chatResp.Choices) == 0 {
		return nil, fmt.Errorf("no choices in response")
	}

	// Extract response data
	message := chatResp.Choices[0].Message
	finishReason := chatResp.Choices[0].FinishReason
	usage := Usage{
		PromptTokens:     0,
		CompletionTokens: 0,
		TotalTokens:      0,
	}
	if chatResp.Usage != nil {
		usage = *chatResp.Usage
	}

	// Extract tool calls
	var toolCalls []ToolCall
	if message.Type == or.ChatMessagesTypeAssistant {
		if assistantMsg := message.ChatAssistantMessage; assistantMsg != nil {
			for idx, tc := range assistantMsg.ToolCalls {
				toolCalls = append(toolCalls, ToolCall{
					Index: idx,
					ID:    tc.ID,
					Type:  string(tc.GetType()),
					Function: ToolCallFunction{
						Name:      tc.Function.Name,
						Arguments: tc.Function.Arguments,
					},
				})
			}
		}
	}

	// Extract text content
	content := GetText(message)

	response := &CompletionResponse{
		StartTime:    start,
		Message:      &message,
		MessageID:    chatResp.ID,
		Model:        chatResp.Model,
		Usage:        usage,
		FinishReason: &finishReason,
		ToolCalls:    toolCalls,
		Content:      content,
	}

	// Apply message capture and usage tracking strategies
	c.onMessageComplete(context.WithoutCancel(ctx), initResult.captureSession, req, *response)

	return response, nil
}

// GetCompletionStream makes a streaming completion request to OpenRouter.
// It returns a StreamReader that:
// - Streams SSE events to the caller transparently
// - Parses SSE chunks internally to extract message metadata
// - Automatically triggers capture/tracking strategies when the stream closes
func (c *ChatClient) GetCompletionStream(ctx context.Context, req CompletionRequest) (StreamReader, error) {
	// Build request body (streaming)
	initResult, err := c.initializeRequest(ctx, req)
	if err != nil {
		return nil, err
	}
	reqBody := initResult.requestBody
	reqBody.Stream = true

	// Make HTTP request
	httpResp, err := c.makeHTTPRequest(ctx, initResult.apiKey, reqBody)
	if err != nil {
		return nil, err
	}

	// Check status code
	if httpResp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(httpResp.Body)
		o11y.NoLogDefer(func() error { return httpResp.Body.Close() })
		return nil, fmt.Errorf("OpenRouter API error (status %d): %s", httpResp.StatusCode, strings.TrimSpace(string(body)))
	}

	// Wrap the response body with SSE parser that accumulates metadata
	streamReader := &streamingResponseReader{
		ctx:                  ctx,
		body:                 httpResp.Body,
		request:              req,
		captureSession:       initResult.captureSession,
		logger:               c.logger,
		client:               c,
		telemetryService:     c.telemetryLogger,
		lineBuf:              &strings.Builder{},
		messageContent:       &strings.Builder{},
		accumulatedToolCalls: make(map[int]ToolCall),
		startTime:            time.Now(),
		messageID:            "",
		model:                "",
		finishReason:         nil,
		usage:                Usage{PromptTokens: 0, CompletionTokens: 0, TotalTokens: 0},
		usageSet:             false,
		isDone:               false,
	}

	return streamReader, nil
}

func (c *ChatClient) GetObjectCompletion(ctx context.Context, req ObjectCompletionRequest) (*CompletionResponse, error) {
	var messages []or.ChatMessages

	// Optional system prompt
	if req.SystemPrompt != "" {
		messages = append(messages, or.CreateChatMessagesSystem(or.ChatSystemMessage{
			Role:    or.ChatSystemMessageRoleSystem,
			Content: or.CreateChatSystemMessageContentStr(req.SystemPrompt),
			Name:    nil,
		}))
	}

	// User message
	messages = append(messages, or.CreateChatMessagesUser(or.ChatUserMessage{
		Role:    or.ChatUserMessageRoleUser,
		Content: or.CreateChatUserMessageContentStr(req.Prompt),
		Name:    nil,
	}))

	completionReq := CompletionRequest{
		OrgID:          req.OrgID,
		ProjectID:      req.ProjectID,
		Messages:       messages,
		Tools:          nil,
		Temperature:    nil,
		Model:          req.Model,
		Stream:         false,
		UsageSource:    req.UsageSource,
		UserID:         req.UserID,
		ExternalUserID: req.ExternalUserID,
		UserEmail:      "",
		HTTPMetadata:   req.HTTPMetadata,
		JSONSchema:     req.JSONSchema,
		ChatID:         uuid.Nil,
		APIKeyID:       "",
	}

	return c.GetCompletion(ctx, completionReq)
}

// streamingResponseReader wraps an HTTP response body and parses SSE chunks as they stream through.
// It implements io.ReadCloser and automatically triggers capture/tracking when closed.
type streamingResponseReader struct {
	ctx              context.Context //nolint:containedctx // Context needed for telemetry and async operations
	body             io.ReadCloser
	request          CompletionRequest
	captureSession   CaptureSession
	logger           *slog.Logger
	client           *ChatClient
	telemetryService TelemetryLogger

	// SSE parsing state
	lineBuf              *strings.Builder
	messageContent       *strings.Builder
	accumulatedToolCalls map[int]ToolCall
	isDone               bool

	messageID    string
	model        string
	finishReason *string
	usage        Usage
	usageSet     bool
	startTime    time.Time
}

// Read implements io.Reader, passing data through while parsing SSE chunks.
func (r *streamingResponseReader) Read(p []byte) (int, error) {
	n, err := r.body.Read(p)
	if n > 0 {
		// Parse SSE chunks from the data
		r.parseSSEChunks(p[:n])
	}
	// Return errors as-is (including io.EOF) - don't wrap them
	// io.Copy and other readers expect io.EOF to signal normal stream completion
	return n, err //nolint:wrapcheck // io.EOF must not be wrapped for io.Reader contract
}

// Close implements io.Closer and triggers capture/tracking strategies.
func (r *streamingResponseReader) Close() error {
	// Process any remaining buffered data
	if r.lineBuf.Len() > 0 {
		r.processSSELine(r.lineBuf.String())
	}

	// Close the underlying body
	err := r.body.Close()
	if err != nil {
		err = fmt.Errorf("close response body: %w", err)
	}

	// If we have accumulated message data, trigger strategies
	if r.messageID != "" {
		response := CompletionResponse{
			StartTime:    r.startTime,
			Message:      nil, // Not available in streaming mode
			MessageID:    r.messageID,
			Model:        r.model,
			Content:      r.messageContent.String(),
			FinishReason: r.finishReason,
			Usage:        r.usage,
			ToolCalls:    make([]ToolCall, 0, len(r.accumulatedToolCalls)),
		}

		// Convert accumulated tool calls map to slice
		for _, tc := range r.accumulatedToolCalls {
			response.ToolCalls = append(response.ToolCalls, tc)
		}

		// Use WithoutCancel to ensure message capture completes even if the stream was killed
		r.client.onMessageComplete(context.WithoutCancel(r.ctx), r.captureSession, r.request, response)
	}

	return err
}

// parseSSEChunks parses Server-Sent Events from the byte stream.
func (r *streamingResponseReader) parseSSEChunks(data []byte) {
	// Accumulate data into line buffer
	r.lineBuf.Write(data)

	// Process complete lines (ending with \n)
	for {
		content := r.lineBuf.String()
		idx := strings.IndexByte(content, '\n')
		if idx == -1 {
			break // No complete line yet
		}

		// Extract line (including \n)
		line := content[:idx+1]
		r.lineBuf.Reset()
		r.lineBuf.WriteString(content[idx+1:])

		// Process the line
		r.processSSELine(line)
	}
}

// processSSELine processes a single SSE line (same logic as original responseCaptor).
func (r *streamingResponseReader) processSSELine(line string) {
	line = strings.TrimSpace(line)
	if line == "" || !strings.HasPrefix(line, "data: ") {
		return
	}

	// Extract JSON data after "data: "
	jsonData := strings.TrimPrefix(line, "data: ")
	if jsonData == "[DONE]" {
		r.isDone = true
		return
	}

	// Parse the chunk using existing StreamingChunk type
	var chunk StreamingChunk
	if err := json.Unmarshal([]byte(jsonData), &chunk); err != nil {
		r.logger.ErrorContext(r.ctx, "failed to parse SSE chunk", attr.SlogError(err))
		return
	}

	// Extract message ID and model (sent in first chunk)
	if chunk.ID != "" {
		r.messageID = chunk.ID
	}
	if chunk.Model != "" {
		r.model = chunk.Model
	}

	// Process choices
	if len(chunk.Choices) > 0 {
		choice := chunk.Choices[0]

		// Accumulate content
		if choice.Delta.Content != "" {
			r.messageContent.WriteString(choice.Delta.Content)
		}

		// Accumulate tool calls
		for _, tc := range choice.Delta.ToolCalls {
			existing := r.accumulatedToolCalls[tc.Index]
			existing.Index = tc.Index
			if tc.ID != "" {
				existing.ID = tc.ID
			}
			if tc.Type != "" {
				existing.Type = tc.Type
			}
			if tc.Function.Name != "" {
				existing.Function.Name = tc.Function.Name
			}
			if tc.Function.Arguments != "" {
				existing.Function.Arguments += tc.Function.Arguments
			}
			r.accumulatedToolCalls[tc.Index] = existing
		}

		// Capture finish reason
		if choice.FinishReason != nil {
			r.finishReason = choice.FinishReason
		}
	}

	// Capture usage (sent in final chunk)
	if chunk.Usage != nil {
		r.usage = *chunk.Usage
		r.usageSet = true
	}
}

// emitGenAITelemetry emits GenAI telemetry to ClickHouse for observability.
func (c *ChatClient) emitGenAITelemetry(
	ctx context.Context,
	toolCalls []ToolCall,
	orgID, projectID, chatID, userID, externalUserID, userEmail, apiKeyID string,
	result CompletionResponse,
) {
	// Skip telemetry if no telemetry service configured
	if c.telemetryLogger == nil {
		return
	}

	duration := float64(time.Since(result.StartTime).Seconds())

	// Build attributes map. Column-mapped keys are extracted to dedicated columns
	// but remain in the attributes JSON. Resource attributes are auto-partitioned
	// based on telemetry.ResourceAttributeKeys.
	attrs := map[attr.Key]any{
		attr.EventSourceKey: string(telemetry.EventSourceChatCompletion),
		attr.ResourceURNKey: "agents:chat:completion",
		attr.LogBodyKey: fmt.Sprintf("LLM chat completion: model=%s, input_tokens=%d, output_tokens=%d",
			result.Model, result.Usage.PromptTokens, result.Usage.CompletionTokens),

		// GenAI semantic convention attributes
		attr.GenAIOperationNameKey:     telemetry.GenAIOperationChat,
		attr.GenAIRequestModelKey:      result.Model,
		attr.GenAIResponseModelKey:     result.Model,
		attr.GenAIUsageInputTokensKey:  result.Usage.PromptTokens,
		attr.GenAIUsageOutputTokensKey: result.Usage.CompletionTokens,
		attr.GenAIUsageTotalTokensKey:  result.Usage.TotalTokens,
		attr.GenAIConversationIDKey:    chatID,
		attr.GenAIConversationDuration: duration,
		attr.APIKeyIDKey:               apiKeyID,
	}

	if result.MessageID != "" {
		attrs[attr.GenAIResponseIDKey] = result.MessageID
	}
	if result.FinishReason != nil {
		attrs[attr.GenAIResponseFinishReasonsKey] = []string{*result.FinishReason}
	}
	if len(toolCalls) > 0 {
		toolCallsJSON, _ := json.Marshal(toolCalls)
		attrs[attr.GenAIToolCallsKey] = string(toolCallsJSON)
	}
	if userID != "" {
		attrs[attr.UserIDKey] = userID
	}
	if externalUserID != "" {
		attrs[attr.ExternalUserIDKey] = externalUserID
	}
	if userEmail != "" {
		attrs[attr.UserEmailKey] = userEmail
	}

	// Extract trace context from the request context
	spanCtx := trace.SpanContextFromContext(ctx)
	if spanCtx.HasTraceID() {
		attrs[attr.TraceIDKey] = spanCtx.TraceID().String()
	}
	if spanCtx.HasSpanID() {
		attrs[attr.SpanIDKey] = spanCtx.SpanID().String()
	}

	toolInfo := telemetry.ToolInfo{
		ID:             chatID,
		URN:            chatID,
		Name:           "",
		ProjectID:      projectID,
		DeploymentID:   "",
		FunctionID:     nil,
		OrganizationID: orgID,
	}

	c.telemetryLogger.Log(ctx, telemetry.LogParams{
		Timestamp:  time.Now(),
		ToolInfo:   toolInfo,
		Attributes: attrs,
	})
}

func (c *ChatClient) CreateEmbeddings(ctx context.Context, orgID string, model string, inputs []string) ([][]float32, error) {
	openrouterKey, err := c.provisioner.ProvisionAPIKey(ctx, orgID)
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
	// Embedding models have 8192 token limit, using ~3 chars/token as conservative estimate
	const maxChars = 24_000
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
