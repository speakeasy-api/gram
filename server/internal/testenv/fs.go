package testenv

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

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
