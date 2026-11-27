package authz

import (
	"maps"
	"slices"
)

// unnamedValue stands in for a resource, project or tool that a grant leaves
// unbounded. No grant or block names it, so only a wildcard or unbounded
// block reaches it.
const unnamedValue = "unnamed-resource"

// GrantsReachSomeResource reports whether grants let the caller use scope on
// at least one resource: some allow grant reaches a resource that no block
// takes away. Org scopes are asked about organizationID only.
//
// For example, mcp:connect on every server of project P with a block on
// server S still reaches the other servers of P, so it holds. A block on
// every server takes the grant away, so it does not.
func GrantsReachSomeResource(grants []Grant, scope Scope, organizationID string) bool {
	for _, grant := range grants {
		if grant.Scope != ScopeRoot && !slices.Contains(ScopeImplicationClosure(grant.Scope), scope) {
			continue
		}
		for _, check := range checksReachedBy(grant, scope, organizationID) {
			allowed, err := GrantsAuthorize(grants, check)
			if err == nil && allowed {
				return true
			}
		}
	}
	return false
}

// checksReachedBy returns concrete checks for resources grant reaches under
// scope, with unnamedValue where the grant is unbounded. It returns several
// when the grant could reach a tool of any disposition, and none when the
// grant limits tools under a scope no tool call uses.
func checksReachedBy(grant Grant, scope Scope, organizationID string) []Check {
	resourceID := grant.Selector.ResourceID()
	if scope.Parts().Resource == "org" {
		resourceID = organizationID
	} else if resourceID == "" || resourceID == WildcardResource {
		resourceID = unnamedValue
	}
	dimensions := maps.Clone(grant.Selector)
	delete(dimensions, SelectorKeyResourceKind)
	delete(dimensions, SelectorKeyResourceID)
	for _, key := range requiredCheckDimensions[ResourceKindForScope(scope)] {
		if value := dimensions[key]; value == "" || value == WildcardResource {
			if dimensions == nil {
				dimensions = map[string]string{}
			}
			dimensions[key] = unnamedValue
		}
	}
	for _, key := range []string{SelectorKeyTool, SelectorKeyDisposition} {
		if dimensions[key] == WildcardResource {
			delete(dimensions, key)
		}
	}

	tool, hasTool := dimensions[SelectorKeyTool]
	disposition, hasDisposition := dimensions[SelectorKeyDisposition]
	if !hasTool && !hasDisposition {
		if scope == ScopeMCPConnect {
			// A connection leaves the tool and disposition out, so it still
			// matches a grant that limits neither.
			return []Check{MCPServer(MCPServerIDFrom(resourceID), ProjectIDFrom(dimensions[SelectorKeyProjectID])).Connect()}
		}
		return []Check{{Scope: scope, ResourceKind: "", ResourceID: resourceID, Dimensions: dimensions, exact: false, representedUser: "", probe: false, group: nil, operation: operationDefault}}
	}
	if scope != ScopeMCPConnect {
		return nil
	}
	// A block on a tool or disposition covers a call that leaves that value
	// out, so each check names both. A grant that names no disposition reaches
	// its tools whatever their annotations, so every disposition is tried.
	if !hasTool {
		tool = unnamedValue
	}
	dispositions := []string{disposition}
	if !hasDisposition {
		dispositions = Dispositions()
	}
	server := MCPServer(MCPServerIDFrom(resourceID), ProjectIDFrom(dimensions[SelectorKeyProjectID]))
	checks := make([]Check, 0, len(dispositions))
	for _, d := range dispositions {
		checks = append(checks, server.ToolCall(tool, d))
	}
	return checks
}
