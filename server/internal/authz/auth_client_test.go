package authz

import (
	"context"
	"maps"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/contextvalues"

	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/workos"
)

const (
	authClientA   = "client_a"
	authProjectP  = "proj_p"
	authProjectQ  = "proj_q"
	sessionUserID = "user_123"
)

func (c Check) withRepresentedUser(userID string) Check {
	c.representedUser = userID
	return c
}

func authClientGrant(principal string, scope Scope, selector Selector) Grant {
	grant := NewGrantWithSelector(scope, selector)
	grant.PrincipalUrn = principal
	return grant
}

func authSelector(resourceID string, extra map[string]string) Selector {
	selector := Selector{SelectorKeyResourceKind: ResourceKindMCPAuthClient, SelectorKeyResourceID: resourceID}
	maps.Copy(selector, extra)
	return selector
}

func sharedClientCheck(projectID string) Check {
	return MCPAuthClientCheck(authClientA, MCPAuthClientDimensions{ClientType: AuthClientTypeServiceAccount, ProjectID: projectID, ConnectionOwner: ""})
}

func personalClientCheck(owner string) Check {
	return MCPAuthClientCheck(authClientA, MCPAuthClientDimensions{ClientType: AuthClientTypeRemoteSession, ProjectID: authProjectP, ConnectionOwner: owner})
}

func TestMCPAuthScopesUseAuthClientResourceKind(t *testing.T) {
	t.Parallel()

	require.Equal(t, ResourceKindMCPAuthClient, ResourceKindForScope(ScopeMCPAuth))
	require.Equal(t, ResourceKindMCPAuthClient, ResourceKindForScope(ScopeMCPBlockedAuth))
	require.Equal(t, ResourceKindMCP, ResourceKindForScope(ScopeMCPConnect))

	exclusion, ok := ExclusionScopeFor(ScopeMCPAuth)
	require.True(t, ok)
	require.Equal(t, ScopeMCPBlockedAuth, exclusion)
	require.Equal(t, []Scope{ScopeMCPAuth}, ScopeImplicationClosure(ScopeMCPAuth))
	for _, scope := range []Scope{ScopeMCPConnect, ScopeMCPWrite, ScopeOrgAdmin} {
		require.NotContains(t, ScopeImplicationClosure(scope), ScopeMCPAuth, "%s must not imply mcp:auth", scope)
	}
}

func TestValidateSelectorForAuthClients(t *testing.T) {
	t.Parallel()

	valid := []Selector{
		authSelector("*", nil),
		authSelector(authClientA, nil),
		authSelector("*", map[string]string{SelectorKeyClientType: AuthClientTypeRemoteSession, SelectorKeyConnectionOwner: SelectorValueRepresentedUser}),
		authSelector("*", map[string]string{SelectorKeyProjectID: authProjectP, SelectorKeyClientType: "*"}),
		authSelector(authClientA, map[string]string{SelectorKeyConnectionOwner: "user_456"}),
	}
	for _, selector := range valid {
		require.NoError(t, ValidateSelector(ScopeMCPAuth, selector), "%v", selector)
		require.NoError(t, ValidateSelector(ScopeMCPBlockedAuth, selector), "%v", selector)
	}

	invalid := map[string]struct {
		scope    Scope
		selector Selector
	}{
		"mcp kind":                {ScopeMCPAuth, Selector{SelectorKeyResourceKind: ResourceKindMCP, SelectorKeyResourceID: "*"}},
		"unknown client type":     {ScopeMCPAuth, authSelector("*", map[string]string{SelectorKeyClientType: "hosted"})},
		"tool key":                {ScopeMCPAuth, authSelector("*", map[string]string{SelectorKeyTool: "search"})},
		"empty owner":             {ScopeMCPAuth, authSelector("*", map[string]string{SelectorKeyConnectionOwner: ""})},
		"empty project":           {ScopeMCPAuth, authSelector("*", map[string]string{SelectorKeyProjectID: ""})},
		"marker as resource":      {ScopeMCPAuth, authSelector(SelectorValueRepresentedUser, nil)},
		"marker as project":       {ScopeMCPAuth, authSelector("*", map[string]string{SelectorKeyProjectID: SelectorValueRepresentedUser})},
		"owner on server scope":   {ScopeMCPConnect, Selector{SelectorKeyResourceKind: ResourceKindMCP, SelectorKeyResourceID: "*", SelectorKeyConnectionOwner: SelectorValueRepresentedUser}},
		"marker on server scope":  {ScopeMCPConnect, Selector{SelectorKeyResourceKind: ResourceKindMCP, SelectorKeyResourceID: "*", SelectorKeyProjectID: SelectorValueRepresentedUser}},
		"client type on mcp kind": {ScopeMCPConnect, Selector{SelectorKeyResourceKind: ResourceKindMCP, SelectorKeyResourceID: "*", SelectorKeyClientType: AuthClientTypeEMA}},
		"owner on shared type":    {ScopeMCPAuth, authSelector("*", map[string]string{SelectorKeyClientType: AuthClientTypeServiceAccount, SelectorKeyConnectionOwner: "user_456"})},
	}
	for name, tc := range invalid {
		require.Error(t, ValidateSelector(tc.scope, tc.selector), name)
	}
}

func TestMCPAuthClientCheckRejectsMissingDimensions(t *testing.T) {
	t.Parallel()

	grants := []Grant{NewGrant(ScopeMCPAuth, WildcardResource)}
	invalid := map[string]Check{
		"missing client":            MCPAuthClientCheck("", MCPAuthClientDimensions{ClientType: AuthClientTypeServiceAccount, ProjectID: authProjectP, ConnectionOwner: ""}),
		"wildcard client":           MCPAuthClientCheck("*", MCPAuthClientDimensions{ClientType: AuthClientTypeServiceAccount, ProjectID: authProjectP, ConnectionOwner: ""}),
		"missing type":              MCPAuthClientCheck(authClientA, MCPAuthClientDimensions{ClientType: "", ProjectID: authProjectP, ConnectionOwner: ""}),
		"unknown type":              MCPAuthClientCheck(authClientA, MCPAuthClientDimensions{ClientType: "hosted", ProjectID: authProjectP, ConnectionOwner: ""}),
		"wildcard type":             MCPAuthClientCheck(authClientA, MCPAuthClientDimensions{ClientType: "*", ProjectID: authProjectP, ConnectionOwner: ""}),
		"missing project":           sharedClientCheck(""),
		"wildcard project":          sharedClientCheck("*"),
		"personal without owner":    personalClientCheck(""),
		"personal wildcard owner":   personalClientCheck("*"),
		"personal marker owner":     personalClientCheck(SelectorValueRepresentedUser),
		"shared client with owner":  MCPAuthClientCheck(authClientA, MCPAuthClientDimensions{ClientType: AuthClientTypeEMA, ProjectID: authProjectP, ConnectionOwner: sessionUserID}),
		"extra dimension":           {Scope: ScopeMCPAuth, ResourceKind: "", ResourceID: authClientA, Dimensions: map[string]string{SelectorKeyClientType: AuthClientTypeServiceAccount, SelectorKeyProjectID: authProjectP, SelectorKeyTool: "x"}, selectorMatch: selectorMatchStrict, representedUser: ""},
		"wrong resource kind":       {Scope: ScopeMCPAuth, ResourceKind: ResourceKindMCP, ResourceID: authClientA, Dimensions: map[string]string{SelectorKeyClientType: AuthClientTypeServiceAccount, SelectorKeyProjectID: authProjectP}, selectorMatch: selectorMatchStrict, representedUser: ""},
		"dimensionless auth scope":  {Scope: ScopeMCPAuth, ResourceKind: "", ResourceID: authClientA, Dimensions: nil, selectorMatch: selectorMatchNormal, representedUser: ""},
		"dimensionless block scope": {Scope: ScopeMCPBlockedAuth, ResourceKind: "", ResourceID: authClientA, Dimensions: nil, selectorMatch: selectorMatchNormal, representedUser: ""},
	}
	for name, check := range invalid {
		_, err := GrantsAuthorize(grants, check)
		require.ErrorIs(t, err, ErrInvalidCheck, name)
	}
}

func TestMCPAuthMatchesStrictly(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		grant   Selector
		check   Check
		allowed bool
	}{
		"wildcard grant":                {authSelector("*", nil), sharedClientCheck(authProjectP), true},
		"client grant":                  {authSelector(authClientA, nil), sharedClientCheck(authProjectP), true},
		"other client":                  {authSelector("client_b", nil), sharedClientCheck(authProjectP), false},
		"type grant matches type":       {authSelector("*", map[string]string{SelectorKeyClientType: AuthClientTypeServiceAccount}), sharedClientCheck(authProjectP), true},
		"type grant other type":         {authSelector("*", map[string]string{SelectorKeyClientType: AuthClientTypeEMA}), sharedClientCheck(authProjectP), false},
		"project grant same project":    {authSelector("*", map[string]string{SelectorKeyProjectID: authProjectP}), sharedClientCheck(authProjectP), true},
		"project grant other project":   {authSelector("*", map[string]string{SelectorKeyProjectID: authProjectQ}), sharedClientCheck(authProjectP), false},
		"owner grant on shared client":  {authSelector("*", map[string]string{SelectorKeyConnectionOwner: "*"}), sharedClientCheck(authProjectP), false},
		"marker grant on shared client": {authSelector("*", map[string]string{SelectorKeyConnectionOwner: SelectorValueRepresentedUser}), sharedClientCheck(authProjectP), false},
	}
	for name, tc := range cases {
		allowed, err := GrantsAuthorize([]Grant{NewGrantWithSelector(ScopeMCPAuth, tc.grant)}, tc.check)
		require.NoError(t, err, name)
		require.Equal(t, tc.allowed, allowed, name)
	}
}

func TestMCPAuthPersonalConnectionsBindRepresentedUser(t *testing.T) {
	t.Parallel()

	markerGrant := NewGrantWithSelector(ScopeMCPAuth, authSelector("*", map[string]string{SelectorKeyClientType: AuthClientTypeRemoteSession, SelectorKeyConnectionOwner: SelectorValueRepresentedUser}))
	wildcardGrant := NewGrant(ScopeMCPAuth, WildcardResource)
	foreignOwnerGrant := NewGrantWithSelector(ScopeMCPAuth, authSelector("*", map[string]string{SelectorKeyConnectionOwner: "user_456"}))

	cases := map[string]struct {
		grant    Grant
		check    Check
		allowed  bool
		notOwned bool
	}{
		"marker binds represented owner": {markerGrant, personalClientCheck(sessionUserID).withRepresentedUser(sessionUserID), true, false},
		"no represented user":            {markerGrant, personalClientCheck(sessionUserID), false, true},
		"foreign owner via marker":       {markerGrant, personalClientCheck("user_456").withRepresentedUser(sessionUserID), false, true},
		"foreign owner via wildcard":     {wildcardGrant, personalClientCheck("user_456").withRepresentedUser(sessionUserID), false, true},
		"foreign owner via named grant":  {foreignOwnerGrant, personalClientCheck("user_456").withRepresentedUser(sessionUserID), false, true},
		"named grant for another owner":  {foreignOwnerGrant, personalClientCheck(sessionUserID).withRepresentedUser(sessionUserID), false, false},
		"wildcard grant own connection":  {wildcardGrant, personalClientCheck(sessionUserID).withRepresentedUser(sessionUserID), true, false},
	}
	for name, tc := range cases {
		allowed, err := GrantsAuthorize([]Grant{tc.grant}, tc.check)
		require.NoError(t, err, name)
		require.Equal(t, tc.allowed, allowed, name)

		explanation, err := ExplainGrantCheck([]Grant{tc.grant}, tc.check)
		require.NoError(t, err, name)
		require.Equal(t, tc.allowed, explanation.Allowed, name)
		require.Equal(t, tc.notOwned, explanation.ConnectionNotOwned, name)
	}
}

func TestMCPBlockedAuthAppliesToSharedClientEverywhere(t *testing.T) {
	t.Parallel()

	allow := authClientGrant("role:member", ScopeMCPAuth, authSelector("*", nil))
	block := authClientGrant("role:member", ScopeMCPBlockedAuth, authSelector(authClientA, nil))
	for _, project := range []string{authProjectP, authProjectQ} {
		allowed, err := GrantsAuthorize([]Grant{allow, block}, sharedClientCheck(project))
		require.NoError(t, err)
		require.False(t, allowed, "a block on the client applies from every server's project")
	}

	typeBlock := authClientGrant("user:all", ScopeMCPBlockedAuth, authSelector("*", map[string]string{SelectorKeyClientType: AuthClientTypeRemoteSession}))
	allowed, err := GrantsAuthorize([]Grant{allow, typeBlock}, personalClientCheck(sessionUserID).withRepresentedUser(sessionUserID))
	require.NoError(t, err)
	require.False(t, allowed, "a client-type block withdraws personal use")

	ownerBlock := authClientGrant("agent:agent_1", ScopeMCPBlockedAuth, authSelector(authClientA, map[string]string{SelectorKeyConnectionOwner: "user_456"}))
	allowed, err = GrantsAuthorize([]Grant{allow, ownerBlock}, personalClientCheck(sessionUserID).withRepresentedUser(sessionUserID))
	require.NoError(t, err)
	require.True(t, allowed, "a restriction on another owner's connection does not reach this one")

	markerBlock := authClientGrant("role:member", ScopeMCPBlockedAuth, authSelector(authClientA, map[string]string{SelectorKeyConnectionOwner: SelectorValueRepresentedUser}))
	allowed, err = GrantsAuthorize([]Grant{allow, markerBlock}, personalClientCheck(sessionUserID).withRepresentedUser(sessionUserID))
	require.NoError(t, err)
	require.False(t, allowed, "a symbolic block binds to the represented user")
}

func TestMCPAuthKeepsDirectGrantPrecedence(t *testing.T) {
	t.Parallel()

	roleBlock := authClientGrant("role:member", ScopeMCPBlockedAuth, authSelector(authClientA, nil))
	directAllow := authClientGrant("user:"+sessionUserID, ScopeMCPAuth, authSelector(authClientA, nil))
	allowed, err := GrantsAuthorize([]Grant{roleBlock, directAllow}, sharedClientCheck(authProjectP))
	require.NoError(t, err)
	require.True(t, allowed, "a direct allow naming the client outranks an inherited block")

	agentAllow := authClientGrant("agent:agent_1", ScopeMCPAuth, authSelector(authClientA, nil))
	allowed, err = GrantsAuthorize([]Grant{roleBlock, agentAllow}, sharedClientCheck(authProjectP))
	require.NoError(t, err)
	require.False(t, allowed, "agent grants never outrank a block")

	ownBlock := authClientGrant("user:"+sessionUserID, ScopeMCPBlockedAuth, authSelector(authClientA, nil))
	allowed, err = GrantsAuthorize([]Grant{ownBlock, directAllow}, sharedClientCheck(authProjectP))
	require.NoError(t, err)
	require.False(t, allowed, "the user's own block still wins")
}

func TestServerScopesKeepLenientMatching(t *testing.T) {
	t.Parallel()

	grant := NewGrantWithSelector(ScopeMCPConnect, Selector{SelectorKeyResourceKind: ResourceKindMCP, SelectorKeyResourceID: "toolset_1", SelectorKeyDisposition: DispositionReadOnly})
	allowed, err := GrantsAuthorize([]Grant{grant}, MCPCheck(ScopeMCPConnect, "toolset_1", authProjectP))
	require.NoError(t, err)
	require.True(t, allowed)
}

func TestEngineBindsRepresentedUserFromSession(t *testing.T) {
	t.Parallel()

	engine := NewEngine(testenv.NewLogger(t), nil, staticChallengeLogging(false), workos.NewStubClient())
	markerGrant := NewGrantWithSelector(ScopeMCPAuth, authSelector("*", map[string]string{SelectorKeyConnectionOwner: SelectorValueRepresentedUser}))
	ctx := GrantsToContext(enterpriseSessionCtx(t), []Grant{markerGrant})

	require.NoError(t, engine.Require(ctx, personalClientCheck(sessionUserID)))
	requireForbidden(t, engine.Require(ctx, personalClientCheck("user_456")))
	requireForbidden(t, engine.Require(ctx, personalClientCheck("user_456").withRepresentedUser("user_456"))) // a caller-set binding is replaced
}

func TestEngineRequiresRepresentedUserPolicyForAgents(t *testing.T) {
	t.Parallel()

	engine := NewEngine(testenv.NewLogger(t), nil, staticChallengeLogging(false), workos.NewStubClient())
	markerGrant := NewGrantWithSelector(ScopeMCPAuth, authSelector("*", map[string]string{SelectorKeyConnectionOwner: SelectorValueRepresentedUser}))
	connect := NewGrant(ScopeMCPConnect, WildcardResource)
	agentPolicy := []Grant{markerGrant, connect}
	// The owner A's live policy holds the marker too; it must bind to U, not A.
	ownerPolicy := []Grant{markerGrant, connect}

	ctxFor := func(authorizer RepresentedUserPolicy) context.Context {
		return admittedPoliciesToContext(principalPolicyTestContext(t, nil, nil, nil), authorizer, agentPolicy, agentPolicy, ownerPolicy)
	}

	permitted := ctxFor(RepresentedUserPolicy{UserID: "user_u", Policy: []Grant{markerGrant}})
	require.NoError(t, engine.Require(permitted, personalClientCheck("user_u")))
	requireForbidden(t, engine.Require(permitted, personalClientCheck("user_a"))) // never the agent owner's connection

	unpermitted := ctxFor(RepresentedUserPolicy{UserID: "user_u", Policy: []Grant{connect}})
	requireForbidden(t, engine.Require(unpermitted, personalClientCheck("user_u"))) // U must hold mcp:auth too
	require.NoError(t, engine.Require(unpermitted, MCPCheck(ScopeMCPConnect, "toolset_1", authProjectP)), "other scopes ignore U's policy")

	ineligible := ctxFor(RepresentedUserPolicy{UserID: "user_u", Policy: []Grant{}})
	requireForbidden(t, engine.Require(ineligible, sharedClientCheck(authProjectP)))

	workload := admittedPoliciesToContext(principalPolicyTestContext(t, nil, nil, nil), RepresentedUserPolicy{UserID: "", Policy: nil}, agentPolicy, agentPolicy, ownerPolicy)
	requireForbidden(t, engine.Require(workload, personalClientCheck("user_u"))) // no represented user resolves no connection
}

func TestSystemAndSupportGrantsExcludeMCPAuth(t *testing.T) {
	t.Parallel()

	for _, role := range []string{SystemRoleAdmin, SystemRoleMember} {
		for _, grant := range SystemRoleGrants[role] {
			require.NotEqual(t, string(ScopeMCPAuth), grant.Scope, role)
		}
	}
	for _, grant := range allScopeGrants() {
		require.NotEqual(t, ScopeMCPAuth, grant.Scope, "administering an organization implies no use of its auth clients")
	}
}

func TestGrantsSatisfyEnforcesOwnership(t *testing.T) {
	t.Parallel()

	grants := []Grant{NewGrant(ScopeMCPAuth, WildcardResource)}
	require.False(t, GrantsSatisfy(grants, personalClientCheck("user_456").withRepresentedUser(sessionUserID)))
	require.False(t, GrantsSatisfy(grants, personalClientCheck(sessionUserID)))
	require.True(t, GrantsSatisfy(grants, personalClientCheck(sessionUserID).withRepresentedUser(sessionUserID)))
}

func TestAssistantTokensRepresentNoUser(t *testing.T) {
	t.Parallel()

	engine := NewEngine(testenv.NewLogger(t), nil, staticChallengeLogging(false), workos.NewStubClient())
	markerGrant := NewGrantWithSelector(ScopeMCPAuth, authSelector("*", map[string]string{SelectorKeyConnectionOwner: SelectorValueRepresentedUser}))
	ctx := contextvalues.SetAuthContext(t.Context(), &contextvalues.AuthContext{ActiveOrganizationID: "org_123", UserID: sessionUserID, AccountType: "enterprise"})
	ctx = contextvalues.SetAssistantPrincipal(ctx, contextvalues.AssistantPrincipal{AssistantID: uuid.New(), ThreadID: uuid.New()})
	ctx = GrantsToContext(ctx, []Grant{markerGrant})

	enforce, err := engine.ShouldEnforce(ctx)
	require.NoError(t, err)
	require.True(t, enforce)
	requireForbidden(t, engine.Require(ctx, personalClientCheck(sessionUserID))) // the assistant's owner is attribution
}

func TestUnenforcedPathsDenyAuthClientChecks(t *testing.T) {
	t.Parallel()

	engine := NewEngine(testenv.NewLogger(t), nil, staticChallengeLogging(false), workos.NewStubClient())
	ctx := contextvalues.SetAuthContext(t.Context(), &contextvalues.AuthContext{ActiveOrganizationID: "org_123", UserID: sessionUserID, AccountType: "enterprise"})
	enforce, err := engine.ShouldEnforce(ctx)
	require.NoError(t, err)
	require.False(t, enforce, "a sessionless context skips RBAC")

	own := personalClientCheck(sessionUserID)
	shared := sharedClientCheck(authProjectP)
	other := MCPCheck(ScopeMCPConnect, "toolset_1", authProjectP)

	requireForbidden(t, engine.Require(ctx, own))
	requireForbidden(t, engine.Require(ctx, other, shared))
	require.NoError(t, engine.Require(ctx, other), "unrelated scopes keep today's behavior")
	requireForbidden(t, engine.RequireAny(ctx, own, shared))
	require.NoError(t, engine.RequireAny(ctx, own, other))
	requireForbidden(t, engine.RequireAnyUnblocked(ctx, shared))

	allowed, err := engine.Evaluate(ctx, shared)
	require.NoError(t, err)
	require.False(t, allowed)

	ids, err := engine.Filter(ctx, []Check{shared, other})
	require.NoError(t, err)
	require.Equal(t, []string{"toolset_1"}, ids)

	matched, err := engine.FindMatched(ctx, []Check{shared, other})
	require.NoError(t, err)
	require.Equal(t, []bool{false, true}, matched)
}

func TestRootAndOverridesGrantNoAuthClientUse(t *testing.T) {
	t.Parallel()

	allowed, err := GrantsAuthorize([]Grant{NewGrant(ScopeRoot, WildcardResource)}, sharedClientCheck(authProjectP))
	require.NoError(t, err)
	require.False(t, allowed, "root administers but uses no auth client")

	rootScoped := sharedClientCheck(authProjectP)
	rootScoped.Scope = ScopeRoot
	rootScoped.ResourceKind = ResourceKindMCPAuthClient
	allowed, err = GrantsAuthorize([]Grant{NewGrant(ScopeRoot, WildcardResource)}, rootScoped)
	require.ErrorIs(t, err, ErrInvalidCheck)
	require.False(t, allowed, "a root-scoped auth-client check is rejected")

	grants := GrantsFromOverrides([]RoleGrant{{Scope: string(ScopeMCPAuth), Selectors: nil}, {Scope: string(ScopeMCPConnect), Selectors: nil}})
	require.Len(t, grants, 1)
	require.Equal(t, ScopeMCPConnect, grants[0].Scope)
}

func TestEmbeddedChatSessionsRepresentNoUser(t *testing.T) {
	t.Parallel()

	engine := NewEngine(testenv.NewLogger(t), nil, staticChallengeLogging(false), workos.NewStubClient())
	markerGrant := NewGrantWithSelector(ScopeMCPAuth, authSelector("*", map[string]string{SelectorKeyConnectionOwner: SelectorValueRepresentedUser}))
	sessionID := "session_123"
	ctx := contextvalues.SetAuthContext(t.Context(), &contextvalues.AuthContext{ActiveOrganizationID: "org_123", UserID: sessionUserID, ExternalUserID: "ext-user", SessionID: &sessionID, AccountType: "enterprise"})
	ctx = GrantsToContext(ctx, []Grant{markerGrant})
	requireForbidden(t, engine.Require(ctx, personalClientCheck(sessionUserID))) // the session's minter is attribution
}
