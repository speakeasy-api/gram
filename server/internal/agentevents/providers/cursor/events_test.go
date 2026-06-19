package cursor_test

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/hooks"
	"github.com/speakeasy-api/gram/server/internal/agentevents"
	cursoragent "github.com/speakeasy-api/gram/server/internal/agentevents/providers/cursor"
	"github.com/speakeasy-api/gram/server/internal/agentevents/types"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
)

func TestParseHookEventType(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		raw      string
		expected types.HookEventType
		ok       bool
	}{
		{raw: "beforeSubmitPrompt", expected: cursoragent.HookEventBeforeSubmitPrompt, ok: true},
		{raw: "afterAgentResponse", expected: cursoragent.HookEventAfterAgentResponse, ok: true},
		{raw: "preToolUse", expected: cursoragent.HookEventPreToolUse, ok: true},
		{raw: "beforeMCPExecution", expected: cursoragent.HookEventBeforeMCPExecution, ok: true},
		{raw: "postToolUse", expected: cursoragent.HookEventPostToolUse, ok: true},
		{raw: "afterMCPExecution", expected: cursoragent.HookEventAfterMCPExecution, ok: true},
		{raw: "postToolUseFailure", expected: cursoragent.HookEventPostToolUseFailure, ok: true},
		{raw: "stop", expected: cursoragent.HookEventStop, ok: true},
		{raw: "afterAgentThought", ok: false},
		{raw: "unknown", ok: false},
	}

	for _, tt := range testCases {
		eventType, ok := cursoragent.ParseHookEventType(tt.raw)
		require.Equal(t, tt.ok, ok, tt.raw)
		assert.Equal(t, tt.expected, eventType)
	}
}

func TestCursorProviderResolvesRiskScanFields(t *testing.T) {
	t.Parallel()

	agent := newCursorAgent(t)
	toolName := "MCP:list_issues"
	payload := &gen.CursorPayload{
		HookEventName: "beforeMCPExecution",
		ToolName:      &toolName,
		ToolInput:     map[string]any{"limit": 10},
	}
	ev := agent.NewEvent(testContext(), payload, time.Now())

	eventType, ok, err := ev.EventType()
	require.NoError(t, err)
	require.True(t, ok)
	assert.Equal(t, types.BeforeMCPExecution, eventType)

	resolvedToolName, ok, err := ev.String(eventType, types.FieldToolName)
	require.NoError(t, err)
	require.True(t, ok)
	assert.Equal(t, "MCP:list_issues", resolvedToolName)

	toolInput, ok, err := ev.Any(eventType, types.FieldToolInput)
	require.NoError(t, err)
	require.True(t, ok)
	assert.Equal(t, map[string]any{"limit": 10}, toolInput)
}

func TestCursorProviderResolvesToolOutput(t *testing.T) {
	t.Parallel()

	agent := newCursorAgent(t)
	toolOutput := map[string]any{"status": "ok"}
	payload := &gen.CursorPayload{
		HookEventName: "postToolUse",
		ToolResponse:  toolOutput,
	}
	ev := agent.NewEvent(testContext(), payload, time.Now())

	eventType, ok, err := ev.EventType()
	require.NoError(t, err)
	require.True(t, ok)
	assert.Equal(t, types.AfterToolUse, eventType)

	resolvedOutput, ok, err := ev.Any(eventType, types.FieldToolOutput)
	require.NoError(t, err)
	require.True(t, ok)
	assert.Equal(t, toolOutput, resolvedOutput)
}

func TestCursorProviderResolvesPromptScanFields(t *testing.T) {
	t.Parallel()

	agent := newCursorAgent(t)
	prompt := "summarize this"
	payload := &gen.CursorPayload{
		HookEventName: "beforeSubmitPrompt",
		Prompt:        &prompt,
	}
	ev := agent.NewEvent(testContext(), payload, time.Now())

	eventType, ok, err := ev.EventType()
	require.NoError(t, err)
	require.True(t, ok)
	assert.Equal(t, types.UserPromptSubmit, eventType)

	resolvedPrompt, ok, err := ev.String(eventType, types.FieldPrompt)
	require.NoError(t, err)
	require.True(t, ok)
	assert.Equal(t, prompt, resolvedPrompt)
}

func newCursorAgent(t *testing.T) *agentevents.Agent[*gen.CursorPayload] {
	t.Helper()

	agent, err := cursoragent.NewAgent()
	require.NoError(t, err)
	return agent
}

func testContext() *contextvalues.AuthContext {
	projectID := uuid.MustParse("22222222-2222-2222-2222-222222222222")
	return &contextvalues.AuthContext{
		ActiveOrganizationID: "org",
		ProjectID:            &projectID,
		UserID:               "user",
		Email:                new("dev@example.com"),
	}
}

//go:fix inline
//go:fix inline
func ptr[T any](v T) *T {
	return new(v)
}
