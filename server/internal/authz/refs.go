package authz

import (
	"reflect"

	"github.com/google/uuid"
)

// This file holds the hand-written half of the typed requests generated in
// refs_gen.go: the helper every ref method builds its check with, and the
// requests that need more than a resource and an action.

// ID is the id a typed ref takes: a resource's uuid, or its id as a string.
// Ref constructors are generic over it so callers pass ids as they hold them.
type ID interface {
	~string | uuid.UUID
}

func idString[T ID](id T) string {
	if u, ok := any(id).(uuid.UUID); ok {
		return u.String()
	}
	return reflect.ValueOf(id).String()
}

func newRefCheck(scope Scope, resourceID string, dimensions map[string]string, match MatchMode) Check {
	mode := selectorMatchNormal
	if match == MatchStrict {
		mode = selectorMatchStrict
	}
	return Check{Scope: scope, ResourceKind: "", ResourceID: resourceID, Dimensions: dimensions, selectorMatch: mode, representedUser: "", probe: false, anyOf: nil}
}

// ToolCall requests mcp:connect for calling one tool on the server. A tool
// or disposition that is empty is carried as unknown (see MCPToolCallCheck).
func (r MCPServerRef) ToolCall(tool, disposition string) Check {
	return MCPToolCallCheck(r.id, MCPToolCallDimensions{Tool: tool, Disposition: disposition, ProjectID: r.project})
}

// Collections name the resources of one kind inside a project, for requests
// that have no resource yet. Create is stored as the kind's write permission
// on the project id, as it always has been, so existing grants keep working.

// MCPServerCollection names the MCP servers of one project.
type MCPServerCollection struct{ project string }

// MCPServers names the MCP servers of the project.
func (r ProjectRef) MCPServers() MCPServerCollection { return MCPServerCollection{project: r.id} }

// Create requests creating an MCP server in the project (mcp:write on the
// project).
func (c MCPServerCollection) Create() Check {
	return MCPServer(c.project, c.project).Write()
}

// EnvironmentCollection names the environments of one project.
type EnvironmentCollection struct{ project string }

// Environments names the environments of the project.
func (r ProjectRef) Environments() EnvironmentCollection {
	return EnvironmentCollection{project: r.id}
}

// Create requests creating an environment in the project
// (environment:write on the project).
func (c EnvironmentCollection) Create() Check {
	return Environment(c.project, c.project).Write()
}

// Read requests environment:read on the project: every environment in it.
// Use it where no environment resolved, so the answer to a missing slug does
// not reveal whether the environment exists.
func (c EnvironmentCollection) Read() Check {
	return Environment(c.project, c.project).Read()
}

// Write requests environment:write on the project: every environment in it.
// It is the same check as Create.
func (c EnvironmentCollection) Write() Check {
	return Environment(c.project, c.project).Write()
}

// AssistantCollection names the assistants of one project.
type AssistantCollection struct{ project string }

// Assistants names the assistants of the project.
func (r ProjectRef) Assistants() AssistantCollection { return AssistantCollection{project: r.id} }

// Create requests creating an assistant in the project (assistant:write on
// the project).
func (c AssistantCollection) Create() Check {
	return Assistant(c.project, c.project).Write()
}
