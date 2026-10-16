package authz

import "slices"

// evaluate interprets one definition without automatic transitive implication.
// Principal precedence and audiences are later slices.
func evaluate(grants []ResolvedGrant, question Evaluation, revision uint64) Decision {
	allowed, uncertainAllow, uncertainBlock := false, false, false
	for _, assignment := range grants {
		grant := assignment.Grant
		if !relevantScope(question.Definition, grant.Permission) || !anchorMatches(grant, question) {
			continue
		}
		match := constraintMatch(grant.Constraints, question.Tool)
		if match == Denied {
			continue
		}
		if slices.Contains(question.Definition.ExcludedBy, grant.Permission) {
			if match == Allowed {
				return Decision{Outcome: Denied, Reason: "matching opposing scope", PolicyRevision: revision, Exclusion: DefiniteExclusion}
			}
			uncertainBlock = true
		} else if match == Allowed {
			allowed = true
		} else {
			uncertainAllow = true
		}
	}
	exclusion := NoExclusion
	if uncertainBlock {
		exclusion = UncertainExclusion
	}
	if !allowed && !uncertainAllow {
		return Decision{Outcome: Denied, Reason: "no matching permission", PolicyRevision: revision, Exclusion: exclusion}
	}
	if !allowed || uncertainBlock {
		return Decision{Outcome: Unresolved, Reason: "missing facts affect policy matching", PolicyRevision: revision, Exclusion: exclusion}
	}
	return Decision{Outcome: Allowed, Reason: "matching permission without a matching exclusion", PolicyRevision: revision, Exclusion: NoExclusion}
}

func relevantScope(definition PermissionDefinition, scope Permission) bool {
	return scope == definition.Name || slices.Contains(definition.ImpliedBy, scope) || slices.Contains(definition.ExcludedBy, scope)
}

func anchorMatches(grant Grant, question Evaluation) bool {
	if !slices.Contains(question.Definition.AllowedAnchors, grant.Anchor.Kind) {
		return false
	}
	if grant.Anchor == question.Target.Resource {
		return true
	}
	return slices.Contains(question.Target.Ancestors, grant.Anchor)
}

// constraintMatch never treats missing facts as evidence for a constrained grant.
// An unconstrained grant, however, does not require a tool classification.
func constraintMatch(constraints Constraints, facts *ToolFacts) Outcome {
	if constraints == (Constraints{}) {
		return Allowed
	}
	if facts == nil {
		return Unresolved
	}
	if constraints.Tool != "" && constraints.Tool != facts.Name {
		return Denied
	}
	if constraints.Disposition != DispositionUnknown {
		if facts.Disposition == DispositionUnknown {
			return Unresolved
		}
		if constraints.Disposition != facts.Disposition {
			return Denied
		}
	}
	return Allowed
}
