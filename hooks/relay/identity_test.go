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

// TestCodexBinaryCandidatesIncludeEditorExtensions: the ChatGPT editor
// extensions bundle their own codex, and on a machine that runs Codex only
// from an editor it is the only copy present — no CLI on PATH, no app bundle.
// Missing it there means no MCP inventory, which the shadow-MCP guard reads as
// an unprovable target and denies (DNO-771).
func TestCodexBinaryCandidatesIncludeEditorExtensions(t *testing.T) {
	home := t.TempDir()
	// Versions installed side by side, as an editor leaves them across an
	// upgrade. 26.10.0 over 26.9.0 is the case a lexical sort inverts, and the
	// .cursor copy must beat the older .vscode ones even though it is globbed
	// from a later directory.
	oldest := writeCodexExtension(t, home, ".vscode", "26.7.1")
	older := writeCodexExtension(t, home, ".vscode", "26.9.0")
	newer := writeCodexExtension(t, home, ".cursor", "26.10.0")

	candidates := codexBinaryCandidates(home, filepath.Join(home, ".codex"))

	require.Contains(t, candidates, newer)
	require.Contains(t, candidates, older)
	require.Less(t, slices.Index(candidates, older), slices.Index(candidates, oldest),
		"a newer extension build must be probed before an older one left behind")
	require.Less(t, slices.Index(candidates, newer), slices.Index(candidates, older),
		"26.10.0 is newer than 26.9.0; comparing the version components as text gets that backwards")
	require.Less(t, slices.Index(candidates, newer), slices.Index(candidates, oldest),
		"the newest build wins wherever it is installed, not just within its own editor directory")
	require.Less(t, slices.Index(candidates, newer),
		slices.Index(candidates, "/Applications/Codex.app/Contents/Resources/codex"),
		"a maintained extension copy outranks the frozen Codex.app")

	// A home with no editor extensions contributes nothing.
	require.Empty(t, codexEditorExtensionBinaries(t.TempDir()))
	require.Empty(t, codexEditorExtensionBinaries(""))
}

// TestCodexEditorExtensionBinariesKeepUnreadableVersions: an extension
// directory naming scheme we cannot parse must not cost us the binary. It
// ranks below everything we can read a version for, but on a machine where it
// is the only copy installed it is still the difference between an MCP
// inventory and none.
func TestCodexEditorExtensionBinariesKeepUnreadableVersions(t *testing.T) {
	home := t.TempDir()
	unreadable := writeCodexExtension(t, home, ".vscode", "nightly")
	known := writeCodexExtension(t, home, ".windsurf", "26.9.0")

	found := codexEditorExtensionBinaries(home)

	require.Equal(t, []string{known, unreadable}, found,
		"an unparseable version sorts last but is never dropped")

	onlyUnreadable := t.TempDir()
	require.Equal(t, []string{writeCodexExtension(t, onlyUnreadable, ".cursor", "nightly")},
		codexEditorExtensionBinaries(onlyUnreadable))
}

// writeCodexExtension lays down the codex binary an openai.chatgpt editor
// extension bundles and returns its path.
func writeCodexExtension(t *testing.T, home, editorDir, version string) string {
	t.Helper()

	path := filepath.Join(home, editorDir, "extensions", "openai.chatgpt-"+version+"-darwin-arm64", "bin", "macos-aarch64", "codex")
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o700))
	require.NoError(t, os.WriteFile(path, []byte("#!/bin/sh\nexit 0\n"), 0o700))
	return path
}
