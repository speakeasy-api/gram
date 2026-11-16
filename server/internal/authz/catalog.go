package authz

import "slices"

// Sensitivity says why granting a scope deserves a warning. The empty value
// marks a scope that needs none.
type Sensitivity string

const (
	// SensitivityNone marks a scope that needs no warning before it is granted.
	SensitivityNone Sensitivity = ""

	// SensitivityConversations marks a scope that exposes other members'
	// agent session transcripts.
	SensitivityConversations Sensitivity = "conversations"

	// SensitivityToolLogs marks a scope that exposes tool arguments, results
	// and prompt text from every member of a project.
	SensitivityToolLogs Sensitivity = "tool_logs"

	// SensitivityDestructiveAdmin marks a scope that can remove members or
	// change what every role can do.
	SensitivityDestructiveAdmin Sensitivity = "destructive_admin"
)

// ScopeCategory groups scopes the way the dashboard presents them. It differs
// from the resource kind a check carries where one resource kind holds several
// permission families: plugin:write and log:read are both checked against a
// project, but they are not project permissions to a person reading a role.
type ScopeCategory string

const (
	ScopeCategoryOrg           ScopeCategory = "org"
	ScopeCategoryProject       ScopeCategory = "project"
	ScopeCategoryMCP           ScopeCategory = "mcp"
	ScopeCategoryMCPAuthClient ScopeCategory = "mcp_auth_client"
	ScopeCategoryEnvironment   ScopeCategory = "environment"
	ScopeCategorySkill         ScopeCategory = "skill"
	ScopeCategoryPlugin        ScopeCategory = "plugin"
	ScopeCategoryAssistant     ScopeCategory = "assistant"
	ScopeCategoryRiskPolicy    ScopeCategory = "risk_policy"
	ScopeCategorySession       ScopeCategory = "session"
	ScopeCategoryInference     ScopeCategory = "inference"
	ScopeCategoryLog           ScopeCategory = "log"
	ScopeCategoryAgent         ScopeCategory = "agent"
	ScopeCategoryWorkload      ScopeCategory = "workload"
)

// ScopeCategories lists every category in presentation order.
var ScopeCategories = []ScopeCategory{
	ScopeCategoryOrg,
	ScopeCategoryProject,
	ScopeCategoryMCP,
	ScopeCategoryMCPAuthClient,
	ScopeCategoryEnvironment,
	ScopeCategorySkill,
	ScopeCategoryPlugin,
	ScopeCategoryAssistant,
	ScopeCategoryRiskPolicy,
	ScopeCategorySession,
	ScopeCategoryInference,
	ScopeCategoryLog,
	ScopeCategoryAgent,
	ScopeCategoryWorkload,
}

// ScopeEntry is what a person needs to know about one scope before granting
// it: a short name, what it allows, where it sits in the dashboard, and
// whether it deserves a warning. Entries are derived from Model, which also
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

// scopeCatalog lists every scope a role can hold, in the order the catalogue
// presents them: by category, then as declared in Model, each block after its
// permission. ScopeRoot is deliberately absent: it is the engine's own bypass,
// never a grant a person reads about.
var scopeCatalog = Model.deriveCatalog()

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
