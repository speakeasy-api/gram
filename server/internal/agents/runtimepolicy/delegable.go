package runtimepolicy

import (
	"encoding/json"
	"fmt"
	"maps"
	"slices"

	"github.com/speakeasy-api/gram/server/internal/authz"
)

// DelegableGrants returns safe representable allow-only candidates, not a full
// resource inventory. An overlapping exclusion removes the entire candidate:
// subtracting a tool from a wildcard cannot be encoded by an allow selector.
func DelegableGrants(agent, owner, caller []authz.Grant) ([]authz.Grant, error) {
	candidates := make(map[string]authz.Grant)
	for _, grant := range agent {
		for _, scope := range authz.ScopeImplicationClosure(grant.Scope) {
			if !IsRuntimeScopeSafe(CurrentRuntimeScopeRegistryVersion, scope) {
				continue
			}
			for _, ownerGrant := range owner {
				selector, ok := intersectSelectors(grant.Selector, ownerGrant.Selector)
				if !ok || !authz.GrantsSatisfy([]authz.Grant{ownerGrant}, delegationCheck(scope, selector)) {
					continue
				}
				for _, callerGrant := range caller {
					narrowed, ok := intersectSelectors(selector, callerGrant.Selector)
					if !ok || !authz.GrantsSatisfy([]authz.Grant{callerGrant}, delegationCheck(scope, narrowed)) {
						continue
					}
					candidate := authz.Grant{PrincipalUrn: "", Scope: scope, Selector: narrowed}
					policy, err := NewDelegatedPolicyV1([]authz.Grant{candidate})
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
					encoded, err := json.Marshal(narrowed)
					if err != nil {
						return nil, fmt.Errorf("encode delegable selector: %w", err)
					}
					candidates[string(scope)+"\x00"+string(encoded)] = candidate
					if len(candidates) > 4096 {
						return nil, fmt.Errorf("too many delegable grant candidates")
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

// DelegationContained proves that the entire delegated selector set, including
// all implied scopes, is allowed by every parent policy. Instance authorization
// alone is insufficient: a narrower exclusion may overlap a broad delegation
// without matching its dimensionless check. Such overlap fails closed because
// delegated policies cannot encode exclusions. Discovery and issuance share
// this check so neither can broaden a parent's effective permissions.
func DelegationContained(delegated DelegatedPolicy, policies ...[]authz.Grant) (bool, error) {
	for _, grant := range delegated.RuntimeGrants() {
		for _, policy := range policies {
			allowed, err := authz.GrantsAuthorize(policy, delegationCheck(grant.Scope, grant.Selector))
			if err != nil {
				return false, fmt.Errorf("evaluate delegable grant: %w", err)
			}
			if !allowed {
				return false, nil
			}
			exclusion, hasExclusion := authz.ExclusionScopeFor(grant.Scope)
			if !hasExclusion {
				continue
			}
			for _, restriction := range policy {
				// Root is deliberately not an exclusion, matching authz's evaluator.
				if !slices.Contains(authz.ScopeImplicationClosure(restriction.Scope), exclusion) {
					continue
				}
				if _, overlaps := intersectSelectors(grant.Selector, restriction.Selector); overlaps {
					return false, nil
				}
			}
		}
	}
	return true, nil
}

func delegationCheck(scope authz.Scope, selector authz.Selector) authz.Check {
	dimensions := maps.Clone(selector)
	delete(dimensions, authz.SelectorKeyResourceKind)
	delete(dimensions, authz.SelectorKeyResourceID)
	return authz.Check{Scope: scope, ResourceKind: selector[authz.SelectorKeyResourceKind], ResourceID: selector[authz.SelectorKeyResourceID], Dimensions: dimensions}.WithStrictSelectorMatch()
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
