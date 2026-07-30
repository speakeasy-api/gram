package relay

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/speakeasy-api/agenthooks"
	"github.com/speakeasy-api/agenthooks/agenthookstest"
	"github.com/stretchr/testify/require"
)

func TestLoadConfigEnablesDefaultDebugLog(t *testing.T) {
	stateHome := t.TempDir()
	t.Setenv("XDG_STATE_HOME", stateHome)
	t.Setenv("GRAM_HOOKS_DEBUG_LOG", "")

	cfg := LoadConfig(Config{
		ServerURL:    "",
		SiteURL:      "",
		ProjectSlug:  "",
		OrgID:        "",
		HooksAPIKey:  "",
		BrowserLogin: false,
		Nonblocking:  false,
		DebugLog:     "",
		ConfigPath:   "",
		ConfigError:  "",
	})

	require.Equal(t, filepath.Join(stateHome, "gram", "hooks", "speakeasy-hooks.log"), cfg.DebugLog)
}

func TestLoadConfigUsesDebugLogEnvironmentOverride(t *testing.T) {
	path := filepath.Join(t.TempDir(), "custom-hooks.log")
	t.Setenv("GRAM_HOOKS_DEBUG_LOG", path)

	cfg := LoadConfig(Config{
		ServerURL:    "",
		SiteURL:      "",
		ProjectSlug:  "",
		OrgID:        "",
		HooksAPIKey:  "",
		BrowserLogin: false,
		Nonblocking:  false,
		DebugLog:     "",
		ConfigPath:   "",
		ConfigError:  "",
	})

	require.Equal(t, path, cfg.DebugLog)
}

func TestDebugLogCapturesRelayAndAgenthooksDiagnostics(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "speakeasy-hooks.log")
	cfg := Config{
		ServerURL:    DefaultServerURL,
		SiteURL:      "",
		ProjectSlug:  "",
		OrgID:        "",
		HooksAPIKey:  "",
		BrowserLogin: false,
		Nonblocking:  false,
		DebugLog:     path,
		ConfigPath:   "",
		ConfigError:  "",
	}

	NewRelay(cfg).debugf("connectivity diagnostic: %s", "dial tcp")
	result := agenthookstest.Invoke(t, NewRunner(cfg), agenthooks.ProviderClaudeCode, []byte("{"))
	require.Zero(t, result.ExitCode)

	logData, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Contains(t, string(logData), "connectivity diagnostic: dial tcp")
	require.Contains(t, string(logData), "agenthooks: decode failed")
	require.Contains(t, string(logData), "level=DEBUG")
	require.Contains(t, string(logData), "level=ERROR")

	info, err := os.Stat(path)
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0o600), info.Mode().Perm())
}
