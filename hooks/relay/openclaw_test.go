package relay

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/speakeasy-api/agenthooks"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/hooks/sdk/models/components"
)

// openclawFrame renders one NDJSON frame the way the generated OpenClaw shim
// writes it. ctx carries the run identity on every agent-scope hook.
func openclawFrame(t *testing.T, seq int, hook, runID string, event map[string]any) string {
	t.Helper()
	frame, err := json.Marshal(map[string]any{
		"seq":   seq,
		"hook":  hook,
		"event": event,
		"ctx": map[string]any{
			"agentId":      "main",
			"sessionKey":   "agent:main:main",
			"sessionId":    "openclaw-session",
			"runId":        runID,
			"workspaceDir": "/work",
		},
	})
	require.NoError(t, err)
	return string(frame)
}

// llmInputFrame is an llm_input frame as the Codex app-server harness fires
// it: the turn's prompt with an empty history.
func llmInputFrame(t *testing.T, seq int, runID, prompt string) string {
	t.Helper()
	return openclawFrame(t, seq, "llm_input", runID, map[string]any{
		"runId":           runID,
		"sessionId":       "openclaw-session",
		"provider":        "openai",
		"model":           "gpt-5",
		"prompt":          prompt,
		"historyMessages": []any{},
		"imagesCount":     0,
	})
}

func beforeAgentRunFrame(t *testing.T, seq int, runID, prompt string) string {
	t.Helper()
	return openclawFrame(t, seq, "before_agent_run", runID, map[string]any{
		"prompt": prompt,
	})
}

// serveOpenClaw drives the relay runner's OpenClaw serve loop with frames, as
// the generated shim does. It returns after the loop has handled every frame,
// including the observe frames it hands to its worker.
func serveOpenClaw(t *testing.T, cfg Config, frames ...string) {
	t.Helper()
	t.Setenv("SPEAKEASY_AI_DEVICE_AGENT_COMMANDS", "speakeasy-hooks-test-missing-device-agent")
	t.Setenv("XDG_STATE_HOME", t.TempDir())

	var stdout, stderr bytes.Buffer
	stdin := strings.NewReader(strings.Join(frames, "\n") + "\n")
	code := NewRunner(cfg).Run(t.Context(), []string{"agenthooks", "serve", "--provider=openclaw", "--timeout=60s"}, stdin, &stdout, &stderr)
	require.Equal(t, 0, code, "serve loop failed: %s", stderr.String())
}

// submittedPrompts returns the prompt.submitted requests the server received.
func (fs *fakeServer) submittedPrompts() []components.IngestRequestBody {
	fs.mu.Lock()
	defer fs.mu.Unlock()
	var prompts []components.IngestRequestBody
	for _, req := range fs.requests {
		if req.Event.Type == components.TypePromptSubmitted {
			prompts = append(prompts, req)
		}
	}
	return prompts
}

func requireOpenclawPrompt(t *testing.T, req components.IngestRequestBody, rawEventName, runID, prompt string) {
	t.Helper()
	require.Equal(t, "openclaw", req.Source.Adapter)
	require.NotNil(t, req.Source.RawEventName)
	require.Equal(t, rawEventName, *req.Source.RawEventName)
	require.NotNil(t, req.Session)
	require.NotNil(t, req.Session.ID)
	require.Equal(t, "openclaw-session", *req.Session.ID)
	require.NotNil(t, req.Session.TurnID)
	require.Equal(t, runID, *req.Session.TurnID)
	require.NotNil(t, req.Data)
	require.NotNil(t, req.Data.Prompt)
	require.NotNil(t, req.Data.Prompt.Text)
	require.Equal(t, prompt, *req.Data.Prompt.Text)
}

// The Codex app-server harness fires llm_input but no before_agent_run, so
// llm_input is the only source of the turn's user prompt.
func TestOpenClawLLMInputReportsPromptWithoutBeforeAgentRun(t *testing.T) {
	fs := newFakeServer(t, nil)
	serveOpenClaw(t, authedConfig(t, fs.URL),
		llmInputFrame(t, 1, "run-codex", "summarize the repo"),
		openclawFrame(t, 2, "agent_end", "run-codex", map[string]any{"runId": "run-codex", "success": true}),
	)

	prompts := fs.submittedPrompts()
	require.Len(t, prompts, 1)
	requireOpenclawPrompt(t, prompts[0], "llm_input", "run-codex", "summarize the repo")
}

func TestOpenClawLLMInputDoesNotUploadModelContext(t *testing.T) {
	fs := newFakeServer(t, nil)
	serveOpenClaw(t, authedConfig(t, fs.URL),
		openclawFrame(t, 1, "llm_input", "run-codex", map[string]any{
			"prompt":          "summarize the repo",
			"historyMessages": []any{map[string]any{"role": "user", "content": "private history"}},
			"systemPrompt":    "private system instructions",
			"tools":           []any{map[string]any{"name": "private_tool"}},
		}),
	)

	prompts := fs.submittedPrompts()
	require.Len(t, prompts, 1)
	requireOpenclawPrompt(t, prompts[0], "llm_input", "run-codex", "summarize the repo")
	require.Empty(t, prompts[0].Raw)
	encoded, err := json.Marshal(prompts[0])
	require.NoError(t, err)
	for _, private := range []string{"private history", "private system instructions", "private_tool"} {
		require.NotContains(t, string(encoded), private)
	}
}

// The built-in harnesses fire before_agent_run before llm_input; the gate
// already reported the prompt, so llm_input must not add a second one.
func TestOpenClawLLMInputIgnoredAfterBeforeAgentRun(t *testing.T) {
	fs := newFakeServer(t, nil)
	serveOpenClaw(t, authedConfig(t, fs.URL),
		beforeAgentRunFrame(t, 1, "run-builtin", "fix the tests"),
		llmInputFrame(t, 2, "run-builtin", "fix the tests"),
	)

	prompts := fs.submittedPrompts()
	require.Len(t, prompts, 1)
	requireOpenclawPrompt(t, prompts[0], "before_agent_run", "run-builtin", "fix the tests")
}

func TestOpenClawLLMInputReportsPromptOncePerRun(t *testing.T) {
	fs := newFakeServer(t, nil)
	serveOpenClaw(t, authedConfig(t, fs.URL),
		llmInputFrame(t, 1, "run-codex", "summarize the repo"),
		llmInputFrame(t, 2, "run-codex", "summarize the repo"),
	)

	prompts := fs.submittedPrompts()
	require.Len(t, prompts, 1)
	requireOpenclawPrompt(t, prompts[0], "llm_input", "run-codex", "summarize the repo")
}

func TestOpenClawPromptStateIsPerRun(t *testing.T) {
	fs := newFakeServer(t, nil)
	serveOpenClaw(t, authedConfig(t, fs.URL),
		beforeAgentRunFrame(t, 1, "run-builtin", "fix the tests"),
		llmInputFrame(t, 2, "run-builtin", "fix the tests"),
		llmInputFrame(t, 3, "run-codex-1", "summarize the repo"),
		llmInputFrame(t, 4, "run-codex-2", "now write the docs"),
	)

	prompts := fs.submittedPrompts()
	require.Len(t, prompts, 3)
	requireOpenclawPrompt(t, prompts[0], "before_agent_run", "run-builtin", "fix the tests")
	requireOpenclawPrompt(t, prompts[1], "llm_input", "run-codex-1", "summarize the repo")
	requireOpenclawPrompt(t, prompts[2], "llm_input", "run-codex-2", "now write the docs")
}

// Without a run id the relay cannot tell whether before_agent_run already
// reported the turn, so llm_input reports nothing.
func TestOpenClawLLMInputWithoutRunIDReportsNothing(t *testing.T) {
	fs := newFakeServer(t, nil)
	serveOpenClaw(t, authedConfig(t, fs.URL),
		openclawFrame(t, 1, "llm_input", "", map[string]any{"sessionId": "openclaw-session", "prompt": "summarize the repo"}),
	)

	require.Empty(t, fs.submittedPrompts())
}

func TestOpenClawLLMInputIgnoredForOtherProviders(t *testing.T) {
	fs := newFakeServer(t, nil)
	r := NewRelay(authedConfig(t, fs.URL))

	err := r.onModelRequest(t.Context(), &agenthooks.ModelEvent{Event: agenthooks.Event{
		Provider:   agenthooks.ProviderOpenCode,
		Kind:       agenthooks.KindModelRequest,
		NativeName: "llm_input",
		Session:    agenthooks.SessionInfo{ID: "opencode-session", TurnID: "turn-1"},
		Raw:        json.RawMessage(`{"event":{"prompt":"summarize the repo"}}`),
	}})
	require.NoError(t, err)
	require.Equal(t, 0, fs.count())
}

// shimConst returns the JSON value of a `const NAME = <json>` line in a
// rendered OpenClaw shim.
func shimConst(t *testing.T, shim, name string) string {
	t.Helper()
	for line := range strings.SplitSeq(shim, "\n") {
		if value, ok := strings.CutPrefix(line, "const "+name+" = "); ok {
			return value
		}
	}
	t.Fatalf("shim declares no %s", name)
	return ""
}

func TestWritePluginOpenClawObservesLLMInput(t *testing.T) {
	dir := t.TempDir()
	err := WritePlugin(t.Context(), "openclaw", dir, PluginConfig{
		ServerURL:    "https://gram.test",
		SiteURL:      "",
		ProjectSlug:  "default",
		OrgID:        "org-1",
		HooksAPIKey:  "shared-key",
		BrowserLogin: false,
		BinaryPath:   "/tmp/speakeasy-hooks",
	})
	require.NoError(t, err)

	b, err := os.ReadFile(filepath.Join(dir, "index.js"))
	require.NoError(t, err)
	shim := string(b)

	var hooks []string
	require.NoError(t, json.Unmarshal([]byte(shimConst(t, shim, "HOOKS")), &hooks))
	require.Contains(t, hooks, "llm_input")
	require.Contains(t, hooks, "before_agent_run")

	var gates map[string]int64
	require.NoError(t, json.Unmarshal([]byte(shimConst(t, shim, "GATE_TIMEOUT_MS")), &gates))
	require.NotContains(t, gates, "llm_input", "llm_input is observe-only")
	require.Contains(t, gates, "before_agent_run")
}

func TestOpenClawPromptRunsEvictsOldestRun(t *testing.T) {
	t.Parallel()
	runs := newOpenclawPromptRuns()
	for i := range openclawPromptRunsCap {
		require.True(t, runs.claim(fmt.Sprintf("run-%d", i)))
	}
	require.False(t, runs.claim("run-0"), "a recorded run must not be claimed again")

	require.True(t, runs.claim("run-new"), "a new run must be recorded when the ring is full")
	require.True(t, runs.claim("run-0"), "the oldest run is evicted to make room")
	require.False(t, runs.claim(fmt.Sprintf("run-%d", openclawPromptRunsCap-1)), "newer runs stay recorded")
	require.Len(t, runs.seen, openclawPromptRunsCap)
}

// The Codex app-server harness puts the workspace files and delivery metadata
// ahead of the user's text in llm_input's prompt. Only the user's text is the
// prompt the turn reports.
func TestOpenClawLLMInputReportsOnlyTheUserRequest(t *testing.T) {
	decorated := strings.Join([]string{
		"OpenClaw runtime context for this turn:\nTreat this OpenClaw-provided context as supporting reference.\n\n## /work/BOOTSTRAP.md\n\nprivate workspace file",
		"OpenClaw delivery metadata:\nThis delivery metadata is runtime routing guidance, not the user's request.\nchannel: cli",
		"Current user request:\nsay hello world",
	}, "\n\n")
	fs := newFakeServer(t, nil)
	serveOpenClaw(t, authedConfig(t, fs.URL), llmInputFrame(t, 1, "run-codex", decorated))

	prompts := fs.submittedPrompts()
	require.Len(t, prompts, 1)
	requireOpenclawPrompt(t, prompts[0], "llm_input", "run-codex", "say hello world")
	encoded, err := json.Marshal(prompts[0])
	require.NoError(t, err)
	require.NotContains(t, string(encoded), "private workspace file")
}

func TestOpenclawUserRequest(t *testing.T) {
	t.Parallel()
	const (
		context  = "OpenClaw runtime context for this turn:\nfiles\n\n## /work/NOTES.md\n\nnotes"
		delivery = "OpenClaw delivery metadata:\nThis delivery metadata is runtime routing guidance, not the user's request.\nhint"
	)
	for name, tc := range map[string]struct{ prompt, want string }{
		"plain prompt":               {"summarize the repo", "summarize the repo"},
		"undecorated header is text": {"Current user request:\nsummarize the repo", "Current user request:\nsummarize the repo"},
		"runtime context":            {context + "\n\nCurrent user request:\nsummarize the repo", "summarize the repo"},
		"delivery metadata only":     {delivery + "\n\nCurrent user request:\nhi", "hi"},
		"context and delivery":       {context + "\n\n" + delivery + "\n\nCurrent user request:\nhi", "hi"},
		"request quotes the header after delivery": {
			delivery + "\n\nCurrent user request:\nfix this\n\nCurrent user request:\nlog line",
			"fix this\n\nCurrent user request:\nlog line",
		},
		"request quotes the header after context and delivery": {
			context + "\n\n" + delivery + "\n\nCurrent user request:\nfix this\n\nCurrent user request:\nlog line",
			"fix this\n\nCurrent user request:\nlog line",
		},
		"workspace file quotes the header": {
			"OpenClaw runtime context for this turn:\nCurrent user request:\nold\n\nCurrent user request:\nnew",
			"new",
		},
		"user mentions the header inline": {context + "\n\nCurrent user request:\nwhat does \"Current user request:\" mean", "what does \"Current user request:\" mean"},
		"context without header":          {context, context},
		"multi-line request":              {context + "\n\nCurrent user request:\nline one\nline two", "line one\nline two"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tc.want, openclawUserRequest(tc.prompt))
		})
	}
}

// toolCompletions returns the tool.completed and tool.failed requests the
// server received, keyed by tool call id.
func (fs *fakeServer) toolCompletions() map[string]components.IngestRequestBody {
	fs.mu.Lock()
	defer fs.mu.Unlock()
	completions := map[string]components.IngestRequestBody{}
	for _, req := range fs.requests {
		if req.Event.Type != components.TypeToolCompleted && req.Event.Type != components.TypeToolFailed {
			continue
		}
		if req.Data == nil || req.Data.ToolCall == nil || req.Data.ToolCall.ID == nil {
			continue
		}
		completions[*req.Data.ToolCall.ID] = req
	}
	return completions
}

func afterToolCallFrame(t *testing.T, seq int, runID, callID, toolName string, result map[string]any) string {
	t.Helper()
	return openclawFrame(t, seq, "after_tool_call", runID, map[string]any{
		"toolName":   toolName,
		"params":     map[string]any{"query": "status"},
		"runId":      runID,
		"toolCallId": callID,
		"result":     result,
		"durationMs": 12,
	})
}

// OpenClaw names MCP tools <server>__<tool>, which the agenthooks name
// decoder does not recognize, so the server name comes from the
// details.mcpServer marker OpenClaw puts on MCP tool results.
func TestOpenClawMCPToolResultAttributesServer(t *testing.T) {
	fs := newFakeServer(t, nil)
	serveOpenClaw(t, authedConfig(t, fs.URL),
		afterToolCallFrame(t, 1, "run-mcp", "call-mcp", "speakeasy__execute_tool", map[string]any{
			"content": []any{map[string]any{"type": "text", "text": "tool failed"}},
			"details": map[string]any{"mcpServer": "speakeasy", "mcpTool": "execute_tool", "status": "error"},
		}),
		afterToolCallFrame(t, 2, "run-mcp", "call-native", "exec", map[string]any{
			"content": []any{map[string]any{"type": "text", "text": "ok"}},
			"details": map[string]any{"exitCode": 0, "status": "completed"},
		}),
		openclawFrame(t, 3, "agent_end", "run-mcp", map[string]any{"runId": "run-mcp", "success": true}),
	)

	completions := fs.toolCompletions()
	require.Len(t, completions, 2)

	mcpCall := completions["call-mcp"]
	require.Equal(t, components.TypeToolCompleted, mcpCall.Event.Type)
	require.NotNil(t, mcpCall.Data.ToolCall.Name)
	require.Equal(t, "speakeasy__execute_tool", *mcpCall.Data.ToolCall.Name, "the native tool name is kept verbatim")
	require.NotNil(t, mcpCall.Data.Mcp)
	require.NotNil(t, mcpCall.Data.Mcp.ServerName)
	require.Equal(t, "speakeasy", *mcpCall.Data.Mcp.ServerName)
	require.NotNil(t, mcpCall.Data.Mcp.ServerIdentity)
	require.Equal(t, "speakeasy", *mcpCall.Data.Mcp.ServerIdentity)
	require.Nil(t, mcpCall.Data.Mcp.URL)
	require.Nil(t, mcpCall.Data.Mcp.Command)
	require.NotNil(t, mcpCall.Data.Mcp.ResultJSON)
	require.Contains(t, *mcpCall.Data.Mcp.ResultJSON, `"mcpServer":"speakeasy"`)

	native := completions["call-native"]
	require.Equal(t, components.TypeToolCompleted, native.Event.Type)
	require.Nil(t, native.Data.Mcp, "a native tool result carries no MCP marker")
}

func TestOpenClawMCPCallReadsDetailsMarker(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		results []json.RawMessage
		server  string
		tool    string
	}{
		{
			name:    "it reads the server and tool from details",
			results: []json.RawMessage{json.RawMessage(`{"content":[],"details":{"mcpServer":"speakeasy","mcpTool":"execute_tool"}}`)},
			server:  "speakeasy",
			tool:    "execute_tool",
		},
		{
			name:    "it falls back to the error payload when there is no output",
			results: []json.RawMessage{nil, json.RawMessage(`{"details":{"mcpServer":" github ","mcpTool":"search"}}`)},
			server:  "github",
			tool:    "search",
		},
		{
			name:    "it ignores native tool details",
			results: []json.RawMessage{json.RawMessage(`{"content":[],"details":{"exitCode":0,"status":"completed"}}`)},
		},
		{
			name:    "it ignores an empty server name",
			results: []json.RawMessage{json.RawMessage(`{"details":{"mcpServer":"","mcpTool":"execute_tool"}}`)},
		},
		{
			name:    "it ignores a string error payload",
			results: []json.RawMessage{json.RawMessage(`"blocked: denied by policy"`)},
		},
		{
			name:    "it ignores non-object details",
			results: []json.RawMessage{json.RawMessage(`{"details":"speakeasy"}`)},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := openClawMCPCall(tc.results...)
			if tc.server == "" {
				require.Nil(t, got)
				return
			}
			require.NotNil(t, got)
			require.Equal(t, tc.server, got.Server)
			require.Equal(t, tc.tool, got.Tool)
		})
	}
}
