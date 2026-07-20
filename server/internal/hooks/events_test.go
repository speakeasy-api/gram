package hooks

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestParseClaudeHookEvent(t *testing.T) {
	t.Parallel()

	tests := []struct {
		raw      string
		expected HookEvent
		ok       bool
	}{
		{raw: "PreToolUse", expected: HookEventPreToolUse, ok: true},
		{raw: "UserPromptSubmit", expected: HookEventUserPromptSubmit, ok: true},
		{raw: "unknown", expected: HookEventUnknown, ok: false},
	}

	for _, tt := range tests {
		t.Run(tt.raw, func(t *testing.T) {
			t.Parallel()

			event, ok := parseClaudeHookEvent(tt.raw)
			assert.Equal(t, tt.expected, event)
			assert.Equal(t, tt.ok, ok)
		})
	}
}

func TestParseCodexHookEvent(t *testing.T) {
	t.Parallel()

	tests := []struct {
		raw      string
		expected HookEvent
		ok       bool
	}{
		{raw: "PermissionRequest", expected: HookEventPermissionRequest, ok: true},
		{raw: "Stop", expected: HookEventStop, ok: true},
		{raw: "unknown", expected: HookEventUnknown, ok: false},
	}

	for _, tt := range tests {
		t.Run(tt.raw, func(t *testing.T) {
			t.Parallel()

			event, ok := parseCodexHookEvent(tt.raw)
			assert.Equal(t, tt.expected, event)
			assert.Equal(t, tt.ok, ok)
		})
	}
}

func TestParseCursorHookEvent(t *testing.T) {
	t.Parallel()

	tests := []struct {
		raw      string
		expected HookEvent
		ok       bool
	}{
		{raw: "beforeSubmitPrompt", expected: HookEventBeforeSubmitPrompt, ok: true},
		{raw: "afterMCPExecution", expected: HookEventAfterMCPExecution, ok: true},
		{raw: "unknown", expected: HookEventUnknown, ok: false},
	}

	for _, tt := range tests {
		t.Run(tt.raw, func(t *testing.T) {
			t.Parallel()

			event, ok := parseCursorHookEvent(tt.raw)
			assert.Equal(t, tt.expected, event)
			assert.Equal(t, tt.ok, ok)
		})
	}
}

func TestParseOpencodeHookEvent(t *testing.T) {
	t.Parallel()

	tests := []struct {
		raw      string
		expected HookEvent
		ok       bool
	}{
		{raw: "session.created", expected: HookEventSessionStart, ok: true},
		{raw: "session.idle", expected: HookEventSessionEnd, ok: true},
		{raw: "session.deleted", expected: HookEventSessionEnd, ok: true},
		{raw: "tool.execute.before", expected: HookEventPreToolUse, ok: true},
		{raw: "tool.execute.after", expected: HookEventPostToolUse, ok: true},
		{raw: "tool.execute.error", expected: HookEventPostToolUseFailure, ok: true},
		{raw: "message.submitted", expected: HookEventUserPromptSubmit, ok: true},
		{raw: "message.completed", expected: HookEventAfterAgentResponse, ok: true},
		{raw: "permission.asked", expected: HookEventPermissionRequest, ok: true},
		{raw: "unknown", expected: HookEventUnknown, ok: false},
	}

	for _, tt := range tests {
		t.Run(tt.raw, func(t *testing.T) {
			t.Parallel()

			event, ok := parseOpencodeHookEvent(tt.raw)
			assert.Equal(t, tt.expected, event)
			assert.Equal(t, tt.ok, ok)
		})
	}
}
