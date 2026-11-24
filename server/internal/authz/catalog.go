package authz

import (
	"maps"
	"slices"
)

// Sensitivity is generated from the registry into sensitivity_gen.go;
// ScopeCategory and ScopeCategories into scopes_vocabulary_gen.go.

// ScopeEntry is what a person needs to know about one scope before granting
// it: a short name, what it allows, where it sits in the dashboard, and
// whether it deserves a warning. Entries are generated from the registry, which also
// holds the authorization semantics the catalogue only describes.
type ScopeEntry struct {
	Scope Scope

	// Label is the short name shown in place of the slug, e.g. "View tool
	// logs".
	Label string

	// Category is the dashboard grouping.
	Category ScopeCategory

	// ResourceType is the resource kind the scope is checked against. It is
	// what selectors on a grant of this scope refer to.
	ResourceType string

	// Description says what a holder can do and, where it matters, what the
	// scope does not include.
	Description string

	// Sensitivity says why the scope deserves a warning before it is granted.
	Sensitivity Sensitivity
}

// Sensitive reports whether granting the scope deserves a warning.
func (e ScopeEntry) Sensitive() bool {
	return e.Sensitivity != SensitivityNone
}

// SubScopes lists the user-visible scopes a grant of this scope also
// satisfies, one step down the expansion graph.
func (e ScopeEntry) SubScopes() []string {
	return CalculateSubScopes(e.Scope)
}

var scopeCatalogByScope = func() map[Scope]ScopeEntry {
	out := make(map[Scope]ScopeEntry, len(scopeCatalog))
	for _, entry := range scopeCatalog {
		out[entry.Scope] = entry
	}
	return out
}()

// ScopeCatalog returns every grantable scope with its presentation metadata,
// in presentation order. The slice is a copy; callers may reorder it.
func ScopeCatalog() []ScopeEntry {
	return slices.Clone(scopeCatalog)
}

// ScopeEntryFor returns the catalogue entry for scope.
func ScopeEntryFor(scope Scope) (ScopeEntry, bool) {
	entry, ok := scopeCatalogByScope[scope]
	return entry, ok
}

// ScopeNames returns every catalogued scope slug in presentation order. The
// API design uses it for its scope enums so a new scope needs no edit there.
func ScopeNames() []string {
	out := make([]string, 0, len(scopeCatalog))
	for _, entry := range scopeCatalog {
		out = append(out, string(entry.Scope))
	}
	return out
}

// ExclusionScopeNames returns, in presentation order of the scopes they
// store exceptions for, every scope that stores exception grants.
func ExclusionScopeNames() []string {
	var out []string
	for _, entry := range scopeCatalog {
		exclusion, ok := ExclusionScopeFor(entry.Scope)
		if ok && !slices.Contains(out, string(exclusion)) {
			out = append(out, string(exclusion))
		}
	}
	return out
}

// SensitiveScopes returns, in presentation order, the catalogue scopes that
// deserve a warning before they are granted.
func SensitiveScopes() []Scope {
	var out []Scope
	for _, entry := range scopeCatalog {
		if entry.Sensitive() {
			out = append(out, entry.Scope)
		}
	}
	return out
}

// DerivedAgentEligibility is one scope's agent eligibility as the model
// declares it.
type DerivedAgentEligibility struct {
	// SafeSince is the agent runtime scope registry version the scope became
	// agent-safe in, or 0 if it never did.
	SafeSince int
	// Retired marks a tombstone: the scope no longer exists.
	Retired bool
}

// AgentEligibilityByScope returns the agent eligibility of every scope the
// model knows, retired tombstones included. The agent runtime scope registry
// reads it.
func AgentEligibilityByScope() map[Scope]DerivedAgentEligibility {
	return maps.Clone(agentEligibilityByScope)
}

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
