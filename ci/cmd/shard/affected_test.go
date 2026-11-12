package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/stretchr/testify/require"
)

const testModule = "example.com/mod"

func importPaths(pkgs []pkg) []string {
	paths := make([]string, 0, len(pkgs))
	for _, p := range pkgs {
		paths = append(paths, p.ImportPath)
	}

	return paths
}

// graphPackages is a small import graph: api imports store, store imports
// model, web imports api, and tools only imports model from its tests.
func graphPackages() []pkg {
	return []pkg{
		{ImportPath: testModule + "/model"},
		{ImportPath: testModule + "/store", Imports: []string{testModule + "/model"}},
		{ImportPath: testModule + "/api", Imports: []string{testModule + "/store"}},
		{ImportPath: testModule + "/web", Imports: []string{testModule + "/api"}},
		{ImportPath: testModule + "/tools", XTestImports: []string{testModule + "/model"}},
		{ImportPath: testModule + "/lone", TestImports: []string{"example.com/other/dep"}},
	}
}

func hasGoIn(dirs ...string) func(string) bool {
	return func(dir string) bool {
		return slices.Contains(dirs, dir)
	}
}

func TestAffected_SelectsTouchedPackagesAndDirectImporters(t *testing.T) {
	t.Parallel()

	got := affected(graphPackages(), testModule, []string{"store/store.go"}, nil, nil, nil, hasGoIn())

	require.False(t, got.everything)
	// api imports store directly. web only reaches it through api, and model
	// is a dependency of store rather than an importer.
	require.Equal(t, []string{testModule + "/store", testModule + "/api"}, importPaths(got.pkgs))
}

func TestAffected_CountsTestOnlyImports(t *testing.T) {
	t.Parallel()

	got := affected(graphPackages(), testModule, []string{"model/model_test.go"}, nil, nil, nil, hasGoIn())

	require.Equal(t, []string{testModule + "/model", testModule + "/store", testModule + "/tools"}, importPaths(got.pkgs))
}

func TestAffected_DeletedPackageStillSelectsImporters(t *testing.T) {
	t.Parallel()

	// "gone" has no test package any more, but api's sources named it.
	pkgs := append(graphPackages(), pkg{ImportPath: testModule + "/user", Imports: []string{testModule + "/gone"}})

	got := affected(pkgs, testModule, []string{"gone/gone.go"}, nil, nil, nil, hasGoIn())

	require.Equal(t, []string{testModule + "/user"}, importPaths(got.pkgs))
}

func TestAffected_NonGoFiles(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		file string
		want []string
	}{
		{name: "file next to Go sources", file: "store/queries.sql", want: []string{testModule + "/store", testModule + "/api"}},
		{name: "testdata", file: "web/testdata/golden/page.html", want: []string{testModule + "/web"}},
		{name: "embedded file in a subdirectory", file: "store/queries/list.sql", want: []string{testModule + "/store", testModule + "/api"}},
	}

	for _, c := range cases {
		got := affected(graphPackages(), testModule, []string{c.file}, nil, nil, nil, hasGoIn("store", "web"))
		require.False(t, got.everything, c.name)
		require.Equal(t, c.want, importPaths(got.pkgs), c.name)
	}
}

func TestAffected_FallsBackToEverything(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name       string
		file       string
		wantReason string
	}{
		{name: "full-on file", file: "go.mod", wantReason: "go.mod affects every package"},
		{name: "full-on tree", file: "db/migrations/0001.sql", wantReason: "affects every package"},
		{name: "file outside any package", file: "docs/guide/intro.md", wantReason: "does not belong to a Go package"},
	}

	for _, c := range cases {
		got := affected(graphPackages(), testModule, []string{"store/store.go", c.file}, nil, []string{"go.mod", "db/migrations/"}, nil, hasGoIn("store"))
		require.True(t, got.everything, c.name)
		require.ErrorContains(t, errString(got.reason), c.wantReason, c.name)
		require.Len(t, got.pkgs, len(graphPackages()), c.name)
	}
}

type errString string

func (e errString) Error() string { return string(e) }

func TestAffected_NothingTouched(t *testing.T) {
	t.Parallel()

	got := affected(graphPackages(), testModule, nil, nil, nil, nil, hasGoIn())

	require.False(t, got.everything)
	require.Empty(t, got.pkgs)
}

func TestAffected_IgnoresFilesOutsideScope(t *testing.T) {
	t.Parallel()

	scope := []string{"go.mod", "store/", "api/", "db/"}
	fullOn := []string{"go.mod", "db/migrations/"}

	// A changeset and another product's sources say nothing about this suite.
	// Without a scope they would count as files outside any package and select
	// everything.
	changed := []string{".changeset/quiet-owls-sing.md", "client/app/src/page.tsx", "store/store.go"}

	got := affected(graphPackages(), testModule, changed, scope, fullOn, nil, hasGoIn("store"))
	require.False(t, got.everything)
	require.Equal(t, []string{testModule + "/store", testModule + "/api"}, importPaths(got.pkgs))

	unscoped := affected(graphPackages(), testModule, changed, nil, fullOn, nil, hasGoIn("store"))
	require.True(t, unscoped.everything)

	// Only out-of-scope files: nothing to test.
	got = affected(graphPackages(), testModule, changed[:2], scope, fullOn, nil, hasGoIn("store"))
	require.False(t, got.everything)
	require.Empty(t, got.pkgs)

	// A full-on path selects everything whether or not the scope names it.
	got = affected(graphPackages(), testModule, []string{"db/migrations/0002.sql"}, scope, fullOn, nil, hasGoIn())
	require.True(t, got.everything)
	got = affected(graphPackages(), testModule, []string{"tools.toml"}, scope, []string{"tools.toml"}, nil, hasGoIn())
	require.True(t, got.everything)
}

func TestAffected_Readers(t *testing.T) {
	t.Parallel()

	readers, err := parseReaders("spec/api.yaml=web,spec/api.yaml=lone")
	require.NoError(t, err)

	got := affected(graphPackages(), testModule, []string{"spec/api.yaml"}, nil, nil, readers, hasGoIn("spec"))

	require.False(t, got.everything)
	require.Equal(t, []string{testModule + "/web", testModule + "/lone"}, importPaths(got.pkgs))

	_, err = parseReaders("spec/api.yaml")
	require.ErrorContains(t, err, "not in <file>=<package directory> form")
}

func TestPackageDir(t *testing.T) {
	t.Parallel()

	dir, ok := packageDir("main.go", hasGoIn())
	require.True(t, ok)
	require.Equal(t, ".", dir)

	dir, ok = packageDir("testdata/in.json", hasGoIn("."))
	require.True(t, ok)
	require.Equal(t, ".", dir)

	dir, ok = packageDir("a/b/c/d.sql", hasGoIn("a", "."))
	require.True(t, ok)
	require.Equal(t, "a", dir)

	_, ok = packageDir("README.md", hasGoIn())
	require.False(t, ok)
}

func TestShardCount(t *testing.T) {
	t.Parallel()

	require.Equal(t, 0, shardCount(0, 1000, 5))
	require.Equal(t, 1, shardCount(1, 1000, 5))
	require.Equal(t, 1, shardCount(200, 1000, 5))
	require.Equal(t, 2, shardCount(201, 1000, 5))
	require.Equal(t, 5, shardCount(1000, 1000, 5))
	require.Equal(t, 5, shardCount(5000, 1000, 5))
}

func TestReadChanged(t *testing.T) {
	t.Parallel()

	file := filepath.Join(t.TempDir(), "changed.txt")
	require.NoError(t, os.WriteFile(file, []byte("a/b.go\n\n  c/d.sql  \n"), 0o600))

	changed, err := readChanged(file)
	require.NoError(t, err)
	require.Equal(t, []string{"a/b.go", "c/d.sql"}, changed)

	_, err = readChanged(filepath.Join(t.TempDir(), "missing.txt"))
	require.ErrorContains(t, err, "open changed files list")
}

// The plan subcommand runs against this module, whose packages and timings
// are real, so only the shape of the answer is asserted.
func TestRunPlan(t *testing.T) {
	t.Parallel()

	var stdout, stderr bytes.Buffer
	require.NoError(t, run([]string{"plan", "-max", "3", "."}, &stdout, &stderr))

	var full plan
	require.NoError(t, json.Unmarshal(stdout.Bytes(), &full))
	require.Equal(t, []int{1, 2, 3}, full.Shards)
	require.Equal(t, full.Total, full.Selected)
	require.NotEmpty(t, full.Reason)

	changed := filepath.Join(t.TempDir(), "changed.txt")
	require.NoError(t, os.WriteFile(changed, []byte("ci/cmd/shard/main.go\n"), 0o600))

	stdout.Reset()
	require.NoError(t, run([]string{"plan", "-max", "3", "-changed", changed, "."}, &stdout, &stderr))

	var narrowed plan
	require.NoError(t, json.Unmarshal(stdout.Bytes(), &narrowed))
	require.Equal(t, 1, narrowed.Selected)
	require.NotEmpty(t, narrowed.Shards)
	require.Empty(t, narrowed.Reason)

	stdout.Reset()
	require.NoError(t, os.WriteFile(changed, []byte("go.mod\n"), 0o600))
	require.NoError(t, run([]string{"plan", "-max", "3", "-changed", changed, "-full-on", "go.mod", "."}, &stdout, &stderr))

	var forced plan
	require.NoError(t, json.Unmarshal(stdout.Bytes(), &forced))
	require.Equal(t, []int{1, 2, 3}, forced.Shards)
	require.Contains(t, forced.Reason, "go.mod")

	require.ErrorContains(t, run([]string{"plan", "."}, &stdout, &stderr), "-max 0 must be at least 1")
}
