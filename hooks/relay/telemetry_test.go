package relay

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// telemetryTestEnv points credential resolution at an isolated auth file and
// re-enables the recorder TestMain disables package-wide.
func telemetryTestEnv(t *testing.T) {
	t.Helper()
	t.Setenv("GRAM_HOOKS_DISABLE_TELEMETRY", "")
	t.Setenv("GRAM_HOOKS_AUTH_FILE", filepath.Join(t.TempDir(), "hooks-auth.env"))
	t.Setenv("GRAM_HOOKS_API_KEY", "")
}

func telemetryTestConfig(serverURL string) Config {
	return Config{
		ServerURL:    serverURL,
		SiteURL:      "",
		ProjectSlug:  "acme-prod",
		OrgID:        "",
		HooksAPIKey:  "",
		BrowserLogin: false,
		Nonblocking:  false,
		DebugLog:     "",
		ConfigPath:   "",
		ConfigError:  "",
	}
}

func TestNewTelemetryRecorderRequiresCredentials(t *testing.T) {
	telemetryTestEnv(t)
	rec := newTelemetryRecorder(NewRelay(telemetryTestConfig("https://gram.example")))
	require.Nil(t, rec, "no credential resolved: the recorder must be skipped, not fail")
}

func TestNewTelemetryRecorderBuildsWithEnvKey(t *testing.T) {
	telemetryTestEnv(t)
	t.Setenv("GRAM_HOOKS_API_KEY", "test-hooks-key")
	rec := newTelemetryRecorder(NewRelay(telemetryTestConfig("https://gram.example")))
	require.NotNil(t, rec)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	require.NoError(t, rec.Shutdown(ctx))
}

func TestNewTelemetryRecorderRefusesInsecureServer(t *testing.T) {
	telemetryTestEnv(t)
	t.Setenv("GRAM_HOOKS_API_KEY", "test-hooks-key")
	rec := newTelemetryRecorder(NewRelay(telemetryTestConfig("http://gram.example")))
	require.Nil(t, rec, "a hooks key must never ride plaintext HTTP")
}

func TestNewTelemetryRecorderSkipsOnConfigError(t *testing.T) {
	telemetryTestEnv(t)
	t.Setenv("GRAM_HOOKS_API_KEY", "test-hooks-key")
	cfg := telemetryTestConfig("https://gram.example")
	cfg.ConfigError = "read speakeasy.json: permission denied"
	rec := newTelemetryRecorder(NewRelay(cfg))
	require.Nil(t, rec, "an unknown deployment identity must not export anywhere")
}

func TestNewTelemetryRecorderHonorsKillSwitch(t *testing.T) {
	telemetryTestEnv(t)
	t.Setenv("GRAM_HOOKS_API_KEY", "test-hooks-key")
	t.Setenv("GRAM_HOOKS_DISABLE_TELEMETRY", "1")
	rec := newTelemetryRecorder(NewRelay(telemetryTestConfig("https://gram.example")))
	require.Nil(t, rec)
}
