package authz

import (
	"context"
	"fmt"
	"maps"
	"slices"
)

// AgentAccess is a request-local adapter for delegated agent permissions. The
// caller supplies authenticated identity, live grants, and persisted ownership;
// authentication, membership, database locks, and intrinsic owner rights stay
// outside this adapter. Its zero value denies every operation.
type AgentAccess struct {
	// grants holds an isolated snapshot with caller-relative selectors bound.
	grants []Grant
}

type agentAccessEngine interface {
	EvaluateLoadedGrants(context.Context, []Grant, ...Check) error
	EvaluateLoadedAuthority(context.Context, []Grant, Scope, Selector) error
}

// NewAgentAccess binds caller-relative owner selectors once for this operation.
// Empty or wildcard caller identities cannot produce delegated authority.
func NewAgentAccess(callerUserID string, grants []Grant) AgentAccess {
	if callerUserID == "" || callerUserID == WildcardResource {
		return AgentAccess{grants: nil}
	}
	grants = slices.Clone(grants)
	for i := range grants {
		grants[i].Selector = maps.Clone(grants[i].Selector)
	}
	return AgentAccess{grants: BindSelectorValue(grants, SelectorKeyOwner, AgentOwnerSelf, callerUserID)}
}

// Allows reports delegated authority over an existing agent. The owner must be
// the current persisted owner, not a caller-supplied ownership claim.
func (a AgentAccess) Allows(scope Scope, agentID, ownerUserID string) bool {
	if ownerUserID == "" || ownerUserID == WildcardResource {
		return false
	}
	check := AgentCheck(scope, agentID, ownerUserID)
	if err := validateInput(check); err != nil {
		return false
	}
	return GrantsSatisfy(a.grants, check)
}

// Require authorizes an existing-agent operation through the ordinary engine
// decision path. Even unrestricted grants cannot authorize a missing owner.
func (a AgentAccess) Require(ctx context.Context, engine agentAccessEngine, scope Scope, agentID, ownerUserID string) error {
	grants := a.grants
	if ownerUserID == "" || ownerUserID == WildcardResource {
		grants = nil
	}
	if err := engine.EvaluateLoadedGrants(ctx, grants, AgentCheck(scope, agentID, ownerUserID)); err != nil {
		return fmt.Errorf("authorize existing agent: %w", err)
	}
	return nil
}

// CanCreate is the capability projection of RequireCreate. Agent write has no
// exclusion scope; adding one disables this projection until exclusion-aware
// containment is implemented. A concrete-agent grant never permits creation.
func (a AgentAccess) CanCreate(ownerUserID string) bool {
	if ownerUserID == "" || ownerUserID == WildcardResource {
		return false
	}
	if _, hasExclusion := ExclusionScopeFor(ScopeAgentWrite); hasExclusion {
		return false
	}
	return GrantsContainSelector(a.grants, ScopeAgentWrite, AgentOwnerSelector(ScopeAgentWrite, ownerUserID))
}

// RequireCreate authorizes creation for an intended owner and records the engine
// decision. It requires authority over all of that owner's agents, not one ID.
// The engine refuses scopes with exclusions instead of treating allow-only
// containment as complete effective authorization.
func (a AgentAccess) RequireCreate(ctx context.Context, engine agentAccessEngine, ownerUserID string) error {
	grants := a.grants
	if ownerUserID == "" || ownerUserID == WildcardResource {
		grants = nil
	}
	if err := engine.EvaluateLoadedAuthority(ctx, grants, ScopeAgentWrite, AgentOwnerSelector(ScopeAgentWrite, ownerUserID)); err != nil {
		return fmt.Errorf("authorize agent creation: %w", err)
	}
	return nil
}

// CreationOwners compiles a bounded-owner filter for directory queries. The
// application still validates active membership and applies database pagination.
func (a AgentAccess) CreationOwners() (all bool, ownerIDs []string) {
	if _, hasExclusion := ExclusionScopeFor(ScopeAgentWrite); hasExclusion {
		return false, []string{}
	}
	if GrantsContainSelector(a.grants, ScopeAgentWrite, AgentOwnerSelector(ScopeAgentWrite, WildcardResource)) {
		return true, []string{}
	}
	owners := make([]string, 0)
	for _, grant := range a.grants {
		owner := grant.Selector[SelectorKeyOwner]
		if owner == "" || slices.Contains(owners, owner) || !a.CanCreate(owner) {
			continue
		}
		owners = append(owners, owner)
	}
	return false, owners
}
