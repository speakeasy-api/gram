package plugins

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// codexHookCommandProbe runs a generated Codex hook command the way Codex runs
// it — the substituted plugin root baked into the command string, the same path
// exported as PLUGIN_ROOT, and the whole line handed to a shell as one argument
// — and returns stdout, stderr, and the exit code.
func codexHookCommandProbe(t *testing.T, command, pluginRoot string) (string, string, int) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the Unix hook command is exercised on Unix; Windows uses commandWindows")
	}
	shell, err := exec.LookPath("bash")
	require.NoError(t, err, "bash is required to exercise the Unix hook command")

	cmd := exec.CommandContext(t.Context(), shell, "-c", strings.ReplaceAll(command, codexPluginRootPlaceholder, pluginRoot))
	cmd.Env = append(os.Environ(), "PLUGIN_ROOT="+pluginRoot)
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

	stdout, stderr, code := codexHookCommandProbe(t, codexHookCommandString(330, false, false), root)

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

	stdout, stderr, code := codexHookCommandProbe(t, codexHookCommandString(60, false, false), stale)

	require.Equal(t, 0, code, "stderr: %s", stderr)
	require.Contains(t, stdout, "script="+filepath.Join(live, "hooks", "bootstrap.sh"))
	require.Contains(t, stdout, "--config="+filepath.Join(live, "speakeasy.json"),
		"the recovered root must also supply the deployment identity")
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

			stdout, stderr, code := codexHookCommandProbe(t, codexHookCommandString(60, false, tt.failOpen), missing)

			require.Equal(t, tt.wantCode, code)
			require.Empty(t, stdout)
			require.Contains(t, stderr, "speakeasy-hooks: no hook payload under "+missing,
				"the failure must name the root it looked under, not exit 127 silently")
		})
	}
}

func TestCodexHookCommandForwardsAsyncFlag(t *testing.T) {
	t.Parallel()
	base := t.TempDir()
	root := codexPluginCache(t, base, "0.28.100")

	stdout, stderr, code := codexHookCommandProbe(t, codexHookCommandString(60, true, false), root)

	require.Equal(t, 0, code, "stderr: %s", stderr)
	require.Contains(t, stdout, "agenthooks run --provider=codex --timeout=60s --async")
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
