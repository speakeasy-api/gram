package authzgen

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// The tests in this file hold the line between the authorization model and
// the evaluator. The registry and this generator are build-time code; package
// authz is what runs. Generated Go is the only thing that passes between
// them, so neither side imports the other.

const (
	authzPackage      = "github.com/speakeasy-api/gram/server/internal/authz"
	registryPackage   = authzPackage + "/internal/registry"
	authzgenPackage   = authzPackage + "/authzgen"
	authzcasesPackage = authzPackage + "/authzcases"
	authzgenCommand   = "github.com/speakeasy-api/gram/server/cmd/authzgen"
)

// serverDir is the server module root, which generated paths are relative to.
func serverDir(t *testing.T) string {
	t.Helper()
	dir, err := filepath.Abs(filepath.Join("..", "..", ".."))
	require.NoError(t, err)
	return dir
}

// goTool runs the go command in the server directory and returns its output.
func goTool(t *testing.T, args ...string) string {
	t.Helper()
	cmd := exec.CommandContext(t.Context(), "go", args...)
	cmd.Dir = serverDir(t)
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, "go %s\n%s", strings.Join(args, " "), out)
	return string(out)
}

// dependencies lists every package pkg needs to build, tests left out.
func dependencies(t *testing.T, pkg string) []string {
	t.Helper()
	return strings.Fields(goTool(t, "list", "-deps", pkg))
}

// The evaluator never depends on the model or on the code that generates from
// it: what it runs is the generated tables, which are reviewed like any other
// code.
func TestAuthzDoesNotDependOnTheRegistry(t *testing.T) {
	t.Parallel()

	deps := dependencies(t, authzPackage)
	for _, buildTime := range []string{registryPackage, authzgenPackage, authzcasesPackage} {
		require.NotContains(t, deps, buildTime, "package authz must not depend on %s", buildTime)
	}
}

// The model and the generator never depend on the evaluator, so the generated
// files can be rewritten whatever state they are in.
func TestRegistryAndGeneratorDoNotDependOnAuthz(t *testing.T) {
	t.Parallel()

	for _, pkg := range []string{registryPackage, authzgenPackage, authzgenCommand} {
		require.NotContains(t, dependencies(t, pkg), authzPackage, "%s must not depend on package authz", pkg)
	}
}

// With every generated file hidden, the generator still builds and writes
// each of them back as it is checked in.
func TestGeneratorRebuildsMissingFiles(t *testing.T) {
	t.Parallel()

	files, err := Files()
	require.NoError(t, err)

	server := serverDir(t)
	hidden := map[string]string{}
	var goFiles []string
	for _, file := range files {
		if strings.HasSuffix(file.Path, ".go") {
			// An empty replacement makes the go command treat the file as absent.
			hidden[filepath.Join(server, file.Path)] = ""
			goFiles = append(goFiles, file.Path)
		}
	}
	require.NotEmpty(t, goFiles)
	// Every *_gen.go file in package authz is one the generator writes, so
	// hiding its output hides them all.
	onDisk, err := filepath.Glob(filepath.Join(server, "internal", "authz", "*_gen.go"))
	require.NoError(t, err)
	for _, path := range onDisk {
		rel, err := filepath.Rel(server, path)
		require.NoError(t, err)
		require.True(t, slices.Contains(goFiles, filepath.ToSlash(rel)), "%s is not written by authzgen", rel)
	}

	work := t.TempDir()
	overlayPath := filepath.Join(work, "overlay.json")
	encoded, err := json.Marshal(map[string]map[string]string{"Replace": hidden})
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(overlayPath, encoded, 0o600))

	binary := filepath.Join(work, "authzgen")
	goTool(t, "build", "-overlay", overlayPath, "-o", binary, authzgenCommand)

	// The command writes paths relative to the server directory; give it an
	// empty one.
	out := filepath.Join(work, "repo", "server")
	require.NoError(t, os.MkdirAll(out, 0o750))
	run := exec.CommandContext(t.Context(), binary)
	run.Dir = out
	output, err := run.CombinedOutput()
	require.NoError(t, err, "%s", output)

	for _, file := range files {
		got, err := os.ReadFile(filepath.Join(out, file.Path))
		require.NoError(t, err, file.Path)
		want, err := os.ReadFile(filepath.Join(server, file.Path))
		require.NoError(t, err, file.Path)
		require.Equal(t, string(want), string(got), "%s", file.Path)
	}
}
