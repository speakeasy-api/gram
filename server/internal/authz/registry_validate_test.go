package authz

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestModel_IsValid(t *testing.T) {
	t.Parallel()
	require.NoError(t, Model.Validate())
}

func validTestPermission(action Action) Permission {
	return Permission{
		Action:      action,
		Scope:       "",
		Implies:     nil,
		Exclusions:  nil,
		Dimensions:  nil,
		Source:      GrantSourceRoles,
		Category:    "",
		Label:       "Label",
		Description: "Description.",
		Sensitivity: SensitivityNone,
		Agent:       AgentUnsafe(),
	}
}

func validTestType(kind Kind, permissions ...Permission) ResourceType {
	return ResourceType{
		Kind:        kind,
		Parent:      "",
		Category:    ScopeCategoryProject,
		Match:       MatchLoose,
		Dimensions:  nil,
		Permissions: permissions,
	}
}

func validTestRegistry(types ...ResourceType) Registry {
	return Registry{
		Root:        RootPermission{Scope: ScopeRoot, Agent: AgentUnsafe()},
		Types:       types,
		SystemRoles: nil,
		Retired:     nil,
	}
}

func TestRegistryValidate_RejectsBrokenModels(t *testing.T) {
	t.Parallel()

	withImplies := func(p Permission, implies ...Action) Permission { p.Implies = implies; return p }
	withAgent := func(p Permission, a AgentEligibility) Permission { p.Agent = a; return p }
	withScope := func(p Permission, s Scope) Permission { p.Scope = s; return p }
	withExclusions := func(p Permission, x ...Exclusion) Permission { p.Exclusions = x; return p }

	cases := []struct {
		name  string
		model Registry
		want  string
	}{
		{
			name:  "duplicate action",
			model: validTestRegistry(validTestType("thing", validTestPermission("read"), validTestPermission("read"))),
			want:  `thing: action "read" is declared twice`,
		},
		{
			name: "duplicate scope across types",
			model: validTestRegistry(
				validTestType("a", withScope(validTestPermission("read"), "shared:read")),
				validTestType("b", withScope(validTestPermission("read"), "shared:read")),
			),
			want: `scope "shared:read" is declared by both a.read and b.read`,
		},
		{
			name: "block twin collides with a declared scope",
			model: validTestRegistry(validTestType("thing",
				withExclusions(validTestPermission("read"), Blocked(AgentUnsafe(), "thing read access")...),
				withScope(validTestPermission("other"), "thing:blocked_read"),
			)),
			want: `scope "thing:blocked_read" is declared by both`,
		},
		{
			name:  "implies unknown action",
			model: validTestRegistry(validTestType("thing", withImplies(validTestPermission("write"), "read"))),
			want:  `thing.write: implies unknown action "read"`,
		},
		{
			name: "implies cycle",
			model: validTestRegistry(validTestType("thing",
				withImplies(validTestPermission("read"), "write"),
				withImplies(validTestPermission("write"), "read"),
			)),
			want: `thing: Implies has a cycle`,
		},
		{
			name: "agent-safe permission implies an unsafe one",
			model: validTestRegistry(validTestType("thing",
				withAgent(withImplies(validTestPermission("write"), "read"), SafeSince(1)),
				validTestPermission("read"),
			)),
			want: `thing.write: agent-safe since v1 but implies read, which is not`,
		},
		{
			name: "agent-safe permission implies one that became safe later",
			model: validTestRegistry(validTestType("thing",
				withAgent(withImplies(validTestPermission("write"), "read"), SafeSince(1)),
				withAgent(validTestPermission("read"), SafeSince(3)),
			)),
			want: `thing.write: agent-safe since v1 but implies read`,
		},
		{
			name: "exemption names an unknown action",
			model: validTestRegistry(validTestType("thing",
				withExclusions(validTestPermission("evaluate"), Exclusion{Kind: ExclusionExemption, Action: "bypass", Agent: AgentUnsafe(), Subject: ""}),
			)),
			want: `thing.evaluate: exemption names unknown action "bypass"`,
		},
		{
			name: "declared blocked_ action",
			model: validTestRegistry(validTestType("thing",
				validTestPermission("blocked_read"),
			)),
			want: `thing.blocked_read: blocked_ actions are generated, not declared`,
		},
		{
			name: "system role names an unknown scope",
			model: func() Registry {
				r := validTestRegistry(validTestType("thing", validTestPermission("read")))
				r.SystemRoles = map[string][]Scope{"admin": {"thing:read", "thing:write"}}
				return r
			}(),
			want: `system role "admin" names unknown scope "thing:write"`,
		},
		{
			name: "system role names a block",
			model: func() Registry {
				r := validTestRegistry(validTestType("thing", withExclusions(validTestPermission("read"), Blocked(AgentUnsafe(), "thing read access")...)))
				r.SystemRoles = map[string][]Scope{"admin": {"thing:blocked_read"}}
				return r
			}(),
			want: `system role "admin" names "thing:blocked_read", which is not a grantable permission`,
		},
		{
			name: "retired scope reused",
			model: func() Registry {
				r := validTestRegistry(validTestType("thing", validTestPermission("read")))
				r.Retired = []Scope{"thing:read"}
				return r
			}(),
			want: `retired scope "thing:read" is reused by thing.read`,
		},
		{
			name: "unknown parent",
			model: func() Registry {
				typ := validTestType("thing", validTestPermission("read"))
				typ.Parent = "nowhere"
				return validTestRegistry(typ)
			}(),
			want: `thing: parent "nowhere" is not a declared resource type`,
		},
		{
			name: "wildcard listed as a dimension value",
			model: func() Registry {
				typ := validTestType("thing", validTestPermission("read"))
				typ.Dimensions = []Dimension{{Key: "mode", Values: []string{"a", WildcardResource}, Required: false}}
				return validTestRegistry(typ)
			}(),
			want: `thing: dimension "mode" lists "*" as a value`,
		},
		{
			name:  "scope with more than one separator",
			model: validTestRegistry(validTestType("thing", withScope(validTestPermission("read"), "thing:read:extra"))),
			want:  `thing.read: scope "thing:read:extra" is not resource:action`,
		},
		{
			name: "permission declares a dimension twice",
			model: func() Registry {
				p := validTestPermission("read")
				p.Dimensions = []Dimension{{Key: "mode", Values: nil, Required: false}, {Key: "mode", Values: nil, Required: false}}
				return validTestRegistry(validTestType("thing", p))
			}(),
			want: `thing.read: dimension "mode" is declared twice`,
		},
		{
			name: "unknown type category",
			model: func() Registry {
				typ := validTestType("thing", validTestPermission("read"))
				typ.Category = "nowhere"
				return validTestRegistry(typ)
			}(),
			want: `thing: unknown category "nowhere"`,
		},
		{
			name: "permission exempts itself",
			model: validTestRegistry(validTestType("thing",
				withExclusions(validTestPermission("evaluate"), Exclusion{Kind: ExclusionExemption, Action: "evaluate", Agent: AgentUnsafe(), Subject: ""}),
			)),
			want: `thing.evaluate: a permission cannot exempt itself`,
		},
		{
			name: "exemption with another grant source",
			model: func() Registry {
				bypass := validTestPermission("bypass")
				bypass.Source = GrantSourceRiskPolicy
				return validTestRegistry(validTestType("thing",
					withExclusions(validTestPermission("evaluate"), Exclusion{Kind: ExclusionExemption, Action: "bypass", Agent: AgentUnsafe(), Subject: ""}),
					bypass,
				))
			}(),
			want: `thing.evaluate: exemption "bypass" has a different grant source`,
		},
		{
			name: "two blocks on one permission",
			model: validTestRegistry(validTestType("thing",
				withExclusions(validTestPermission("read"), append(Blocked(AgentUnsafe(), "thing read access"), Blocked(AgentUnsafe(), "thing read access")...)...),
			)),
			want: `thing.read: at most one block per permission`,
		},
		{
			name: "system role names a scope twice",
			model: func() Registry {
				r := validTestRegistry(validTestType("thing", validTestPermission("read")))
				r.SystemRoles = map[string][]Scope{"admin": {"thing:read", "thing:read"}}
				return r
			}(),
			want: `system role "admin" names "thing:read" twice`,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			err := tc.model.Validate()
			require.Error(t, err)
			require.Contains(t, err.Error(), tc.want)
		})
	}
}

// Source has no default: a permission that leaves it unset is rejected.
func TestRegistryValidate_RequiresSource(t *testing.T) {
	t.Parallel()

	read := validTestPermission("read")
	read.Source = ""
	err := validTestRegistry(validTestType("thing", read)).Validate()
	require.Error(t, err)
	require.Contains(t, err.Error(), `thing.read: Source must be GrantSourceRoles or GrantSourceRiskPolicy, got ""`)
}

func TestSafeSince_RejectsVersionsBelowOne(t *testing.T) {
	t.Parallel()
	require.Panics(t, func() { SafeSince(0) })
	require.Panics(t, func() { SafeSince(-1) })
}

func TestResourceType_Scopes(t *testing.T) {
	t.Parallel()

	project := validTestType("project")
	require.Equal(t, Scope("project:write"), project.ScopeFor(validTestPermission("write")))
	require.Equal(t, Scope("project:blocked_write"), project.BlockScopeFor(validTestPermission("write")))

	// A historical scope keeps its own prefix for the block too.
	plugin := withTestScope(validTestPermission("plugin_write"), ScopePluginWrite)
	require.Equal(t, ScopePluginWrite, project.ScopeFor(plugin))
	require.Equal(t, ScopePluginBlockedWrite, project.BlockScopeFor(plugin))
}

func withTestScope(p Permission, s Scope) Permission {
	p.Scope = s
	return p
}
