package authz

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// TestOpenKeys_Golden lists every grant key a check may leave out, by scope and
// kind of check. Opening a key loosens matching for every check of that kind,
// so a change to the registry's Open lists must change this list too, where a
// reviewer sees it.
func TestOpenKeys_Golden(t *testing.T) {
	t.Parallel()

	// No project_id: a type's parent key is never opened. org:hooks_ingest
	// declares project_id itself, and org has no parent.
	project := []string{SelectorKeyProjectID}
	server := []string{SelectorKeyServerURL, SelectorKeyServerIdentity}
	// A list of entries, not a map literal, so the list names only the
	// scopes that open a key.
	entries := []struct {
		scope Scope
		keys  openKeys
	}{
		{ScopeOrgHooksIngest, openKeys{Probe: project, Connection: nil, PolicyAudience: nil}},
		{ScopeRiskPolicyBlock, openKeys{Probe: server, Connection: nil, PolicyAudience: nil}},
		{ScopeRiskPolicyBypass, openKeys{Probe: server, Connection: nil, PolicyAudience: nil}},
		{ScopeRiskPolicyEvaluate, openKeys{Probe: server, Connection: nil, PolicyAudience: server}},
		{ScopeMCPConnect, openKeys{
			Probe:          []string{SelectorKeyTool, SelectorKeyDisposition},
			Connection:     []string{SelectorKeyTool, SelectorKeyDisposition},
			PolicyAudience: nil,
		}},
	}
	want := make(map[Scope]openKeys, len(entries))
	for _, e := range entries {
		want[e.scope] = e.keys
	}
	require.Equal(t, want, scopeOpenKeys)
}

// Auth clients, blocks and root open nothing: every check on them carries
// every key a grant names, probes included.
func TestOpenKeys_NoneOnAuthClientsBlocksOrRoot(t *testing.T) {
	t.Parallel()

	require.NotContains(t, scopeOpenKeys, ScopeMCPAuth)
	require.NotContains(t, scopeOpenKeys, ScopeRoot)
	for scope, exclusion := range scopeExclusions {
		if exclusion == "" || exclusion == ScopeRiskPolicyBypass {
			continue
		}
		require.NotContains(t, scopeOpenKeys, exclusion, "block of %s", scope)
	}
}

// An exact check opens nothing, even a probe of a scope that opens keys.
func TestOpenKeys_ExactCheckOpensNothing(t *testing.T) {
	t.Parallel()

	probe := Check{Scope: ScopeMCPConnect, ResourceKind: "", ResourceID: "server-1", Dimensions: nil, exact: false, representedUser: "", probe: true, anyOf: nil, operation: operationDefault}
	require.True(t, probe.mayOmit(SelectorKeyTool))
	require.False(t, probe.mayOmit(SelectorKeyProjectID))
	probe.exact = true
	require.False(t, probe.mayOmit(SelectorKeyTool))
}
