package authz

// This file holds the hand-written half of the typed requests generated in
// refs_gen.go: the helper every ref method builds its check with, and the
// requests that need more than a resource and an action.

func newRefCheck(scope Scope, resourceID string, dimensions map[string]string, match MatchMode) Check {
	mode := selectorMatchNormal
	if match == MatchStrict {
		mode = selectorMatchStrict
	}
	return Check{Scope: scope, ResourceKind: "", ResourceID: resourceID, Dimensions: dimensions, selectorMatch: mode, representedUser: ""}
}

// ToolCall requests mcp:connect for calling one tool on the server. A tool
// or disposition that is empty is carried as unknown (see MCPToolCallCheck).
func (r MCPServerRef) ToolCall(tool, disposition string) Check {
	return MCPToolCallCheck(string(r.id), MCPToolCallDimensions{Tool: tool, Disposition: disposition, ProjectID: string(r.project)})
}

// Collections name the resources of one kind inside a project, for requests
// that have no resource yet. Create is stored as the kind's write permission
// on the project id, as it always has been, so existing grants keep working.

// MCPServerCollection names the MCP servers of one project.
type MCPServerCollection struct{ project ProjectID }

// MCPServers names the MCP servers of the project.
func (r ProjectRef) MCPServers() MCPServerCollection { return MCPServerCollection{project: r.id} }

// Create requests creating an MCP server in the project (mcp:write on the
// project).
func (c MCPServerCollection) Create() Check {
	return MCPServer(MCPServerID(c.project), c.project).Write()
}

// EnvironmentCollection names the environments of one project.
type EnvironmentCollection struct{ project ProjectID }

// Environments names the environments of the project.
func (r ProjectRef) Environments() EnvironmentCollection {
	return EnvironmentCollection{project: r.id}
}

// Create requests creating an environment in the project
// (environment:write on the project).
func (c EnvironmentCollection) Create() Check {
	return Environment(EnvironmentID(c.project), c.project).Write()
}

// AssistantCollection names the assistants of one project.
type AssistantCollection struct{ project ProjectID }

// Assistants names the assistants of the project.
func (r ProjectRef) Assistants() AssistantCollection { return AssistantCollection{project: r.id} }

// Create requests creating an assistant in the project (assistant:write on
// the project).
func (c AssistantCollection) Create() Check {
	return Assistant(AssistantID(c.project), c.project).Write()
}
