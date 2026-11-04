package authz

import (
	"fmt"
	"slices"

	s "github.com/speakeasy-api/gram/server/internal/authz/schema"
)

// The scope tables the engine, the permission catalogue, grant validation
// and system role seeding read are projections of the default registry: a
// declared scope's visibility, satisfying scopes, exclusion, grant surface
// and role defaults all come from its declaration in declarations.go.
// undeclaredScopes supplies the same facts for the scopes no type declares
// yet. TestScopeTablesGolden pins the combined result.
var (
	scopeTables            = mustBuildScopeTables(mustDefaultRegistry(), undeclaredScopes)
	scopeVisibilityByScope = scopeTables.visibility
	scopeExpansions        = scopeTables.expansions
	scopeExclusions        = scopeTables.exclusions
	scopeGrantSurfaces     = scopeTables.surfaces
	adminScopes            = scopeTables.roleScopes[SystemRoleAdmin]
	memberScopes           = scopeTables.roleScopes[SystemRoleMember]
)

// undeclaredScope is the table entry of a scope that no registry type
// declares.
type undeclaredScope struct {
	visibility  scopeVisibility
	satisfiedBy []Scope
	exclusion   Scope
	roles       []string
}

// undeclaredScopes are the scopes still defined by hand: the root scope,
// which no grant is checked against, and the agent and workload scopes,
// whose types join the registry with the agents slice. Each entry moves into
// declarations.go when its type is declared; a scope must not be in both.
//
// Configuring workload identity is an administrator's job, and it sits
// beside agent:authorize deliberately: assigning an agent to a workload is
// the same delegation as issuing that agent a credential. Neither is a member
// default: workload:read alone discloses which machines an organization
// recognises, which is its trust policy, so a member who needs to see it gets
// an explicit grant through a custom role.
var undeclaredScopes = map[Scope]undeclaredScope{
	ScopeRoot:                 {visibility: scopeVisibilityInternal, satisfiedBy: nil, exclusion: "", roles: nil},
	ScopeAgentRead:            {visibility: scopeVisibilityUserVisible, satisfiedBy: nil, exclusion: "", roles: []string{SystemRoleAdmin}},
	ScopeAgentWrite:           {visibility: scopeVisibilityUserVisible, satisfiedBy: nil, exclusion: "", roles: []string{SystemRoleAdmin}},
	ScopeAgentAuthorize:       {visibility: scopeVisibilityUserVisible, satisfiedBy: nil, exclusion: "", roles: []string{SystemRoleAdmin}},
	ScopeAgentTransfer:        {visibility: scopeVisibilityUserVisible, satisfiedBy: nil, exclusion: "", roles: []string{SystemRoleAdmin}},
	ScopeWorkloadRead:         {visibility: scopeVisibilityUserVisible, satisfiedBy: []Scope{ScopeWorkloadWrite}, exclusion: ScopeWorkloadBlockedRead, roles: []string{SystemRoleAdmin}},
	ScopeWorkloadBlockedRead:  {visibility: scopeVisibilityInternal, satisfiedBy: nil, exclusion: "", roles: nil},
	ScopeWorkloadWrite:        {visibility: scopeVisibilityUserVisible, satisfiedBy: nil, exclusion: ScopeWorkloadBlockedWrite, roles: []string{SystemRoleAdmin}},
	ScopeWorkloadBlockedWrite: {visibility: scopeVisibilityInternal, satisfiedBy: []Scope{ScopeWorkloadBlockedRead}, exclusion: "", roles: nil},
}

type legacyScopeTables struct {
	visibility map[Scope]scopeVisibility
	expansions map[Scope][]Scope
	exclusions map[Scope]Scope
	surfaces   map[Scope]GrantSurface
	roleScopes map[string][]Scope
}

func mustDefaultRegistry() *s.Registry {
	reg, err := DefaultRegistry()
	if err != nil {
		panic(err)
	}
	return reg
}

func mustBuildScopeTables(reg *s.Registry, undeclared map[Scope]undeclaredScope) legacyScopeTables {
	tables, err := buildScopeTables(reg, undeclared)
	if err != nil {
		panic(err)
	}
	return tables
}

// buildScopeTables projects the registry and the undeclared scopes into the
// scope tables. Undeclared scopes take the access grant surface.
func buildScopeTables(reg *s.Registry, undeclared map[Scope]undeclaredScope) (legacyScopeTables, error) {
	tables := legacyScopeTables{
		visibility: make(map[Scope]scopeVisibility),
		expansions: make(map[Scope][]Scope),
		exclusions: make(map[Scope]Scope),
		surfaces:   make(map[Scope]GrantSurface),
		roleScopes: make(map[string][]Scope),
	}
	for _, def := range reg.Scopes() {
		scope := Scope(def.Name)
		if _, dup := undeclared[scope]; dup {
			return legacyScopeTables{}, fmt.Errorf("scope %s is both declared and listed as undeclared", scope)
		}
		tables.visibility[scope] = scopeVisibilityInternal
		if def.Visibility == s.VisibilityUserVisible {
			tables.visibility[scope] = scopeVisibilityUserVisible
		}
		tables.expansions[scope] = scopesOf(def.SatisfiedBy)
		tables.exclusions[scope] = Scope(def.Exclusion)
		tables.surfaces[scope] = GrantSurface(def.GrantSurface)
		for _, role := range def.Roles {
			tables.roleScopes[role] = append(tables.roleScopes[role], scope)
		}
	}
	for scope, entry := range undeclared {
		tables.visibility[scope] = entry.visibility
		tables.expansions[scope] = slices.Clone(entry.satisfiedBy)
		tables.exclusions[scope] = entry.exclusion
		tables.surfaces[scope] = GrantSurfaceAccess
		for _, role := range entry.roles {
			tables.roleScopes[role] = append(tables.roleScopes[role], scope)
		}
	}
	for _, scopes := range tables.roleScopes {
		slices.Sort(scopes)
	}
	return tables, nil
}

func scopesOf(names []string) []Scope {
	if len(names) == 0 {
		return nil
	}
	out := make([]Scope, len(names))
	for i, name := range names {
		out[i] = Scope(name)
	}
	return out
}
