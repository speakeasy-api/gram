package openrouter

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	or "github.com/OpenRouterTeam/go-sdk/models/components"
	"github.com/google/uuid"
	"github.com/speakeasy-api/gram/server/internal/billing"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/telemetry"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Mock implementations for testing

type mockProvisioner struct {
	apiKey string
	err    error
}

func (m *mockProvisioner) ProvisionAPIKey(ctx context.Context, orgID string) (string, error) {
	if m.err != nil {
		return "", m.err
	}
	return m.apiKey, nil
}

func (m *mockProvisioner) RefreshAPIKeyLimit(ctx context.Context, orgID string, limit *int) (int, error) {
	return 0, nil
}

func (m *mockProvisioner) GetCreditsUsed(ctx context.Context, orgID string) (float64, int, error) {
	return 0, 0, nil
}

func (m *mockProvisioner) GetModelUsage(ctx context.Context, generationID string, orgID string) (*ModelUsage, error) {
	return nil, nil
}

type mockMessageCaptureStrategy struct {
	mu                   sync.Mutex
	startOrResumeCalled  bool
	captureMessageCalled bool
	startOrResumeError   error
	captureError         error
	startRequest         *CompletionRequest
	capturedRequest      *CompletionRequest
	capturedResponse     *CompletionResponse
}

func (m *mockMessageCaptureStrategy) StartOrResumeChat(ctx context.Context, request CompletionRequest) (CaptureSession, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.startOrResumeCalled = true
	m.startRequest = &request
	return nil, m.startOrResumeError
}

func (m *mockMessageCaptureStrategy) CaptureMessage(ctx context.Context, session CaptureSession, request CompletionRequest, response CompletionResponse) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.captureMessageCalled = true
	m.capturedRequest = &request
	m.capturedResponse = &response
	return m.captureError
}

type mockUsageTrackingStrategy struct {
	mu               sync.Mutex
	trackUsageCalled bool
	trackUsageError  error
	generationID     string
	orgID            string
	projectID        string
	source           billing.ModelUsageSource
	chatID           string
}

func (m *mockUsageTrackingStrategy) TrackUsage(ctx context.Context, generationID, orgID, projectID string, source billing.ModelUsageSource, chatID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.trackUsageCalled = true
	m.generationID = generationID
	m.orgID = orgID
	m.projectID = projectID
	m.source = source
	m.chatID = chatID
	return m.trackUsageError
}

type mockChatTitleGenerator struct {
	mu        sync.Mutex
	called    bool
	err       error
	chatID    string
	orgID     string
	projectID string
	callCount int
}

func (m *mockChatTitleGenerator) ScheduleChatTitleGeneration(ctx context.Context, chatID, orgID, projectID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.called = true
	m.callCount++
	m.chatID = chatID
	m.orgID = orgID
	m.projectID = projectID
	return m.err
}

type mockChatResolutionAnalyzer struct {
	mu        sync.Mutex
	called    bool
	err       error
	chatID    uuid.UUID
	projectID uuid.UUID
	orgID     string
	apiKeyID  string
	callCount int
}

func (m *mockChatResolutionAnalyzer) ScheduleChatResolutionAnalysis(ctx context.Context, chatID, projectID uuid.UUID, orgID, apiKeyID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.called = true
	m.callCount++
	m.chatID = chatID
	m.projectID = projectID
	m.orgID = orgID
	m.apiKeyID = apiKeyID
	return m.err
}

type mockTelemetryLogger struct {
	mu     sync.Mutex
	called bool
	logs   []telemetry.LogParams
}

func (m *mockTelemetryLogger) Log(ctx context.Context, params telemetry.LogParams) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.called = true
	m.logs = append(m.logs, params)
}

func TestChatClient_GetCompletion(t *testing.T) {
	t.Parallel()

	// Create a mock OpenRouter server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify request
		assert.Equal(t, "POST", r.Method)
		assert.Equal(t, "/api/v1/chat/completions", r.URL.Path)
		assert.Equal(t, "application/json", r.Header.Get("Content-Type"))
		assert.Contains(t, r.Header.Get("Authorization"), "Bearer test-api-key")

		// Parse request body
		var reqBody OpenAIChatRequest
		err := json.NewDecoder(r.Body).Decode(&reqBody)
		if !assert.NoError(t, err) {
			return
		}
		assert.False(t, reqBody.Stream)

		// Send mock response - using raw JSON to avoid type complexity
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"id": "msg_123",
			"model": "openai/gpt-5.4",
			"choices": [{
				"message": {
					"role": "assistant",
					"content": "Hello, world!"
				},
				"finish_reason": "stop"
			}],
			"usage": {
				"prompt_tokens": 10,
				"completion_tokens": 5,
				"total_tokens": 15
			}
		}`))
	}))
	defer server.Close()

	// Create mocks
	provisioner := &mockProvisioner{apiKey: "test-api-key"}
	captureStrategy := &mockMessageCaptureStrategy{}
	trackingStrategy := &mockUsageTrackingStrategy{}
	titleGenerator := &mockChatTitleGenerator{}
	resolutionAnalyzer := &mockChatResolutionAnalyzer{}
	telemetryLogger := &mockTelemetryLogger{}

	tracerProvider := testenv.NewTracerProvider(t)
	guardianPolicy, err := guardian.NewUnsafePolicy(tracerProvider, []string{})
	require.NoError(t, err)

	// Create client
	client := NewUnifiedClient(
		slog.Default(),
		guardianPolicy,
		provisioner,
		captureStrategy,
		trackingStrategy,
		titleGenerator,
		resolutionAnalyzer,
		telemetryLogger,
	)

	// Override the HTTP client to use the test server
	client.httpClient = &http.Client{
		Transport: &testTransport{server: server},
	}

	// Create test request
	chatID := uuid.New()
	projectID := uuid.New()
	req := CompletionRequest{
		OrgID:     "test-org",
		ProjectID: projectID.String(),
		Messages: []or.ChatMessages{
			CreateMessageUser("Hello"),
		},
		ChatID:      chatID,
		UsageSource: billing.ModelUsageSourcePlayground,
		APIKeyID:    "test-api-key-id",
	}

	// Call GetCompletion
	resp, err := client.GetCompletion(context.Background(), req)
	require.NoError(t, err)
	require.NotNil(t, resp)

	// Verify response
	assert.Equal(t, "msg_123", resp.MessageID)
	assert.Equal(t, "openai/gpt-5.4", resp.Model)
	assert.Equal(t, "Hello, world!", resp.Content)
	assert.Equal(t, 10, resp.Usage.PromptTokens)
	assert.Equal(t, 5, resp.Usage.CompletionTokens)
	assert.Equal(t, 15, resp.Usage.TotalTokens)

	// Give async operations time to complete
	time.Sleep(100 * time.Millisecond)

	// Verify all services were called
	assert.True(t, captureStrategy.startOrResumeCalled, "StartOrResumeChat should be called")
	assert.True(t, captureStrategy.captureMessageCalled, "CaptureMessage should be called")
	assert.True(t, trackingStrategy.trackUsageCalled, "TrackUsage should be called")
	assert.True(t, titleGenerator.called, "ScheduleChatTitleGeneration should be called")
	assert.True(t, resolutionAnalyzer.called, "ScheduleChatResolutionAnalysis should be called")
	assert.True(t, telemetryLogger.called, "CreateLog should be called")

	// Verify captured data
	assert.Equal(t, "msg_123", captureStrategy.capturedResponse.MessageID)
	assert.Equal(t, "msg_123", trackingStrategy.generationID)
	assert.Equal(t, "test-org", trackingStrategy.orgID)
	assert.Equal(t, projectID.String(), trackingStrategy.projectID)
	assert.Equal(t, chatID.String(), titleGenerator.chatID)
	assert.Equal(t, "test-org", titleGenerator.orgID)
	assert.Equal(t, projectID.String(), titleGenerator.projectID)
	assert.Equal(t, chatID, resolutionAnalyzer.chatID)
	assert.Equal(t, projectID, resolutionAnalyzer.projectID)
	assert.Equal(t, "test-org", resolutionAnalyzer.orgID)
	assert.Equal(t, "test-api-key-id", resolutionAnalyzer.apiKeyID)
}

func TestChatClient_GetCompletionStream(t *testing.T) {
	t.Parallel()

	// Create a mock OpenRouter server that streams SSE
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify request
		assert.Equal(t, "POST", r.Method)
		assert.Equal(t, "/api/v1/chat/completions", r.URL.Path)

		// Parse request body
		var reqBody OpenAIChatRequest
		err := json.NewDecoder(r.Body).Decode(&reqBody)
		if !assert.NoError(t, err) {
			return
		}
		assert.True(t, reqBody.Stream)

		// Stream SSE response
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)

		flusher, ok := w.(http.Flusher)
		if !assert.True(t, ok, "ResponseWriter should implement http.Flusher") {
			return
		}

		// Send initial chunk with ID and model
		_, _ = fmt.Fprintf(w, "data: {\"id\":\"msg_456\",\"model\":\"openai/gpt-5.4\",\"choices\":[{\"delta\":{\"content\":\"Hello\"}}]}\n\n")
		flusher.Flush()

		// Send content chunk
		_, _ = fmt.Fprintf(w, "data: {\"choices\":[{\"delta\":{\"content\":\" streaming\"}}]}\n\n")
		flusher.Flush()

		// Send final chunk with usage and finish reason
		_, _ = fmt.Fprintf(w, "data: {\"choices\":[{\"finish_reason\":\"stop\"}],\"usage\":{\"prompt_tokens\":10,\"completion_tokens\":5,\"total_tokens\":15}}\n\n")
		flusher.Flush()

		_, _ = fmt.Fprintf(w, "data: [DONE]\n\n")
		flusher.Flush()
	}))
	defer server.Close()

	// Create mocks
	provisioner := &mockProvisioner{apiKey: "test-api-key"}
	captureStrategy := &mockMessageCaptureStrategy{}
	trackingStrategy := &mockUsageTrackingStrategy{}
	titleGenerator := &mockChatTitleGenerator{}
	resolutionAnalyzer := &mockChatResolutionAnalyzer{}
	telemetryLogger := &mockTelemetryLogger{}

	tracerProvider := testenv.NewTracerProvider(t)
	guardianPolicy, err := guardian.NewUnsafePolicy(tracerProvider, []string{})
	require.NoError(t, err)

	// Create client
	client := NewUnifiedClient(
		slog.Default(),
		guardianPolicy,
		provisioner,
		captureStrategy,
		trackingStrategy,
		titleGenerator,
		resolutionAnalyzer,
		telemetryLogger,
	)

	// Override the HTTP client to use the test server
	client.httpClient = &http.Client{
		Transport: &testTransport{server: server},
	}

	// Create test request
	chatID := uuid.New()
	projectID := uuid.New()
	req := CompletionRequest{
		OrgID:     "test-org",
		ProjectID: projectID.String(),
		Messages: []or.ChatMessages{
			CreateMessageUser("Hello"),
		},
		Stream:      true,
		ChatID:      chatID,
		UsageSource: billing.ModelUsageSourcePlayground,
		APIKeyID:    "test-api-key-id",
	}

	// Call GetCompletionStream
	streamReader, err := client.GetCompletionStream(context.Background(), req)
	require.NoError(t, err)
	require.NotNil(t, streamReader)

	// Read the entire stream
	body, err := io.ReadAll(streamReader)
	require.NoError(t, err)

	// Close the stream (this triggers message capture and tracking)
	err = streamReader.Close()
	require.NoError(t, err)

	// Verify we received the streamed data (comes as raw SSE format)
	bodyStr := string(body)
	assert.Contains(t, bodyStr, "Hello")
	assert.Contains(t, bodyStr, "streaming")

	// Give async operations time to complete
	time.Sleep(100 * time.Millisecond)

	// Verify all services were called
	assert.True(t, captureStrategy.startOrResumeCalled, "StartOrResumeChat should be called")
	assert.True(t, captureStrategy.captureMessageCalled, "CaptureMessage should be called")
	assert.True(t, trackingStrategy.trackUsageCalled, "TrackUsage should be called")
	assert.True(t, titleGenerator.called, "ScheduleChatTitleGeneration should be called")
	assert.True(t, resolutionAnalyzer.called, "ScheduleChatResolutionAnalysis should be called")
	assert.True(t, telemetryLogger.called, "CreateLog should be called")

	// Verify captured data
	assert.Equal(t, "msg_456", captureStrategy.capturedResponse.MessageID)
	assert.Equal(t, "openai/gpt-5.4", captureStrategy.capturedResponse.Model)
	assert.Equal(t, "Hello streaming", captureStrategy.capturedResponse.Content)
	assert.Equal(t, 10, captureStrategy.capturedResponse.Usage.PromptTokens)
	assert.Equal(t, 5, captureStrategy.capturedResponse.Usage.CompletionTokens)
	assert.Equal(t, 15, captureStrategy.capturedResponse.Usage.TotalTokens)
}

func TestChatClient_GetCompletion_WithToolCalls(t *testing.T) {
	t.Parallel()

	// Create a mock OpenRouter server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Send mock response with tool calls
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"id": "msg_789",
			"model": "openai/gpt-5.4",
			"choices": [{
				"message": {
					"role": "assistant",
					"content": "",
					"tool_calls": [{
						"id": "call_1",
						"type": "function",
						"function": {
							"name": "get_weather",
							"arguments": "{\"location\":\"NYC\"}"
						}
					}]
				},
				"finish_reason": "tool_calls"
			}],
			"usage": {
				"prompt_tokens": 20,
				"completion_tokens": 10,
				"total_tokens": 30
			}
		}`))
	}))
	defer server.Close()

	// Create mocks
	provisioner := &mockProvisioner{apiKey: "test-api-key"}
	captureStrategy := &mockMessageCaptureStrategy{}
	trackingStrategy := &mockUsageTrackingStrategy{}
	titleGenerator := &mockChatTitleGenerator{}
	resolutionAnalyzer := &mockChatResolutionAnalyzer{}
	telemetryService := &mockTelemetryLogger{}

	tracerProvider := testenv.NewTracerProvider(t)
	guardianPolicy, err := guardian.NewUnsafePolicy(tracerProvider, []string{})
	require.NoError(t, err)

	// Create client
	client := NewUnifiedClient(
		slog.Default(),
		guardianPolicy,
		provisioner,
		captureStrategy,
		trackingStrategy,
		titleGenerator,
		resolutionAnalyzer,
		telemetryService,
	)

	// Override the HTTP client to use the test server
	client.httpClient = &http.Client{
		Transport: &testTransport{server: server},
	}

	// Create test request with tools
	chatID := uuid.New()
	projectID := uuid.New()
	req := CompletionRequest{
		OrgID:     "test-org",
		ProjectID: projectID.String(),
		Messages: []or.ChatMessages{
			CreateMessageUser("What's the weather in NYC?"),
		},
		Tools: []Tool{
			{
				Type: "function",
				Function: &FunctionDefinition{
					Name:        "get_weather",
					Description: "Get weather for a location",
					Parameters:  json.RawMessage(`{"type":"object","properties":{"location":{"type":"string"}}}`),
				},
			},
		},
		ChatID:      chatID,
		UsageSource: billing.ModelUsageSourcePlayground,
		APIKeyID:    "test-api-key-id",
	}

	// Call GetCompletion
	resp, err := client.GetCompletion(context.Background(), req)
	require.NoError(t, err)
	require.NotNil(t, resp)

	// Verify tool calls were captured
	assert.Len(t, resp.ToolCalls, 1)
	assert.Equal(t, "call_1", resp.ToolCalls[0].ID)
	assert.Equal(t, "get_weather", resp.ToolCalls[0].Function.Name)

	// Give async operations time to complete
	time.Sleep(100 * time.Millisecond)

	// Verify telemetry includes tool calls
	assert.True(t, telemetryService.called)
	assert.NotEmpty(t, telemetryService.logs)
}

func TestChatClient_NormalizesMixedAssistantOnlyForOpenRouterRequest(t *testing.T) {
	t.Parallel()

	var reqBody OpenAIChatRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&reqBody); err != nil {
			t.Errorf("decode request body: %v", err)
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"id": "msg_123",
			"model": "anthropic/claude-sonnet-4.6",
			"choices": [{
				"message": {
					"role": "assistant",
					"content": "done"
				},
				"finish_reason": "stop"
			}],
			"usage": {
				"prompt_tokens": 20,
				"completion_tokens": 10,
				"total_tokens": 30
			}
		}`))
	}))
	defer server.Close()

	captureStrategy := &mockMessageCaptureStrategy{}

	tracerProvider := testenv.NewTracerProvider(t)
	guardianPolicy, err := guardian.NewUnsafePolicy(tracerProvider, []string{})
	require.NoError(t, err)

	client := NewUnifiedClient(
		slog.Default(),
		guardianPolicy,
		&mockProvisioner{apiKey: "test-api-key"},
		captureStrategy,
		nil,
		nil,
		nil,
		nil,
	)
	client.httpClient = &http.Client{
		Transport: &testTransport{server: server},
	}

	mixed := combinedAssistant("I'll check the weather.", "call_1")
	req := CompletionRequest{
		OrgID:     "test-org",
		ProjectID: uuid.New().String(),
		Model:     "anthropic/claude-sonnet-4.6",
		Messages: []or.ChatMessages{
			CreateMessageUser("weather?"),
			mixed,
		},
		ChatID:                    uuid.New(),
		UsageSource:               billing.ModelUsageSourcePlayground,
		NormalizeOutboundMessages: true,
	}

	_, err = client.GetCompletion(context.Background(), req)
	require.NoError(t, err)

	require.Len(t, reqBody.Messages, 2)
	require.Equal(t, "weather?", GetText(reqBody.Messages[0]))
	require.Empty(t, GetText(reqBody.Messages[1]))
	require.Len(t, reqBody.Messages[1].ChatAssistantMessage.ToolCalls, 1)
	require.Equal(t, "call_1", reqBody.Messages[1].ChatAssistantMessage.ToolCalls[0].ID)

	require.NotNil(t, captureStrategy.startRequest)
	require.Len(t, captureStrategy.startRequest.Messages, 2)
	require.Equal(t, "I'll check the weather.", GetText(captureStrategy.startRequest.Messages[1]))
	require.Len(t, captureStrategy.startRequest.Messages[1].ChatAssistantMessage.ToolCalls, 1)

	require.NotNil(t, captureStrategy.capturedRequest)
	require.Equal(t, "I'll check the weather.", GetText(captureStrategy.capturedRequest.Messages[1]))
}

func TestChatClient_PassesMixedAssistantThroughWhenNormalizeFlagUnset(t *testing.T) {
	t.Parallel()

	var reqBody OpenAIChatRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&reqBody); err != nil {
			t.Errorf("decode request body: %v", err)
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"id": "msg_123",
			"model": "anthropic/claude-sonnet-4.6",
			"choices": [{
				"message": {
					"role": "assistant",
					"content": "done"
				},
				"finish_reason": "stop"
			}],
			"usage": {
				"prompt_tokens": 20,
				"completion_tokens": 10,
				"total_tokens": 30
			}
		}`))
	}))
	defer server.Close()

	tracerProvider := testenv.NewTracerProvider(t)
	guardianPolicy, err := guardian.NewUnsafePolicy(tracerProvider, []string{})
	require.NoError(t, err)

	client := NewUnifiedClient(
		slog.Default(),
		guardianPolicy,
		&mockProvisioner{apiKey: "test-api-key"},
		&mockMessageCaptureStrategy{},
		nil,
		nil,
		nil,
		nil,
	)
	client.httpClient = &http.Client{
		Transport: &testTransport{server: server},
	}

	mixed := combinedAssistant("I'll check the weather.", "call_1")
	req := CompletionRequest{
		OrgID:     "test-org",
		ProjectID: uuid.New().String(),
		Model:     "anthropic/claude-sonnet-4.6",
		Messages: []or.ChatMessages{
			CreateMessageUser("weather?"),
			mixed,
		},
		ChatID:      uuid.New(),
		UsageSource: billing.ModelUsageSourcePlayground,
	}

	_, err = client.GetCompletion(context.Background(), req)
	require.NoError(t, err)

	require.Len(t, reqBody.Messages, 2)
	require.Equal(t, "I'll check the weather.", GetText(reqBody.Messages[1]),
		"narrative text must be preserved when NormalizeOutboundMessages is false")
	require.Len(t, reqBody.Messages[1].ChatAssistantMessage.ToolCalls, 1)
	require.Equal(t, "call_1", reqBody.Messages[1].ChatAssistantMessage.ToolCalls[0].ID)
}

func TestChatClient_ErrorHandling(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name               string
		provisionerError   error
		startOrResumeError error
		captureError       error
		expectedError      string
	}{
		{
			name:             "provisioner error",
			provisionerError: fmt.Errorf("failed to provision key"),
			expectedError:    "provision OpenRouter key",
		},
		{
			name:               "start or resume error",
			startOrResumeError: fmt.Errorf("failed to start chat"),
			expectedError:      "start or resume chat",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			// Create mocks with errors
			provisioner := &mockProvisioner{
				apiKey: "test-api-key",
				err:    tt.provisionerError,
			}
			captureStrategy := &mockMessageCaptureStrategy{
				startOrResumeError: tt.startOrResumeError,
				captureError:       tt.captureError,
			}
			trackingStrategy := &mockUsageTrackingStrategy{}
			titleGenerator := &mockChatTitleGenerator{}
			resolutionAnalyzer := &mockChatResolutionAnalyzer{}
			telemetryService := &mockTelemetryLogger{}

			tracerProvider := testenv.NewTracerProvider(t)
			guardianPolicy, err := guardian.NewUnsafePolicy(tracerProvider, []string{})
			require.NoError(t, err)

			// Create client
			client := NewUnifiedClient(
				slog.Default(),
				guardianPolicy,
				provisioner,
				captureStrategy,
				trackingStrategy,
				titleGenerator,
				resolutionAnalyzer,
				telemetryService,
			)

			// Create test request
			projectID := uuid.New()
			req := CompletionRequest{
				OrgID:     "test-org",
				ProjectID: projectID.String(),
				Messages: []or.ChatMessages{
					CreateMessageUser("Hello"),
				},
				ChatID:      uuid.New(),
				UsageSource: billing.ModelUsageSourcePlayground,
			}

			// Call GetCompletion
			_, err = client.GetCompletion(context.Background(), req)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.expectedError)
		})
	}
}

func TestChatClient_MultipleCompletions_TitleAndResolutionScheduling(t *testing.T) {
	t.Parallel()

	// Create a mock OpenRouter server
	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		w.Header().Set("Content-Type", "application/json")
		response := fmt.Sprintf(`{
			"id": "msg_%d",
			"model": "openai/gpt-5.4",
			"choices": [{
				"message": {
					"role": "assistant",
					"content": "Response %d"
				},
				"finish_reason": "stop"
			}],
			"usage": {
				"prompt_tokens": 10,
				"completion_tokens": 5,
				"total_tokens": 15
			}
		}`, callCount, callCount)
		_, _ = w.Write([]byte(response))
	}))
	defer server.Close()

	// Create mocks
	provisioner := &mockProvisioner{apiKey: "test-api-key"}
	captureStrategy := &mockMessageCaptureStrategy{}
	trackingStrategy := &mockUsageTrackingStrategy{}
	titleGenerator := &mockChatTitleGenerator{}
	resolutionAnalyzer := &mockChatResolutionAnalyzer{}
	telemetryService := &mockTelemetryLogger{}

	tracerProvider := testenv.NewTracerProvider(t)
	guardianPolicy, err := guardian.NewUnsafePolicy(tracerProvider, []string{})
	require.NoError(t, err)

	// Create client
	client := NewUnifiedClient(
		slog.Default(),
		guardianPolicy,
		provisioner,
		captureStrategy,
		trackingStrategy,
		titleGenerator,
		resolutionAnalyzer,
		telemetryService,
	)

	// Override the HTTP client to use the test server
	client.httpClient = &http.Client{
		Transport: &testTransport{server: server},
	}

	// Create test request
	chatID := uuid.New()
	projectID := uuid.New()
	req := CompletionRequest{
		OrgID:     "test-org",
		ProjectID: projectID.String(),
		Messages: []or.ChatMessages{
			CreateMessageUser("Hello"),
		},
		ChatID:      chatID,
		UsageSource: billing.ModelUsageSourcePlayground,
		APIKeyID:    "test-api-key-id",
	}

	// Make multiple completions
	for range 3 {
		_, err := client.GetCompletion(context.Background(), req)
		require.NoError(t, err)
	}

	// Give async operations time to complete
	time.Sleep(200 * time.Millisecond)

	// Title generation should be scheduled for each completion since the simple mock always reports isFirstMessage=true.
	// In production, the real capture strategy only returns isFirstMessage=true on the first call.
	titleGenerator.mu.Lock()
	assert.Equal(t, 3, titleGenerator.callCount, "Title generation should be scheduled when isFirstMessage=true (mock always returns true)")
	titleGenerator.mu.Unlock()

	// Resolution analysis should be scheduled for every message (resets timer)
	resolutionAnalyzer.mu.Lock()
	assert.Equal(t, 3, resolutionAnalyzer.callCount, "Resolution analysis should be scheduled for each completion (resets timer)")
	resolutionAnalyzer.mu.Unlock()
}

// trackingTitleGenerator records every ScheduleChatTitleGeneration call with its chatID.
type trackingTitleGenerator struct {
	mu    sync.Mutex
	calls []string // chatIDs for each call
	err   error
}

func (m *trackingTitleGenerator) ScheduleChatTitleGeneration(ctx context.Context, chatID, orgID, projectID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.calls = append(m.calls, chatID)
	return m.err
}

// trackingCaptureStrategy simulates the real ChatMessageCaptureStrategy counting behavior.
// It maintains a per-chat message count and tracks all stored messages.
type trackingCaptureStrategy struct {
	mu             sync.Mutex
	messageCount   map[uuid.UUID]int // per-chat message count (simulates CountChatMessages)
	storedMessages []storedMessage
}

type storedMessage struct {
	chatID uuid.UUID
	role   string
	source string // "StartOrResumeChat" or "CaptureMessage"
}

func newTrackingCaptureStrategy() *trackingCaptureStrategy {
	return &trackingCaptureStrategy{
		messageCount:   make(map[uuid.UUID]int),
		storedMessages: nil,
	}
}

func (t *trackingCaptureStrategy) StartOrResumeChat(ctx context.Context, request CompletionRequest) (CaptureSession, error) {
	t.mu.Lock()
	defer t.mu.Unlock()

	chatID := request.ChatID
	if chatID == uuid.Nil {
		return nil, nil
	}

	currentCount := t.messageCount[chatID]
	if currentCount < len(request.Messages) {
		newMessages := request.Messages[currentCount:]
		for _, msg := range newMessages {
			t.storedMessages = append(t.storedMessages, storedMessage{
				chatID: chatID,
				role:   GetRole(msg),
				source: "StartOrResumeChat",
			})
			t.messageCount[chatID]++
		}
	}
	return nil, nil
}

func (t *trackingCaptureStrategy) CaptureMessage(ctx context.Context, session CaptureSession, request CompletionRequest, response CompletionResponse) error {
	t.mu.Lock()
	defer t.mu.Unlock()

	if request.ChatID == uuid.Nil {
		return nil
	}

	t.storedMessages = append(t.storedMessages, storedMessage{
		chatID: request.ChatID,
		role:   "assistant",
		source: "CaptureMessage",
	})
	t.messageCount[request.ChatID]++
	return nil
}

func newMockServer(t *testing.T) *httptest.Server {
	t.Helper()
	callCount := 0
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		w.Header().Set("Content-Type", "application/json")
		response := fmt.Sprintf(`{
			"id": "msg_%d",
			"model": "openai/gpt-5.4",
			"choices": [{
				"message": {
					"role": "assistant",
					"content": "Response %d"
				},
				"finish_reason": "stop"
			}],
			"usage": {
				"prompt_tokens": 10,
				"completion_tokens": 5,
				"total_tokens": 15
			}
		}`, callCount, callCount)
		_, _ = w.Write([]byte(response))
	}))
}

// BUG: Title generation should NOT be scheduled when ChatID is nil.
// The title generation activity itself uses GetCompletion with ChatID=uuid.Nil,
// which means onMessageComplete erroneously schedules another title generation
// workflow with the nil UUID, creating v1:generate-chat-title:00000000-...
func TestChatClient_NilChatID_ShouldNotScheduleTitleGeneration(t *testing.T) {
	t.Parallel()

	server := newMockServer(t)
	defer server.Close()

	tracerProvider := testenv.NewTracerProvider(t)
	guardianPolicy, err := guardian.NewUnsafePolicy(tracerProvider, []string{})
	require.NoError(t, err)

	titleGenerator := &trackingTitleGenerator{}
	client := NewUnifiedClient(
		slog.Default(),
		guardianPolicy,
		&mockProvisioner{apiKey: "test-api-key"},
		&mockMessageCaptureStrategy{},
		&mockUsageTrackingStrategy{},
		titleGenerator,
		&mockChatResolutionAnalyzer{},
		&mockTelemetryLogger{},
	)
	client.httpClient = &http.Client{Transport: &testTransport{server: server}}

	projectID := uuid.New()
	req := CompletionRequest{
		OrgID:       "test-org",
		ProjectID:   projectID.String(),
		Messages:    []or.ChatMessages{CreateMessageUser("Hello")},
		ChatID:      uuid.Nil, // No chat — like title generation activity's internal call
		UsageSource: billing.ModelUsageSourceGram,
		APIKeyID:    "",
	}

	_, err = client.GetCompletion(context.Background(), req)
	require.NoError(t, err)

	time.Sleep(100 * time.Millisecond)

	titleGenerator.mu.Lock()
	defer titleGenerator.mu.Unlock()
	require.Empty(t, titleGenerator.calls,
		"title generation must not be scheduled when ChatID is nil")
}

// Title generation should be scheduled on every completion that has a real ChatID,
// but NEVER for completions with ChatID == uuid.Nil (e.g. internal title-gen calls).
func TestChatClient_TitleGeneration_ScheduledPerCompletionWithValidChatID(t *testing.T) {
	t.Parallel()

	server := newMockServer(t)
	defer server.Close()

	tracerProvider := testenv.NewTracerProvider(t)
	guardianPolicy, err := guardian.NewUnsafePolicy(tracerProvider, []string{})
	require.NoError(t, err)

	titleGenerator := &trackingTitleGenerator{}
	tracker := newTrackingCaptureStrategy()
	client := NewUnifiedClient(
		slog.Default(),
		guardianPolicy,
		&mockProvisioner{apiKey: "test-api-key"},
		tracker,
		&mockUsageTrackingStrategy{},
		titleGenerator,
		&mockChatResolutionAnalyzer{},
		&mockTelemetryLogger{},
	)
	client.httpClient = &http.Client{Transport: &testTransport{server: server}}

	chatID := uuid.New()
	projectID := uuid.New()

	// Three completions with a valid ChatID
	for range 3 {
		_, err := client.GetCompletion(context.Background(), CompletionRequest{
			OrgID:       "test-org",
			ProjectID:   projectID.String(),
			Messages:    []or.ChatMessages{CreateMessageUser("Hello")},
			ChatID:      chatID,
			UsageSource: billing.ModelUsageSourcePlayground,
			APIKeyID:    "key-1",
		})
		require.NoError(t, err)
	}

	// One completion with nil ChatID (simulating title-gen activity's internal call)
	_, err = client.GetCompletion(context.Background(), CompletionRequest{
		OrgID:       "test-org",
		ProjectID:   projectID.String(),
		Messages:    []or.ChatMessages{CreateMessageUser("Generate title")},
		ChatID:      uuid.Nil,
		UsageSource: billing.ModelUsageSourceGram,
		APIKeyID:    "",
	})
	require.NoError(t, err)

	time.Sleep(100 * time.Millisecond)

	titleGenerator.mu.Lock()
	defer titleGenerator.mu.Unlock()

	// Only the 3 real-chat completions should trigger title gen, not the nil one
	require.Len(t, titleGenerator.calls, 3,
		"title generation should be scheduled for each completion with a valid ChatID")
	for _, id := range titleGenerator.calls {
		require.Equal(t, chatID.String(), id,
			"all title generation calls should use the real chat ID, never nil")
	}
}

// Verify that reloading a chat and sending a new message does not duplicate
// the last message in the history.
func TestChatClient_ReloadChat_NoDuplicateMessages(t *testing.T) {
	t.Parallel()

	server := newMockServer(t)
	defer server.Close()

	tracerProvider := testenv.NewTracerProvider(t)
	guardianPolicy, err := guardian.NewUnsafePolicy(tracerProvider, []string{})
	require.NoError(t, err)

	tracker := newTrackingCaptureStrategy()
	client := NewUnifiedClient(
		slog.Default(),
		guardianPolicy,
		&mockProvisioner{apiKey: "test-api-key"},
		tracker,
		&mockUsageTrackingStrategy{},
		&mockChatTitleGenerator{},
		&mockChatResolutionAnalyzer{},
		&mockTelemetryLogger{},
	)
	client.httpClient = &http.Client{Transport: &testTransport{server: server}}

	chatID := uuid.New()
	projectID := uuid.New()

	// Round 1: First message
	req1 := CompletionRequest{
		OrgID:       "test-org",
		ProjectID:   projectID.String(),
		Messages:    []or.ChatMessages{CreateMessageUser("Hello")},
		ChatID:      chatID,
		UsageSource: billing.ModelUsageSourcePlayground,
		APIKeyID:    "key-1",
	}
	_, err = client.GetCompletion(context.Background(), req1)
	require.NoError(t, err)

	// After round 1: DB should have [user(StartOrResumeChat), assistant(CaptureMessage)]
	tracker.mu.Lock()
	require.Equal(t, 2, tracker.messageCount[chatID], "round 1: should have 2 messages stored")
	require.Len(t, tracker.storedMessages, 2)
	require.Equal(t, "user", tracker.storedMessages[0].role)
	require.Equal(t, "StartOrResumeChat", tracker.storedMessages[0].source)
	require.Equal(t, "assistant", tracker.storedMessages[1].role)
	require.Equal(t, "CaptureMessage", tracker.storedMessages[1].source)
	tracker.mu.Unlock()

	// Round 2: Simulate reload — client sends all history + new user message
	req2 := CompletionRequest{
		OrgID:     "test-org",
		ProjectID: projectID.String(),
		Messages: []or.ChatMessages{
			CreateMessageUser("Hello"),           // from history
			CreateMessageAssistant("Response 1"), // from history
			CreateMessageUser("How are you?"),    // new user message
		},
		ChatID:      chatID,
		UsageSource: billing.ModelUsageSourcePlayground,
		APIKeyID:    "key-1",
	}
	_, err = client.GetCompletion(context.Background(), req2)
	require.NoError(t, err)

	// After round 2: DB should have 4 messages total, NO duplicates
	tracker.mu.Lock()
	defer tracker.mu.Unlock()

	require.Equal(t, 4, tracker.messageCount[chatID],
		"round 2: should have exactly 4 messages (user1, assistant1, user2, assistant2)")
	require.Len(t, tracker.storedMessages, 4)

	// Verify the assistant from round 1 was NOT re-stored by StartOrResumeChat
	assistantStoredByStartOrResume := 0
	for _, msg := range tracker.storedMessages {
		if msg.role == "assistant" && msg.source == "StartOrResumeChat" {
			assistantStoredByStartOrResume++
		}
	}
	require.Equal(t, 0, assistantStoredByStartOrResume,
		"assistant messages should never be re-stored by StartOrResumeChat on reload")

	// Verify the exact sequence of stored messages
	require.Equal(t, "user", tracker.storedMessages[0].role)      // round 1: user
	require.Equal(t, "assistant", tracker.storedMessages[1].role) // round 1: assistant response
	require.Equal(t, "user", tracker.storedMessages[2].role)      // round 2: new user message only
	require.Equal(t, "assistant", tracker.storedMessages[3].role) // round 2: assistant response
}

// TestChatClient_GetCompletion_WithJSONSchema verifies that when a JSONSchema is provided,
// it gets passed through to the OpenRouter API as response_format for structured output.
// This is required for AI SDK's generateObject to work correctly.
func TestChatClient_GetCompletion_WithJSONSchema(t *testing.T) {
	t.Parallel()

	// Track the request body sent to the server
	var receivedRequestBody OpenAIChatRequest

	// Create a mock OpenRouter server that captures the request
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Parse and capture request body
		err := json.NewDecoder(r.Body).Decode(&receivedRequestBody)
		if !assert.NoError(t, err) {
			w.WriteHeader(http.StatusBadRequest)
			return
		}

		// Send mock response with structured JSON
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"id": "msg_schema_test",
			"model": "openai/gpt-5.4-mini",
			"choices": [{
				"message": {
					"role": "assistant",
					"content": "{\"isQuestion\": true}"
				},
				"finish_reason": "stop"
			}],
			"usage": {
				"prompt_tokens": 10,
				"completion_tokens": 5,
				"total_tokens": 15
			}
		}`))
	}))
	defer server.Close()

	// Create mocks
	provisioner := &mockProvisioner{apiKey: "test-api-key"}
	captureStrategy := &mockMessageCaptureStrategy{}
	trackingStrategy := &mockUsageTrackingStrategy{}
	titleGenerator := &mockChatTitleGenerator{}
	resolutionAnalyzer := &mockChatResolutionAnalyzer{}
	telemetryService := &mockTelemetryLogger{}

	tracerProvider := testenv.NewTracerProvider(t)
	guardianPolicy, err := guardian.NewUnsafePolicy(tracerProvider, []string{})
	require.NoError(t, err)

	// Create client
	client := NewUnifiedClient(
		slog.Default(),
		guardianPolicy,
		provisioner,
		captureStrategy,
		trackingStrategy,
		titleGenerator,
		resolutionAnalyzer,
		telemetryService,
	)

	// Override the HTTP client to use the test server
	client.httpClient = &http.Client{
		Transport: &testTransport{server: server},
	}

	// Create test request with JSON schema (simulating AI SDK's generateObject)
	chatID := uuid.New()
	projectID := uuid.New()
	req := CompletionRequest{
		OrgID:     "test-org",
		ProjectID: projectID.String(),
		Messages: []or.ChatMessages{
			CreateMessageUser("Is this a question?"),
		},
		ChatID:      chatID,
		UsageSource: billing.ModelUsageSourcePlayground,
		APIKeyID:    "test-api-key-id",
		JSONSchema: &or.ChatJSONSchemaConfig{
			Name: "question_check",
			Schema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"isQuestion": map[string]any{
						"type":        "boolean",
						"description": "Whether the message is a question",
					},
				},
				"required": []string{"isQuestion"},
			},
			Description: nil,
			Strict:      nil,
		},
	}

	// Call GetCompletion
	resp, err := client.GetCompletion(context.Background(), req)
	require.NoError(t, err)
	require.NotNil(t, resp)

	// Verify response
	assert.Equal(t, "msg_schema_test", resp.MessageID)
	assert.Contains(t, resp.Content, "isQuestion")

	// Verify that response_format was included in the request to OpenRouter
	require.NotNil(t, receivedRequestBody.ResponseFormat, "response_format should be set when JSONSchema is provided")

	// Verify the response_format contains the JSON schema
	assert.Equal(t, "json_schema", string(receivedRequestBody.ResponseFormat.Type),
		"response_format type should be json_schema")
	assert.NotNil(t, receivedRequestBody.ResponseFormat.ChatFormatJSONSchemaConfig,
		"ChatFormatJSONSchemaConfig should be set")
	assert.Equal(t, "question_check", receivedRequestBody.ResponseFormat.ChatFormatJSONSchemaConfig.JSONSchema.Name,
		"schema name should match")
}

// TestChatClient_GetCompletion_WithoutJSONSchema verifies that when no JSONSchema is provided,
// response_format is not set (preserving default behavior).
func TestChatClient_GetCompletion_WithoutJSONSchema(t *testing.T) {
	t.Parallel()

	// Track the request body sent to the server
	var receivedRequestBody OpenAIChatRequest

	// Create a mock OpenRouter server that captures the request
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Parse and capture request body
		err := json.NewDecoder(r.Body).Decode(&receivedRequestBody)
		if !assert.NoError(t, err) {
			w.WriteHeader(http.StatusBadRequest)
			return
		}

		// Send mock response
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"id": "msg_no_schema",
			"model": "openai/gpt-5.4",
			"choices": [{
				"message": {
					"role": "assistant",
					"content": "Hello, world!"
				},
				"finish_reason": "stop"
			}],
			"usage": {
				"prompt_tokens": 10,
				"completion_tokens": 5,
				"total_tokens": 15
			}
		}`))
	}))
	defer server.Close()

	// Create mocks
	provisioner := &mockProvisioner{apiKey: "test-api-key"}
	captureStrategy := &mockMessageCaptureStrategy{}
	trackingStrategy := &mockUsageTrackingStrategy{}
	titleGenerator := &mockChatTitleGenerator{}
	resolutionAnalyzer := &mockChatResolutionAnalyzer{}
	telemetryService := &mockTelemetryLogger{}

	tracerProvider := testenv.NewTracerProvider(t)
	guardianPolicy, err := guardian.NewUnsafePolicy(tracerProvider, []string{})
	require.NoError(t, err)

	// Create client
	client := NewUnifiedClient(
		slog.Default(),
		guardianPolicy,
		provisioner,
		captureStrategy,
		trackingStrategy,
		titleGenerator,
		resolutionAnalyzer,
		telemetryService,
	)

	// Override the HTTP client to use the test server
	client.httpClient = &http.Client{
		Transport: &testTransport{server: server},
	}

	// Create test request WITHOUT JSON schema
	chatID := uuid.New()
	projectID := uuid.New()
	req := CompletionRequest{
		OrgID:     "test-org",
		ProjectID: projectID.String(),
		Messages: []or.ChatMessages{
			CreateMessageUser("Hello"),
		},
		ChatID:      chatID,
		UsageSource: billing.ModelUsageSourcePlayground,
		APIKeyID:    "test-api-key-id",
		JSONSchema:  nil, // No schema
	}

	// Call GetCompletion
	resp, err := client.GetCompletion(context.Background(), req)
	require.NoError(t, err)
	require.NotNil(t, resp)

	// Verify that response_format was NOT set
	assert.Nil(t, receivedRequestBody.ResponseFormat,
		"response_format should not be set when JSONSchema is nil")
}

func firstAllowedModelForProvider(prefix string) string {
	var models []string
	for m := range allowList {
		if strings.HasPrefix(m, prefix) {
			models = append(models, m)
		}
	}
	sort.Strings(models)
	return models[0]
}

func TestResolveModel_AllowedModelReturnedAsIs(t *testing.T) {
	t.Parallel()
	require.Equal(t, "openai/gpt-5.4", ResolveModel("openai/gpt-5.4"))
}

func TestResolveModel_UnsupportedOpenAIFallback(t *testing.T) {
	t.Parallel()
	expected := firstAllowedModelForProvider("openai/")
	require.Equal(t, expected, ResolveModel("openai/gpt-4"))
}

func TestResolveModel_UnsupportedAnthropicFallback(t *testing.T) {
	t.Parallel()
	expected := firstAllowedModelForProvider("anthropic/")
	require.Equal(t, expected, ResolveModel("anthropic/claude-2"))
}

func TestResolveModel_UnknownProviderReturnsEmpty(t *testing.T) {
	t.Parallel()
	require.Empty(t, ResolveModel("fakeprovider/some-model"))
}

func TestResolveModel_BareModelNameReturnsEmpty(t *testing.T) {
	t.Parallel()
	require.Empty(t, ResolveModel("gpt-4"))
}

func TestResolveModel_EmptyModelReturnsEmpty(t *testing.T) {
	t.Parallel()
	require.Empty(t, ResolveModel(""))
}

func TestChatClient_GetCompletion_UnsupportedModelFallback(t *testing.T) {
	t.Parallel()

	// Track which model the server receives
	var receivedModel string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var reqBody OpenAIChatRequest
		_ = json.NewDecoder(r.Body).Decode(&reqBody)
		receivedModel = reqBody.Model

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(fmt.Appendf(nil, `{
			"id": "msg_fallback",
			"model": "%s",
			"choices": [{"message": {"role": "assistant", "content": "Hello"}, "finish_reason": "stop"}],
			"usage": {"prompt_tokens": 10, "completion_tokens": 5, "total_tokens": 15}
		}`, reqBody.Model))
	}))
	defer server.Close()

	tracerProvider := testenv.NewTracerProvider(t)
	guardianPolicy, err := guardian.NewUnsafePolicy(tracerProvider, []string{})
	require.NoError(t, err)

	client := NewUnifiedClient(
		slog.Default(),
		guardianPolicy,
		&mockProvisioner{apiKey: "test-api-key"},
		&mockMessageCaptureStrategy{},
		&mockUsageTrackingStrategy{},
		&mockChatTitleGenerator{},
		&mockChatResolutionAnalyzer{},
		&mockTelemetryLogger{},
	)
	client.httpClient = &http.Client{Transport: &testTransport{server: server}}

	projectID := uuid.New()
	req := CompletionRequest{
		OrgID:       "test-org",
		ProjectID:   projectID.String(),
		Messages:    []or.ChatMessages{CreateMessageUser("Hello")},
		Model:       "openai/gpt-4", // unsupported model
		ChatID:      uuid.New(),
		UsageSource: billing.ModelUsageSourcePlayground,
	}

	resp, err := client.GetCompletion(t.Context(), req)
	require.NoError(t, err, "unsupported model should fall back, not error")
	require.NotNil(t, resp)

	// The server should have received a supported openai model, not gpt-4
	require.True(t, IsModelAllowed(receivedModel),
		"server should receive a model from the allowlist, got: %s", receivedModel)
	require.True(t, strings.HasPrefix(receivedModel, "openai/"),
		"fallback model should be from the same provider (openai), got: %s", receivedModel)
}

// testTransport is a custom http.RoundTripper that redirects all requests to the test server
type testTransport struct {
	server *httptest.Server
}

func (t *testTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	// Modify the request URL to point to the test server
	req.URL.Scheme = "http"
	req.URL.Host = t.server.URL[7:] // Remove "http://" prefix
	resp, err := http.DefaultTransport.RoundTrip(req)
	if err != nil {
		return nil, fmt.Errorf("test transport round trip: %w", err)
	}
	return resp, nil
}
