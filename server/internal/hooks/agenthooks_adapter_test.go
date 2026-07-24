package hooks

import (
	"strings"
	"testing"
	"time"

	"github.com/speakeasy-api/agenthooks"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/hooks"
)

var adapterTestTime = time.Date(2026, 7, 24, 15, 4, 5, 0, time.UTC)

func TestAgenthooksTypedEvent_PromptSubmitted(t *testing.T) {
	t.Parallel()

	payload := canonicalIngestPayload("claude", "prompt.submitted", "sess-prompt-1")
	payload.Source.RawEventName = new("UserPromptSubmit")
	payload.Source.UserEmail = new("dev@example.com")
	payload.Session.TurnID = new("turn-42")
	payload.Session.Cwd = new("/home/dev/proj")
	payload.Session.Model = new("claude-sonnet-4-5")
	payload.Data = &gen.HookIngestData{
		Prompt: &gen.HookPromptData{Text: new("delete all customer rows")},
	}
	payload.Raw = map[string]any{
		"hook_event_name": "UserPromptSubmit",
		"prompt":          "delete all customer rows",
		"session_id":      "sess-prompt-1",
	}

	typed := agenthooksTypedEvent(payload, adapterTestTime)
	ev, ok := typed.(*agenthooks.PromptEvent)
	require.True(t, ok, "prompt.submitted must map to *agenthooks.PromptEvent, got %T", typed)

	require.Equal(t, agenthooks.ProviderClaudeCode, ev.Provider)
	require.Equal(t, agenthooks.KindPromptSubmitted, ev.Kind)
	require.Equal(t, "UserPromptSubmit", ev.NativeName)
	require.True(t, ev.Time.Equal(adapterTestTime))
	require.Equal(t, "delete all customer rows", ev.Prompt)

	require.Equal(t, "sess-prompt-1", ev.Session.ID)
	require.Equal(t, "turn-42", ev.Session.TurnID)
	require.Equal(t, "/home/dev/proj", ev.Session.CWD)
	require.Equal(t, "claude-sonnet-4-5", ev.Session.Model)
	require.Equal(t, "dev@example.com", ev.Session.UserEmail)

	require.JSONEq(t,
		`{"hook_event_name":"UserPromptSubmit","prompt":"delete all customer rows","session_id":"sess-prompt-1"}`,
		string(ev.Raw))
	// The raw payload keeps RawField fidelity for policies needing more than
	// the normalized projection.
	require.Equal(t, `"UserPromptSubmit"`, string(ev.RawField("hook_event_name")))
}

func TestAgenthooksTypedEvent_ToolRequested(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		payload func() *gen.IngestPayload
		check   func(t *testing.T, typed any)
	}{
		{
			name: "plain tool with native id",
			payload: func() *gen.IngestPayload {
				payload := canonicalIngestPayload("claude", "tool.requested", "sess-tool-1")
				payload.Source.RawEventName = new("PreToolUse")
				payload.Data = &gen.HookIngestData{
					ToolCall: &gen.HookToolCallData{
						ID:    new("toolu_01ABC"),
						Name:  new("Bash"),
						Input: map[string]any{"command": "ls -la"},
					},
				}
				return payload
			},
			check: func(t *testing.T, typed any) {
				ev, ok := typed.(*agenthooks.ToolPreEvent)
				require.True(t, ok, "expected *agenthooks.ToolPreEvent, got %T", typed)
				require.Equal(t, agenthooks.KindToolPre, ev.Kind)
				require.Equal(t, "PreToolUse", ev.NativeName)
				require.Equal(t, "toolu_01ABC", ev.Tool.ID)
				require.False(t, ev.Tool.Synthesized)
				require.Equal(t, "Bash", ev.Tool.Name)
				require.Equal(t, agenthooks.ToolShell, ev.Tool.Canonical)
				require.Nil(t, ev.Tool.MCP)
				require.JSONEq(t, `{"command":"ls -la"}`, string(ev.Tool.Input))
				require.JSONEq(t, `{"command":"ls -la"}`, string(ev.Tool.RawInput))
			},
		},
		{
			name: "missing tool id is synthesized the way the library does",
			payload: func() *gen.IngestPayload {
				payload := canonicalIngestPayload("codex", "tool.requested", "sess-tool-2")
				payload.Session.TurnID = new("turn-7")
				payload.Data = &gen.HookIngestData{
					ToolCall: &gen.HookToolCallData{
						Name:  new("shell"),
						Input: map[string]any{"command": "rm -rf /tmp/x"},
					},
				}
				return payload
			},
			check: func(t *testing.T, typed any) {
				ev, ok := typed.(*agenthooks.ToolPreEvent)
				require.True(t, ok, "expected *agenthooks.ToolPreEvent, got %T", typed)
				require.True(t, ev.Tool.Synthesized)
				require.True(t, strings.HasPrefix(ev.Tool.ID, "hook_synth_"), "got id %q", ev.Tool.ID)
				// The synthesis inputs are session id, turn id, tool name, and
				// the normalized input — pin the wiring via the pure helper.
				require.Equal(t,
					agenthooks.SynthesizeToolID("sess-tool-2", "turn-7", "shell", ev.Tool.Input),
					ev.Tool.ID)
			},
		},
		{
			name: "mcp tool from claude name dialect without transport info",
			payload: func() *gen.IngestPayload {
				payload := canonicalIngestPayload("claude", "tool.requested", "sess-mcp-1")
				payload.Source.RawEventName = new("PreToolUse")
				payload.Data = &gen.HookIngestData{
					ToolCall: &gen.HookToolCallData{
						ID:    new("toolu_02DEF"),
						Name:  new("mcp__github__create_issue"),
						Input: map[string]any{"title": "bug report"},
					},
				}
				return payload
			},
			check: func(t *testing.T, typed any) {
				ev, ok := typed.(*agenthooks.ToolPreEvent)
				require.True(t, ok, "expected *agenthooks.ToolPreEvent, got %T", typed)
				require.Equal(t, agenthooks.ToolMCP, ev.Tool.Canonical)
				require.NotNil(t, ev.Tool.MCP)
				require.Equal(t, "github", ev.Tool.MCP.Server)
				require.Equal(t, "create_issue", ev.Tool.MCP.Tool)
				require.Empty(t, ev.Tool.MCP.URL)
				require.Empty(t, ev.Tool.MCP.Command)
			},
		},
		{
			name: "mcp tool with url transport and cursor stringified input",
			payload: func() *gen.IngestPayload {
				payload := canonicalIngestPayload("cursor", "tool.requested", "sess-mcp-2")
				payload.Source.RawEventName = new("beforeMCPExecution")
				payload.Data = &gen.HookIngestData{
					ToolCall: &gen.HookToolCallData{
						Name: new("MCP:add"),
						// Cursor ships tool input as a JSON-encoded string;
						// the relay passes it through on the wire.
						Input: `{"a":1,"b":2}`,
					},
					Mcp: &gen.HookMCPData{
						ServerName:     new("adder"),
						ServerIdentity: new("mcp.example.test"),
						URL:            new("https://mcp.example.test/sse"),
					},
				}
				return payload
			},
			check: func(t *testing.T, typed any) {
				ev, ok := typed.(*agenthooks.ToolPreEvent)
				require.True(t, ok, "expected *agenthooks.ToolPreEvent, got %T", typed)
				require.Equal(t, agenthooks.ToolMCP, ev.Tool.Canonical)
				require.NotNil(t, ev.Tool.MCP)
				// The envelope's server name wins over the name-dialect decode
				// (MCP:add carries no server segment).
				require.Equal(t, "adder", ev.Tool.MCP.Server)
				require.Equal(t, "add", ev.Tool.MCP.Tool)
				require.Equal(t, "https://mcp.example.test/sse", ev.Tool.MCP.URL)
				require.Empty(t, ev.Tool.MCP.Command)
				// Input is un-stringified to an object; RawInput keeps the
				// JSON-string form the provider sent.
				require.JSONEq(t, `{"a":1,"b":2}`, string(ev.Tool.Input))
				require.JSONEq(t, `"{\"a\":1,\"b\":2}"`, string(ev.Tool.RawInput))
				// Cursor MCP calls carry no native id: the adapter synthesizes
				// one like the library does.
				require.True(t, ev.Tool.Synthesized)
			},
		},
		{
			name: "mcp block with command transport forces mcp on a bare tool name",
			payload: func() *gen.IngestPayload {
				payload := canonicalIngestPayload("openclaw", "tool.requested", "sess-mcp-3")
				payload.Data = &gen.HookIngestData{
					ToolCall: &gen.HookToolCallData{
						ID:    new("call-77"),
						Name:  new("query"),
						Input: map[string]any{"sql": "select 1"},
					},
					Mcp: &gen.HookMCPData{
						ServerName: new("sqlite"),
						Command:    new("npx -y mcp-sqlite ./db.sqlite"),
					},
				}
				return payload
			},
			check: func(t *testing.T, typed any) {
				ev, ok := typed.(*agenthooks.ToolPreEvent)
				require.True(t, ok, "expected *agenthooks.ToolPreEvent, got %T", typed)
				require.Equal(t, agenthooks.ToolMCP, ev.Tool.Canonical)
				require.NotNil(t, ev.Tool.MCP)
				require.Equal(t, "sqlite", ev.Tool.MCP.Server)
				require.Equal(t, "query", ev.Tool.MCP.Tool)
				require.Equal(t, "npx -y mcp-sqlite ./db.sqlite", ev.Tool.MCP.Command)
				require.Empty(t, ev.Tool.MCP.URL)
			},
		},
		{
			name: "permission_type routes to PermissionEvent",
			payload: func() *gen.IngestPayload {
				payload := canonicalIngestPayload("codex", "tool.requested", "sess-perm-1")
				payload.Source.RawEventName = new("PermissionRequest")
				payload.Data = &gen.HookIngestData{
					ToolCall: &gen.HookToolCallData{
						ID:             new("call-88"),
						Name:           new("shell"),
						Input:          map[string]any{"command": "git push"},
						PermissionType: new("approval"),
					},
				}
				payload.Raw = map[string]any{
					"hook_event_name": "PermissionRequest",
					"permission_type": "approval",
				}
				return payload
			},
			check: func(t *testing.T, typed any) {
				ev, ok := typed.(*agenthooks.PermissionEvent)
				require.True(t, ok, "expected *agenthooks.PermissionEvent, got %T", typed)
				require.Equal(t, agenthooks.KindPermission, ev.Kind)
				require.Equal(t, agenthooks.ProviderCodex, ev.Provider)
				require.Equal(t, "PermissionRequest", ev.NativeName)
				require.Equal(t, "shell", ev.Tool.Name)
				require.Equal(t, agenthooks.ToolShell, ev.Tool.Canonical)
				// permission_type itself stays reachable the library way, via
				// the verbatim raw payload.
				require.Equal(t, `"approval"`, string(ev.RawField("permission_type")))
			},
		},
		{
			name: "blank permission_type stays ToolPreEvent",
			payload: func() *gen.IngestPayload {
				payload := canonicalIngestPayload("codex", "tool.requested", "sess-perm-2")
				payload.Data = &gen.HookIngestData{
					ToolCall: &gen.HookToolCallData{
						ID:             new("call-99"),
						Name:           new("shell"),
						Input:          map[string]any{"command": "ls"},
						PermissionType: new("   "),
					},
				}
				return payload
			},
			check: func(t *testing.T, typed any) {
				_, ok := typed.(*agenthooks.ToolPreEvent)
				require.True(t, ok, "expected *agenthooks.ToolPreEvent, got %T", typed)
			},
		},
		{
			name: "permission_type on an mcp tool keeps the mcp identity",
			payload: func() *gen.IngestPayload {
				payload := canonicalIngestPayload("claude", "tool.requested", "sess-perm-3")
				payload.Data = &gen.HookIngestData{
					ToolCall: &gen.HookToolCallData{
						ID:             new("toolu_03GHI"),
						Name:           new("mcp__linear__create_ticket"),
						Input:          map[string]any{"title": "follow-up"},
						PermissionType: new("tool_use"),
					},
					Mcp: &gen.HookMCPData{
						ServerName: new("linear"),
						URL:        new("https://mcp.linear.test/sse"),
					},
				}
				return payload
			},
			check: func(t *testing.T, typed any) {
				ev, ok := typed.(*agenthooks.PermissionEvent)
				require.True(t, ok, "expected *agenthooks.PermissionEvent, got %T", typed)
				require.Equal(t, agenthooks.ToolMCP, ev.Tool.Canonical)
				require.NotNil(t, ev.Tool.MCP)
				require.Equal(t, "linear", ev.Tool.MCP.Server)
				require.Equal(t, "create_ticket", ev.Tool.MCP.Tool)
				require.Equal(t, "https://mcp.linear.test/sse", ev.Tool.MCP.URL)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			tt.check(t, agenthooksTypedEvent(tt.payload(), adapterTestTime))
		})
	}
}

func TestAgenthooksTypedEvent_NonDecisionTypesReturnNil(t *testing.T) {
	t.Parallel()

	for _, eventType := range []string{
		"session.started",
		"session.updated",
		"session.ended",
		"tool.completed",
		"tool.failed",
		"assistant.responded",
		"assistant.thought",
		"usage.reported",
		"notification.reported",
		"skill.activated",
	} {
		payload := canonicalIngestPayload("claude", eventType, "sess-nil")
		require.Nil(t, agenthooksTypedEvent(payload, adapterTestTime),
			"event type %q must not map to a typed decision event", eventType)
	}
}

func TestAgenthooksTypedEvent_MissingBlocksStayZeroValued(t *testing.T) {
	t.Parallel()

	payload := canonicalIngestPayload("cursor", "tool.requested", "")
	payload.Session = nil

	typed := agenthooksTypedEvent(payload, adapterTestTime)
	ev, ok := typed.(*agenthooks.ToolPreEvent)
	require.True(t, ok, "expected *agenthooks.ToolPreEvent, got %T", typed)

	require.Equal(t, agenthooks.ProviderCursor, ev.Provider)
	require.Empty(t, ev.NativeName)
	require.Empty(t, ev.Session.ID)
	require.Empty(t, ev.Session.UserEmail)
	require.Nil(t, ev.Raw)

	require.Empty(t, ev.Tool.Name)
	require.Equal(t, agenthooks.ToolOther, ev.Tool.Canonical)
	require.Nil(t, ev.Tool.MCP)
	// The Input-is-always-an-object invariant holds even with no tool_call
	// block, and RawInput stays nil ("provider sent none").
	require.JSONEq(t, `{}`, string(ev.Tool.Input))
	require.Nil(t, ev.Tool.RawInput)
	require.True(t, ev.Tool.Synthesized)
}

// The adapter's slug reverse-mapping must mirror the relay's adapterSlug
// (hooks/relay/envelope.go): claude is the only slug that differs from its
// Provider value.
func TestAgenthooksProvider_ReversesRelayAdapterSlug(t *testing.T) {
	t.Parallel()

	require.Equal(t, agenthooks.ProviderClaudeCode, agenthooksProvider("claude"))
	require.Equal(t, agenthooks.ProviderCursor, agenthooksProvider("cursor"))
	require.Equal(t, agenthooks.ProviderCodex, agenthooksProvider("codex"))
	require.Equal(t, agenthooks.ProviderGemini, agenthooksProvider("gemini"))
	require.Equal(t, agenthooks.ProviderOpenCode, agenthooksProvider("opencode"))
	require.Equal(t, agenthooks.ProviderKimi, agenthooksProvider("kimi-code"))
	// Custom adapter slugs pass through as their own Provider value.
	require.Equal(t, agenthooks.Provider("openclaw"), agenthooksProvider("openclaw"))
	require.Equal(t, agenthooks.ProviderClaudeCode, agenthooksProvider("  claude  "))
}
