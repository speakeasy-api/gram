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
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/billing"
	"github.com/speakeasy-api/gram/server/internal/chat/repo"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
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

	stored, err := repo.New(ti.conn).GetChat(ctx, repo.GetChatParams{ID: chatID, ProjectID: ti.projectID})
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
	ctx := grantOrgAdminWithChatWrite(t, initSessionCtx(t, ti))

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

func (m *mockCompletionClient) ResolveKey(_ context.Context, _ string, _ string, _ billing.ModelUsageSource, _ openrouter.KeyType) (openrouter.ResolvedKey, error) {
	return openrouter.PlatformKey(), nil
}

// A summary is transcript content in prose, so summarizing bypasses the exact
// disclosure chat.load blocks unless it enforces the same gate. The mock has no
// GetCompletion expectation registered, so reaching the model fails the test —
// proving the transcript is never read.
func TestService_Summarize_MemberCannotSummarizeAnothersChat(t *testing.T) {
	t.Parallel()
	client := &mockCompletionClient{}
	ti := newTestChatServiceWithCompletion(t, client)
	seedCtx := initSessionCtx(t, ti)

	victim := seedChat(t, seedCtx, ti, "some-other-user", "", "their session")
	seedMessageContent(t, seedCtx, ti, victim, "rotate the production credentials")

	memberCtx, _ := memberSessionCtx(t, ti)
	_, err := ti.service.Summarize(memberCtx, &gen.SummarizePayload{ID: victim.String()})
	requireOopsCode(t, err, oops.CodeForbidden)
	client.AssertNotCalled(t, "GetCompletion", mock.Anything, mock.Anything)
}

// The cached path returns a stored summary without calling the model, and is
// just as much a disclosure — it must be gated identically.
func TestService_Summarize_CachedSummaryStillGated(t *testing.T) {
	t.Parallel()
	client := &mockCompletionClient{}
	client.On("GetCompletion", mock.Anything, mock.Anything).
		Return(assistantTextResponse("Cached summary."), nil).
		Once()

	ti := newTestChatServiceWithCompletion(t, client)
	ownerCtx := grantOrgAdminWithChatRead(t, initSessionCtx(t, ti))

	victim := seedChat(t, ownerCtx, ti, "some-other-user", "", "their session")
	seedMessageContent(t, ownerCtx, ti, victim, "rotate the production credentials")

	// Populate the cache as a chat:read holder.
	first, err := ti.service.Summarize(ownerCtx, &gen.SummarizePayload{ID: victim.String()})
	require.NoError(t, err)
	require.False(t, first.Cached)

	memberCtx, _ := memberSessionCtx(t, ti)
	_, err = ti.service.Summarize(memberCtx, &gen.SummarizePayload{ID: victim.String()})
	requireOopsCode(t, err, oops.CodeForbidden)

	client.AssertExpectations(t)
}

// Handing a reviewer a session's contents is a session access, so it belongs in
// the same audit trail chat.load writes — otherwise the log claims a session
// was never read when its substance was returned.
func TestService_Summarize_AuditsSessionAccess(t *testing.T) {
	t.Parallel()
	client := &mockCompletionClient{}
	client.On("GetCompletion", mock.Anything, mock.Anything).
		Return(assistantTextResponse("A summary."), nil).
		Once()

	ti := newTestChatServiceWithCompletion(t, client)
	ctx := grantOrgAdminWithChatRead(t, initSessionCtx(t, ti))

	chatID := seedChat(t, ctx, ti, "some-other-user", "", "Audited session")
	seedMessageContent(t, ctx, ti, chatID, "deploy the service")

	before, err := audittest.AuditLogCountByAction(t.Context(), ti.conn, audit.ActionChatSessionAccess)
	require.NoError(t, err)

	_, err = ti.service.Summarize(ctx, &gen.SummarizePayload{ID: chatID.String()})
	require.NoError(t, err)

	after, err := audittest.AuditLogCountByAction(t.Context(), ti.conn, audit.ActionChatSessionAccess)
	require.NoError(t, err)
	require.Equal(t, before+1, after, "summarizing a session records an access audit event")

	rec, err := audittest.LatestAuditLogByAction(t.Context(), ti.conn, audit.ActionChatSessionAccess)
	require.NoError(t, err)
	require.Equal(t, "chat_session", rec.SubjectType)
	require.Equal(t, "Audited session", rec.SubjectDisplay)
}

// The impersonation block lives in the shared gate, so it covers summarize too.
// Without it, an impersonating admin blocked from opening a transcript could
// read the same substance back as prose.
func TestService_Summarize_ImpersonatingAdminBlocked(t *testing.T) {
	t.Parallel()
	client := &mockCompletionClient{}
	ti := newTestChatServiceWithCompletion(t, client)
	ctx := grantOrgAdminWithChatRead(t, initSessionCtx(t, ti))

	chatID := seedChat(t, ctx, ti, "some-other-user", "", "Customer session")
	seedMessageContent(t, ctx, ti, chatID, "rotate the production credentials")

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	authCtx.IsAdmin = true
	impersonating := contextvalues.SetAdminOverrideInContext(
		contextvalues.SetAuthContext(ctx, authCtx), authCtx.ActiveOrganizationID)

	_, err := ti.service.Summarize(impersonating, &gen.SummarizePayload{ID: chatID.String()})
	requireOopsCode(t, err, oops.CodeForbidden)
	client.AssertNotCalled(t, "GetCompletion", mock.Anything, mock.Anything)
}

// Regenerating discards a stored summary and bills a fresh model call, so it
// takes chat:write even though plain summarizing is a read. A reviewer holding
// only chat:read keeps the cached read and loses the overwrite.
func TestService_Summarize_RegenerateRequiresChatWrite(t *testing.T) {
	t.Parallel()
	client := &mockCompletionClient{}
	client.On("GetCompletion", mock.Anything, mock.Anything).
		Return(assistantTextResponse("Original summary."), nil).
		Once()

	ti := newTestChatServiceWithCompletion(t, client)
	writeCtx := grantOrgAdminWithChatWrite(t, initSessionCtx(t, ti))

	victim := seedChat(t, writeCtx, ti, "some-other-user", "", "their session")
	seedMessageContent(t, writeCtx, ti, victim, "rotate the production credentials")

	first, err := ti.service.Summarize(writeCtx, &gen.SummarizePayload{ID: victim.String()})
	require.NoError(t, err)
	require.Equal(t, "Original summary.", first.Summary)

	readCtx := grantOrgAdminWithChatRead(t, initSessionCtx(t, ti))

	// The cached read still works for a chat:read holder.
	cached, err := ti.service.Summarize(readCtx, &gen.SummarizePayload{ID: victim.String()})
	require.NoError(t, err)
	require.True(t, cached.Cached)
	require.Equal(t, "Original summary.", cached.Summary)

	// Forcing a regeneration does not.
	_, err = ti.service.Summarize(readCtx, &gen.SummarizePayload{ID: victim.String(), Regenerate: true})
	requireOopsCode(t, err, oops.CodeForbidden)

	// The stored summary is untouched and no second model call was made.
	stored, err := repo.New(ti.conn).GetChat(writeCtx, repo.GetChatParams{ID: victim, ProjectID: ti.projectID})
	require.NoError(t, err)
	require.Equal(t, "Original summary.", stored.Summary.String)
	client.AssertExpectations(t)
}
