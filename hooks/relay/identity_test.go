package relay

import (
	"encoding/base64"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"slices"
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

// startIdentitySocket serves body for GET /v1/identity on a unix socket and
// returns its path. status lets tests exercise non-200 handling.
func startIdentitySocket(t *testing.T, status int, body string) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("unix-socket fixture is POSIX-only")
	}
	// Not t.TempDir(): it embeds the test name and can blow the ~104-byte
	// sockaddr_un path limit on macOS.
	dir, err := os.MkdirTemp("", "sk")
	require.NoError(t, err)
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	socket := filepath.Join(dir, "agent.sock")
	ln, err := net.Listen("unix", socket)
	require.NoError(t, err)
	srv := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Not require: FailNow from a non-test goroutine can hang the test.
		// t.Errorf is goroutine-safe, and the 404 makes the client-side
		// assertion fail loudly too.
		if r.URL.Path != "/v1/identity" {
			t.Errorf("unexpected request path %q", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	})}
	go func() { _ = srv.Serve(ln) }()
	t.Cleanup(func() { _ = srv.Close() })
	return socket
}

func TestDeviceAgentEmailSocketRescue(t *testing.T) {
	// A daemon running from a location the exec chain can't guess (no PATH
	// entry, no well-known dir) is still reachable at its fixed socket path.
	socket := startIdentitySocket(t, http.StatusOK, `{"v":1,"enrolled":true,"email":"socket@example.com","source":"managed"}`)
	t.Setenv("SPEAKEASY_SOCKET", socket)
	t.Setenv("HOME", t.TempDir()) // no well-known installs
	t.Setenv("PATH", t.TempDir()) // bare "speakeasyd" resolves nowhere
	t.Setenv("GRAM_DEVICE_AGENT_COMMANDS", "")

	require.Equal(t, "socket@example.com", deviceAgentEmail(t.Context()))
}

func TestDeviceAgentEmailEnvOverrideSilencesSocket(t *testing.T) {
	// The env override is a complete off switch: even a live, answering
	// socket must not be consulted.
	socket := startIdentitySocket(t, http.StatusOK, `{"v":1,"enrolled":true,"email":"socket@example.com","source":"managed"}`)
	t.Setenv("SPEAKEASY_SOCKET", socket)
	t.Setenv("GRAM_DEVICE_AGENT_COMMANDS", "speakeasy-hooks-test-missing-device-agent")

	require.Empty(t, deviceAgentEmail(t.Context()))
}

func TestSocketIdentityEmailRejectsUnusableResponses(t *testing.T) {
	cases := []struct {
		name   string
		status int
		body   string
	}{
		{"contract version too new", http.StatusOK, `{"v":2,"enrolled":true,"email":"new@example.com"}`},
		{"not enrolled", http.StatusOK, `{"v":1,"enrolled":false}`},
		{"invalid email", http.StatusOK, `{"v":1,"enrolled":true,"email":"not-an-email"}`},
		{"http error", http.StatusInternalServerError, `boom`},
		{"garbage body", http.StatusOK, `not json`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			socket := startIdentitySocket(t, tc.status, tc.body)
			t.Setenv("SPEAKEASY_SOCKET", socket)
			require.Empty(t, socketIdentityEmail(t.Context()))
		})
	}
}

func TestSocketIdentityEmailAbsentSocket(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix-socket fixture is POSIX-only")
	}
	t.Setenv("SPEAKEASY_SOCKET", filepath.Join(t.TempDir(), "nope.sock"))
	require.Empty(t, socketIdentityEmail(t.Context()))
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

// TestProbeCodexBinarySkipsUnrunnableCandidates: the probe's result is exec'd,
// so it must return the first candidate that is actually runnable and skip a
// path that merely exists. Driven through explicit candidates rather than the
// process environment — pointing PATH or CODEX_HOME at a fixture would let a
// miss fall through to the real /Applications copy and execute it.
func TestProbeCodexBinarySkipsUnrunnableCandidates(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("executable-bit fixture is POSIX-only")
	}

	dir := t.TempDir()
	notExecutable := filepath.Join(dir, "present-but-not-runnable")
	runnable := filepath.Join(dir, "codex")
	require.NoError(t, os.WriteFile(notExecutable, []byte("#!/bin/sh\nexit 0\n"), 0o600))
	require.NoError(t, os.WriteFile(runnable, []byte("#!/bin/sh\nexit 0\n"), 0o700))

	candidates := []string{filepath.Join(dir, "missing"), notExecutable, runnable}
	require.Equal(t, runnable, probeCodexBinary(candidates))

	require.Empty(t, probeCodexBinary([]string{filepath.Join(dir, "missing")}))
	require.Empty(t, probeCodexBinary(nil))
}

// TestCodexBinaryCandidatesProbeTheUnifiedAppFirst pins the ordering the
// /Applications entries cannot exercise on a test machine: the frozen
// Codex.app must never win over the ChatGPT app that supersedes it.
func TestCodexBinaryCandidatesProbeTheUnifiedAppFirst(t *testing.T) {
	candidates := codexBinaryCandidates("/home/dev", "/home/dev/.codex")

	unified := slices.Index(candidates, "/Applications/ChatGPT.app/Contents/Resources/codex")
	frozen := slices.Index(candidates, "/Applications/Codex.app/Contents/Resources/codex")

	require.NotEqual(t, -1, unified, "the unified ChatGPT app bundles the codex binary and must be probed")
	require.NotEqual(t, -1, frozen)
	require.Less(t, unified, frozen)

	// Both app bundles rank below a managed or user-owned install.
	require.Less(t, slices.Index(candidates, "/home/dev/.codex/packages/standalone/current/bin/codex"), unified)
	require.Less(t, slices.Index(candidates, "/home/dev/.local/bin/codex"), unified)
}
