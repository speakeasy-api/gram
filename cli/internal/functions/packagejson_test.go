package functions

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRewritePackageJSON(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "package.json")
	writeFile(t, path, `{
  "type": "module",
  "name": "template-name",
  "version": "1.2.3",
  "scripts": {
    "dev": "a && b",
    "_:build": "speakeasy functions build",
    "push": "speakeasy functions push"
  },
  "dependencies": {
    "@gram-ai/functions": "workspace:",
    "@modelcontextprotocol/sdk": "catalog:",
    "zod": "^4"
  },
  "devDependencies": {
    "typescript": "^5"
  }
}
`)

	err := rewritePackageJSON(path, packageJSONRewrite{
		Name: "@acme/my-tools",
		Dependencies: map[string]string{
			"@gram-ai/functions":        "^0.19.0",
			"@modelcontextprotocol/sdk": "^1.20.1",
			"not-a-dependency":          "^9.9.9",
		},
	})
	require.NoError(t, err)

	got, err := os.ReadFile(filepath.Clean(path))
	require.NoError(t, err)
	// Compare line by line: the key order and formatting matter here, not
	// only the JSON value.
	require.Equal(t, strings.Split(`{
  "type": "module",
  "name": "@acme/my-tools",
  "version": "0.0.0",
  "scripts": {
    "dev": "a && b",
    "build": "speakeasy functions build",
    "push": "speakeasy functions push"
  },
  "dependencies": {
    "@gram-ai/functions": "^0.19.0",
    "@modelcontextprotocol/sdk": "^1.20.1",
    "zod": "^4"
  },
  "devDependencies": {
    "typescript": "^5"
  }
}
`, "\n"), strings.Split(string(got), "\n"))
}

func TestRewritePackageJSON_AddsMissingFields(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "package.json")
	writeFile(t, path, `{"private": true}`)

	require.NoError(t, rewritePackageJSON(path, packageJSONRewrite{Name: "tools", Dependencies: nil}))

	got, err := os.ReadFile(filepath.Clean(path))
	require.NoError(t, err)
	require.JSONEq(t, `{"private": true, "name": "tools", "version": "0.0.0"}`, string(got))
}

func TestRewritePackageJSON_RejectsInvalidJSON(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "package.json")
	writeFile(t, path, `[1, 2]`)

	err := rewritePackageJSON(path, packageJSONRewrite{Name: "tools", Dependencies: nil})
	require.ErrorContains(t, err, "expected a JSON object")
}
