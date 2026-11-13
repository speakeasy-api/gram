package relay

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/speakeasy-api/agenthooks"
	"github.com/speakeasy-api/agenthooks/agenthookstest"
	"github.com/stretchr/testify/require"
)

// discoveryBox is a tokenBox whose token command comes from the device
// agent's identity (contract C8) rather than the env override. identity is the
// JSON `speakeasyd identity` prints; every run appends a line to the returned
// counter file.
func discoveryBox(t *testing.T, serverURL string, identity any) (Config, *fakeTokenCommand, string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("identity fixture is a POSIX shell script")
	}
	cfg, fc := tokenBox(t, serverURL)
	t.Setenv("SPEAKEASY_AI_HOOKS_TOKEN_COMMAND", "")
	dir := t.TempDir()
	counter := filepath.Join(dir, "identity-runs")
	body, err := json.Marshal(identity)
	require.NoError(t, err)
	script := filepath.Join(dir, "speakeasyd")
	require.NoError(t, os.WriteFile(script, []byte("#!/bin/sh\necho run >> '"+counter+"'\nprintf '%s' '"+string(body)+"'\n"), 0o700)) // #nosec G306 -- test script must be executable
	t.Setenv("SPEAKEASY_AI_DEVICE_AGENT_COMMANDS", script)
	t.Setenv("SPEAKEASY_AI_DEVICE_AGENT_TIMEOUT_TENTHS", "20")
	resetDeviceIdentityMemo()
	return cfg, fc, counter
}

// invokeDiscovered is invoke without its device-agent override: the identity
// fixture discoveryBox installed must stay the one that answers.
func invokeDiscovered(t *testing.T, cfg Config, fixture string) agenthookstest.Result {
	t.Helper()
	if v := os.Getenv("XDG_STATE_HOME"); v == "" || v != spoolStateHome {
		t.Setenv("XDG_STATE_HOME", t.TempDir())
	}
	return agenthookstest.Invoke(t, NewRunner(cfg), agenthooks.ProviderClaudeCode, agenthookstest.Fixture(t, fixture), "--variant=cli")
}

func identityRuns(t *testing.T, counter string) int {
	t.Helper()
	b, err := os.ReadFile(counter)
	if os.IsNotExist(err) {
		return 0
	}
	require.NoError(t, err)
	return strings.Count(string(b), "run\n")
}

func TestDiscoveredWorkloadUsesTheAgentsArgv(t *testing.T) {
	fs := newFakeServer(t, nil)
	argv := []string{"/Library/Application Support/Speakeasy/bin/speakeasyd", "hooks-token", "--socket", "/tmp/a b/agent.sock"}
	cfg, fc, _ := discoveryBox(t, fs.URL, map[string]any{"email": "box@example.com", "workload": true, "hooks_token_argv": argv})

	invokeDiscovered(t, cfg, "claude/pre_tool_use.json")

	require.Equal(t, strings.Join(argv, "\x00"), fc.lastSeen, "the discovered argv is executed as given, spaces and all")
	require.Equal(t, 1, fs.count())
	require.Equal(t, "Bearer tok-1", fs.headers[0].Get("Authorization"))
	require.Empty(t, fs.headers[0].Get("Gram-Key"), "a discovered workload box never sends a key")
}

func TestDiscoveredArgvRunsWithoutSplitting(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses a POSIX shell script")
	}
	dir := filepath.Join(t.TempDir(), "Application Support")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	script := filepath.Join(dir, "speakeasyd")
	require.NoError(t, os.WriteFile(script, []byte(`#!/bin/sh
printf '{"authorization":"Bearer t","project":"%s","expires_at":"2099-01-01T00:00:00Z"}' "$2"
`), 0o700)) // #nosec G306 -- test script must be executable

	out, err := execTokenCommand(t.Context(), []string{script, "hooks-token", "with space"}, false)

	require.NoError(t, err)
	require.Contains(t, string(out), `"project":"with space"`, "an argv element containing a space reaches the command intact")
}

func TestEnvTokenCommandOverridesDiscovery(t *testing.T) {
	fs := newFakeServer(t, nil)
	cfg, fc, _ := discoveryBox(t, fs.URL, map[string]any{"workload": true, "hooks_token_argv": []string{"/discovered/speakeasyd", "hooks-token"}})
	t.Setenv("SPEAKEASY_AI_HOOKS_TOKEN_COMMAND", "/override/speakeasyd hooks-token")

	invokeDiscovered(t, cfg, "claude/pre_tool_use.json")

	require.Equal(t, "/override/speakeasyd\x00hooks-token", fc.lastSeen)
	require.Empty(t, fs.headers[0].Get("Gram-Key"))
}

func TestDiscoveredWorkloadWithoutUsableArgvFailsClosed(t *testing.T) {
	for _, tc := range []struct {
		name     string
		identity map[string]any
	}{
		{name: "argv missing", identity: map[string]any{"workload": true}},
		{name: "argv relative", identity: map[string]any{"workload": true, "hooks_token_argv": []string{"speakeasyd", "hooks-token"}}},
		{name: "argv empty", identity: map[string]any{"workload": true, "hooks_token_argv": []string{}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fs := newFakeServer(t, nil)
			cfg, _, _ := discoveryBox(t, fs.URL, tc.identity)
			// Run the real executor, so an unusable argv is refused, not faked.
			prev := runTokenCommand
			runTokenCommand = execTokenCommand
			t.Cleanup(func() { runTokenCommand = prev })
			setSpoolStateHome(t)
			writeOrgSettings(cfg, false)

			res := invokeDiscovered(t, cfg, "claude/pre_tool_use.json")

			require.Equal(t, 0, fs.count(), "no key of any kind may be sent instead")
			require.Contains(t, string(res.Stdout), `"permissionDecision":"deny"`, "a fail-closed org blocks the gating event")
			require.Contains(t, string(res.Stdout), "credential-unavailable")
		})
	}
}

func TestIdentityWithoutWorkloadKeepsKeyAuth(t *testing.T) {
	fs := newFakeServer(t, nil)
	cfg, fc, _ := discoveryBox(t, fs.URL, map[string]any{"email": "person@example.com"})

	invokeDiscovered(t, cfg, "claude/pre_tool_use.json")

	require.Equal(t, int32(0), fc.calls.Load(), "no token command runs on a box the agent does not call a workload")
	require.Equal(t, 1, fs.count())
	require.Equal(t, "env-key-must-not-be-used", fs.headers[0].Get("Gram-Key"), "today's precedence applies: the env key")
	require.Empty(t, fs.headers[0].Get("Authorization"))
}

func TestIdentityLookupRunsOncePerEvent(t *testing.T) {
	fs := newFakeServer(t, nil)
	cfg, _, counter := discoveryBox(t, fs.URL, map[string]any{"email": "box@example.com", "workload": true, "hooks_token_argv": []string{"/opt/speakeasyd", "hooks-token"}})

	invokeDiscovered(t, cfg, "claude/pre_tool_use.json")
	require.Equal(t, 1, identityRuns(t, counter), "credential selection and attribution share one identity lookup")

	invokeDiscovered(t, cfg, "claude/post_tool_use.json")
	require.Equal(t, 1, identityRuns(t, counter), "a lookup inside the TTL is reused")
}

func TestTokenBoxKeepsTokenAuthWhenIdentityIsUnavailable(t *testing.T) {
	fs := newFakeServer(t, nil)
	argv := []string{"/opt/speakeasy/speakeasyd", "hooks-token"}
	cfg, fc, _ := discoveryBox(t, fs.URL, map[string]any{"workload": true, "hooks_token_argv": argv})
	invokeDiscovered(t, cfg, "claude/pre_tool_use.json")
	require.Equal(t, int32(1), fc.calls.Load())

	// The device agent stops answering and the cached token expires: the box
	// must keep authenticating by token, never drop back to a key.
	t.Setenv("SPEAKEASY_AI_DEVICE_AGENT_COMMANDS", "speakeasy-hooks-test-missing-device-agent")
	resetDeviceIdentityMemo()
	fc.now = fc.now.Add(time.Hour)
	fc.lastSeen = ""

	invokeDiscovered(t, cfg, "claude/pre_tool_use.json")

	require.Equal(t, int32(2), fc.calls.Load())
	require.Equal(t, strings.Join(argv, "\x00"), fc.lastSeen, "the argv recorded with the token is reused")
	for _, h := range fs.headers {
		require.Empty(t, h.Get("Gram-Key"))
	}
}
