package gram

import (
	"bytes"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// A subprocess stand-in for the account executable: handle cancellation, finish
// recovery, then exit. No infrastructure or real Go build is involved.
func TestAccountWrapperChildProcess(t *testing.T) {
	t.Parallel()
	if os.Getenv("ACCOUNT_WRAPPER_HELPER") != "1" {
		return
	}
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, syscall.SIGINT, syscall.SIGTERM)
	dir := os.Getenv("ACCOUNT_WRAPPER_CASE")
	if err := os.WriteFile(filepath.Join(dir, "ready"), nil, 0600); err != nil {
		os.Exit(90)
	}
	ticker := time.NewTicker(10 * time.Millisecond)
	// Every helper path exits the process, which also releases the ticker.
	for {
		select {
		case <-signals:
			time.Sleep(40 * time.Millisecond) // recovery must finish before wrapper exits
			if err := os.WriteFile(filepath.Join(dir, "recovered"), nil, 0600); err != nil {
				os.Exit(91)
			}
			os.Exit(1)
		case <-ticker.C:
			if _, err := os.Stat(filepath.Join(dir, "release")); err == nil {
				if os.Getenv("ACCOUNT_WRAPPER_FAIL") == "action" {
					os.Exit(23)
				}
				os.Exit(0)
			}
		}
	}
}

func TestAccountWrapper(t *testing.T) {
	t.Parallel()
	script, err := filepath.Abs("../../../.mise-tasks/account.sh")
	require.NoError(t, err)
	binary, err := os.Executable()
	require.NoError(t, err)
	fakeBin := t.TempDir()
	buildTemp := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(fakeBin, "mise"), []byte(`#!/bin/sh
[ "$1" = run ] && [ "$2" = -q ] && [ "$3" = build:server-cache ] && [ "$4" = --out ] || exit 92
printf '%s' "$5" > "$ACCOUNT_WRAPPER_CASE/build-path"
[ "$ACCOUNT_WRAPPER_FAIL" != build ] || exit 19
printf '#!/bin/sh\nexec "$ACCOUNT_WRAPPER_TEST_BINARY" -test.run=^TestAccountWrapperChildProcess$\n' > "$5"
chmod 700 "$5"
`), 0700))
	type invocation struct {
		cmd    *exec.Cmd
		dir    string
		output *bytes.Buffer
		stdout *bytes.Buffer
	}
	start := func(failure string) invocation {
		dir := t.TempDir()
		cmd := exec.Command("bash", script, "repair")
		cmd.Env = append(os.Environ(), "PATH="+fakeBin+string(os.PathListSeparator)+os.Getenv("PATH"), "TMPDIR="+buildTemp, "ACCOUNT_WRAPPER_HELPER=1", "ACCOUNT_WRAPPER_CASE="+dir, "ACCOUNT_WRAPPER_TEST_BINARY="+binary, "ACCOUNT_WRAPPER_FAIL="+failure)
		var out, stdout bytes.Buffer
		cmd.Stdout = &stdout
		cmd.Stderr = &out
		require.NoError(t, cmd.Start())
		t.Cleanup(func() { _ = cmd.Process.Kill() })
		return invocation{cmd, dir, &out, &stdout}
	}
	exists := func(path string) bool { _, err := os.Stat(path); return err == nil }
	ready := func(i invocation) {
		require.Eventually(t, func() bool { return exists(filepath.Join(i.dir, "ready")) }, 5*time.Second, 10*time.Millisecond)
	}
	outputPath := func(i invocation) string {
		data, err := os.ReadFile(filepath.Join(i.dir, "build-path"))
		require.NoError(t, err)
		return string(data)
	}
	wait := func(i invocation, code int) {
		done := make(chan error, 1)
		go func() { done <- i.cmd.Wait() }()
		select {
		case err := <-done:
			if code == 0 {
				require.NoError(t, err, i.output.String())
			} else {
				require.Error(t, err)
			}
			require.Equal(t, code, i.cmd.ProcessState.ExitCode(), i.output.String())
		case <-time.After(5 * time.Second):
			t.Fatal("wrapper failed to finish")
		}
		require.NoDirExists(t, filepath.Dir(outputPath(i)))
		require.Empty(t, i.stdout.String())
		require.Contains(t, i.output.String(), "Preparing command...")
	}
	{
		t.Log("concurrent invocations own binaries")
		first, second := start(""), start("")
		ready(first)
		ready(second)
		require.NotEqual(t, outputPath(first), outputPath(second))
		require.NoError(t, os.WriteFile(filepath.Join(first.dir, "release"), nil, 0600))
		wait(first, 0)
		require.FileExists(t, outputPath(second))
		require.NoError(t, os.WriteFile(filepath.Join(second.dir, "release"), nil, 0600))
		wait(second, 0)
	}
	for _, sig := range []syscall.Signal{syscall.SIGTERM, syscall.SIGINT} {
		t.Logf("forward signal %d and await recovery", sig)
		i := start("")
		ready(i)
		require.NoError(t, i.cmd.Process.Signal(sig))
		wait(i, 128+int(sig))
		require.FileExists(t, filepath.Join(i.dir, "recovered"))
	}
	{
		t.Log("action failure cleanup")
		i := start("action")
		ready(i)
		require.NoError(t, os.WriteFile(filepath.Join(i.dir, "release"), nil, 0600))
		wait(i, 23)
	}
	{
		t.Log("build failure cleanup")
		i := start("build")
		wait(i, 19)
	}
}
