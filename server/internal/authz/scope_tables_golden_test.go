package authz

import (
	"cmp"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

var updateScopeTables = flag.Bool("update-scope-tables", false, "rewrite testdata/engine_compatibility/scope_tables.golden from the current scope tables")

// TestScopeTablesGolden pins the observable behaviour of every registered
// scope: visibility, which other scopes' grants satisfy it, exclusion scope, implication closure,
// sub-scopes, grant surface, resource kind, accepted selector keys, and system
// role defaults. It records behaviour rather than table layout, so moving scope
// definitions between tables must reproduce it exactly. A deliberate behaviour
// change updates it in the same PR with
//
//	go test ./internal/authz -run TestScopeTablesGolden -update-scope-tables
func TestScopeTablesGolden(t *testing.T) {
	t.Parallel()

	got := dumpScopeTables()
	path := filepath.Join(engineCompatibilityDir, "scope_tables.golden")
	if *updateScopeTables {
		require.NoError(t, os.WriteFile(path, []byte(got), 0o600))
	}
	want, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, string(want), got)
}

func dumpScopeTables() string {
	var b strings.Builder
	for _, scope := range RegisteredScopes() {
		visibility, _ := scope.Visibility()
		exclusion, _ := scope.BlockedBy()
		surface, _ := scope.GrantSurface()
		expansions := satisfyingScopes(scope)

		fmt.Fprintf(&b, "%s\n", scope)
		fmt.Fprintf(&b, "  visibility: %s\n", visibility)
		fmt.Fprintf(&b, "  resource_kind: %s\n", scope.ResourceKind())
		fmt.Fprintf(&b, "  surface: %s\n", cmp.Or(string(surface), "-"))
		fmt.Fprintf(&b, "  satisfied_by: %v\n", expansions)
		fmt.Fprintf(&b, "  exclusion: %s\n", cmp.Or(string(exclusion), "-"))
		fmt.Fprintf(&b, "  closure: %v\n", scope.ImpliedScopes())
		fmt.Fprintf(&b, "  sub_scopes: %v\n", scope.SubScopes())
		fmt.Fprintf(&b, "  selector_keys: %v\n", acceptedSelectorKeys(scope))
		fmt.Fprintf(&b, "  roles: %v\n", systemRolesHolding(scope))
	}
	return b.String()
}

// acceptedSelectorKeys probes ValidateSelector with each known extra key so
// the dump records behaviour, not whichever table currently stores it.
func acceptedSelectorKeys(scope Scope) []string {
	probes := map[string]string{
		SelectorKeyTool:            "tool",
		SelectorKeyDisposition:     DispositionReadOnly,
		SelectorKeyProjectID:       "project",
		SelectorKeyServerURL:       "https://example.test",
		SelectorKeyServerIdentity:  "identity",
		SelectorKeyClientType:      AuthClientTypeRemoteSession,
		SelectorKeyConnectionOwner: "owner",
		SelectorKeyUpstreamClient:  "upstream",
	}
	var accepted []string
	for key, value := range probes {
		sel := Selector{
			SelectorKeyResourceKind: scope.ResourceKind(),
			SelectorKeyResourceID:   WildcardResource,
			key:                     value,
		}
		if ValidateSelector(scope, sel) == nil {
			accepted = append(accepted, key)
		}
	}
	slices.Sort(accepted)
	return accepted
}

func systemRolesHolding(scope Scope) []string {
	var roles []string
	for role, grants := range SystemRoleGrants {
		if slices.ContainsFunc(grants, func(g *RoleGrant) bool { return g.Scope == string(scope) }) {
			roles = append(roles, role)
		}
	}
	slices.Sort(roles)
	return roles
}

// satisfyingScopes probes the evaluator for every other registered scope whose
// wildcard grant satisfies a check on scope, so the dump records which grants
// satisfy a check rather than how the expansion table stores it. Root is left
// out: it satisfies everything except auth-client checks, and the corpus
// covers it separately.
func satisfyingScopes(scope Scope) []Scope {
	check := Check{Scope: scope, ResourceKind: scope.ResourceKind(), ResourceID: "probe", Dimensions: nil, selectorMatch: selectorMatchNormal, representedUser: ""}
	var satisfying []Scope
	for _, candidate := range RegisteredScopes() {
		if candidate == scope || candidate == ScopeRoot {
			continue
		}
		grant := NewGrantWithSelector(candidate, Selector{SelectorKeyResourceID: WildcardResource})
		if GrantsSatisfy([]Grant{grant}, check) {
			satisfying = append(satisfying, candidate)
		}
	}
	slices.Sort(satisfying)
	return satisfying
}
