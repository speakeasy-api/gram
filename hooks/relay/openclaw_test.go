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
