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

func TestParseCopilotHookEvent(t *testing.T) {
	t.Parallel()

	tests := []struct {
		raw      string
		expected HookEvent
		ok       bool
	}{
		{raw: "sessionStart", expected: HookEventSessionStart, ok: true},
		{raw: "sessionEnd", expected: HookEventSessionEnd, ok: true},
		{raw: "userPromptSubmitted", expected: HookEventUserPromptSubmit, ok: true},
		{raw: "preToolUse", expected: HookEventPreToolUse, ok: true},
		{raw: "postToolUse", expected: HookEventPostToolUse, ok: true},
		{raw: "postToolUseFailure", expected: HookEventPostToolUseFailure, ok: true},
		{raw: "permissionRequest", expected: HookEventPermissionRequest, ok: true},
		{raw: "agentStop", expected: HookEventStop, ok: true},
		{raw: "subagentStop", expected: HookEventSubagentStop, ok: true},
		{raw: "notification", expected: HookEventNotification, ok: true},
		// Copilot events with no canonical equivalent stay unmapped so they
		// fall through to the canonical Event.Type.
		{raw: "preCompact", expected: HookEventUnknown, ok: false},
		{raw: "subagentStart", expected: HookEventUnknown, ok: false},
		{raw: "unknown", expected: HookEventUnknown, ok: false},
	}

	for _, tt := range tests {
		t.Run(tt.raw, func(t *testing.T) {
			t.Parallel()

			event, ok := parseCopilotHookEvent(tt.raw)
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
		// session.idle and message.part.updated fall through to the canonical
		// Event.Type (see parseOpencodeHookEvent) rather than being re-derived
		// from the raw name.
		{raw: "session.idle", expected: HookEventUnknown, ok: false},
		{raw: "server.instance.disposed", expected: HookEventSessionEnd, ok: true},
		{raw: "tool.execute.before", expected: HookEventPreToolUse, ok: true},
		{raw: "tool.execute.after", expected: HookEventPostToolUse, ok: true},
		{raw: "message.part.updated", expected: HookEventUnknown, ok: false},
		{raw: "chat.message", expected: HookEventUserPromptSubmit, ok: true},
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
