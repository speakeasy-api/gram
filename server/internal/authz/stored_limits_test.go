package authz

import (
	"maps"
	"testing"

	"github.com/stretchr/testify/require"
)

func limitedSelector(scope Scope, key, value string) Selector {
	selector := NewSelector(scope, "server-one")
	selector[key] = value
	return selector
}

// A tool or disposition limit is saved on connect rules and connect blocks
// only. Viewing or managing a server names no tool.
func TestValidateSelector_LimitsAreForConnectOnly(t *testing.T) {
	t.Parallel()

	limits := map[string]string{SelectorKeyTool: "search", SelectorKeyDisposition: DispositionReadOnly}
	for key, value := range limits {
		for _, scope := range []Scope{ScopeMCPConnect, ScopeMCPBlockedConnect} {
			require.NoError(t, ValidateSelector(scope, limitedSelector(scope, key, value)), "%s limited by %s", scope, key)
		}
		for _, scope := range []Scope{ScopeMCPRead, ScopeMCPWrite, ScopeMCPBlockedRead, ScopeMCPBlockedWrite} {
			require.ErrorContains(t, ValidateSelector(scope, limitedSelector(scope, key, value)), "is not allowed for scope", "%s limited by %s", scope, key)
		}
	}
}

// Rows saved before limits moved to connect still validate when read back,
// and nothing else is loosened for them.
func TestValidateStoredSelector_AcceptsLimitsSavedEarlier(t *testing.T) {
	t.Parallel()

	for _, scope := range []Scope{ScopeMCPRead, ScopeMCPWrite, ScopeMCPBlockedRead, ScopeMCPBlockedWrite} {
		require.NoError(t, ValidateStoredSelector(scope, limitedSelector(scope, SelectorKeyTool, "search")))
		require.NoError(t, ValidateStoredSelector(scope, limitedSelector(scope, SelectorKeyDisposition, DispositionDestructive)))
		require.Error(t, ValidateStoredSelector(scope, limitedSelector(scope, SelectorKeyDisposition, "not-a-disposition")))
		require.Error(t, ValidateStoredSelector(scope, limitedSelector(scope, SelectorKeyServerURL, "https://example.com")))
	}
	require.Error(t, ValidateStoredSelector(ScopeProjectRead, limitedSelector(ScopeProjectRead, SelectorKeyTool, "search")))
}

// Loading a stored rule as the effect it has must not change a decision. A
// check for viewing or managing a server never names a tool, so a limited
// allow already matches like an unlimited one and a limited block never
// matches.
func TestStoredSelectorInEffect_KeepsEveryDecision(t *testing.T) {
	t.Parallel()

	checks := []Selector{
		{SelectorKeyResourceKind: ResourceKindMCP, SelectorKeyResourceID: "server-one", SelectorKeyProjectID: "project-one"},
		{SelectorKeyResourceKind: ResourceKindMCP, SelectorKeyResourceID: "server-two", SelectorKeyProjectID: "project-one"},
		{SelectorKeyResourceKind: ResourceKindMCP, SelectorKeyResourceID: "server-one"},
	}
	limits := map[string]string{SelectorKeyTool: "search", SelectorKeyDisposition: DispositionReadOnly}

	for key, value := range limits {
		for _, scope := range []Scope{ScopeMCPRead, ScopeMCPWrite} {
			stored := limitedSelector(scope, key, value)
			before := maps.Clone(stored)

			inEffect, ok := StoredSelectorInEffect(scope, stored)
			require.True(t, ok)
			require.NotContains(t, inEffect, key)
			require.Equal(t, before, stored, "the stored selector must not be changed in place")
			for _, check := range checks {
				require.Equal(t, stored.Matches(check), inEffect.Matches(check), "%s limited by %s against %v", scope, key, check)
			}
		}
		for _, scope := range []Scope{ScopeMCPBlockedRead, ScopeMCPBlockedWrite} {
			stored := limitedSelector(scope, key, value)

			_, ok := StoredSelectorInEffect(scope, stored)
			require.False(t, ok)
			for _, check := range checks {
				require.False(t, stored.StrictMatches(check), "%s limited by %s never blocked %v", scope, key, check)
			}
		}
	}
}

// Rules without a limit, and connect rules with one, load exactly as stored.
func TestStoredSelectorInEffect_LeavesOtherRulesAlone(t *testing.T) {
	t.Parallel()

	for _, scope := range []Scope{ScopeMCPRead, ScopeMCPWrite, ScopeMCPBlockedRead, ScopeMCPBlockedWrite} {
		stored := NewSelector(scope, "server-one")
		inEffect, ok := StoredSelectorInEffect(scope, stored)
		require.True(t, ok)
		require.Equal(t, stored, inEffect)
	}
	for _, scope := range []Scope{ScopeMCPConnect, ScopeMCPBlockedConnect} {
		stored := limitedSelector(scope, SelectorKeyTool, "search")
		inEffect, ok := StoredSelectorInEffect(scope, stored)
		require.True(t, ok)
		require.Equal(t, stored, inEffect)
	}
}
