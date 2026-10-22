package authz

import (
	"context"
	"fmt"
	"slices"
)

// GrantSourceKind distinguishes assignment origins without making them actors.
type GrantSourceKind string

const (
	// UserGrantSource holds one user's directly assigned policy.
	UserGrantSource GrantSourceKind = "user"

	// AgentGrantSource holds one agent's directly assigned policy.
	AgentGrantSource GrantSourceKind = "agent"

	// RoleGrantSource holds policy inherited through an explicit role assignment.
	RoleGrantSource GrantSourceKind = "role"

	// AllUsersGrantSource holds policy applying to users, never automatically agents.
	AllUsersGrantSource GrantSourceKind = "all_users"
)

// GrantSource identifies a tenant-bound assignment origin, not an execution subject.
type GrantSource struct {
	// OrganizationID prevents assignments crossing tenant boundaries.
	OrganizationID string

	// Kind identifies a direct user, direct agent, role, or all-users source.
	Kind GrantSourceKind

	// ID names a concrete user, agent, or role; all-users sources leave it empty.
	ID string
}

// ResolvedGrant keeps policy provenance beside the immutable grant value.
type ResolvedGrant struct {
	// Source records where this assignment originated, not who is executing.
	Source GrantSource

	// Grant contains the assigned scope, anchor, and constraints.
	Grant Grant
}

// ResolvedPolicy combines eligible sources for exactly one execution principal.
type ResolvedPolicy struct {
	// Principal is the actor or delegator bounded by this policy.
	Principal Principal

	// Grants retains provenance even when identical grants occur in several sources.
	Grants []ResolvedGrant
}

// PolicyResolution is a detached snapshot for one decision, not reusable authority.
type PolicyResolution struct {
	// Policies keeps actor and delegator authority independent and in that order.
	Policies []ResolvedPolicy

	// Revision identifies the coherent in-memory admission and policy state.
	Revision uint64
}

// intersect evaluates the complete operation independently for each policy.
// Each policy interprets its alternatives before intersection; grants are never
// merged across principals to assemble an allow that neither policy permits.
func (s PolicyResolution) intersect(decide func([]ResolvedGrant) Decision) Decision {
	if len(s.Policies) == 0 {
		return Decision{Outcome: Denied, Reason: "no execution policy", PolicyRevision: s.Revision}
	}
	result := Decision{Outcome: Allowed, Reason: "all execution policies permit the operation", PolicyRevision: s.Revision, Exclusion: NoExclusion}
	for i, policy := range s.Policies {
		decision := decide(policy.Grants)
		if decision.Outcome != Allowed {
			label := "principal policy"
			if i > 0 {
				label = "delegator policy"
			}
			decision.Reason = label + ": " + decision.Reason
			if decision.Outcome == Denied {
				return decision
			}
			result = decision
		}
	}
	return result
}

// PolicyResolver admits the execution and resolves every bound policy coherently.
// Implementations must not load actor and delegator from unrelated snapshots.
type PolicyResolver interface {
	// ResolvePolicies returns copied policy with assignment provenance.
	ResolvePolicies(context.Context, Subject) (PolicyResolution, error)
}

// PolicyWriter persists assignments after separate domain write authorization.
type PolicyWriter interface {
	// ReplaceSourceGrants replaces one source, including an empty revocation.
	ReplaceSourceGrants(context.Context, GrantSource, []Grant) error
}

// RoleAssignmentWriter persists domain-authorized role membership changes.
type RoleAssignmentWriter interface {
	// ReplaceRoles replaces the explicit role sources of one principal.
	ReplaceRoles(context.Context, Principal, []GrantSource) error
}

func directGrantSource(principal Principal) GrantSource {
	kind := UserGrantSource
	if principal.Kind == AgentPrincipal {
		kind = AgentGrantSource
	}
	return GrantSource{OrganizationID: principal.OrganizationID, Kind: kind, ID: principal.ID}
}

func validateGrantSource(source GrantSource) error {
	if source.OrganizationID == "" || source.OrganizationID == "*" {
		return fmt.Errorf("grant source requires a concrete organization")
	}
	switch source.Kind {
	case UserGrantSource, AgentGrantSource, RoleGrantSource:
		if source.ID == "" || source.ID == "*" {
			return fmt.Errorf("grant source requires a concrete ID")
		}
	case AllUsersGrantSource:
		if source.ID != "" {
			return fmt.Errorf("all-users source must not name an individual")
		}
	default:
		return fmt.Errorf("unknown grant source kind %q", source.Kind)
	}
	return nil
}

// ReplaceSourceGrants validates assignment shape, not permission to edit policy.
func (c *Client) ReplaceSourceGrants(ctx context.Context, source GrantSource, grants []Grant) error {
	if err := validateGrantSource(source); err != nil {
		return err
	}
	if err := c.validateGrants(source.OrganizationID, grants); err != nil {
		return err
	}
	writer, ok := c.backend.(PolicyWriter)
	if !ok {
		return fmt.Errorf("authorization backend does not support policy writes")
	}
	return writer.ReplaceSourceGrants(ctx, source, slices.Clone(grants))
}

// ReplaceRoles validates membership shape; a domain must authorize the edit and
// establish eligibility. Roles confer only their assigned grants, never defaults.
func (c *Client) ReplaceRoles(ctx context.Context, principal Principal, roles []GrantSource) error {
	if err := validatePrincipal(principal); err != nil {
		return err
	}
	seen := make(map[GrantSource]bool, len(roles))
	for _, role := range roles {
		if err := validateGrantSource(role); err != nil {
			return err
		}
		if role.Kind != RoleGrantSource || role.OrganizationID != principal.OrganizationID {
			return fmt.Errorf("role assignment requires a role in the principal's organization")
		}
		if seen[role] {
			return fmt.Errorf("duplicate role assignment")
		}
		seen[role] = true
	}
	writer, ok := c.backend.(RoleAssignmentWriter)
	if !ok {
		return fmt.Errorf("authorization backend does not support role assignment writes")
	}
	return writer.ReplaceRoles(ctx, principal, slices.Clone(roles))
}
