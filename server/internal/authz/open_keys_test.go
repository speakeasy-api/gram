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

	project := []string{SelectorKeyProjectID}
	server := []string{SelectorKeyServerURL, SelectorKeyServerIdentity}
	want := map[Scope]openKeys{
		ScopeAssistantRead:      {Probe: project, Connection: nil, PolicyAudience: nil},
		ScopeAssistantWrite:     {Probe: project, Connection: nil, PolicyAudience: nil},
		ScopeEnvironmentRead:    {Probe: project, Connection: nil, PolicyAudience: nil},
		ScopeEnvironmentWrite:   {Probe: project, Connection: nil, PolicyAudience: nil},
		ScopeInferenceExecute:   {Probe: project, Connection: nil, PolicyAudience: nil},
		ScopeMCPRead:            {Probe: project, Connection: nil, PolicyAudience: nil},
		ScopeMCPWrite:           {Probe: project, Connection: nil, PolicyAudience: nil},
		ScopeOrgHooksIngest:     {Probe: project, Connection: nil, PolicyAudience: nil},
		ScopeRiskPolicyBlock:    {Probe: server, Connection: nil, PolicyAudience: nil},
		ScopeRiskPolicyBypass:   {Probe: server, Connection: nil, PolicyAudience: nil},
		ScopeRiskPolicyEvaluate: {Probe: server, Connection: nil, PolicyAudience: server},
		ScopeMCPConnect: {
			Probe:          []string{SelectorKeyProjectID, SelectorKeyTool, SelectorKeyDisposition},
			Connection:     []string{SelectorKeyTool, SelectorKeyDisposition},
			PolicyAudience: nil,
		},
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

	probe := Check{Scope: ScopeMCPRead, ResourceKind: "", ResourceID: "server-1", Dimensions: nil, exact: false, representedUser: "", probe: true, anyOf: nil, operation: operationDefault}
	require.True(t, probe.mayOmit(SelectorKeyProjectID))
	probe.exact = true
	require.False(t, probe.mayOmit(SelectorKeyProjectID))
}
