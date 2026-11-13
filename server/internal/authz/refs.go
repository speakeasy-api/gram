package authz

// This file holds the hand-written half of the typed requests generated in
// refs_gen.go: the helper every ref method builds its check with, and the
// requests that need more than a resource and an action.

// MatchMode is how an allow grant's selector is compared with a check.
type MatchMode int

const (
	// MatchLoose skips grant keys the check does not carry. It is today's
	// default for every resource type except auth clients.
	MatchLoose MatchMode = iota
	// MatchStrict requires every grant key to be present in the check.
	MatchStrict
)

func newRefCheck(scope Scope, resourceID string, dimensions map[string]string, match MatchMode) Check {
	mode := selectorMatchNormal
	if match == MatchStrict {
		mode = selectorMatchStrict
	}
	return Check{Scope: scope, ResourceKind: "", ResourceID: resourceID, Dimensions: dimensions, selectorMatch: mode, representedUser: "", probe: false, anyOf: nil, operation: operationDefault, organization: None[OrgID]()}
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
	return MCPServer(MCPServerID{id: c.project.id}, c.project).Write()
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
	return Environment(EnvironmentID{id: c.project.id}, c.project).Write()
}

// Read requests environment:read on the project: every environment in it.
// Use it where no environment resolved, so the answer to a missing slug does
// not reveal whether the environment exists.
func (c EnvironmentCollection) Read() Check {
	return Environment(EnvironmentID{id: c.project.id}, c.project).Read()
}

// Write requests environment:write on the project: every environment in it.
// It is the same check as Create.
func (c EnvironmentCollection) Write() Check {
	return Environment(EnvironmentID{id: c.project.id}, c.project).Write()
}

// AssistantCollection names the assistants of one project.
type AssistantCollection struct{ project ProjectID }

// Assistants names the assistants of the project.
func (r ProjectRef) Assistants() AssistantCollection { return AssistantCollection{project: r.id} }

// Create requests creating an assistant in the project (assistant:write on
// the project).
func (c AssistantCollection) Create() Check {
	return Assistant(AssistantID{id: c.project.id}, c.project).Write()
}
