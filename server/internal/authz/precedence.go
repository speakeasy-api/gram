package authz

import (
	"fmt"
	"strings"

	"github.com/speakeasy-api/gram/server/internal/urn"
)

// Principal precedence for blocklist exclusions.
//
// A caller's grants come from principals of different specificity: the
// organization-wide user:all, the roles they hold, and the user or agent
// itself. Allows from every principal combine, and a blocklist exclusion
// (*:blocked_*) from any of them normally withdraws the matching allow.
//
// One case is decided by specificity instead: a grant made directly to the
// user that names a concrete resource outranks a block that reaches them only
// through a role or user:all. An administrator who blocks a server for a
// directory-synced role can still give one member of that role access to it
// by name, without changing role membership.
//
// Everything else keeps block-wins semantics:
//   - A block held by the user itself still beats its own grants.
//   - A direct grant spanning every resource (resource_id "*") does not
//     override, so a broad user grant can still be revoked on one resource by
//     a role or organization-wide block.
//   - Agent grants never override. An agent's owner can write its policy
//     without being an administrator, so letting those grants outrank a block
//     would let owners lift a block an administrator put on the agent's role
//     or on everyone.
//   - Non-blocklist exclusions such as risk_policy:bypass are unaffected: a
//     bypass exempts a principal from a policy rather than taking access away.
//
// A narrowed direct grant overrides only as far as it reaches. A direct grant
// for one tool proves server-level access and that tool, while every other
// tool stays blocked. For a tool call, the direct grant must name every
// dimension it constrains: a disposition-scoped grant does not reach a tool
// whose annotations yield no disposition.

// IsDirectGrant reports whether grant was made to one user by name rather than
// reaching them through a role or user:all. Only these grants can outrank an
// inherited blocklist exclusion; agent grants never do.
func IsDirectGrant(grant Grant) bool {
	principal, err := urn.ParsePrincipal(grant.PrincipalUrn)
	if err != nil {
		return false
	}
	return principal.Type == urn.PrincipalTypeUser && principal.ID != urn.AllUsersPrincipalID
}

// DirectOverrideGrants returns the subset of grants that can outrank a block
// inherited from a role or user:all: every direct allow naming a concrete
// resource, plus every direct exclusion, so the principal's own blocks still
// withdraw its own allows.
func DirectOverrideGrants(grants []Grant) []Grant {
	var direct []Grant
	for _, grant := range grants {
		if !isDirectOverrideGrant(grant) {
			continue
		}
		direct = append(direct, grant)
	}
	return direct
}

// isDirectOverrideGrant reports whether grant takes part in direct override
// evaluation: a direct exclusion, or a direct allow naming a concrete resource.
func isDirectOverrideGrant(grant Grant) bool {
	if !IsDirectGrant(grant) {
		return false
	}
	return IsBlocklistScope(grant.Scope) || grant.Selector.ResourceID() != WildcardResource
}

// ExclusionYieldsToDirectGrants reports whether a direct concrete grant can
// outrank the exclusion paired with scope when that exclusion is inherited.
func ExclusionYieldsToDirectGrants(scope Scope) bool {
	exclusion, ok := ExclusionScopeFor(scope)
	return ok && IsBlocklistScope(exclusion)
}

// IsBlocklistScope reports whether scope is one of the *:blocked_* scopes.
func IsBlocklistScope(scope Scope) bool {
	return strings.HasPrefix(scope.Parts().Action, "blocked_")
}

// directOverride returns the direct grant that authorizes check despite a
// matching exclusion inherited from a role or user:all, or nil when the
// exclusion stands.
func directOverride(grants []Grant, check Check) (*Grant, *Check, error) {
	if !ExclusionYieldsToDirectGrants(check.Scope) {
		return nil, nil, nil
	}
	check = directOverrideCheck(check)
	expression := expressionForCheck(check)
	if expression == nil {
		return nil, nil, nil
	}
	direct := DirectOverrideGrants(grants)
	if len(direct) == 0 {
		return nil, nil, nil
	}
	result, err := expression.Evaluate(direct)
	if err != nil {
		return nil, nil, fmt.Errorf("evaluate direct grants: %w", err)
	}
	if !result.Satisfied {
		return nil, nil, nil
	}
	grant, matchedCheck := matchingGrant(direct, check.expand())
	return grant, matchedCheck, nil
}

// directOverrideCheck returns the check a direct grant must satisfy to outrank
// an inherited exclusion. A server-level check is proven by any direct grant on
// the server, but a tool call only by one whose every constraint the call
// carries. Loose matching would let a disposition-scoped grant reach a tool
// with no derivable disposition, past a block that otherwise covers it.
func directOverrideCheck(check Check) Check {
	if _, toolCall := check.Dimensions[SelectorKeyTool]; toolCall {
		return check.WithStrictSelectorMatch()
	}
	return check
}
