package authz

import "slices"

// ToolKindAccess lists the dispositions a set of grants allows on one MCP
// server, and the ones its blocks take away.
type ToolKindAccess struct {
	Allowed []string
	Blocked []string
}

// No grant names this tool, so only rules for a whole server or a kind of tool
// apply to it.
const unnamedToolProbe = "authz-unnamed-tool-probe"

// ToolKindsOnServer asks the evaluator about each disposition on server. Blocks
// are found by asking again as if the caller were also given every server, so a
// role that only blocks still reports them. For example, connect limited to
// read-only tools plus a block on destructive tools gives Allowed [read_only]
// and Blocked [destructive].
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
