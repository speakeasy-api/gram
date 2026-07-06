package plugins

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/hooks/relay"
)

var testHooksBinaryPin = hooksBinaryPin{
	Version: "9.9.9",
	Checksums: map[string]string{
		"darwin_amd64": "1111111111111111111111111111111111111111111111111111111111111111",
		"darwin_arm64": "2222222222222222222222222222222222222222222222222222222222222222",
		"linux_amd64":  "3333333333333333333333333333333333333333333333333333333333333333",
		"linux_arm64":  "4444444444444444444444444444444444444444444444444444444444444444",
	},
}

func alphaGenerateConfig() GenerateConfig {
	return GenerateConfig{
		OrgName:           "Acme Corp",
		OrgEmail:          "team@acme.test",
		OrgID:             "org-alpha",
		ServerURL:         "https://gram.test",
		APIKey:            "",
		HooksAPIKey:       "gram_test_hooks_key",
		ProjectSlug:       "default",
		IsDefaultProject:  true,
		Version:           "0.1.100",
		MarketplaceName:   "",
		ObservabilityMode: false,
		Channel:           ChannelAlpha,
	}
}

func TestGenerateClaudeObservabilityBinaryPlugin(t *testing.T) {
	t.Parallel()
	files := map[string][]byte{}
	require.NoError(t, generateClaudeObservabilityBinaryPluginInDir(files, "", alphaGenerateConfig(), testHooksBinaryPin))

	for _, want := range []string{".claude-plugin/plugin.json", "hooks/hooks.json", "hooks/run.sh", "speakeasy.json"} {
		require.Contains(t, files, want)
	}
	require.NotContains(t, files, "hooks/hook.sh", "alpha plugin must not ship the bash sender")
	require.NotContains(t, files, "hooks/auth.sh", "alpha plugin must not ship the bash auth stack")

	hooksJSON := files["hooks/hooks.json"]
	require.True(t, json.Valid(hooksJSON))
	require.NotContains(t, string(hooksJSON), hooksShimSentinel)

	var parsed struct {
		Hooks map[string][]struct {
			Hooks []struct {
				Command string `json:"command"`
				Timeout int    `json:"timeout"`
				Async   bool   `json:"async"`
			} `json:"hooks"`
		} `json:"hooks"`
	}
	require.NoError(t, json.Unmarshal(hooksJSON, &parsed))

	// The subscription set comes from relay.InstallManifest; model.response has
	// no Claude hook mapping and is skipped by the renderer.
	for _, event := range []string{"SessionStart", "UserPromptSubmit", "PreToolUse", "PermissionRequest", "PostToolUse", "PostToolUseFailure", "Stop", "SessionEnd", "Notification"} {
		require.Contains(t, parsed.Hooks, event)
	}

	sessionStart := parsed.Hooks["SessionStart"][0].Hooks[0]
	require.Equal(t, `sh "$CLAUDE_PLUGIN_ROOT/hooks/run.sh" agenthooks run --provider=claude-code --timeout=5m30s`, sessionStart.Command)
	require.Equal(t, 330, sessionStart.Timeout, "SessionStart must keep the browser-login budget")
	require.False(t, sessionStart.Async)
	require.True(t, parsed.Hooks["PostToolUse"][0].Hooks[0].Async, "telemetry events are fire-and-forget")
	require.False(t, parsed.Hooks["Stop"][0].Hooks[0].Async, "async Stop hooks are dropped by cowork")

	var fileCfg relay.FileConfig
	require.NoError(t, json.Unmarshal(files["speakeasy.json"], &fileCfg))
	require.Equal(t, relay.FileConfig{
		ServerURL:   "https://gram.test",
		Project:     "default",
		Org:         "org-alpha",
		Nonblocking: false,
	}, fileCfg)

	shim := string(files["hooks/run.sh"])
	require.Contains(t, shim, `VERSION="9.9.9"`)
	require.Contains(t, shim, `PROVIDER="claude"`)
	require.Contains(t, shim, testHooksBinaryPin.Checksums["linux_arm64"])
}

func TestGenerateCursorObservabilityBinaryPlugin(t *testing.T) {
	t.Parallel()
	files := map[string][]byte{}
	require.NoError(t, generateCursorObservabilityBinaryPluginInDir(files, "", "acme-corp-observability-cursor", alphaGenerateConfig(), testHooksBinaryPin))

	hooksJSON := files["hooks/hooks.json"]
	require.True(t, json.Valid(hooksJSON))
	require.NotContains(t, string(hooksJSON), hooksShimSentinel)
	require.NotContains(t, string(hooksJSON), "://", "a URL scheme anywhere in cursor hooks.json disables all hooks")

	var parsed struct {
		Version int `json:"version"`
		Hooks   map[string][]struct {
			Command    string `json:"command"`
			FailClosed bool   `json:"failClosed"`
		} `json:"hooks"`
	}
	require.NoError(t, json.Unmarshal(hooksJSON, &parsed))
	require.Equal(t, 1, parsed.Version)

	for _, event := range []string{"sessionStart", "beforeSubmitPrompt", "beforeShellExecution", "beforeMCPExecution", "preToolUse", "afterShellExecution", "postToolUse", "stop", "sessionEnd", "afterAgentResponse", "afterAgentThought"} {
		require.Contains(t, parsed.Hooks, event)
	}

	prompt := parsed.Hooks["beforeSubmitPrompt"][0]
	require.Equal(t, `sh "$CURSOR_PLUGIN_ROOT/hooks/run.sh" agenthooks run --provider=cursor --timeout=1m0s`, prompt.Command)
	require.True(t, prompt.FailClosed, "decision hooks must not silently allow when the shimmed binary crashes")
	require.False(t, parsed.Hooks["stop"][0].FailClosed)

	require.Contains(t, string(files["hooks/run.sh"]), `PROVIDER="cursor"`)
}

func TestGenerateCodexObservabilityBinaryPlugin(t *testing.T) {
	t.Parallel()
	cfg := alphaGenerateConfig()
	files := map[string][]byte{}
	require.NoError(t, generateCodexObservabilityBinaryPluginInDir(files, "", cfg, testHooksBinaryPin))

	require.Contains(t, files, ".codex-plugin/plugin.json")
	require.Contains(t, files, "hooks/hooks.json")
	require.Contains(t, files, "hooks/run.sh")
	require.Contains(t, files, "speakeasy.json")
	require.NotContains(t, files, "config.toml", "user-scope trust seeding must not leak into the marketplace layout")
	require.NotContains(t, files, "hooks.json", "codex hooks config must live in the plugin's hooks/ directory")

	hooksJSON := files["hooks/hooks.json"]
	require.True(t, json.Valid(hooksJSON))
	require.Contains(t, string(hooksJSON), `sh \"$HOME/.codex/.tmp/marketplaces/acme-corp-speakeasy/acme-corp-observability-codex/hooks/run.sh\" agenthooks run --provider=codex`)
	require.Contains(t, string(hooksJSON), "--async", "codex telemetry hooks detach via the binary's --async re-exec")
}

func TestGenerateCodexInstallScriptAlphaApprovals(t *testing.T) {
	t.Parallel()
	cfg := alphaGenerateConfig()
	script, err := GenerateCodexInstallScript("https://gram.test/marketplace/tok.git", cfg)
	require.NoError(t, err)

	approvals, err := computeCodexBinaryHookApprovals(cfg)
	require.NoError(t, err)
	require.NotEmpty(t, approvals)
	for _, approval := range approvals {
		require.Contains(t, string(script), approval.StateKey)
		require.Contains(t, string(script), approval.TrustedHash)
	}
	require.Contains(t, approvals[0].StateKey, "acme-corp-observability-codex@acme-corp-speakeasy:hooks/hooks.json:")
	require.NotContains(t, string(script), "auth_preflight.sh", "alpha install script must approve the shim commands, not the bash stack")
}

func TestAlphaChannelRequiresBinaryPin(t *testing.T) {
	t.Parallel()
	files := map[string][]byte{}
	err := generateClaudeObservabilityBinaryPluginInDir(files, "", alphaGenerateConfig(), hooksBinaryPin{Version: "", Checksums: map[string]string{}})
	require.ErrorContains(t, err, "not pinned")

	partial := hooksBinaryPin{Version: "1.0.0", Checksums: map[string]string{"darwin_arm64": "abc"}}
	err = generateCursorObservabilityBinaryPluginInDir(files, "", "x", alphaGenerateConfig(), partial)
	require.ErrorContains(t, err, "missing a checksum")
}

// TestPluginFingerprintChangesWithChannel pins the rollout mechanics: flipping
// an org's channel must change its plugin fingerprint so the rollout workflow
// republishes it, while stable configs keep their fingerprint. Not parallel —
// it stubs the package-level binary pin.
//
//nolint:paralleltest // mutates currentHooksBinaryPin
func TestPluginFingerprintChangesWithChannel(t *testing.T) {
	saved := currentHooksBinaryPin
	currentHooksBinaryPin = testHooksBinaryPin
	t.Cleanup(func() { currentHooksBinaryPin = saved })

	stable := alphaGenerateConfig()
	stable.Channel = ChannelStable
	alpha := alphaGenerateConfig()

	stableFingerprint, err := PluginFingerprint(nil, stable)
	require.NoError(t, err)
	alphaFingerprint, err := PluginFingerprint(nil, alpha)
	require.NoError(t, err)
	require.NotEqual(t, stableFingerprint, alphaFingerprint)

	again, err := PluginFingerprint(nil, alpha)
	require.NoError(t, err)
	require.Equal(t, alphaFingerprint, again, "alpha fingerprints must be deterministic")
}

// TestAuthCacheFormatSharedAcrossChannels pins the cross-channel credential
// contract: the bash channel's gram_hooks_write_auth must produce exactly the
// bytes hooks/relay/auth.go parses (same file, same keys, same .established
// marker), so an org flipped between channels keeps its sign-in in both
// directions.
func TestAuthCacheFormatSharedAcrossChannels(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	authFile := filepath.Join(dir, "auth.env")
	require.NoError(t, os.WriteFile(filepath.Join(dir, "auth.sh"), renderSharedAuthScript(), 0o755))

	cmd := exec.Command("bash", "-c", `. ./auth.sh; gram_hooks_write_auth "$@"`, "-",
		"https://gram.test", "gram_key_123", "default", "user@acme.test", "org-alpha")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GRAM_HOOKS_AUTH_FILE="+authFile)
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, "gram_hooks_write_auth failed: %s", out)

	// Exact byte format of relay's writeAuth/readAuthFile (hooks/relay/auth.go).
	written, err := os.ReadFile(authFile)
	require.NoError(t, err)
	require.Equal(t,
		"server_url=https://gram.test\napi_key=gram_key_123\nproject=default\nemail=user@acme.test\norg=org-alpha\n",
		string(written))

	_, err = os.Stat(authFile + ".established")
	require.NoError(t, err, "the fail-closed ratchet marker must be shared across channels")
}

// TestHooksBinaryShimForwardsInvocation seeds the versioned cache with a fake
// binary and asserts the shim execs it with the plugin's speakeasy.json and
// the hook argv intact.
func TestHooksBinaryShimForwardsInvocation(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	pluginDir := filepath.Join(root, "plugin")
	dataDir := filepath.Join(root, "data")
	argvFile := filepath.Join(root, "argv")
	require.NoError(t, os.MkdirAll(filepath.Join(pluginDir, "hooks"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(pluginDir, "hooks", "run.sh"), renderHooksBinaryShim("claude", testHooksBinaryPin), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(pluginDir, "speakeasy.json"), []byte(`{"server_url":"https://gram.test","project":"default"}`), 0o644))

	binDir := filepath.Join(dataDir, "speakeasy", "hooks", testHooksBinaryPin.Version)
	require.NoError(t, os.MkdirAll(binDir, 0o755))
	fakeBinary := "#!/bin/sh\nfor arg in \"$@\"; do printf '%s\\n' \"$arg\"; done > \"$ARGV_FILE\"\n"
	require.NoError(t, os.WriteFile(filepath.Join(binDir, "speakeasy-hooks"), []byte(fakeBinary), 0o755))

	cmd := exec.Command("sh", filepath.Join(pluginDir, "hooks", "run.sh"), "agenthooks", "run", "--provider=claude-code")
	cmd.Env = append(os.Environ(), "XDG_DATA_HOME="+dataDir, "ARGV_FILE="+argvFile)
	cmd.Stdin = strings.NewReader("{}")
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, "shim failed: %s", out)

	argv, err := os.ReadFile(argvFile)
	require.NoError(t, err)
	args := strings.Split(strings.TrimSpace(string(argv)), "\n")
	require.Len(t, args, 4)
	require.True(t, strings.HasPrefix(args[0], "--config="), "config flag must come first: %v", args)
	configPath := strings.TrimPrefix(args[0], "--config=")
	require.Equal(t, "speakeasy.json", filepath.Base(configPath))
	var fileCfg relay.FileConfig
	configBytes, err := os.ReadFile(configPath)
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(configBytes, &fileCfg))
	require.Equal(t, "https://gram.test", fileCfg.ServerURL)
	require.Equal(t, []string{"agenthooks", "run", "--provider=claude-code"}, args[1:])
}

// TestHooksBinaryShimFailsOpenWithoutBootstrap covers the never-bootstrapped
// posture: no cached binary and a failing download must not block the agent —
// the shim exits 0 and, on cursor, emits the pass-through body.
func TestHooksBinaryShimFailsOpenWithoutBootstrap(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	pluginDir := filepath.Join(root, "plugin")
	pathDir := filepath.Join(root, "bin")
	require.NoError(t, os.MkdirAll(filepath.Join(pluginDir, "hooks"), 0o755))
	require.NoError(t, os.MkdirAll(pathDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(pluginDir, "hooks", "run.sh"), renderHooksBinaryShim("cursor", testHooksBinaryPin), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(pathDir, "curl"), []byte("#!/bin/sh\nexit 22\n"), 0o755))

	cmd := exec.Command("sh", filepath.Join(pluginDir, "hooks", "run.sh"), "agenthooks", "run", "--provider=cursor")
	cmd.Env = append(os.Environ(),
		"XDG_DATA_HOME="+filepath.Join(root, "data"),
		"PATH="+pathDir+string(os.PathListSeparator)+os.Getenv("PATH"),
	)
	cmd.Stdin = strings.NewReader("{}")
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	require.NoError(t, cmd.Run(), "shim must fail open, not error: %s", stderr.String())
	require.Equal(t, "{}", stdout.String(), "cursor fail-open must emit a pass-through body")
	require.Contains(t, stderr.String(), "download failed")
}
