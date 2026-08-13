package plugins

import (
	"encoding/base64"
	"encoding/binary"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf16"

	"github.com/stretchr/testify/require"
)

// codexHookCommandProbe runs a generated Codex hook command the way Codex runs
// it — plugin paths exported in the environment and the whole line handed to a
// shell as one argument — and returns stdout, stderr, and the exit code.
func codexHookCommandProbe(t *testing.T, command, pluginRoot, pluginData string, extraEnv ...string) (string, string, int) {
	t.Helper()
	// Codex hands the line to $SHELL, so run it through one rather than
	// splitting it here.
	shell, err := exec.LookPath("bash")
	require.NoError(t, err, "bash is required to exercise the Unix hook command")

	cmd := exec.CommandContext(t.Context(), shell, "-c", command)
	cmd.Env = append(os.Environ(), "PLUGIN_ROOT="+pluginRoot, "PLUGIN_DATA="+pluginData)
	cmd.Env = append(cmd.Env, extraEnv...)
	var stdout, stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	runErr := cmd.Run()
	var exitErr *exec.ExitError
	if runErr != nil && !errors.As(runErr, &exitErr) {
		require.NoError(t, runErr)
	}
	return stdout.String(), stderr.String(), cmd.ProcessState.ExitCode()
}

// codexPluginCache lays out a plugin version directory the way Codex's plugin
// store does (<cache>/<marketplace>/<plugin>/<version>) with a bootstrap script
// that echoes the arguments it was handed, and returns the version root.
func codexPluginCache(t *testing.T, base, version string) string {
	t.Helper()
	root := filepath.Join(base, "plugins", "cache", "acme-speakeasy", "acme-observability-codex", version)
	require.NoError(t, os.MkdirAll(filepath.Join(root, "hooks"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "hooks", "bootstrap.sh"),
		[]byte("#!/usr/bin/env bash\nprintf 'script=%s args=%s\\n' \"$0\" \"$*\"\n"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "speakeasy.json"), []byte("{}\n"), 0o644))
	return root
}

func TestCodexHookCommandRunsBootstrapFromPluginRoot(t *testing.T) {
	t.Parallel()
	base := t.TempDir()
	root := codexPluginCache(t, base, "0.28.100")
	data := filepath.Join(base, "plugins", "data", "acme-observability-codex-acme-speakeasy")

	stdout, stderr, code := codexHookCommandProbe(t, codexHookCommandString(330, false, false), root, data)

	require.Equal(t, 0, code, "stderr: %s", stderr)
	require.Contains(t, stdout, "script="+filepath.Join(root, "hooks", "bootstrap.sh"))
	require.Contains(t, stdout, "--config="+filepath.Join(root, "speakeasy.json"))
	require.Contains(t, stdout, "agenthooks run --provider=codex --timeout=330s")
}

// Codex reinstalls a republished plugin into a sibling version directory and
// deletes the previous one, so a hook command it resolved before the swap names
// a script that is gone. `bash <missing file>` exits 127 with no output, which
// is the bare "hook exited with code 127" users see on SessionStart and
// UserPromptSubmit; the command must re-resolve instead.
func TestCodexHookCommandSurvivesPluginCacheVersionSwap(t *testing.T) {
	t.Parallel()
	base := t.TempDir()
	live := codexPluginCache(t, base, "0.28.101")
	stale := filepath.Join(filepath.Dir(live), "0.28.100")
	incomplete := filepath.Join(filepath.Dir(live), "0.28.102", "hooks")
	require.NoError(t, os.MkdirAll(incomplete, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(incomplete, "bootstrap.sh"), []byte("#!/usr/bin/env bash\nexit 99\n"), 0o755))
	data := filepath.Join(base, "plugins", "data", "acme-observability-codex-acme-speakeasy")

	stdout, stderr, code := codexHookCommandProbe(t, codexHookCommandString(60, false, false), stale, data)

	require.Equal(t, 0, code, "stderr: %s", stderr)
	require.Contains(t, stdout, "script="+filepath.Join(live, "hooks", "bootstrap.sh"))
	require.Contains(t, stdout, "--config="+filepath.Join(live, "speakeasy.json"),
		"an incomplete newer sibling must not mask the newest complete payload")
}

func TestCodexHookCommandUsesPersistedPayloadWhenPluginCacheIsGone(t *testing.T) {
	t.Parallel()
	base := t.TempDir()
	stale := filepath.Join(base, "plugins", "cache", "acme-speakeasy", "acme-observability-codex", "0.28.100")
	data := filepath.Join(base, "plugins", "data", "acme-observability-codex-acme-speakeasy")
	stable := filepath.Join(data, filepath.FromSlash(codexHooksStablePayloadSubdir))
	generation := filepath.Join(stable, "generations", "unix-test")
	require.NoError(t, os.MkdirAll(filepath.Join(generation, "hooks"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(generation, "hooks", "bootstrap.sh"),
		[]byte("#!/usr/bin/env bash\nprintf 'script=%s args=%s\\n' \"$0\" \"$*\"\n"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(generation, "speakeasy.json"), []byte("{}\n"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(stable, ".unix-current"), []byte("unix-test\n"), 0o600))

	stdout, stderr, code := codexHookCommandProbe(t, codexHookCommandString(60, false, false), stale, data)

	require.Equal(t, 0, code, "stderr: %s", stderr)
	require.Contains(t, stdout, "script="+filepath.Join(generation, "hooks", "bootstrap.sh"))
	require.Contains(t, stdout, "--config="+filepath.Join(generation, "speakeasy.json"))
}

func TestCodexHookCommandReportsMissingPayloadInsteadOfExit127(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name     string
		failOpen bool
		wantCode int
	}{
		{name: "fail closed", failOpen: false, wantCode: 1},
		{name: "fail open", failOpen: true, wantCode: 0},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			missing := filepath.Join(t.TempDir(), "plugins", "cache", "acme-speakeasy", "acme-observability-codex", "0.28.100")
			data := filepath.Join(t.TempDir(), "plugins", "data", "acme-observability-codex-acme-speakeasy")

			stdout, stderr, code := codexHookCommandProbe(t, codexHookCommandString(60, false, tt.failOpen), missing, data)

			require.Equal(t, tt.wantCode, code)
			require.Empty(t, stdout)
			require.Contains(t, stderr, "speakeasy-hooks: no complete hook payload for "+missing,
				"the failure must name the root it looked under, not exit 127 silently")
		})
	}
}

func TestCodexHookCommandForwardsAsyncFlag(t *testing.T) {
	t.Parallel()
	base := t.TempDir()
	root := codexPluginCache(t, base, "0.28.100")
	data := filepath.Join(base, "plugins", "data", "acme-observability-codex-acme-speakeasy")

	stdout, stderr, code := codexHookCommandProbe(t, codexHookCommandString(60, true, false), root, data)

	require.Equal(t, 0, code, "stderr: %s", stderr)
	require.Contains(t, stdout, "agenthooks run --provider=codex --timeout=60s --async")
}

func TestCodexPowerShellCommandUsesRuntimePluginPaths(t *testing.T) {
	t.Parallel()
	command := codexHookCommandStringWindows(60, true, false)
	const prefix = `powershell.exe -NoLogo -NoProfile -ExecutionPolicy Bypass -EncodedCommand `
	require.True(t, strings.HasPrefix(command, prefix))
	encoded, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(command, prefix))
	require.NoError(t, err)
	require.Zero(t, len(encoded)%2)
	units := make([]uint16, len(encoded)/2)
	for i := range units {
		units[i] = binary.LittleEndian.Uint16(encoded[2*i:])
	}
	script := string(utf16.Decode(units))
	require.Contains(t, script, `$r=$env:PLUGIN_ROOT`)
	require.Contains(t, script, `$d=Join-Path $env:PLUGIN_DATA`)
	require.Contains(t, script, `.windows-current`)
	require.Contains(t, script, `--provider=codex --timeout=60s --async`)
}

// The command is single-quoted for `bash -c`, so a single quote anywhere inside
// would terminate the script early and hand the rest to the shell.
func TestCodexHookCommandCarriesNoSingleQuotes(t *testing.T) {
	t.Parallel()
	for _, event := range CodexObservabilityHookEvents {
		timeoutSeconds, async := codexHookParams(event)
		for _, failOpen := range []bool{false, true} {
			command := codexHookCommandString(timeoutSeconds, async, failOpen)
			require.Equal(t, 2, strings.Count(command, "'"), "event %q: only the bash -c wrapper may quote", event)
			require.True(t, strings.HasPrefix(command, "bash -c '"))
			require.True(t, strings.HasSuffix(command, "'"))
		}
	}
}
