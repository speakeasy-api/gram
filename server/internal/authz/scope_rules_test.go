package authz

// Rules that belong to one family of scopes: chat:read needs a grant on every
// chat, inference needs a grant naming the project, and the channels scopes
// are implied by nothing else and enforced even without a session.

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// An unrestricted chat:read grant (held by admins) satisfies the chat read
// check; a principal with no chat:read grant does not. Members never hold a
// chat:read grant — their access to sessions they own is granted by
// owner-matching in the chat handlers, not by this scope.
func TestChatRead_RequiresUnrestrictedGrant(t *testing.T) {
	t.Parallel()

	admin := []Grant{NewGrant(ScopeChatRead, WildcardResource)}
	got, _ := matchingGrant(admin, Chat(ChatIDFrom("chat_1")).Read().expand())
	require.NotNil(t, got, "admin chat:read satisfies the read check")

	none, _ := matchingGrant(nil, Chat(ChatIDFrom("chat_1")).Read().expand())
	require.Nil(t, none, "no chat:read grant means no chat read access")
}

// chat:read carries no extra selector dimensions: only resource_kind and
// resource_id are valid on the chat family.
func TestChatRead_SelectorRejectsExtraKeys(t *testing.T) {
	t.Parallel()

	require.NoError(t, ValidateSelector(ScopeChatRead, Selector{
		SelectorKeyResourceKind: ResourceKindChat,
		SelectorKeyResourceID:   WildcardResource,
	}))

	require.Error(t, ValidateSelector(ScopeChatRead, Selector{
		SelectorKeyResourceKind: ResourceKindChat,
		SelectorKeyResourceID:   WildcardResource,
		"user_id":               "user_self",
	}), "chat scopes allow no extra selector keys")
}

func TestInferenceExecuteRequiresExplicitProjectGrant(t *testing.T) {
	t.Parallel()
	grant := NewGrant(ScopeInferenceExecute, WildcardResource)
	grant.Selector[SelectorKeyProjectID] = "project_a"
	require.NoError(t, ValidateSelector(grant.Scope, grant.Selector))
	require.Equal(t, ResourceKindInference, ResourceKindForScope(grant.Scope))
	require.Equal(t, "project_a", InferenceExecuteCheck("project_a").ResourceID)
	require.NoError(t, validateInput(InferenceExecuteCheck("project_a")))
	require.Error(t, validateInput(InferenceExecuteCheck("")))
	for _, project := range []string{"project_a", "project_b", ""} {
		got, _ := matchingGrant([]Grant{grant}, InferenceExecuteCheck(project).expand())
		if project == "project_a" {
			require.NotNil(t, got)
		} else {
			require.Nil(t, got)
		}
	}
	for _, scope := range []Scope{ScopeChatWrite, ScopeProjectWrite, ScopeOrgAdmin} {
		got, _ := matchingGrant([]Grant{NewGrant(scope, WildcardResource)}, InferenceExecuteCheck("project_a").expand())
		require.Nil(t, got, scope)
	}
	got, _ := matchingGrant([]Grant{grant}, Chat(ChatIDFrom("chat_a")).Write().expand())
	require.Nil(t, got)
	require.Equal(t, []Scope{ScopeInferenceExecute}, ScopeImplicationClosure(ScopeInferenceExecute))
	for role, grants := range SystemRoleGrants {
		for _, g := range grants {
			require.NotEqual(t, string(ScopeInferenceExecute), g.Scope, role)
		}
	}
	require.NoError(t, ValidateGrantSource(GrantSourceRoles, []*RoleGrant{{Scope: string(ScopeInferenceExecute)}}))
}

func TestInferenceSelector(t *testing.T) {
	t.Parallel()
	for _, project := range []string{"project_a", WildcardResource} {
		grant := NewGrant(ScopeInferenceExecute, WildcardResource)
		grant.Selector[SelectorKeyProjectID] = project
		require.NoError(t, ValidateSelector(grant.Scope, grant.Selector))
	}
	require.NoError(t, ValidateSelector(ScopeInferenceExecute, NewGrant(ScopeInferenceExecute, WildcardResource).Selector))
	require.Error(t, ValidateSelector(ScopeInferenceExecute, NewGrant(ScopeInferenceExecute, "project_a").Selector))
	for key, value := range map[string]string{SelectorKeyProjectID: "", SelectorKeyTool: "tool_a"} {
		grant := NewGrant(ScopeInferenceExecute, WildcardResource)
		grant.Selector[key] = value
		require.Error(t, ValidateSelector(grant.Scope, grant.Selector))
	}
}

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
		require.Equal(t, ResourceKindProject, ResourceKindForScope(scope))
		for _, other := range append(scopes, ScopeMCPConnect, ScopeMCPRead, ScopeMCPWrite, ScopeOrgAdmin) {
			require.Equal(t, scope == other, GrantsSatisfy([]Grant{NewGrant(other, "project_example")}, Check{Scope: scope, ResourceID: "project_example"}), "grant %s must not imply %s", other, scope)
		}
		require.False(t, GrantsSatisfy(roleGrants(SystemRoleMember), Check{Scope: scope, ResourceID: "project_example"}))
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

	require.True(t, GrantsSatisfy(grants, Check{Scope: ScopeChannelsConnect, ResourceID: "project_example"}))
	require.True(t, GrantsSatisfy(grants, Check{Scope: ScopeChannelsAdmin, ResourceID: "project_example"}))
	require.False(t, GrantsSatisfy(grants, Check{Scope: ScopeChannelsReadAll, ResourceID: "project_example"}),
		"the administrator role must not carry channels:read_all; it is granted explicitly")
	for _, grant := range SystemRoleGrants[SystemRoleAdmin] {
		require.NotEqual(t, string(ScopeChannelsReadAll), grant.Scope)
	}
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
	check := Check{Scope: ScopeChannelsConnect, ResourceID: "project_example"}

	allowed, err := engine.Evaluate(channelsContext(), check)
	require.NoError(t, err)
	require.False(t, allowed, "a channels caller holding no grant must be denied, not admitted by inactive enforcement")

	allowed, err = engine.Evaluate(channelsContext(NewGrant(ScopeOrgAdmin, "org_example"), NewGrant(ScopeMCPConnect, "org_example")), check)
	require.NoError(t, err)
	require.False(t, allowed, "organization administration and MCP access must not grant channels:connect")

	allowed, err = engine.Evaluate(channelsContext(NewGrant(ScopeChannelsConnect, "project_example")), check)
	require.NoError(t, err)
	require.True(t, allowed)
	allowed, err = engine.Evaluate(channelsContext(NewGrant(ScopeChannelsConnect, "other_project")), check)
	require.NoError(t, err)
	require.False(t, allowed, "a Channels grant for another project must not authorize this project")
}
