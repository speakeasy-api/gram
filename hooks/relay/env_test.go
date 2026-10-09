package relay

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestEnv_PrefersSpeakeasyAIName(t *testing.T) {
	t.Setenv("SPEAKEASY_AI_HOOKS_PROJECT_SLUG", " new-project ")
	t.Setenv("GRAM_HOOKS_PROJECT_SLUG", "legacy-project")
	require.Equal(t, "new-project", Env("HOOKS_PROJECT_SLUG"))
}

func TestEnv_FallsBackToDeprecatedGramName(t *testing.T) {
	t.Setenv("SPEAKEASY_AI_HOOKS_PROJECT_SLUG", "")
	t.Setenv("GRAM_HOOKS_PROJECT_SLUG", "legacy-project")
	require.Equal(t, "legacy-project", Env("HOOKS_PROJECT_SLUG"))
}

func TestEnv_LegacyNamesReachConfig(t *testing.T) {
	for _, name := range []string{"HOOKS_SERVER_URL", "HOOKS_SITE_URL", "HOOKS_PROJECT_SLUG", "HOOKS_ORG_ID", "HOOKS_ORG_KEY", "HOOKS_BROWSER_LOGIN", "HOOKS_NONBLOCKING", "HOOKS_OBSERVABILITY_MODE", "HOOKS_DEBUG_LOG"} {
		t.Setenv("SPEAKEASY_AI_"+name, "")
		t.Setenv("GRAM_"+name, "")
	}
	t.Setenv("GRAM_HOOKS_SERVER_URL", "https://legacy.example.test/")
	t.Setenv("GRAM_HOOKS_SITE_URL", "https://legacy-site.example.test")
	t.Setenv("GRAM_HOOKS_PROJECT_SLUG", "legacy-project")
	t.Setenv("GRAM_HOOKS_ORG_ID", "legacy-org")
	t.Setenv("GRAM_HOOKS_ORG_KEY", "legacy-org-key")
	t.Setenv("GRAM_HOOKS_BROWSER_LOGIN", "true")
	t.Setenv("GRAM_HOOKS_NONBLOCKING", "1")

	cfg := LoadConfig(Config{})
	require.Equal(t, "https://legacy.example.test", cfg.ServerURL)
	require.Equal(t, "https://legacy-site.example.test", cfg.SiteURL)
	require.Equal(t, "legacy-project", cfg.ProjectSlug)
	require.Equal(t, "legacy-org", cfg.OrgID)
	require.Equal(t, "legacy-org-key", cfg.HooksAPIKey)
	require.True(t, cfg.BrowserLogin)
	require.True(t, cfg.Nonblocking)

	// The SPEAKEASY_AI_* name wins when both are set.
	t.Setenv("SPEAKEASY_AI_HOOKS_PROJECT_SLUG", "new-project")
	require.Equal(t, "new-project", LoadConfig(Config{}).ProjectSlug)
}

func TestAuthFilePath_DefaultsToSpeakeasyAIDir(t *testing.T) {
	configHome := t.TempDir()
	t.Setenv("SPEAKEASY_AI_HOOKS_AUTH_FILE", "")
	t.Setenv("GRAM_HOOKS_AUTH_FILE", "")
	t.Setenv("XDG_CONFIG_HOME", configHome)

	require.Equal(t, filepath.Join(configHome, "speakeasy-ai", "hooks-auth.env"), authFilePath())
}

func TestAuthFilePath_KeepsUsingLegacyCache(t *testing.T) {
	configHome := t.TempDir()
	t.Setenv("SPEAKEASY_AI_HOOKS_AUTH_FILE", "")
	t.Setenv("GRAM_HOOKS_AUTH_FILE", "")
	t.Setenv("XDG_CONFIG_HOME", configHome)
	legacy := filepath.Join(configHome, "gram", "hooks-auth.env")
	require.NoError(t, os.MkdirAll(filepath.Dir(legacy), 0o700))
	require.NoError(t, os.WriteFile(legacy, []byte("api_key=k\n"), 0o600))

	require.Equal(t, legacy, authFilePath(), "an existing legacy cache stays in use")

	current := filepath.Join(configHome, "speakeasy-ai", "hooks-auth.env")
	require.NoError(t, os.MkdirAll(filepath.Dir(current), 0o700))
	require.NoError(t, os.WriteFile(current, []byte("api_key=k\n"), 0o600))
	require.Equal(t, current, authFilePath(), "the new cache wins once it exists")
}

func TestAuthFilePath_KeepsLegacyPathWhileItsOrgSettingsExist(t *testing.T) {
	configHome := t.TempDir()
	t.Setenv("SPEAKEASY_AI_HOOKS_AUTH_FILE", "")
	t.Setenv("GRAM_HOOKS_AUTH_FILE", "")
	t.Setenv("XDG_CONFIG_HOME", configHome)
	legacy := filepath.Join(configHome, "gram", "hooks-auth.env")
	require.NoError(t, os.MkdirAll(filepath.Dir(legacy), 0o700))
	require.NoError(t, os.WriteFile(legacy+".org-settings.json", []byte("{}"), 0o600))

	require.Equal(t, legacy, authFilePath(), "a forgotten legacy key must not orphan its saved org settings")
}

func TestForgetAuth_ClearsBothDefaultCaches(t *testing.T) {
	configHome := t.TempDir()
	t.Setenv("SPEAKEASY_AI_HOOKS_AUTH_FILE", "")
	t.Setenv("GRAM_HOOKS_AUTH_FILE", "")
	t.Setenv("XDG_CONFIG_HOME", configHome)
	current := filepath.Join(configHome, "speakeasy-ai", "hooks-auth.env")
	legacy := filepath.Join(configHome, "gram", "hooks-auth.env")
	for _, path := range []string{current, legacy} {
		require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o700))
		require.NoError(t, os.WriteFile(path, []byte("api_key=k\n"), 0o600))
	}

	forgetAuth()

	require.NoFileExists(t, current)
	require.NoFileExists(t, legacy, "a stale legacy key must not take over once the current one is forgotten")
}

func TestAuthFilePath_HonorsLegacyOverride(t *testing.T) {
	override := filepath.Join(t.TempDir(), "custom.env")
	t.Setenv("SPEAKEASY_AI_HOOKS_AUTH_FILE", "")
	t.Setenv("GRAM_HOOKS_AUTH_FILE", override)
	require.Equal(t, override, authFilePath())
}
