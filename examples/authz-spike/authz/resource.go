// Package authz is an isolated in-memory authorization design spike.
// It is not wired into production services.
//
// This file describes what an operation accesses and where that resource belongs;
// it does not decide whether access is allowed. ResourceKind distinguishes resource
// families, and ResourceRef identifies one concrete resource within an organization.
// Target combines that resource with its enclosing boundaries (ancestors).
//
// For a server in a project, ancestry lets the same mcp:connect scope match a grant
// on the server, its project, or its organization when permission definitions allow
// those anchors. It does not make project:read imply MCP access: containment and
// permission implication are separate concepts.
//
// The application's resource adapter supplies targets from trusted, loaded data.
// At the client boundary, snapshotTarget rejects missing fields, wildcards,
// cross-organization ancestors, and repeated references. Every non-organization
// target requires exactly one canonical organization ancestor; an organization
// target names that organization itself and has no ancestors. The function then
// copies the ancestor slice: an input snapshot, not a transaction or policy snapshot.
//
// Structural validation cannot prove that a server really belongs to the supplied
// project; that remains the application's organization-scoped lookup responsibility.
// Permission-specific ancestry requirements are validated separately by the client.
package authz

import "fmt"

// ResourceKind identifies a policy boundary, independently of permissions.
type ResourceKind string

const (
	// Organization is the tenant boundary for all resources in this spike.
	Organization ResourceKind = "organization"

	// Project groups resources without automatically granting child permissions.
	Project ResourceKind = "project"

	// MCPServer represents a server's management and tool-use boundary.
	MCPServer ResourceKind = "mcp"

	// Environment protects environment metadata and secret-bearing operations.
	Environment ResourceKind = "environment"

	// Skill protects a published skill.
	Skill ResourceKind = "skill"

	// Assistant protects configuration and interaction with an assistant.
	Assistant ResourceKind = "assistant"

	// Chat protects conversation metadata, content, and mutations.
	Chat ResourceKind = "chat"

	// Plugin protects package content, not execution of its members.
	Plugin ResourceKind = "plugin"

	// Agent protects an agent identity's independent management operations.
	Agent ResourceKind = "agent"
)

// ResourceRef names one resource within a tenant.
type ResourceRef struct {
	// OrganizationID prevents matching identical resource IDs across tenants.
	OrganizationID string

	// Kind identifies which resource family the reference belongs to.
	Kind ResourceKind

	// ID is a concrete resource identifier, never an implicit wildcard.
	ID string
}

// Target contains identity and ancestry obtained from a trusted resource store.
// Structural validation cannot prove that caller-supplied ancestry is truthful.
type Target struct {
	// Resource identifies what the operation acts on.
	Resource ResourceRef

	// Ancestors come from trusted resource data and enable broader grant matching.
	// They do not supply unrelated permissions.
	Ancestors []ResourceRef
}

// snapshotTarget validates and copies target data at the client boundary.
func snapshotTarget(input Target) (Target, error) {
	resource, ancestors := input.Resource, input.Ancestors
	if err := validateRef(resource); err != nil {
		return Target{}, fmt.Errorf("target resource: %w", err)
	}

	seen := map[ResourceRef]bool{resource: true}
	organizations := 0
	for _, ancestor := range ancestors {
		if err := validateRef(ancestor); err != nil {
			return Target{}, fmt.Errorf("target ancestor: %w", err)
		}
		if ancestor.OrganizationID != resource.OrganizationID {
			return Target{}, fmt.Errorf("target ancestor belongs to a different organization")
		}
		if seen[ancestor] {
			return Target{}, fmt.Errorf("target ancestry contains a repeated resource")
		}
		seen[ancestor] = true
		if ancestor.Kind == Organization {
			organizations++
		}
	}
	if resource.Kind == Organization {
		if len(ancestors) != 0 {
			return Target{}, fmt.Errorf("organization targets cannot have ancestors")
		}
	} else if organizations != 1 {
		return Target{}, fmt.Errorf("target requires exactly one organization ancestor")
	}

	return Target{Resource: resource, Ancestors: append([]ResourceRef(nil), ancestors...)}, nil
}

func validateRef(ref ResourceRef) error {
	if ref.OrganizationID == "" || ref.Kind == "" || ref.ID == "" {
		return fmt.Errorf("organization, kind, and resource ID are required")
	}
	if ref.OrganizationID == "*" || ref.Kind == "*" || ref.ID == "*" {
		return fmt.Errorf("resource references must be concrete")
	}
	if ref.Kind == Organization && ref.ID != ref.OrganizationID {
		return fmt.Errorf("organization resource ID must equal its organization ID")
	}
	return nil
}
