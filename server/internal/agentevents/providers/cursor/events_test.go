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
	"github.com/speakeasy-api/gram/server/internal/message"
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
	assert.Equal(t, types.MCPToolCallStarted, eventType)

	resolvedToolName, ok, err := ev.String(types.FieldToolName)
	require.NoError(t, err)
	require.True(t, ok)
	assert.Equal(t, "MCP:list_issues", resolvedToolName)

	scannable, ok, err := ev.String(types.FieldScannableText)
	require.NoError(t, err)
	require.True(t, ok)
	assert.JSONEq(t, `{"limit":10}`, scannable)

	scanType, ok, err := ev.Any(types.FieldScanMessageType)
	require.NoError(t, err)
	require.True(t, ok)
	assert.Equal(t, message.ToolRequest, scanType)
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

	scannable, ok, err := ev.String(types.FieldScannableText)
	require.NoError(t, err)
	require.True(t, ok)
	assert.Equal(t, prompt, scannable)

	scanType, ok, err := ev.Any(types.FieldScanMessageType)
	require.NoError(t, err)
	require.True(t, ok)
	assert.Equal(t, message.User, scanType)
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
func ptr[T any](v T) *T {
	return new(v)
}
