package relay

import (
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
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
	if runtime.GOOS != "windows" {
		require.Equal(t, os.FileMode(0o600), info.Mode().Perm())
	}
}

func TestDebugLogSecuresExistingFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "speakeasy-hooks.log")
	require.NoError(t, os.WriteFile(path, []byte("existing\n"), 0o644))
	require.NoError(t, os.Chmod(path, 0o644))

	logger := newDebugLogger(path)
	logger.ErrorContext(t.Context(), "diagnostic")

	info, err := os.Stat(path)
	require.NoError(t, err)
	if runtime.GOOS != "windows" {
		require.Equal(t, os.FileMode(0o600), info.Mode().Perm())
	}
}

func TestDebugLogCapsIndividualRecords(t *testing.T) {
	path := filepath.Join(t.TempDir(), "speakeasy-hooks.log")
	logger := newDebugLogger(path)
	logger.ErrorContext(t.Context(), strings.Repeat("x", maxDebugLogRecordBytes*2))

	logData, err := os.ReadFile(path)
	require.NoError(t, err)
	require.LessOrEqual(t, len(logData), maxDebugLogRecordBytes)
	require.Contains(t, string(logData), "record truncated")
}

func TestTransportDiagnosticRedactsAndCapsURLErrors(t *testing.T) {
	err := &url.Error{
		Op:  "Post",
		URL: "https://user:password@example.com/hooks?token=secret",
		Err: errors.New(strings.Repeat("connection failure ", 256)),
	}

	diagnostic := transportDiagnostic(err)

	require.LessOrEqual(t, len(diagnostic), maxDiagnosticBytes)
	require.NotContains(t, diagnostic, "password")
	require.NotContains(t, diagnostic, "secret")
	require.Contains(t, diagnostic, "example.com")
	require.Contains(t, diagnostic, "connection failure")
}

func TestDeliveryLogRedactsConfiguredServerURL(t *testing.T) {
	path := filepath.Join(t.TempDir(), "speakeasy-hooks.log")
	cfg := Config{
		ServerURL:    "http://user:password@example.com/hooks?token=secret",
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
	event := &agenthooks.PromptEvent{
		Event: agenthooks.Event{
			Provider:   agenthooks.ProviderClaudeCode,
			Kind:       agenthooks.KindPromptSubmitted,
			NativeName: "UserPromptSubmit",
		},
		Prompt: "hello",
	}

	NewRelay(cfg).deliver(t.Context(), event)

	logData, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Contains(t, string(logData), "example.com")
	require.NotContains(t, string(logData), "password")
	require.NotContains(t, string(logData), "secret")
}
