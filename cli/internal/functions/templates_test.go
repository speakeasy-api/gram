package functions

import (
	"encoding/json"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"slices"
	"testing"

	"github.com/stretchr/testify/require"
)

// The templates are the single source for both this CLI and the npm
// scaffolder, so these tests pin the contract both rely on.

func TestTemplates_MatchEmbeddedDirs(t *testing.T) {
	t.Parallel()

	entries, err := fs.ReadDir(templatesFS, "templates")
	require.NoError(t, err)

	var dirs []string
	for _, e := range entries {
		require.True(t, e.IsDir(), "unexpected file %s in templates", e.Name())
		dirs = append(dirs, e.Name())
	}

	var names []string
	for _, tmpl := range Templates {
		names = append(names, tmpl.Name)
	}

	require.ElementsMatch(t, names, dirs)
}

func TestTemplates_EmbedEveryFileOnDisk(t *testing.T) {
	t.Parallel()

	var onDisk []string
	err := filepath.WalkDir("templates", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() && slices.Contains(skippedTemplateEntries, d.Name()) {
			return fs.SkipDir
		}
		if !d.IsDir() {
			onDisk = append(onDisk, filepath.ToSlash(p))
		}
		return nil
	})
	require.NoError(t, err)

	var embedded []string
	err = fs.WalkDir(templatesFS, "templates", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() && slices.Contains(skippedTemplateEntries, d.Name()) {
			return fs.SkipDir
		}
		if !d.IsDir() {
			embedded = append(embedded, p)
		}
		return nil
	})
	require.NoError(t, err)

	// go:embed drops files whose names start with "." or "_". A template that
	// needs one must ship it under another name and rename it in Init, as
	// gitignore is.
	require.ElementsMatch(t, onDisk, embedded)
}

func TestTemplates_PackageContract(t *testing.T) {
	t.Parallel()

	for _, tmpl := range Templates {
		t.Run(tmpl.Name, func(t *testing.T) {
			t.Parallel()

			raw, err := templatesFS.ReadFile(path.Join("templates", tmpl.Name, "package.json"))
			require.NoError(t, err)

			var pkg struct {
				Scripts      map[string]string `json:"scripts"`
				Dependencies map[string]string `json:"dependencies"`
			}
			require.NoError(t, json.Unmarshal(raw, &pkg))

			require.Equal(t, "speakeasy functions build", pkg.Scripts["_:build"])
			require.Equal(t, "speakeasy functions push", pkg.Scripts["push"])
			require.Contains(t, pkg.Scripts, "dev", "speakeasy functions dev runs this script")
			for name, script := range pkg.Scripts {
				require.NotRegexp(t, `(^|[\s;&|])gf\s`, script, "script %s still calls gf", name)
			}

			require.Contains(t, pkg.Dependencies, SDKPackage)
			require.Contains(t, pkg.Dependencies, mcpSDKPackage)

			steps, err := templatesFS.ReadFile(path.Join("templates", tmpl.Name, "NEXT_STEPS.txt"))
			require.NoError(t, err)
			require.Contains(t, string(steps), "speakeasy functions")
		})
	}
}

func TestTemplates_NotInstalled(t *testing.T) {
	t.Parallel()

	// A node_modules or dist directory under templates/ would be embedded
	// into the CLI binary. Init skips them, but they bloat the build.
	for _, tmpl := range Templates {
		for _, name := range []string{"node_modules", "dist"} {
			_, err := os.Stat(filepath.Join("templates", tmpl.Name, name))
			require.ErrorIs(t, err, fs.ErrNotExist, "remove templates/%s/%s", tmpl.Name, name)
		}
	}
}
