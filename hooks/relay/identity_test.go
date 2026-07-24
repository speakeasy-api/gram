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

func TestDeviceAgentCommandsEnvOverrideIsExclusive(t *testing.T) {
	t.Setenv("GRAM_DEVICE_AGENT_COMMANDS", "one,two")
	require.Equal(t, []string{"one", "two"}, deviceAgentCommands())
}

func TestDeviceAgentEmailUsesAdvertisedPath(t *testing.T) {
	// The daemon advertises its own executable path on startup (the device
	// agent's HOOK_IDENTITY contract), so discovery works from an arbitrary
	// IT-chosen install dir with nothing on PATH.
	if runtime.GOOS == "windows" {
		t.Skip("executable shell fixture is POSIX-only")
	}
	installDir := t.TempDir() // deliberately NOT a well-known location
	agent := filepath.Join(installDir, "speakeasyd")
	require.NoError(t, os.WriteFile(agent, []byte("#!/bin/sh\nprintf '%s' '{\"email\":\"advertised@example.com\"}'\n"), 0o700))

	home := t.TempDir()
	advertDir := filepath.Join(home, "Library", "Application Support", "Speakeasy")
	if runtime.GOOS != "darwin" {
		advertDir = filepath.Join(home, ".local", "state", "speakeasy")
	}
	require.NoError(t, os.MkdirAll(advertDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(advertDir, "agent-path"), []byte(agent+"\n"), 0o644))

	t.Setenv("HOME", home)
	t.Setenv("XDG_STATE_HOME", "")
	t.Setenv("PATH", t.TempDir())
	t.Setenv("GRAM_DEVICE_AGENT_COMMANDS", "")
	t.Setenv("GRAM_DEVICE_AGENT_TIMEOUT_TENTHS", "20")

	require.Equal(t, agent, deviceAgentCommands()[0])
	require.Equal(t, "advertised@example.com", deviceAgentEmail(t.Context()))
}

func TestAdvertisedAgentPathIgnoresBogusContent(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fixture layout is POSIX-only")
	}
	home := t.TempDir()
	advertDir := filepath.Join(home, "Library", "Application Support", "Speakeasy")
	if runtime.GOOS != "darwin" {
		advertDir = filepath.Join(home, ".local", "state", "speakeasy")
	}
	require.NoError(t, os.MkdirAll(advertDir, 0o755))
	t.Setenv("HOME", home)
	t.Setenv("XDG_STATE_HOME", "")

	advert := filepath.Join(advertDir, "agent-path")

	// Relative path: refused.
	require.NoError(t, os.WriteFile(advert, []byte("speakeasyd\n"), 0o644))
	require.Empty(t, advertisedAgentPath())

	// Absolute but nonexistent: refused.
	require.NoError(t, os.WriteFile(advert, []byte(filepath.Join(home, "gone")+"\n"), 0o644))
	require.Empty(t, advertisedAgentPath())

	// Absent file (an older daemon): refused without error.
	require.NoError(t, os.Remove(advert))
	require.Empty(t, advertisedAgentPath())
}

func TestDeviceAgentEmailFindsAgentOffPATH(t *testing.T) {
	// The MDM install layout: speakeasyd lives under the per-user Speakeasy
	// dir and is NOT on PATH. Managed attribution must still resolve via the
	// well-known install locations.
	if runtime.GOOS == "windows" {
		t.Skip("executable shell fixture is POSIX-only")
	}
	home := t.TempDir()
	binDir := filepath.Join(home, "Library", "Application Support", "Speakeasy", "bin")
	if runtime.GOOS != "darwin" {
		// The supported legacy dotdir location — regression coverage for
		// keeping it in the candidate list.
		binDir = filepath.Join(home, ".speakeasy", "bin")
	}
	require.NoError(t, os.MkdirAll(binDir, 0o755))
	agent := filepath.Join(binDir, "speakeasyd")
	require.NoError(t, os.WriteFile(agent, []byte("#!/bin/sh\nprintf '%s' '{\"email\":\"managed@example.com\"}'\n"), 0o700))

	t.Setenv("HOME", home)
	t.Setenv("PATH", t.TempDir()) // bare "speakeasyd" resolves nowhere
	t.Setenv("GRAM_DEVICE_AGENT_COMMANDS", "")
	t.Setenv("GRAM_DEVICE_AGENT_TIMEOUT_TENTHS", "20")

	require.Contains(t, deviceAgentCommands(), agent)
	require.Equal(t, "managed@example.com", deviceAgentEmail(t.Context()))
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
