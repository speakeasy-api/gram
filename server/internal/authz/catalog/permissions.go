package catalog

import (
	"fmt"
	"slices"

	"github.com/speakeasy-api/gram/server/internal/authz"
)

// Definition declares what one checkable permission means.
type Definition struct {
	Name authz.Scope
	// Resource is the kind of resource a target must name.
	Resource string
	// AllowedAnchors are the boundaries a grant of this permission may name:
	// a concrete resource of kind Resource, or a broader kind such as a whole
	// project. A stored grant anchored more broadly still allows until it is
	// migrated, and decisions label it BasisLegacyBroadGrant.
	AllowedAnchors []string
	// RequiredAncestors are the containing resources a target must carry.
	RequiredAncestors []string
	// ImpliedBy lists the other permissions that also satisfy this one. It is
	// one step deep and explicit; resource ancestry implies nothing.
	ImpliedBy []authz.Scope
	// ExcludedBy is the block whose matching grant withdraws this permission.
	ExcludedBy authz.Scope
	// BlockWins makes every matching block a prohibition, whichever principal
	// holds the allow. Without it a direct user grant on a concrete resource
	// outranks a block inherited from a role or user:all.
	BlockWins bool
}

// mcpServerAncestors are the containers every MCP server target carries.
var mcpServerAncestors = []string{authz.ResourceKindProject, authz.ResourceKindOrg}

// definitions is the registry. Configuring a server (mcp:write) and reading
// its settings (mcp:read) never imply using it (mcp:connect), a connect grant
// names one server, and a matching block always wins.
var definitions = mustValidate([]Definition{
	{
		Name:              authz.ScopeMCPRead,
		Resource:          authz.ResourceKindMCP,
		AllowedAnchors:    []string{authz.ResourceKindMCP, authz.ResourceKindProject, authz.ResourceKindOrg},
		RequiredAncestors: mcpServerAncestors,
		ImpliedBy:         []authz.Scope{authz.ScopeMCPWrite},
		ExcludedBy:        authz.ScopeMCPBlockedRead,
		BlockWins:         true,
	},
	{
		Name:              authz.ScopeMCPWrite,
		Resource:          authz.ResourceKindMCP,
		AllowedAnchors:    []string{authz.ResourceKindMCP, authz.ResourceKindProject, authz.ResourceKindOrg},
		RequiredAncestors: mcpServerAncestors,
		ImpliedBy:         nil,
		ExcludedBy:        authz.ScopeMCPBlockedWrite,
		BlockWins:         true,
	},
	{
		Name:              authz.ScopeMCPConnect,
		Resource:          authz.ResourceKindMCP,
		AllowedAnchors:    []string{authz.ResourceKindMCP},
		RequiredAncestors: mcpServerAncestors,
		ImpliedBy:         nil,
		ExcludedBy:        authz.ScopeMCPBlockedConnect,
		BlockWins:         true,
	},
})

// Definitions returns every registered definition.
func Definitions() []Definition {
	defs := make([]Definition, len(definitions))
	for i, def := range definitions {
		defs[i] = def.clone()
	}
	return defs
}

func (d Definition) clone() Definition {
	d.AllowedAnchors = slices.Clone(d.AllowedAnchors)
	d.RequiredAncestors = slices.Clone(d.RequiredAncestors)
	d.ImpliedBy = slices.Clone(d.ImpliedBy)
	return d
}

// anchoredWithin reports whether a grant selector names a boundary the
// definition allows: a concrete resource, or a broader allowed kind.
func (d Definition) anchoredWithin(selector authz.Selector) bool {
	if selector.ResourceID() != authz.WildcardResource {
		return slices.Contains(d.AllowedAnchors, d.Resource)
	}
	if project := selector[authz.SelectorKeyProjectID]; project != "" && project != authz.WildcardResource {
		return slices.Contains(d.AllowedAnchors, authz.ResourceKindProject)
	}
	return slices.Contains(d.AllowedAnchors, authz.ResourceKindOrg)
}

func mustValidate(defs []Definition) []Definition {
	if err := validate(defs); err != nil {
		panic(err)
	}
	return defs
}

func validate(defs []Definition) error {
	seen := make(map[authz.Scope]bool, len(defs))
	for _, def := range defs {
		if seen[def.Name] {
			return fmt.Errorf("permission %q is defined twice", def.Name)
		}
		seen[def.Name] = true
		if authz.IsBlocklistScope(def.Name) {
			return fmt.Errorf("permission %q is a block, not a checkable permission", def.Name)
		}
		if def.Resource == "" || def.Resource != authz.ResourceKindForScope(def.Name) {
			return fmt.Errorf("permission %q protects %q, not %q", def.Name, authz.ResourceKindForScope(def.Name), def.Resource)
		}
		if !slices.Contains(def.AllowedAnchors, def.Resource) {
			return fmt.Errorf("permission %q cannot be granted on a single %s", def.Name, def.Resource)
		}
		for _, implied := range def.ImpliedBy {
			if authz.ResourceKindForScope(implied) != def.Resource || authz.IsBlocklistScope(implied) {
				return fmt.Errorf("permission %q cannot be implied by %q", def.Name, implied)
			}
		}
		if !authz.IsBlocklistScope(def.ExcludedBy) || authz.ResourceKindForScope(def.ExcludedBy) != def.Resource {
			return fmt.Errorf("permission %q needs a %s block, not %q", def.Name, def.Resource, def.ExcludedBy)
		}
	}
	return nil
}
