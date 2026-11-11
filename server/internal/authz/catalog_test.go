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

		surface, ok := entry.Scope.GrantSurface()
		require.True(t, ok, "%s: grant surface", entry.Scope)
		require.NotEmpty(t, surface)

		visibility, ok := entry.Scope.Visibility()
		require.True(t, ok)
		if visibility == ScopeVisibilityInternal {
			require.True(t, strings.HasPrefix(entry.Label, "Exception: "), "%s: internal scopes are labelled as exceptions", entry.Scope)
			require.False(t, entry.Sensitive(), "%s: exception scopes withdraw access and are never sensitive", entry.Scope)
		} else {
			require.False(t, strings.HasPrefix(entry.Label, "Exception: "), "%s: visible scopes are not labelled as exceptions", entry.Scope)
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

// The API design builds its scope enums from these, so they must cover the
// whole catalogue and list each exclusion scope once.
func TestScopeNames_FollowTheCatalogue(t *testing.T) {
	t.Parallel()

	names := ScopeNames()
	require.Len(t, names, len(scopeCatalog))
	for i, entry := range scopeCatalog {
		require.Equal(t, string(entry.Scope), names[i])
	}

	exclusions := ExclusionScopeNames()
	sorted := slices.Clone(exclusions)
	slices.Sort(sorted)
	require.Len(t, slices.Compact(sorted), len(exclusions), "each exclusion scope is listed once")
	for _, entry := range scopeCatalog {
		exclusion, ok := entry.Scope.BlockedBy()
		if ok {
			require.Contains(t, exclusions, string(exclusion), "%s: exclusion scope missing", entry.Scope)
		}
	}
	require.Equal(t, exclusions[0], string(ScopeOrgBlockedRead), "exclusions follow the catalogue order")
	require.Contains(t, exclusions, string(ScopeRiskPolicyBypass))
}

func TestScopeCatalog_IsACopy(t *testing.T) {
	t.Parallel()

	first := ScopeCatalog()
	first[0].Label = "changed"
	second := ScopeCatalog()
	require.NotEqual(t, "changed", second[0].Label)
}

// The catalogue is grouped by category in ScopeCategories order, so a
// consumer that groups it gets one contiguous section per category.
func TestScopeCatalog_IsGroupedByCategory(t *testing.T) {
	t.Parallel()

	var seen []ScopeCategory
	for _, entry := range scopeCatalog {
		if len(seen) == 0 || seen[len(seen)-1] != entry.Category {
			require.NotContains(t, seen, entry.Category, "%s: category %q is split across the catalogue", entry.Scope, entry.Category)
			seen = append(seen, entry.Category)
		}
	}
	require.Equal(t, ScopeCategories, seen)
}

func TestScopeCategories_AreDistinct(t *testing.T) {
	t.Parallel()

	sorted := slices.Clone(ScopeCategories)
	slices.Sort(sorted)
	require.Len(t, slices.Compact(sorted), len(ScopeCategories))
}

// Every exception scope must withdraw something: be the blockedBy of an allow
// scope, or satisfy a narrower exception scope that is. An exception scope
// nobody reaches would store grants the engine never reads.
func TestScopeCatalog_ExceptionScopesAreReachable(t *testing.T) {
	t.Parallel()

	reached := make(map[Scope]bool)
	for _, entry := range scopeDefinitions {
		if entry.blockedBy != "" {
			reached[entry.blockedBy] = true
		}
	}
	for _, entry := range scopeDefinitions {
		if reached[entry.Scope] {
			for _, broader := range entry.satisfiedBy {
				reached[broader] = true
			}
		}
	}

	for _, entry := range scopeCatalog {
		if entry.visibility == scopeVisibilityInternal {
			require.True(t, reached[entry.Scope], "%s: exception scope withdraws nothing", entry.Scope)
		}
		if entry.blockedBy != "" {
			_, ok := ScopeEntryFor(entry.blockedBy)
			require.True(t, ok, "%s: blockedBy %s is not catalogued", entry.Scope, entry.blockedBy)
		}
		for _, satisfying := range entry.satisfiedBy {
			_, ok := ScopeEntryFor(satisfying)
			require.True(t, ok, "%s: satisfiedBy %s is not catalogued", entry.Scope, satisfying)
		}
	}
}

// Resource kinds are data on each entry. Today they agree with the scope's
// prefix everywhere except the auth-client scopes, which are checked against
// the client rather than a server. A new disagreement must be deliberate.
func TestScopeCatalog_ResourceTypeFollowsPrefixExceptAuthClients(t *testing.T) {
	t.Parallel()

	for _, entry := range scopeDefinitions {
		if entry.Scope == ScopeMCPAuth || entry.Scope == ScopeMCPBlockedAuth {
			require.Equal(t, ResourceKindMCPAuthClient, entry.ResourceType, entry.Scope)
			continue
		}
		require.Equal(t, resourceKindForUnregisteredScope(entry.Scope), entry.ResourceType, entry.Scope)
	}
}

// Root is registered so stored root grants evaluate, but it is never listed.
func TestScopeCatalog_RootIsRegisteredButNotCatalogued(t *testing.T) {
	t.Parallel()

	require.Contains(t, RegisteredScopes(), ScopeRoot)
	_, ok := ScopeEntryFor(ScopeRoot)
	require.False(t, ok)
	visibility, ok := ScopeRoot.Visibility()
	require.True(t, ok)
	require.Equal(t, ScopeVisibilityInternal, visibility)
	require.Equal(t, ResourceKindWildcard, ScopeRoot.ResourceKind())
}
