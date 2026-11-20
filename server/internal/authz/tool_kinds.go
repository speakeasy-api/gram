package authz

import (
	"maps"
	"slices"
)

// Dispositions returns every disposition a tool call can carry, sorted.
func Dispositions() []string {
	return slices.Sorted(maps.Keys(validDispositions))
}

// ToolKindAccess is what a set of grants does for tools of each disposition on
// one MCP server.
type ToolKindAccess struct {
	// Allowed lists the dispositions of the tools the grants let a caller use.
	Allowed []string
	// Blocked lists the dispositions the grants' own blocks take away, even
	// from someone who is given the server by another rule.
	Blocked []string
}

// unnamedToolProbe is the tool name ToolKindsOnServer asks about. No grant
// names it, so only rules for a whole server or a kind of tool apply.
const unnamedToolProbe = "authz-unnamed-tool-probe"

// ToolKindsOnServer asks the evaluator, for each disposition, about a call to
// a tool no grant names on server. For example, a role with connect limited to
// read-only tools and a block on destructive tools gets Allowed [read_only]
// and Blocked [destructive]. A block is found by asking again as if the
// caller were also given every server, so a role that only blocks still
// reports its blocks.
func ToolKindsOnServer(grants []Grant, server MCPServerRef) (ToolKindAccess, error) {
	givenEveryServer := append(slices.Clone(grants), Grant{
		PrincipalUrn: "",
		Scope:        ScopeMCPConnect,
		Selector:     NewSelector(ScopeMCPConnect, WildcardResource),
		Inferred:     false,
	})

	access := ToolKindAccess{Allowed: []string{}, Blocked: []string{}}
	for _, disposition := range Dispositions() {
		check := server.ToolCall(unnamedToolProbe, disposition)
		allowed, err := GrantsAuthorize(grants, check)
		if err != nil {
			return ToolKindAccess{}, err
		}
		if allowed {
			access.Allowed = append(access.Allowed, disposition)
		}
		allowedElsewhere, err := GrantsAuthorize(givenEveryServer, check)
		if err != nil {
			return ToolKindAccess{}, err
		}
		if !allowedElsewhere {
			access.Blocked = append(access.Blocked, disposition)
		}
	}
	return access, nil
}
