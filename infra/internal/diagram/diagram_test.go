package diagram

import (
	"os"
	"path/filepath"
	"testing"
)

// repoRoot is three levels up from infra/internal/diagram.
const repoRoot = "../../.."

func writeTempGo(t *testing.T, src string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "src.go")
	if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
		t.Fatalf("write temp file: %v", err)
	}
	return path
}

// TestGoImportsUnaliasedUsesDeclaredPackageName guards the bug where an unaliased
// generated-proto import was keyed by the directory basename (v1) instead of the
// package's declared name (pingv1), silently dropping its publishers/consumers.
func TestGoImportsUnaliasedUsesDeclaredPackageName(t *testing.T) {
	path := writeTempGo(t, `package x

import "github.com/speakeasy-api/gram/infra/gen/gram/ping/v1"

var _ = pingv1.Message{}
`)

	imports, err := goImports(path, repoRoot)
	if err != nil {
		t.Fatalf("goImports: %v", err)
	}

	if got := imports["pingv1"]; got != "gram.ping.v1" {
		t.Errorf("declared package name not resolved: imports[%q] = %q, want %q", "pingv1", got, "gram.ping.v1")
	}
	if _, ok := imports["v1"]; ok {
		t.Errorf("import keyed by directory basename %q; should use declared package name", "v1")
	}
}

// TestGoImportsAliased confirms an explicit alias is used verbatim as the key.
func TestGoImportsAliased(t *testing.T) {
	path := writeTempGo(t, `package x

import riskv1 "github.com/speakeasy-api/gram/infra/gen/gram/risk/v1"

var _ = riskv1.Finding{}
`)

	imports, err := goImports(path, repoRoot)
	if err != nil {
		t.Fatalf("goImports: %v", err)
	}

	if got := imports["riskv1"]; got != "gram.risk.v1" {
		t.Errorf("imports[%q] = %q, want %q", "riskv1", got, "gram.risk.v1")
	}
}
