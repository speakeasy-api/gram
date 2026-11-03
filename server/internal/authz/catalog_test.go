package authz

import (
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// Every scope the engine knows must be described, and nothing may be
// described that the engine does not know. ScopeRoot is the one exception:
// it is the engine's bypass, not a grant a person chooses.
func TestScopeCatalog_CoversEveryRegisteredScope(t *testing.T) {
	t.Parallel()

	catalogued := make(map[Scope]bool, len(scopeCatalog))
	for _, entry := range scopeCatalog {
		require.False(t, catalogued[entry.Scope], "scope %q is catalogued twice", entry.Scope)
		catalogued[entry.Scope] = true
	}

	for _, scope := range RegisteredScopes() {
		if scope == ScopeRoot {
			require.False(t, catalogued[scope], "root must not be catalogued")
			continue
		}
		require.True(t, catalogued[scope], "registered scope %q has no catalogue entry", scope)
	}
	require.Len(t, scopeCatalog, len(RegisteredScopes())-1)
}

func TestScopeCatalog_EntriesAreComplete(t *testing.T) {
	t.Parallel()

	for _, entry := range scopeCatalog {
		require.NotEmpty(t, entry.Label, "%s: label", entry.Scope)
		require.NotEmpty(t, entry.Description, "%s: description", entry.Scope)
		require.True(t, strings.HasSuffix(entry.Description, "."), "%s: description must be a sentence", entry.Scope)
		require.Contains(t, ScopeCategories, entry.Category, "%s: category", entry.Scope)
		require.NotEmpty(t, entry.ResourceType, "%s: resource type", entry.Scope)

		surface, ok := GrantSurfaceForScope(entry.Scope)
		require.True(t, ok, "%s: grant surface", entry.Scope)
		require.NotEmpty(t, surface)

		visibility, ok := ScopeVisibilityFor(entry.Scope)
		require.True(t, ok)
		if visibility == ScopeVisibilityInternal {
			require.True(t, strings.HasPrefix(entry.Label, "Blocked: "), "%s: internal scopes are labelled as blocks", entry.Scope)
			require.False(t, entry.Sensitive(), "%s: exception scopes withdraw access and are never sensitive", entry.Scope)
		} else {
			require.False(t, strings.HasPrefix(entry.Label, "Blocked: "), "%s: visible scopes are not labelled as blocks", entry.Scope)
		}
	}
}

// The sensitive set is a product decision (AIS-825): other members' private
// activity, and administration that can remove people or rewrite every role.
// Secrets and credentials are deliberately not in it.
func TestScopeCatalog_SensitiveSet(t *testing.T) {
	t.Parallel()

	require.Equal(t, []Scope{ScopeOrgAdmin, ScopeChatRead, ScopeChatWrite, ScopeLogRead}, SensitiveScopes())

	reasons := []struct {
		scope Scope
		want  Sensitivity
	}{
		{scope: ScopeOrgAdmin, want: SensitivityDestructiveAdmin},
		{scope: ScopeChatRead, want: SensitivityConversations},
		{scope: ScopeChatWrite, want: SensitivityConversations},
		{scope: ScopeLogRead, want: SensitivityToolLogs},
	}
	for _, tc := range reasons {
		entry, ok := ScopeEntryFor(tc.scope)
		require.True(t, ok)
		require.Equal(t, tc.want, entry.Sensitivity, tc.scope)
	}
}

// A scope that includes a sensitive scope is itself sensitive, or the warning
// is lost when the broader scope is granted instead.
func TestScopeCatalog_SensitivityFollowsImplication(t *testing.T) {
	t.Parallel()

	for _, entry := range scopeCatalog {
		for _, sub := range entry.SubScopes() {
			subEntry, ok := ScopeEntryFor(Scope(sub))
			require.True(t, ok, "%s includes unknown %s", entry.Scope, sub)
			if subEntry.Sensitive() {
				require.True(t, entry.Sensitive(), "%s includes sensitive %s but is not sensitive itself", entry.Scope, sub)
			}
		}
	}
}

func TestScopeCatalog_SubScopesMatchExpansions(t *testing.T) {
	t.Parallel()

	admin, ok := ScopeEntryFor(ScopeOrgAdmin)
	require.True(t, ok)
	require.Equal(t, []string{"org:device_agent_sync", "org:hooks_ingest", "org:read"}, admin.SubScopes())

	write, ok := ScopeEntryFor(ScopeMCPWrite)
	require.True(t, ok)
	require.Equal(t, []string{"mcp:connect", "mcp:read"}, write.SubScopes())

	chatWrite, ok := ScopeEntryFor(ScopeChatWrite)
	require.True(t, ok)
	require.Equal(t, []string{"chat:read"}, chatWrite.SubScopes())

	logRead, ok := ScopeEntryFor(ScopeLogRead)
	require.True(t, ok)
	require.Empty(t, logRead.SubScopes())
}

func TestScopeCatalog_IsACopy(t *testing.T) {
	t.Parallel()

	first := ScopeCatalog()
	first[0].Label = "changed"
	second := ScopeCatalog()
	require.NotEqual(t, "changed", second[0].Label)
}

func TestScopeCategories_AreDistinct(t *testing.T) {
	t.Parallel()

	sorted := slices.Clone(ScopeCategories)
	slices.Sort(sorted)
	require.Len(t, slices.Compact(sorted), len(ScopeCategories))
}
