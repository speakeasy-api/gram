package chat_test

import (
	"encoding/json"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/chat"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/openrouter"
)

func strptr(s string) *string { return &s }

func TestService_SummarizeToolActivity_Running(t *testing.T) {
	t.Parallel()

	client := &mockCompletionClient{}
	var captured openrouter.CompletionRequest
	client.On("GetCompletion", mock.Anything, mock.Anything).
		Run(func(args mock.Arguments) {
			captured, _ = args.Get(1).(openrouter.CompletionRequest)
		}).
		Return(assistantTextResponse("Investigating failing tool calls"), nil).
		Once()

	ti := newTestChatServiceWithCompletion(t, client)
	ctx := initSessionCtx(t, ti)

	res, err := ti.service.SummarizeToolActivity(ctx, &gen.SummarizeToolActivityPayload{
		UserMessage: strptr("Why are my tool calls failing?"),
		InProgress:  true,
		ToolCalls: []*gen.ToolActivityCall{
			{Name: "list_deployments", Arguments: nil},
			{Name: "get_deployment_logs", Arguments: strptr(`{"id":"abc"}`)},
		},
	})
	require.NoError(t, err)
	require.Equal(t, "Investigating failing tool calls", res.Summary)

	// The prompt (system + user) must actually carry the user's request and the
	// tool names — the feature hinges on buildToolActivityPrompt rendering them.
	require.Len(t, captured.Messages, 2)
	promptJSON, err := json.Marshal(captured.Messages)
	require.NoError(t, err)
	prompt := string(promptJSON)
	require.Contains(t, prompt, "Why are my tool calls failing?")
	require.Contains(t, prompt, "list_deployments")
	require.Contains(t, prompt, "get_deployment_logs")
	// Running turns must instruct the model to use the present tense.
	require.Contains(t, prompt, "present tense")
	require.NotContains(t, prompt, "past tense")
	client.AssertExpectations(t)
}

func TestService_SummarizeToolActivity_CompletedUsesPastTense(t *testing.T) {
	t.Parallel()

	client := &mockCompletionClient{}
	var captured openrouter.CompletionRequest
	client.On("GetCompletion", mock.Anything, mock.Anything).
		Run(func(args mock.Arguments) {
			captured, _ = args.Get(1).(openrouter.CompletionRequest)
		}).
		Return(assistantTextResponse("Searched the web for pricing"), nil).
		Once()

	ti := newTestChatServiceWithCompletion(t, client)
	ctx := initSessionCtx(t, ti)

	_, err := ti.service.SummarizeToolActivity(ctx, &gen.SummarizeToolActivityPayload{
		InProgress: false,
		ToolCalls:  []*gen.ToolActivityCall{{Name: "search_web"}},
	})
	require.NoError(t, err)

	promptJSON, err := json.Marshal(captured.Messages)
	require.NoError(t, err)
	prompt := string(promptJSON)
	require.Contains(t, prompt, "past tense")
	require.NotContains(t, prompt, "present tense")
	client.AssertExpectations(t)
}

func TestService_SummarizeToolActivity_BoundsLongOutput(t *testing.T) {
	t.Parallel()

	long := strings.Repeat("word ", 200)
	client := &mockCompletionClient{}
	client.On("GetCompletion", mock.Anything, mock.Anything).
		Return(assistantTextResponse(long), nil).
		Once()

	ti := newTestChatServiceWithCompletion(t, client)
	ctx := initSessionCtx(t, ti)

	res, err := ti.service.SummarizeToolActivity(ctx, &gen.SummarizeToolActivityPayload{
		InProgress: true,
		ToolCalls:  []*gen.ToolActivityCall{{Name: "search_web"}},
	})
	require.NoError(t, err)
	// A non-conforming, paragraph-length response is capped, not passed through.
	require.LessOrEqual(t, utf8.RuneCountInString(res.Summary), 120)
	require.NotEmpty(t, res.Summary)
	client.AssertExpectations(t)
}

func TestService_SummarizeToolActivity_SanitizesOutput(t *testing.T) {
	t.Parallel()

	client := &mockCompletionClient{}
	client.On("GetCompletion", mock.Anything, mock.Anything).
		Return(assistantTextResponse("\"Searched the web for pricing.\"\nExtra line"), nil).
		Once()

	ti := newTestChatServiceWithCompletion(t, client)
	ctx := initSessionCtx(t, ti)

	res, err := ti.service.SummarizeToolActivity(ctx, &gen.SummarizeToolActivityPayload{
		InProgress: false,
		ToolCalls: []*gen.ToolActivityCall{
			{Name: "search_web", Arguments: strptr(`{"q":"pricing"}`)},
		},
	})
	require.NoError(t, err)
	// Surrounding quotes, trailing period, and trailing lines are stripped.
	require.Equal(t, "Searched the web for pricing", res.Summary)
	client.AssertExpectations(t)
}

func TestService_SummarizeToolActivity_NoToolCalls(t *testing.T) {
	t.Parallel()

	client := &mockCompletionClient{}
	ti := newTestChatServiceWithCompletion(t, client)
	ctx := initSessionCtx(t, ti)

	_, err := ti.service.SummarizeToolActivity(ctx, &gen.SummarizeToolActivityPayload{
		ToolCalls: nil,
	})
	requireOopsCode(t, err, oops.CodeBadRequest)
	client.AssertNotCalled(t, "GetCompletion", mock.Anything, mock.Anything)
}

func TestService_SummarizeToolActivity_Unauthorized(t *testing.T) {
	t.Parallel()

	client := &mockCompletionClient{}
	ti := newTestChatServiceWithCompletion(t, client)

	// No auth context on the bare test context → unauthorized.
	_, err := ti.service.SummarizeToolActivity(t.Context(), &gen.SummarizeToolActivityPayload{
		ToolCalls: []*gen.ToolActivityCall{{Name: "search_web"}},
	})
	requireOopsCode(t, err, oops.CodeUnauthorized)
	client.AssertNotCalled(t, "GetCompletion", mock.Anything, mock.Anything)
}
