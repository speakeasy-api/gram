package authz

import (
	"errors"
	"fmt"
	"maps"
	"slices"
	"testing"
)

// The soundness tests build every small world of MCP grants and ask the real
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

const (
	soundUser     = "user:u1"
	soundRole     = "role:organization:r1"
	soundEveryone = "user:all"

	soundProjectA = "p1"
	soundProjectB = "p2"
)

var (
	soundPrincipals   = []string{soundUser, soundRole, soundEveryone}
	soundScopes       = []Scope{ScopeMCPRead, ScopeMCPWrite, ScopeMCPConnect, ScopeMCPBlockedRead, ScopeMCPBlockedWrite, ScopeMCPBlockedConnect}
	soundDispositions = []string{DispositionReadOnly, DispositionDestructive, DispositionUnannotated}
	soundTools        = []string{"t1", "t2"}
)

// soundServer is one MCP server in the world and the project it belongs to.
type soundServer struct {
	id      string
	project string
}

// Servers s1 and s2 are in project A, s3 is in project B.
var soundServers = []soundServer{{"s1", soundProjectA}, {"s2", soundProjectA}, {"s3", soundProjectB}}

// soundCheck is a check together with the server and project it asks about.
type soundCheck struct {
	name   string
	server soundServer
	check  Check
}

func soundServerChecks(server soundServer) []soundCheck {
	ref := MCPServer(MCPServerIDFrom(server.id), ProjectIDFrom(server.project))
	checks := []soundCheck{
		{name: "read", server: server, check: ref.Read()},
		{name: "write", server: server, check: ref.Write()},
		{name: "connect", server: server, check: ref.Connect()},
	}
	for _, tool := range soundTools {
		for _, disposition := range append([]string{""}, soundDispositions...) {
			checks = append(checks, soundCheck{
				name:   fmt.Sprintf("call %s (%s)", tool, disposition),
				server: server,
				check:  ref.ToolCall(tool, disposition),
			})
		}
	}
	return checks
}

func soundAllChecks() []soundCheck {
	var checks []soundCheck
	for _, server := range soundServers {
		checks = append(checks, soundServerChecks(server)...)
	}
	return checks
}

// soundAtoms is every storable grant in the world: each principal, scope,
// resource (s1 or every server), project limit (none or project A), tool limit
// and disposition limit that ValidateSelector accepts.
func soundAtoms() []Grant {
	var atoms []Grant
	for _, principal := range soundPrincipals {
		for _, scope := range soundScopes {
			for _, resource := range []string{"s1", WildcardResource} {
				for _, project := range []string{"", soundProjectA} {
					for _, tool := range []string{"", "t1"} {
						for _, disposition := range append([]string{""}, soundDispositions...) {
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
							atoms = append(atoms, grant)
						}
					}
				}
			}
		}
	}
	return atoms
}

// soundGrantSets calls fn with the empty set, every single grant and every
// pair of distinct grants.
func soundGrantSets(atoms []Grant, fn func([]Grant)) {
	fn(nil)
	for i := range atoms {
		fn([]Grant{atoms[i]})
		for j := i + 1; j < len(atoms); j++ {
			fn([]Grant{atoms[i], atoms[j]})
		}
	}
}

// soundAnswer is the evaluator's whole answer: allowed only when there is no
// error.
type soundAnswer struct {
	allowed bool
	err     error
}

func soundAuthorize(grants []Grant, check Check) soundAnswer {
	allowed, err := GrantsAuthorize(grants, check)
	return soundAnswer{allowed: allowed && err == nil, err: err}
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

// soundFailures collects at most a few failures per property, so a broken
// rule reports examples instead of thousands of lines.
type soundFailures struct {
	t     *testing.T
	count int
}

func (f *soundFailures) add(format string, args ...any) {
	f.t.Helper()
	f.count++
	if f.count <= 5 {
		f.t.Errorf(format, args...)
	}
}

func (f *soundFailures) done() {
	f.t.Helper()
	if f.count > 5 {
		f.t.Errorf("... and %d more", f.count-5)
	}
}

func TestSoundness_WorldSize(t *testing.T) {
	t.Parallel()
	atoms := soundAtoms()
	sets := 0
	soundGrantSets(atoms, func([]Grant) { sets++ })
	t.Logf("%d storable grants, %d grant sets, %d checks", len(atoms), sets, len(soundAllChecks()))
	if len(atoms) == 0 {
		t.Fatal("the world has no storable grants")
	}
}

// P1: no grants means no access.
func TestSoundness_NoGrantsNoAccess(t *testing.T) {
	t.Parallel()
	for _, c := range soundAllChecks() {
		if soundAuthorize(nil, c.check).allowed {
			t.Errorf("%s on %s allowed with no grants", c.name, c.server.id)
		}
	}
}

// P2: adding an allow grant never removes access.
// P3: adding a block never gives access.
func TestSoundness_AddingGrantsIsMonotonic(t *testing.T) {
	t.Parallel()
	atoms := soundAtoms()
	checks := soundAllChecks()
	allowRemoved := &soundFailures{t: t}
	blockGranted := &soundFailures{t: t}
	soundGrantSets(atoms, func(grants []Grant) {
		if len(grants) == 0 {
			return
		}
		for i, added := range grants {
			before := slices.Delete(slices.Clone(grants), i, i+1)
			for _, c := range checks {
				was := soundAuthorize(before, c.check).allowed
				now := soundAuthorize(grants, c.check).allowed
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
func TestSoundness_ProjectIsolation(t *testing.T) {
	t.Parallel()
	failures := &soundFailures{t: t}
	inB := soundServerChecks(soundServers[2])
	soundGrantSets(soundAtoms(), func(grants []Grant) {
		for _, g := range grants {
			if !IsBlocklistScope(g.Scope) && g.Selector[SelectorKeyProjectID] != soundProjectA {
				return
			}
		}
		for _, c := range inB {
			if soundAuthorize(grants, c.check).allowed {
				failures.add("P4: [%s] allowed %s on %s in project B", describeGrants(grants), c.name, c.server.id)
			}
		}
	})
	failures.done()
}

// P5: with no blocks, a grant set that allows a scope on a server also allows
// every scope that scope implies on it.
func TestSoundness_ImplicationHolds(t *testing.T) {
	t.Parallel()
	failures := &soundFailures{t: t}
	soundGrantSets(soundAtoms(), func(grants []Grant) {
		if slices.ContainsFunc(grants, func(g Grant) bool { return IsBlocklistScope(g.Scope) }) {
			return
		}
		for _, server := range soundServers {
			byScope := map[Scope]soundCheck{}
			for _, c := range soundServerChecks(server)[:3] {
				byScope[c.check.Scope] = c
			}
			for lower, higherScopes := range scopeExpansions {
				lowerCheck, ok := byScope[lower]
				if !ok {
					continue
				}
				for _, higher := range higherScopes {
					higherCheck, ok := byScope[higher]
					if !ok {
						continue
					}
					if soundAuthorize(grants, higherCheck.check).allowed && !soundAuthorize(grants, lowerCheck.check).allowed {
						failures.add("P5: [%s] allows %s on %s but not %s", describeGrants(grants), higherCheck.name, server.id, lowerCheck.name)
					}
				}
			}
		}
	})
	failures.done()
}

// soundWidenings returns each allowed widening of an allow grant: every
// server instead of one, or one limit removed. Widenings ValidateSelector
// refuses are left out.
func soundWidenings(g Grant) []Grant {
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

// P6: widening an allow grant never removes access. The one expected
// exception is P7's: a grant made to the user by name for one server outranks
// an inherited block, and widening it to every server gives that up.
func TestSoundness_WideningNeverRemovesAccess(t *testing.T) {
	t.Parallel()
	failures := &soundFailures{t: t}
	checks := soundAllChecks()
	soundGrantSets(soundAtoms(), func(grants []Grant) {
		hasBlock := slices.ContainsFunc(grants, func(g Grant) bool { return IsBlocklistScope(g.Scope) })
		for i, g := range grants {
			if IsBlocklistScope(g.Scope) {
				continue
			}
			outranksBlocks := hasBlock && IsDirectGrant(g) && g.Selector.ResourceID() != WildcardResource
			for _, wide := range soundWidenings(g) {
				if outranksBlocks && wide.Selector.ResourceID() == WildcardResource {
					continue
				}
				widened := slices.Clone(grants)
				widened[i] = wide
				for _, c := range checks {
					if soundAuthorize(grants, c.check).allowed && !soundAuthorize(widened, c.check).allowed {
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
func TestSoundness_BlockWins(t *testing.T) {
	t.Parallel()
	failures := &soundFailures{t: t}
	checks := soundAllChecks()
	soundGrantSets(soundAtoms(), func(grants []Grant) {
		for _, c := range checks {
			blockScope, ok := ExclusionScopeFor(c.check.Scope)
			if !ok {
				continue
			}
			namesServer := slices.ContainsFunc(grants, func(g Grant) bool {
				return !IsBlocklistScope(g.Scope) && IsDirectGrant(g) && g.Selector.ResourceID() == c.server.id
			})
			for _, holder := range soundPrincipals {
				block := NewGrant(blockScope, c.server.id)
				block.PrincipalUrn = holder
				allowed := soundAuthorize(append(slices.Clone(grants), block), c.check).allowed
				if !allowed {
					continue
				}
				if holder == soundUser || !namesServer {
					failures.add("P7: [%s] plus %s still allows %s on %s", describeGrants(grants), describeGrant(block), c.name, c.server.id)
				}
			}
		}
	})
	failures.done()
}

// P9: a grant limited to a tool or a disposition never allows a call to
// another tool or a call with another disposition.
func TestSoundness_LimitsAreRespected(t *testing.T) {
	t.Parallel()
	failures := &soundFailures{t: t}
	for _, g := range soundAtoms() {
		if IsBlocklistScope(g.Scope) {
			continue
		}
		limitTool, hasTool := g.Selector[SelectorKeyTool]
		limitDisposition, hasDisposition := g.Selector[SelectorKeyDisposition]
		if !hasTool && !hasDisposition {
			continue
		}
		for _, server := range soundServers {
			ref := MCPServer(MCPServerIDFrom(server.id), ProjectIDFrom(server.project))
			for _, tool := range soundTools {
				for _, disposition := range append([]string{""}, soundDispositions...) {
					outside := (hasTool && tool != limitTool) || (hasDisposition && disposition != limitDisposition)
					if outside && soundAuthorize([]Grant{g}, ref.ToolCall(tool, disposition)).allowed {
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
func TestSoundness_InvalidChecksAreRefused(t *testing.T) {
	t.Parallel()
	failures := &soundFailures{t: t}
	invalid := map[string]Check{
		"read with no server id":    MCPServer(MCPServerIDFrom(""), ProjectIDFrom(soundProjectA)).Read(),
		"connect with no server id": MCPServer(MCPServerIDFrom(""), ProjectIDFrom(soundProjectA)).Connect(),
		"call with no server id":    MCPServer(MCPServerIDFrom(""), ProjectIDFrom(soundProjectA)).ToolCall("t1", DispositionReadOnly),
		"read with no project id":   MCPServer(MCPServerIDFrom("s1"), ProjectIDFrom("")).Read(),
		"connect with no project":   MCPServer(MCPServerIDFrom("s1"), ProjectIDFrom("")).Connect(),
		"call with no project id":   MCPServer(MCPServerIDFrom("s1"), ProjectIDFrom("")).ToolCall("t1", DispositionReadOnly),
	}
	soundGrantSets(soundAtoms(), func(grants []Grant) {
		for name, check := range invalid {
			answer := soundAuthorize(grants, check)
			if answer.allowed || !errors.Is(answer.err, ErrInvalidCheck) {
				failures.add("P10: [%s] answered %s with allowed=%v err=%v", describeGrants(grants), name, answer.allowed, answer.err)
			}
		}
	})
	failures.done()
}

// P11: the order of the grants never changes the answer.
func TestSoundness_OrderDoesNotMatter(t *testing.T) {
	t.Parallel()
	failures := &soundFailures{t: t}
	checks := soundAllChecks()
	soundGrantSets(soundAtoms(), func(grants []Grant) {
		if len(grants) < 2 {
			return
		}
		reversed := slices.Clone(grants)
		slices.Reverse(reversed)
		for _, c := range checks {
			a, b := soundAuthorize(grants, c.check), soundAuthorize(reversed, c.check)
			if a.allowed != b.allowed || (a.err == nil) != (b.err == nil) {
				failures.add("P11: [%s] answers %s on %s differently when reversed", describeGrants(grants), c.name, c.server.id)
			}
		}
	})
	failures.done()
}
