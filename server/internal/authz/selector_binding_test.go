package authz

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/urn"
)

func TestBindSelectorValueIsDimensionIndependent(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, key string
		scope     Scope
	}{
		{name: "agent owner", key: SelectorKeyOwner, scope: ScopeAgentWrite},
		{name: "MCP tool", key: SelectorKeyTool, scope: ScopeMCPConnect},
		{name: "project", key: SelectorKeyProjectID, scope: ScopeAssistantWrite},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			selector := NewSelector(tc.scope, "resource")
			selector[tc.key] = "self"
			grant := NewGrantWithSelector(tc.scope, selector)
			grant.PrincipalUrn = urn.NewPrincipal(urn.PrincipalTypeUser, "test-user").String()
			bound := BindSelectorValue([]Grant{grant}, tc.key, "self", "caller")
			require.Len(t, bound, 1)
			require.Equal(t, "caller", bound[0].Selector[tc.key])
			require.Equal(t, grant.PrincipalUrn, bound[0].PrincipalUrn)
			require.Equal(t, grant.Scope, bound[0].Scope)
			require.Equal(t, "self", grant.Selector[tc.key], "shared grants must not be mutated")
			check := (Check{Scope: tc.scope, ResourceID: "resource", Dimensions: map[string]string{tc.key: "caller"}}).WithStrictSelectorMatch()
			require.True(t, GrantsSatisfy(bound, check))
			check.Dimensions[tc.key] = "other"
			require.False(t, GrantsSatisfy(bound, check))
			check.Dimensions = nil
			require.False(t, GrantsSatisfy(bound, check), "strict matching rejects every missing dimension, not just owners")
			require.True(t, GrantsSatisfy(bound, Check{Scope: tc.scope, ResourceID: "resource"}), "normal matching remains permissive for every dimension")
		})
	}
}

func TestBindSelectorValuePreservesOtherConstraints(t *testing.T) {
	t.Parallel()
	selector := NewSelector(ScopeMCPConnect, "server")
	selector[SelectorKeyTool] = "current"
	selector[SelectorKeyProjectID] = "current"
	bound := BindSelectorValue([]Grant{NewGrantWithSelector(ScopeMCPConnect, selector)}, SelectorKeyTool, "current", "selected-tool")
	require.Equal(t, "selected-tool", bound[0].Selector[SelectorKeyTool])
	require.Equal(t, "current", bound[0].Selector[SelectorKeyProjectID])

	for _, tool := range []string{"*", "literal-tool"} {
		selector := NewSelector(ScopeMCPConnect, "server")
		selector[SelectorKeyTool] = tool
		grants := []Grant{NewGrantWithSelector(ScopeMCPConnect, selector)}
		require.Equal(t, grants, BindSelectorValue(grants, SelectorKeyTool, "current", "selected-tool"))
	}
}

func TestBindSelectorValueRejectsUnresolvedOrWildcardBinding(t *testing.T) {
	t.Parallel()
	for _, value := range []string{"", "*"} {
		t.Run(value, func(t *testing.T) {
			t.Parallel()
			selector := NewSelector(ScopeMCPConnect, "server")
			selector[SelectorKeyTool] = "current"
			grants := []Grant{NewGrantWithSelector(ScopeMCPBlockedConnect, selector), NewGrant(ScopeMCPConnect, "other-server")}
			bound := BindSelectorValue(grants, SelectorKeyTool, "current", value)
			require.Empty(t, bound, "an unresolved exclusion must not be dropped while retaining allows")
		})
	}
}

func TestWildcardResourcesRequireContainmentNotRuntimeChecks(t *testing.T) {
	t.Parallel()
	for _, scope := range []Scope{ScopeAgentWrite, ScopeMCPConnect} {
		t.Run(string(scope), func(t *testing.T) {
			t.Parallel()
			check := (Check{Scope: scope, ResourceID: "*", Dimensions: map[string]string{SelectorKeyOwner: "caller"}}).WithStrictSelectorMatch()
			require.ErrorIs(t, validateInput(check), ErrInvalidCheck)
			require.True(t, GrantsContainSelector([]Grant{NewGrant(scope, "*")}, scope, NewSelector(scope, "*")))
		})
	}
}
