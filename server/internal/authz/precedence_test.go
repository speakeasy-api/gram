package authz

import (
	"testing"

	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/workos"
	"github.com/stretchr/testify/require"
)

const (
	precedenceUser     = "user:user-1"
	precedenceAgent    = "agent:0199a8f4-6f53-7c4e-9a52-2f0f0f0f0f01"
	precedenceRole     = "role:organization:role-1"
	precedenceEveryone = "user:all"
)

func heldBy(principal string, grant Grant) Grant {
	grant.PrincipalUrn = principal
	return grant
}

func mcpToolGrant(scope Scope, serverID string, tool string) Grant {
	selector := NewSelector(scope, serverID)
	selector[SelectorKeyTool] = tool
	return NewGrantWithSelector(scope, selector)
}

// principalPrecedenceCase is one precedence scenario and its runtime decision.
type principalPrecedenceCase struct {
	name    string
	grants  []Grant
	check   Check
	allowed bool
}

// principalPrecedenceCases are shared by the runtime and explanation tests, so
// an explanation is checked against the same fixtures as enforcement.
func principalPrecedenceCases() []principalPrecedenceCase {
	serverCheck := MCPCheck(ScopeMCPConnect, "server-1", "project-1")
	return []principalPrecedenceCase{
		{
			name: "direct user grant outranks role block",
			grants: []Grant{
				heldBy(precedenceRole, NewGrant(ScopeMCPConnect, WildcardResource)),
				heldBy(precedenceRole, NewGrant(ScopeMCPBlockedConnect, "server-1")),
				heldBy(precedenceUser, NewGrant(ScopeMCPConnect, "server-1")),
			},
			check:   serverCheck,
			allowed: true,
		},
		{
			name: "direct user grant outranks everyone block",
			grants: []Grant{
				heldBy(precedenceEveryone, NewGrant(ScopeMCPBlockedConnect, "server-1")),
				heldBy(precedenceUser, NewGrant(ScopeMCPConnect, "server-1")),
			},
			check:   serverCheck,
			allowed: true,
		},
		{
			name: "agent grant does not outrank role block",
			grants: []Grant{
				heldBy(precedenceRole, NewGrant(ScopeMCPBlockedConnect, "server-1")),
				heldBy(precedenceAgent, NewGrant(ScopeMCPConnect, "server-1")),
			},
			check:   serverCheck,
			allowed: false,
		},
		{
			name: "direct grant through scope expansion outranks role block",
			grants: []Grant{
				heldBy(precedenceRole, NewGrant(ScopeMCPBlockedConnect, "server-1")),
				heldBy(precedenceUser, NewGrant(ScopeMCPWrite, "server-1")),
			},
			check:   serverCheck,
			allowed: true,
		},
		{
			name: "direct wildcard grant does not outrank role block",
			grants: []Grant{
				heldBy(precedenceRole, NewGrant(ScopeMCPBlockedConnect, "server-1")),
				heldBy(precedenceUser, NewGrant(ScopeMCPConnect, WildcardResource)),
			},
			check:   serverCheck,
			allowed: false,
		},
		{
			name: "direct grant does not outrank the principal's own block",
			grants: []Grant{
				heldBy(precedenceUser, NewGrant(ScopeMCPConnect, "server-1")),
				heldBy(precedenceUser, NewGrant(ScopeMCPBlockedConnect, "server-1")),
			},
			check:   serverCheck,
			allowed: false,
		},
		{
			name: "direct grant for another server does not outrank role block",
			grants: []Grant{
				heldBy(precedenceRole, NewGrant(ScopeMCPConnect, WildcardResource)),
				heldBy(precedenceRole, NewGrant(ScopeMCPBlockedConnect, "server-1")),
				heldBy(precedenceUser, NewGrant(ScopeMCPConnect, "server-2")),
			},
			check:   serverCheck,
			allowed: false,
		},
		{
			name: "role grant does not outrank everyone block",
			grants: []Grant{
				heldBy(precedenceEveryone, NewGrant(ScopeMCPBlockedConnect, "server-1")),
				heldBy(precedenceRole, NewGrant(ScopeMCPConnect, "server-1")),
			},
			check:   serverCheck,
			allowed: false,
		},
		{
			name: "everyone grant does not outrank role block",
			grants: []Grant{
				heldBy(precedenceRole, NewGrant(ScopeMCPBlockedConnect, "server-1")),
				heldBy(precedenceEveryone, NewGrant(ScopeMCPConnect, "server-1")),
			},
			check:   serverCheck,
			allowed: false,
		},
		{
			name: "grants without a principal keep block-wins",
			grants: []Grant{
				NewGrant(ScopeMCPConnect, "server-1"),
				NewGrant(ScopeMCPBlockedConnect, "server-1"),
			},
			check:   serverCheck,
			allowed: false,
		},
		{
			name: "direct grant for one scope does not outrank a block on another",
			grants: []Grant{
				heldBy(precedenceRole, NewGrant(ScopeMCPRead, WildcardResource)),
				heldBy(precedenceRole, NewGrant(ScopeMCPBlockedRead, "server-1")),
				heldBy(precedenceUser, NewGrant(ScopeMCPConnect, "server-1")),
			},
			check:   MCPCheck(ScopeMCPRead, "server-1", "project-1"),
			allowed: false,
		},
		{
			name: "direct project grant outranks role block",
			grants: []Grant{
				heldBy(precedenceRole, NewGrant(ScopeProjectRead, WildcardResource)),
				heldBy(precedenceRole, NewGrant(ScopeProjectBlockedRead, "project-1")),
				heldBy(precedenceUser, NewGrant(ScopeProjectRead, "project-1")),
			},
			check:   Check{Scope: ScopeProjectRead, ResourceKind: "", ResourceID: "project-1", Dimensions: nil},
			allowed: true,
		},
		{
			name: "project block without a direct grant still denies",
			grants: []Grant{
				heldBy(precedenceRole, NewGrant(ScopeProjectRead, WildcardResource)),
				heldBy(precedenceRole, NewGrant(ScopeProjectBlockedRead, "project-1")),
			},
			check:   Check{Scope: ScopeProjectRead, ResourceKind: "", ResourceID: "project-1", Dimensions: nil},
			allowed: false,
		},
		{
			name: "direct project-wide grant does not outrank role block",
			grants: []Grant{
				heldBy(precedenceRole, NewGrant(ScopeMCPConnect, WildcardResource)),
				heldBy(precedenceRole, NewGrant(ScopeMCPBlockedConnect, "server-1")),
				heldBy(precedenceUser, NewGrantWithSelector(ScopeMCPConnect, Selector{
					SelectorKeyResourceKind: ResourceKindMCP,
					SelectorKeyResourceID:   WildcardResource,
					SelectorKeyProjectID:    "project-1",
				})),
			},
			check:   serverCheck,
			allowed: false,
		},
		{
			name: "direct grant outranks project-scoped role block",
			grants: []Grant{
				heldBy(precedenceRole, NewGrant(ScopeMCPConnect, WildcardResource)),
				heldBy(precedenceRole, NewGrantWithSelector(ScopeMCPBlockedConnect, Selector{
					SelectorKeyResourceKind: ResourceKindMCP,
					SelectorKeyResourceID:   "server-1",
					SelectorKeyProjectID:    "project-1",
				})),
				heldBy(precedenceUser, NewGrant(ScopeMCPConnect, "server-1")),
			},
			check:   serverCheck,
			allowed: true,
		},
		{
			name: "direct evaluate grant does not outrank role bypass",
			grants: []Grant{
				heldBy(precedenceRole, NewGrant(ScopeRiskPolicyBypass, "policy-1")),
				heldBy(precedenceUser, NewGrant(ScopeRiskPolicyEvaluate, "policy-1")),
			},
			check:   RiskPolicyEvaluateCheck("policy-1"),
			allowed: false,
		},
	}
}

func TestGrantsAuthorizePrincipalPrecedence(t *testing.T) {
	t.Parallel()

	for _, tc := range principalPrecedenceCases() {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			allowed, err := GrantsAuthorize(tc.grants, tc.check)
			require.NoError(t, err)
			require.Equal(t, tc.allowed, allowed)
		})
	}
}

func TestGrantsAuthorizeNarrowedDirectGrantOutranksRoleBlockOnlyAsFarAsItReaches(t *testing.T) {
	t.Parallel()

	grants := []Grant{
		heldBy(precedenceRole, NewGrant(ScopeMCPConnect, WildcardResource)),
		heldBy(precedenceRole, NewGrant(ScopeMCPBlockedConnect, "server-1")),
		heldBy(precedenceUser, mcpToolGrant(ScopeMCPConnect, "server-1", "tool-a")),
	}

	allowed, err := GrantsAuthorize(grants, MCPCheck(ScopeMCPConnect, "server-1", "project-1"))
	require.NoError(t, err)
	require.True(t, allowed, "a tool-scoped direct grant proves server-level access")

	allowed, err = GrantsAuthorize(grants, MCPToolCallCheck("server-1", MCPToolCallDimensions{Tool: "tool-a", Disposition: "", ProjectID: "project-1"}))
	require.NoError(t, err)
	require.True(t, allowed)

	allowed, err = GrantsAuthorize(grants, MCPToolCallCheck("server-1", MCPToolCallDimensions{Tool: "tool-b", Disposition: "", ProjectID: "project-1"}))
	require.NoError(t, err)
	require.False(t, allowed, "tools outside the direct grant stay blocked")
}

func TestGrantsAuthorizeDispositionDirectGrantReachesOnlyMatchingTools(t *testing.T) {
	t.Parallel()

	direct := NewSelector(ScopeMCPConnect, "server-1")
	direct[SelectorKeyDisposition] = DispositionReadOnly
	grants := []Grant{
		heldBy(precedenceRole, NewGrant(ScopeMCPConnect, WildcardResource)),
		heldBy(precedenceRole, NewGrant(ScopeMCPBlockedConnect, "server-1")),
		heldBy(precedenceUser, NewGrantWithSelector(ScopeMCPConnect, direct)),
	}

	allowed, err := GrantsAuthorize(grants, MCPToolCallCheck("server-1", MCPToolCallDimensions{Tool: "list", Disposition: DispositionReadOnly, ProjectID: "project-1"}))
	require.NoError(t, err)
	require.True(t, allowed)

	allowed, err = GrantsAuthorize(grants, MCPToolCallCheck("server-1", MCPToolCallDimensions{Tool: "unannotated", Disposition: "", ProjectID: "project-1"}))
	require.NoError(t, err)
	require.False(t, allowed, "a tool with no derivable disposition is outside a disposition-scoped grant")
}

func TestEngineFilterPrincipalPrecedence(t *testing.T) {
	t.Parallel()

	engine := NewEngine(testenv.NewLogger(t), nil, staticChallengeLogging(false), workos.NewStubClient())
	ctx := GrantsToContext(enterpriseSessionCtx(t), []Grant{
		heldBy(precedenceRole, NewGrant(ScopeMCPConnect, WildcardResource)),
		heldBy(precedenceRole, NewGrant(ScopeMCPBlockedConnect, "server-1")),
		heldBy(precedenceRole, NewGrant(ScopeMCPBlockedConnect, "server-2")),
		heldBy(precedenceUser, NewGrant(ScopeMCPConnect, "server-1")),
	})

	allowed, err := engine.Filter(ctx, []Check{
		MCPCheck(ScopeMCPConnect, "server-1", ""),
		MCPCheck(ScopeMCPConnect, "server-2", ""),
		MCPCheck(ScopeMCPConnect, "server-3", ""),
	})
	require.NoError(t, err)
	require.ElementsMatch(t, []string{"server-1", "server-3"}, allowed)
}

func TestEngineRequirePrincipalPrecedenceAppliesPerPolicy(t *testing.T) {
	t.Parallel()

	engine := NewEngine(testenv.NewLogger(t), nil, staticChallengeLogging(false), workos.NewStubClient())
	credentialPolicy := []Grant{NewGrant(ScopeMCPConnect, "server-1")}
	agentPolicy := []Grant{heldBy(precedenceAgent, NewGrant(ScopeMCPConnect, "server-1"))}
	ownerPolicy := []Grant{
		heldBy(precedenceRole, NewGrant(ScopeMCPConnect, WildcardResource)),
		heldBy(precedenceRole, NewGrant(ScopeMCPBlockedConnect, "server-1")),
	}
	check := MCPCheck(ScopeMCPConnect, "server-1", "project-1")

	ctx := principalPolicyTestContext(t, credentialPolicy, agentPolicy, ownerPolicy)
	var forbidden *oops.ShareableError
	require.ErrorAs(t, engine.Require(ctx, check), &forbidden, "every admitted policy must allow, so the owner's role block still denies")
	require.Equal(t, oops.CodeForbidden, forbidden.Code)

	ownerPolicy = append(ownerPolicy, heldBy(precedenceUser, NewGrant(ScopeMCPConnect, "server-1")))
	ctx = principalPolicyTestContext(t, credentialPolicy, agentPolicy, ownerPolicy)
	require.NoError(t, engine.Require(ctx, check), "the owner's direct grant outranks the owner's role block")

	agentPolicy = append(agentPolicy, heldBy(precedenceRole, NewGrant(ScopeMCPBlockedConnect, "server-1")))
	ctx = principalPolicyTestContext(t, credentialPolicy, agentPolicy, ownerPolicy)
	require.ErrorAs(t, engine.Require(ctx, check), &forbidden, "the agent's own grant does not outrank its role's block")
	require.Equal(t, oops.CodeForbidden, forbidden.Code)
}

func TestRequireAnyUnblockedPrincipalPrecedence(t *testing.T) {
	t.Parallel()

	engine := NewEngine(testenv.NewLogger(t), nil, staticChallengeLogging(false), workos.NewStubClient())
	checks := []Check{
		{Scope: ScopeSkillRead, ResourceID: "project-one", Dimensions: map[string]string{SelectorKeyProjectID: "project-one"}},
		{Scope: ScopeSkillRead, ResourceID: "skill-one", Dimensions: map[string]string{SelectorKeyProjectID: "project-one"}},
	}
	for _, tc := range []struct {
		name    string
		grants  []Grant
		allowed bool
	}{
		{
			name: "direct skill grant outranks role block",
			grants: []Grant{
				heldBy(precedenceRole, NewGrant(ScopeSkillWrite, "project-one")),
				heldBy(precedenceRole, NewGrant(ScopeSkillBlockedRead, "skill-one")),
				heldBy(precedenceUser, NewGrant(ScopeSkillRead, "skill-one")),
			},
			allowed: true,
		},
		{
			name: "direct wildcard grant does not outrank role block",
			grants: []Grant{
				heldBy(precedenceRole, NewGrant(ScopeSkillBlockedRead, "skill-one")),
				heldBy(precedenceUser, NewGrant(ScopeSkillRead, WildcardResource)),
			},
			allowed: false,
		},
		{
			name: "own block on another alternative still rejects",
			grants: []Grant{
				heldBy(precedenceUser, NewGrant(ScopeSkillRead, "skill-one")),
				heldBy(precedenceUser, NewGrant(ScopeSkillBlockedRead, "project-one")),
			},
			allowed: false,
		},
		{
			name: "direct skill grant does not outrank the principal's own block",
			grants: []Grant{
				heldBy(precedenceUser, NewGrant(ScopeSkillRead, "skill-one")),
				heldBy(precedenceUser, NewGrant(ScopeSkillBlockedRead, "skill-one")),
			},
			allowed: false,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			ctx := GrantsToContext(enterpriseSessionCtx(t), tc.grants)
			err := engine.RequireAnyUnblocked(ctx, checks...)
			if tc.allowed {
				require.NoError(t, err)
				return
			}
			var oopsErr *oops.ShareableError
			require.ErrorAs(t, err, &oopsErr)
			require.Equal(t, oops.CodeForbidden, oopsErr.Code)
		})
	}
}

func TestIsDirectGrant(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		principal string
		direct    bool
	}{
		{precedenceUser, true},
		{precedenceAgent, false},
		{precedenceEveryone, false},
		{precedenceRole, false},
		{"", false},
	} {
		t.Run(tc.principal, func(t *testing.T) {
			t.Parallel()

			require.Equal(t, tc.direct, IsDirectGrant(heldBy(tc.principal, NewGrant(ScopeMCPConnect, "server-1"))))
		})
	}
}
