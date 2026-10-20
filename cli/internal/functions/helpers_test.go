package functions

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/require"
)

// fakeBinDir returns a directory for fake executables. Tests that use it are
// skipped on Windows, where the shell scripts cannot run.
func fakeBinDir(t *testing.T) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("fake executables are shell scripts")
	}
	return t.TempDir()
}

// writeFakeBin writes an executable shell script called name into dir.
func writeFakeBin(t *testing.T, dir string, name string, script string) {
	t.Helper()
	path := filepath.Join(dir, name)
	require.NoError(t, os.WriteFile(path, []byte("#!/bin/sh\n"+script), 0o600))
	require.NoError(t, os.Chmod(path, 0o700)) // #nosec G302 -- test executable.
}

// testRunner returns a Runner whose PATH is only binDir, with output in out.
func testRunner(binDir string, out *bytes.Buffer, env ...string) Runner {
	return Runner{
		Env:    append([]string{"PATH=" + binDir}, env...),
		Stdin:  nil,
		Stdout: out,
		Stderr: out,
	}
}

func writeFile(t *testing.T, path string, content string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o750))
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
}
