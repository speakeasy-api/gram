package middleware

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// rpcPathPattern matches the route literals goa emits in the generated
// gen/http/<service>/server/paths.go files, one per HTTP endpoint.
var rpcPathPattern = regexp.MustCompile(`"(/rpc/[^"]+)"`)

// TestDemoGuardClassifiesEveryRPCVerb walks every generated /rpc route and
// requires its method-name verb to appear in demoReadOnlyVerbs or
// demoMutatingVerbs.
//
// Demo sessions hold every user-visible scope (authz.DemoScopeGrants), so this
// classification — not scope enforcement — is what keeps the shared demo
// organization read-only. An unclassified verb is blocked in the demo org, so
// shipping one is safe but silently costs the demo a page if the endpoint was
// a read. This test is where that gets noticed.
func TestDemoGuardClassifiesEveryRPCVerb(t *testing.T) {
	t.Parallel()

	routes := generatedRPCRoutes(t)
	require.Greater(t, len(routes), 100, "expected the generated route scan to find the full RPC surface")

	unclassified := map[string][]string{}
	for _, route := range routes {
		// Mirrors isMutatingRPC: /rpc/auth.* is exempt from the guard, so its
		// verbs need no classification.
		if strings.HasPrefix(route, authRPCPrefix) {
			continue
		}

		verb := leadingLowerWord(route[strings.LastIndex(route, ".")+1:])
		if classifyDemoVerb(verb) == demoVerbUnclassified {
			unclassified[verb] = append(unclassified[verb], route)
		}
	}

	verbs := make([]string, 0, len(unclassified))
	for verb := range unclassified {
		verbs = append(verbs, verb)
	}
	sort.Strings(verbs)

	for _, verb := range verbs {
		t.Errorf(
			"RPC verb %q is in neither demoReadOnlyVerbs nor demoMutatingVerbs, so the demo org rejects %s. "+
				"Add it to demoReadOnlyVerbs if these endpoints only read, otherwise to demoMutatingVerbs.",
			verb, strings.Join(unclassified[verb], ", "),
		)
	}
}

// TestDemoGuardVerbSetsAreDisjoint guards against a verb being classified both
// ways, where the read set would silently win.
func TestDemoGuardVerbSetsAreDisjoint(t *testing.T) {
	t.Parallel()

	for verb := range demoReadOnlyVerbs {
		_, mutating := demoMutatingVerbs[verb]
		require.False(t, mutating, "verb %q is classified as both a read and a mutation", verb)
	}
}

// generatedRPCRoutes returns every /rpc route literal goa generated under
// server/gen/http.
func generatedRPCRoutes(t *testing.T) []string {
	t.Helper()

	root := filepath.Join("..", "..", "gen", "http")
	seen := map[string]struct{}{}

	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() || entry.Name() != "paths.go" {
			return nil
		}

		contents, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		for _, match := range rpcPathPattern.FindAllStringSubmatch(string(contents), -1) {
			seen[match[1]] = struct{}{}
		}
		return nil
	})
	require.NoError(t, err)

	routes := make([]string, 0, len(seen))
	for route := range seen {
		routes = append(routes, route)
	}
	sort.Strings(routes)
	return routes
}
