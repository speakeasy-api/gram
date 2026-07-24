package chat_test

import (
	"context"
	"testing"
	"time"

	or "github.com/OpenRouterTeam/go-sdk/models/components"
	"github.com/OpenRouterTeam/go-sdk/optionalnullable"
	"github.com/google/uuid"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/chat"
	"github.com/speakeasy-api/gram/server/internal/chat/repo"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/openrouter"
)

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
	args := m.Called(ctx, orgID, model, inputs)
	v, _ := args.Get(0).([][]float32)
	return v, args.Error(1)
}

func assistantTextResponse(text string) *openrouter.CompletionResponse {
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
		Model:        "test-model",
		Usage:        openrouter.Usage{PromptTokens: 0, CompletionTokens: 0, TotalTokens: 0},
		FinishReason: nil,
		ToolCalls:    nil,
		Content:      text,
	}
}

func TestService_Summarize_GeneratesAndCaches(t *testing.T) {
	t.Parallel()

	client := &mockCompletionClient{}
	client.On("GetCompletion", mock.Anything, mock.Anything).
		Return(assistantTextResponse("The user asked for a deploy. The agent shipped it successfully."), nil).
		Once()

	ti := newTestChatServiceWithCompletion(t, client)
	ctx := initSessionCtx(t, ti)
	chatID := seedChat(t, ctx, ti, "", "ext-user", "Deploy session")
	seedMessageContent(t, ctx, ti, chatID, "Please deploy the API to staging")

	first, err := ti.service.Summarize(ctx, &gen.SummarizePayload{ID: chatID.String()})
	require.NoError(t, err)
	require.False(t, first.Cached)
	require.Contains(t, first.Summary, "deploy")
	require.NotEmpty(t, first.SummaryGeneratedAt)

	// Second call should hit the cache and not invoke the model again.
	second, err := ti.service.Summarize(ctx, &gen.SummarizePayload{ID: chatID.String()})
	require.NoError(t, err)
	require.True(t, second.Cached)
	require.Equal(t, first.Summary, second.Summary)
	require.Equal(t, first.SummaryGeneratedAt, second.SummaryGeneratedAt)

	client.AssertExpectations(t)

	loaded, err := ti.service.LoadChat(ctx, &gen.LoadChatPayload{ID: chatID.String()})
	require.NoError(t, err)
	require.NotNil(t, loaded.Summary)
	require.Equal(t, first.Summary, *loaded.Summary)
	require.NotNil(t, loaded.SummaryGeneratedAt)
}

func TestService_Summarize_RegenerateOverwrites(t *testing.T) {
	t.Parallel()

	client := &mockCompletionClient{}
	client.On("GetCompletion", mock.Anything, mock.Anything).
		Return(assistantTextResponse("First summary."), nil).
		Once()
	client.On("GetCompletion", mock.Anything, mock.Anything).
		Return(assistantTextResponse("Second summary."), nil).
		Once()

	ti := newTestChatServiceWithCompletion(t, client)
	ctx := initSessionCtx(t, ti)
	chatID := seedChat(t, ctx, ti, "", "ext-user", "Regen session")
	seedMessageContent(t, ctx, ti, chatID, "Refactor the auth middleware")

	first, err := ti.service.Summarize(ctx, &gen.SummarizePayload{ID: chatID.String()})
	require.NoError(t, err)
	require.Equal(t, "First summary.", first.Summary)

	second, err := ti.service.Summarize(ctx, &gen.SummarizePayload{ID: chatID.String(), Regenerate: true})
	require.NoError(t, err)
	require.False(t, second.Cached)
	require.Equal(t, "Second summary.", second.Summary)

	client.AssertExpectations(t)

	stored, err := repo.New(ti.conn).GetChat(ctx, chatID)
	require.NoError(t, err)
	require.Equal(t, "Second summary.", stored.Summary.String)
}

func TestService_Summarize_EmptyTranscript(t *testing.T) {
	t.Parallel()

	client := &mockCompletionClient{}
	ti := newTestChatServiceWithCompletion(t, client)
	ctx := initSessionCtx(t, ti)
	chatID := seedChat(t, ctx, ti, "", "ext-user", "Empty session")

	_, err := ti.service.Summarize(ctx, &gen.SummarizePayload{ID: chatID.String()})
	requireOopsCode(t, err, oops.CodeBadRequest)
	client.AssertNotCalled(t, "GetCompletion", mock.Anything, mock.Anything)
}

func TestService_Summarize_MissingChat(t *testing.T) {
	t.Parallel()

	client := &mockCompletionClient{}
	ti := newTestChatServiceWithCompletion(t, client)
	ctx := initSessionCtx(t, ti)

	_, err := ti.service.Summarize(ctx, &gen.SummarizePayload{ID: uuid.NewString()})
	requireOopsCode(t, err, oops.CodeNotFound)
}

func TestService_Summarize_InvalidID(t *testing.T) {
	t.Parallel()

	client := &mockCompletionClient{}
	ti := newTestChatServiceWithCompletion(t, client)
	ctx := initSessionCtx(t, ti)

	_, err := ti.service.Summarize(ctx, &gen.SummarizePayload{ID: "not-a-uuid"})
	requireOopsCode(t, err, oops.CodeBadRequest)
}

func TestService_ListChats_ExposesPinned(t *testing.T) {
	t.Parallel()

	ti := newTestChatService(t)
	ctx := grantOrgAdminWithChatRead(t, initSessionCtx(t, ti))

	pinnedID := seedChat(t, ctx, ti, "", "ext-user", "Pinned")
	unpinnedID := seedChat(t, ctx, ti, "", "ext-user", "Unpinned")
	seedMessageContent(t, ctx, ti, pinnedID, "hello")
	seedMessageContent(t, ctx, ti, unpinnedID, "hello")

	require.NoError(t, ti.service.SetPinned(ctx, &gen.SetPinnedPayload{ID: pinnedID.String(), Pinned: true}))

	res, err := ti.service.ListChats(ctx, defaultPayload())
	require.NoError(t, err)

	byID := map[string]*gen.ChatOverview{}
	for _, c := range res.Chats {
		byID[c.ID] = c
	}
	require.NotNil(t, byID[pinnedID.String()].Pinned)
	require.True(t, *byID[pinnedID.String()].Pinned)
	require.NotNil(t, byID[unpinnedID.String()].Pinned)
	require.False(t, *byID[unpinnedID.String()].Pinned)
}
