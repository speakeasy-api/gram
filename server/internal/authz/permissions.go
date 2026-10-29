package authz

import (
	"fmt"
	"slices"
)

// PermissionDefinition declares what one checkable permission means. It is the
// single source for the permission's implications, the block that withdraws
// it, how that block combines with allows, and the facts a target must carry
// before the permission can be decided.
//
// Only the MCP server family is defined today. Scopes without a definition
// keep the hand-written rules in scopeExpansions, scopeExclusions and
// precedence.go; scopes with one get those rules from here, so the engine,
// the Client, delegation containment and explanations all read the same
// meaning.
type PermissionDefinition struct {
	Name Scope
	// Resource is the kind of resource a target must name.
	Resource string
	// AllowedAnchors are the boundaries a grant of this permission may name:
	// a concrete resource of kind Resource, or a broader kind such as a whole
	// project. A stored grant anchored more broadly than this still allows
	// until it is migrated, and decisions label it LegacyBroadGrant.
	AllowedAnchors []string
	// RequiredAncestors are the containing resources a target must carry,
	// loaded by the service from tenant-scoped records.
	RequiredAncestors []string
	// ImpliedBy lists the other permissions that also satisfy this one. It is
	// one step deep and explicit; resource ancestry implies nothing.
	ImpliedBy []Scope
	// ExcludedBy is the blocklist scope whose matching grant withdraws this
	// permission.
	ExcludedBy Scope
	Precedence BlockPrecedence
}

// BlockPrecedence decides whether a matching block can be outranked.
type BlockPrecedence int

const (
	// BlockPrecedenceDirectGrantOutranks lets a direct user grant naming a
	// concrete resource outrank a block inherited from a role or user:all.
	BlockPrecedenceDirectGrantOutranks BlockPrecedence = iota
	// BlockPrecedenceBlockWins makes every matching block a prohibition,
	// whichever principal holds the allow.
	BlockPrecedenceBlockWins
)

// mcpServerAncestors are the containing resources every MCP server check
// carries: the server's project and organization.
var mcpServerAncestors = []string{ResourceKindProject, ResourceKindOrg}

// permissionDefinitions is the registry. Configuring a server (mcp:write) and
// reading its settings (mcp:read) never imply using it (mcp:connect), and a
// matching block always wins.
var permissionDefinitions = mustValidateDefinitions([]PermissionDefinition{
	{
		Name:              ScopeMCPRead,
		Resource:          ResourceKindMCP,
		AllowedAnchors:    []string{ResourceKindMCP, ResourceKindProject, ResourceKindOrg},
		RequiredAncestors: mcpServerAncestors,
		ImpliedBy:         []Scope{ScopeMCPWrite},
		ExcludedBy:        ScopeMCPBlockedRead,
		Precedence:        BlockPrecedenceBlockWins,
	},
	{
		Name:              ScopeMCPWrite,
		Resource:          ResourceKindMCP,
		AllowedAnchors:    []string{ResourceKindMCP, ResourceKindProject, ResourceKindOrg},
		RequiredAncestors: mcpServerAncestors,
		ImpliedBy:         nil,
		ExcludedBy:        ScopeMCPBlockedWrite,
		Precedence:        BlockPrecedenceBlockWins,
	},
	{
		Name:              ScopeMCPConnect,
		Resource:          ResourceKindMCP,
		AllowedAnchors:    []string{ResourceKindMCP},
		RequiredAncestors: mcpServerAncestors,
		ImpliedBy:         nil,
		ExcludedBy:        ScopeMCPBlockedConnect,
		Precedence:        BlockPrecedenceBlockWins,
	},
})

// AllDefinitions returns every registered permission definition.
func AllDefinitions() []PermissionDefinition {
	defs := make([]PermissionDefinition, len(permissionDefinitions))
	for i, def := range permissionDefinitions {
		defs[i] = def.clone()
	}
	return defs
}

func definitionFor(scope Scope) (PermissionDefinition, bool) {
	for _, def := range permissionDefinitions {
		if def.Name == scope {
			return def, true
		}
	}
	return PermissionDefinition{}, false
}

func (d PermissionDefinition) clone() PermissionDefinition {
	d.AllowedAnchors = slices.Clone(d.AllowedAnchors)
	d.RequiredAncestors = slices.Clone(d.RequiredAncestors)
	d.ImpliedBy = slices.Clone(d.ImpliedBy)
	return d
}

// anchoredWithin reports whether a grant selector names a boundary the
// definition allows: a concrete resource, or a broader allowed kind.
func (d PermissionDefinition) anchoredWithin(selector Selector) bool {
	if selector.ResourceID() != WildcardResource {
		return slices.Contains(d.AllowedAnchors, d.Resource)
	}
	if selector[SelectorKeyProjectID] != "" && selector[SelectorKeyProjectID] != WildcardResource {
		return slices.Contains(d.AllowedAnchors, ResourceKindProject)
	}
	return slices.Contains(d.AllowedAnchors, ResourceKindOrg)
}

func mustValidateDefinitions(defs []PermissionDefinition) []PermissionDefinition {
	if err := validateDefinitions(defs); err != nil {
		panic(err)
	}
	return defs
}

func validateDefinitions(defs []PermissionDefinition) error {
	seen := make(map[Scope]bool, len(defs))
	for _, def := range defs {
		if seen[def.Name] {
			return fmt.Errorf("permission %q is defined twice", def.Name)
		}
		seen[def.Name] = true
		if IsBlocklistScope(def.Name) {
			return fmt.Errorf("permission %q is a block, not a checkable permission", def.Name)
		}
		if def.Resource == "" || def.Resource != ResourceKindForScope(def.Name) {
			return fmt.Errorf("permission %q protects %q, not %q", def.Name, ResourceKindForScope(def.Name), def.Resource)
		}
		if !slices.Contains(def.AllowedAnchors, def.Resource) {
			return fmt.Errorf("permission %q cannot be granted on a single %s", def.Name, def.Resource)
		}
		for _, implied := range def.ImpliedBy {
			if ResourceKindForScope(implied) != def.Resource || IsBlocklistScope(implied) {
				return fmt.Errorf("permission %q cannot be implied by %q", def.Name, implied)
			}
		}
		if !IsBlocklistScope(def.ExcludedBy) || ResourceKindForScope(def.ExcludedBy) != def.Resource {
			return fmt.Errorf("permission %q needs a %s block, not %q", def.Name, def.Resource, def.ExcludedBy)
		}
	}
	return nil
}

// withDefinedExpansions replaces the expansions of every defined permission
// with the ones its definition declares.
func withDefinedExpansions(expansions map[Scope][]Scope) map[Scope][]Scope {
	for _, def := range permissionDefinitions {
		expansions[def.Name] = slices.Clone(def.ImpliedBy)
	}
	return expansions
}

// withDefinedExclusions replaces the exclusion of every defined permission
// with the one its definition declares.
func withDefinedExclusions(exclusions map[Scope]Scope) map[Scope]Scope {
	for _, def := range permissionDefinitions {
		exclusions[def.Name] = def.ExcludedBy
	}
	return exclusions
}
