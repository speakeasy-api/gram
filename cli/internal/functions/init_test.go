package functions

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func initOptions(dir string, template string) InitOptions {
	return InitOptions{
		Dir:            dir,
		Template:       template,
		Name:           "@acme/my-tools",
		Git:            false,
		Install:        false,
		PackageManager: "pnpm",
		SDKVersion:     "",
	}
}

func readPackage(t *testing.T, dir string) map[string]any {
	t.Helper()
	raw, err := os.ReadFile(filepath.Clean(filepath.Join(dir, "package.json")))
	require.NoError(t, err)
	var pkg map[string]any
	require.NoError(t, json.Unmarshal(raw, &pkg))
	return pkg
}

func TestInit_Templates(t *testing.T) {
	t.Parallel()

	for _, tmpl := range Templates {
		t.Run(tmpl.Name, func(t *testing.T) {
			t.Parallel()

			dir := filepath.Join(t.TempDir(), "my-tools")
			var out bytes.Buffer
			r := testRunner(t.TempDir(), &out)

			require.NoError(t, r.Init(t.Context(), initOptions(dir, tmpl.Name)))

			for _, f := range []string{"package.json", "tsconfig.json", "README.md", ".gitignore", "src/gram.ts", "src/server.ts"} {
				require.FileExists(t, filepath.Join(dir, f))
			}
			for _, f := range []string{"gitignore", "NEXT_STEPS.txt", "CHANGELOG.md", "node_modules", "dist"} {
				require.NoFileExists(t, filepath.Join(dir, f))
				require.NoDirExists(t, filepath.Join(dir, f))
			}

			pkg := readPackage(t, dir)
			require.Equal(t, "@acme/my-tools", pkg["name"])
			require.Equal(t, "0.0.0", pkg["version"])

			deps, ok := pkg["dependencies"].(map[string]any)
			require.True(t, ok)
			require.Equal(t, "^"+MinSDKVersion, deps[SDKPackage])
			require.Equal(t, mcpSDKVersion, deps[mcpSDKPackage])

			scripts, ok := pkg["scripts"].(map[string]any)
			require.True(t, ok)
			require.Equal(t, "speakeasy functions build", scripts["build"])
			require.Equal(t, "speakeasy functions push", scripts["push"])
			require.NotContains(t, scripts, "_:build")

			require.Contains(t, out.String(), "All done! Jump in with `cd "+dir+"`")
			require.Contains(t, out.String(), "pnpm run dev")
			require.NotContains(t, out.String(), "$PACKAGE_MANAGER")
			require.NotContains(t, out.String(), "$DIR")
		})
	}
}

func TestInit_LinksContributingGuide(t *testing.T) {
	t.Parallel()

	dir := filepath.Join(t.TempDir(), "my-tools")
	r := testRunner(t.TempDir(), &bytes.Buffer{})
	require.NoError(t, r.Init(t.Context(), initOptions(dir, "functions")))

	want, err := os.ReadFile(filepath.Clean(filepath.Join(dir, "CONTRIBUTING.md")))
	require.NoError(t, err)
	for _, name := range []string{"AGENTS.md", "CLAUDE.md"} {
		got, err := os.ReadFile(filepath.Clean(filepath.Join(dir, name)))
		require.NoError(t, err)
		require.Equal(t, want, got, name)

		target, err := os.Readlink(filepath.Join(dir, name))
		if err == nil {
			require.Equal(t, "CONTRIBUTING.md", target)
		}
	}
}

func TestInit_SDKVersionOverride(t *testing.T) {
	t.Parallel()

	dir := filepath.Join(t.TempDir(), "my-tools")
	opts := initOptions(dir, "mcp")
	opts.SDKVersion = "file:/src/gram/ts-framework/functions"
	require.NoError(t, testRunner(t.TempDir(), &bytes.Buffer{}).Init(t.Context(), opts))

	deps, ok := readPackage(t, dir)["dependencies"].(map[string]any)
	require.True(t, ok)
	require.Equal(t, "file:/src/gram/ts-framework/functions", deps[SDKPackage])
}

func TestInit_EmptyExistingDir(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	require.NoError(t, testRunner(t.TempDir(), &bytes.Buffer{}).Init(t.Context(), initOptions(dir, "functions")))
	require.FileExists(t, filepath.Join(dir, "package.json"))
}

func TestInit_NonEmptyDir(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "keep.txt"), "mine")

	err := testRunner(t.TempDir(), &bytes.Buffer{}).Init(t.Context(), initOptions(dir, "functions"))
	require.ErrorContains(t, err, "already exists and is not empty")
	require.NoFileExists(t, filepath.Join(dir, "package.json"))
}

func TestInit_InvalidInputs(t *testing.T) {
	t.Parallel()

	r := testRunner(t.TempDir(), &bytes.Buffer{})

	opts := initOptions(filepath.Join(t.TempDir(), "a"), "nope")
	require.ErrorContains(t, r.Init(t.Context(), opts), `unknown template "nope": choose one of functions, mcp`)

	opts = initOptions(filepath.Join(t.TempDir(), "b"), "functions")
	opts.Name = "Not Valid"
	require.ErrorContains(t, r.Init(t.Context(), opts), `invalid project name "Not Valid"`)
}

func TestInit_GitAndInstall(t *testing.T) {
	t.Parallel()

	bin := fakeBinDir(t)
	log := filepath.Join(t.TempDir(), "calls.log")
	record := `echo "${0##*/} $* @ $(pwd)" >> "` + log + `"` + "\n"
	writeFakeBin(t, bin, "git", record)
	writeFakeBin(t, bin, "pnpm", record)

	dir := filepath.Join(t.TempDir(), "my-tools")
	opts := initOptions(dir, "functions")
	opts.Git = true
	opts.Install = true

	var out bytes.Buffer
	require.NoError(t, testRunner(bin, &out).Init(t.Context(), opts))

	calls, err := os.ReadFile(filepath.Clean(log))
	require.NoError(t, err)
	resolved, err := filepath.EvalSymlinks(dir)
	require.NoError(t, err)
	require.Equal(t, []string{
		"git init --quiet @ " + resolved,
		"pnpm install @ " + resolved,
	}, strings.Split(strings.TrimSpace(string(calls)), "\n"))
	require.Contains(t, out.String(), "Installing dependencies with pnpm")
}

func TestInit_InstallFailure(t *testing.T) {
	t.Parallel()

	bin := fakeBinDir(t)
	writeFakeBin(t, bin, "npm", "exit 1\n")

	opts := initOptions(filepath.Join(t.TempDir(), "my-tools"), "functions")
	opts.Install = true
	opts.PackageManager = "npm"

	err := testRunner(bin, &bytes.Buffer{}).Init(t.Context(), opts)
	require.ErrorContains(t, err, "install dependencies")
}

func TestInit_MissingGitIsNotFatal(t *testing.T) {
	t.Parallel()

	dir := filepath.Join(t.TempDir(), "my-tools")
	opts := initOptions(dir, "functions")
	opts.Git = true

	var out bytes.Buffer
	require.NoError(t, testRunner(t.TempDir(), &out).Init(t.Context(), opts))
	require.Contains(t, out.String(), "Skipped git init")
	require.FileExists(t, filepath.Join(dir, "package.json"))
}

func TestDefaultDir(t *testing.T) {
	t.Parallel()

	require.Equal(t, "tools", DefaultDir("@acme/tools"))
	require.Equal(t, "tools", DefaultDir("tools"))
}

func TestValidateProjectName(t *testing.T) {
	t.Parallel()

	for _, name := range []string{"tools", "my-tools", "my_tools", "@acme/tools", "a1"} {
		require.NoError(t, ValidateProjectName(name), name)
	}
	for _, name := range []string{"", "Tools", "my tools", "@acme/", "a/b/c", "../x", "acme/tools"} {
		require.Error(t, ValidateProjectName(name), name)
	}
}
