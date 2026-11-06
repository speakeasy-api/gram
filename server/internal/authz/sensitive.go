package authz

// SensitiveScopesAdded lists, in catalogue order, the sensitive scopes a
// grant set newly satisfies when it changes from before to after. Each side
// counts everything its scopes imply, so adding org:admin to a holder of
// org:read reports org:admin, and adding chat:write to a holder of chat:read
// reports chat:write alone. Nil when nothing sensitive was added.
//
// The comparison is scope-level: a scope held on some resources before and on
// every resource after is not an addition here. Role edits record that
// widening in their snapshots; membership changes have no such record and use
// SensitiveScopesWidened instead.
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
// after coverage holds that before did not, or that before held only on some
// resources where after holds them on every resource. A member who could read
// one project's tool logs and is given a role with unrestricted log:read is
// reported as gaining log:read. Implication carries coverage along: an
// unrestricted chat:write holds chat:read unrestricted too. Nil when nothing
// sensitive was added or widened.
func SensitiveScopesWidened(before, after []ScopeCoverage) []string {
	had := impliedCoverage(before)
	has := impliedCoverage(after)
	var added []string
	for _, entry := range scopeCatalog {
		if !entry.Sensitive() {
			continue
		}
		hasUnrestricted, ok := has[entry.Scope]
		if !ok {
			continue
		}
		hadUnrestricted, held := had[entry.Scope]
		if held && (hadUnrestricted || !hasUnrestricted) {
			continue
		}
		added = append(added, string(entry.Scope))
	}
	return added
}

// impliedCoverage maps every scope a coverage set satisfies to whether it is
// satisfied on every resource.
func impliedCoverage(coverage []ScopeCoverage) map[Scope]bool {
	implied := make(map[Scope]bool, len(coverage))
	for _, held := range coverage {
		for _, satisfied := range ScopeImplicationClosure(held.Scope) {
			implied[satisfied] = implied[satisfied] || held.Unrestricted
		}
	}
	return implied
}

func unrestrictedCoverage(scopes []Scope) []ScopeCoverage {
	coverage := make([]ScopeCoverage, 0, len(scopes))
	for _, scope := range scopes {
		coverage = append(coverage, ScopeCoverage{Scope: scope, Unrestricted: true})
	}
	return coverage
}
