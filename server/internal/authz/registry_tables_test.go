package authz

import (
	"encoding/json"
	"maps"
	"slices"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/testenv"
)

// TestModel_DerivedTablesGolden snapshots every table derived from Model. A
// registry edit shows up in review as a diff of what the evaluator reads:
// which scopes satisfy which, which exclusions apply, who may write them.
// Regenerate with `go test ./internal/authz -run DerivedTablesGolden -args -update`
// and read the diff before committing it.
//
// The snapshot was first written from the hand-kept tables these derivations
// replaced, so it also records that the switch to the registry changed
// nothing.
func TestModel_DerivedTablesGolden(t *testing.T) {
	t.Parallel()

	type agentEntry struct {
		SafeSince int  `json:"safe_since"`
		Retired   bool `json:"retired"`
	}
	type snapshot struct {
		Visibility         map[Scope]string          `json:"visibility"`
		Expansions         map[Scope][]Scope         `json:"expansions"`
		Exclusions         map[Scope]Scope           `json:"exclusions"`
		GrantSource        map[Scope]GrantSource     `json:"grant_source"`
		ResourceKinds      map[Scope]string          `json:"resource_kinds"`
		SelectorKeys       map[string][]string       `json:"selector_keys"`
		ScopeSelectorKeys  map[Scope][]string        `json:"scope_selector_keys"`
		RequiredDimensions map[string][]string       `json:"required_dimensions"`
		DimensionValues    map[string][]string       `json:"dimension_values"`
		SystemRoles        map[string][]Scope        `json:"system_roles"`
		Agent              map[Scope]agentEntry      `json:"agent"`
		Categories         map[ScopeCategory][]Scope `json:"categories"`
		Sensitive          map[Scope]Sensitivity     `json:"sensitive"`
		Tasks              map[string][]Scope        `json:"tasks"`
	}

	keys := func(m map[string]bool) []string { return slices.Sorted(maps.Keys(m)) }

	snap := snapshot{
		Visibility:         make(map[Scope]string),
		Expansions:         make(map[Scope][]Scope),
		Exclusions:         make(map[Scope]Scope),
		GrantSource:        scopeGrantSource,
		ResourceKinds:      make(map[Scope]string),
		SelectorKeys:       make(map[string][]string),
		ScopeSelectorKeys:  make(map[Scope][]string),
		RequiredDimensions: requiredCheckDimensions,
		DimensionValues: map[string][]string{
			"mcp." + SelectorKeyDisposition:            keys(validDispositions),
			"mcp_auth_client." + SelectorKeyClientType: keys(validAuthClientTypes),
		},
		SystemRoles: make(map[string][]Scope),
		Agent:       make(map[Scope]agentEntry),
		Categories:  make(map[ScopeCategory][]Scope),
		Sensitive:   make(map[Scope]Sensitivity),
		Tasks:       make(map[string][]Scope),
	}
	for _, scope := range RegisteredScopes() {
		visibility, ok := ScopeVisibilityFor(scope)
		require.True(t, ok)
		snap.Visibility[scope] = visibility
		snap.Expansions[scope] = sortedOrEmpty(scopeExpansions[scope])
		if exclusion, ok := ExclusionScopeFor(scope); ok {
			snap.Exclusions[scope] = exclusion
		}
		snap.ResourceKinds[scope] = ResourceKindForScope(scope)
	}
	for kind, allowed := range allowedSelectorKeys {
		snap.SelectorKeys[kind] = keys(allowed)
	}
	for scope, allowed := range scopeSelectorKeys {
		snap.ScopeSelectorKeys[scope] = keys(allowed)
	}
	for role, grants := range SystemRoleGrants {
		for _, grant := range grants {
			snap.SystemRoles[role] = append(snap.SystemRoles[role], Scope(grant.Scope))
		}
	}
	for scope, eligibility := range AgentEligibilityByScope() {
		snap.Agent[scope] = agentEntry(eligibility)
	}
	for _, entry := range ScopeCatalog() {
		snap.Categories[entry.Category] = append(snap.Categories[entry.Category], entry.Scope)
		if entry.Sensitive() {
			snap.Sensitive[entry.Scope] = entry.Sensitivity
		}
	}
	for _, task := range Tasks() {
		snap.Tasks[task.Slug] = task.Scopes
	}

	data, err := json.Marshal(snap)
	require.NoError(t, err)
	testenv.RequireYAMLGolden(t, "testdata/registry_tables.golden.yaml", data)
}

func sortedOrEmpty(in []Scope) []Scope {
	out := slices.Clone(in)
	if out == nil {
		out = []Scope{}
	}
	slices.Sort(out)
	return out
}
