package authz

import (
	"errors"
	"fmt"
	"maps"
	"slices"
	"testing"

	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/workos"
)

// These tests build every small world of MCP grants and ask the real
// evaluator (GrantsAuthorize) every check that world can answer, then assert
// properties that must hold whatever the grants are. Nothing here models the
// rules a second time, so the tests cannot drift from the code: they only
// compare the evaluator's answers with each other.
//
// The world is small on purpose. Mistakes in rules like these show up with two
// of everything: one project and another, one server and another, a user, a
// role and everyone, and up to two grants at a time. Every grant that
// ValidateSelector would refuse is left out, so the world is exactly the set
// of grants that can be stored.
//
// P numbers follow the typed checks page (step 5). A to D are the gaps added
// after it: agents and several policies (A), any-of requests (B), precedence
// (C) and every API giving the same answer (D). The engine tests (B, D) run on
// grants with no tool or disposition limit to keep them fast. Not covered
// here: how agent policies are stored and delegated (runtimepolicy) and
// organization isolation, which the grant loader enforces before the
// evaluator runs.

func TestGrantInvariants_WorldSize(t *testing.T) {
	t.Parallel()
	storable := storableGrants()
	sets := 0
	forEachGrantSet(storable, func([]Grant) { sets++ })
	t.Logf("%d storable grants, %d grant sets, %d checks", len(storable), sets, len(everyCheck()))
	if len(storable) == 0 {
		t.Fatal("the world has no storable grants")
	}
}

// P1: no grants means no access.
func TestGrantInvariants_NoGrantsNoAccess(t *testing.T) {
	t.Parallel()
	for _, c := range everyCheck() {
		if isAllowed(t, nil, c.check) {
			t.Errorf("%s on %s allowed with no grants", c.name, c.server.id)
		}
	}
}

// P2: adding an allow grant never removes access.
// P3: adding a block never gives access.
func TestGrantInvariants_AddingGrantsIsMonotonic(t *testing.T) {
	t.Parallel()
	storable := storableGrants()
	checks := everyCheck()
	allowRemoved := &failureReport{t: t}
	blockGranted := &failureReport{t: t}
	forEachGrantSet(storable, func(grants []Grant) {
		if len(grants) == 0 {
			return
		}
		for i, added := range grants {
			before := slices.Delete(slices.Clone(grants), i, i+1)
			for _, c := range checks {
				was := isAllowed(t, before, c.check)
				now := isAllowed(t, grants, c.check)
				switch {
				case IsBlocklistScope(added.Scope) && !was && now:
					blockGranted.add("P3: adding block %s to [%s] allowed %s on %s", describeGrant(added), describeGrants(before), c.name, c.server.id)
				case !IsBlocklistScope(added.Scope) && was && !now:
					allowRemoved.add("P2: adding %s to [%s] removed %s on %s", describeGrant(added), describeGrants(before), c.name, c.server.id)
				}
			}
		}
	})
	allowRemoved.done()
	blockGranted.done()
}

// P4: a grant limited to project A never satisfies a check in project B.
func TestGrantInvariants_ProjectIsolation(t *testing.T) {
	t.Parallel()
	failures := &failureReport{t: t}
	inB := checksOnServer(mcpServers[2])
	forEachGrantSet(storableGrants(), func(grants []Grant) {
		for _, g := range grants {
			if !IsBlocklistScope(g.Scope) && g.Selector[SelectorKeyProjectID] != projectA {
				return
			}
		}
		for _, c := range inB {
			if isAllowed(t, grants, c.check) {
				failures.add("P4: [%s] allowed %s on %s in project B", describeGrants(grants), c.name, c.server.id)
			}
		}
	})
	failures.done()
}

// P5: with no blocks, a grant set that allows a scope on a server also allows
// every scope that scope implies on it.
func TestGrantInvariants_ImplicationHolds(t *testing.T) {
	t.Parallel()
	failures := &failureReport{t: t}
	forEachGrantSet(storableGrants(), func(grants []Grant) {
		if slices.ContainsFunc(grants, func(g Grant) bool { return IsBlocklistScope(g.Scope) }) {
			return
		}
		for _, server := range mcpServers {
			serverChecks := checksOnServer(server)[:3]
			byScope := func(scope Scope) (namedCheck, bool) {
				i := slices.IndexFunc(serverChecks, func(c namedCheck) bool { return c.check.Scope == scope })
				if i < 0 {
					return namedCheck{}, false
				}
				return serverChecks[i], true
			}
			for lower, higherScopes := range scopeExpansions {
				lowerCheck, ok := byScope(lower)
				if !ok {
					continue
				}
				for _, higher := range higherScopes {
					higherCheck, ok := byScope(higher)
					if !ok {
						continue
					}
					if isAllowed(t, grants, higherCheck.check) && !isAllowed(t, grants, lowerCheck.check) {
						failures.add("P5: [%s] allows %s on %s but not %s", describeGrants(grants), higherCheck.name, server.id, lowerCheck.name)
					}
				}
			}
		}
	})
	failures.done()
}

// P6: widening an allow grant never removes access. The one expected
// exception is P7's: a grant made to the user by name for one server outranks
// an inherited block, and widening it to every server gives that up.
func TestGrantInvariants_WideningNeverRemovesAccess(t *testing.T) {
	t.Parallel()
	failures := &failureReport{t: t}
	checks := everyCheck()
	forEachGrantSet(storableGrants(), func(grants []Grant) {
		hasBlock := slices.ContainsFunc(grants, func(g Grant) bool { return IsBlocklistScope(g.Scope) })
		for i, g := range grants {
			if IsBlocklistScope(g.Scope) {
				continue
			}
			outranksBlocks := hasBlock && IsDirectGrant(g) && g.Selector.ResourceID() != WildcardResource
			for _, wide := range widenings(g) {
				if outranksBlocks && wide.Selector.ResourceID() == WildcardResource {
					continue
				}
				widened := slices.Clone(grants)
				widened[i] = wide
				for _, c := range checks {
					if isAllowed(t, grants, c.check) && !isAllowed(t, widened, c.check) {
						failures.add("P6: widening %s to %s in [%s] removed %s on %s", describeGrant(g), describeGrant(wide), describeGrants(grants), c.name, c.server.id)
					}
				}
			}
		}
	})
	failures.done()
}

// P7: a block wins over every allow, with one exception: a grant made to the
// user by name for that one server outranks a block that reaches them through
// a role or everyone. The user's own block always wins.
func TestGrantInvariants_BlockWins(t *testing.T) {
	t.Parallel()
	failures := &failureReport{t: t}
	checks := everyCheck()
	forEachGrantSet(storableGrants(), func(grants []Grant) {
		for _, c := range checks {
			blockScope, ok := ExclusionScopeFor(c.check.Scope)
			if !ok {
				continue
			}
			namesServer := slices.ContainsFunc(grants, func(g Grant) bool {
				return !IsBlocklistScope(g.Scope) && IsDirectGrant(g) && g.Selector.ResourceID() == c.server.id
			})
			for _, holder := range allPrincipals {
				block := NewGrant(blockScope, c.server.id)
				block.PrincipalUrn = holder
				allowed := isAllowed(t, append(slices.Clone(grants), block), c.check)
				if !allowed {
					continue
				}
				if holder == principalUser || !namesServer {
					failures.add("P7: [%s] plus %s still allows %s on %s", describeGrants(grants), describeGrant(block), c.name, c.server.id)
				}
			}
		}
	})
	failures.done()
}

// P8: absence never grants. Leaving the tool or disposition out of a tool call
// never turns a refusal into an allow.
func TestGrantInvariants_AbsentValueNeverGrants(t *testing.T) {
	t.Parallel()
	failures := &failureReport{t: t}
	forEachGrantSet(storableGrants(), func(grants []Grant) {
		for _, server := range mcpServers {
			ref := MCPServer(MCPServerIDFrom(server.id), ProjectIDFrom(server.project))
			for _, tool := range toolNames {
				for _, disposition := range allDispositions {
					full := isAllowed(t, grants, ref.ToolCall(tool, disposition))
					if full {
						continue
					}
					if isAllowed(t, grants, ref.ToolCall(tool, "")) {
						failures.add("P8: [%s] refuses %s (%s) on %s but allows it with no disposition", describeGrants(grants), tool, disposition, server.id)
					}
					if isAllowed(t, grants, ref.ToolCall("", disposition)) {
						failures.add("P8: [%s] refuses %s (%s) on %s but allows it with no tool", describeGrants(grants), tool, disposition, server.id)
					}
				}
			}
		}
	})
	failures.done()
}

// P9: a grant limited to a tool or a disposition never allows a call to
// another tool or a call with another disposition.
func TestGrantInvariants_LimitsAreRespected(t *testing.T) {
	t.Parallel()
	failures := &failureReport{t: t}
	for _, g := range storableGrants() {
		if IsBlocklistScope(g.Scope) {
			continue
		}
		limitTool, hasTool := g.Selector[SelectorKeyTool]
		limitDisposition, hasDisposition := g.Selector[SelectorKeyDisposition]
		if !hasTool && !hasDisposition {
			continue
		}
		for _, server := range mcpServers {
			ref := MCPServer(MCPServerIDFrom(server.id), ProjectIDFrom(server.project))
			for _, tool := range toolNames {
				for _, disposition := range append([]string{""}, allDispositions...) {
					outside := (hasTool && tool != limitTool) || (hasDisposition && disposition != limitDisposition)
					if outside && isAllowed(t, []Grant{g}, ref.ToolCall(tool, disposition)) {
						failures.add("P9: %s allows calling %s (%s) on %s", describeGrant(g), tool, disposition, server.id)
					}
				}
			}
		}
	}
	failures.done()
}

// P10 and P15: a check without a server id or without a project id is refused
// with ErrInvalidCheck, whatever the grants.
func TestGrantInvariants_InvalidChecksAreRefused(t *testing.T) {
	t.Parallel()
	failures := &failureReport{t: t}
	invalid := map[string]Check{
		"read with no server id":    MCPServer(MCPServerIDFrom(""), ProjectIDFrom(projectA)).Read(),
		"connect with no server id": MCPServer(MCPServerIDFrom(""), ProjectIDFrom(projectA)).Connect(),
		"call with no server id":    MCPServer(MCPServerIDFrom(""), ProjectIDFrom(projectA)).ToolCall("t1", DispositionReadOnly),
		"read with no project id":   MCPServer(MCPServerIDFrom("s1"), ProjectIDFrom("")).Read(),
		"connect with no project":   MCPServer(MCPServerIDFrom("s1"), ProjectIDFrom("")).Connect(),
		"call with no project id":   MCPServer(MCPServerIDFrom("s1"), ProjectIDFrom("")).ToolCall("t1", DispositionReadOnly),
	}
	forEachGrantSet(storableGrants(), func(grants []Grant) {
		for name, check := range invalid {
			answer := authorize(grants, check)
			if answer.allowed || !errors.Is(answer.err, ErrInvalidCheck) {
				failures.add("P10: [%s] answered %s with allowed=%v err=%v", describeGrants(grants), name, answer.allowed, answer.err)
			}
		}
	})
	failures.done()
}

// P11: the order of the grants never changes the answer.
func TestGrantInvariants_OrderDoesNotMatter(t *testing.T) {
	t.Parallel()
	failures := &failureReport{t: t}
	checks := everyCheck()
	forEachGrantSet(storableGrants(), func(grants []Grant) {
		if len(grants) < 2 {
			return
		}
		reversed := slices.Clone(grants)
		slices.Reverse(reversed)
		for _, c := range checks {
			a, b := authorize(grants, c.check), authorize(reversed, c.check)
			if a.allowed != b.allowed || (a.err == nil) != (b.err == nil) {
				failures.add("P11: [%s] answers %s on %s differently when reversed", describeGrants(grants), c.name, c.server.id)
			}
		}
	})
	failures.done()
}

// A1: a request that carries several policies, such as an agent's credential,
// the agent and its owner, is allowed only what every policy allows on its
// own. A block in any one of them takes the check away.
func TestGrantInvariants_EveryPolicyMustAllow(t *testing.T) {
	t.Parallel()
	failures := &failureReport{t: t}
	checks := everyCheck()
	policies := append([][]Grant{nil}, singleGrantPolicies(userGrants(storableGrants()))...)
	for _, first := range policies {
		for _, second := range policies {
			both := grantAuthorization{policies: [][]Grant{first, second}, representedUser: "", representedPolicies: nil, legacyAuthCeiling: false}
			for _, c := range checks {
				want := isAllowed(t, first, c.check) && isAllowed(t, second, c.check)
				if got := policiesAllow(t, both, c.check); got != want {
					failures.add("A1: policies [%s] and [%s] answer %s on %s with %v, each alone says %v", describeGrants(first), describeGrants(second), c.name, c.server.id, got, want)
				}
			}
		}
	}
	failures.done()
}

// A2: an agent never gets more than the user would with the same grants, and
// an agent's grant never outranks a block from a role or everyone, even when it
// names the server. An agent's owner can write its policy without being an
// administrator, so letting it outrank a block would let owners lift one.
func TestGrantInvariants_AgentGrantsNeverOutrankBlocks(t *testing.T) {
	t.Parallel()
	failures := &failureReport{t: t}
	checks := everyCheck()
	forEachGrantSet(storableGrants(), func(grants []Grant) {
		asAgent := slices.Clone(grants)
		for i := range asAgent {
			if asAgent[i].PrincipalUrn == principalUser {
				asAgent[i].PrincipalUrn = principalAgent
			}
		}
		for _, c := range checks {
			if isAllowed(t, asAgent, c.check) && !isAllowed(t, grants, c.check) {
				failures.add("A2: [%s] allows %s on %s for the agent but not for the user", describeGrants(asAgent), c.name, c.server.id)
			}
			blockScope, ok := ExclusionScopeFor(c.check.Scope)
			if !ok {
				continue
			}
			for _, holder := range []string{principalRole, principalEveryone} {
				block := NewGrant(blockScope, c.server.id)
				block.PrincipalUrn = holder
				if isAllowed(t, append(slices.Clone(asAgent), block), c.check) {
					failures.add("A2: agent grants [%s] outrank %s for %s on %s", describeGrants(asAgent), describeGrant(block), c.name, c.server.id)
				}
			}
		}
	})
	failures.done()
}

// A3: a grant on personal auth clients for "$represented_user" covers only the
// connection of the user the request acts for. A request that acts for nobody
// covers no personal connection, and a binding the caller sets is replaced.
// An agent acting for a user needs that user's own policy to allow it too.
func TestGrantInvariants_PersonalConnectionsBindToRepresentedUser(t *testing.T) {
	t.Parallel()
	personal := NewGrantWithSelector(ScopeMCPAuth, PersonalAuthClientSelector())
	personal.PrincipalUrn = principalEveryone
	for _, represented := range []string{"", "u1", "u2"} {
		for _, owner := range []string{"u1", "u2"} {
			for _, callerBinding := range []string{"", "u1", "u2"} {
				for _, userAllows := range []bool{true, false} {
					check := MCPAuthClientCheck("client1", MCPAuthClientDimensions{ClientType: AuthClientTypeRemoteSession, ProjectID: projectA, ConnectionOwner: owner, UpstreamClient: "upstream1"})
					check.representedUser = callerBinding
					authorization := grantAuthorization{policies: [][]Grant{{personal}}, representedUser: represented, representedPolicies: nil, legacyAuthCeiling: false}
					if represented != "" {
						userPolicy := []Grant{}
						if userAllows {
							userPolicy = []Grant{personal}
						}
						authorization.representedPolicies = [][]Grant{userPolicy}
					}
					want := represented != "" && owner == represented && userAllows
					if got := policiesAllow(t, authorization, check); got != want {
						t.Errorf("A3: acting for %q (user policy allows: %v), caller binding %q, connection of %q: got %v, want %v", represented, userAllows, callerBinding, owner, got, want)
					}
				}
			}
		}
	}
}

// B1: RequireAny allows when one alternative is allowed on its own, and
// refuses when none is. A block on one alternative only takes that one away.
// B2: the order of the alternatives never changes the answer.
// B3: Require(AnyOf(...)) and Can(AnyOf(...)) answer as RequireAnyUnblocked.
func TestGrantInvariants_AnyOf(t *testing.T) {
	t.Parallel()
	engine := newInvariantEngine(t)
	failures := &failureReport{t: t}
	checks := serverChecks()
	forEachGrantSet(serverLevelGrants(), func(grants []Grant) {
		ctx := GrantsToContext(enterpriseSessionCtx(t), grants)
		alone := make([]bool, len(checks))
		for i, c := range checks {
			alone[i] = engineAllows(t, engine.Require(ctx, c.check))
		}
		for i, a := range checks {
			for j, b := range checks {
				if i == j {
					continue
				}
				anyAllowed := engineAllows(t, engine.RequireAny(ctx, a.check, b.check))
				if anyAllowed != (alone[i] || alone[j]) {
					failures.add("B1: [%s] RequireAny(%s on %s, %s on %s) = %v, alone %v and %v", describeGrants(grants), a.name, a.server.id, b.name, b.server.id, anyAllowed, alone[i], alone[j])
				}
				unblocked := engineAllows(t, engine.RequireAnyUnblocked(ctx, a.check, b.check))
				if unblocked != engineAllows(t, engine.RequireAnyUnblocked(ctx, b.check, a.check)) {
					failures.add("B2: [%s] RequireAnyUnblocked(%s on %s, %s on %s) depends on order", describeGrants(grants), a.name, a.server.id, b.name, b.server.id)
				}
				if engineAllows(t, engine.Require(ctx, AnyOf(a.check, b.check))) != unblocked {
					failures.add("B3: [%s] Require(AnyOf(%s on %s, %s on %s)) differs from RequireAnyUnblocked", describeGrants(grants), a.name, a.server.id, b.name, b.server.id)
				}
				can, err := engine.Can(ctx, AnyOf(a.check, b.check))
				if err != nil {
					t.Fatalf("Can(AnyOf) errored: %v", err)
				}
				if can != unblocked {
					failures.add("B3: [%s] Can(AnyOf(%s on %s, %s on %s)) = %v, RequireAnyUnblocked = %v", describeGrants(grants), a.name, a.server.id, b.name, b.server.id, can, unblocked)
				}
			}
		}
	})
	failures.done()
}

// B4: RequireAnyUnblocked refuses when a block covers any one alternative,
// even if the other alternative is allowed. The P7 exception still holds: a
// grant made to the user by name for that server outranks a block from a role
// or everyone.
func TestGrantInvariants_AnyOfUnblockedRefusesEveryBlock(t *testing.T) {
	t.Parallel()
	engine := newInvariantEngine(t)
	failures := &failureReport{t: t}
	checks := serverChecks()
	forEachGrantSet(serverLevelGrants(), func(grants []Grant) {
		for i, a := range checks {
			blockScope, _ := ExclusionScopeFor(a.check.Scope)
			namesServer := slices.ContainsFunc(grants, func(g Grant) bool {
				return !IsBlocklistScope(g.Scope) && IsDirectGrant(g) && g.Selector.ResourceID() == a.server.id
			})
			for _, holder := range allPrincipals {
				if holder != principalUser && namesServer {
					continue
				}
				block := NewGrant(blockScope, a.server.id)
				block.PrincipalUrn = holder
				ctx := GrantsToContext(enterpriseSessionCtx(t), append(slices.Clone(grants), block))
				for j, b := range checks {
					if i == j {
						continue
					}
					if engineAllows(t, engine.RequireAnyUnblocked(ctx, b.check, a.check)) {
						failures.add("B4: [%s] plus %s still allows RequireAnyUnblocked(%s on %s, %s on %s)", describeGrants(grants), describeGrant(block), b.name, b.server.id, a.name, a.server.id)
					}
				}
			}
		}
	})
	failures.done()
}

// C1: a grant made to the user by name for every server ("*") never outranks
// a block from a role or everyone. Only a grant naming the one server does
// (P7). Grants the default access backfill created (Inferred) never outrank a
// block either, even when they name the server.
func TestGrantInvariants_OnlyNamedServerGrantsOutrankBlocks(t *testing.T) {
	t.Parallel()
	failures := &failureReport{t: t}
	checks := everyCheck()
	forEachGrantSet(storableGrants(), func(grants []Grant) {
		inferred := slices.Clone(grants)
		for i := range inferred {
			inferred[i].Inferred = true
		}
		for _, c := range checks {
			blockScope, ok := ExclusionScopeFor(c.check.Scope)
			if !ok {
				continue
			}
			namesServer := slices.ContainsFunc(grants, func(g Grant) bool {
				return !IsBlocklistScope(g.Scope) && IsDirectGrant(g) && g.Selector.ResourceID() == c.server.id
			})
			for _, holder := range []string{principalRole, principalEveryone} {
				block := NewGrant(blockScope, c.server.id)
				block.PrincipalUrn = holder
				if !namesServer && isAllowed(t, append(slices.Clone(grants), block), c.check) {
					failures.add("C1: [%s] outranks %s for %s on %s without naming the server", describeGrants(grants), describeGrant(block), c.name, c.server.id)
				}
				if isAllowed(t, append(slices.Clone(inferred), block), c.check) {
					failures.add("C1: backfilled [%s] outranks %s for %s on %s", describeGrants(inferred), describeGrant(block), c.name, c.server.id)
				}
			}
		}
	})
	failures.done()
}

// C2: wider covers narrower and never the other way round. A root grant
// covers every server in every project, a grant on every server covers each
// one, and a grant naming one server covers no other.
func TestGrantInvariants_WiderCoversNarrower(t *testing.T) {
	t.Parallel()
	failures := &failureReport{t: t}
	root := NewGrant(ScopeRoot, WildcardResource)
	root.PrincipalUrn = principalUser
	for _, c := range everyCheck() {
		if !isAllowed(t, []Grant{root}, c.check) {
			failures.add("C2: root does not allow %s on %s", c.name, c.server.id)
		}
	}
	for _, g := range storableGrants() {
		if IsBlocklistScope(g.Scope) {
			continue
		}
		for _, c := range everyCheck() {
			if !isAllowed(t, []Grant{g}, c.check) {
				continue
			}
			if g.Selector.ResourceID() != WildcardResource && g.Selector.ResourceID() != c.server.id {
				failures.add("C2: %s allows %s on %s", describeGrant(g), c.name, c.server.id)
			}
			everyServer := g
			everyServer.Selector = maps.Clone(g.Selector)
			everyServer.Selector[SelectorKeyResourceID] = WildcardResource
			if !isAllowed(t, []Grant{everyServer}, c.check) {
				failures.add("C2: %s allows %s on %s but %s does not", describeGrant(g), c.name, c.server.id, describeGrant(everyServer))
			}
		}
	}
	failures.done()
}

// D1: every way of asking gives the same answer. Require, Evaluate, Can,
// Filter and FindMatched agree with GrantsAuthorize on every check.
func TestGrantInvariants_SameAnswerEverywhere(t *testing.T) {
	t.Parallel()
	engine := newInvariantEngine(t)
	failures := &failureReport{t: t}
	checks := everyCheck()
	all := make([]Check, len(checks))
	for i, c := range checks {
		all[i] = c.check
	}
	forEachGrantSet(serverLevelGrants(), func(grants []Grant) {
		ctx := GrantsToContext(enterpriseSessionCtx(t), grants)
		matched, err := engine.FindMatched(ctx, all)
		if err != nil {
			t.Fatalf("FindMatched errored: %v", err)
		}
		for i, c := range checks {
			want := isAllowed(t, grants, c.check)
			evaluated, err := engine.Evaluate(ctx, c.check)
			if err != nil {
				t.Fatalf("Evaluate errored: %v", err)
			}
			can, err := engine.Can(ctx, c.check)
			if err != nil {
				t.Fatalf("Can errored: %v", err)
			}
			filtered, err := engine.Filter(ctx, []Check{c.check})
			if err != nil {
				t.Fatalf("Filter errored: %v", err)
			}
			answers := map[string]bool{
				"Require":     engineAllows(t, engine.Require(ctx, c.check)),
				"Evaluate":    evaluated,
				"Can":         can,
				"Filter":      len(filtered) == 1,
				"FindMatched": matched[i],
			}
			for name, got := range answers {
				if got != want {
					failures.add("D1: [%s] %s says %v for %s on %s, GrantsAuthorize says %v", describeGrants(grants), name, got, c.name, c.server.id, want)
				}
			}
		}
	})
	failures.done()
}

// D2: a probe never claims access no concrete call has. A tool call probe that
// leaves out the tool or the disposition is allowed only when some call with
// those values filled in is allowed. The reverse need not hold: a block on one
// tool also covers a probe that names no tool.
func TestGrantInvariants_ProbesClaimNoMoreThanCalls(t *testing.T) {
	t.Parallel()
	failures := &failureReport{t: t}
	forEachGrantSet(storableGrants(), func(grants []Grant) {
		for _, server := range mcpServers {
			ref := MCPServer(MCPServerIDFrom(server.id), ProjectIDFrom(server.project))
			for _, tool := range append([]string{""}, toolNames...) {
				for _, disposition := range append([]string{""}, allDispositions...) {
					if tool != "" && disposition != "" {
						continue
					}
					if !isAllowed(t, grants, ref.ToolCall(tool, disposition).Probe()) {
						continue
					}
					if !someCallAllowed(t, grants, ref, tool, disposition) {
						failures.add("D2: [%s] allows a probe of %q (%q) on %s but no call", describeGrants(grants), tool, disposition, server.id)
					}
				}
			}
		}
	})
	failures.done()
}

const (
	principalUser     = "user:u1"
	principalRole     = "role:organization:r1"
	principalEveryone = "user:all"

	projectA = "p1"
	projectB = "p2"
)

var (
	allPrincipals   = []string{principalUser, principalRole, principalEveryone}
	mcpScopes       = []Scope{ScopeMCPRead, ScopeMCPWrite, ScopeMCPConnect, ScopeMCPBlockedRead, ScopeMCPBlockedWrite, ScopeMCPBlockedConnect}
	allDispositions = []string{DispositionReadOnly, DispositionDestructive, DispositionIdempotent, DispositionOpenWorld, DispositionUnannotated}
	toolNames       = []string{"t1", "t2"}
)

// mcpServer is one MCP server in the world and the project it belongs to.
type mcpServer struct {
	id      string
	project string
}

// Servers s1 and s2 are in project A, s3 is in project B.
var mcpServers = []mcpServer{{"s1", projectA}, {"s2", projectA}, {"s3", projectB}}

// namedCheck is a check together with the server and project it asks about.
type namedCheck struct {
	name   string
	server mcpServer
	check  Check
}

func checksOnServer(server mcpServer) []namedCheck {
	ref := MCPServer(MCPServerIDFrom(server.id), ProjectIDFrom(server.project))
	checks := []namedCheck{
		{name: "read", server: server, check: ref.Read()},
		{name: "write", server: server, check: ref.Write()},
		{name: "connect", server: server, check: ref.Connect()},
	}
	for _, tool := range toolNames {
		for _, disposition := range append([]string{""}, allDispositions...) {
			checks = append(checks, namedCheck{
				name:   fmt.Sprintf("call %s (%s)", tool, disposition),
				server: server,
				check:  ref.ToolCall(tool, disposition),
			})
		}
	}
	return checks
}

func everyCheck() []namedCheck {
	var checks []namedCheck
	for _, server := range mcpServers {
		checks = append(checks, checksOnServer(server)...)
	}
	return checks
}

// storableGrants is every storable grant in the world: each principal, scope,
// resource (s1 or every server), project limit (none or project A), tool limit
// and disposition limit that ValidateSelector accepts.
func storableGrants() []Grant {
	var storable []Grant
	for _, principal := range allPrincipals {
		for _, scope := range mcpScopes {
			for _, resource := range []string{"s1", WildcardResource} {
				for _, project := range []string{"", projectA} {
					for _, tool := range []string{"", "t1"} {
						for _, disposition := range append([]string{""}, allDispositions...) {
							selector := NewSelector(scope, resource)
							if project != "" {
								selector[SelectorKeyProjectID] = project
							}
							if tool != "" {
								selector[SelectorKeyTool] = tool
							}
							if disposition != "" {
								selector[SelectorKeyDisposition] = disposition
							}
							if ValidateSelector(scope, selector) != nil {
								continue
							}
							grant := NewGrantWithSelector(scope, selector)
							grant.PrincipalUrn = principal
							storable = append(storable, grant)
						}
					}
				}
			}
		}
	}
	return storable
}

// forEachGrantSet calls fn with the empty set, every single grant and every
// pair of distinct grants.
func forEachGrantSet(storable []Grant, fn func([]Grant)) {
	fn(nil)
	for i := range storable {
		fn([]Grant{storable[i]})
		for j := i + 1; j < len(storable); j++ {
			fn([]Grant{storable[i], storable[j]})
		}
	}
}

// evaluatorAnswer is the evaluator's whole answer: allowed only when there is no
// error.
type evaluatorAnswer struct {
	allowed bool
	err     error
}

func authorize(grants []Grant, check Check) evaluatorAnswer {
	allowed, err := GrantsAuthorize(grants, check)
	return evaluatorAnswer{allowed: allowed && err == nil, err: err}
}

// isAllowed asks a check the world builds, which is always valid, so any
// error fails the test instead of reading as a refusal.
func isAllowed(t *testing.T, grants []Grant, check Check) bool {
	t.Helper()
	answer := authorize(grants, check)
	if answer.err != nil {
		t.Fatalf("valid check %+v errored with [%s]: %v", check, describeGrants(grants), answer.err)
	}
	return answer.allowed
}

func describeGrant(g Grant) string {
	keys := slices.Sorted(maps.Keys(g.Selector))
	out := fmt.Sprintf("%s %s {", g.PrincipalUrn, g.Scope)
	for i, key := range keys {
		if i > 0 {
			out += " "
		}
		out += key + "=" + g.Selector[key]
	}
	return out + "}"
}

func describeGrants(grants []Grant) string {
	if len(grants) == 0 {
		return "no grants"
	}
	out := ""
	for i, g := range grants {
		if i > 0 {
			out += ", "
		}
		out += describeGrant(g)
	}
	return out
}

// failureReport collects at most a few failures per property, so a broken
// rule reports examples instead of thousands of lines.
type failureReport struct {
	t     *testing.T
	count int
}

func (f *failureReport) add(format string, args ...any) {
	f.t.Helper()
	f.count++
	if f.count <= 5 {
		f.t.Errorf(format, args...)
	}
}

func (f *failureReport) done() {
	f.t.Helper()
	if f.count > 5 {
		f.t.Errorf("... and %d more", f.count-5)
	}
}

// widenings returns each allowed widening of an allow grant: every
// server instead of one, or one limit removed. Widenings ValidateSelector
// refuses are left out.
func widenings(g Grant) []Grant {
	var out []Grant
	widen := func(change func(Selector)) {
		selector := maps.Clone(g.Selector)
		change(selector)
		if maps.Equal(selector, g.Selector) || ValidateSelector(g.Scope, selector) != nil {
			return
		}
		widened := g
		widened.Selector = selector
		out = append(out, widened)
	}
	widen(func(s Selector) { s[SelectorKeyResourceID] = WildcardResource })
	for _, key := range []string{SelectorKeyProjectID, SelectorKeyTool, SelectorKeyDisposition} {
		widen(func(s Selector) { delete(s, key) })
	}
	return out
}

// principalAgent is an agent principal. Agent grants combine like a user's,
// but never outrank a block.
const principalAgent = "agent:018f8d7b-58d7-7cc4-bb16-9f8c6b99a001"

// userGrants keeps the grants made to the user by name, which is all a
// policy needs to hold for A1.
func userGrants(grants []Grant) []Grant {
	return slices.DeleteFunc(slices.Clone(grants), func(g Grant) bool { return g.PrincipalUrn != principalUser })
}

func singleGrantPolicies(grants []Grant) [][]Grant {
	policies := make([][]Grant, len(grants))
	for i, g := range grants {
		policies[i] = []Grant{g}
	}
	return policies
}

// policiesAllow asks the evaluator a request with several policies uses.
func policiesAllow(t *testing.T, authorization grantAuthorization, check Check) bool {
	t.Helper()
	evaluation, err := authorization.evaluate(check)
	if err != nil {
		t.Fatalf("valid check %+v errored: %v", check, err)
	}
	return evaluation.Grant != nil && !evaluation.Denied
}

// serverLevelGrants is the storable grants with no tool or disposition limit.
// The engine tests run every API on every set, so they use this smaller world.
func serverLevelGrants() []Grant {
	return slices.DeleteFunc(storableGrants(), func(g Grant) bool {
		_, tool := g.Selector[SelectorKeyTool]
		_, disposition := g.Selector[SelectorKeyDisposition]
		return tool || disposition
	})
}

// serverChecks is read, write and connect on every server.
func serverChecks() []namedCheck {
	var checks []namedCheck
	for _, server := range mcpServers {
		checks = append(checks, checksOnServer(server)[:3]...)
	}
	return checks
}

func newInvariantEngine(t *testing.T) *Engine {
	t.Helper()
	return NewEngine(testenv.NewLogger(t), nil, staticChallengeLogging(false), workos.NewStubClient())
}

// engineAllows reads an engine answer: nil is an allow, forbidden a refusal,
// and anything else fails the test.
func engineAllows(t *testing.T, err error) bool {
	t.Helper()
	if err == nil {
		return true
	}
	var shareable *oops.ShareableError
	if !errors.As(err, &shareable) || shareable.Code != oops.CodeForbidden {
		t.Fatalf("engine errored: %v", err)
	}
	return false
}

// someCallAllowed reports whether a call on ref with the probe's missing tool
// or disposition filled in is allowed.
func someCallAllowed(t *testing.T, grants []Grant, ref MCPServerRef, tool, disposition string) bool {
	tools, dispositions := []string{tool}, []string{disposition}
	if tool == "" {
		tools = toolNames
	}
	if disposition == "" {
		dispositions = allDispositions
	}
	for _, tool := range tools {
		for _, disposition := range dispositions {
			if isAllowed(t, grants, ref.ToolCall(tool, disposition)) {
				return true
			}
		}
	}
	return false
}
