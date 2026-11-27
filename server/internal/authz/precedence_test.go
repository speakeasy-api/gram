package authz

import (
	"math/rand/v2"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/workos"
)

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

	allowed, err := GrantsAuthorize(grants, MCPServer(MCPServerIDFrom("server-1"), ProjectIDFrom("project-1")).Connect())
	require.NoError(t, err)
	require.True(t, allowed, "a tool-scoped direct grant proves server-level access")

	allowed, err = GrantsAuthorize(grants, MCPServer(MCPServerIDFrom("server-1"), ProjectIDFrom("project-1")).ToolCall("tool-a", ""))
	require.NoError(t, err)
	require.True(t, allowed)

	allowed, err = GrantsAuthorize(grants, MCPServer(MCPServerIDFrom("server-1"), ProjectIDFrom("project-1")).ToolCall("tool-b", ""))
	require.NoError(t, err)
	require.False(t, allowed, "tools outside the direct grant stay blocked")
}

func TestGrantsAuthorizeInferredDirectGrantNeverOutranksInheritedBlock(t *testing.T) {
	t.Parallel()

	client := MCPAuthClientCheck("client-1", MCPAuthClientDimensions{ClientType: AuthClientTypeServiceAccount, ProjectID: "project-1", ConnectionOwner: "", UpstreamClient: ""})
	inferred := heldBy(precedenceUser, Grant{PrincipalUrn: "", Scope: ScopeMCPAuth, Selector: AuthClientSelector("client-1", ""), Inferred: true})
	block := heldBy(precedenceRole, Grant{PrincipalUrn: "", Scope: ScopeMCPBlockedAuth, Selector: AuthClientSelector("client-1", ""), Inferred: false})

	allowed, err := GrantsAuthorize([]Grant{inferred}, client)
	require.NoError(t, err)
	require.True(t, allowed, "an inferred grant allows like any other")

	allowed, err = GrantsAuthorize([]Grant{inferred, block}, client)
	require.NoError(t, err)
	require.False(t, allowed, "an inferred grant yields to an inherited block")

	explicit := inferred
	explicit.Inferred = false
	allowed, err = GrantsAuthorize([]Grant{explicit, block}, client)
	require.NoError(t, err)
	require.True(t, allowed, "the same grant written by name outranks the block")
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

	allowed, err := GrantsAuthorize(grants, MCPServer(MCPServerIDFrom("server-1"), ProjectIDFrom("project-1")).ToolCall("list", DispositionReadOnly))
	require.NoError(t, err)
	require.True(t, allowed)

	allowed, err = GrantsAuthorize(grants, MCPServer(MCPServerIDFrom("server-1"), ProjectIDFrom("project-1")).ToolCall("unannotated", ""))
	require.NoError(t, err)
	require.False(t, allowed, "a tool with no derivable disposition is outside a disposition-scoped grant")
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
	check := MCPServer(MCPServerIDFrom("server-1"), ProjectIDFrom("project-1")).Connect()

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

func TestEitherPrincipalPrecedence(t *testing.T) {
	t.Parallel()

	engine := NewEngine(testenv.NewLogger(t), nil, staticChallengeLogging(false), workos.NewStubClient())
	checks := []Check{
		{Scope: ScopeSkillRead, ResourceID: "project-one", Dimensions: nil},
		{Scope: ScopeSkillRead, ResourceID: "skill-one", Dimensions: nil},
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
			err := engine.Require(ctx, Either(checks...))
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

func TestExplainGrantCheckMatchesPrincipalPrecedence(t *testing.T) {
	t.Parallel()

	for _, tc := range principalPrecedenceCases() {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			explanation := requireConsistentExplanation(t, tc.grants, tc.check)
			require.Equal(t, tc.allowed, explanation.Allowed)
		})
	}
}

func TestExplainGrantCheckMatchesGrantsAuthorizeForRandomGrantSets(t *testing.T) {
	t.Parallel()

	principals := []string{precedenceUser, precedenceRole, "role:organization:role-2", precedenceEveryone, precedenceAgent}
	scopes := []Scope{ScopeMCPConnect, ScopeMCPRead, ScopeMCPWrite, ScopeMCPBlockedConnect, ScopeMCPBlockedRead, ScopeMCPBlockedWrite}
	selectors := []func(Scope) Selector{
		func(scope Scope) Selector { return NewSelector(scope, "server-1") },
		func(scope Scope) Selector { return NewSelector(scope, "server-2") },
		func(scope Scope) Selector { return NewSelector(scope, WildcardResource) },
		func(scope Scope) Selector {
			selector := NewSelector(scope, WildcardResource)
			selector[SelectorKeyProjectID] = "project-1"
			return selector
		},
		func(scope Scope) Selector {
			selector := NewSelector(scope, "server-1")
			selector[SelectorKeyTool] = "tool-a"
			return selector
		},
		func(scope Scope) Selector {
			selector := NewSelector(scope, "server-1")
			selector[SelectorKeyDisposition] = DispositionDestructive
			return selector
		},
		func(scope Scope) Selector {
			selector := NewSelector(scope, "server-1")
			selector[SelectorKeyDisposition] = DispositionReadOnly
			return selector
		},
	}
	checks := []Check{
		MCPServer(MCPServerIDFrom("server-1"), ProjectIDFrom("project-1")).Connect(),
		MCPServer(MCPServerIDFrom("server-1"), ProjectIDFrom("project-1")).Read(),
		MCPServer(MCPServerIDFrom("server-1"), ProjectIDFrom("project-1")).Write(),
		MCPServer(MCPServerIDFrom("server-1"), ProjectIDFrom("project-1")).ToolCall("tool-a", DispositionReadOnly),
		MCPServer(MCPServerIDFrom("server-1"), ProjectIDFrom("project-1")).ToolCall("tool-b", DispositionDestructive),
		MCPServer(MCPServerIDFrom("server-1"), ProjectIDFrom("project-1")).ToolCall("tool-c", ""),
	}

	// A fixed seed keeps failures reproducible while still covering far more
	// combinations than the hand-written fixtures.
	rng := rand.New(rand.NewPCG(390, 2026))
	const iterations = 2000
	for range iterations {
		grants := make([]Grant, rng.IntN(6))
		for i := range grants {
			scope := scopes[rng.IntN(len(scopes))]
			grants[i] = heldBy(principals[rng.IntN(len(principals))], NewGrantWithSelector(scope, selectors[rng.IntN(len(selectors))](scope)))
		}
		for _, check := range checks {
			requireConsistentExplanation(t, grants, check)
		}
	}
}

func TestExplainGrantCheckBlockFromAnotherRoleWins(t *testing.T) {
	t.Parallel()

	grants := []Grant{
		heldBy(precedenceRole, NewGrant(ScopeMCPConnect, "server-1")),
		heldBy("role:organization:role-2", NewGrant(ScopeMCPBlockedConnect, "server-1")),
	}

	explanation := requireConsistentExplanation(t, grants, MCPServer(MCPServerIDFrom("server-1"), ProjectIDFrom("project-1")).Connect())
	require.False(t, explanation.Allowed)
	require.Equal(t, []explainedGrant{
		{principal: precedenceRole, scope: ScopeMCPConnect, effect: GrantEffectBlocked, reason: BlockedReasonNone},
		{principal: "role:organization:role-2", scope: ScopeMCPBlockedConnect, effect: GrantEffectBlocks, reason: BlockedReasonNone},
	}, explainedGrants(explanation))
}

func TestExplainGrantCheckDirectGrantOutranksRoleAndEveryoneBlocks(t *testing.T) {
	t.Parallel()

	grants := []Grant{
		heldBy(precedenceRole, NewGrant(ScopeMCPConnect, WildcardResource)),
		heldBy(precedenceRole, NewGrant(ScopeMCPBlockedConnect, "server-1")),
		heldBy(precedenceEveryone, NewGrant(ScopeMCPBlockedConnect, "server-1")),
		heldBy(precedenceUser, NewGrant(ScopeMCPConnect, "server-1")),
	}

	explanation := requireConsistentExplanation(t, grants, MCPServer(MCPServerIDFrom("server-1"), ProjectIDFrom("project-1")).Connect())
	require.True(t, explanation.Allowed)
	require.Equal(t, []explainedGrant{
		{principal: precedenceRole, scope: ScopeMCPConnect, effect: GrantEffectBlocked, reason: BlockedReasonNone},
		{principal: precedenceUser, scope: ScopeMCPConnect, effect: GrantEffectOverrides, reason: BlockedReasonNone},
		{principal: precedenceRole, scope: ScopeMCPBlockedConnect, effect: GrantEffectOverridden, reason: BlockedReasonNone},
		{principal: precedenceEveryone, scope: ScopeMCPBlockedConnect, effect: GrantEffectOverridden, reason: BlockedReasonNone},
	}, explainedGrants(explanation))
}

func TestExplainGrantCheckOwnBlockBeatsOwnGrant(t *testing.T) {
	t.Parallel()

	grants := []Grant{
		heldBy(precedenceUser, NewGrant(ScopeMCPConnect, "server-1")),
		heldBy(precedenceUser, NewGrant(ScopeMCPBlockedConnect, "server-1")),
	}

	explanation := requireConsistentExplanation(t, grants, MCPServer(MCPServerIDFrom("server-1"), ProjectIDFrom("project-1")).Connect())
	require.False(t, explanation.Allowed)
	require.Equal(t, []explainedGrant{
		{principal: precedenceUser, scope: ScopeMCPConnect, effect: GrantEffectBlocked, reason: BlockedReasonOwnExclusion},
		{principal: precedenceUser, scope: ScopeMCPBlockedConnect, effect: GrantEffectBlocks, reason: BlockedReasonNone},
	}, explainedGrants(explanation))
}

func TestExplainGrantCheckWildcardDirectGrantDoesNotOutrankRoleBlock(t *testing.T) {
	t.Parallel()

	grants := []Grant{
		heldBy(precedenceRole, NewGrant(ScopeMCPBlockedConnect, "server-1")),
		heldBy(precedenceUser, NewGrant(ScopeMCPConnect, WildcardResource)),
	}

	explanation := requireConsistentExplanation(t, grants, MCPServer(MCPServerIDFrom("server-1"), ProjectIDFrom("project-1")).Connect())
	require.False(t, explanation.Allowed)
	require.Equal(t, []explainedGrant{
		{principal: precedenceUser, scope: ScopeMCPConnect, effect: GrantEffectBlocked, reason: BlockedReasonWildcardDirectGrant},
		{principal: precedenceRole, scope: ScopeMCPBlockedConnect, effect: GrantEffectBlocks, reason: BlockedReasonNone},
	}, explainedGrants(explanation))
}

func TestExplainGrantCheckOrganizationWideGrantAllows(t *testing.T) {
	t.Parallel()

	grants := []Grant{
		heldBy(precedenceEveryone, NewGrant(ScopeMCPConnect, WildcardResource)),
		heldBy(precedenceRole, NewGrant(ScopeMCPRead, "server-2")),
	}

	explanation := requireConsistentExplanation(t, grants, MCPServer(MCPServerIDFrom("server-1"), ProjectIDFrom("project-1")).Connect())
	require.True(t, explanation.Allowed)
	require.Equal(t, []explainedGrant{
		{principal: precedenceEveryone, scope: ScopeMCPConnect, effect: GrantEffectAllows, reason: BlockedReasonNone},
	}, explainedGrants(explanation), "grants for other servers do not take part")
}

func TestExplainGrantCheckScopeExpansionAllows(t *testing.T) {
	t.Parallel()

	grants := []Grant{
		heldBy(precedenceRole, NewGrant(ScopeMCPWrite, "server-1")),
	}

	for _, scope := range []Scope{ScopeMCPRead, ScopeMCPWrite} {
		explanation := requireConsistentExplanation(t, grants, mcpCheck(scope, "server-1", "project-1"))
		require.True(t, explanation.Allowed, scope)
		require.Equal(t, []explainedGrant{
			{principal: precedenceRole, scope: ScopeMCPWrite, effect: GrantEffectAllows, reason: BlockedReasonNone},
		}, explainedGrants(explanation), scope)
	}

	// mcp:write does not reach mcp:connect.
	explanation := requireConsistentExplanation(t, grants, MCPServer(MCPServerIDFrom("server-1"), ProjectIDFrom("project-1")).Connect())
	require.False(t, explanation.Allowed)
	require.Empty(t, explainedGrants(explanation))
}

func TestExplainGrantCheckNoMatchingGrant(t *testing.T) {
	t.Parallel()

	explanation := requireConsistentExplanation(t, nil, MCPServer(MCPServerIDFrom("server-1"), ProjectIDFrom("project-1")).Connect())
	require.False(t, explanation.Allowed)
	require.Empty(t, explanation.Contributions)
}

func TestExplainGrantCheckDispositionDirectGrantDoesNotReachUnannotatedTool(t *testing.T) {
	t.Parallel()

	direct := NewSelector(ScopeMCPConnect, "server-1")
	direct[SelectorKeyDisposition] = DispositionReadOnly
	grants := []Grant{
		heldBy(precedenceRole, NewGrant(ScopeMCPBlockedConnect, "server-1")),
		heldBy(precedenceUser, NewGrantWithSelector(ScopeMCPConnect, direct)),
	}

	// The call carries no disposition, so the disposition-limited direct
	// grant does not cover it at all and only the block is explained.
	explanation := requireConsistentExplanation(t, grants, MCPServer(MCPServerIDFrom("server-1"), ProjectIDFrom("project-1")).ToolCall("unannotated", ""))
	require.False(t, explanation.Allowed)
	require.Equal(t, []explainedGrant{
		{principal: precedenceRole, scope: ScopeMCPBlockedConnect, effect: GrantEffectBlocks, reason: BlockedReasonNone},
	}, explainedGrants(explanation))
}

func TestExplainGrantCheckToolBlockDoesNotReachServerCheck(t *testing.T) {
	t.Parallel()

	blocked := NewSelector(ScopeMCPBlockedConnect, "server-1")
	blocked[SelectorKeyDisposition] = DispositionDestructive
	grants := []Grant{
		heldBy(precedenceRole, NewGrant(ScopeMCPConnect, "server-1")),
		heldBy(precedenceRole, NewGrantWithSelector(ScopeMCPBlockedConnect, blocked)),
	}

	server := requireConsistentExplanation(t, grants, MCPServer(MCPServerIDFrom("server-1"), ProjectIDFrom("project-1")).Connect())
	require.True(t, server.Allowed)
	require.Equal(t, []explainedGrant{
		{principal: precedenceRole, scope: ScopeMCPConnect, effect: GrantEffectAllows, reason: BlockedReasonNone},
	}, explainedGrants(server))

	tool := requireConsistentExplanation(t, grants, MCPServer(MCPServerIDFrom("server-1"), ProjectIDFrom("project-1")).ToolCall("delete", DispositionDestructive))
	require.False(t, tool.Allowed)
	require.Equal(t, []explainedGrant{
		{principal: precedenceRole, scope: ScopeMCPConnect, effect: GrantEffectBlocked, reason: BlockedReasonNone},
		{principal: precedenceRole, scope: ScopeMCPBlockedConnect, effect: GrantEffectBlocks, reason: BlockedReasonNone},
	}, explainedGrants(tool))
}

func TestExplainGrantCheckRejectsWildcardResource(t *testing.T) {
	t.Parallel()

	_, err := ExplainGrantCheck(nil, mcpCheck(ScopeMCPConnect, WildcardResource, ""))
	require.Error(t, err)
}

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
	serverCheck := MCPServer(MCPServerIDFrom("server-1"), ProjectIDFrom("project-1")).Connect()
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
			name: "direct write grant does not imply connect past a role block",
			grants: []Grant{
				heldBy(precedenceRole, NewGrant(ScopeMCPConnect, WildcardResource)),
				heldBy(precedenceRole, NewGrant(ScopeMCPBlockedConnect, "server-1")),
				heldBy(precedenceUser, NewGrant(ScopeMCPWrite, "server-1")),
			},
			check:   serverCheck,
			allowed: false,
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
			check:   MCPServer(MCPServerIDFrom("server-1"), ProjectIDFrom("project-1")).Read(),
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

// explainedGrant is the comparable shape of one contribution.
type explainedGrant struct {
	principal string
	scope     Scope
	effect    GrantEffect
	reason    BlockedReason
}

func explainedGrants(explanation GrantCheckExplanation) []explainedGrant {
	out := make([]explainedGrant, 0, len(explanation.Contributions))
	for _, contribution := range explanation.Contributions {
		out = append(out, explainedGrant{
			principal: contribution.Grant.PrincipalUrn,
			scope:     contribution.Grant.Scope,
			effect:    contribution.Effect,
			reason:    contribution.Reason,
		})
	}
	return out
}

// requireConsistentExplanation asserts the explanation agrees with runtime
// enforcement and that its contributions account for the decision.
func requireConsistentExplanation(t *testing.T, grants []Grant, check Check) GrantCheckExplanation {
	t.Helper()

	allowed, err := GrantsAuthorize(grants, check)
	require.NoError(t, err)
	explanation, err := ExplainGrantCheck(grants, check)
	require.NoError(t, err)
	require.Equal(t, allowed, explanation.Allowed, "explanation decision must match GrantsAuthorize")

	proving := 0
	for _, contribution := range explanation.Contributions {
		if contribution.Effect == GrantEffectAllows || contribution.Effect == GrantEffectOverrides {
			proving++
		}
	}
	if allowed {
		require.Positive(t, proving, "an allowed decision names the grant proving it")
	} else {
		require.Zero(t, proving, "a denied decision names no proving grant")
	}
	return explanation
}
