package authz

// SensitiveScopesAdded lists, in catalogue order, the sensitive scopes a
// grant set newly satisfies when it changes from before to after. Each side
// counts everything its scopes imply, so adding org:admin to a holder of
// org:read reports org:admin, and adding chat:write to a holder of chat:read
// reports chat:write alone. Nil when nothing sensitive was added.
//
// Exception scopes (*:blocked_*) are grants of nothing: they withdraw access
// rather than give it, are never sensitive themselves, and imply no other
// scope, so they are ignored on both sides. A side that holds org:admin
// together with org:blocked_admin therefore counts as holding org:admin, and
// lifting the exception alone is not reported here. The dashboard reviews
// such a lift as an addition before the save is sent.
func SensitiveScopesAdded(before, after []Scope) []string {
	had := impliedScopes(before)
	has := impliedScopes(after)
	var added []string
	for _, entry := range scopeCatalog {
		if !entry.Sensitive() {
			continue
		}
		if _, ok := has[entry.Scope]; !ok {
			continue
		}
		if _, ok := had[entry.Scope]; ok {
			continue
		}
		added = append(added, string(entry.Scope))
	}
	return added
}

func impliedScopes(scopes []Scope) map[Scope]struct{} {
	implied := make(map[Scope]struct{}, len(scopes))
	for _, scope := range scopes {
		for _, satisfied := range ScopeImplicationClosure(scope) {
			implied[satisfied] = struct{}{}
		}
	}
	return implied
}
