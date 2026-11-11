package relay

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/speakeasy-api/agenthooks"
	"github.com/speakeasy-api/agenthooks/agenthookstest"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/hooks/sdk/models/components"
)

// vibeFixture loads one captured Vibe stdin payload. agenthookstest.Fixture
// reads the external module's embedded corpus, which has no vibe directory
// (the module does not know the provider), so Vibe fixtures live here.
func vibeFixture(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", "vibe", name))
	require.NoError(t, err)
	return b
}

// vibeInvoke mirrors agenthookstest.Invoke for the vibe provider: it builds
// the same argv Invoke does ("agenthooks run --provider=vibe") and captures
// the same streams, but drives RunVibeCommand, because the external
// agenthooks Runner rejects --provider=vibe in parseArgs before any relay
// code could see it. The Result type is agenthookstest's, so assertions read
// exactly like the other providers' hook tests.
func vibeInvoke(t *testing.T, cfg Config, payload []byte, extraArgs ...string) agenthookstest.Result {
	t.Helper()
	t.Setenv("SPEAKEASY_AI_DEVICE_AGENT_COMMANDS", "speakeasy-hooks-test-missing-device-agent")
	if v := os.Getenv("XDG_STATE_HOME"); v == "" || v != spoolStateHome {
		t.Setenv("XDG_STATE_HOME", t.TempDir())
	}
	args := append([]string{"agenthooks", "run", "--provider=" + string(ProviderVibe)}, extraArgs...)
	var stdout, stderr bytes.Buffer
	code := RunVibeCommand(context.Background(), cfg, args, bytes.NewReader(payload), &stdout, &stderr)
	return agenthookstest.Result{Stdout: stdout.Bytes(), Stderr: stderr.String(), ExitCode: code}
}

// vibeReply decodes the structured stdout response of a vibe invocation.
func vibeReply(t *testing.T, res agenthookstest.Result) vibeResponse {
	t.Helper()
	require.Equal(t, 0, res.ExitCode, "stderr: %s", res.Stderr)
	var resp vibeResponse
	require.NoError(t, json.Unmarshal(res.Stdout, &resp), "stdout: %q", res.Stdout)
	return resp
}

func TestVibePreToolEnvelope(t *testing.T) {
	fs := newFakeServer(t, nil)
	res := vibeInvoke(t, authedConfig(t, fs.URL), vibeFixture(t, "pre_tool.json"))

	resp := vibeReply(t, res)
	require.Equal(t, vibeDecisionAllow, resp.Decision)

	require.Equal(t, 1, fs.count())
	got := fs.last()
	require.Equal(t, schemaVersion, got.SchemaVersion)
	require.Equal(t, "vibe", got.Source.Adapter)
	require.NotNil(t, got.Source.RawEventName)
	require.Equal(t, "pre_tool", *got.Source.RawEventName)
	require.Equal(t, components.TypeToolRequested, got.Event.Type)
	require.NotNil(t, got.Session)
	require.Equal(t, "vibe-session-1", *got.Session.ID)
	require.Equal(t, "/tmp/vibe-project", *got.Session.Cwd)
	require.NotNil(t, got.Data)
	require.NotNil(t, got.Data.ToolCall)
	// The qualified built-in name maps to the short shape other providers send.
	require.Equal(t, "bash", *got.Data.ToolCall.Name)
	require.Equal(t, "call_42", *got.Data.ToolCall.ID)
	inputJSON, err := json.Marshal(got.Data.ToolCall.Input)
	require.NoError(t, err)
	require.Contains(t, string(inputJSON), "ls -la")
}

func TestVibePreToolMCPNameDecoded(t *testing.T) {
	fs := newFakeServer(t, nil)
	res := vibeInvoke(t, authedConfig(t, fs.URL), vibeFixture(t, "pre_tool_mcp.json"))
	require.Equal(t, vibeDecisionAllow, vibeReply(t, res).Decision)

	got := fs.last()
	require.Equal(t, components.TypeToolRequested, got.Event.Type)
	require.NotNil(t, got.Data.ToolCall)
	// The verbatim mcp_<server>.<tool> name stays on the tool call...
	require.Equal(t, "mcp_linear.create_issue", *got.Data.ToolCall.Name)
	// ...while the dot dialect is split into server and tool.
	require.NotNil(t, got.Data.Mcp)
	require.Equal(t, "linear", *got.Data.Mcp.ServerName)
}

func TestVibePreToolDenyBecomesStructuredDeny(t *testing.T) {
	fs := newFakeServer(t, func(components.IngestRequestBody) (int, decision) {
		return http.StatusOK, decision{Decision: "deny", Reason: "policy_denied", Message: "blocked by policy X"}
	})
	res := vibeInvoke(t, authedConfig(t, fs.URL), vibeFixture(t, "pre_tool.json"))

	resp := vibeReply(t, res)
	require.Equal(t, vibeDecisionDeny, resp.Decision)
	require.Equal(t, "blocked by policy X", resp.Reason)
	require.Equal(t, 1, fs.count())
	require.Equal(t, components.TypeToolRequested, fs.last().Event.Type)
}

func TestVibePreToolDenyWithoutMessageFallsBack(t *testing.T) {
	fs := newFakeServer(t, func(components.IngestRequestBody) (int, decision) {
		return http.StatusOK, decision{Decision: "deny", Reason: "policy_denied"}
	})
	res := vibeInvoke(t, authedConfig(t, fs.URL), vibeFixture(t, "pre_tool.json"))

	resp := vibeReply(t, res)
	require.Equal(t, vibeDecisionDeny, resp.Decision)
	require.NotEmpty(t, resp.Reason, "a deny must always carry a reason for Vibe")
}

// TestVibePreToolUpdatedInput encodes the full tool_input replacement. The
// relay's own handlers never rewrite input today, so this pins the encoder
// against an agenthooks decision carrying one.
func TestVibePreToolUpdatedInput(t *testing.T) {
	dec := agenthooks.Allow().WithUpdatedInput(map[string]any{"command": "ls"})
	resp := vibeEncodePreTool(dec)

	require.Equal(t, vibeDecisionAllow, resp.Decision)
	require.NotNil(t, resp.HookSpecificOutput)
	require.JSONEq(t, `{"command": "ls"}`, string(resp.HookSpecificOutput.ToolInput))
}

func TestVibePostToolSuccessEnvelope(t *testing.T) {
	fs := newFakeServer(t, nil)
	res := vibeInvoke(t, authedConfig(t, fs.URL), vibeFixture(t, "post_tool.json"))

	resp := vibeReply(t, res)
	require.Equal(t, vibeDecisionAllow, resp.Decision)

	got := fs.last()
	require.Equal(t, "vibe", got.Source.Adapter)
	require.Equal(t, "post_tool", *got.Source.RawEventName)
	require.Equal(t, components.TypeToolCompleted, got.Event.Type)
	require.NotNil(t, got.Data.ToolCall)
	require.Equal(t, "bash", *got.Data.ToolCall.Name)
	require.Equal(t, "call_42", *got.Data.ToolCall.ID)
	require.NotNil(t, got.Data.ToolCall.Output)
	outJSON, err := json.Marshal(got.Data.ToolCall.Output)
	require.NoError(t, err)
	require.Contains(t, string(outJSON), "total 8")
	require.NotNil(t, got.Data.ToolCall.DurationMs)
}

func TestVibePostToolFailureEnvelope(t *testing.T) {
	fs := newFakeServer(t, nil)
	res := vibeInvoke(t, authedConfig(t, fs.URL), vibeFixture(t, "post_tool_failure.json"))
	require.Equal(t, vibeDecisionAllow, vibeReply(t, res).Decision)

	got := fs.last()
	require.Equal(t, components.TypeToolFailed, got.Event.Type)
	require.NotNil(t, got.Data.ToolCall)
	require.Equal(t, "read_file", *got.Data.ToolCall.Name)
	require.NotNil(t, got.Data.ToolCall.Error)
	errJSON, err := json.Marshal(got.Data.ToolCall.Error)
	require.NoError(t, err)
	require.Contains(t, string(errJSON), "File not found")
}

// TestVibePostToolDenyReplacesOutputText pins the post_tool deny mapping: Vibe
// replaces the model-visible tool_output_text with the reason.
func TestVibePostToolDenyReplacesOutputText(t *testing.T) {
	fs := newFakeServer(t, func(components.IngestRequestBody) (int, decision) {
		return http.StatusOK, decision{Decision: "deny", Reason: "pii_detected", Message: "output contained secrets"}
	})
	res := vibeInvoke(t, authedConfig(t, fs.URL), vibeFixture(t, "post_tool.json"))

	resp := vibeReply(t, res)
	require.Equal(t, vibeDecisionDeny, resp.Decision)
	require.Equal(t, "output contained secrets", resp.Reason)
	require.Equal(t, 1, fs.count())
	require.Equal(t, components.TypeToolCompleted, fs.last().Event.Type)
}

// TestVibePostToolMessageBecomesAdditionalContext pins the non-blocking
// message mapping: Vibe appends it to tool_output_text.
func TestVibePostToolMessageBecomesAdditionalContext(t *testing.T) {
	fs := newFakeServer(t, func(components.IngestRequestBody) (int, decision) {
		return http.StatusOK, decision{Decision: "allow", Message: "note from policy"}
	})
	res := vibeInvoke(t, authedConfig(t, fs.URL), vibeFixture(t, "post_tool.json"))

	resp := vibeReply(t, res)
	require.Equal(t, vibeDecisionAllow, resp.Decision)
	require.NotNil(t, resp.HookSpecificOutput)
	require.Equal(t, "note from policy", resp.HookSpecificOutput.AdditionalContext)
}

func TestVibePostAgentEnvelope(t *testing.T) {
	fs := newFakeServer(t, nil)
	res := vibeInvoke(t, authedConfig(t, fs.URL), vibeFixture(t, "post_agent.json"))

	resp := vibeReply(t, res)
	require.Equal(t, vibeDecisionAllow, resp.Decision)

	got := fs.last()
	require.Equal(t, "vibe", got.Source.Adapter)
	require.Equal(t, "post_agent", *got.Source.RawEventName)
	require.Equal(t, components.TypeAssistantResponded, got.Event.Type)
	require.Equal(t, "vibe-session-1", *got.Session.ID)
}

// TestVibePostAgentDenyInjectsRetry pins the post_agent deny mapping: Vibe
// injects the reason as a retry user message (capped at 3 per turn by Vibe).
func TestVibePostAgentDenyInjectsRetry(t *testing.T) {
	fs := newFakeServer(t, func(components.IngestRequestBody) (int, decision) {
		return http.StatusOK, decision{Decision: "deny", Reason: "incomplete", Message: "run the tests first"}
	})
	res := vibeInvoke(t, authedConfig(t, fs.URL), vibeFixture(t, "post_agent.json"))

	resp := vibeReply(t, res)
	require.Equal(t, vibeDecisionDeny, resp.Decision)
	require.Equal(t, "run the tests first", resp.Reason)
}

// TestVibePostAgentSubagentMarksAgent checks the subagent marker: a
// parent_session_id makes the unified event carry Agent info.
func TestVibePostAgentSubagentMarksAgent(t *testing.T) {
	fs := newFakeServer(t, nil)
	res := vibeInvoke(t, authedConfig(t, fs.URL), vibeFixture(t, "post_agent_subagent.json"))
	require.Equal(t, vibeDecisionAllow, vibeReply(t, res).Decision)

	var in vibeHookInput
	require.NoError(t, json.Unmarshal(vibeFixture(t, "post_agent_subagent.json"), &in))
	ev := vibeEvent(&in, agenthooks.KindStop, vibeFixture(t, "post_agent_subagent.json"))
	require.NotNil(t, ev.Agent)
	require.Equal(t, "vibe-subagent-9", ev.Agent.ID)
	require.Equal(t, "reviewer", ev.Agent.Type)
	require.Equal(t, "vibe-session-1", ev.Ext["parent_session_id"])
	require.Equal(t, ProviderVibe, ev.Provider)
}

func TestVibeUnknownHookPassesThrough(t *testing.T) {
	fs := newFakeServer(t, nil)
	// Vibe fires more hook types than the relay classifies (e.g. session_start
	// in 3.1.x); a type it cannot classify must pass through, so use one that
	// is not a real Vibe hook at all.
	payload := []byte(`{"hook_event_name":"not_a_real_hook","session_id":"s","cwd":"/tmp"}`)
	res := vibeInvoke(t, authedConfig(t, fs.URL), payload)

	require.Equal(t, 0, res.ExitCode)
	require.Empty(t, strings.TrimSpace(string(res.Stdout)), "unknown hook types pass through with empty stdout")
	require.Equal(t, 0, fs.count(), "unclassifiable events are not relayed")
}

// TestVibeFailOpenOnUndecodablePayload: without strict, a payload the relay
// cannot decode fails open — exit 0, empty stdout (Vibe's pass-through).
func TestVibeFailOpenOnUndecodablePayload(t *testing.T) {
	fs := newFakeServer(t, nil)
	res := vibeInvoke(t, authedConfig(t, fs.URL), []byte(`{"hook_event_name":`))

	require.Equal(t, 0, res.ExitCode)
	require.Empty(t, strings.TrimSpace(string(res.Stdout)))
	require.Equal(t, 0, fs.count())
}

// TestVibeStrictPreToolFailureDenies: under strict, a failure on pre_tool is
// answered with a structured deny (Vibe surfaces the reason as the tool
// error) instead of relying on the non-zero-exit path.
func TestVibeStrictPreToolFailureDenies(t *testing.T) {
	fs := newFakeServer(t, nil)
	// The discriminant survives; the rest of the payload is broken.
	payload := []byte(`{"hook_event_name":"pre_tool","tool_name":123}`)
	res := vibeInvoke(t, authedConfig(t, fs.URL), payload, "--strict")

	resp := vibeReply(t, res)
	require.Equal(t, vibeDecisionDeny, resp.Decision)
	require.Contains(t, resp.Reason, "pre_tool")
	require.Equal(t, 0, fs.count())
}

// TestVibeStrictTruncatedPayloadFailsClosed: a payload truncated by the stdin
// LimitReader no longer parses, so the head sniff yields an empty event name;
// the byte-level sniff must still recover the type, and under strict the
// failure denies (pre_tool) rather than passing through.
func TestVibeStrictTruncatedPayloadFailsClosed(t *testing.T) {
	fs := newFakeServer(t, nil)
	// Invalid JSON, but the event name survives in the raw bytes.
	truncated := []byte(`{"hook_event_name":"pre_tool","tool_input":{"command":"rm -rf`)
	res := vibeInvoke(t, authedConfig(t, fs.URL), truncated, "--strict")

	resp := vibeReply(t, res)
	require.Equal(t, vibeDecisionDeny, resp.Decision)
	require.Contains(t, resp.Reason, "pre_tool")
	require.Equal(t, 0, fs.count())

	// Fully unrecognisable bytes under strict still fail closed.
	res = vibeInvoke(t, authedConfig(t, fs.URL), []byte(`{"hook_event_name":"`), "--strict")
	resp = vibeReply(t, res)
	require.Equal(t, vibeDecisionDeny, resp.Decision)
	require.NotEmpty(t, resp.Reason)

	// Without strict the same truncated payload passes through.
	res = vibeInvoke(t, authedConfig(t, fs.URL), truncated)
	require.Equal(t, 0, res.ExitCode)
	require.Empty(t, strings.TrimSpace(string(res.Stdout)))
}

// TestVibeStrictPostToolFailureClears: under strict, a failure on post_tool
// is answered with a deny whose reason replaces tool_output_text — an empty
// reason clears the output, the strict post_tool outcome.
func TestVibeStrictPostToolFailureClears(t *testing.T) {
	fs := newFakeServer(t, nil)
	payload := []byte(`{"hook_event_name":"post_tool","tool_status":true}`)
	res := vibeInvoke(t, authedConfig(t, fs.URL), payload, "--strict")

	resp := vibeReply(t, res)
	require.Equal(t, vibeDecisionDeny, resp.Decision)
	require.Empty(t, resp.Reason)
	require.Equal(t, 0, fs.count())
}

// TestVibeStrictForbiddenOnPostAgent: strict is not valid for post_agent in
// Vibe's schema, so a post_agent failure always fails open.
func TestVibeStrictForbiddenOnPostAgent(t *testing.T) {
	fs := newFakeServer(t, nil)
	payload := []byte(`{"hook_event_name":"post_agent","agent_name":7}`)
	res := vibeInvoke(t, authedConfig(t, fs.URL), payload, "--strict")

	require.Equal(t, 0, res.ExitCode)
	require.Empty(t, strings.TrimSpace(string(res.Stdout)))
	require.Equal(t, 0, fs.count())
}

// TestVibeStrictFromEnv: the strict posture can also arrive via the
// SPEAKEASY_AI_HOOKS_STRICT env var, for hook commands that cannot add flags.
func TestVibeStrictFromEnv(t *testing.T) {
	t.Setenv("SPEAKEASY_AI_HOOKS_STRICT", "true")
	fs := newFakeServer(t, nil)
	payload := []byte(`{"hook_event_name":"pre_tool","tool_name":123}`)
	res := vibeInvoke(t, authedConfig(t, fs.URL), payload)

	resp := vibeReply(t, res)
	require.Equal(t, vibeDecisionDeny, resp.Decision)
}

func TestVibeArgParsing(t *testing.T) {
	require.True(t, IsVibeInvocation([]string{"--config=/x/speakeasy.json", "agenthooks", "run", "--provider=vibe"}))
	require.True(t, IsVibeInvocation([]string{"agenthooks", "run", "--provider=vibe", "--timeout=30s"}))
	require.False(t, IsVibeInvocation([]string{"agenthooks", "run", "--provider=claude-code"}))
	require.False(t, IsVibeInvocation([]string{"login", "--force"}))
	require.False(t, IsVibeInvocation(nil))

	inv, err := parseVibeArgs([]string{"--config=/x/speakeasy.json", "agenthooks", "run", "--provider=vibe", "--timeout=30s", "--strict"})
	require.NoError(t, err)
	require.Equal(t, 30*time.Second, inv.timeout)
	require.True(t, inv.strict)

	_, err = parseVibeArgs([]string{"agenthooks", "serve", "--provider=vibe"})
	require.Error(t, err, "Vibe spawns one process per event; serve mode does not exist")

	_, err = parseVibeArgs([]string{"agenthooks", "run", "--provider=claude-code"})
	require.Error(t, err)
}

// TestVibeHooksTomlRendered pins the generated hooks.toml the install
// subcommand writes for --provider=vibe: one hook per event type, only
// pre_tool strict (a strict post_tool failure would clear the result of a
// tool that already ran), commands pointing at the binary with the plugin
// config.
func TestVibeHooksTomlRendered(t *testing.T) {
	dir := t.TempDir()
	cfg := PluginConfig{ServerURL: "https://app.getgram.ai", ProjectSlug: "default", BinaryPath: "/bin/speakeasy-hooks"}
	require.NoError(t, WritePlugin(context.Background(), "vibe", dir, cfg))

	b, err := os.ReadFile(filepath.Join(dir, ".vibe", "hooks.toml"))
	require.NoError(t, err)
	doc := string(b)
	require.Contains(t, doc, `type = "pre_tool"`)
	require.Contains(t, doc, `type = "post_tool"`)
	require.Contains(t, doc, `type = "post_agent"`)
	require.Contains(t, doc, "agenthooks run --provider=vibe --strict")
	require.Contains(t, doc, filepath.Join(dir, configFileName))
	// Only pre_tool is strict; post_agent forbids strict in Vibe's schema.
	require.Equal(t, 1, strings.Count(doc, "strict = true"))
	// The strict flag rides only on the pre_tool command.
	require.Equal(t, 1, strings.Count(doc, "--strict"))

	// The deployment identity file is written beside it, like every provider.
	require.FileExists(t, filepath.Join(dir, configFileName))
}

// TestLoadVibeMCPServers covers config discovery: [[mcp_servers]] from the
// Vibe home's config.toml ($VIBE_HOME honoured), plugin mcp.json under the
// home's plugins/ and the cwd's trusted .vibe/plugins/, stdio args joined
// onto the command, and Vibe's name normalisation matching in resolveVibeMCP.
func TestLoadVibeMCPServers(t *testing.T) {
	home := t.TempDir()
	t.Setenv("VIBE_HOME", home)
	cwd := t.TempDir()

	require.NoError(t, os.WriteFile(filepath.Join(home, "config.toml"), []byte(`
[[mcp_servers]]
name = "speakeasy-linear"
url = "https://app.getgram.ai/mcp/linear"

[[mcp_servers]]
name = "local-stdio"
command = "npx"
args = ["-y", "@example/mcp"]
`), 0o600))
	pluginDir := filepath.Join(home, "plugins", "my-plugin")
	require.NoError(t, os.MkdirAll(pluginDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(pluginDir, "mcp.json"), []byte(`{"mcpServers":{"docs":{"command":"uvx","args":["docs-mcp"]}}}`), 0o600))
	projDir := filepath.Join(cwd, ".vibe", "plugins", "proj-plugin")
	require.NoError(t, os.MkdirAll(projDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(projDir, "mcp.json"), []byte(`{"mcpServers":{"proj":{"url":"https://proj.example.test/mcp"}}}`), 0o600))

	servers := loadVibeMCPServers(cwd)
	byName := map[string]agenthooks.MCPServer{}
	for _, s := range servers {
		byName[s.Name] = s
	}
	require.Equal(t, "https://app.getgram.ai/mcp/linear", byName["speakeasy-linear"].URL)
	require.Equal(t, "npx -y @example/mcp", byName["local-stdio"].Command, "stdio args must join onto the command")
	require.Equal(t, "uvx docs-mcp", byName["docs"].Command)
	require.Equal(t, "https://proj.example.test/mcp", byName["proj"].URL)

	// Vibe normalises server names in tool calls: "-" becomes "_".
	var tc agenthooks.ToolCall
	tc.MCP = &agenthooks.MCPCall{Server: "speakeasy_linear"}
	resolveVibeMCP(&tc, servers)
	require.Equal(t, "https://app.getgram.ai/mcp/linear", tc.MCP.URL)
	require.True(t, tc.MCP.FromConfig)
	require.Equal(t, agenthooks.ToolMCP, tc.Canonical)
}

// TestLoadVibeMCPServersSkipsUnsafeFiles: a FIFO or an oversized mcp.json in
// the project scope must be skipped, not stall or balloon the gating read.
func TestLoadVibeMCPServersSkipsUnsafeFiles(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("FIFO test is unix-only")
	}
	home := t.TempDir()
	t.Setenv("VIBE_HOME", home)
	cwd := t.TempDir()
	projDir := filepath.Join(cwd, ".vibe", "plugins", "evil")
	require.NoError(t, os.MkdirAll(projDir, 0o755))
	require.NoError(t, syscall.Mkfifo(filepath.Join(projDir, "mcp.json"), 0o600))
	bigDir := filepath.Join(cwd, ".vibe", "plugins", "big")
	require.NoError(t, os.MkdirAll(bigDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(bigDir, "mcp.json"), make([]byte, maxMCPConfigBytes+1), 0o600))

	require.Empty(t, loadVibeMCPServers(cwd))
}
