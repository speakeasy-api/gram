package relay

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/speakeasy-api/agenthooks"
	"github.com/speakeasy-api/agenthooks/agenthookstest"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/hooks/sdk/models/components"
)

// opencodeEnvelope decodes one embedded OpenCode fixture through a fresh
// runner and returns the envelope buildEnvelope produced. OpenCode fixtures
// decode to different typed events per hook (PromptEvent, ToolPreEvent, ...),
// so every handler funnels into the same captured envelope, mirroring
// TestEnvelopeCodexSkillInference's helper.
func opencodeEnvelope(t *testing.T, fixture string) components.IngestRequestBody {
	t.Helper()
	runner := agenthooks.New()
	var got components.IngestRequestBody
	runner.OnPromptSubmitted(func(_ context.Context, e *agenthooks.PromptEvent) (agenthooks.PromptDecision, error) {
		got = buildEnvelope(e, "test-host")
		return agenthooks.AcceptPrompt(), nil
	})
	runner.OnToolPre(func(_ context.Context, e *agenthooks.ToolPreEvent) (agenthooks.ToolPreDecision, error) {
		got = buildEnvelope(e, "test-host")
		return agenthooks.NoDecision(), nil
	})
	runner.OnToolPost(func(_ context.Context, e *agenthooks.ToolPostEvent) (agenthooks.ToolPostDecision, error) {
		got = buildEnvelope(e, "test-host")
		return agenthooks.Observed(), nil
	})
	runner.OnToolError(func(_ context.Context, e *agenthooks.ToolPostEvent) (agenthooks.ToolPostDecision, error) {
		got = buildEnvelope(e, "test-host")
		return agenthooks.Observed(), nil
	})
	runner.OnPermission(func(_ context.Context, e *agenthooks.PermissionEvent) (agenthooks.ToolPreDecision, error) {
		got = buildEnvelope(e, "test-host")
		return agenthooks.NoDecision(), nil
	})
	runner.OnStop(func(_ context.Context, e *agenthooks.StopEvent) (agenthooks.StopDecision, error) {
		got = buildEnvelope(e, "test-host")
		return agenthooks.Finish(), nil
	})

	payload := agenthookstest.Fixture(t, "opencode/"+fixture)
	agenthookstest.Invoke(t, runner, agenthooks.ProviderOpenCode, payload)
	return got
}

func TestOpenCodePromptSubmittedEnvelope(t *testing.T) {
	got := opencodeEnvelope(t, "chat_message.json")

	require.Equal(t, components.TypePromptSubmitted, got.Event.Type)
	require.Equal(t, "opencode", got.Source.Adapter)
	require.NotNil(t, got.Data)
	require.NotNil(t, got.Data.Prompt)
	require.Equal(t, "deploy to staging", *got.Data.Prompt.Text)
}

func TestOpenCodeToolRequestedEnvelope(t *testing.T) {
	got := opencodeEnvelope(t, "tool_execute_before.json")

	require.Equal(t, components.TypeToolRequested, got.Event.Type)
	require.Equal(t, "opencode", got.Source.Adapter)
	require.NotNil(t, got.Data)
	require.NotNil(t, got.Data.ToolCall)
	require.NotNil(t, got.Data.ToolCall.Name)
	require.Equal(t, "bash", *got.Data.ToolCall.Name)
	require.NotNil(t, got.Data.ToolCall.ID)
	require.Equal(t, "call-7", *got.Data.ToolCall.ID)
}

func TestOpenCodeToolCompletedEnvelope(t *testing.T) {
	got := opencodeEnvelope(t, "tool_execute_after.json")

	require.Equal(t, components.TypeToolCompleted, got.Event.Type)
	require.NotNil(t, got.Data.ToolCall)
	require.NotNil(t, got.Data.ToolCall.Output)
	outJSON, err := json.Marshal(got.Data.ToolCall.Output)
	require.NoError(t, err)
	require.Contains(t, string(outJSON), "all tests pass")
}

// TestOpenCodeToolFailedEnvelope covers the message.part.updated tool-error
// path: tool.execute.after never fires when a tool errors, so this bus frame
// is the only native failure signal (decodeOpenCodeToolError).
func TestOpenCodeToolFailedEnvelope(t *testing.T) {
	got := opencodeEnvelope(t, "message_part_updated_tool_error.json")

	require.Equal(t, components.TypeToolFailed, got.Event.Type)
	require.NotNil(t, got.Data.ToolCall)
	require.NotNil(t, got.Data.ToolCall.Name)
	require.Equal(t, "read", *got.Data.ToolCall.Name)
	require.NotNil(t, got.Data.ToolCall.Error)
	errJSON, err := json.Marshal(got.Data.ToolCall.Error)
	require.NoError(t, err)
	require.Contains(t, string(errJSON), "File not found")
}

// TestOpenCodePermissionRequestedEnvelope pins permissionTypeOf's OpenCode
// fallback (envelope.go): the permission frame names the field "type", not
// "permission_type".
func TestOpenCodePermissionRequestedEnvelope(t *testing.T) {
	got := opencodeEnvelope(t, "permission_asked.json")

	require.Equal(t, components.TypeToolRequested, got.Event.Type)
	require.NotNil(t, got.Data.ToolCall)
	require.NotNil(t, got.Data.ToolCall.PermissionType)
	require.Equal(t, "bash", *got.Data.ToolCall.PermissionType)
}

// TestOpenCodeStopCarriesUsage pins the agenthooks v0.4.0 behavior: the shim
// splices end-of-turn token/cost totals into session.idle alongside
// finalMessage, so StopEvent.Usage — and the envelope's data.usage — carries
// per-turn tokens and cost.
func TestOpenCodeStopCarriesUsage(t *testing.T) {
	got := opencodeEnvelope(t, "session_idle.json")

	require.Equal(t, components.TypeAssistantResponded, got.Event.Type)
	require.NotNil(t, got.Data)
	require.NotNil(t, got.Data.Message)
	require.Equal(t, "Deployed to staging.", *got.Data.Message.Text)
	require.NotNil(t, got.Data.Usage)
	require.Equal(t, int64(1200), *got.Data.Usage.InputTokens)
	require.Equal(t, int64(340), *got.Data.Usage.OutputTokens)
	require.Equal(t, int64(800), *got.Data.Usage.CacheReadTokens)
	require.Equal(t, int64(64), *got.Data.Usage.CacheWriteTokens)
	require.Equal(t, 0.0123, *got.Data.Usage.Cost)
}

// TestOpenCodeMCPIdentityRedactedAndAttributed pins MCP resolution for
// OpenCode: tool names carry no reserved MCP prefix (quirk #28 in
// mcpresolve.go), so identity and transport only attach via a
// config-matched name, and the resolved URL must still be redacted.
func TestOpenCodeMCPIdentityRedactedAndAttributed(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	cfgJSON, err := json.Marshal(map[string]any{
		"mcp": map[string]any{
			"linear": map[string]any{
				"url": "https://user:secret@mcp.example.com/sse?api_key=leaked&workspace=acme",
			},
		},
	})
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "opencode.json"), cfgJSON, 0o600))

	payload, err := json.Marshal(map[string]any{
		"seq":  3,
		"hook": "tool.execute.before",
		"input": map[string]any{
			"sessionID": "oc-sess-1",
			"callID":    "call-7",
			"tool":      "linear_create_issue",
			"directory": dir,
		},
		"output": map[string]any{
			"args": map[string]any{"title": "bug: flaky test"},
		},
	})
	require.NoError(t, err)

	runner := agenthooks.New()
	var got components.IngestRequestBody
	runner.OnToolPre(func(_ context.Context, e *agenthooks.ToolPreEvent) (agenthooks.ToolPreDecision, error) {
		got = buildEnvelope(e, "test-host")
		return agenthooks.NoDecision(), nil
	})
	agenthookstest.Invoke(t, runner, agenthooks.ProviderOpenCode, payload)

	require.Equal(t, components.TypeToolRequested, got.Event.Type)
	require.NotNil(t, got.Data.Mcp)
	require.NotNil(t, got.Data.Mcp.ServerName)
	require.Equal(t, "linear", *got.Data.Mcp.ServerName)
	require.NotNil(t, got.Data.Mcp.URL)
	require.Equal(t, "https://mcp.example.com/sse?api_key=%2A%2A%2A&workspace=acme", *got.Data.Mcp.URL)
	require.NotContains(t, *got.Data.Mcp.URL, "secret")
	require.NotContains(t, *got.Data.Mcp.URL, "leaked")
}

// TestOpenCodeDenyBlocksToolCall mirrors TestIngestDenyBlocksToolCall for the
// OpenCode shim's reply dialect: a server deny becomes the re-thrown "error"
// field the shim's Object.assign contract expects (encodeOpenCodeReply), not
// the Claude-style permissionDecision JSON.
func TestOpenCodeDenyBlocksToolCall(t *testing.T) {
	fs := newFakeServer(t, func(components.IngestRequestBody) (int, decision) {
		return http.StatusOK, decision{Decision: "deny", Reason: "policy_denied", Message: "blocked by policy X"}
	})
	cfg := authedConfig(t, fs.URL)
	res := invoke(t, cfg, agenthooks.ProviderOpenCode, "opencode/tool_execute_before.json")

	require.Equal(t, 0, res.ExitCode)
	require.Contains(t, string(res.Stdout), `"error":"blocked by policy X"`)
	require.Equal(t, 1, fs.count())
	require.Equal(t, components.TypeToolRequested, fs.last().Event.Type)
}
