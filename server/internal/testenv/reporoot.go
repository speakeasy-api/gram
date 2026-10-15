package testenv

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"golang.org/x/mod/modfile"
)

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
