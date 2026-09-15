package relay

import (
	"bytes"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/speakeasy-api/agenthooks"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/hooks/sdk/models/components"
)

// piFrameJSON builds one NDJSON request frame the way the generated extension
// writes it.
func piFrameJSON(t *testing.T, seq int64, hook, cwd string, input map[string]any) string {
	t.Helper()
	b, err := json.Marshal(map[string]any{
		"seq":  seq,
		"hook": hook,
		"session": map[string]any{
			"id":      "pi-session-1",
			"turn_id": "turn-1",
			"cwd":     cwd,
			"model":   "anthropic/claude-sonnet-4",
		},
		"input": input,
	})
	require.NoError(t, err)
	return string(b)
}

// all returns every request the fake server captured, in arrival order.
func (fs *fakeServer) all() []components.IngestRequestBody {
	fs.mu.Lock()
	defer fs.mu.Unlock()
	out := make([]components.IngestRequestBody, len(fs.requests))
	copy(out, fs.requests)
	return out
}

// piServe runs the serve loop over the given frames and returns the replies in
// order. The environment overrides mirror invoke's: an unresolvable device
// agent command keeps identity lookups off the machine's real agent, and a
// per-test spool directory keeps a successful send from execing the test
// binary as a detached drain.
func piServe(t *testing.T, cfg Config, frames ...string) []piReply {
	t.Helper()
	t.Setenv("GRAM_DEVICE_AGENT_COMMANDS", "speakeasy-hooks-test-missing-device-agent")
	if v := os.Getenv("XDG_STATE_HOME"); v == "" || v != spoolStateHome {
		t.Setenv("XDG_STATE_HOME", t.TempDir())
	}

	var out bytes.Buffer
	code := RunPiServe(t.Context(), cfg, strings.NewReader(strings.Join(frames, "\n")+"\n"), &out)
	require.Equal(t, 0, code)

	replies := make([]piReply, 0, len(frames))
	for line := range strings.Lines(out.String()) {
		if strings.TrimSpace(line) == "" {
			continue
		}
		var reply piReply
		require.NoError(t, json.Unmarshal([]byte(line), &reply))
		replies = append(replies, reply)
	}
	return replies
}

func TestPiToolRequestedEnvelope(t *testing.T) {
	fs := newFakeServer(t, nil)
	replies := piServe(t, authedConfig(t, fs.URL), piFrameJSON(t, 1, piHookToolCall, t.TempDir(), map[string]any{
		"tool_call_id": "call-7",
		"tool_name":    "bash",
		"input":        map[string]any{"command": "ls -la"},
	}))

	require.Len(t, replies, 1)
	require.False(t, replies[0].Block)
	require.Equal(t, int64(1), replies[0].Seq)

	require.Equal(t, 1, fs.count())
	got := fs.last()
	require.Equal(t, schemaVersion, got.SchemaVersion)
	require.Equal(t, "pi", got.Source.Adapter)
	require.NotNil(t, got.Source.RawEventName)
	require.Equal(t, "tool_call", *got.Source.RawEventName)
	require.Equal(t, components.TypeToolRequested, got.Event.Type)
	require.NotNil(t, got.Session)
	require.NotNil(t, got.Session.ID)
	require.Equal(t, "pi-session-1", *got.Session.ID)
	require.NotNil(t, got.Session.Model)
	require.Equal(t, "anthropic/claude-sonnet-4", *got.Session.Model)
	require.NotNil(t, got.Data)
	require.NotNil(t, got.Data.ToolCall)
	require.Equal(t, "bash", *got.Data.ToolCall.Name)
	require.Equal(t, "call-7", *got.Data.ToolCall.ID)
	inputJSON, err := json.Marshal(got.Data.ToolCall.Input)
	require.NoError(t, err)
	require.Contains(t, string(inputJSON), "ls -la")
}

func TestPiPromptSubmittedEnvelope(t *testing.T) {
	fs := newFakeServer(t, nil)
	replies := piServe(t, authedConfig(t, fs.URL), piFrameJSON(t, 4, piHookInput, t.TempDir(), map[string]any{
		"text":   "deploy to staging",
		"source": "interactive",
	}))

	require.Len(t, replies, 1)
	require.False(t, replies[0].Block)
	got := fs.last()
	require.Equal(t, components.TypePromptSubmitted, got.Event.Type)
	require.NotNil(t, got.Data.Prompt)
	require.Equal(t, "deploy to staging", *got.Data.Prompt.Text)
}

func TestPiToolCompletedEnvelope(t *testing.T) {
	fs := newFakeServer(t, nil)
	piServe(t, authedConfig(t, fs.URL), piFrameJSON(t, 1, piHookToolResult, t.TempDir(), map[string]any{
		"tool_call_id": "call-7",
		"tool_name":    "bash",
		"input":        map[string]any{"command": "go test ./..."},
		"output":       "all tests pass",
		"is_error":     false,
	}))

	got := fs.last()
	require.Equal(t, components.TypeToolCompleted, got.Event.Type)
	require.NotNil(t, got.Data.ToolCall)
	outputJSON, err := json.Marshal(got.Data.ToolCall.Output)
	require.NoError(t, err)
	require.Contains(t, string(outputJSON), "all tests pass")
	require.Nil(t, got.Data.ToolCall.Error)
}

// A failed Pi tool reports through the same tool_result event as a successful
// one; only the isError flag distinguishes them, so the canonical type has to
// come from the flag rather than the native event name.
func TestPiToolFailedEnvelope(t *testing.T) {
	fs := newFakeServer(t, nil)
	piServe(t, authedConfig(t, fs.URL), piFrameJSON(t, 1, piHookToolResult, t.TempDir(), map[string]any{
		"tool_call_id": "call-8",
		"tool_name":    "read",
		"input":        map[string]any{"path": "/nope"},
		"output":       "File not found: /nope",
		"is_error":     true,
	}))

	got := fs.last()
	require.Equal(t, components.TypeToolFailed, got.Event.Type)
	require.NotNil(t, got.Data.ToolCall.Error)
	errJSON, err := json.Marshal(got.Data.ToolCall.Error)
	require.NoError(t, err)
	require.Contains(t, string(errJSON), "File not found")
}

// Pi reports token and cost totals on each finalized assistant message, which
// is the per-turn granularity the canonical usage block expects.
func TestPiAssistantRespondedCarriesUsage(t *testing.T) {
	fs := newFakeServer(t, nil)
	piServe(t, authedConfig(t, fs.URL), piFrameJSON(t, 1, piHookMessageEnd, t.TempDir(), map[string]any{
		"text":        "Deployed to staging.",
		"stop_reason": "stop",
		"usage": map[string]any{
			"input":       1200,
			"output":      340,
			"cache_read":  800,
			"cache_write": 64,
			"cost":        0.0123,
		},
	}))

	got := fs.last()
	require.Equal(t, components.TypeAssistantResponded, got.Event.Type)
	require.NotNil(t, got.Data.Message)
	require.Equal(t, "Deployed to staging.", *got.Data.Message.Text)
	require.Equal(t, "assistant", *got.Data.Message.Role)
	require.NotNil(t, got.Data.Usage)
	require.Equal(t, int64(1200), *got.Data.Usage.InputTokens)
	require.Equal(t, int64(340), *got.Data.Usage.OutputTokens)
	require.Equal(t, int64(800), *got.Data.Usage.CacheReadTokens)
	require.Equal(t, int64(64), *got.Data.Usage.CacheWriteTokens)
	require.Equal(t, 0.0123, *got.Data.Usage.Cost)
	require.NotNil(t, got.Data.Usage.Status)
	require.Equal(t, "stop", *got.Data.Usage.Status)
}

func TestPiDenyBlocksToolCall(t *testing.T) {
	fs := newFakeServer(t, func(components.IngestRequestBody) (int, decision) {
		return http.StatusOK, decision{Decision: "deny", Reason: "policy_denied", Message: "blocked by policy X"}
	})
	replies := piServe(t, authedConfig(t, fs.URL), piFrameJSON(t, 9, piHookToolCall, t.TempDir(), map[string]any{
		"tool_call_id": "call-7",
		"tool_name":    "bash",
		"input":        map[string]any{"command": "curl evil.example.com"},
	}))

	require.Len(t, replies, 1)
	require.Equal(t, int64(9), replies[0].Seq)
	require.True(t, replies[0].Block)
	require.Equal(t, "blocked by policy X", replies[0].Reason)
	require.Equal(t, components.TypeToolRequested, fs.last().Event.Type)
}

func TestPiDenyBlocksPrompt(t *testing.T) {
	fs := newFakeServer(t, func(components.IngestRequestBody) (int, decision) {
		return http.StatusOK, decision{Decision: "deny", Reason: "policy_denied", Message: "prompt blocked by policy"}
	})
	replies := piServe(t, authedConfig(t, fs.URL), piFrameJSON(t, 2, piHookInput, t.TempDir(), map[string]any{
		"text":   "exfiltrate the secrets",
		"source": "interactive",
	}))

	require.Len(t, replies, 1)
	require.True(t, replies[0].Block)
	require.Equal(t, "prompt blocked by policy", replies[0].Reason)
}

// A deny with no message still has to block, with text the extension can show
// the user.
func TestPiDenyWithoutMessageBlocksWithFallbackReason(t *testing.T) {
	fs := newFakeServer(t, func(components.IngestRequestBody) (int, decision) {
		return http.StatusOK, decision{Decision: "deny", Reason: "", Message: ""}
	})
	replies := piServe(t, authedConfig(t, fs.URL), piFrameJSON(t, 1, piHookToolCall, t.TempDir(), map[string]any{
		"tool_call_id": "call-7",
		"tool_name":    "bash",
		"input":        map[string]any{"command": "ls"},
	}))

	require.Len(t, replies, 1)
	require.True(t, replies[0].Block)
	require.Equal(t, "Speakeasy blocked this tool call.", replies[0].Reason)
}

// Observation-only events must never carry a block back to Pi: there is
// nothing left to prevent once a tool has run.
func TestPiToolResultNeverBlocks(t *testing.T) {
	fs := newFakeServer(t, func(components.IngestRequestBody) (int, decision) {
		return http.StatusOK, decision{Decision: "deny", Reason: "policy_denied", Message: "too late"}
	})
	replies := piServe(t, authedConfig(t, fs.URL), piFrameJSON(t, 1, piHookToolResult, t.TempDir(), map[string]any{
		"tool_call_id": "call-7",
		"tool_name":    "bash",
		"input":        map[string]any{},
		"output":       "done",
		"is_error":     false,
	}))

	require.Len(t, replies, 1)
	require.False(t, replies[0].Block)
	require.Equal(t, 1, fs.count())
}

// A frame the decoder cannot classify is answered without a decision and
// relayed to nobody: a shim newer than the binary must not produce telemetry
// with no canonical event type, and it must not wedge Pi either.
func TestPiUnknownHookIsIgnored(t *testing.T) {
	fs := newFakeServer(t, nil)
	replies := piServe(t, authedConfig(t, fs.URL),
		piFrameJSON(t, 1, "some_future_event", t.TempDir(), map[string]any{"x": 1}),
		"{not json}",
	)

	require.Len(t, replies, 1)
	require.Equal(t, int64(1), replies[0].Seq)
	require.False(t, replies[0].Block)
	require.Equal(t, 0, fs.count())
}

// A gated action the shim could not send whole must not be allowed
// unevaluated, even when the server would have allowed what it saw.
func TestPiOversizedGatedFramesBlock(t *testing.T) {
	fs := newFakeServer(t, nil)
	oversized := func(seq int64, hook string, input map[string]any) string {
		var frame map[string]any
		require.NoError(t, json.Unmarshal([]byte(piFrameJSON(t, seq, hook, t.TempDir(), input)), &frame))
		frame["oversized"] = true
		b, err := json.Marshal(frame)
		require.NoError(t, err)
		return string(b)
	}
	replies := piServe(t, authedConfig(t, fs.URL),
		oversized(1, piHookToolCall, map[string]any{"tool_call_id": "call-1", "tool_name": "bash"}),
		oversized(2, piHookInput, map[string]any{"tool_call_id": "", "tool_name": ""}),
		oversized(3, piHookToolResult, map[string]any{"tool_call_id": "call-1", "tool_name": "bash"}),
	)

	require.Len(t, replies, 3)
	require.True(t, replies[0].Block)
	require.Contains(t, replies[0].Reason, "too large")
	require.True(t, replies[1].Block)
	require.Contains(t, replies[1].Reason, "too large")
	require.False(t, replies[2].Block)
}

// The initialize frame only starts the relay; it carries no event.
func TestPiInitializeFrameReportsNothing(t *testing.T) {
	fs := newFakeServer(t, nil)
	replies := piServe(t, authedConfig(t, fs.URL), piFrameJSON(t, 1, piHookInitialize, t.TempDir(), map[string]any{}))

	require.Len(t, replies, 1)
	require.Equal(t, 0, fs.count())
}

// writePiMCPConfig writes a project-scoped Pi MCP config and returns its
// directory. HOME is redirected so an operator's real global config cannot
// leak into the assertions.
func writePiMCPConfig(t *testing.T, servers map[string]any) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("USERPROFILE", t.TempDir())
	body, err := json.Marshal(map[string]any{"mcpServers": servers})
	require.NoError(t, err)
	require.NoError(t, os.MkdirAll(filepath.Join(dir, ".pi"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".pi", "mcp.json"), body, 0o600))
	return dir
}

// Pi ships no MCP client, so the servers an MCP extension can reach are only
// discoverable from its config file. The snapshot puts them in Shadow MCP
// inventory at session start, before any tool call, with credentials redacted.
func TestPiSessionStartReportsMCPInventory(t *testing.T) {
	dir := writePiMCPConfig(t, map[string]any{
		"linear": map[string]any{"url": "https://user:secret@mcp.example.com/sse?api_key=leaked&workspace=acme"},
		"notes":  map[string]any{"command": "npx", "args": []string{"-y", "notes-mcp"}},
	})

	fs := newFakeServer(t, nil)
	piServe(t, authedConfig(t, fs.URL), piFrameJSON(t, 1, piHookSessionStart, dir, map[string]any{"reason": "startup"}))

	requests := fs.all()
	require.Len(t, requests, 2)
	inventory := requests[0]
	require.Equal(t, components.TypeMcpInventory, inventory.Event.Type)
	require.Equal(t, "pi", inventory.Source.Adapter)
	require.NotNil(t, inventory.Data.McpInventoryCollected)
	require.True(t, *inventory.Data.McpInventoryCollected)
	require.Len(t, inventory.Data.McpInventory, 2)

	byName := map[string]components.HookMCPData{}
	for _, entry := range inventory.Data.McpInventory {
		byName[*entry.ServerName] = entry
	}
	linear := byName["linear"]
	require.NotNil(t, linear.URL)
	require.NotContains(t, *linear.URL, "secret")
	require.NotContains(t, *linear.URL, "leaked")
	require.Contains(t, *linear.URL, "mcp.example.com")
	notes := byName["notes"]
	require.NotNil(t, notes.Command)
	require.Equal(t, "npx -y notes-mcp", *notes.Command)

	require.Equal(t, components.TypeSessionStarted, requests[1].Event.Type)
}

// One snapshot per session is enough: repeating it on every tool call would
// double every session's ingest volume.
func TestPiMCPInventoryReportedOncePerSession(t *testing.T) {
	dir := writePiMCPConfig(t, map[string]any{
		"linear": map[string]any{"url": "https://mcp.example.com/sse"},
	})

	fs := newFakeServer(t, nil)
	piServe(t, authedConfig(t, fs.URL),
		piFrameJSON(t, 1, piHookSessionStart, dir, map[string]any{"reason": "startup"}),
		piFrameJSON(t, 2, piHookToolCall, dir, map[string]any{
			"tool_call_id": "call-1",
			"tool_name":    "linear_create_issue",
			"input":        map[string]any{"title": "bug"},
		}),
	)

	inventories := 0
	for _, req := range fs.all() {
		if req.Event.Type == components.TypeMcpInventory {
			inventories++
		}
	}
	require.Equal(t, 1, inventories)
}

// MCP tools bridged into Pi by an extension are registered as ordinary Pi
// tools, so a config-matched name is the only signal that a call left the
// machine — and the resolved URL must still be redacted.
func TestPiMCPToolCallAttributedAndRedacted(t *testing.T) {
	dir := writePiMCPConfig(t, map[string]any{
		"linear": map[string]any{"url": "https://user:secret@mcp.example.com/sse?api_key=leaked&workspace=acme"},
	})

	fs := newFakeServer(t, nil)
	piServe(t, authedConfig(t, fs.URL), piFrameJSON(t, 1, piHookToolCall, dir, map[string]any{
		"tool_call_id": "call-7",
		"tool_name":    "linear_create_issue",
		"input":        map[string]any{"title": "bug: flaky test"},
	}))

	var toolCall components.IngestRequestBody
	for _, req := range fs.all() {
		if req.Event.Type == components.TypeToolRequested {
			toolCall = req
		}
	}
	require.NotNil(t, toolCall.Data)
	require.NotNil(t, toolCall.Data.Mcp)
	require.Equal(t, "linear", *toolCall.Data.Mcp.ServerName)
	require.NotNil(t, toolCall.Data.Mcp.URL)
	require.Equal(t, "https://mcp.example.com/sse?api_key=%2A%2A%2A&workspace=acme", *toolCall.Data.Mcp.URL)
}

// A native Pi tool whose name happens to share a prefix with nothing in the
// config must not be attributed to an MCP server.
func TestPiNativeToolNotAttributedToMCP(t *testing.T) {
	dir := writePiMCPConfig(t, map[string]any{
		"linear": map[string]any{"url": "https://mcp.example.com/sse"},
	})

	fs := newFakeServer(t, nil)
	piServe(t, authedConfig(t, fs.URL), piFrameJSON(t, 1, piHookToolCall, dir, map[string]any{
		"tool_call_id": "call-7",
		"tool_name":    "bash",
		"input":        map[string]any{"command": "ls"},
	}))

	var toolCall components.IngestRequestBody
	for _, req := range fs.all() {
		if req.Event.Type == components.TypeToolRequested {
			toolCall = req
		}
	}
	require.NotNil(t, toolCall.Data)
	require.Nil(t, toolCall.Data.Mcp)
}

func TestPiMCPNameMatchingPrefersLongestServerName(t *testing.T) {
	servers := []agenthooks.MCPServer{
		{Name: "notes"},
		{Name: "notes_admin"},
	}
	matched, server, tool := matchPiMCPName(servers, "notes_admin_delete")
	require.NotNil(t, matched)
	require.Equal(t, "notes_admin", server)
	require.Equal(t, "delete", tool)
}

func TestPiMCPNameMatchingAcceptsReservedDialects(t *testing.T) {
	servers := []agenthooks.MCPServer{{Name: "linear"}}

	matched, server, tool := matchPiMCPName(servers, "mcp__linear__create_issue")
	require.NotNil(t, matched)
	require.Equal(t, "linear", server)
	require.Equal(t, "create_issue", tool)

	matched, server, tool = matchPiMCPName(servers, "mcp_linear_create_issue")
	require.NotNil(t, matched)
	require.Equal(t, "linear", server)
	require.Equal(t, "create_issue", tool)
}

func TestPiMCPNameMatchingIgnoresUnknownServers(t *testing.T) {
	matched, _, _ := matchPiMCPName([]agenthooks.MCPServer{{Name: "linear"}}, "github_create_pr")
	require.Nil(t, matched)
}

// A disabled server is not reachable, so it must not appear in inventory.
func TestPiMCPConfigSkipsDisabledServers(t *testing.T) {
	dir := writePiMCPConfig(t, map[string]any{
		"linear":   map[string]any{"url": "https://mcp.example.com/sse", "enabled": false},
		"internal": map[string]any{"url": "https://internal.example.com/mcp"},
	})

	servers := loadPiMCPServers(dir)
	require.Len(t, servers, 1)
	require.Equal(t, "internal", servers[0].Name)
}

// The project config is repository-controlled and read on the gating path, so
// anything but a bounded regular file is ignored rather than read.
func TestPiMCPConfigIgnoresNonRegularAndOversizedFiles(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("USERPROFILE", t.TempDir())

	notAFile := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(notAFile, ".pi", "mcp.json"), 0o755))
	require.Empty(t, loadPiMCPServers(notAFile))

	oversized := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(oversized, ".pi"), 0o755))
	body := `{"mcpServers":{"linear":{"url":"https://mcp.example.com/sse"}}}` + strings.Repeat(" ", maxPiMCPConfigBytes)
	require.NoError(t, os.WriteFile(filepath.Join(oversized, ".pi", "mcp.json"), []byte(body), 0o600))
	require.Empty(t, loadPiMCPServers(oversized))
}

// Project entries win over global ones of the same name, matching the
// precedence Pi applies to its own project-local configuration.
func TestPiMCPConfigProjectOverridesGlobal(t *testing.T) {
	home := t.TempDir()
	dir := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)

	globalBody, err := json.Marshal(map[string]any{"mcpServers": map[string]any{
		"linear": map[string]any{"url": "https://global.example.com/mcp"},
		"only":   map[string]any{"url": "https://only-global.example.com/mcp"},
	}})
	require.NoError(t, err)
	require.NoError(t, os.MkdirAll(filepath.Join(home, ".pi", "agent"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(home, ".pi", "agent", "mcp.json"), globalBody, 0o600))

	projectBody, err := json.Marshal(map[string]any{"mcpServers": map[string]any{
		"linear": map[string]any{"url": "https://project.example.com/mcp"},
	}})
	require.NoError(t, err)
	require.NoError(t, os.MkdirAll(filepath.Join(dir, ".pi"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".pi", "mcp.json"), projectBody, 0o600))

	servers := loadPiMCPServers(dir)
	require.Len(t, servers, 2)
	byName := map[string]string{}
	for _, s := range servers {
		byName[s.Name] = s.URL
	}
	require.Equal(t, "https://project.example.com/mcp", byName["linear"])
	require.Equal(t, "https://only-global.example.com/mcp", byName["only"])
}

// The extension and the relay are two halves of one frame protocol, and only
// the generated file names the subcommand that speaks it.
func TestRenderPiExtensionForBinaryBakesTheRelayCommand(t *testing.T) {
	content, err := RenderPiExtensionForBinary("/usr/local/bin/speakeasy-hooks", "/tmp/plugin/speakeasy.json")
	require.NoError(t, err)

	body := string(content)
	require.Contains(t, body, `const COMMAND: string[] = ["/usr/local/bin/speakeasy-hooks","pi","serve","--config=/tmp/plugin/speakeasy.json"]`)
	for _, hook := range []string{piHookSessionStart, piHookSessionShutdown, piHookInput, piHookToolCall, piHookToolResult, piHookMessageEnd} {
		require.Contains(t, body, `pi.on("`+hook+`"`)
	}
	require.NotContains(t, body, piExtensionCommandMarker)
}

func TestRenderPiExtensionForBootstrapResolvesPackageRelativePaths(t *testing.T) {
	body := string(RenderPiExtensionForBootstrap())

	require.Contains(t, body, `"--config=" + join(ROOT, "speakeasy.json")`)
	require.Contains(t, body, `join(ROOT, "hooks", "bootstrap.sh")`)
	require.Contains(t, body, `join(ROOT, "hooks", "bootstrap.ps1")`)
	require.Contains(t, body, `"pi","serve"`)
	require.NotContains(t, body, piExtensionCommandMarker)
}

// The shim's side of the oversized-frame contract: it must flag the frame the
// relay then blocks, and retire a relay that missed its deadline.
func TestRenderPiExtensionBoundsFramesAndReplacesStalledRelay(t *testing.T) {
	body := string(RenderPiExtensionForBootstrap())

	require.Contains(t, body, "oversized: true")
	require.Contains(t, body, "MAX_FRAME_BYTES")
	require.Contains(t, body, "replace(proc)")
}

func TestWritePluginUnknownProviderWritesNothing(t *testing.T) {
	dir := t.TempDir()
	err := WritePlugin(t.Context(), "pie", dir, PluginConfig{
		ServerURL:    "https://gram.test",
		ProjectSlug:  "default",
		OrgID:        "org-1",
		HooksAPIKey:  "shared-key",
		BrowserLogin: false,
		BinaryPath:   "/tmp/speakeasy-hooks",
	})
	require.ErrorContains(t, err, "unknown provider")

	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	require.Empty(t, entries)
}
