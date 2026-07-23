package relay

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"maps"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

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
	// effects, when set, is merged into the response body alongside the
	// decision, mirroring the server's org_settings side channel.
	effects func(components.IngestRequestBody) map[string]any
}

func newFakeServer(t *testing.T, respond func(components.IngestRequestBody) (int, decision)) *fakeServer {
	t.Helper()
	fs := &fakeServer{Server: nil, mu: sync.Mutex{}, requests: nil, headers: nil, respond: respond, effects: nil}
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
		out := struct {
			decision
			Effects map[string]any `json:"effects,omitempty"`
		}{decision: dec, Effects: nil}
		if fs.effects != nil {
			out.Effects = fs.effects(p)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(out)
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
// spoolStateHome is the XDG_STATE_HOME a test installed via
// setSpoolStateHome. invoke preserves exactly that value and overrides
// anything else: an ambient XDG_STATE_HOME (CI sandboxes commonly export a
// tmp-located one) would be shared across all tests, and a shared spool plus
// a successful send would exec the TEST BINARY as a detached drain.
var spoolStateHome string

// setSpoolStateHome points the spool at a per-test directory and registers
// it as the one value invoke will preserve. Tests that inspect or seed the
// spool call this before invoke.
func setSpoolStateHome(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("XDG_STATE_HOME", dir)
	spoolStateHome = dir
	t.Cleanup(func() { spoolStateHome = "" })
	return dir
}

func invoke(t *testing.T, cfg Config, provider agenthooks.Provider, fixture string) agenthookstest.Result {
	t.Helper()
	t.Setenv("GRAM_DEVICE_AGENT_COMMANDS", "speakeasy-hooks-test-missing-device-agent")
	if v := os.Getenv("XDG_STATE_HOME"); v == "" || v != spoolStateHome {
		t.Setenv("XDG_STATE_HOME", t.TempDir())
	}
	runner := NewRunner(cfg)
	payload := agenthookstest.Fixture(t, fixture)
	return agenthookstest.Invoke(t, runner, provider, payload, "--variant=cli")
}

func authedConfig(t *testing.T, serverURL string) Config {
	t.Helper()
	t.Setenv("GRAM_HOOKS_AUTH_FILE", filepath.Join(t.TempDir(), "hooks-auth.env"))
	t.Setenv("GRAM_HOOKS_API_KEY", "test-hooks-key")
	t.Setenv("GRAM_HOOKS_DISABLE_LOCAL_AUTH", "1")
	return Config{ServerURL: serverURL, ProjectSlug: "default", OrgID: "", HooksAPIKey: "", BrowserLogin: false, Nonblocking: false, DebugLog: "", ConfigPath: "", ConfigError: ""}
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

// TestEnvelopeCursorMCPRedactsRawTransport pins that the verbatim raw payload
// gets the same MCP transport redaction as the structured mcp block: cursor
// ships the server url/command inside the hook payload itself, so credentials
// there must not leave the machine.
func TestEnvelopeCursorMCPRedactsRawTransport(t *testing.T) {
	// agenthooks writes cursor dedup markers to os.TempDir; isolate them so
	// reruns don't classify the fixture as a duplicate delivery.
	t.Setenv("TMPDIR", t.TempDir())
	payload := []byte(`{"conversation_id":"conv-1","hook_event_name":"beforeMCPExecution","tool_name":"MCP:create_issue","tool_input":"{}","url":"https://user:hunter2@mcp.example.com/sse?api_key=supersecret","command":"GITHUB_TOKEN=ghp_secret999 npx -y srv --token=abc123"}`)
	runner := agenthooks.New(agenthooks.WithoutMCPResolution())
	var got components.IngestRequestBody
	runner.OnToolPre(func(_ context.Context, e *agenthooks.ToolPreEvent) (agenthooks.ToolPreDecision, error) {
		got = buildEnvelope(e, "test-host")
		return agenthooks.NoDecision(), nil
	})
	agenthookstest.Invoke(t, runner, agenthooks.ProviderCursor, payload)

	require.NotNil(t, got.Data)
	require.NotNil(t, got.Data.Mcp)
	require.NotNil(t, got.Data.Mcp.URL)
	require.NotContains(t, *got.Data.Mcp.URL, "hunter2")
	require.NotContains(t, *got.Data.Mcp.URL, "supersecret")

	rawJSON, err := json.Marshal(got.Raw)
	require.NoError(t, err)
	require.NotContains(t, string(rawJSON), "hunter2")
	require.NotContains(t, string(rawJSON), "supersecret")
	require.NotContains(t, string(rawJSON), "abc123")
	require.NotContains(t, string(rawJSON), "ghp_secret999", "env-assignment credentials must be masked")
	require.Contains(t, string(rawJSON), "GITHUB_TOKEN=***")
	require.Contains(t, string(rawJSON), "mcp.example.com", "redaction must keep the host so evidence stays matchable")
	require.Contains(t, string(rawJSON), "conv-1", "unrelated raw fields must survive untouched")
}

// TestScrubRawPayloadRunsOnEveryEvent pins the legacy scrub posture: raw is
// rewritten wherever a payload carries secret-bearing top-level keys, not only
// when the structured mcp block was populated.
func TestScrubRawPayloadRunsOnEveryEvent(t *testing.T) {
	scrubbed := scrubRawPayload([]byte(`{"hook_event_name":"beforeShellExecution","command":"curl -H authorization: bearer sekrit-1"}`))
	require.NotContains(t, string(scrubbed), "sekrit-1")

	verbatim := []byte(`{"hook_event_name":"stop","final_message":"done",  "spacing":"preserved"}`)
	require.Equal(t, string(verbatim), string(scrubRawPayload(verbatim)), "payloads needing no rewrite pass through verbatim")
}

// TestSendRetriesTransientConnectionFailure pins the relay-level transport
// replay: the SDK does not retry connection errors on POST, so a dropped
// connection must be retried here instead of denying a blocking hook.
func TestSendRetriesTransientConnectionFailure(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	t.Cleanup(func() { _ = ln.Close() })

	mux := http.NewServeMux()
	var served atomic.Int32
	mux.HandleFunc("/", func(w http.ResponseWriter, _ *http.Request) {
		served.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"decision":"allow"}`))
	})
	// Kill the first connection before any response, then serve normally.
	killed := make(chan struct{})
	go func() {
		conn, acceptErr := ln.Accept()
		if acceptErr == nil {
			_ = conn.Close()
		}
		close(killed)
		_ = http.Serve(ln, mux)
	}()

	cl := newClient("http://" + ln.Addr().String())
	res := cl.send(t.Context(), creds{ServerURL: "", APIKey: "k", Project: "p", Email: "", Org: "", Source: credEnv}, components.IngestRequestBody{
		SchemaVersion: schemaVersion,
		Source:        components.HookIngestSource{Adapter: "claude", AdapterVersion: nil, RawEventName: nil, Hostname: nil, UserEmail: nil},
		Session:       nil,
		Event:         components.HookIngestEvent{Type: components.TypeSessionUpdated, OccurredAt: nil},
		Data:          nil,
		Raw:           nil,
	}, newIdempotencyToken())

	<-killed
	require.Equal(t, http.StatusOK, res.statusCode, "a transient connection drop must be replayed, not surfaced")
	require.GreaterOrEqual(t, served.Load(), int32(1))
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

// TestLegacyNonblockingEnforcesDeny pins the observability-mode removal: a
// plugin config still carrying the legacy nonblocking flag no longer swallows
// explicit deny decisions — only the fail-open (outage) posture survives.
func TestLegacyNonblockingEnforcesDeny(t *testing.T) {
	fs := newFakeServer(t, func(components.IngestRequestBody) (int, decision) {
		return http.StatusOK, decision{Decision: "deny", Reason: "policy_denied", Message: "blocked by policy"}
	})
	cfg := authedConfig(t, fs.URL)
	cfg.Nonblocking = true
	res := invoke(t, cfg, agenthooks.ProviderClaudeCode, "claude/pre_tool_use.json")

	require.Contains(t, string(res.Stdout), `"permissionDecision":"deny"`, "legacy nonblocking must not swallow explicit denies")
	require.Contains(t, string(res.Stdout), "blocked by policy")
}

func TestRatchetNeverAuthedFailsOpen(t *testing.T) {
	fs := newFakeServer(t, nil)
	t.Setenv("GRAM_HOOKS_AUTH_FILE", filepath.Join(t.TempDir(), "hooks-auth.env"))
	t.Setenv("GRAM_HOOKS_DISABLE_LOCAL_AUTH", "1")
	t.Setenv("GRAM_HOOKS_API_KEY", "")
	cfg := Config{ServerURL: fs.URL, ProjectSlug: "default", OrgID: "", HooksAPIKey: "", BrowserLogin: false, Nonblocking: false, DebugLog: "", ConfigPath: "", ConfigError: ""}

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
	cfg := Config{ServerURL: fs.URL, ProjectSlug: "default", OrgID: "", HooksAPIKey: "", BrowserLogin: false, Nonblocking: false, DebugLog: "", ConfigPath: "", ConfigError: ""}

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
	cfg := Config{ServerURL: fs.URL, ProjectSlug: "default", OrgID: "", HooksAPIKey: "", BrowserLogin: false, Nonblocking: false, DebugLog: "", ConfigPath: "", ConfigError: ""}

	res := invoke(t, cfg, agenthooks.ProviderClaudeCode, "claude/pre_tool_use.json")

	require.Contains(t, string(res.Stdout), `"permissionDecision":"deny"`, "a rejected credential must fail closed")
	_, statErr := os.Stat(authFile)
	require.True(t, os.IsNotExist(statErr), "the rejected cached key must be forgotten")
	require.True(t, authEstablished(), "forgetting the key must not reopen the ratchet")
	require.FileExists(t, authFile+".reauth-needed", "the rejection must leave the reconnect marker")
}

func TestOrgKeyFallbackSendsWithoutPersonalCredential(t *testing.T) {
	fs := newFakeServer(t, nil)
	t.Setenv("GRAM_HOOKS_AUTH_FILE", filepath.Join(t.TempDir(), "hooks-auth.env"))
	t.Setenv("GRAM_HOOKS_API_KEY", "")
	cfg := Config{ServerURL: fs.URL, ProjectSlug: "default", OrgID: "org-1", HooksAPIKey: "shared-org-key", BrowserLogin: false, Nonblocking: false, DebugLog: "", ConfigPath: "", ConfigError: ""}

	res := invoke(t, cfg, agenthooks.ProviderClaudeCode, "claude/pre_tool_use.json")

	require.Equal(t, 0, res.ExitCode)
	require.Equal(t, 1, fs.count())
	require.Equal(t, "shared-org-key", fs.headers[0].Get("Gram-Key"))
	require.NoFileExists(t, authFilePath(), "the shared key must never be cached locally")
}

func TestRejectedCacheRetriesThroughOrgKeyWithCachedIdentity(t *testing.T) {
	var requests atomic.Int32
	fs := newFakeServer(t, func(components.IngestRequestBody) (int, decision) {
		if requests.Add(1) == 1 {
			return http.StatusUnauthorized, decision{Decision: "", Reason: "", Message: "stale key"}
		}
		return http.StatusOK, decision{Decision: "allow", Reason: "", Message: ""}
	})
	authFile := filepath.Join(t.TempDir(), "hooks-auth.env")
	require.NoError(t, os.WriteFile(authFile, []byte("server_url="+fs.URL+"\napi_key=revoked-key\nproject=default\nemail=developer@example.com\norg=org-1\n"), 0o600))
	t.Setenv("GRAM_HOOKS_AUTH_FILE", authFile)
	t.Setenv("GRAM_HOOKS_API_KEY", "")
	cfg := Config{ServerURL: fs.URL, ProjectSlug: "default", OrgID: "org-1", HooksAPIKey: "shared-org-key", BrowserLogin: false, Nonblocking: false, DebugLog: "", ConfigPath: "", ConfigError: ""}

	res := invoke(t, cfg, agenthooks.ProviderClaudeCode, "claude/pre_tool_use.json")

	require.Equal(t, 0, res.ExitCode)
	require.Equal(t, 2, fs.count())
	require.Equal(t, "revoked-key", fs.headers[0].Get("Gram-Key"))
	require.Equal(t, "shared-org-key", fs.headers[1].Get("Gram-Key"))
	require.NotNil(t, fs.requests[1].Source.UserEmail)
	require.Equal(t, "developer@example.com", *fs.requests[1].Source.UserEmail)
	require.NoFileExists(t, authFile)
	require.FileExists(t, authFile+".reauth-needed")
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

// TestLegacyNonblockingFailsOpenOnOutage pins the legacy flag's surviving
// half: a plugin config still carrying nonblocking behaves as fail-open, so an
// unreachable server lets the gating event through — while a definitive 4xx
// (TestLegacyNonblockingKeepsClientErrorsClosed) still blocks.
func TestLegacyNonblockingFailsOpenOnOutage(t *testing.T) {
	fs := newFakeServer(t, nil)
	cfg := authedConfig(t, fs.URL)
	cfg.Nonblocking = true
	fs.Close()

	res := invoke(t, cfg, agenthooks.ProviderClaudeCode, "claude/pre_tool_use.json")

	require.Equal(t, 0, res.ExitCode)
	require.Equal(t, "{}", string(bytes.TrimSpace(res.Stdout)))
}

// TestLegacyNonblockingKeepsClientErrorsClosed: the legacy flag maps to
// fail-open, not never-block — a definitive 4xx still fails closed.
func TestLegacyNonblockingKeepsClientErrorsClosed(t *testing.T) {
	fs := newFakeServer(t, func(components.IngestRequestBody) (int, decision) {
		return http.StatusBadRequest, decision{Decision: "", Reason: "", Message: ""}
	})
	cfg := authedConfig(t, fs.URL)
	cfg.Nonblocking = true
	res := invoke(t, cfg, agenthooks.ProviderClaudeCode, "claude/pre_tool_use.json")

	require.Contains(t, string(res.Stdout), `"permissionDecision":"deny"`)
	require.Contains(t, string(res.Stdout), "HTTP 400")
}

// TestCachedAuthUsesConfigProject pins that the plugin's configured project
// always wins over the project recorded in the shared credential cache, so a
// key minted in another workspace cannot route this workspace's events there.
func TestCachedAuthUsesConfigProject(t *testing.T) {
	authFile := filepath.Join(t.TempDir(), "hooks-auth.env")
	require.NoError(t, os.WriteFile(authFile, []byte("server_url=https://gram.test\napi_key=key-1\nproject=other-workspace\norg=org-1\n"), 0o600))
	t.Setenv("GRAM_HOOKS_AUTH_FILE", authFile)

	c, ok := readCachedAuth(Config{ServerURL: "https://gram.test", ProjectSlug: "this-workspace", OrgID: "org-1", HooksAPIKey: "", BrowserLogin: false, Nonblocking: false, DebugLog: "", ConfigPath: "", ConfigError: ""})
	require.True(t, ok)
	require.Equal(t, "this-workspace", c.Project)

	c, ok = readCachedAuth(Config{ServerURL: "https://gram.test", ProjectSlug: "", OrgID: "org-1", HooksAPIKey: "", BrowserLogin: false, Nonblocking: false, DebugLog: "", ConfigPath: "", ConfigError: ""})
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

func TestCursorSessionEndRelaysCanonicalLifecycle(t *testing.T) {
	fs := newFakeServer(t, nil)
	cfg := authedConfig(t, fs.URL)
	payload := []byte(`{"hook_event_name":"sessionEnd","conversation_id":"sess-cursor-end","status":"completed"}`)

	res := agenthookstest.Invoke(t, NewRunner(cfg), agenthooks.ProviderCursor, payload, "--variant=cli")

	require.Equal(t, 0, res.ExitCode)
	require.Equal(t, 1, fs.count())
	last := fs.last()
	require.Equal(t, components.TypeSessionEnded, last.Event.Type)
	require.NotNil(t, last.Session)
	require.NotNil(t, last.Session.ID)
	require.Equal(t, "sess-cursor-end", *last.Session.ID)
}

func TestCodexSessionEndRelaysCanonicalLifecycle(t *testing.T) {
	fs := newFakeServer(t, nil)
	cfg := authedConfig(t, fs.URL)
	payload := []byte(`{"session_id":"sess-codex-end","cwd":"/work/repo","hook_event_name":"SessionEnd","reason":"other"}`)

	res := agenthookstest.Invoke(t, NewRunner(cfg), agenthooks.ProviderCodex, payload, "--variant=cli")

	require.Equal(t, 0, res.ExitCode)
	require.Equal(t, 1, fs.count())
	last := fs.last()
	require.Equal(t, components.TypeSessionEnded, last.Event.Type)
	require.NotNil(t, last.Session)
	require.NotNil(t, last.Session.ID)
	require.Equal(t, "sess-codex-end", *last.Session.ID)
}

func TestStopEventsRemainPerTurn(t *testing.T) {
	fs := newFakeServer(t, nil)
	cfg := authedConfig(t, fs.URL)

	invoke(t, cfg, agenthooks.ProviderCursor, "cursor/stop.json")
	require.Equal(t, components.TypeUsageReported, fs.last().Event.Type)

	invoke(t, cfg, agenthooks.ProviderCodex, "codex/stop.json")
	require.Equal(t, components.TypeAssistantResponded, fs.last().Event.Type)
}

// TestLoginCommandCarriesConfig pins the nudge → login contract: the sign-in
// command must reference the plugin's speakeasy.json so the minted credential
// matches the server/project the hook path authenticates against.
func TestLoginCommandCarriesConfig(t *testing.T) {
	cfgPath := filepath.Join(t.TempDir(), "speakeasy.json")
	require.NoError(t, os.WriteFile(cfgPath, []byte(`{"server_url":"https://gram.test","project":"acme","org":"org-1","hooks_api_key":"shared-key","browser_login":true}`), 0o600))

	got, rest := SplitInlineFlags(Config{ServerURL: "", ProjectSlug: "", OrgID: "", HooksAPIKey: "", BrowserLogin: false, Nonblocking: false, DebugLog: "", ConfigPath: "", ConfigError: ""}, []string{"--config=" + cfgPath, "--force"})
	require.Equal(t, []string{"--force"}, rest)
	require.Equal(t, cfgPath, got.ConfigPath)
	require.Equal(t, "https://gram.test", got.ServerURL)
	require.Equal(t, "acme", got.ProjectSlug)
	require.Equal(t, "org-1", got.OrgID)
	require.Equal(t, "shared-key", got.HooksAPIKey)
	require.True(t, got.BrowserLogin)

	require.Contains(t, NewRelay(got).loginCommand(), " login --config="+cfgPath)
}

func TestWritePluginMatchesPublishedEventSets(t *testing.T) {
	tests := []struct {
		provider string
		path     string
		want     []string
	}{
		{
			provider: "claude",
			path:     filepath.Join("hooks", "hooks.json"),
			want:     []string{"ConfigChange", "Notification", "PostToolUse", "PostToolUseFailure", "PreToolUse", "SessionEnd", "SessionStart", "Stop", "UserPromptSubmit"},
		},
		{
			provider: "cursor",
			path:     filepath.Join("hooks", "hooks.json"),
			want:     []string{"afterAgentResponse", "afterAgentThought", "afterMCPExecution", "beforeMCPExecution", "beforeSubmitPrompt", "postToolUse", "postToolUseFailure", "preToolUse", "sessionEnd", "sessionStart", "stop"},
		},
		{
			provider: "codex",
			path:     "hooks.json",
			want:     []string{"PermissionRequest", "PostToolUse", "PreToolUse", "SessionEnd", "SessionStart", "Stop", "UserPromptSubmit"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.provider, func(t *testing.T) {
			dir := t.TempDir()
			err := WritePlugin(t.Context(), tt.provider, dir, PluginConfig{
				ServerURL:    "https://gram.test",
				ProjectSlug:  "default",
				OrgID:        "org-1",
				HooksAPIKey:  "shared-key",
				BrowserLogin: false,
				BinaryPath:   "/tmp/speakeasy-hooks",
			})
			require.NoError(t, err)
			var doc struct {
				Hooks map[string]json.RawMessage `json:"hooks"`
			}
			b, err := os.ReadFile(filepath.Join(dir, tt.path))
			require.NoError(t, err)
			require.NoError(t, json.Unmarshal(b, &doc))
			require.ElementsMatch(t, tt.want, slices.Collect(maps.Keys(doc.Hooks)))

			if tt.provider == "cursor" {
				var entries []map[string]any
				require.NoError(t, json.Unmarshal(doc.Hooks["sessionStart"], &entries))
				require.Equal(t, true, entries[0]["failClosed"])
			}
		})
	}
}

func TestClaudeConfigChangeIsRelayed(t *testing.T) {
	fs := newFakeServer(t, nil)
	cfg := authedConfig(t, fs.URL)
	t.Setenv("GRAM_DEVICE_AGENT_COMMANDS", "speakeasy-hooks-test-missing-device-agent")
	t.Setenv("PATH", t.TempDir())
	stubMCPInventorySpawn(t)
	payload := []byte(`{"session_id":"session-1","hook_event_name":"ConfigChange","source":"project_settings"}`)

	res := agenthookstest.Invoke(t, NewRunner(cfg), agenthooks.ProviderClaudeCode, payload, "--variant=cli")

	require.Equal(t, 0, res.ExitCode)
	require.Equal(t, 1, fs.count())
	require.Equal(t, components.TypeSessionUpdated, fs.last().Event.Type)
	require.Equal(t, "ConfigChange", *fs.last().Source.RawEventName)
	require.Nil(t, fs.last().Data, "the config-change event never carries inventory; the detached collector relays it")
}

func TestParseClaudeMCPInventory(t *testing.T) {
	entries := parseClaudeMCPInventory(strings.Join([]string{
		"Checking MCP server health...",
		"remote: https://user:password@mcp.example.com/sse?api_key=secret&workspace=acme (SSE) - connected",
		"plugin:linear:issues: npx -y mcp-remote https://linear.example.com/mcp?token=secret (STDIO) - connected",
		"claude.ai Notion (Acme): https://mcp.notion.com/mcp (HTTP) - needs authentication",
	}, "\n"))

	require.Len(t, entries, 3)
	require.Equal(t, "remote", entries[0].Name)
	require.Equal(t, "https://user:password@mcp.example.com/sse?api_key=secret&workspace=acme", entries[0].URL)
	require.Empty(t, entries[0].Command)
	require.Equal(t, "issues", entries[1].Name)
	require.Equal(t, "npx -y mcp-remote https://linear.example.com/mcp?token=secret", entries[1].Command)
	require.Equal(t, "Notion (Acme)", entries[2].Name)
}

// recordedInventorySpawn captures one detached-collector spawn for assertions.
type recordedInventorySpawn struct {
	cwd       string
	sessionID string
}

// stubMCPInventorySpawn replaces the detached collector spawn with a recorder so
// tests that drive SessionStart/ConfigChange through the runner never fork a
// real background process (which would re-exec the test binary). It returns a
// pointer to the slice of recorded spawns.
func stubMCPInventorySpawn(t *testing.T) *[]recordedInventorySpawn {
	t.Helper()
	var got []recordedInventorySpawn
	orig := spawnMCPInventory
	spawnMCPInventory = func(_ Config, cwd, sessionID string) error {
		got = append(got, recordedInventorySpawn{cwd: cwd, sessionID: sessionID})
		return nil
	}
	t.Cleanup(func() { spawnMCPInventory = orig })
	return &got
}

// TestClaudeSessionStartSpawnsMCPInventoryCollector proves session start relays
// immediately and delegates the slow inventory collection to a detached worker
// instead of blocking on it (DNO-607).
func TestClaudeSessionStartSpawnsMCPInventoryCollector(t *testing.T) {
	fs := newFakeServer(t, nil)
	cfg := authedConfig(t, fs.URL)
	spawns := stubMCPInventorySpawn(t)
	cwd := t.TempDir()
	payload := []byte(`{"session_id":"session-inventory","cwd":"` + cwd + `","hook_event_name":"SessionStart","source":"startup"}`)

	res := agenthookstest.Invoke(t, NewRunner(cfg), agenthooks.ProviderClaudeCode, payload, "--variant=cli")

	require.Equal(t, 0, res.ExitCode)
	// The session-start event is relayed without the inventory riding on it.
	require.Equal(t, 1, fs.count())
	require.Equal(t, components.TypeSessionStarted, fs.last().Event.Type)
	require.Nil(t, fs.last().Data, "the session-start event never carries inventory; the detached collector relays it")
	// The collector is spawned once, carrying the session's identity.
	require.Len(t, *spawns, 1)
	require.Equal(t, cwd, (*spawns)[0].cwd)
	require.Equal(t, "session-inventory", (*spawns)[0].sessionID)
}

// TestMCPInventoryCollectorRelaysRedactedInventory exercises the detached
// collector entrypoint: it runs `claude mcp list` in the session cwd and relays
// a redacted inventory snapshot as its own session.updated event.
func TestMCPInventoryCollectorRelaysRedactedInventory(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake claude executable uses a POSIX shell")
	}

	binDir := t.TempDir()
	claudePath := filepath.Join(binDir, "claude")
	require.NoError(t, os.WriteFile(claudePath, []byte("#!/bin/sh\npwd > \"$FAKE_CLAUDE_CWD_FILE\"\nprintf '%s\\n' \"$FAKE_CLAUDE_MCP_LIST\"\n"), 0o700))
	t.Setenv("PATH", binDir)
	cwdFile := filepath.Join(t.TempDir(), "cwd")
	t.Setenv("FAKE_CLAUDE_CWD_FILE", cwdFile)
	t.Setenv("FAKE_CLAUDE_MCP_LIST", strings.Join([]string{
		"remote: https://user:password@mcp.example.com/sse?api_key=secret&workspace=acme (SSE) - connected",
		"local: env GITHUB_TOKEN=ghp_secret local-mcp --auth token (STDIO) - connected",
		"malformed: https://user:leaked@example.com/%zz?token=leaked (HTTP) - connected",
	}, "\n"))

	fs := newFakeServer(t, nil)
	cfg := authedConfig(t, fs.URL)
	cwd := t.TempDir()

	require.Equal(t, 0, RunMCPInventory(context.Background(), cfg, cwd, "session-inventory"))

	require.Equal(t, 1, fs.count())
	require.Equal(t, components.TypeSessionUpdated, fs.last().Event.Type)
	require.NotNil(t, fs.last().Data)
	require.Len(t, fs.last().Data.McpInventory, 2)
	require.Equal(t, "remote", *fs.last().Data.McpInventory[0].ServerName)
	require.Equal(t, "https://mcp.example.com/sse?api_key=%2A%2A%2A&workspace=acme", *fs.last().Data.McpInventory[0].URL)
	require.NotContains(t, *fs.last().Data.McpInventory[0].URL, "password")
	require.Equal(t, "env GITHUB_TOKEN=*** local-mcp --auth ***", *fs.last().Data.McpInventory[1].Command)
	invocationCWD, err := os.ReadFile(cwdFile)
	require.NoError(t, err)
	require.Equal(t, cwd, strings.TrimSpace(string(invocationCWD)))
}

// TestMCPInventoryCollectorCollectsFreshInventory proves each collector run
// re-reads the live server list rather than reusing a stale snapshot.
func TestMCPInventoryCollectorCollectsFreshInventory(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake claude executable uses a POSIX shell")
	}

	binDir := t.TempDir()
	claudePath := filepath.Join(binDir, "claude")
	require.NoError(t, os.WriteFile(claudePath, []byte("#!/bin/sh\nprintf '%s\\n' \"$FAKE_CLAUDE_MCP_LIST\"\n"), 0o700))
	t.Setenv("PATH", binDir)

	fs := newFakeServer(t, nil)
	cfg := authedConfig(t, fs.URL)
	cwd := t.TempDir()

	t.Setenv("FAKE_CLAUDE_MCP_LIST", "first: https://first.example.com/mcp (HTTP) - connected")
	require.Equal(t, 0, RunMCPInventory(context.Background(), cfg, cwd, "session-refresh"))

	t.Setenv("FAKE_CLAUDE_MCP_LIST", "second: https://second.example.com/mcp (HTTP) - connected")
	require.Equal(t, 0, RunMCPInventory(context.Background(), cfg, cwd, "session-refresh"))

	require.Equal(t, 2, fs.count())
	require.Equal(t, "https://first.example.com/mcp", *fs.requests[0].Data.McpInventory[0].URL)
	require.Equal(t, "https://second.example.com/mcp", *fs.requests[1].Data.McpInventory[0].URL)
}

// TestLoginCommandQuotesUnsafePaths ensures the nudge command survives shell
// parsing when the plugin lives under a path with spaces.
func TestLoginCommandQuotesUnsafePaths(t *testing.T) {
	cfgPath := filepath.Join(t.TempDir(), "plugin dir", "speakeasy.json")
	cmd := NewRelay(Config{ServerURL: "https://gram.test", ProjectSlug: "acme", OrgID: "", HooksAPIKey: "", BrowserLogin: false, Nonblocking: false, DebugLog: "", ConfigPath: cfgPath, ConfigError: ""}).loginCommand()
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
	cfg := Config{ServerURL: fs.URL, ProjectSlug: "default", OrgID: "", HooksAPIKey: "", BrowserLogin: false, Nonblocking: false, DebugLog: "", ConfigPath: "", ConfigError: ""}

	first := invoke(t, cfg, agenthooks.ProviderClaudeCode, "claude/user_prompt_submit.json")
	require.Equal(t, 0, first.ExitCode)
	require.Contains(t, string(first.Stdout), "additionalContext")
	require.Contains(t, string(first.Stdout), "login")

	second := invoke(t, cfg, agenthooks.ProviderClaudeCode, "claude/user_prompt_submit.json")
	require.NotContains(t, string(second.Stdout), "additionalContext", "nudge fires at most once per session")
}

// TestEnvelopeCodexSkillInference mirrors the bash senders' best-effort Codex
// skill detection: a reader tool opening skills/<name>/SKILL.md and an
// explicit $skill-name prompt mention (validated against the skill roots on
// disk) both attach data.skill while the event keeps its true type on the
// wire — a reclassified event would skip the server's tool/prompt policy scan.
func TestEnvelopeCodexSkillInference(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())
	dir := t.TempDir()
	home := filepath.Join(dir, "home")
	repo := filepath.Join(dir, "repo")
	cwd := filepath.Join(repo, "nested", "sub")
	codexHome := filepath.Join(dir, "codex-home")
	for _, p := range []string{
		filepath.Join(home, ".agents", "skills", "home-skill"),
		filepath.Join(repo, ".agents", "skills", "repo-skill"),
		filepath.Join(codexHome, "skills", ".system", "sys-skill"),
	} {
		require.NoError(t, os.MkdirAll(p, 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(p, "SKILL.md"), []byte("# skill"), 0o644))
	}
	require.NoError(t, os.MkdirAll(cwd, 0o755))
	t.Setenv("HOME", home)
	t.Setenv("CODEX_HOME", codexHome)

	envelope := func(payload string) components.IngestRequestBody {
		t.Helper()
		runner := agenthooks.New()
		var got components.IngestRequestBody
		runner.OnToolPre(func(_ context.Context, e *agenthooks.ToolPreEvent) (agenthooks.ToolPreDecision, error) {
			got = buildEnvelope(e, "test-host")
			return agenthooks.NoDecision(), nil
		})
		runner.OnToolPost(func(_ context.Context, e *agenthooks.ToolPostEvent) (agenthooks.ToolPostDecision, error) {
			got = buildEnvelope(e, "test-host")
			return agenthooks.Observed(), nil
		})
		runner.OnPermission(func(_ context.Context, e *agenthooks.PermissionEvent) (agenthooks.ToolPreDecision, error) {
			got = buildEnvelope(e, "test-host")
			return agenthooks.NoDecision(), nil
		})
		runner.OnPromptSubmitted(func(_ context.Context, e *agenthooks.PromptEvent) (agenthooks.PromptDecision, error) {
			got = buildEnvelope(e, "test-host")
			return agenthooks.AcceptPrompt(), nil
		})
		agenthookstest.Invoke(t, runner, agenthooks.ProviderCodex, []byte(payload))
		return got
	}
	skillOf := func(p components.IngestRequestBody) string {
		if p.Data == nil || p.Data.Skill == nil {
			return ""
		}
		return p.Data.Skill.Name
	}

	got := envelope(`{"hook_event_name":"PreToolUse","session_id":"sess-skill","tool_name":"Bash","tool_input":{"command":"sed -n 1,240p ` + repo + `/.agents/skills/repo-skill/SKILL.md"},"tool_use_id":"call_1"}`)
	require.Equal(t, components.TypeToolRequested, got.Event.Type, "a detected skill read must keep its true event type")
	require.Equal(t, "repo-skill", skillOf(got), "a SKILL.md path in a reader tool input must resolve the skill name")

	got = envelope(`{"hook_event_name":"PostToolUse","session_id":"sess-skill","tool_name":"Bash","tool_input":{"command":"sed -n 1,240p ` + repo + `/.agents/skills/repo-skill/SKILL.md"},"tool_response":{"output":"ok"},"tool_use_id":"call_1"}`)
	require.Equal(t, components.TypeToolCompleted, got.Event.Type)
	require.Empty(t, skillOf(got), "completions must not re-report the activation")

	got = envelope(`{"hook_event_name":"PermissionRequest","session_id":"sess-skill","tool_name":"Bash","tool_input":{"command":"cat ` + repo + `/.agents/skills/repo-skill/SKILL.md"},"permission_type":"exec"}`)
	require.Equal(t, components.TypeToolRequested, got.Event.Type)
	require.Empty(t, skillOf(got), "permission previews may be denied and must not count as activations")

	got = envelope(`{"hook_event_name":"PreToolUse","session_id":"sess-skill","tool_name":"apply_patch","tool_input":{"changes":"` + repo + `/.agents/skills/repo-skill/SKILL.md"},"tool_use_id":"call_2"}`)
	require.Empty(t, skillOf(got), "editing a SKILL.md is not an activation")

	got = envelope(`{"hook_event_name":"UserPromptSubmit","session_id":"sess-skill","prompt":"Check $HOME then use $home-skill please","cwd":"` + cwd + `"}`)
	require.Equal(t, components.TypePromptSubmitted, got.Event.Type, "a skill mention must keep the prompt event type")
	require.Equal(t, "home-skill", skillOf(got), "$name mentions must resolve against $HOME/.agents/skills")

	got = envelope(`{"hook_event_name":"UserPromptSubmit","session_id":"sess-skill","prompt":"use $repo-skill","cwd":"` + cwd + `"}`)
	require.Equal(t, "repo-skill", skillOf(got), "$name mentions must resolve by walking up from the session cwd")

	got = envelope(`{"hook_event_name":"UserPromptSubmit","session_id":"sess-skill","prompt":"pay $50 to $unknown-skill","cwd":"` + cwd + `"}`)
	require.Empty(t, skillOf(got), "$ tokens that resolve to no skill directory must be ignored")

	got = envelope(`{"hook_event_name":"UserPromptSubmit","session_id":"sess-skill","prompt":"please use $home-skill.","cwd":"` + cwd + `"}`)
	require.Equal(t, "home-skill", skillOf(got), "sentence-final punctuation must not defeat a mention")

	got = envelope(`{"hook_event_name":"UserPromptSubmit","session_id":"sess-skill","prompt":"use $repo-skill","cwd":"nested/sub"}`)
	require.Empty(t, skillOf(got), "a relative cwd must terminate the walk without matching")

	got = envelope(`{"hook_event_name":"UserPromptSubmit","session_id":"sess-skill","prompt":"use $sys-skill","cwd":"` + cwd + `"}`)
	require.Equal(t, "sys-skill", skillOf(got), "bundled skills under a .system subdirectory must resolve by bare name")

	got = envelope(`{"hook_event_name":"PreToolUse","session_id":"sess-skill","tool_name":"Bash","tool_input":{"command":"cat /opt/codex/skills/.system/imagegen/SKILL.md"},"tool_use_id":"call_3"}`)
	require.Equal(t, components.TypeToolRequested, got.Event.Type)
	require.Equal(t, "imagegen", skillOf(got), "reads of .system skill paths must infer the bare skill name")
}

func TestEnvelopeCursorSkillInference(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())
	home := t.TempDir()
	t.Setenv("HOME", home)
	sequence := 0
	payload := func(nativeName, toolName string, toolInput any) []byte {
		t.Helper()
		sequence++
		input := map[string]any{
			"conversation_id": "cursor-skill-" + strconv.Itoa(sequence),
			"hook_event_name": nativeName,
			"tool_name":       toolName,
			"tool_input":      toolInput,
			"tool_use_id":     "call-" + strconv.Itoa(sequence),
		}
		if nativeName == "beforeReadFile" {
			input["file_path"] = toolInput
			delete(input, "tool_name")
			delete(input, "tool_input")
		}
		if nativeName == "postToolUse" {
			input["tool_response"] = map[string]any{"output": "ok"}
		}
		b, err := json.Marshal(input)
		require.NoError(t, err)
		return b
	}
	envelope := func(input []byte) components.IngestRequestBody {
		t.Helper()
		runner := agenthooks.New(agenthooks.WithDedupDir(t.TempDir()), agenthooks.WithoutBackfill())
		var got components.IngestRequestBody
		runner.OnToolPre(func(_ context.Context, e *agenthooks.ToolPreEvent) (agenthooks.ToolPreDecision, error) {
			got = buildEnvelope(e, "test-host")
			return agenthooks.NoDecision(), nil
		})
		runner.OnToolPost(func(_ context.Context, e *agenthooks.ToolPostEvent) (agenthooks.ToolPostDecision, error) {
			got = buildEnvelope(e, "test-host")
			return agenthooks.Observed(), nil
		})
		res := agenthookstest.Invoke(t, runner, agenthooks.ProviderCursor, input, "--variant=cli")
		require.Equal(t, 0, res.ExitCode)
		require.Equal(t, schemaVersion, got.SchemaVersion, "the payload must reach a tool handler")
		return got
	}
	skillOf := func(got components.IngestRequestBody) string {
		if got.Data == nil || got.Data.Skill == nil {
			return ""
		}
		return got.Data.Skill.Name
	}

	workspacePath := filepath.Join(home, ".cursor", "skills", "workspace-skill", "SKILL.md")
	got := envelope(payload("preToolUse", "Read", map[string]any{"file_path": workspacePath}))
	require.Equal(t, components.TypeToolRequested, got.Event.Type)
	require.Equal(t, "workspace-skill", skillOf(got))

	pluginRoot := filepath.Join(t.TempDir(), "arbitrary", "plugin")
	pluginPath := filepath.Join(pluginRoot, "skills", "plugin-skill", "SKILL.md")
	require.NoError(t, os.MkdirAll(filepath.Join(pluginRoot, ".cursor-plugin"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(pluginRoot, ".cursor-plugin", "plugin.json"), []byte(`{}`), 0o644))
	got = envelope(payload("preToolUse", "Read", map[string]any{"file_path": pluginPath}))
	require.Equal(t, components.TypeToolRequested, got.Event.Type)
	require.Equal(t, "plugin-skill", skillOf(got))

	got = envelope(payload("preToolUse", "Read", map[string]any{"path": workspacePath}))
	require.Equal(t, "workspace-skill", skillOf(got), "legacy Cursor payloads use path instead of file_path")

	got = envelope(payload("beforeReadFile", "", workspacePath))
	require.Equal(t, components.TypeToolRequested, got.Event.Type)
	require.Equal(t, "ReadFile", *got.Data.ToolCall.Name)
	require.Empty(t, skillOf(got), "the duplicate beforeReadFile is normalized to ReadFile")

	got = envelope(payload("postToolUse", "Read", map[string]any{"file_path": workspacePath}))
	require.Equal(t, components.TypeToolCompleted, got.Event.Type)
	require.Empty(t, skillOf(got), "completions must not re-report the activation")

	require.Empty(t, skillOf(envelope(payload("preToolUse", "Bash", map[string]any{"file_path": workspacePath}))))
	require.Empty(t, skillOf(envelope(payload("preToolUse", "Read", map[string]any{"file_path": filepath.Join(filepath.Dir(workspacePath), "README.md")}))))
	require.Empty(t, skillOf(envelope(payload("preToolUse", "Read", "{"))))
	require.Empty(t, skillOf(envelope(payload("preToolUse", "Read", map[string]any{}))))
	require.Empty(t, skillOf(envelope(payload("preToolUse", "Read", map[string]any{"file_path": "SKILL.md"}))))
	require.Empty(t, skillOf(envelope(payload("preToolUse", "Read", map[string]any{"file_path": filepath.Join(t.TempDir(), "docs", "not-a-skill", "SKILL.md")}))))
	require.Empty(t, skillOf(envelope(payload("preToolUse", "Read", map[string]any{"file_path": filepath.Join(t.TempDir(), "docs", "skills", "example", "SKILL.md")}))))
	require.Empty(t, skillOf(envelope(payload("preToolUse", "Read", map[string]any{"file_path": filepath.Join(t.TempDir(), "plugin", "skills", "SKILL.md")}))))
}

// TestRedactCommandMasksSeparatedHeaderValue pins the tokenized-header shape:
// quote stripping splits `--header "X-API-Key: abc"` into a bare header token
// and its value, and the value must not survive redaction.
func TestRedactCommandMasksSeparatedHeaderValue(t *testing.T) {
	got := redactCommand(`npx srv --header "X-API-Key: abc123" -H "Cookie: sid=zzz9" tail-arg`)
	require.NotContains(t, got, "abc123")
	require.NotContains(t, got, "zzz9")
	require.Contains(t, got, "X-API-Key: ***")
	require.Contains(t, got, "Cookie: ***")
	require.Contains(t, got, "tail-arg", "non-secret arguments must survive")

	got = redactCommand("curl -H authorization:token-value-1")
	require.NotContains(t, got, "token-value-1", "in-token header values keep masking")
	require.Contains(t, got, "authorization: ***")

	got = redactCommand(`curl --header "Authorization:Bearer tok-77" https://api.example.com/v1`)
	require.NotContains(t, got, "tok-77", "a scheme-only header value keeps the mask pending for the credential")
	require.Contains(t, got, "Authorization: Bearer ***")

	got = redactCommand(`curl -H "Authorization: Token secret123" tail2`)
	require.NotContains(t, got, "secret123", "non-bearer auth schemes keep the mask pending")
	require.Contains(t, got, "Authorization: Token ***")
	require.Contains(t, got, "tail2")

	got = redactCommand("curl -H Authorization:Digest cred-abc-1")
	require.NotContains(t, got, "cred-abc-1")
	require.Contains(t, got, "Authorization: Digest ***")

	got = redactCommand("cmd --api-key token tail3")
	require.Contains(t, got, "--api-key *** tail3", "a flag value colliding with a scheme word is still the secret")

	got = redactCommand(`curl -H "Cookie: sid=abc; csrf=def; theme=dark" tail4`)
	require.NotContains(t, got, "sid=abc")
	require.NotContains(t, got, "csrf=def", "every fragment of a multi-part cookie is credential material")
	require.NotContains(t, got, "theme=dark")
	require.Contains(t, got, "tail4", "masking must stop with the cookie value")

	got = redactCommand(`curl -H "Cookie: sid=abc;" https://api.example.com/v1`)
	require.NotContains(t, got, "sid=abc")
	require.Contains(t, got, "api.example.com", "a trailing ';' on the last fragment must not swallow the next argument")

	got = redactCommand(`curl -H "Cookie:sid=abc; csrf=def" tail5`)
	require.NotContains(t, got, "sid=abc")
	require.NotContains(t, got, "csrf=def", "a no-space header value ending in ';' continues the cookie mask")
	require.Contains(t, got, "tail5")

	got = redactCommand(`curl -H "X-API-Key: abc123;" retry=3 tail6`)
	require.NotContains(t, got, "abc123")
	require.Contains(t, got, "retry=3", "cookie continuation is scoped to cookie headers")
	require.Contains(t, got, "tail6")

	got = redactCommand(`npx srv --header "api-key: abc125" tail8`)
	require.NotContains(t, got, "abc125", "keyword-bearing header names count as secret headers")
	require.Contains(t, got, "tail8")

	got = redactCommand(`curl -H "X-Auth-Token: tok-99" https://good.example.com/v3`)
	require.NotContains(t, got, "tok-99")
	require.Contains(t, got, "good.example.com")

	got = redactCommand("connect oauth://svc.example.com/cb tail9")
	require.Contains(t, got, "svc.example.com", "a keyword-bearing URL scheme is not a header")
	require.Contains(t, got, "tail9")

	got = redactCommand("curl -H X-Auth-Token:https://token.example/secret-path tail10")
	require.NotContains(t, got, "token.example", "a no-space header value that is a URL is still the header's secret")
	require.Contains(t, got, "tail10")

	got = redactCommand(`curl -H'X-API-Key: abc124' tail7`)
	require.NotContains(t, got, "abc124", "curl's attached short-option header form is still a secret header")
	require.Contains(t, got, "tail7")

	got = redactCommand(`curl -H'Authorization:Bearer tok-88' https://ok.example.com/v2`)
	require.NotContains(t, got, "tok-88")
	require.Contains(t, got, "ok.example.com")

	got = redactCommand("GITHUB_PAT=github_pat_11ABCDEF npx -y srv")
	require.NotContains(t, got, "github_pat_11ABCDEF", "a benign-named env assignment can still carry a recognizable credential")
	require.Contains(t, got, "GITHUB_PAT=***")

	got = redactCommand("PROXY_URL=https://user:hunter7@proxy.internal npx -y srv")
	require.NotContains(t, got, "hunter7")
}

// TestCodexToolCompletionReplaysRequestID: codex PostToolUse omits both
// tool_use_id and tool_input, so its synthesized id diverges from the
// request's; the queued request id must be replayed on the completion or
// tool.completed never attaches to the tool_calls row.
func TestCodexToolCompletionReplaysRequestID(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	t.Setenv("TMPDIR", t.TempDir())
	fs := newFakeServer(t, nil)
	cfg := authedConfig(t, fs.URL)

	pre := []byte(`{"hook_event_name":"PreToolUse","session_id":"sess-cx1","turn_id":"turn-9","tool_name":"shell","tool_input":{"command":"ls -la"},"cwd":"/work"}`)
	post := []byte(`{"hook_event_name":"PostToolUse","session_id":"sess-cx1","turn_id":"turn-9","tool_name":"shell","tool_output":"file listing","cwd":"/work"}`)
	agenthookstest.Invoke(t, NewRunner(cfg), agenthooks.ProviderCodex, pre, "--variant=cli")
	agenthookstest.Invoke(t, NewRunner(cfg), agenthooks.ProviderCodex, post, "--variant=cli")

	var reqTool, doneTool *components.HookToolCallData
	for _, b := range fs.requests {
		switch b.Event.Type {
		case components.TypeToolRequested:
			reqTool = b.Data.ToolCall
		case components.TypeToolCompleted:
			doneTool = b.Data.ToolCall
		}
	}
	require.NotNil(t, reqTool)
	require.NotNil(t, doneTool)
	require.NotNil(t, reqTool.ID)
	require.NotNil(t, doneTool.ID)
	require.NotEmpty(t, *reqTool.ID)
	require.Equal(t, *reqTool.ID, *doneTool.ID, "the completion must replay the request's id")
}

// TestCursorGenericMCPEchoesSkipped: Cursor fires the generic pre/post hooks
// around MCP:* calls too; the dedicated before/afterMCPExecution events carry
// the same call, so the echoes must not be ingested a second time.
func TestCursorGenericMCPEchoesSkipped(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())
	fs := newFakeServer(t, nil)
	cfg := authedConfig(t, fs.URL)

	echoPre := []byte(`{"conversation_id":"conv-echo","hook_event_name":"preToolUse","tool_name":"MCP:create_issue","tool_input":"{}"}`)
	echoPost := []byte(`{"conversation_id":"conv-echo","hook_event_name":"postToolUse","tool_name":"MCP:create_issue","tool_input":"{}"}`)
	res := agenthookstest.Invoke(t, NewRunner(cfg), agenthooks.ProviderCursor, echoPre, "--variant=cli")
	require.Equal(t, 0, res.ExitCode)
	agenthookstest.Invoke(t, NewRunner(cfg), agenthooks.ProviderCursor, echoPost, "--variant=cli")

	for _, b := range fs.requests {
		require.NotEqual(t, components.TypeToolRequested, b.Event.Type, "a generic MCP echo must not be ingested")
		require.NotEqual(t, components.TypeToolCompleted, b.Event.Type, "a generic MCP echo must not be ingested")
	}
}

// TestCodexDeniedRequestDoesNotQueueID: a denied codex request never executes,
// so no completion drains its queue entry; queueing it would attach the next
// same-tool result to the wrong call.
func TestCodexDeniedRequestDoesNotQueueID(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	t.Setenv("TMPDIR", t.TempDir())
	fs := newFakeServer(t, func(b components.IngestRequestBody) (int, decision) {
		if b.Event.Type == components.TypeToolRequested {
			in, _ := json.Marshal(b.Data.ToolCall.Input)
			if strings.Contains(string(in), "blocked-cmd") {
				return http.StatusOK, decision{Decision: "deny", Reason: "policy_denied", Message: "blocked"}
			}
		}
		return http.StatusOK, decision{Decision: "allow", Reason: "", Message: ""}
	})
	cfg := authedConfig(t, fs.URL)

	denied := []byte(`{"hook_event_name":"PreToolUse","session_id":"sess-cx2","turn_id":"turn-2","tool_name":"shell","tool_input":{"command":"blocked-cmd"},"cwd":"/work"}`)
	allowed := []byte(`{"hook_event_name":"PreToolUse","session_id":"sess-cx2","turn_id":"turn-2","tool_name":"shell","tool_input":{"command":"ls"},"cwd":"/work"}`)
	post := []byte(`{"hook_event_name":"PostToolUse","session_id":"sess-cx2","turn_id":"turn-2","tool_name":"shell","tool_output":"ok","cwd":"/work"}`)
	agenthookstest.Invoke(t, NewRunner(cfg), agenthooks.ProviderCodex, denied, "--variant=cli")
	agenthookstest.Invoke(t, NewRunner(cfg), agenthooks.ProviderCodex, allowed, "--variant=cli")
	agenthookstest.Invoke(t, NewRunner(cfg), agenthooks.ProviderCodex, post, "--variant=cli")

	var allowedID, doneID string
	for _, b := range fs.requests {
		if b.Data == nil || b.Data.ToolCall == nil || b.Data.ToolCall.ID == nil {
			continue
		}
		switch b.Event.Type {
		case components.TypeToolRequested:
			in, _ := json.Marshal(b.Data.ToolCall.Input)
			if !strings.Contains(string(in), "blocked-cmd") {
				allowedID = *b.Data.ToolCall.ID
			}
		case components.TypeToolCompleted:
			doneID = *b.Data.ToolCall.ID
		}
	}
	require.NotEmpty(t, allowedID)
	require.Equal(t, allowedID, doneID, "the completion must correlate to the allowed request, not the denied one")
}

// TestCodexToolQueueConcurrentPushPop: concurrent hook processes share the
// queue (the async completion sender overlaps the next request's hook); every
// pushed id must survive and pop exactly once.
func TestCodexToolQueueConcurrentPushPop(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the queue lock is a no-op on windows; concurrent pushes may clobber")
	}
	path := filepath.Join(t.TempDir(), "queue.ids")
	var wg sync.WaitGroup
	for i := range 16 {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			pushCodexToolID(path, "id-"+strconv.Itoa(n))
		}(i)
	}
	wg.Wait()

	seen := map[string]bool{}
	for range 16 {
		id := popCodexToolID(path)
		require.NotEmpty(t, id, "every concurrent push must survive")
		require.False(t, seen[id], "each queued id pops exactly once")
		seen[id] = true
	}
	require.Empty(t, popCodexToolID(path), "the drained queue yields nothing")
}

// TestMissingVerdictBlocksGatingEvent: a JSON 2xx whose body carries no
// explicit decision must not read as an allow on a blocking hook.
func TestMissingVerdictBlocksGatingEvent(t *testing.T) {
	fs := newFakeServer(t, func(components.IngestRequestBody) (int, decision) {
		return http.StatusOK, decision{Decision: "", Reason: "", Message: ""}
	})
	cfg := authedConfig(t, fs.URL)

	res := invoke(t, cfg, agenthooks.ProviderClaudeCode, "claude/pre_tool_use.json")
	require.Contains(t, string(res.Stdout), `"permissionDecision":"deny"`)
	require.Contains(t, string(res.Stdout), "verdict")
}

// TestUnparseable2xxBlocksGatingEvent: a 2xx the SDK cannot parse (wrong
// content type — e.g. an intercepting proxy) carries no verdict and must not
// read as an implicit allow on a blocking hook.
func TestUnparseable2xxBlocksGatingEvent(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte("<html>intercepted</html>"))
	}))
	t.Cleanup(srv.Close)
	cfg := authedConfig(t, srv.URL)

	res := invoke(t, cfg, agenthooks.ProviderClaudeCode, "claude/pre_tool_use.json")
	require.Contains(t, string(res.Stdout), `"permissionDecision":"deny"`)
	require.Contains(t, string(res.Stdout), "verdict")
}

// TestRejectedCachedKeyNudgesPromptReconnect covers the stale-cache recovery
// path: when the server rejects the cached key on a prompt submission, the
// cache is cleared, a reauth marker is left, and the prompt fails open with a
// reconnect nudge instead of blocking every turn — without ever sending an
// unauthenticated request.
func TestRejectedCachedKeyNudgesPromptReconnect(t *testing.T) {
	fs := newFakeServer(t, func(components.IngestRequestBody) (int, decision) {
		return http.StatusUnauthorized, decision{Decision: "", Reason: "", Message: "unauthorized: api_key not found"}
	})
	authFile := filepath.Join(t.TempDir(), "hooks-auth.env")
	require.NoError(t, os.WriteFile(authFile, []byte("server_url="+fs.URL+"\napi_key=stale-key\nproject=default\n"), 0o600))
	require.NoError(t, os.WriteFile(authFile+".established", []byte{}, 0o600))
	t.Setenv("GRAM_HOOKS_AUTH_FILE", authFile)
	t.Setenv("GRAM_HOOKS_API_KEY", "")
	t.Setenv("TMPDIR", t.TempDir())
	cfg := Config{ServerURL: fs.URL, ProjectSlug: "default", OrgID: "", HooksAPIKey: "", BrowserLogin: false, Nonblocking: false, DebugLog: "", ConfigPath: "", ConfigError: ""}

	first := invoke(t, cfg, agenthooks.ProviderClaudeCode, "claude/user_prompt_submit.json")
	require.Equal(t, 0, first.ExitCode, "a rejected cached key must fail the prompt open")
	require.Contains(t, string(first.Stdout), "additionalContext")
	require.Contains(t, string(first.Stdout), "login")
	_, statErr := os.Stat(authFile)
	require.True(t, os.IsNotExist(statErr), "the rejected cached key must be forgotten")
	require.FileExists(t, authFile+".established", "clearing a rejected key must preserve the fail-closed ratchet marker")
	require.FileExists(t, authFile+".reauth-needed")
	require.Equal(t, 1, fs.count())

	second := invoke(t, cfg, agenthooks.ProviderClaudeCode, "claude/user_prompt_submit.json")
	require.Equal(t, 0, second.ExitCode, "prompts keep failing open while reconnect is pending")
	require.Equal(t, 1, fs.count(), "no unauthenticated request may follow the cleared key")
}

// TestRejectedCachedKeyStillBlocksToolUse verifies stale-cache recovery is not
// a general bypass: tool events fail closed on the rejection and keep failing
// closed (without sending) while the reauth marker stands.
func TestRejectedCachedKeyStillBlocksToolUse(t *testing.T) {
	fs := newFakeServer(t, func(components.IngestRequestBody) (int, decision) {
		return http.StatusUnauthorized, decision{Decision: "", Reason: "", Message: "unauthorized: api_key not found"}
	})
	authFile := filepath.Join(t.TempDir(), "hooks-auth.env")
	require.NoError(t, os.WriteFile(authFile, []byte("server_url="+fs.URL+"\napi_key=stale-key\nproject=default\n"), 0o600))
	require.NoError(t, os.WriteFile(authFile+".established", []byte{}, 0o600))
	t.Setenv("GRAM_HOOKS_AUTH_FILE", authFile)
	t.Setenv("GRAM_HOOKS_API_KEY", "")
	cfg := Config{ServerURL: fs.URL, ProjectSlug: "default", OrgID: "", HooksAPIKey: "", BrowserLogin: false, Nonblocking: false, DebugLog: "", ConfigPath: "", ConfigError: ""}

	first := invoke(t, cfg, agenthooks.ProviderClaudeCode, "claude/pre_tool_use.json")
	require.Contains(t, string(first.Stdout), `"permissionDecision":"deny"`)
	require.Contains(t, string(first.Stdout), "unauthorized: api_key not found")
	require.FileExists(t, authFile+".reauth-needed")
	require.Equal(t, 1, fs.count())

	second := invoke(t, cfg, agenthooks.ProviderClaudeCode, "claude/pre_tool_use.json")
	require.Contains(t, string(second.Stdout), `"permissionDecision":"deny"`, "reauth-needed tool events must still fail closed")
	require.Contains(t, string(second.Stdout), "reconnect")
	require.Equal(t, 1, fs.count(), "reauth-needed tool events must not send unauthenticated requests")
}

// TestWriteAuthClearsReauthMarker pins the recovery contract: a successful
// sign-in ends the reconnect posture.
func TestWriteAuthClearsReauthMarker(t *testing.T) {
	t.Setenv("GRAM_HOOKS_AUTH_FILE", filepath.Join(t.TempDir(), "hooks-auth.env"))
	markReauthNeeded()
	require.True(t, reauthNeeded())
	require.NoError(t, writeAuth(creds{ServerURL: "https://gram.test", APIKey: "k", Project: "p", Email: "", Org: "", Source: credCache}))
	require.False(t, reauthNeeded(), "a fresh credential must clear the reconnect marker")
}

// TestEnvKeyRejectionNamesConfiguredKey: when the explicitly configured
// GRAM_HOOKS_API_KEY is rejected, the failure must name the variable — a
// re-login cannot replace an env key, so pointing at the sign-in flow alone
// would strand the user.
func TestEnvKeyRejectionNamesConfiguredKey(t *testing.T) {
	fs := newFakeServer(t, func(components.IngestRequestBody) (int, decision) {
		return http.StatusUnauthorized, decision{Decision: "", Reason: "", Message: "unauthorized: api_key not found"}
	})
	cfg := authedConfig(t, fs.URL)
	res := invoke(t, cfg, agenthooks.ProviderClaudeCode, "claude/pre_tool_use.json")

	require.Contains(t, string(res.Stdout), `"permissionDecision":"deny"`)
	require.Contains(t, string(res.Stdout), "GRAM_HOOKS_API_KEY")
	require.Contains(t, string(res.Stdout), "unauthorized: api_key not found", "the server response must ride along")
}

// TestResolveAuthIgnoresGenericGramAPIKey pins that the generic MCP credential
// no longer authenticates hook telemetry.
func TestResolveAuthIgnoresGenericGramAPIKey(t *testing.T) {
	t.Setenv("GRAM_HOOKS_AUTH_FILE", filepath.Join(t.TempDir(), "hooks-auth.env"))
	t.Setenv("GRAM_HOOKS_API_KEY", "")
	t.Setenv("GRAM_API_KEY", "mcp-key")

	_, ok := resolveAuth(Config{ServerURL: "https://gram.test", ProjectSlug: "default", OrgID: "", HooksAPIKey: "", BrowserLogin: false, Nonblocking: false, DebugLog: "", ConfigPath: "", ConfigError: ""})
	require.False(t, ok, "GRAM_API_KEY is a different product surface and must not authenticate hooks")
}

// TestInsecureServerURL pins the plaintext-endpoint policy: only exact
// loopback hosts may use http.
func TestInsecureServerURL(t *testing.T) {
	require.False(t, insecureServerURL("https://app.getgram.ai"))
	require.False(t, insecureServerURL("http://127.0.0.1:8080"))
	require.False(t, insecureServerURL("http://localhost/ingest"))
	require.False(t, insecureServerURL("http://[::1]:3000"))
	require.True(t, insecureServerURL("http://gram.example.com"))
	require.True(t, insecureServerURL("http://127.0.0.2"), "only the exact loopback address is exempt")
	require.True(t, insecureServerURL("http://localhost.evil.example"), "loopback names must match as whole hosts")
	require.True(t, insecureServerURL("http://localhost:pw@evil.example"), "userinfo must not spoof a loopback host")
	require.False(t, insecureServerURL("http://user@localhost:8080/x"), "userinfo on a real loopback host stays exempt")
	require.True(t, insecureServerURL("ftp://gram.example.com"))
}

// TestInsecureServerURLFailsClosedWhenEstablished: an established machine
// pointed at a plaintext endpoint refuses before any credential leaves it.
func TestInsecureServerURLFailsClosedWhenEstablished(t *testing.T) {
	authFile := filepath.Join(t.TempDir(), "hooks-auth.env")
	require.NoError(t, os.WriteFile(authFile+".established", []byte{}, 0o600))
	t.Setenv("GRAM_HOOKS_AUTH_FILE", authFile)
	t.Setenv("GRAM_HOOKS_API_KEY", "leaky-key")
	cfg := Config{ServerURL: "http://gram.example.com", ProjectSlug: "default", OrgID: "", HooksAPIKey: "", BrowserLogin: false, Nonblocking: false, DebugLog: "", ConfigPath: "", ConfigError: ""}

	res := invoke(t, cfg, agenthooks.ProviderClaudeCode, "claude/pre_tool_use.json")
	require.Contains(t, string(res.Stdout), `"permissionDecision":"deny"`)
	require.Contains(t, string(res.Stdout), "insecure")
}

// TestInsecureServerURLFailsOpenNeverAuthed mirrors the ratchet: before first
// auth an insecure endpoint skips the network silently instead of bricking the
// agent, so no key can leak either way.
func TestInsecureServerURLFailsOpenNeverAuthed(t *testing.T) {
	t.Setenv("GRAM_HOOKS_AUTH_FILE", filepath.Join(t.TempDir(), "hooks-auth.env"))
	t.Setenv("GRAM_HOOKS_API_KEY", "leaky-key")
	cfg := Config{ServerURL: "http://gram.example.com", ProjectSlug: "default", OrgID: "", HooksAPIKey: "", BrowserLogin: false, Nonblocking: false, DebugLog: "", ConfigPath: "", ConfigError: ""}

	res := invoke(t, cfg, agenthooks.ProviderClaudeCode, "claude/pre_tool_use.json")
	require.Equal(t, 0, res.ExitCode)
	require.Equal(t, "{}", string(bytes.TrimSpace(res.Stdout)))
}

// TestRedactCommandMasksURLQuerySecrets: a server URL passed as a plain
// command argument (npx mcp-remote <url>) must get the same query-string
// masking as the structured MCP URL.
func TestRedactCommandMasksURLQuerySecrets(t *testing.T) {
	got := redactCommand("npx -y mcp-remote https://mcp.example.com/sse?api_key=sekrit22&channel=eng")
	require.NotContains(t, got, "sekrit22")
	require.Contains(t, got, "mcp.example.com", "the host must survive so the identity stays matchable")
	require.Contains(t, got, "channel", "non-secret query parameters must survive")

	got = redactCommand("npx -y mcp-remote https://user:hunter9@mcp.example.com/mcp")
	require.NotContains(t, got, "hunter9")
	require.NotContains(t, got, "user:")
	require.Contains(t, got, "mcp.example.com/mcp", "userinfo URLs keep host and path, matching the structured MCP URL")
}

func TestMCPInventoryRedactionMasksSignedURLCredentials(t *testing.T) {
	got, ok := redactMCPInventoryURL("https://mcp.example.com/sse?sig=short&X-Amz-Signature=aws-secret&X-Amz-Credential=aws-credential&channel=eng")
	require.True(t, ok)
	require.NotContains(t, got, "short")
	require.NotContains(t, got, "aws-secret")
	require.NotContains(t, got, "aws-credential")
	require.Contains(t, got, "channel=eng", "non-secret query parameters must survive")

	command := redactCommand("npx -y mcp-remote https://mcp.example.com/sse?X-Goog-Signature=goog-secret&channel=eng")
	require.NotContains(t, command, "goog-secret")
	require.Contains(t, command, "channel=eng", "command URL redaction must preserve benign parameters")
}

// TestBackfilledPromptDenyGatesTriggeringToolEvent pins the Cursor recovery
// path: when beforeSubmitPrompt was missed, the backfilled prompt's decision
// is reporting-only and discarded by agenthooks, so a server deny must gate
// the tool event that triggered the backfill — it was the turn's only
// prompt-policy check.
func TestBackfilledPromptDenyGatesTriggeringToolEvent(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())
	transcript := filepath.Join(t.TempDir(), "transcript.jsonl")
	require.NoError(t, os.WriteFile(transcript, []byte(`{"type":"message","role":"user","message":{"content":[{"type":"text","text":"<user_query>run the shipped task</user_query>"}]}}`+"\n"), 0o600))

	var prompts, tools atomic.Int32
	fs := newFakeServer(t, func(b components.IngestRequestBody) (int, decision) {
		if b.Event.Type == components.TypePromptSubmitted {
			prompts.Add(1)
			return http.StatusOK, decision{Decision: "deny", Reason: "policy_denied", Message: "Speakeasy blocked this prompt: matched policy pi-guard"}
		}
		tools.Add(1)
		return http.StatusOK, decision{Decision: "allow", Reason: "", Message: ""}
	})
	cfg := authedConfig(t, fs.URL)

	payload, err := json.Marshal(map[string]string{
		"conversation_id": "conv-backfill-1",
		"generation_id":   "gen-1",
		"hook_event_name": "beforeShellExecution",
		"transcript_path": transcript,
		"command":         "curl example.com",
		"cwd":             "/work/repo",
	})
	require.NoError(t, err)
	runner := NewRunner(cfg)
	res := agenthookstest.Invoke(t, runner, agenthooks.ProviderCursor, payload, "--variant=cli")

	require.Contains(t, string(res.Stdout), "deny")
	require.Contains(t, string(res.Stdout), "pi-guard", "the prompt deny message must reach the user on the triggering event")
	require.Equal(t, int32(1), prompts.Load(), "the backfilled prompt must still be reported")
	require.Equal(t, int32(0), tools.Load(), "the gated tool call is not reported: the agent never got to make it")
}

// TestRejectedCachedKeyCursorPromptFailsOpen: the reauth posture fails prompt
// submissions open on every provider — a stale cache must not brick Cursor
// turns. The turn's tool events remain gated, so nothing is lost policy-wise.
func TestRejectedCachedKeyCursorPromptFailsOpen(t *testing.T) {
	fs := newFakeServer(t, func(components.IngestRequestBody) (int, decision) {
		return http.StatusUnauthorized, decision{Decision: "", Reason: "", Message: "unauthorized: api_key not found"}
	})
	authFile := filepath.Join(t.TempDir(), "hooks-auth.env")
	require.NoError(t, os.WriteFile(authFile, []byte("server_url="+fs.URL+"\napi_key=stale-key\nproject=default\n"), 0o600))
	require.NoError(t, os.WriteFile(authFile+".established", []byte{}, 0o600))
	t.Setenv("GRAM_HOOKS_AUTH_FILE", authFile)
	t.Setenv("GRAM_HOOKS_API_KEY", "")
	t.Setenv("TMPDIR", t.TempDir())
	cfg := Config{ServerURL: fs.URL, ProjectSlug: "default", OrgID: "", HooksAPIKey: "", BrowserLogin: false, Nonblocking: false, DebugLog: "", ConfigPath: "", ConfigError: ""}

	res := invoke(t, cfg, agenthooks.ProviderCursor, "cursor/before_submit_prompt.json")
	require.Equal(t, 0, res.ExitCode, "a rejected cached key must fail the prompt open on cursor too")
	require.NotContains(t, string(res.Stdout), `"continue":false`)
	require.NotContains(t, string(res.Stdout), "reconnect", "the reauth message must not block the prompt")
	require.FileExists(t, authFile+".reauth-needed")
	_, statErr := os.Stat(authFile)
	require.True(t, os.IsNotExist(statErr), "the rejected cached key must be forgotten")
}

// TestSplitInlineFlagsRecordsConfigError pins that an unreadable --config file
// is surfaced instead of silently keeping the default deployment identity.
func TestSplitInlineFlagsRecordsConfigError(t *testing.T) {
	cfg, rest := SplitInlineFlags(Config{ServerURL: "", ProjectSlug: "", OrgID: "", HooksAPIKey: "", BrowserLogin: false, Nonblocking: false, DebugLog: "", ConfigPath: "", ConfigError: ""}, []string{"--config=" + filepath.Join(t.TempDir(), "missing.json"), "run"})
	require.NotEmpty(t, cfg.ConfigError)
	require.Equal(t, []string{"run"}, rest)
}

// TestBrokenConfigFailsClosedWhenEstablished: with the plugin config
// unreadable the deployment identity is unknown, so an established machine
// must block without sending anything — a cached key for the fallback server
// would route this workspace's events to another project.
func TestBrokenConfigFailsClosedWhenEstablished(t *testing.T) {
	fs := newFakeServer(t, nil)
	authFile := filepath.Join(t.TempDir(), "hooks-auth.env")
	require.NoError(t, os.WriteFile(authFile, []byte("server_url="+fs.URL+"\napi_key=cached-key\nproject=other-project\n"), 0o600))
	require.NoError(t, os.WriteFile(authFile+".established", []byte{}, 0o600))
	t.Setenv("GRAM_HOOKS_AUTH_FILE", authFile)
	t.Setenv("GRAM_HOOKS_API_KEY", "")
	cfg := Config{ServerURL: fs.URL, ProjectSlug: "", OrgID: "", HooksAPIKey: "", BrowserLogin: false, Nonblocking: false, DebugLog: "", ConfigPath: "/missing/speakeasy.json", ConfigError: "open /missing/speakeasy.json: no such file or directory"}

	res := invoke(t, cfg, agenthooks.ProviderClaudeCode, "claude/pre_tool_use.json")
	require.Contains(t, string(res.Stdout), `"permissionDecision":"deny"`)
	require.Contains(t, string(res.Stdout), "Reinstall")
	require.Equal(t, 0, fs.count(), "no event may leave the machine under an unknown deployment identity")
}

// TestBrokenConfigFailsOpenNeverAuthed: a fresh install with an unreadable
// config stays silent — fail open, and no login nudge since sign-in cannot
// recover the deployment identity.
func TestBrokenConfigFailsOpenNeverAuthed(t *testing.T) {
	t.Setenv("GRAM_HOOKS_AUTH_FILE", filepath.Join(t.TempDir(), "hooks-auth.env"))
	t.Setenv("GRAM_HOOKS_API_KEY", "")
	t.Setenv("TMPDIR", t.TempDir())
	cfg := Config{ServerURL: "https://app.example.test", ProjectSlug: "", OrgID: "", HooksAPIKey: "", BrowserLogin: false, Nonblocking: false, DebugLog: "", ConfigPath: "/missing/speakeasy.json", ConfigError: "open /missing/speakeasy.json: no such file or directory"}

	res := invoke(t, cfg, agenthooks.ProviderClaudeCode, "claude/user_prompt_submit.json")
	require.Equal(t, 0, res.ExitCode)
	require.NotContains(t, string(res.Stdout), "additionalContext")
}

// TestSendBoundsTotalRetryTime pins the overall send budget: an endpoint that
// accepts connections but never responds must not stack the SDK's internal
// retry budget with the transport replays past a controlled deadline.
func TestSendBoundsTotalRetryTime(t *testing.T) {
	// The handler never reads the request body, so the server cannot see the
	// client abandon the connection; cleanup runs LIFO, so close(hung) must be
	// registered after srv.Close to release the handler before Close waits on
	// its outstanding request.
	hung := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		<-hung
	}))
	t.Cleanup(srv.Close)
	t.Cleanup(func() { close(hung) })

	cl := newClient(srv.URL)
	cl.budget = 2 * time.Second
	start := time.Now()
	res := cl.send(t.Context(), creds{ServerURL: "", APIKey: "k", Project: "p", Email: "", Org: "", Source: credEnv}, components.IngestRequestBody{
		SchemaVersion: schemaVersion,
		Source:        components.HookIngestSource{Adapter: "claude", AdapterVersion: nil, RawEventName: nil, Hostname: nil, UserEmail: nil},
		Session:       nil,
		Event:         components.HookIngestEvent{Type: components.TypeSessionUpdated, OccurredAt: nil},
		Data:          nil,
		Raw:           nil,
	}, newIdempotencyToken())

	require.Equal(t, 0, res.statusCode, "a hung endpoint yields a transport failure, not a verdict")
	require.Less(t, time.Since(start), 10*time.Second, "the send budget must bound retries end to end")
}
