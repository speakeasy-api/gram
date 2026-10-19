package main

import (
	"testing"

	"github.com/stretchr/testify/require"
)

const mod = "example.com/mod"

func mainModule() *struct{ Main bool } {
	return &struct{ Main bool }{Main: true}
}

// testGraph builds a graph for a module at /repo with three packages: api,
// which embeds spec.yaml and has tests that link store; store, which has
// testdata; and util, which nothing links.
func testGraph(t *testing.T) graph {
	t.Helper()

	g, err := buildGraph("/repo", []listed{
		{ImportPath: mod + "/server/api", Dir: "/repo/server/api", EmbedFiles: []string{"spec.yaml"}, Module: mainModule()},
		{ImportPath: mod + "/server/store", Dir: "/repo/server/store", TestEmbedFiles: []string{"testdata/golden.json"}, Module: mainModule()},
		{ImportPath: mod + "/server/util", Dir: "/repo/server/util", Module: mainModule()},
		// Test variants and test mains as 'go list -deps -test' prints them.
		{ImportPath: mod + "/server/api [" + mod + "/server/api.test]", Dir: "/repo/server/api", ForTest: mod + "/server/api", Module: mainModule()},
		{ImportPath: mod + "/server/api.test", Dir: "/cache/b001", Module: mainModule(), Deps: []string{
			"fmt",
			mod + "/server/api [" + mod + "/server/api.test]",
			mod + "/server/store",
		}},
		{ImportPath: mod + "/server/store.test", Dir: "/cache/b002", Module: mainModule(), Deps: []string{
			mod + "/server/store [" + mod + "/server/store.test]",
		}},
		// Dependencies from other modules are never local packages.
		{ImportPath: "example.com/dep", Dir: "/gopath/dep"},
	})
	require.NoError(t, err)

	return g
}

func TestBuildGraph(t *testing.T) {
	t.Parallel()

	g := testGraph(t)

	require.Equal(t, map[string]string{
		"server/api":   mod + "/server/api",
		"server/store": mod + "/server/store",
		"server/util":  mod + "/server/util",
	}, g.dirs)
	require.Equal(t, map[string][]string{
		"server/api/spec.yaml":              {mod + "/server/api"},
		"server/store/testdata/golden.json": {mod + "/server/store"},
	}, g.embeds)
	require.Equal(t, []string{"fmt", mod + "/server/api", mod + "/server/store"}, g.links[mod+"/server/api"])
}

func TestClassify(t *testing.T) {
	t.Parallel()

	g := testGraph(t)

	cases := []struct {
		name     string
		files    []string
		changed  []string
		wantFull bool
	}{
		{name: "go file", files: []string{"server/store/store.go"}, changed: []string{"store"}},
		{name: "test file", files: []string{"server/api/api_test.go"}, changed: []string{"api"}},
		{name: "go file outside the graph", files: []string{"cli/main.go"}},
		{name: "embedded file", files: []string{"server/api/spec.yaml"}, changed: []string{"api"}},
		{name: "testdata", files: []string{"server/store/testdata/cases/a.txt"}, changed: []string{"store"}},
		{name: "go file under testdata", files: []string{"server/store/testdata/src/x.go"}, changed: []string{"store"}},
		{name: "documentation", files: []string{"server/api/README.md"}},
		{name: "sqlc input", files: []string{"server/store/queries.sql"}},
		{name: "proto source", files: []string{"infra/proto/gram/v1/event.proto"}},
		{name: "rust runner", files: []string{"agents/runner/src/main.rs"}},
		{name: "several packages", files: []string{"server/api/api.go", "server/store/store.go"}, changed: []string{"api", "store"}},
		{name: "schema read at runtime", files: []string{"server/store/store.go", "server/database/schema.sql"}, wantFull: true},
		{name: "go.mod", files: []string{"go.mod"}, wantFull: true},
		{name: "workflow", files: []string{".github/workflows/pr.yaml"}, wantFull: true},
		{name: "generated spec", files: []string{"server/gen/http/openapi3.yaml"}, wantFull: true},
		{name: "unclean path", files: []string{"./server/store/../store/store.go"}, changed: []string{"store"}},
	}

	for _, c := range cases {
		sel := classify(g, c.files)

		if c.wantFull {
			require.NotEmpty(t, sel.full, c.name)
			continue
		}

		require.Empty(t, sel.full, c.name)

		want := map[string]bool{}
		for _, name := range c.changed {
			want[mod+"/server/"+name] = true
		}
		require.Equal(t, want, sel.changed, c.name)
	}
}

func TestAffected(t *testing.T) {
	t.Parallel()

	g := testGraph(t)

	// api's tests link store, so a store change reaches both.
	changed := map[string]bool{mod + "/server/store": true}
	require.True(t, g.affected(mod+"/server/api", changed))
	require.True(t, g.affected(mod+"/server/store", changed))

	// Nothing else links api.
	changed = map[string]bool{mod + "/server/api": true}
	require.True(t, g.affected(mod+"/server/api", changed))
	require.False(t, g.affected(mod+"/server/store", changed))

	// A package with no test main is affected only by its own change.
	require.False(t, g.affected(mod+"/server/util", changed))
	require.True(t, g.affected(mod+"/server/util", map[string]bool{mod + "/server/util": true}))
}
