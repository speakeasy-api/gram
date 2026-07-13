package relay

import (
	"encoding/base64"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestIdentityOutputEmail(t *testing.T) {
	require.Equal(t, "plain@example.com", identityOutputEmail([]byte(" plain@example.com\n")))
	require.Equal(t, "nested@example.com", identityOutputEmail([]byte(`{"identity":{"preferred_username":"nested@example.com"}}`)))
	require.Empty(t, identityOutputEmail([]byte(`{"email":"not-an-email"}`)))
}

func TestDeviceAgentEmail(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("executable shell fixture is POSIX-only")
	}
	dir := t.TempDir()
	agent := filepath.Join(dir, "fake-device-agent")
	require.NoError(t, os.WriteFile(agent, []byte("#!/bin/sh\nprintf '%s' '{\"user_email\":\"device@example.com\"}'\n"), 0o700))
	t.Setenv("GRAM_DEVICE_AGENT_COMMANDS", agent)
	t.Setenv("GRAM_DEVICE_AGENT_TIMEOUT_TENTHS", "20")

	require.Equal(t, "device@example.com", deviceAgentEmail(t.Context()))
}

func TestTopLevelPayloadEmailIgnoresNestedValues(t *testing.T) {
	require.Equal(t, "cursor@example.com", topLevelPayloadEmail([]byte(`{"user_email":"cursor@example.com","tool_input":{"user_email":"nested@example.com"}}`)))
	require.Empty(t, topLevelPayloadEmail([]byte(`{"tool_input":{"user_email":"nested@example.com"}}`)))
}

func TestCodexAppServerEmail(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("executable shell fixture is POSIX-only")
	}
	dir := t.TempDir()
	codex := filepath.Join(dir, "codex")
	script := "#!/bin/sh\n" +
		"IFS= read -r initialize\n" +
		"printf '%s\\n' '{\"id\":71001,\"result\":{}}'\n" +
		"IFS= read -r initialized\n" +
		"IFS= read -r account\n" +
		"printf '%s\\n' '{\"id\":71002,\"result\":{\"account\":{\"email\":\"codex@example.com\"}}}'\n"
	require.NoError(t, os.WriteFile(codex, []byte(script), 0o700))
	t.Setenv("PATH", dir)
	t.Setenv("GRAM_CODEX_IDENTITY_TIMEOUT_TENTHS", "20")

	require.Equal(t, "codex@example.com", codexAppServerEmail(t.Context()))
}

func TestCodexAccountEmailOnlyUsesActiveAccount(t *testing.T) {
	require.Equal(t, "active@example.com", codexAccountEmail(map[string]any{
		"email":   "unrelated@example.com",
		"account": map[string]any{"email": "active@example.com"},
	}))
	require.Empty(t, codexAccountEmail(map[string]any{"email": "unrelated@example.com"}))
}

func TestCodexAuthFileEmailPrefersAccessToken(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("CODEX_HOME", dir)
	accessClaims := base64.RawURLEncoding.EncodeToString([]byte(`{"https://api.openai.com/profile":{"email":"access@example.com"}}`))
	idClaims := base64.RawURLEncoding.EncodeToString([]byte(`{"email":"id@example.com"}`))
	auth := `{"tokens":{"access_token":"x.` + accessClaims + `.x","id_token":"x.` + idClaims + `.x"}}`
	require.NoError(t, os.WriteFile(filepath.Join(dir, "auth.json"), []byte(auth), 0o600))

	require.Equal(t, "access@example.com", codexAuthFileEmail())
}
