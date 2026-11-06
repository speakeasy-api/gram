package authz

const (
	// MCPConnect grants connection and tool use within its constraints.
	MCPConnect Permission = "mcp:connect"

	// MCPRead grants inspection and implies connection permission.
	MCPRead Permission = "mcp:read"

	// MCPWrite grants management and implies inspection and connection.
	MCPWrite Permission = "mcp:write"

	// MCPBlockedConnect opposes connection and tool-use permission.
	MCPBlockedConnect Permission = "mcp:blocked_connect"

	// MCPBlockedRead opposes inspection permission.
	MCPBlockedRead Permission = "mcp:blocked_read"

	// MCPBlockedWrite opposes management permission.
	MCPBlockedWrite Permission = "mcp:blocked_write"
)

// MCPDefinitions returns ordered declarations, indexed once during client setup.
// The spike models organization-wide MCP grants with an organization anchor.
// Project ancestry never makes project:read imply an MCP permission.
func MCPDefinitions() []PermissionDefinition {
	return []PermissionDefinition{
		{
			Name: MCPConnect, Resource: MCPServer,
			AllowedConstraints: []ConstraintKind{ToolConstraint, DispositionConstraint},
			AllowedAnchors:     []ResourceKind{MCPServer, Project, Organization},
			ImpliedBy:          []Permission{MCPRead, MCPWrite},
			ExcludedBy:         []Permission{MCPBlockedConnect},
			Forms:              []OperationForm{ToolOperation},
			RequiredAncestors:  []ResourceKind{Project},
			CreationParent:     "", Discoverable: true,
		},
		{
			Name: MCPRead, Resource: MCPServer,
			AllowedConstraints: []ConstraintKind{ToolConstraint, DispositionConstraint},
			AllowedAnchors:     []ResourceKind{MCPServer, Project, Organization},
			ImpliedBy:          []Permission{MCPWrite},
			ExcludedBy:         []Permission{MCPBlockedRead},
			Forms:              []OperationForm{ResourceOperation},
			RequiredAncestors:  []ResourceKind{Project},
			CreationParent:     "", Discoverable: false,
		},
		{
			Name: MCPWrite, Resource: MCPServer,
			AllowedConstraints: []ConstraintKind{ToolConstraint, DispositionConstraint},
			AllowedAnchors:     []ResourceKind{MCPServer, Project, Organization},
			ImpliedBy:          nil, ExcludedBy: []Permission{MCPBlockedWrite},
			Forms:             []OperationForm{ResourceOperation, CreationOperation},
			RequiredAncestors: []ResourceKind{Project},
			CreationParent:    Project, Discoverable: false,
		},
		{
			Name: MCPBlockedConnect, Resource: MCPServer,
			AllowedConstraints: []ConstraintKind{ToolConstraint, DispositionConstraint},
			AllowedAnchors:     []ResourceKind{MCPServer, Project, Organization},
			ImpliedBy:          nil, ExcludedBy: nil, Forms: nil,
			RequiredAncestors: []ResourceKind{Project},
			CreationParent:    "", Discoverable: false,
		},
		{
			Name: MCPBlockedRead, Resource: MCPServer,
			AllowedConstraints: []ConstraintKind{ToolConstraint, DispositionConstraint},
			AllowedAnchors:     []ResourceKind{MCPServer, Project, Organization},
			ImpliedBy:          nil, ExcludedBy: nil, Forms: nil,
			RequiredAncestors: []ResourceKind{Project},
			CreationParent:    "", Discoverable: false,
		},
		{
			Name: MCPBlockedWrite, Resource: MCPServer,
			AllowedConstraints: []ConstraintKind{ToolConstraint, DispositionConstraint},
			AllowedAnchors:     []ResourceKind{MCPServer, Project, Organization},
			ImpliedBy:          nil, ExcludedBy: nil, Forms: nil,
			RequiredAncestors: []ResourceKind{Project},
			CreationParent:    "", Discoverable: false,
		},
	}
}
