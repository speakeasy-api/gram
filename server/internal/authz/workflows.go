package authz

import "slices"

// Workflow is a task a person does in the product and every scope the task
// needs end to end. A single denied scope is explained better as "part of
// setting up an agent" than as a bare slug, and a role built for the task
// should hold all of its scopes, not just the one that happened to fail.
//
// The list is hand-written. TestWorkflows keeps it inside the catalogue; the
// handlers that enforce each scope are the source of truth for what a task
// really needs.
type Workflow struct {
	Slug   string
	Label  string
	Scopes []Scope
}

var workflows = []Workflow{
	{
		Slug:   "agent_setup",
		Label:  "Set up an agent",
		Scopes: []Scope{ScopeAgentWrite, ScopeAgentAuthorize, ScopeEnvironmentRead},
	},
	{
		Slug:   "mcp_maintenance",
		Label:  "Maintain an MCP server",
		Scopes: []Scope{ScopeMCPWrite, ScopeEnvironmentRead},
	},
	{
		Slug:   "session_review",
		Label:  "Review other members' sessions",
		Scopes: []Scope{ScopeChatRead, ScopeLogRead},
	},
	{
		Slug:   "plugin_publish",
		Label:  "Publish a plugin",
		Scopes: []Scope{ScopePluginWrite, ScopeProjectRead},
	},
}

// Workflows returns a copy of the workflow list.
func Workflows() []Workflow {
	out := make([]Workflow, 0, len(workflows))
	for _, workflow := range workflows {
		out = append(out, Workflow{
			Slug:   workflow.Slug,
			Label:  workflow.Label,
			Scopes: slices.Clone(workflow.Scopes),
		})
	}
	return out
}

// WorkflowsNeeding returns the workflows that include scope.
func WorkflowsNeeding(scope Scope) []Workflow {
	var out []Workflow
	for _, workflow := range Workflows() {
		if slices.Contains(workflow.Scopes, scope) {
			out = append(out, workflow)
		}
	}
	return out
}
