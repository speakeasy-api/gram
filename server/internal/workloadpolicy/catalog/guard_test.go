package catalog_test

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

// repositoryRoot is the checkout root, relative to this package's directory,
// where go test runs.
const repositoryRoot = "../../../.."

// definitionsDir holds the only platform definitions in the repository,
// relative to repositoryRoot.
const definitionsDir = "server/internal/workloadpolicy/catalog/platforms"

// skippedDirs are directory names that never hold the repository's own files.
var skippedDirs = map[string]bool{
	".git":         true,
	"node_modules": true,
	"dist":         true,
}

// TestNoPlatformDefinitionsOutsideTheCatalog keeps one copy of every platform
// definition. A YAML file with a definition's top-level keys anywhere else is a
// second copy that will drift from the one the server loads; tests read the
// embedded catalog, or the fixture generated from it, instead.
func TestNoPlatformDefinitionsOutsideTheCatalog(t *testing.T) {
	t.Parallel()

	var copies []string
	err := filepath.WalkDir(repositoryRoot, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			if skippedDirs[entry.Name()] || isNestedCheckout(path) {
				return filepath.SkipDir
			}
			return nil
		}
		ext := filepath.Ext(path)
		if ext != ".yaml" && ext != ".yml" {
			return nil
		}

		relative, err := filepath.Rel(repositoryRoot, path)
		if err != nil {
			return fmt.Errorf("relative path of %s: %w", path, err)
		}
		if filepath.ToSlash(filepath.Dir(relative)) == definitionsDir {
			return nil
		}

		raw, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("read %s: %w", path, err)
		}
		if looksLikePlatformDefinition(raw) {
			copies = append(copies, filepath.ToSlash(relative))
		}
		return nil
	})
	require.NoError(t, err)
	require.Empty(t, copies, "platform definitions belong only in %s", definitionsDir)
}

// isNestedCheckout reports whether dir, below the repository root, is another
// checkout of the repository, such as a git worktree or a submodule. Its files
// belong to that checkout, not this one.
func isNestedCheckout(dir string) bool {
	if filepath.Clean(dir) == filepath.Clean(repositoryRoot) {
		return false
	}
	_, err := os.Lstat(filepath.Join(dir, ".git"))
	return err == nil
}

// looksLikePlatformDefinition reports whether raw is a YAML mapping carrying
// the keys every platform definition has. A file that does not parse as a YAML
// mapping, such as a templated one, is not a definition.
func looksLikePlatformDefinition(raw []byte) bool {
	var document map[string]yaml.Node
	if err := yaml.Unmarshal(raw, &document); err != nil {
		return false
	}
	for _, key := range []string{"issuer", "jwks_uri", "subject"} {
		if _, ok := document[key]; !ok {
			return false
		}
	}
	return true
}
