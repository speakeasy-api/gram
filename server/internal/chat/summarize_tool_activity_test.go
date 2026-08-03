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

//go:fix inline
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
		UserMessage: new("Why are my tool calls failing?"),
		InProgress:  true,
		ToolCalls: []*gen.ToolActivityCall{
			{Name: "list_deployments"},
			{Name: "get_deployment_logs"},
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

func TestService_SummarizeToolActivity_GenericToolFallsBackToUserRequest(t *testing.T) {
	t.Parallel()

	client := &mockCompletionClient{}
	var captured openrouter.CompletionRequest
	client.On("GetCompletion", mock.Anything, mock.Anything).
		Run(func(args mock.Arguments) {
			captured, _ = args.Get(1).(openrouter.CompletionRequest)
		}).
		Return(assistantTextResponse("Investigating the failing deploy"), nil).
		Once()

	ti := newTestChatServiceWithCompletion(t, client)
	ctx := initSessionCtx(t, ti)

	// A turn that only called a generic "compose" tool: the name is withheld
	// entirely — shown it, the model describes the mechanics ("Drafted the
	// message") instead of the user's task.
	_, err := ti.service.SummarizeToolActivity(ctx, &gen.SummarizeToolActivityPayload{
		UserMessage: new("Why did my deploy fail?"),
		InProgress:  true,
		ToolCalls:   []*gen.ToolActivityCall{{Name: "compose"}},
	})
	require.NoError(t, err)

	promptJSON, err := json.Marshal(captured.Messages)
	require.NoError(t, err)
	prompt := string(promptJSON)
	require.Contains(t, prompt, "Why did my deploy fail?")
	require.NotContains(t, prompt, "compose")
	client.AssertExpectations(t)
}

func TestService_SummarizeToolActivity_GenericToolsDroppedFromToolList(t *testing.T) {
	t.Parallel()

	client := &mockCompletionClient{}
	var captured openrouter.CompletionRequest
	client.On("GetCompletion", mock.Anything, mock.Anything).
		Run(func(args mock.Arguments) {
			captured, _ = args.Get(1).(openrouter.CompletionRequest)
		}).
		Return(assistantTextResponse("Aggregating failures and finding patterns"), nil).
		Once()

	ti := newTestChatServiceWithCompletion(t, client)
	ctx := initSessionCtx(t, ti)

	_, err := ti.service.SummarizeToolActivity(ctx, &gen.SummarizeToolActivityPayload{
		InProgress: true,
		ToolCalls: []*gen.ToolActivityCall{
			{Name: "compose"},
			{Name: "query_tool_errors"},
		},
	})
	require.NoError(t, err)

	promptJSON, err := json.Marshal(captured.Messages)
	require.NoError(t, err)
	prompt := string(promptJSON)
	require.Contains(t, prompt, "query_tool_errors")
	require.NotContains(t, prompt, "compose")
	client.AssertExpectations(t)
}

func TestService_SummarizeToolActivity_GenericToolWithoutUserMessage(t *testing.T) {
	t.Parallel()

	client := &mockCompletionClient{}
	ti := newTestChatServiceWithCompletion(t, client)
	ctx := initSessionCtx(t, ti)

	// Nothing informative to summarize: no request to describe and no tool name
	// worth showing. Fail fast so the client keeps its heuristic label.
	_, err := ti.service.SummarizeToolActivity(ctx, &gen.SummarizeToolActivityPayload{
		InProgress: true,
		ToolCalls:  []*gen.ToolActivityCall{{Name: "compose"}},
	})
	requireOopsCode(t, err, oops.CodeBadRequest)
	client.AssertNotCalled(t, "GetCompletion", mock.Anything, mock.Anything)
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
			{Name: "search_web"},
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
