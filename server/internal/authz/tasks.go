package authz

import "slices"

// ScopeTask is a task a person does in the product and every scope the task
// needs end to end. A single denied scope is explained better as "part of
// setting up an agent" than as a bare slug, and a role built for the task
// should hold all of its scopes, not just the one that happened to fail.
//
// The list is hand-written. TestTasks keeps it inside the catalogue; the
// handlers that enforce each scope are the source of truth for what a task
// really needs.
type ScopeTask struct {
	Slug   string
	Label  string
	Scopes []Scope
}

var scopeTasks = []ScopeTask{
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

// Tasks returns a copy of the task list.
func Tasks() []ScopeTask {
	out := make([]ScopeTask, 0, len(scopeTasks))
	for _, task := range scopeTasks {
		out = append(out, ScopeTask{
			Slug:   task.Slug,
			Label:  task.Label,
			Scopes: slices.Clone(task.Scopes),
		})
	}
	return out
}

// TasksNeeding returns the tasks that include scope.
func TasksNeeding(scope Scope) []ScopeTask {
	var out []ScopeTask
	for _, task := range Tasks() {
		if slices.Contains(task.Scopes, scope) {
			out = append(out, task)
		}
	}
	return out
}
