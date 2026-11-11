package authz

// SensitiveScopesAdded lists, in catalogue order, the sensitive scopes a
// grant set newly satisfies when it changes from before to after. Each side
// counts everything its scopes imply, so adding org:admin to a holder of
// org:read reports org:admin, and adding chat:write to a holder of chat:read
// reports chat:write alone. Nil when nothing sensitive was added.
//
// The comparison is scope-level: a scope held on some resources before and on
// more, or every, resource after is not an addition here. Role edits record
// that widening in their snapshots; membership changes have no such record
// and use SensitiveScopesWidened instead.
//
// Exception scopes (*:blocked_*) are grants of nothing: they withdraw access
// rather than give it, are never sensitive themselves, and imply no other
// scope, so they are ignored on both sides. A side that holds org:admin
// together with org:blocked_admin therefore counts as holding org:admin, and
// lifting the exception alone is not reported here. The dashboard reviews
// such a lift as an addition before the save is sent.
func SensitiveScopesAdded(before, after []Scope) []string {
	return SensitiveScopesWidened(unrestrictedCoverage(before), unrestrictedCoverage(after))
}

// SensitiveScopesWidened lists, in catalogue order, the sensitive scopes the
// after coverage reaches somewhere the before coverage did not: scopes not
// held before, held on some resources before and on every resource after, or
// held on some resources before and on another resource after. A member who
// could read one project's tool logs and is given a role with log:read on a
// second project, or on every project, is reported as gaining log:read.
// Implication carries coverage along: an unrestricted chat:write holds
// chat:read unrestricted too. Nil when nothing sensitive was added or
// widened.
func SensitiveScopesWidened(before, after []ScopeCoverage) []string {
	had := impliedCoverage(before)
	has := impliedCoverage(after)
	var added []string
	for _, entry := range scopeCatalog {
		if !entry.Sensitive() {
			continue
		}
		reach, ok := has[entry.Scope]
		if !ok {
			continue
		}
		if held, ok := had[entry.Scope]; ok && held.covers(reach) {
			continue
		}
		added = append(added, string(entry.Scope))
	}
	return added
}

// coverage is how widely one scope is held: on every resource, or on the
// resources keyed in restricted.
type coverage struct {
	unrestricted bool
	restricted   map[string]struct{}
}

// covers reports whether everything other reaches, c reaches too.
func (c *coverage) covers(other *coverage) bool {
	if c.unrestricted {
		return true
	}
	if other.unrestricted {
		return false
	}
	for key := range other.restricted {
		if _, ok := c.restricted[key]; !ok {
			return false
		}
	}
	return true
}

// impliedCoverage maps every scope a coverage set satisfies to how widely it
// is satisfied.
func impliedCoverage(held []ScopeCoverage) map[Scope]*coverage {
	implied := make(map[Scope]*coverage, len(held))
	for _, holding := range held {
		for _, satisfied := range holding.Scope.ImpliedScopes() {
			reach, ok := implied[satisfied]
			if !ok {
				reach = &coverage{unrestricted: false, restricted: make(map[string]struct{})}
				implied[satisfied] = reach
			}
			reach.unrestricted = reach.unrestricted || holding.Unrestricted
			for _, key := range holding.Resources {
				reach.restricted[key] = struct{}{}
			}
		}
	}
	return implied
}

func unrestrictedCoverage(scopes []Scope) []ScopeCoverage {
	coverage := make([]ScopeCoverage, 0, len(scopes))
	for _, scope := range scopes {
		coverage = append(coverage, ScopeCoverage{Scope: scope, Unrestricted: true, Resources: nil})
	}
	return coverage
}
