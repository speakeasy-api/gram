package relay

import (
	"encoding/json"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/speakeasy-api/agenthooks"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/hooks/sdk/models/components"
	"github.com/speakeasy-api/gram/hooks/wire"
)

// blockReportCapture records what the fake agent socket received.
type blockReportCapture struct {
	mu      sync.Mutex
	reports []agentBlockReport
	status  int
	delay   time.Duration
}

func (c *blockReportCapture) count() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.reports)
}

func (c *blockReportCapture) last() agentBlockReport {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.reports[len(c.reports)-1]
}

// startBlockReportSocket serves POST /v1/blocks/report on a unix socket,
// capturing payloads. Mirrors startIdentitySocket's tempdir handling.
func startBlockReportSocket(t *testing.T, status int, delay time.Duration) (string, *blockReportCapture) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("unix-socket fixture is POSIX-only")
	}
	capture := &blockReportCapture{mu: sync.Mutex{}, reports: nil, status: status, delay: delay}
	// Not t.TempDir(): it embeds the test name and can blow the ~104-byte
	// sockaddr_un path limit on macOS.
	dir, err := os.MkdirTemp("", "sk")
	require.NoError(t, err)
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	socket := filepath.Join(dir, "agent.sock")
	ln, err := net.Listen("unix", socket)
	require.NoError(t, err)
	srv := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if capture.delay > 0 {
			time.Sleep(capture.delay)
		}
		if r.URL.Path != agentBlockReportPath || r.Method != http.MethodPost {
			t.Errorf("unexpected request %s %q", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
			return
		}
		body, _ := io.ReadAll(r.Body)
		var report agentBlockReport
		if err := json.Unmarshal(body, &report); err != nil {
			t.Errorf("undecodable block report: %v", err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		capture.mu.Lock()
		capture.reports = append(capture.reports, report)
		capture.mu.Unlock()
		w.WriteHeader(capture.status)
	})}
	go func() { _ = srv.Serve(ln) }()
	t.Cleanup(func() { _ = srv.Close() })
	return socket, capture
}

func requestableBlockEffects(components.IngestRequestBody) map[string]any {
	return map[string]any{
		"block": map[string]any{
			"v":                  1,
			"category":           "shadow_mcp",
			"requestable":        true,
			"request_token":      "rpbr2.test-token",
			"request_url":        "https://app.example.test/risk-policy-bypass/request#request_token=rpbr2.test-token",
			"request_expires_at": "2026-08-14T12:00:00Z",
			"server_name":        "mcp.example.com",
			"server_url":         "https://mcp.example.com/sse",
			"policy_name":        "Shadow MCP policy",
			"tool_name":          "search",
			"block_url":          "https://app.example.test/blocks/0000",
		},
	}
}

func denyWithEffects(t *testing.T, effects func(components.IngestRequestBody) map[string]any) *fakeServer {
	t.Helper()
	fs := newFakeServer(t, func(p components.IngestRequestBody) (int, decision) {
		// agenthooks ≥0.6 backfills a synthetic prompt.submitted before the
		// first real event of a session. Only the tool call carries the policy
		// deny — as on the real server, where shadow-MCP enforcement gates
		// tool.pre — so the deny reaches onToolPre instead of short-circuiting
		// at the backfilled prompt.
		if p.Event.Type != components.TypeToolRequested {
			return http.StatusOK, decision{Decision: "allow", Reason: "", Message: ""}
		}
		return http.StatusOK, decision{Decision: "deny", Reason: "policy_denied", Message: "blocked by policy X"}
	})
	fs.effects = effects
	return fs
}

// A requestable deny reaches the agent socket with the effect's fields plus
// event attribution, and the deny itself is unchanged.
func TestDenyWithBlockEffectNotifiesAgent(t *testing.T) {
	fs := denyWithEffects(t, requestableBlockEffects)
	socket, capture := startBlockReportSocket(t, http.StatusAccepted, 0)
	t.Setenv("SPEAKEASY_SOCKET", socket)

	cfg := authedConfig(t, fs.URL)
	cfg.DebugLog = filepath.Join(t.TempDir(), "debug.log")
	res := invoke(t, cfg, agenthooks.ProviderClaudeCode, "claude/pre_tool_use.json")
	require.Contains(t, string(res.Stdout), `"permissionDecision":"deny"`)

	if capture.count() == 0 {
		b, _ := os.ReadFile(cfg.DebugLog)
		t.Logf("debug log: %s", b)
	}
	require.Equal(t, 1, capture.count(), "exactly one report per denied call")
	report := capture.last()
	require.Equal(t, 1, report.V)
	require.Equal(t, "shadow_mcp", report.Category)
	require.True(t, report.Requestable)
	require.Equal(t, "rpbr2.test-token", report.RequestToken)
	require.Contains(t, report.RequestURL, "#request_token=rpbr2.test-token")
	require.Equal(t, "2026-08-14T12:00:00Z", report.RequestExpiresAt)
	require.Equal(t, "mcp.example.com", report.ServerName)
	require.Equal(t, "https://mcp.example.com/sse", report.ServerURL)
	require.Equal(t, "Shadow MCP policy", report.PolicyName)
	require.Equal(t, "search", report.ToolName)
	require.Equal(t, "https://app.example.test/blocks/0000", report.BlockURL)
	require.Equal(t, "claude-code", report.Provider)
	require.Equal(t, "sess-claude-1", report.SessionID)
	_, err := time.Parse(time.RFC3339, report.BlockedAt)
	require.NoError(t, err)
}

// A deny without a block effect — the overwhelmingly common case — must not
// touch the agent socket at all.
func TestDenyWithoutBlockEffectDoesNotDial(t *testing.T) {
	fs := denyWithEffects(t, nil)
	socket, capture := startBlockReportSocket(t, http.StatusAccepted, 0)
	t.Setenv("SPEAKEASY_SOCKET", socket)

	res := invoke(t, authedConfig(t, fs.URL), agenthooks.ProviderClaudeCode, "claude/pre_tool_use.json")
	require.Contains(t, string(res.Stdout), `"permissionDecision":"deny"`)
	require.Equal(t, 0, capture.count())
}

// Effects the notifier doesn't understand or can't act on are dropped before
// the dial: not requestable, a newer contract version, or a missing token.
func TestDenyWithUnusableBlockEffectDoesNotDial(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(map[string]any)
	}{
		{"not requestable", func(effect map[string]any) { effect["requestable"] = false }},
		{"version too new", func(effect map[string]any) { effect["v"] = 2 }},
		{"missing token", func(effect map[string]any) { effect["request_token"] = "" }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fs := denyWithEffects(t, func(body components.IngestRequestBody) map[string]any {
				effects := requestableBlockEffects(body)
				tc.mutate(effects["block"].(map[string]any))
				return effects
			})
			socket, capture := startBlockReportSocket(t, http.StatusAccepted, 0)
			t.Setenv("SPEAKEASY_SOCKET", socket)

			res := invoke(t, authedConfig(t, fs.URL), agenthooks.ProviderClaudeCode, "claude/pre_tool_use.json")
			require.Contains(t, string(res.Stdout), `"permissionDecision":"deny"`)
			require.Equal(t, 0, capture.count())
		})
	}
}

// An allow with a (misconfigured) block effect must never notify — the effect
// only rides denies, but the guard is on our side too.
func TestAllowNeverNotifies(t *testing.T) {
	fs := newFakeServer(t, nil) // scripted allow
	fs.effects = requestableBlockEffects
	socket, capture := startBlockReportSocket(t, http.StatusAccepted, 0)
	t.Setenv("SPEAKEASY_SOCKET", socket)

	res := invoke(t, authedConfig(t, fs.URL), agenthooks.ProviderClaudeCode, "claude/pre_tool_use.json")
	require.NotContains(t, string(res.Stdout), `"permissionDecision":"deny"`)
	require.Equal(t, 0, capture.count())
}

// The daemon answering 404 (a build that predates the contract) changes
// nothing about the deny.
func TestDenyNotifySwallowsOldDaemon404(t *testing.T) {
	fs := denyWithEffects(t, requestableBlockEffects)
	socket, _ := startBlockReportSocket(t, http.StatusNotFound, 0)
	t.Setenv("SPEAKEASY_SOCKET", socket)

	res := invoke(t, authedConfig(t, fs.URL), agenthooks.ProviderClaudeCode, "claude/pre_tool_use.json")
	require.Contains(t, string(res.Stdout), `"permissionDecision":"deny"`)
}

// A machine with no agent at all: the dial fails instantly and the deny is
// unchanged.
func TestDenyNotifySwallowsAbsentSocket(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix-socket fixture is POSIX-only")
	}
	fs := denyWithEffects(t, requestableBlockEffects)
	t.Setenv("SPEAKEASY_SOCKET", filepath.Join(t.TempDir(), "nope.sock"))

	res := invoke(t, authedConfig(t, fs.URL), agenthooks.ProviderClaudeCode, "claude/pre_tool_use.json")
	require.Contains(t, string(res.Stdout), `"permissionDecision":"deny"`)
}

// A hung daemon costs at most the notify budget, never the provider's
// patience.
func TestDenyNotifyBoundedByHungAgent(t *testing.T) {
	fs := denyWithEffects(t, requestableBlockEffects)
	socket, _ := startBlockReportSocket(t, http.StatusAccepted, 5*time.Second)
	t.Setenv("SPEAKEASY_SOCKET", socket)

	start := time.Now()
	res := invoke(t, authedConfig(t, fs.URL), agenthooks.ProviderClaudeCode, "claude/pre_tool_use.json")
	elapsed := time.Since(start)
	require.Contains(t, string(res.Stdout), `"permissionDecision":"deny"`)
	// Bound = the 300ms budget plus slack for the local ingest exchange and
	// runner overhead — tight enough that a budget regression to even 1s fails.
	require.Less(t, elapsed, 1300*time.Millisecond, "a hung agent must cost at most the notify budget, elapsed=%s", elapsed)
}

func TestDecodeBlockEffectRejectsNonObjects(t *testing.T) {
	require.Nil(t, decodeBlockEffect(nil))
	require.Nil(t, decodeBlockEffect("string"))
	require.Nil(t, decodeBlockEffect([]any{"list"}))
	require.Nil(t, decodeBlockEffect(map[string]any{"v": "not-a-number"}))
}

// decodeBlockEffect reads keys off the map by hand (the SDK pre-decodes
// Effects, so there are no raw bytes to unmarshal); this pins those keys to
// wire.BlockEffect's JSON tags so the reader and the shared type cannot
// drift. exhaustruct keeps both literals below naming every field, so a field
// added to the wire type without a matching read fails here, and a renamed
// tag fails the round trip.
func TestDecodeBlockEffectMatchesWireContract(t *testing.T) {
	want := wire.BlockEffect{
		V:                wire.BlockEffectVersion,
		Category:         "shadow_mcp",
		Requestable:      true,
		RequestToken:     "rpbr2.contract",
		RequestURL:       "https://app.example.test/risk-policy-bypass/request#request_token=rpbr2.contract",
		RequestExpiresAt: "2026-08-14T12:00:00Z",
		ServerName:       "mcp.example.com",
		ServerURL:        "https://mcp.example.com/sse",
		PolicyName:       "Shadow MCP policy",
		ToolName:         "search",
		BlockURL:         "https://app.example.test/blocks/0000",
	}
	raw, err := json.Marshal(want)
	require.NoError(t, err)
	var decoded map[string]any
	require.NoError(t, json.Unmarshal(raw, &decoded))
	got := decodeBlockEffect(decoded)
	require.NotNil(t, got)
	require.Equal(t, want, *got)
}
