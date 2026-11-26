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

// These tests ask the real evaluator every check in a small world (two
// projects, servers and principals, up to two grants) and compare its answers
// with each other, so they cannot drift from the code. Rule mistakes show up
// at that size.

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

func TestGrantInvariants_NoGrantsNoAccess(t *testing.T) {
	t.Parallel()
	for _, c := range everyCheck() {
		if isAllowed(t, nil, c.check) {
			t.Errorf("%s on %s allowed with no grants", c.name, c.server.id)
		}
	}
}

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
					blockGranted.add("adding block %s to [%s] allowed %s on %s", describeGrant(added), describeGrants(before), c.name, c.server.id)
				case !IsBlocklistScope(added.Scope) && was && !now:
					allowRemoved.add("adding %s to [%s] removed %s on %s", describeGrant(added), describeGrants(before), c.name, c.server.id)
				}
			}
		}
	})
	allowRemoved.done()
	blockGranted.done()
}

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
				failures.add("[%s] allowed %s on %s in project B", describeGrants(grants), c.name, c.server.id)
			}
		}
	})
	failures.done()
}

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
						failures.add("[%s] allows %s on %s but not %s", describeGrants(grants), higherCheck.name, server.id, lowerCheck.name)
					}
				}
			}
		}
	})
	failures.done()
}

// The one exception: a user's own grant for one server outranks an inherited
// block, and widening it to every server gives that up.
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
						failures.add("widening %s to %s in [%s] removed %s on %s", describeGrant(g), describeGrant(wide), describeGrants(grants), c.name, c.server.id)
					}
				}
			}
		}
	})
	failures.done()
}

// A block wins, except that a user's own grant for that one server outranks a
// block reaching them through a role or everyone.
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
					failures.add("[%s] plus %s still allows %s on %s", describeGrants(grants), describeGrant(block), c.name, c.server.id)
				}
			}
		}
	})
	failures.done()
}

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
						failures.add("[%s] refuses %s (%s) on %s but allows it with no disposition", describeGrants(grants), tool, disposition, server.id)
					}
					if isAllowed(t, grants, ref.ToolCall("", disposition)) {
						failures.add("[%s] refuses %s (%s) on %s but allows it with no tool", describeGrants(grants), tool, disposition, server.id)
					}
				}
			}
		}
	})
	failures.done()
}

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
						failures.add("%s allows calling %s (%s) on %s", describeGrant(g), tool, disposition, server.id)
					}
				}
			}
		}
	}
	failures.done()
}

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
				failures.add("[%s] answered %s with allowed=%v err=%v", describeGrants(grants), name, answer.allowed, answer.err)
			}
		}
	})
	failures.done()
}

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
				failures.add("[%s] answers %s on %s differently when reversed", describeGrants(grants), c.name, c.server.id)
			}
		}
	})
	failures.done()
}

// An agent's credential, the agent and its owner each carry a policy, and each
// must allow the check on its own.
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
					failures.add("policies [%s] and [%s] answer %s on %s with %v, each alone says %v", describeGrants(first), describeGrants(second), c.name, c.server.id, got, want)
				}
			}
		}
	}
	failures.done()
}

// An agent's owner can write its policy without being an administrator, so an
// agent grant outranking a block would let owners lift blocks.
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
				failures.add("[%s] allows %s on %s for the agent but not for the user", describeGrants(asAgent), c.name, c.server.id)
			}
			blockScope, ok := ExclusionScopeFor(c.check.Scope)
			if !ok {
				continue
			}
			for _, holder := range []string{principalRole, principalEveryone} {
				block := NewGrant(blockScope, c.server.id)
				block.PrincipalUrn = holder
				if isAllowed(t, append(slices.Clone(asAgent), block), c.check) {
					failures.add("agent grants [%s] outrank %s for %s on %s", describeGrants(asAgent), describeGrant(block), c.name, c.server.id)
				}
			}
		}
	})
	failures.done()
}

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
						t.Errorf("acting for %q (user policy allows: %v), caller binding %q, connection of %q: got %v, want %v", represented, userAllows, callerBinding, owner, got, want)
					}
				}
			}
		}
	}
}

// Either is allowed only when one alternative passes on its own, whatever the
// order, and Can gives the same answer as Require.
func TestGrantInvariants_Either(t *testing.T) {
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
				allowed := engineAllows(t, engine.Require(ctx, Either(a.check, b.check)))
				if allowed && !alone[i] && !alone[j] {
					failures.add("[%s] Either(%s on %s, %s on %s) is allowed, but neither alternative is on its own", describeGrants(grants), a.name, a.server.id, b.name, b.server.id)
				}
				if allowed != engineAllows(t, engine.Require(ctx, Either(b.check, a.check))) {
					failures.add("[%s] Either(%s on %s, %s on %s) depends on order", describeGrants(grants), a.name, a.server.id, b.name, b.server.id)
				}
				decided, err := engine.Decide(ctx, Either(a.check, b.check))
				if err != nil {
					t.Fatalf("Decide(Either) errored: %v", err)
				}
				if decided.AllAllowed != allowed {
					failures.add("[%s] Decide(Either(%s on %s, %s on %s)) = %v, Require = %v", describeGrants(grants), a.name, a.server.id, b.name, b.server.id, decided.AllAllowed, allowed)
				}
			}
		}
	})
	failures.done()
}

func TestGrantInvariants_EitherRefusesEveryBlock(t *testing.T) {
	t.Parallel()
	engine := newInvariantEngine(t)
	failures := &failureReport{t: t}
	checks := serverChecks()
	forEachGrantSet(serverLevelGrants(), func(grants []Grant) {
		for i, a := range checks {
			blockScope, _ := ExclusionScopeFor(a.check.Scope)
			// A user's own, non-backfilled grant for this alternative can
			// outrank a role's block, so that case is not a counterexample.
			outranksBlock := isAllowed(t, DirectOverrideGrants(grants), a.check)
			for _, holder := range allPrincipals {
				if holder != principalUser && outranksBlock {
					continue
				}
				block := NewGrant(blockScope, a.server.id)
				block.PrincipalUrn = holder
				ctx := GrantsToContext(enterpriseSessionCtx(t), append(slices.Clone(grants), block))
				for j, b := range checks {
					if i == j {
						continue
					}
					if engineAllows(t, engine.Require(ctx, Either(b.check, a.check))) {
						failures.add("[%s] plus %s still allows Either(%s on %s, %s on %s)", describeGrants(grants), describeGrant(block), b.name, b.server.id, a.name, a.server.id)
					}
				}
			}
		}
	})
	failures.done()
}

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
					failures.add("[%s] outranks %s for %s on %s without naming the server", describeGrants(grants), describeGrant(block), c.name, c.server.id)
				}
				if isAllowed(t, append(slices.Clone(inferred), block), c.check) {
					failures.add("backfilled [%s] outranks %s for %s on %s", describeGrants(inferred), describeGrant(block), c.name, c.server.id)
				}
			}
		}
	})
	failures.done()
}

func TestGrantInvariants_WiderCoversNarrower(t *testing.T) {
	t.Parallel()
	failures := &failureReport{t: t}
	root := NewGrant(ScopeRoot, WildcardResource)
	root.PrincipalUrn = principalUser
	for _, c := range everyCheck() {
		if !isAllowed(t, []Grant{root}, c.check) {
			failures.add("root does not allow %s on %s", c.name, c.server.id)
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
				failures.add("%s allows %s on %s", describeGrant(g), c.name, c.server.id)
			}
			everyServer := g
			everyServer.Selector = maps.Clone(g.Selector)
			everyServer.Selector[SelectorKeyResourceID] = WildcardResource
			if !isAllowed(t, []Grant{everyServer}, c.check) {
				failures.add("%s allows %s on %s but %s does not", describeGrant(g), c.name, c.server.id, describeGrant(everyServer))
			}
		}
	}
	failures.done()
}

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
		batch, err := engine.Decide(ctx, all...)
		if err != nil {
			t.Fatalf("Decide errored: %v", err)
		}
		for i, c := range checks {
			want := isAllowed(t, grants, c.check)
			alone, err := engine.Decide(ctx, c.check)
			if err != nil {
				t.Fatalf("Decide errored: %v", err)
			}
			answers := map[string]bool{
				"Require":         engineAllows(t, engine.Require(ctx, c.check)),
				"Decide alone":    alone.AllAllowed,
				"Decide in batch": batch.Each[i].Allowed,
			}
			for name, got := range answers {
				if got != want {
					failures.add("[%s] %s says %v for %s on %s, GrantsAuthorize says %v", describeGrants(grants), name, got, c.name, c.server.id, want)
				}
			}
		}
	})
	failures.done()
}

// The reverse need not hold: a block on one tool also covers a probe naming no tool.
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
						failures.add("[%s] allows a probe of %q (%q) on %s but no call", describeGrants(grants), tool, disposition, server.id)
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

type mcpServer struct {
	id      string
	project string
}

// Servers s1 and s2 are in project A, s3 is in project B.
var mcpServers = []mcpServer{{"s1", projectA}, {"s2", projectA}, {"s3", projectB}}

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

// storableGrants leaves out every grant ValidateSelector would refuse.
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

func forEachGrantSet(storable []Grant, fn func([]Grant)) {
	fn(nil)
	for i := range storable {
		fn([]Grant{storable[i]})
		for j := i + 1; j < len(storable); j++ {
			fn([]Grant{storable[i], storable[j]})
		}
	}
}

type evaluatorAnswer struct {
	allowed bool
	err     error
}

func authorize(grants []Grant, check Check) evaluatorAnswer {
	allowed, err := GrantsAuthorize(grants, check)
	return evaluatorAnswer{allowed: allowed && err == nil, err: err}
}

// isAllowed fails the test on an error: the world only builds valid checks.
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

// failureReport keeps a few examples, so a broken rule does not print thousands of lines.
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

const principalAgent = "agent:018f8d7b-58d7-7cc4-bb16-9f8c6b99a001"

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

func policiesAllow(t *testing.T, authorization grantAuthorization, check Check) bool {
	t.Helper()
	evaluation, err := authorization.evaluate(check)
	if err != nil {
		t.Fatalf("valid check %+v errored: %v", check, err)
	}
	return evaluation.Grant != nil && !evaluation.Denied
}

// serverLevelGrants keeps the engine tests fast: they run every API on every set.
func serverLevelGrants() []Grant {
	return slices.DeleteFunc(storableGrants(), func(g Grant) bool {
		_, tool := g.Selector[SelectorKeyTool]
		_, disposition := g.Selector[SelectorKeyDisposition]
		return tool || disposition
	})
}

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

func someCallAllowed(t *testing.T, grants []Grant, ref MCPServerRef, tool, disposition string) bool {
	t.Helper()
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
