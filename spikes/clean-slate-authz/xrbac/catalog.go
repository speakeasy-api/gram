// Package xrbac is a spike of "Technical implementation plan: Extensible RBAC
// for Speakeasy": a scope catalogue, a validating Client in front of a
// swappable Backend, and a shared evaluator over the existing grant model
// (principal + scope + selector), with deny-wins, relationship rules and
// validity intervals. It deliberately keeps today's data shape.
package xrbac

import (
	"fmt"
	"slices"
)

type Scope string

const (
	ScopeOrgRead           Scope = "org:read"
	ScopeProjectRead       Scope = "project:read"
	ScopeAgentCreate       Scope = "agent:create"
	ScopeAgentRead         Scope = "agent:read"
	ScopeAgentWrite        Scope = "agent:write"
	ScopeMCPRead           Scope = "mcp:read"
	ScopeMCPWrite          Scope = "mcp:write"
	ScopeMCPConnect        Scope = "mcp:connect"
	ScopeMCPBlockedRead    Scope = "mcp:blocked_read"
	ScopeMCPBlockedWrite   Scope = "mcp:blocked_write"
	ScopeMCPBlockedConn    Scope = "mcp:blocked_connect"
	ScopeChatRead          Scope = "chat:read"
	ScopeChatBlockedRead   Scope = "chat:blocked_read"
	ScopeAgentBlockedWrite Scope = "agent:blocked_write"
)

type ExclusionPurpose string

const (
	ExclusionNone          ExclusionPurpose = "none"
	ExclusionAccessDeny    ExclusionPurpose = "access_deny"
	ExclusionRiskExemption ExclusionPurpose = "risk_exemption"
)

type ScopeLifecycle string

const (
	LifecycleActive  ScopeLifecycle = "active"
	LifecycleRetired ScopeLifecycle = "retired"
)

type GrantSurface string

const GrantSurfaceAccess GrantSurface = "access_management"

// ScopeDefinition is the plan's catalogue entry, one per scope. Registering a
// definition grants nothing to anyone.
type ScopeDefinition struct {
	Scope             Scope
	Description       string
	ResourceKind      string   // "mcp", "agent", "chat", "project", "org"
	AllowedAnchors    []string // kinds a grant may be anchored at
	RequiredAncestors []string // ancestor kinds an existing-resource target must carry
	Visibility        string   // "user_visible" or "internal"
	GrantSurface      GrantSurface
	ImpliedBy         []Scope // one-step, authored edges
	ExcludedBy        []Scope // opposing scopes
	ExclusionPurpose  ExclusionPurpose
	SelectorKeys      []string // narrowing keys beyond resource_kind/resource_id
	Lifecycle         ScopeLifecycle
	RuntimeSafeSince  int // first agent-runtime version that may hold it; 0 = never
}

const CurrentAgentRuntimeScopeVersion = 6

var definitions = []ScopeDefinition{
	{Scope: ScopeOrgRead, Description: "View organization settings and directory", ResourceKind: "org",
		AllowedAnchors: []string{"org"}, Visibility: "user_visible", GrantSurface: GrantSurfaceAccess,
		ExclusionPurpose: ExclusionNone, Lifecycle: LifecycleActive},
	{Scope: ScopeProjectRead, Description: "View project metadata", ResourceKind: "project",
		AllowedAnchors: []string{"project", "org"}, RequiredAncestors: []string{"org"}, Visibility: "user_visible",
		GrantSurface: GrantSurfaceAccess, ExclusionPurpose: ExclusionNone, Lifecycle: LifecycleActive, RuntimeSafeSince: 1},
	{Scope: ScopeAgentCreate, Description: "Create agents you own", ResourceKind: "org",
		AllowedAnchors: []string{"org"}, Visibility: "user_visible", GrantSurface: GrantSurfaceAccess,
		ExclusionPurpose: ExclusionNone, Lifecycle: LifecycleActive},
	{Scope: ScopeAgentRead, Description: "Inspect agents' non-secret configuration", ResourceKind: "agent",
		AllowedAnchors: []string{"agent", "project", "org"}, RequiredAncestors: []string{"project", "org"},
		Visibility: "user_visible", GrantSurface: GrantSurfaceAccess, ImpliedBy: []Scope{ScopeAgentWrite},
		ExclusionPurpose: ExclusionNone, SelectorKeys: []string{"project_id"}, Lifecycle: LifecycleActive},
	{Scope: ScopeAgentWrite, Description: "Configure agents and their policies", ResourceKind: "agent",
		AllowedAnchors: []string{"agent", "project", "org"}, RequiredAncestors: []string{"project", "org"},
		Visibility: "user_visible", GrantSurface: GrantSurfaceAccess, ExcludedBy: []Scope{ScopeAgentBlockedWrite},
		ExclusionPurpose: ExclusionAccessDeny, SelectorKeys: []string{"project_id"}, Lifecycle: LifecycleActive},
	{Scope: ScopeMCPRead, Description: "Inspect non-secret server configuration", ResourceKind: "mcp",
		AllowedAnchors: []string{"mcp", "project", "org"}, RequiredAncestors: []string{"project", "org"},
		Visibility: "user_visible", GrantSurface: GrantSurfaceAccess, ImpliedBy: []Scope{ScopeMCPWrite},
		ExcludedBy: []Scope{ScopeMCPBlockedRead}, ExclusionPurpose: ExclusionAccessDeny,
		SelectorKeys: []string{"project_id"}, Lifecycle: LifecycleActive, RuntimeSafeSince: 1},
	{Scope: ScopeMCPWrite, Description: "Edit server configuration, audience and visibility", ResourceKind: "mcp",
		AllowedAnchors: []string{"mcp", "project", "org"}, RequiredAncestors: []string{"project", "org"},
		Visibility: "user_visible", GrantSurface: GrantSurfaceAccess, ExcludedBy: []Scope{ScopeMCPBlockedWrite},
		ExclusionPurpose: ExclusionAccessDeny, SelectorKeys: []string{"project_id"}, Lifecycle: LifecycleActive},
	// After the semantic cutover: no ImpliedBy, so management never satisfies connect.
	{Scope: ScopeMCPConnect, Description: "Connect to the server and call its tools", ResourceKind: "mcp",
		AllowedAnchors: []string{"mcp", "project", "org"}, RequiredAncestors: []string{"project", "org"},
		Visibility: "user_visible", GrantSurface: GrantSurfaceAccess, ExcludedBy: []Scope{ScopeMCPBlockedConn},
		ExclusionPurpose: ExclusionAccessDeny, SelectorKeys: []string{"project_id", "tool", "disposition"},
		Lifecycle: LifecycleActive, RuntimeSafeSince: 1},
	{Scope: ScopeChatRead, Description: "Read transcripts of the selected people's sessions", ResourceKind: "chat",
		AllowedAnchors: []string{"chat", "project", "org"}, RequiredAncestors: []string{"project", "org"},
		Visibility: "user_visible", GrantSurface: GrantSurfaceAccess, ExcludedBy: []Scope{ScopeChatBlockedRead},
		ExclusionPurpose: ExclusionAccessDeny, SelectorKeys: []string{"project_id", "subject_principal"},
		Lifecycle: LifecycleActive},
	// Legacy blocked scopes stay in the vocabulary and decode to access denials.
	{Scope: ScopeMCPBlockedRead, ResourceKind: "mcp", AllowedAnchors: []string{"mcp", "project", "org"}, Visibility: "internal",
		GrantSurface: GrantSurfaceAccess, ExclusionPurpose: ExclusionNone, SelectorKeys: []string{"project_id"}, Lifecycle: LifecycleActive},
	{Scope: ScopeMCPBlockedWrite, ResourceKind: "mcp", AllowedAnchors: []string{"mcp", "project", "org"}, Visibility: "internal",
		GrantSurface: GrantSurfaceAccess, ExclusionPurpose: ExclusionNone, SelectorKeys: []string{"project_id"}, Lifecycle: LifecycleActive},
	{Scope: ScopeMCPBlockedConn, ResourceKind: "mcp", AllowedAnchors: []string{"mcp", "project", "org"}, Visibility: "internal",
		GrantSurface: GrantSurfaceAccess, ExclusionPurpose: ExclusionNone, SelectorKeys: []string{"project_id", "tool", "disposition"},
		Lifecycle: LifecycleActive, RuntimeSafeSince: 1},
	{Scope: ScopeChatBlockedRead, ResourceKind: "chat", AllowedAnchors: []string{"chat", "project", "org"}, Visibility: "internal",
		GrantSurface: GrantSurfaceAccess, ExclusionPurpose: ExclusionNone, SelectorKeys: []string{"project_id", "subject_principal"}, Lifecycle: LifecycleActive},
	{Scope: ScopeAgentBlockedWrite, ResourceKind: "agent", AllowedAnchors: []string{"agent", "project", "org"}, Visibility: "internal",
		GrantSurface: GrantSurfaceAccess, ExclusionPurpose: ExclusionNone, SelectorKeys: []string{"project_id"}, Lifecycle: LifecycleActive},
}

var byScope = func() map[Scope]ScopeDefinition {
	if err := ValidateScopeDefinitions(definitions); err != nil {
		panic(err)
	}
	m := map[Scope]ScopeDefinition{}
	for _, d := range definitions {
		m[d.Scope] = d
	}
	return m
}()

// denies maps each opposing scope back to the scope it denies.
var denies = func() map[Scope]Scope {
	m := map[Scope]Scope{}
	for _, d := range definitions {
		if d.ExclusionPurpose == ExclusionAccessDeny {
			for _, x := range d.ExcludedBy {
				m[x] = d.Scope
			}
		}
	}
	return m
}()

// LookupScopeDefinition returns a defensive copy.
func LookupScopeDefinition(s Scope) (ScopeDefinition, bool) {
	d, ok := byScope[s]
	d.AllowedAnchors = slices.Clone(d.AllowedAnchors)
	d.RequiredAncestors = slices.Clone(d.RequiredAncestors)
	d.ImpliedBy = slices.Clone(d.ImpliedBy)
	d.ExcludedBy = slices.Clone(d.ExcludedBy)
	d.SelectorKeys = slices.Clone(d.SelectorKeys)
	return d, ok
}

// ImpliedScopes reverses ImpliedBy edges: the scopes s directly satisfies.
func ImpliedScopes(s Scope) []Scope {
	var out []Scope
	for _, d := range definitions {
		if slices.Contains(d.ImpliedBy, s) {
			out = append(out, d.Scope)
		}
	}
	slices.Sort(out)
	return out
}

func ValidateScopeDefinitions(defs []ScopeDefinition) error {
	seen := map[Scope]bool{}
	for _, d := range defs {
		if seen[d.Scope] {
			return fmt.Errorf("duplicate scope %q", d.Scope)
		}
		seen[d.Scope] = true
	}
	for _, d := range defs {
		for _, s := range append(slices.Clone(d.ImpliedBy), d.ExcludedBy...) {
			if !seen[s] {
				return fmt.Errorf("%s references unknown scope %q", d.Scope, s)
			}
		}
		if (len(d.ExcludedBy) == 0) != (d.ExclusionPurpose == ExclusionNone) {
			return fmt.Errorf("%s: ExcludedBy and ExclusionPurpose disagree", d.Scope)
		}
	}
	return nil
}

// ValidateAgentRuntimeScope checks policy vocabulary, not delegation authority.
func ValidateAgentRuntimeScope(version int, s Scope) error {
	d, ok := byScope[s]
	if !ok || d.Lifecycle != LifecycleActive || d.RuntimeSafeSince == 0 || version < d.RuntimeSafeSince {
		return fmt.Errorf("%s is not agent-runtime eligible at version %d", s, version)
	}
	return nil
}
