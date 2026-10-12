package runtimepolicy

import (
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"slices"

	"github.com/speakeasy-api/gram/server/internal/authz"
)

// MaxDelegableGrantCandidates bounds the candidates one discovery may return.
const MaxDelegableGrantCandidates = 4096

// ErrTooManyDelegableGrantCandidates marks a discovery over that bound.
var ErrTooManyDelegableGrantCandidates = errors.New("too many delegable grant candidates")

// DelegableGrants returns safe representable allow-only candidates, not a full
// resource inventory. An overlapping exclusion removes the entire candidate:
// subtracting a tool from a wildcard cannot be encoded by an allow selector.
// Optional resource constraints narrow candidates before containment, so an
// unrelated exclusion does not hide a safe concrete candidate.
func DelegableGrants(agent, owner, caller []authz.Grant, constraints ...authz.Selector) ([]authz.Grant, error) {
	return delegableGrants(agent, owner, caller, false, constraints)
}

// DelegableGrantsWithExclusions returns what DelegableGrants does, except that
// an exclusion narrower than a candidate is carried into the result instead of
// removing the candidate. A wildcard allow overlapping one blocked resource
// stays delegable, paired with the exclusion for that resource.
func DelegableGrantsWithExclusions(agent, owner, caller []authz.Grant) ([]authz.Grant, error) {
	return delegableGrants(agent, owner, caller, true, nil)
}

func delegableGrants(agent, owner, caller []authz.Grant, carryExclusions bool, constraints []authz.Selector) ([]authz.Grant, error) {
	candidates := make(map[string]authz.Grant)
	visited := make(map[string]struct{})
	for _, grant := range agent {
		// Exclusions are restrictions, not authority to delegate. They reach a
		// delegated policy only when carried alongside the allow they narrow.
		if authz.IsBlocklistScope(grant.Scope) {
			continue
		}
		selector := grant.Selector
		compatible := true
		for _, constraint := range constraints {
			selector, compatible = intersectSelectors(selector, constraint)
			if !compatible {
				break
			}
		}
		if !compatible {
			continue
		}
		for _, scope := range authz.ScopeImplicationClosure(grant.Scope) {
			if !IsRuntimeScopeSafe(CurrentRuntimeScopeRegistryVersion, scope) {
				continue
			}
			for _, ownerGrant := range owner {
				selector, ok := intersectSelectors(selector, ownerGrant.Selector)
				if !ok || !authz.GrantsContainSelector([]authz.Grant{ownerGrant}, scope, selector) {
					continue
				}
				for _, callerGrant := range caller {
					narrowed, ok := intersectSelectors(selector, callerGrant.Selector)
					if !ok || !authz.GrantsContainSelector([]authz.Grant{callerGrant}, scope, narrowed) {
						continue
					}
					candidate := authz.Grant{PrincipalUrn: "", Scope: scope, Selector: narrowed}
					key, err := delegableGrantKey(candidate)
					if err != nil {
						return nil, err
					}
					if _, seen := visited[key]; seen {
						continue
					}
					visited[key] = struct{}{}
					delegated, ok := delegationFor(candidate, carryExclusions, agent, owner, caller)
					if !ok {
						continue
					}
					policy, err := NewDelegatedPolicy(CurrentDelegatedPolicyVersion, delegated)
					if err != nil {
						continue
					}
					safe, err := DelegationContained(policy, agent, owner, caller)
					if err != nil {
						return nil, err
					}
					if !safe {
						continue
					}
					for _, grant := range delegated {
						key, err := delegableGrantKey(grant)
						if err != nil {
							return nil, err
						}
						candidates[key] = grant
					}
					if len(candidates) > MaxDelegableGrantCandidates {
						return nil, ErrTooManyDelegableGrantCandidates
					}
				}
			}
		}
	}
	keys := slices.Sorted(maps.Keys(candidates))
	result := make([]authz.Grant, 0, len(keys))
	for _, key := range keys {
		result = append(result, candidates[key])
	}
	return result, nil
}

func delegableGrantKey(grant authz.Grant) (string, error) {
	encoded, err := json.Marshal(grant.Selector)
	if err != nil {
		return "", fmt.Errorf("encode delegable selector: %w", err)
	}
	return string(grant.Scope) + "\x00" + string(encoded), nil
}

// delegationFor returns candidate plus, when carrying, the exclusion withdrawing
// each live parent restriction that overlaps it. Nothing is carried when the
// exclusion scope cannot be delegated, so the candidate falls back to failing
// containment. It reports false when a direct grant overrides only part of an
// overlap: delegated policies cannot express that override, and the carried
// exclusion would also withdraw the directly granted part.
func delegationFor(candidate authz.Grant, carryExclusions bool, policies ...[]authz.Grant) ([]authz.Grant, bool) {
	delegated := []authz.Grant{candidate}
	exclusion, ok := authz.ExclusionScopeFor(candidate.Scope)
	if !carryExclusions || !ok || !IsRuntimeScopeSafe(CurrentRuntimeScopeRegistryVersion, exclusion) {
		return delegated, true
	}
	for _, policy := range policies {
		for _, overlap := range liveOverlaps(policy, candidate, exclusion) {
			if overlap.inherited && partiallyOverridden(policy, candidate.Scope, overlap.selector) {
				return nil, false
			}
			if !slices.ContainsFunc(delegated[1:], func(existing authz.Grant) bool { return maps.Equal(existing.Selector, overlap.selector) }) {
				delegated = append(delegated, authz.Grant{PrincipalUrn: "", Scope: exclusion, Selector: overlap.selector})
			}
		}
	}
	return delegated, true
}

// partiallyOverridden reports whether a direct grant in policy overrides some
// of overlap for scope at runtime, such as one tool on a blocked server.
// liveOverlaps has already removed overlaps a direct grant fully overrides;
// both follow authz.DirectOverrideGrants and must change with it.
func partiallyOverridden(policy []authz.Grant, scope authz.Scope, overlap authz.Selector) bool {
	if !authz.ExclusionYieldsToDirectGrants(scope) {
		return false
	}
	for _, direct := range authz.DirectOverrideGrants(policy) {
		if authz.IsBlocklistScope(direct.Scope) {
			continue
		}
		narrowed, ok := intersectSelectors(overlap, direct.Selector)
		if ok && authz.GrantsContainSelector([]authz.Grant{direct}, scope, narrowed) {
			return true
		}
	}
	return false
}

// DelegationContained proves that the entire delegated selector set, including
// all implied scopes, is allowed by every parent policy. Instance authorization
// alone is insufficient: a narrower exclusion may overlap a broad delegation
// without matching its dimensionless check. Such overlap fails closed unless
// the delegated policy carries an exclusion covering it, or a direct grant
// naming the resource outranks the inherited restriction. Delegated exclusions
// only narrow, so they need no parent authority of their own. Discovery and
// issuance share this check so neither can broaden a parent's effective
// permissions.
func DelegationContained(delegated DelegatedPolicy, policies ...[]authz.Grant) (bool, error) {
	grants := delegated.RuntimeGrants()
	for _, grant := range grants {
		if authz.IsBlocklistScope(grant.Scope) {
			continue
		}
		for _, policy := range policies {
			if !authz.GrantsContainSelector(policy, grant.Scope, grant.Selector) {
				return false, nil
			}
			exclusion, hasExclusion := authz.ExclusionScopeFor(grant.Scope)
			if !hasExclusion {
				continue
			}
			for _, overlap := range liveOverlaps(policy, grant, exclusion) {
				covered := slices.ContainsFunc(grants, func(carried authz.Grant) bool {
					return carried.Scope == exclusion && carried.Selector.StrictMatches(overlap.selector)
				})
				if !covered {
					return false, nil
				}
			}
		}
	}
	return true, nil
}

// liveOverlap is the part of a grant one restriction withdraws. inherited
// marks a restriction from a role or user:all, which a direct grant outranks.
type liveOverlap struct {
	selector  authz.Selector
	inherited bool
}

// liveOverlaps returns the part of grant each restriction in policy withdraws
// at runtime. Root is deliberately not an exclusion, matching authz's
// evaluator. A direct grant naming the restricted resource outranks
// restrictions inherited from roles or user:all, exactly as it does at
// runtime; the principal's own restrictions are never outranked.
func liveOverlaps(policy []authz.Grant, grant authz.Grant, exclusion authz.Scope) []liveOverlap {
	var overlaps []liveOverlap
	for _, restriction := range policy {
		if !slices.Contains(authz.ScopeImplicationClosure(restriction.Scope), exclusion) {
			continue
		}
		overlap, ok := intersectSelectors(grant.Selector, restriction.Selector)
		if !ok {
			continue
		}
		inherited := !authz.IsDirectGrant(restriction)
		outranked := inherited &&
			authz.ExclusionYieldsToDirectGrants(grant.Scope) &&
			authz.GrantsContainSelector(authz.DirectOverrideGrants(policy), grant.Scope, overlap)
		if !outranked {
			overlaps = append(overlaps, liveOverlap{selector: overlap, inherited: inherited})
		}
	}
	return overlaps
}

// A selector is a conjunction of exact values or wildcards. Intersection keeps
// every pinned dimension from either side; incompatible values have no overlap.
func intersectSelectors(a, b authz.Selector) (authz.Selector, bool) {
	result := maps.Clone(a)
	if result == nil {
		result = make(authz.Selector)
	}
	for key, value := range b {
		previous, exists := result[key]
		if !exists || previous == "*" {
			result[key] = value
			continue
		}
		if value != "*" && value != previous {
			return nil, false
		}
	}
	return result, true
}
