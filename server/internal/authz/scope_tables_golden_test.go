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

var updateScopeTables = flag.Bool("update-scope-tables", false, "rewrite testdata/scope_tables.golden from the current scope tables")

// TestScopeTablesGolden pins the observable behaviour of every registered
// scope: visibility, one-step expansions, exclusion scope, implication closure,
// sub-scopes, grant surface, resource kind, accepted selector keys, and system
// role defaults. Moving a scope from the legacy tables into the scope registry
// must leave this file unchanged; a deliberate behaviour change updates it with
//
//	go test ./internal/authz -run TestScopeTablesGolden -update-scope-tables
func TestScopeTablesGolden(t *testing.T) {
	t.Parallel()

	got := dumpScopeTables()
	path := filepath.Join("testdata", "scope_tables.golden")
	if *updateScopeTables {
		require.NoError(t, os.MkdirAll("testdata", 0o750))
		require.NoError(t, os.WriteFile(path, []byte(got), 0o600))
	}
	want, err := os.ReadFile(path) //nolint:gosec // fixed test path
	require.NoError(t, err)
	require.Equal(t, string(want), got)
}

func dumpScopeTables() string {
	var b strings.Builder
	for _, scope := range RegisteredScopes() {
		visibility, _ := ScopeVisibilityFor(scope)
		exclusion, _ := ExclusionScopeFor(scope)
		surface, _ := GrantSurfaceForScope(scope)
		expansions := slices.Clone(scopeExpansions[scope])
		slices.Sort(expansions)

		fmt.Fprintf(&b, "%s\n", scope)
		fmt.Fprintf(&b, "  visibility: %s\n", visibility)
		fmt.Fprintf(&b, "  resource_kind: %s\n", ResourceKindForScope(scope))
		fmt.Fprintf(&b, "  surface: %s\n", cmp.Or(string(surface), "-"))
		fmt.Fprintf(&b, "  satisfied_by: %v\n", expansions)
		fmt.Fprintf(&b, "  exclusion: %s\n", cmp.Or(string(exclusion), "-"))
		fmt.Fprintf(&b, "  closure: %v\n", ScopeImplicationClosure(scope))
		fmt.Fprintf(&b, "  sub_scopes: %v\n", CalculateSubScopes(scope))
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
			SelectorKeyResourceKind: ResourceKindForScope(scope),
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
