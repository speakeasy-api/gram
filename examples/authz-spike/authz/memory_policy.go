package authz

import (
	"context"
	"slices"
)

var (
	_ PolicyResolver       = (*MemoryBackend)(nil)
	_ PolicyWriter         = (*MemoryBackend)(nil)
	_ RoleAssignmentWriter = (*MemoryBackend)(nil)
	_ CredentialWriter     = (*MemoryBackend)(nil)
)

// ReplaceSourceGrants atomically replaces one source; no defaults are synthesized.
// The client validates shape and the domain separately authorizes the editor.
func (b *MemoryBackend) ReplaceSourceGrants(ctx context.Context, source GrantSource, grants []Grant) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	b.policies[source] = slices.Clone(grants)
	b.revision++
	return nil
}

// ReplaceRoles atomically replaces one principal's explicit memberships.
// Caller eligibility and mutation authority are domain responsibilities.
func (b *MemoryBackend) ReplaceRoles(ctx context.Context, principal Principal, roles []GrantSource) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	b.roles[principal] = slices.Clone(roles)
	b.revision++
	return nil
}

// ResolvePolicies admits once and copies binding, role membership, and grants
// under one lock. Copies remain coherent after the lock is released but are not
// proof of freshness for a later operation. No grants are installed in context.
func (b *MemoryBackend) ResolvePolicies(ctx context.Context, subject Subject) (PolicyResolution, error) {
	if err := ctx.Err(); err != nil {
		return PolicyResolution{}, err
	}
	b.mu.RLock()
	defer b.mu.RUnlock()
	if err := ctx.Err(); err != nil {
		return PolicyResolution{}, err
	}
	execution, err := b.admitExecutionLocked(subject)
	if err != nil {
		return PolicyResolution{}, err
	}
	policies := make([]ResolvedPolicy, 0, len(execution.principals))
	for _, principal := range execution.principals {
		policies = append(policies, b.resolvePolicyLocked(principal))
	}
	return PolicyResolution{Policies: policies, Revision: b.revision}, nil
}

// resolvePolicyLocked combines only this principal's eligible sources. The caller
// must hold b.mu throughout admission and resolution of every bound principal.
func (b *MemoryBackend) resolvePolicyLocked(principal Principal) ResolvedPolicy {
	sources := []GrantSource{directGrantSource(principal)}
	sources = append(sources, b.roles[principal]...)
	if principal.Kind == UserPrincipal {
		sources = append(sources, GrantSource{OrganizationID: principal.OrganizationID, Kind: AllUsersGrantSource, ID: ""})
	}
	grants := []ResolvedGrant{}
	for _, source := range sources {
		for _, grant := range b.policies[source] {
			grants = append(grants, ResolvedGrant{Source: source, Grant: grant})
		}
	}
	return ResolvedPolicy{Principal: principal, Grants: grants}
}
