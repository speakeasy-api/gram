package relay

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/speakeasy-api/agenthooks"
	"github.com/speakeasy-api/agenthooks/agenthookstest"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/hooks/sdk/models/components"
)

// fakeServer captures ingest requests and returns a scripted decision.
type fakeServer struct {
	*httptest.Server
	mu       sync.Mutex
	requests []components.IngestRequestBody
	headers  []http.Header
	respond  func(components.IngestRequestBody) (int, decision)
}

func newFakeServer(t *testing.T, respond func(components.IngestRequestBody) (int, decision)) *fakeServer {
	t.Helper()
	fs := &fakeServer{Server: nil, mu: sync.Mutex{}, requests: nil, headers: nil, respond: respond}
	fs.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var p components.IngestRequestBody
		_ = json.Unmarshal(body, &p)
		fs.mu.Lock()
		fs.requests = append(fs.requests, p)
		fs.headers = append(fs.headers, r.Header.Clone())
		fs.mu.Unlock()

		status, dec := http.StatusOK, decision{Decision: "allow", Reason: "", Message: ""}
		if fs.respond != nil {
			status, dec = fs.respond(p)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(dec)
	}))
	t.Cleanup(fs.Close)
	return fs
}

func (fs *fakeServer) count() int {
	fs.mu.Lock()
	defer fs.mu.Unlock()
	return len(fs.requests)
}

func (fs *fakeServer) last() components.IngestRequestBody {
	fs.mu.Lock()
	defer fs.mu.Unlock()
	return fs.requests[len(fs.requests)-1]
}

// invoke runs the relay runner against a fixture exactly as a provider would.
func invoke(t *testing.T, cfg Config, provider agenthooks.Provider, fixture string) agenthookstest.Result {
	t.Helper()
	runner := NewRunner(cfg)
	payload := agenthookstest.Fixture(t, fixture)
	return agenthookstest.Invoke(t, runner, provider, payload, "--variant=cli")
}

func authedConfig(t *testing.T, serverURL string) Config {
	t.Helper()
	t.Setenv("GRAM_HOOKS_AUTH_FILE", filepath.Join(t.TempDir(), "hooks-auth.env"))
	t.Setenv("GRAM_HOOKS_API_KEY", "test-hooks-key")
	t.Setenv("GRAM_HOOKS_DISABLE_LOCAL_AUTH", "1")
	return Config{ServerURL: serverURL, ProjectSlug: "default", OrgID: "", Nonblocking: false}
}

func TestEnvelopeClaudePreToolUse(t *testing.T) {
	payload := agenthookstest.Fixture(t, "claude/pre_tool_use.json")
	runner := agenthooks.New()
	var got components.IngestRequestBody
	runner.OnToolPre(func(_ context.Context, e *agenthooks.ToolPreEvent) (agenthooks.ToolPreDecision, error) {
		got = buildEnvelope(e, "test-host")
		return agenthooks.NoDecision(), nil
	})
	agenthookstest.Invoke(t, runner, agenthooks.ProviderClaudeCode, payload)

	require.Equal(t, schemaVersion, got.SchemaVersion)
	require.Equal(t, "claude", got.Source.Adapter)
	require.NotNil(t, got.Source.RawEventName)
	require.Equal(t, "PreToolUse", *got.Source.RawEventName)
	require.Equal(t, components.TypeToolRequested, got.Event.Type)
	require.NotNil(t, got.Session)
	require.NotNil(t, got.Session.ID)
	require.Equal(t, "sess-claude-1", *got.Session.ID)
	require.NotNil(t, got.Data)
	require.NotNil(t, got.Data.ToolCall)
	require.NotNil(t, got.Data.ToolCall.Name)
	require.Equal(t, "Bash", *got.Data.ToolCall.Name)
	require.NotNil(t, got.Data.ToolCall.ID)
	require.Equal(t, "toolu_01ABC", *got.Data.ToolCall.ID)
	require.NotEmpty(t, got.Raw)
}

func TestEnvelopeClaudeMCPToolResolvesServer(t *testing.T) {
	payload := agenthookstest.Fixture(t, "claude/pre_tool_use_mcp.json")
	runner := agenthooks.New(agenthooks.WithoutMCPResolution())
	var got components.IngestRequestBody
	runner.OnToolPre(func(_ context.Context, e *agenthooks.ToolPreEvent) (agenthooks.ToolPreDecision, error) {
		got = buildEnvelope(e, "test-host")
		return agenthooks.NoDecision(), nil
	})
	agenthookstest.Invoke(t, runner, agenthooks.ProviderClaudeCode, payload)

	require.Equal(t, components.TypeToolRequested, got.Event.Type)
	require.NotNil(t, got.Data.Mcp)
	require.NotNil(t, got.Data.Mcp.ServerName)
	require.Equal(t, "github", *got.Data.Mcp.ServerName)
	require.NotNil(t, got.Data.Mcp.ServerIdentity)
	require.Equal(t, "github", *got.Data.Mcp.ServerIdentity)
}

func TestEnvelopeClaudeSkillReclassifies(t *testing.T) {
	payload := []byte(`{"session_id":"s1","hook_event_name":"PreToolUse","tool_name":"Skill","tool_input":{"skill":"my-skill"},"tool_use_id":"t1"}`)
	runner := agenthooks.New()
	var got components.IngestRequestBody
	runner.OnToolPre(func(_ context.Context, e *agenthooks.ToolPreEvent) (agenthooks.ToolPreDecision, error) {
		got = buildEnvelope(e, "h")
		return agenthooks.NoDecision(), nil
	})
	agenthookstest.Invoke(t, runner, agenthooks.ProviderClaudeCode, payload)

	require.Equal(t, components.TypeSkillActivated, got.Event.Type)
	require.NotNil(t, got.Data.Skill)
	require.Equal(t, "my-skill", got.Data.Skill.Name)
}

func TestIngestAllowSendsAuthenticatedRequest(t *testing.T) {
	fs := newFakeServer(t, func(components.IngestRequestBody) (int, decision) {
		return http.StatusOK, decision{Decision: "allow", Reason: "", Message: ""}
	})
	cfg := authedConfig(t, fs.URL)
	res := invoke(t, cfg, agenthooks.ProviderClaudeCode, "claude/pre_tool_use.json")

	require.Equal(t, 0, res.ExitCode)
	require.Equal(t, "{}", string(bytes.TrimSpace(res.Stdout)))
	require.Equal(t, 1, fs.count())
	require.Equal(t, "test-hooks-key", fs.headers[0].Get("Gram-Key"))
	require.Equal(t, "default", fs.headers[0].Get("Gram-Project"))
	require.NotEmpty(t, fs.headers[0].Get("Idempotency-Key"))
	require.Equal(t, components.TypeToolRequested, fs.last().Event.Type)
	// The verbatim provider payload must cross the wire as a JSON object, not
	// a base64 rendering of the raw bytes.
	require.IsType(t, map[string]any{}, fs.last().Raw)
}

func TestIngestDenyBlocksToolCall(t *testing.T) {
	fs := newFakeServer(t, func(components.IngestRequestBody) (int, decision) {
		return http.StatusOK, decision{Decision: "deny", Reason: "policy_denied", Message: "blocked by policy X"}
	})
	cfg := authedConfig(t, fs.URL)
	res := invoke(t, cfg, agenthooks.ProviderClaudeCode, "claude/pre_tool_use.json")

	require.Equal(t, 0, res.ExitCode)
	require.Contains(t, string(res.Stdout), `"permissionDecision":"deny"`)
	require.Contains(t, string(res.Stdout), "blocked by policy X")
}

func TestIngestDenyBlocksPrompt(t *testing.T) {
	fs := newFakeServer(t, func(components.IngestRequestBody) (int, decision) {
		return http.StatusOK, decision{Decision: "deny", Reason: "policy_denied", Message: "prompt blocked"}
	})
	cfg := authedConfig(t, fs.URL)
	res := invoke(t, cfg, agenthooks.ProviderClaudeCode, "claude/user_prompt_submit.json")

	require.Equal(t, 0, res.ExitCode)
	require.Contains(t, string(res.Stdout), `"decision":"block"`)
	require.Contains(t, string(res.Stdout), "prompt blocked")
}

func TestTelemetryEventIsFireAndForget(t *testing.T) {
	fs := newFakeServer(t, func(components.IngestRequestBody) (int, decision) {
		return http.StatusOK, decision{Decision: "allow", Reason: "", Message: ""}
	})
	cfg := authedConfig(t, fs.URL)
	res := invoke(t, cfg, agenthooks.ProviderClaudeCode, "claude/post_tool_use.json")

	require.Equal(t, 0, res.ExitCode)
	require.Equal(t, 1, fs.count())
	require.Equal(t, components.TypeToolCompleted, fs.last().Event.Type)
	require.NotNil(t, fs.last().Data.ToolCall)
	require.NotEmpty(t, fs.last().Data.ToolCall.Output)
}

func TestNonBlockingSwallowsDeny(t *testing.T) {
	fs := newFakeServer(t, func(components.IngestRequestBody) (int, decision) {
		return http.StatusOK, decision{Decision: "deny", Reason: "policy_denied", Message: "would block"}
	})
	cfg := authedConfig(t, fs.URL)
	cfg.Nonblocking = true
	res := invoke(t, cfg, agenthooks.ProviderClaudeCode, "claude/pre_tool_use.json")

	require.Equal(t, 0, res.ExitCode)
	require.Equal(t, "{}", string(bytes.TrimSpace(res.Stdout)))
}

func TestRatchetNeverAuthedFailsOpen(t *testing.T) {
	fs := newFakeServer(t, nil)
	t.Setenv("GRAM_HOOKS_AUTH_FILE", filepath.Join(t.TempDir(), "hooks-auth.env"))
	t.Setenv("GRAM_HOOKS_DISABLE_LOCAL_AUTH", "1")
	t.Setenv("GRAM_HOOKS_API_KEY", "")
	t.Setenv("GRAM_API_KEY", "")
	cfg := Config{ServerURL: fs.URL, ProjectSlug: "default", OrgID: "", Nonblocking: false}

	res := invoke(t, cfg, agenthooks.ProviderClaudeCode, "claude/pre_tool_use.json")

	require.Equal(t, 0, res.ExitCode, "never-authenticated machine must not block")
	require.Equal(t, "{}", string(bytes.TrimSpace(res.Stdout)))
	require.Equal(t, 0, fs.count(), "no events may leak before authentication")
}

func TestRatchetEstablishedFailsClosed(t *testing.T) {
	fs := newFakeServer(t, nil)
	authFile := filepath.Join(t.TempDir(), "hooks-auth.env")
	require.NoError(t, os.WriteFile(authFile+".established", []byte{}, 0o600))
	t.Setenv("GRAM_HOOKS_AUTH_FILE", authFile)
	t.Setenv("GRAM_HOOKS_DISABLE_LOCAL_AUTH", "1")
	t.Setenv("GRAM_HOOKS_API_KEY", "")
	t.Setenv("GRAM_API_KEY", "")
	cfg := Config{ServerURL: fs.URL, ProjectSlug: "default", OrgID: "", Nonblocking: false}

	res := invoke(t, cfg, agenthooks.ProviderClaudeCode, "claude/pre_tool_use.json")

	require.Contains(t, string(res.Stdout), `"permissionDecision":"deny"`, "established machine with broken creds must fail closed")
	require.Equal(t, 0, fs.count())
}

// TestAuthRejectedForgetsCachedKey covers the 401 recovery path: a
// cache-sourced key the server rejects is forgotten, the established marker
// survives, and the gating call fails closed.
func TestAuthRejectedForgetsCachedKey(t *testing.T) {
	fs := newFakeServer(t, func(components.IngestRequestBody) (int, decision) {
		return http.StatusUnauthorized, decision{Decision: "", Reason: "", Message: ""}
	})
	authFile := filepath.Join(t.TempDir(), "hooks-auth.env")
	require.NoError(t, os.WriteFile(authFile, []byte("server_url="+fs.URL+"\napi_key=revoked-key\nproject=default\n"), 0o600))
	require.NoError(t, os.WriteFile(authFile+".established", []byte{}, 0o600))
	t.Setenv("GRAM_HOOKS_AUTH_FILE", authFile)
	t.Setenv("GRAM_HOOKS_API_KEY", "")
	t.Setenv("GRAM_API_KEY", "")
	cfg := Config{ServerURL: fs.URL, ProjectSlug: "default", OrgID: "", Nonblocking: false}

	res := invoke(t, cfg, agenthooks.ProviderClaudeCode, "claude/pre_tool_use.json")

	require.Contains(t, string(res.Stdout), `"permissionDecision":"deny"`, "a rejected credential must fail closed")
	_, statErr := os.Stat(authFile)
	require.True(t, os.IsNotExist(statErr), "the rejected cached key must be forgotten")
	require.True(t, authEstablished(), "forgetting the key must not reopen the ratchet")
}

// TestServerErrorBlocksToolCall pins the non-2xx posture with credentials
// present: the server never confirmed the action, so blocking mode denies. A
// 4xx status exercises the same branch as 5xx without the retry budget's
// wall-clock cost (5xx is retried for up to 30s).
func TestServerErrorBlocksToolCall(t *testing.T) {
	fs := newFakeServer(t, func(components.IngestRequestBody) (int, decision) {
		return http.StatusBadRequest, decision{Decision: "", Reason: "", Message: ""}
	})
	cfg := authedConfig(t, fs.URL)
	res := invoke(t, cfg, agenthooks.ProviderClaudeCode, "claude/pre_tool_use.json")

	require.Contains(t, string(res.Stdout), `"permissionDecision":"deny"`, "an unconfirmed action must not proceed in blocking mode")
	require.Contains(t, string(res.Stdout), "HTTP 400")
}

// TestServerErrorPassesWhenNonblocking mirrors TestServerErrorBlocksToolCall
// under observability mode: transport failures must not block the agent.
func TestServerErrorPassesWhenNonblocking(t *testing.T) {
	fs := newFakeServer(t, func(components.IngestRequestBody) (int, decision) {
		return http.StatusBadRequest, decision{Decision: "", Reason: "", Message: ""}
	})
	cfg := authedConfig(t, fs.URL)
	cfg.Nonblocking = true
	res := invoke(t, cfg, agenthooks.ProviderClaudeCode, "claude/pre_tool_use.json")

	require.Equal(t, 0, res.ExitCode)
	require.Equal(t, "{}", string(bytes.TrimSpace(res.Stdout)))
}

// TestCachedAuthUsesConfigProject pins that the plugin's configured project
// always wins over the project recorded in the shared credential cache, so a
// key minted in another workspace cannot route this workspace's events there.
func TestCachedAuthUsesConfigProject(t *testing.T) {
	authFile := filepath.Join(t.TempDir(), "hooks-auth.env")
	require.NoError(t, os.WriteFile(authFile, []byte("server_url=https://gram.test\napi_key=key-1\nproject=other-workspace\norg=org-1\n"), 0o600))
	t.Setenv("GRAM_HOOKS_AUTH_FILE", authFile)

	c, ok := readCachedAuth(Config{ServerURL: "https://gram.test", ProjectSlug: "this-workspace", OrgID: "org-1", Nonblocking: false, DebugLog: "", ConfigPath: ""})
	require.True(t, ok)
	require.Equal(t, "this-workspace", c.Project)

	c, ok = readCachedAuth(Config{ServerURL: "https://gram.test", ProjectSlug: "", OrgID: "org-1", Nonblocking: false, DebugLog: "", ConfigPath: ""})
	require.True(t, ok)
	require.Equal(t, "other-workspace", c.Project, "cache project remains the fallback when the config has none")
}

// TestCursorModelResponseRelaysMessage covers the interactive-only cursor
// events the e2e harness cannot exercise (headless Cursor does not reliably
// emit afterAgentResponse): the assistant message text and token usage must
// reach the server as assistant.responded.
func TestCursorModelResponseRelaysMessage(t *testing.T) {
	fs := newFakeServer(t, nil)
	cfg := authedConfig(t, fs.URL)
	payload := []byte(`{"hook_event_name":"afterAgentResponse","conversation_id":"sess-cursor-1","text":"final answer","input_tokens":10,"output_tokens":5}`)

	runner := NewRunner(cfg)
	res := agenthookstest.Invoke(t, runner, agenthooks.ProviderCursor, payload, "--variant=cli")

	require.Equal(t, 0, res.ExitCode)
	require.Equal(t, 1, fs.count())
	last := fs.last()
	require.Equal(t, components.TypeAssistantResponded, last.Event.Type)
	require.NotNil(t, last.Data)
	require.NotNil(t, last.Data.Message)
	require.NotNil(t, last.Data.Message.Text)
	require.Equal(t, "final answer", *last.Data.Message.Text)
	require.NotNil(t, last.Data.Message.Role)
	require.Equal(t, "assistant", *last.Data.Message.Role)
	require.NotNil(t, last.Data.Usage)
	require.Equal(t, int64(10), *last.Data.Usage.InputTokens)
	require.Equal(t, int64(5), *last.Data.Usage.OutputTokens)
}

// TestLoginCommandCarriesConfig pins the nudge → login contract: the sign-in
// command must reference the plugin's speakeasy.json so the minted credential
// matches the server/project the hook path authenticates against.
func TestLoginCommandCarriesConfig(t *testing.T) {
	cfgPath := filepath.Join(t.TempDir(), "speakeasy.json")
	require.NoError(t, os.WriteFile(cfgPath, []byte(`{"server_url":"https://gram.test","project":"acme"}`), 0o600))

	got, rest := SplitInlineFlags(Config{ServerURL: "", ProjectSlug: "", OrgID: "", Nonblocking: false, DebugLog: "", ConfigPath: ""}, []string{"--config=" + cfgPath, "--force"})
	require.Equal(t, []string{"--force"}, rest)
	require.Equal(t, cfgPath, got.ConfigPath)
	require.Equal(t, "https://gram.test", got.ServerURL)
	require.Equal(t, "acme", got.ProjectSlug)

	require.Contains(t, NewRelay(got).loginCommand(), " login --config="+cfgPath)
}

// TestLoginCommandQuotesUnsafePaths ensures the nudge command survives shell
// parsing when the plugin lives under a path with spaces.
func TestLoginCommandQuotesUnsafePaths(t *testing.T) {
	cfgPath := filepath.Join(t.TempDir(), "plugin dir", "speakeasy.json")
	cmd := NewRelay(Config{ServerURL: "https://gram.test", ProjectSlug: "acme", OrgID: "", Nonblocking: false, DebugLog: "", ConfigPath: cfgPath}).loginCommand()
	require.Contains(t, cmd, " --config='"+cfgPath+"'")
}

func TestNudgeEmittedOncePerSession(t *testing.T) {
	fs := newFakeServer(t, nil)
	t.Setenv("GRAM_HOOKS_AUTH_FILE", filepath.Join(t.TempDir(), "hooks-auth.env"))
	t.Setenv("GRAM_HOOKS_DISABLE_LOCAL_AUTH", "1")
	// The nudge marker lands in os.TempDir keyed by the fixture's fixed
	// session id; isolate it so reruns start unclaimed.
	t.Setenv("TMPDIR", t.TempDir())
	t.Setenv("GRAM_HOOKS_API_KEY", "")
	t.Setenv("GRAM_API_KEY", "")
	cfg := Config{ServerURL: fs.URL, ProjectSlug: "default", OrgID: "", Nonblocking: false}

	first := invoke(t, cfg, agenthooks.ProviderClaudeCode, "claude/user_prompt_submit.json")
	require.Equal(t, 0, first.ExitCode)
	require.Contains(t, string(first.Stdout), "additionalContext")
	require.Contains(t, string(first.Stdout), "login")

	second := invoke(t, cfg, agenthooks.ProviderClaudeCode, "claude/user_prompt_submit.json")
	require.NotContains(t, string(second.Stdout), "additionalContext", "nudge fires at most once per session")
}
