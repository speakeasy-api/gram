package authz

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

func TestChannelsScopesAreIndependent(t *testing.T) {
	t.Parallel()
	roleGrants := func(role string) []Grant {
		var overrides []RoleGrant
		for _, grant := range SystemRoleGrants[role] {
			overrides = append(overrides, *grant)
		}
		return GrantsFromOverrides(overrides)
	}
	scopes := []Scope{ScopeChannelsConnect, ScopeChannelsAdmin, ScopeChannelsReadAll}
	for _, scope := range scopes {
		require.Contains(t, RegisteredScopes(), scope)
		require.Equal(t, ResourceKindOrg, ResourceKindForScope(scope))
		for _, other := range append(scopes, ScopeMCPConnect, ScopeMCPRead, ScopeMCPWrite, ScopeOrgAdmin) {
			require.Equal(t, scope == other, GrantsSatisfy([]Grant{NewGrant(other, "org_example")}, Check{Scope: scope, ResourceID: "org_example"}), "grant %s must not imply %s", other, scope)
		}
		require.False(t, GrantsSatisfy(roleGrants(SystemRoleMember), Check{Scope: scope, ResourceID: "org_example"}))
	}
}

// An administrator administers Channels without reading private conversations.
// channels:read_all is the one Channels permission that discloses other
// members' message content, so it is granted explicitly through a custom role
// rather than arriving with the system administrator role.
func TestChannelsAdminRoleDoesNotReadPrivateConversations(t *testing.T) {
	t.Parallel()
	grants := GrantsFromOverrides(func() []RoleGrant {
		var overrides []RoleGrant
		for _, grant := range SystemRoleGrants[SystemRoleAdmin] {
			overrides = append(overrides, *grant)
		}
		return overrides
	}())

	require.True(t, GrantsSatisfy(grants, Check{Scope: ScopeChannelsConnect, ResourceID: "org_example"}))
	require.True(t, GrantsSatisfy(grants, Check{Scope: ScopeChannelsAdmin, ResourceID: "org_example"}))
	require.False(t, GrantsSatisfy(grants, Check{Scope: ScopeChannelsReadAll, ResourceID: "org_example"}),
		"the administrator role must not carry channels:read_all; it is granted explicitly")
	require.NotContains(t, adminScopes, ScopeChannelsReadAll)
}

// Channels is session-less by design: it authenticates an OAuth bearer or a
// browser session through PrepareExternalContext, which binds no SessionID.
// Evaluate reports every check as allowed when enforcement is off, so a
// session-less surface left out of enforcedWithoutSession is granted
// everything rather than nothing. This is the one place that asymmetry is
// pinned down.
func TestChannelsSurfaceIsEnforcedWithoutASession(t *testing.T) {
	t.Parallel()
	surface := func(value string) context.Context {
		return contextvalues.SetActingSurface(t.Context(), value)
	}
	require.True(t, enforcedWithoutSession(surface(contextvalues.ActingSurfaceChannels)))
	require.True(t, enforcedWithoutSession(surface(contextvalues.ActingSurfacePlatformMCP)))
	require.False(t, enforcedWithoutSession(t.Context()))
	require.False(t, enforcedWithoutSession(surface("dashboard")))
	require.False(t, enforcedWithoutSession(surface("")))
}

// Enforcement on the Channels surface actually denies: a session-less Channels
// caller with no Channels grant is refused, and is granted only what it holds.
func TestChannelsSessionlessCallerIsDenied(t *testing.T) {
	t.Parallel()
	engine := NewEngine(testenv.NewLogger(t), nil, func(context.Context, string) (bool, error) { return false, nil }, nil)
	channelsContext := func(grants ...Grant) context.Context {
		ctx := contextvalues.WithAuthenticatedActor(t.Context(), &contextvalues.AuthContext{
			UserID:               "user_example",
			ActiveOrganizationID: "org_example",
		}, urn.NewPrincipal(urn.PrincipalTypeUser, "user_example"))
		return GrantsToContext(contextvalues.SetActingSurface(ctx, contextvalues.ActingSurfaceChannels), grants)
	}
	check := Check{Scope: ScopeChannelsConnect, ResourceID: "org_example"}

	allowed, err := engine.Evaluate(channelsContext(), check)
	require.NoError(t, err)
	require.False(t, allowed, "a channels caller holding no grant must be denied, not admitted by inactive enforcement")

	allowed, err = engine.Evaluate(channelsContext(NewGrant(ScopeOrgAdmin, "org_example"), NewGrant(ScopeMCPConnect, "org_example")), check)
	require.NoError(t, err)
	require.False(t, allowed, "organization administration and MCP access must not grant channels:connect")

	allowed, err = engine.Evaluate(channelsContext(NewGrant(ScopeChannelsConnect, "org_example")), check)
	require.NoError(t, err)
	require.True(t, allowed)
}
