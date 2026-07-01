package plugins

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// When ProxyHooksViaAgent is set, every tool's hook command becomes the single
// native `speakeasyd hook --tool <x>` invocation instead of `bash hook.sh`.

func proxyCfg() GenerateConfig {
	return GenerateConfig{
		OrgName:            "Acme",
		ServerURL:          "https://app.getgram.ai",
		HooksAPIKey:        "gram_local_secret_xyz",
		ProxyHooksViaAgent: true,
	}
}

func TestProxyHooks_CursorCommand(t *testing.T) {
	t.Parallel()
	cfg := proxyCfg()
	files, err := GeneratePluginPackages(nil, cfg)
	require.NoError(t, err)

	var hooks cursorHooksConfig
	path := "cursor-plugins/" + CursorObservabilitySlug(cfg) + "/hooks/hooks.json"
	require.NoError(t, json.Unmarshal(files[path], &hooks))
	require.NotEmpty(t, hooks.Hooks)
	for event, cmds := range hooks.Hooks {
		require.Equal(t, "speakeasyd hook --tool cursor", cmds[0].Command, "event %s", event)
		require.NotContains(t, cmds[0].Command, "bash")
	}
}

func TestProxyHooks_ClaudeCommand(t *testing.T) {
	t.Parallel()
	cfg := proxyCfg()
	files, err := GeneratePluginPackages(nil, cfg)
	require.NoError(t, err)

	var hooks claudeHooksConfig
	path := ClaudeObservabilitySlug(cfg) + "/hooks/hooks.json"
	require.NoError(t, json.Unmarshal(files[path], &hooks))
	require.NotEmpty(t, hooks.Hooks)
	for event, matchers := range hooks.Hooks {
		require.Equal(t, "speakeasyd hook --tool claude_code", matchers[0].Hooks[0].Command, "event %s", event)
	}
}

func TestProxyHooks_CodexCommandAndTrustHash(t *testing.T) {
	t.Parallel()
	cfg := proxyCfg()
	files, err := GeneratePluginPackages(nil, cfg)
	require.NoError(t, err)

	var hooks codexHooksConfig
	path := CodexObservabilitySlug(cfg) + "/hooks/hooks.json"
	require.NoError(t, json.Unmarshal(files[path], &hooks))
	require.NotEmpty(t, hooks.Hooks)
	for event, groups := range hooks.Hooks {
		require.Equal(t, "speakeasyd hook --tool codex", groups[0].Hooks[0].Command, "event %s", event)
	}

	// The command is part of Codex's trust hash, so the proxy command must hash
	// differently from the bash one — otherwise the pre-approval wouldn't match.
	mp, plugin := "acme-speakeasy", CodexObservabilitySlug(cfg)
	bashHash, err := computeCodexHookHash("PreToolUse", mp, plugin, false)
	require.NoError(t, err)
	agentHash, err := computeCodexHookHash("PreToolUse", mp, plugin, true)
	require.NoError(t, err)
	require.NotEqual(t, bashHash, agentHash, "trust hash must track the command change")
}

func TestProxyHooks_OffKeepsBashCommand(t *testing.T) {
	t.Parallel()
	cfg := proxyCfg()
	cfg.ProxyHooksViaAgent = false
	files, err := GeneratePluginPackages(nil, cfg)
	require.NoError(t, err)

	var hooks cursorHooksConfig
	path := "cursor-plugins/" + CursorObservabilitySlug(cfg) + "/hooks/hooks.json"
	require.NoError(t, json.Unmarshal(files[path], &hooks))
	for _, cmds := range hooks.Hooks {
		require.True(t, strings.HasPrefix(cmds[0].Command, "bash "), "default must stay bash: %q", cmds[0].Command)
	}
}
