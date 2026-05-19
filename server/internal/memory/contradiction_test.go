package memory

import (
	"context"
	"errors"
	"testing"
	"time"

	or "github.com/OpenRouterTeam/go-sdk/models/components"
	"github.com/OpenRouterTeam/go-sdk/optionalnullable"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/openrouter"
)

// mockCompletionClient implements openrouter.CompletionClient for tests.
type mockCompletionClient struct {
	mock.Mock
}

func (m *mockCompletionClient) GetCompletion(ctx context.Context, request openrouter.CompletionRequest) (*openrouter.CompletionResponse, error) {
	args := m.Called(ctx, request)
	resp, _ := args.Get(0).(*openrouter.CompletionResponse)
	return resp, args.Error(1)
}

func (m *mockCompletionClient) GetCompletionStream(ctx context.Context, request openrouter.CompletionRequest) (openrouter.StreamReader, error) {
	args := m.Called(ctx, request)
	r, _ := args.Get(0).(openrouter.StreamReader)
	return r, args.Error(1)
}

func (m *mockCompletionClient) GetObjectCompletion(ctx context.Context, request openrouter.ObjectCompletionRequest) (*openrouter.CompletionResponse, error) {
	args := m.Called(ctx, request)
	resp, _ := args.Get(0).(*openrouter.CompletionResponse)
	return resp, args.Error(1)
}

func (m *mockCompletionClient) CreateEmbeddings(ctx context.Context, orgID string, model string, inputs []string, opts ...openrouter.EmbeddingOption) ([][]float32, error) {
	var resolved openrouter.EmbeddingOptions
	for _, opt := range opts {
		opt(&resolved)
	}
	args := m.Called(ctx, orgID, model, inputs, resolved)
	v, _ := args.Get(0).([][]float32)
	return v, args.Error(1)
}

func newAssistantTextResponse(text string) *openrouter.CompletionResponse {
	content := or.CreateChatAssistantMessageContentStr(text)
	msg := or.CreateChatMessagesAssistant(or.ChatAssistantMessage{
		Role:             or.ChatAssistantMessageRoleAssistant,
		Content:          optionalnullable.From(&content),
		Name:             nil,
		ToolCalls:        nil,
		Refusal:          nil,
		Reasoning:        nil,
		ReasoningDetails: nil,
		Images:           nil,
		Audio:            nil,
	})
	return &openrouter.CompletionResponse{
		StartTime:    time.Time{},
		Message:      &msg,
		MessageID:    "msg_test",
		Model:        DefaultContradictionModel,
		Usage:        openrouter.Usage{PromptTokens: 0, CompletionTokens: 0, TotalTokens: 0},
		FinishReason: nil,
		ToolCalls:    nil,
		Content:      text,
	}
}

func newServiceForContradictionTest(t *testing.T, client *mockCompletionClient) *MemoryService {
	t.Helper()

	logger := testenv.NewLogger(t)
	tracerProvider := testenv.NewTracerProvider(t)
	meterProvider := testenv.NewMeterProvider(t)

	return &MemoryService{
		logger:             logger,
		tracer:             tracerProvider.Tracer("test"),
		db:                 nil,
		completions:        client,
		audit:              nil,
		metrics:            newMemoryMetrics(meterProvider, logger),
		embeddingModel:     DefaultEmbeddingModel,
		contradictionModel: DefaultContradictionModel,
		halfLife:           DefaultHalfLife,
		hardCap:            DefaultHardCap,
		dedupeUpper:        DefaultDedupeUpper,
		dedupeLower:        DefaultDedupeLower,
		forgetAmbiguityGap: DefaultForgetAmbiguityGap,
		perResultBytes:     DefaultPerResultBytes,
		aggregateBytes:     DefaultAggregateBytes,
		truncationSuffix:   DefaultTruncationSuffix,
	}
}

func TestDetectContradictionTrue(t *testing.T) {
	t.Parallel()

	client := &mockCompletionClient{}
	client.On("GetObjectCompletion", mock.Anything, mock.Anything).
		Return(newAssistantTextResponse(`{"contradicts": true, "confidence": 0.95}`), nil).Once()

	svc := newServiceForContradictionTest(t, client)
	got, err := svc.detectContradiction(t.Context(), "org_1", "00000000-0000-0000-0000-000000000001", "user prefers tea", "user no longer drinks tea")
	require.NoError(t, err)
	require.True(t, got)
	client.AssertExpectations(t)
}

func TestDetectContradictionFalse(t *testing.T) {
	t.Parallel()

	client := &mockCompletionClient{}
	client.On("GetObjectCompletion", mock.Anything, mock.Anything).
		Return(newAssistantTextResponse(`{"contradicts": false, "confidence": 0.8}`), nil).Once()

	svc := newServiceForContradictionTest(t, client)
	got, err := svc.detectContradiction(t.Context(), "org_1", "00000000-0000-0000-0000-000000000001", "user prefers tea", "user also drinks coffee")
	require.NoError(t, err)
	require.False(t, got)
	client.AssertExpectations(t)
}

func TestDetectContradictionAPIError(t *testing.T) {
	t.Parallel()

	wantErr := errors.New("upstream blew up")
	client := &mockCompletionClient{}
	client.On("GetObjectCompletion", mock.Anything, mock.Anything).
		Return((*openrouter.CompletionResponse)(nil), wantErr).Once()

	svc := newServiceForContradictionTest(t, client)
	got, err := svc.detectContradiction(t.Context(), "org_1", "00000000-0000-0000-0000-000000000001", "a", "b")
	require.Error(t, err)
	require.False(t, got)
	require.ErrorIs(t, err, wantErr)
}

func TestDetectContradictionParseFailure(t *testing.T) {
	t.Parallel()

	client := &mockCompletionClient{}
	client.On("GetObjectCompletion", mock.Anything, mock.Anything).
		Return(newAssistantTextResponse("not json"), nil).Once()

	svc := newServiceForContradictionTest(t, client)
	got, err := svc.detectContradiction(t.Context(), "org_1", "00000000-0000-0000-0000-000000000001", "a", "b")
	require.Error(t, err)
	require.False(t, got)
}

func TestDetectContradictionEmptyResponse(t *testing.T) {
	t.Parallel()

	client := &mockCompletionClient{}
	client.On("GetObjectCompletion", mock.Anything, mock.Anything).
		Return((*openrouter.CompletionResponse)(nil), nil).Once()

	svc := newServiceForContradictionTest(t, client)
	got, err := svc.detectContradiction(t.Context(), "org_1", "00000000-0000-0000-0000-000000000001", "a", "b")
	require.Error(t, err)
	require.False(t, got)
}

func TestDetectContradictionIgnoresExtraFields(t *testing.T) {
	t.Parallel()

	// `additionalProperties: false` is enforced by the upstream model. If the
	// model still returns extra fields, json.Unmarshal must drop them silently.
	client := &mockCompletionClient{}
	client.On("GetObjectCompletion", mock.Anything, mock.Anything).
		Return(newAssistantTextResponse(`{"contradicts": true, "confidence": 0.7, "rationale": "wins"}`), nil).Once()

	svc := newServiceForContradictionTest(t, client)
	got, err := svc.detectContradiction(t.Context(), "org_1", "00000000-0000-0000-0000-000000000001", "a", "b")
	require.NoError(t, err)
	require.True(t, got)
}
