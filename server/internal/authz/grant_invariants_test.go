package authz

import (
	"errors"
	"fmt"
	"maps"
	"slices"
	"testing"
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
// Property numbers follow the typed checks page (step 5). Not covered here:
// P8 (absent values, added with its own change), P12 (probes), P13 (agents and delegation, which live in runtimepolicy) and
// P14 (organization isolation, which the grant loader enforces before the
// evaluator runs).

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
