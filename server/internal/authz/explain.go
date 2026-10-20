package authz

import (
	"fmt"
	"slices"
)

// GrantEffect classifies how one loaded grant shaped the decision for a check.
type GrantEffect string

const (
	// GrantEffectAllows marks an allow grant that proves the check.
	GrantEffectAllows GrantEffect = "allows"

	// GrantEffectOverrides marks a direct grant that proves the check despite
	// an exclusion inherited from a role or user:all.
	GrantEffectOverrides GrantEffect = "overrides"

	// GrantEffectBlocks marks an exclusion that withdraws the check.
	GrantEffectBlocks GrantEffect = "blocks"

	// GrantEffectBlocked marks an allow grant that matched the check but that a
	// matching exclusion kept from counting. The grant itself still exists;
	// it proves nothing for this check.
	GrantEffectBlocked GrantEffect = "blocked"

	// GrantEffectOverridden marks an inherited exclusion that a direct grant
	// outranked.
	GrantEffectOverridden GrantEffect = "overridden"
)

// BlockedReason says why a blocked direct allow grant could not outrank the
// exclusion that blocked it.
type BlockedReason string

const (
	// BlockedReasonNone is used for every contribution other than a blocked
	// direct grant, and for direct grants whose exclusion never yields to
	// them.
	BlockedReasonNone BlockedReason = ""

	// BlockedReasonWildcardDirectGrant marks a direct grant spanning every
	// resource, which ranks like any inherited grant.
	BlockedReasonWildcardDirectGrant BlockedReason = "wildcard_direct_grant"

	// BlockedReasonNarrowerDirectGrant marks a direct grant constraining a
	// dimension the tool call does not carry, such as a disposition-scoped
	// grant checked against a tool with no derivable disposition.
	BlockedReasonNarrowerDirectGrant BlockedReason = "narrower_direct_grant"

	// BlockedReasonOwnExclusion marks a direct grant blocked by an exclusion
	// held by the same principal.
	BlockedReasonOwnExclusion BlockedReason = "own_exclusion"
)

// GrantContribution is one loaded grant that matched a check, and its effect
// on the decision.
type GrantContribution struct {
	// Grant is the loaded grant, including the principal holding it.
	Grant Grant

	// Effect is how the grant shaped the decision.
	Effect GrantEffect

	// Reason qualifies a blocked direct grant; it is empty otherwise.
	Reason BlockedReason
}

// GrantCheckExplanation is the decision for one check and every grant that
// matched it.
type GrantCheckExplanation struct {
	// Allowed is the decision, taken from the same evaluation GrantsAuthorize
	// and Engine use.
	Allowed bool

	// Contributions lists every allow grant matching the check, then every
	// matching exclusion, each in load order. Grants that do not match the
	// check are omitted.
	Contributions []GrantContribution
}

// ExplainGrantCheck evaluates one check against already-loaded grants and
// reports every matching grant with its effect. The decision comes from the
// same evaluation as GrantsAuthorize; the contributions are classified as
// sets rather than by which grant happened to match first, so they do not
// depend on load order: every allow grant proving the check is listed, as is
// every exclusion withdrawing it.
func ExplainGrantCheck(grants []Grant, check Check) (GrantCheckExplanation, error) {
	if err := validateInput(check); err != nil {
		return GrantCheckExplanation{}, err
	}
	evaluation, err := evaluateGrantCheck(grants, check)
	if err != nil {
		return GrantCheckExplanation{}, fmt.Errorf("evaluate check: %w", err)
	}
	allowed := evaluation.Grant != nil && !evaluation.Denied

	allowChecks := check.expand()
	var exclusionChecks []Check
	if difference, ok := expressionForCheck(check).(GrantDifference); ok {
		if exclusion, ok := difference.Exclusion.(GrantCheck); ok {
			exclusionChecks = expandWithoutRoot(exclusion.Check)
		}
	}

	var allows, exclusions []Grant
	for i := range grants {
		grant := &grants[i]
		if grantMatchingCheck(grant, allowChecks) != nil {
			allows = append(allows, *grant)
		}
		if grantMatchingCheck(grant, exclusionChecks) != nil {
			exclusions = append(exclusions, *grant)
		}
	}

	contributions := make([]GrantContribution, 0, len(allows)+len(exclusions))
	add := func(grant Grant, effect GrantEffect, reason BlockedReason) {
		contributions = append(contributions, GrantContribution{Grant: grant, Effect: effect, Reason: reason})
	}
	switch {
	case len(allows) == 0 || len(exclusions) == 0:
		for _, grant := range allows {
			add(grant, GrantEffectAllows, BlockedReasonNone)
		}
		for _, grant := range exclusions {
			add(grant, GrantEffectBlocks, BlockedReasonNone)
		}
	case allowed:
		// Every exclusion matched, so the decision can only have come from a
		// direct grant outranking all of them.
		overrideChecks := directOverrideCheck(check).expand()
		for _, grant := range allows {
			if isDirectOverrideGrant(grant) && grantMatchingCheck(&grant, overrideChecks) != nil {
				add(grant, GrantEffectOverrides, BlockedReasonNone)
				continue
			}
			add(grant, GrantEffectBlocked, blockedReason(grant, check, exclusions))
		}
		for _, grant := range exclusions {
			add(grant, GrantEffectOverridden, BlockedReasonNone)
		}
	default:
		for _, grant := range allows {
			add(grant, GrantEffectBlocked, blockedReason(grant, check, exclusions))
		}
		for _, grant := range exclusions {
			add(grant, GrantEffectBlocks, BlockedReasonNone)
		}
	}

	return GrantCheckExplanation{Allowed: allowed, Contributions: contributions}, nil
}

// blockedReason says why a blocked allow grant did not outrank the
// exclusions matching check. Only direct grants facing an exclusion that
// yields to them have a reason.
func blockedReason(grant Grant, check Check, exclusions []Grant) BlockedReason {
	if !IsDirectGrant(grant) || !ExclusionYieldsToDirectGrants(check.Scope) {
		return BlockedReasonNone
	}
	if !isDirectOverrideGrant(grant) {
		return BlockedReasonWildcardDirectGrant
	}
	if grantMatchingCheck(&grant, directOverrideCheck(check).expand()) == nil {
		return BlockedReasonNarrowerDirectGrant
	}
	if slices.ContainsFunc(exclusions, IsDirectGrant) {
		return BlockedReasonOwnExclusion
	}
	return BlockedReasonNone
}
