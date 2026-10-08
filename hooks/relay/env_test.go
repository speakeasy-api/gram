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
	for _, name := range []string{"HOOKS_SERVER_URL", "HOOKS_SITE_URL", "HOOKS_PROJECT_SLUG", "HOOKS_ORG_ID", "HOOKS_ORG_KEY"} {
		t.Setenv("SPEAKEASY_AI_"+name, "")
	}
	t.Setenv("GRAM_HOOKS_SERVER_URL", "https://legacy.example.test")
	t.Setenv("GRAM_HOOKS_PROJECT_SLUG", "legacy-project")
	t.Setenv("GRAM_HOOKS_ORG_ID", "legacy-org")

	require.Equal(t, "https://legacy.example.test", Env("HOOKS_SERVER_URL"))
	require.Equal(t, "legacy-project", Env("HOOKS_PROJECT_SLUG"))
	require.Equal(t, "legacy-org", Env("HOOKS_ORG_ID"))
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

func TestAuthFilePath_HonorsLegacyOverride(t *testing.T) {
	override := filepath.Join(t.TempDir(), "custom.env")
	t.Setenv("SPEAKEASY_AI_HOOKS_AUTH_FILE", "")
	t.Setenv("GRAM_HOOKS_AUTH_FILE", override)
	require.Equal(t, override, authFilePath())
}
