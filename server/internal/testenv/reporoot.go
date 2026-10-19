package testenv

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"golang.org/x/mod/modfile"
)

// FindCheckoutRoot locates the source checkout itself, for the few tests that
// must see all of it, such as repository-wide scans or the dashboard's
// node_modules. Under go test it is FindRepoRoot. Under Bazel, FindRepoRoot is
// the test's runfiles tree, which holds only declared inputs, and its go.mod
// links back to the checkout. Such tests depend on undeclared files, so their
// targets are tagged "external" to keep Bazel from caching their results.
func FindCheckoutRoot(ctx context.Context) (string, error) {
	root, err := FindRepoRoot(ctx)
	if err != nil {
		return "", err
	}
	goMod, err := filepath.EvalSymlinks(filepath.Join(root, "go.mod"))
	if err != nil {
		return "", fmt.Errorf("resolve checkout go.mod: %w", err)
	}
	return filepath.Dir(goMod), nil
}

// FindRepoRoot locates the checkout for tests and local development helpers.
// It walks up from the working directory to the Speakeasy module root and uses
// filesystem state rather than compiler source paths so it works with -trimpath
// and always selects the checkout the process is running in.
func FindRepoRoot(ctx context.Context) (string, error) {
	start, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("get working directory: %w", err)
	}
	for dir := start; ; dir = filepath.Dir(dir) {
		if err := ctx.Err(); err != nil {
			return "", fmt.Errorf("find repository root: %w", err)
		}
		// #nosec G304 -- reads only go.mod in ancestors of the local working directory.
		data, err := os.ReadFile(filepath.Join(dir, "go.mod"))
		switch {
		case err == nil:
			if modfile.ModulePath(data) == "github.com/speakeasy-api/gram" {
				return dir, nil
			}
		case !errors.Is(err, os.ErrNotExist):
			return "", fmt.Errorf("read module in %s: %w", dir, err)
		}
		if filepath.Dir(dir) == dir {
			return "", fmt.Errorf("find Speakeasy repository root from %s: no matching go.mod", start)
		}
	}
}
