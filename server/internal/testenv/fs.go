package testenv

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// rootPath returns the absolute path to a file under the server/ directory. It
// uses runtime.Caller to locate the testenv package source and resolves
// relative to that, so the path is stable regardless of which test
// package invokes it.
func rootPath(elem ...string) string {
	_, thisFile, _, _ := runtime.Caller(0)
	// thisFile is .../server/internal/testenv/fs.go
	// walk up to server/
	serverDir := filepath.Join(filepath.Dir(thisFile), "..", "..")
	return filepath.Join(append([]string{serverDir}, elem...)...)
}

func ReadFixture(t *testing.T, path string) []byte {
	t.Helper()

	p := filepath.Clean(path)

	if !strings.HasPrefix(p, "fixtures") && !strings.HasPrefix(p, "./fixtures") {
		t.Fatalf("fixture path must be in the fixtures directory: %s", path)
	}

	bs, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf("%s: read fixture: %v", path, err)
	}

	return bs
}
