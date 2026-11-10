package authz

import (
	"math/rand/v2"
	"testing"

	"github.com/stretchr/testify/require"
)

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
		MCPCheck(ScopeMCPConnect, "server-1", "project-1"),
		MCPCheck(ScopeMCPRead, "server-1", "project-1"),
		MCPCheck(ScopeMCPWrite, "server-1", "project-1"),
		MCPToolCallCheck("server-1", MCPToolCallDimensions{Tool: "tool-a", Disposition: DispositionReadOnly, ProjectID: "project-1"}),
		MCPToolCallCheck("server-1", MCPToolCallDimensions{Tool: "tool-b", Disposition: DispositionDestructive, ProjectID: "project-1"}),
		MCPToolCallCheck("server-1", MCPToolCallDimensions{Tool: "tool-c", Disposition: "", ProjectID: "project-1"}),
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

	explanation := requireConsistentExplanation(t, grants, MCPCheck(ScopeMCPConnect, "server-1", "project-1"))
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

	explanation := requireConsistentExplanation(t, grants, MCPCheck(ScopeMCPConnect, "server-1", "project-1"))
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

	explanation := requireConsistentExplanation(t, grants, MCPCheck(ScopeMCPConnect, "server-1", "project-1"))
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

	explanation := requireConsistentExplanation(t, grants, MCPCheck(ScopeMCPConnect, "server-1", "project-1"))
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

	explanation := requireConsistentExplanation(t, grants, MCPCheck(ScopeMCPConnect, "server-1", "project-1"))
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

	for _, scope := range []Scope{ScopeMCPConnect, ScopeMCPRead, ScopeMCPWrite} {
		explanation := requireConsistentExplanation(t, grants, MCPCheck(scope, "server-1", "project-1"))
		require.True(t, explanation.Allowed, scope)
		require.Equal(t, []explainedGrant{
			{principal: precedenceRole, scope: ScopeMCPWrite, effect: GrantEffectAllows, reason: BlockedReasonNone},
		}, explainedGrants(explanation), scope)
	}
}

func TestExplainGrantCheckNoMatchingGrant(t *testing.T) {
	t.Parallel()

	explanation := requireConsistentExplanation(t, nil, MCPCheck(ScopeMCPConnect, "server-1", "project-1"))
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

	explanation := requireConsistentExplanation(t, grants, MCPToolCallCheck("server-1", MCPToolCallDimensions{Tool: "unannotated", Disposition: "", ProjectID: "project-1"}))
	require.False(t, explanation.Allowed)
	require.Equal(t, []explainedGrant{
		{principal: precedenceUser, scope: ScopeMCPConnect, effect: GrantEffectBlocked, reason: BlockedReasonNarrowerDirectGrant},
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

	server := requireConsistentExplanation(t, grants, MCPCheck(ScopeMCPConnect, "server-1", "project-1"))
	require.True(t, server.Allowed)
	require.Equal(t, []explainedGrant{
		{principal: precedenceRole, scope: ScopeMCPConnect, effect: GrantEffectAllows, reason: BlockedReasonNone},
	}, explainedGrants(server))

	tool := requireConsistentExplanation(t, grants, MCPToolCallCheck("server-1", MCPToolCallDimensions{Tool: "delete", Disposition: DispositionDestructive, ProjectID: "project-1"}))
	require.False(t, tool.Allowed)
	require.Equal(t, []explainedGrant{
		{principal: precedenceRole, scope: ScopeMCPConnect, effect: GrantEffectBlocked, reason: BlockedReasonNone},
		{principal: precedenceRole, scope: ScopeMCPBlockedConnect, effect: GrantEffectBlocks, reason: BlockedReasonNone},
	}, explainedGrants(tool))
}

func TestExplainGrantCheckRejectsWildcardResource(t *testing.T) {
	t.Parallel()

	_, err := ExplainGrantCheck(nil, MCPCheck(ScopeMCPConnect, WildcardResource, ""))
	require.Error(t, err)
}
