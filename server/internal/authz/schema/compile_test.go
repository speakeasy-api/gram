package schema_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	s "github.com/speakeasy-api/gram/server/internal/authz/schema"
)

var testConfig = s.Config{
	Roles:         []string{"admin", "member"},
	GrantSurfaces: []string{"access", "risk_policy"},
	Conditions: []s.Condition{
		{Name: "mcp_tools", SelectorKeys: []string{"tool", "disposition"}},
	},
	RetiredRuntimeScopes: []string{"mcp_approval:read"},
}

func organizationType() s.TypeDecl {
	return s.NewType("organization", s.StoredKind("org"), s.GrantSurface("access"),
		s.Grant("org:read", s.GrantSpec{Description: "Read the organization", DefaultFor: []string{"admin", "member"}}),
		s.Grant("org:admin", s.GrantSpec{Description: "Administer the organization", DefaultFor: []string{"admin"}}),
		s.Block("org:blocked_read", s.BlockSpec{}),
		s.Block("org:blocked_admin", s.BlockSpec{ImpliedBy: []string{"org:blocked_read"}}),
		s.Permission("read", s.Union(s.Rel("org:read"), s.Rel("org:admin")),
			s.AnswersScope("org:read"), s.ExcludedBy("org:blocked_read")),
		s.Permission("administer", s.Rel("org:admin"),
			s.AnswersScope("org:admin"), s.ExcludedBy("org:blocked_admin")),
	)
}

func projectType() s.TypeDecl {
	return s.NewType("project", s.Parent("organization"), s.GrantSurface("access"),
		s.Grant("project:read", s.GrantSpec{AnchoredAt: []string{"organization", s.Self}, Description: "Read the project"}),
		s.Grant("plugin:write", s.GrantSpec{Description: "Publish plugins"}),
		s.Block("plugin:blocked_write", s.BlockSpec{}),
		s.Permission("read", s.Rel("project:read"), s.AnswersScope("project:read")),
		s.Permission("publish_plugin", s.Rel("plugin:write"), s.AnswersScope("plugin:write"), s.ExcludedBy("plugin:blocked_write")),
		s.Permission("plugin_publication", s.AnyOf(s.Perm("publish_plugin"), s.Via("organization", "administer")), s.AnyBlockVetoes()),
	)
}

func mcpServerType() s.TypeDecl {
	return s.NewType("mcp_server", s.Parent("project"), s.StoredKind("mcp"), s.GrantSurface("access"),
		s.SelectorKeys("project_id", "tool", "disposition"),
		s.Grant("mcp:read", s.GrantSpec{AnchoredAt: []string{"organization", "project", s.Self}, Conditions: []string{"mcp_tools"}, Description: "Inspect server configuration", DelegatableSince: 1}),
		s.Grant("mcp:write", s.GrantSpec{AnchoredAt: []string{"organization", "project", s.Self}, Conditions: []string{"mcp_tools"}, Description: "Change server configuration"}),
		s.Grant("mcp:connect", s.GrantSpec{AnchoredAt: []string{"organization", "project", s.Self}, Conditions: []string{"mcp_tools"}, Description: "Connect and invoke permitted tools"}),
		s.Block("mcp:blocked_connect", s.BlockSpec{Conditions: []string{"mcp_tools"}}),
		s.Permission("read", s.Union(s.Rel("mcp:read"), s.Rel("mcp:write")), s.AnswersScope("mcp:read")),
		s.Permission("edit", s.Rel("mcp:write"), s.AnswersScope("mcp:write")),
		s.Permission("connect", s.Union(s.Rel("mcp:connect"), s.Perm("read")),
			s.AnswersScope("mcp:connect"), s.ExcludedBy("mcp:blocked_connect")),
	)
}

func agentType() s.TypeDecl {
	return s.NewType("agent", s.Parent("organization"), s.GrantSurface("access"),
		s.Attr("owner", s.FactUser),
		s.Attr("deleted", s.FactBool),
		s.Attr("owner_reassignment_required_at", s.FactNullableTimestamp),
		s.Guard("current", s.All(s.IsFalse("deleted"), s.IsNull("owner_reassignment_required_at"))),
		s.Permission("is_owner", s.When("current", s.Is("owner"))),
	)
}

func chatType() s.TypeDecl {
	return s.NewType("chat", s.Parent("project"), s.GrantSurface("access"),
		s.Attr("internal_user_id", s.FactNullableString),
		s.Attr("external_user_id", s.FactNullableString),
		s.Attr("agent", "agent"),
		s.Derived("author", s.NonEmptyPrincipal(s.FactUser, "internal_user_id")),
		s.Guard("agent_only", s.All(
			s.IsNull("internal_user_id"),
			s.NullOrEmpty("external_user_id"),
			s.Present("agent"),
		)),
		s.Grant("chat:read", s.GrantSpec{AnchoredAt: []string{"organization", s.Self}, Sensitive: true, Description: "Read session transcripts"}),
		s.Grant("chat:write", s.GrantSpec{AnchoredAt: []string{"organization", s.Self}, Description: "Change session transcripts"}),
		s.Permission("read_grant", s.Union(s.Rel("chat:read"), s.Rel("chat:write")), s.AnswersScope("chat:read")),
		s.Permission("write_grant", s.Rel("chat:write"), s.AnswersScope("chat:write")),
		s.Permission("read", s.Union(
			s.Perm("read_grant"), s.Is("author"),
			s.When("agent_only", s.Via("agent", "is_owner")),
		)),
		s.Permission("write", s.Union(s.Perm("write_grant"), s.Is("author"))),
	)
}

func riskPolicyType() s.TypeDecl {
	return s.NewType("risk_policy", s.GrantSurface("risk_policy"),
		s.Grant("risk_policy:evaluate", s.GrantSpec{Description: "Apply the policy"}),
		s.Grant("risk_policy:bypass", s.GrantSpec{Description: "Skip the policy"}),
		s.Permission("evaluate", s.Minus(s.Rel("risk_policy:evaluate"), s.Rel("risk_policy:bypass")), s.AnswersScope("risk_policy:evaluate")),
		s.Permission("bypass", s.Rel("risk_policy:bypass"), s.AnswersScope("risk_policy:bypass")),
	)
}

func validTypes() []s.TypeDecl {
	return []s.TypeDecl{
		organizationType(), projectType(), mcpServerType(), agentType(), chatType(), riskPolicyType(),
	}
}

func compileValid(t *testing.T) *s.Registry {
	t.Helper()
	reg, err := s.Compile(testConfig, validTypes()...)
	require.NoError(t, err)
	return reg
}

func TestCompile_SyntheticDeclarations(t *testing.T) {
	t.Parallel()

	reg := compileValid(t)

	mcpRead, ok := reg.Scope("mcp:read")
	require.True(t, ok)
	require.Equal(t, "mcp_server", mcpRead.Type)
	require.Equal(t, "mcp", mcpRead.ResourceKind)
	require.Equal(t, s.MatchingPermissive, mcpRead.Matching)
	require.Equal(t, s.VisibilityUserVisible, mcpRead.Visibility)
	require.Equal(t, "access", mcpRead.GrantSurface)
	require.Equal(t, []string{"organization", "project", s.Self}, mcpRead.Anchors)
	require.Equal(t, []string{"mcp_tools"}, mcpRead.Conditions)
	require.Equal(t, []string{"disposition", "project_id", "tool"}, mcpRead.SelectorKeys)
	require.Equal(t, 1, mcpRead.DelegatableSince)

	block, ok := reg.Scope("mcp:blocked_connect")
	require.True(t, ok)
	require.True(t, block.Block)
	require.Equal(t, s.MatchingStrict, block.Matching)
	require.Equal(t, s.VisibilityInternal, block.Visibility)

	chatRead, ok := reg.Scope("chat:read")
	require.True(t, ok)
	require.True(t, chatRead.Sensitive)
	require.Equal(t, []string{"chat:write"}, chatRead.SatisfiedBy)

	require.Equal(t, []string{"org:admin", "org:read"}, reg.RoleDefaults("admin"))
	require.Equal(t, []string{"org:read"}, reg.RoleDefaults("member"))
}

func TestCompile_PermissionPrerequisites(t *testing.T) {
	t.Parallel()

	reg := compileValid(t)

	read, ok := reg.Permission(s.PermissionRef{Type: "chat", Permission: "read"})
	require.True(t, ok)
	require.Empty(t, read.AnswersScope)
	require.Equal(t, []string{"agent", "author", "external_user_id", "internal_user_id"}, read.Facts)
	require.Equal(t, []string{"chat:read", "chat:write"}, read.Scopes)
	require.Equal(t, []s.PermissionRef{{Type: "agent", Permission: "is_owner"}}, read.Traverses)

	connect, ok := reg.Permission(s.PermissionRef{Type: "mcp_server", Permission: "connect"})
	require.True(t, ok)
	require.Equal(t, []string{"mcp_tools"}, connect.Conditions)
	require.Equal(t, []string{"mcp:blocked_connect"}, connect.ExcludedBy)
	require.Equal(t, []string{"mcp:blocked_connect", "mcp:connect", "mcp:read", "mcp:write"}, connect.Scopes)

	ref, ok := reg.PermissionForCheck("mcp:connect")
	require.True(t, ok)
	require.Equal(t, s.PermissionRef{Type: "mcp_server", Permission: "connect"}, ref)

	isOwner, ok := reg.Permission(s.PermissionRef{Type: "agent", Permission: "is_owner"})
	require.True(t, ok)
	require.Equal(t, []string{"deleted", "owner", "owner_reassignment_required_at"}, isOwner.Facts)
	guard, ok := reg.Guard("agent", "current")
	require.True(t, ok)
	require.Equal(t, s.CondAll, guard.Op())
}

func TestCompile_ResolvesExpressions(t *testing.T) {
	t.Parallel()

	reg := compileValid(t)

	read, ok := reg.Permission(s.PermissionRef{Type: "chat", Permission: "read"})
	require.True(t, ok)
	require.Equal(t, s.OpUnion, read.Expr.Op())
	branches := read.Expr.Operands()
	require.Len(t, branches, 3)
	require.Equal(t, s.OpPerm, branches[0].Op())
	require.Equal(t, "read_grant", branches[0].Name())
	require.Equal(t, s.OpIs, branches[1].Op())
	require.Equal(t, "author", branches[1].Name())
	require.Equal(t, s.OpWhen, branches[2].Op())
	require.Equal(t, "agent_only", branches[2].Guard())
	via := branches[2].Operands()[0]
	require.Equal(t, s.OpVia, via.Op())
	require.Equal(t, "agent", via.Name())
	require.Equal(t, "is_owner", via.Permission())
	require.Equal(t, "agent", via.TargetType())

	edit, ok := reg.Permission(s.PermissionRef{Type: "mcp_server", Permission: "edit"})
	require.True(t, ok)
	require.Equal(t, s.OpRel, edit.Expr.Op())
	require.Equal(t, "mcp:write", edit.Expr.Name())
	require.Equal(t, "mcp:write", edit.Expr.Scope())
}

func TestCompile_LegacyCheckProjection(t *testing.T) {
	t.Parallel()

	reg := compileValid(t)

	tests := []struct {
		scope       string
		satisfiedBy []string
		exclusion   string
		closure     []string
		subScopes   []string
	}{
		{"org:read", []string{"org:admin"}, "org:blocked_read", []string{"org:read"}, nil},
		{"org:admin", nil, "org:blocked_admin", []string{"org:admin", "org:read"}, []string{"org:read"}},
		{"org:blocked_admin", []string{"org:blocked_read"}, "", []string{"org:blocked_admin"}, nil},
		{"org:blocked_read", nil, "", []string{"org:blocked_admin", "org:blocked_read"}, nil},
		// Perm("read") reaches viewer and editor, so both satisfy connect directly.
		{"mcp:connect", []string{"mcp:read", "mcp:write"}, "mcp:blocked_connect", []string{"mcp:connect"}, nil},
		{"mcp:write", nil, "", []string{"mcp:connect", "mcp:read", "mcp:write"}, []string{"mcp:connect", "mcp:read"}},
		// Raw subtraction projects as the exclusion without block precedence.
		{"risk_policy:evaluate", nil, "risk_policy:bypass", []string{"risk_policy:evaluate"}, nil},
	}
	for _, tt := range tests {
		got, ok := reg.Scope(tt.scope)
		require.True(t, ok, tt.scope)
		require.Equal(t, tt.satisfiedBy, got.SatisfiedBy, tt.scope)
		require.Equal(t, tt.exclusion, got.Exclusion, tt.scope)
		require.Equal(t, tt.closure, got.Closure, tt.scope)
		require.Equal(t, tt.subScopes, got.SubScopes, tt.scope)
	}
}

func TestCompile_AlternativesAndRuntime(t *testing.T) {
	t.Parallel()

	reg := compileValid(t)

	alts, ok := reg.Alternatives(s.PermissionRef{Type: "project", Permission: "plugin_publication"})
	require.True(t, ok)
	require.True(t, alts.AnyBlockVetoes)
	require.Equal(t, []s.PermissionRef{
		{Type: "project", Permission: "publish_plugin"},
		{Type: "organization", Permission: "administer"},
	}, alts.Options)

	runtime := reg.RuntimeScopes()
	require.Contains(t, runtime, s.RuntimeScope{Name: "mcp:read", Lifecycle: s.RuntimeActive, SafeSince: 1})
	require.Contains(t, runtime, s.RuntimeScope{Name: "mcp:write", Lifecycle: s.RuntimeActive, SafeSince: 0})
	require.Contains(t, runtime, s.RuntimeScope{Name: "mcp_approval:read", Lifecycle: s.RuntimeRetired, SafeSince: 0})
	require.Len(t, runtime, len(reg.Scopes())+1)
}

// Parent and AnchoredAt describe containment only. A child type gains none of
// its parent's grants or permissions, and the parent gains none of the
// child's.
func TestCompile_ParentAndAnchorsImplyNothing(t *testing.T) {
	t.Parallel()

	reg := compileValid(t)

	for _, name := range []string{"project:read", "mcp:read", "mcp:connect"} {
		scope, ok := reg.Scope(name)
		require.True(t, ok)
		for _, satisfying := range scope.SatisfiedBy {
			other, ok := reg.Scope(satisfying)
			require.True(t, ok)
			require.Equal(t, scope.Type, other.Type, "%s is satisfied by %s from another type", name, satisfying)
		}
	}

	mcpType, ok := reg.Type("mcp_server")
	require.True(t, ok)
	require.Equal(t, "project", mcpType.Parent)
	require.Equal(t, []string{"read", "edit", "connect"}, mcpType.Permissions)

	connect, ok := reg.Permission(s.PermissionRef{Type: "mcp_server", Permission: "connect"})
	require.True(t, ok)
	require.NotContains(t, connect.Scopes, "project:read")
	require.NotContains(t, connect.Scopes, "org:admin")
	require.Empty(t, connect.Traverses)
}

func TestRegistry_ReturnsCopies(t *testing.T) {
	t.Parallel()

	reg := compileValid(t)

	scope, ok := reg.Scope("mcp:read")
	require.True(t, ok)
	scope.Anchors[0] = "mutated"
	scope.SelectorKeys[0] = "mutated"
	scope.Closure[0] = "mutated"

	perm, ok := reg.Permission(s.PermissionRef{Type: "mcp_server", Permission: "connect"})
	require.True(t, ok)
	perm.Scopes[0] = "mutated"
	perm.Expr.Operands()[0] = s.Rel("mutated")

	scopes := reg.Scopes()
	scopes[0].Name = "mutated"

	types := reg.Types()
	types[0].Permissions[0] = "mutated"

	again, ok := reg.Scope("mcp:read")
	require.True(t, ok)
	require.Equal(t, []string{"organization", "project", s.Self}, again.Anchors)
	require.Equal(t, []string{"disposition", "project_id", "tool"}, again.SelectorKeys)
	require.Equal(t, []string{"mcp:connect", "mcp:read"}, again.Closure)

	permAgain, ok := reg.Permission(s.PermissionRef{Type: "mcp_server", Permission: "connect"})
	require.True(t, ok)
	require.Equal(t, "mcp:blocked_connect", permAgain.Scopes[0])
	require.Equal(t, "mcp:connect", permAgain.Expr.Operands()[0].Name())

	require.NotEqual(t, "mutated", reg.Scopes()[0].Name)
	require.NotEqual(t, "mutated", reg.Types()[0].Permissions[0])
}

func TestCompile_ReportsEveryProblem(t *testing.T) {
	t.Parallel()

	_, err := s.Compile(testConfig,
		s.NewType("broken", s.GrantSurface("access"),
			s.Grant("broken:read", s.GrantSpec{}),
			s.Permission("read", s.Rel("missing"), s.AnswersScope("broken:read")),
		),
	)
	var compileErr *s.CompileError
	require.ErrorAs(t, err, &compileErr)
	require.Len(t, compileErr.Problems, 3)
	require.Contains(t, err.Error(), `type "broken": grant "broken:read": user-visible grant needs a Description`)
	require.Contains(t, err.Error(), `type "broken": permission "read": Rel names unknown relation "missing"`)
	require.Contains(t, err.Error(), `type "broken": permission "read": AnswersScope "broken:read" but no Rel("broken:read") satisfies it`)
}

func TestCompile_RejectsInvalidDeclarations(t *testing.T) {
	t.Parallel()

	grant := func(scope string, spec s.GrantSpec) s.TypeOption {
		spec.Description = scope
		return s.Grant(scope, spec)
	}
	simple := func(name string, opts ...s.TypeOption) s.TypeDecl {
		base := []s.TypeOption{
			s.GrantSurface("access"),
			grant(name+":read", s.GrantSpec{}),
			s.Permission("read", s.Rel(name+":read"), s.AnswersScope(name+":read")),
		}
		return s.NewType(name, append(base, opts...)...)
	}

	tests := []struct {
		name  string
		cfg   s.Config
		types []s.TypeDecl
		want  string
	}{
		{
			name:  "duplicate type",
			types: []s.TypeDecl{simple("thing"), simple("thing")},
			want:  `type "thing": declared twice`,
		},
		{
			name:  "reserved type name",
			types: []s.TypeDecl{s.NewType("user")},
			want:  `type "user": name is reserved`,
		},
		{
			name:  "shared resource kind",
			types: []s.TypeDecl{simple("thing"), simple("other", s.StoredKind("thing"))},
			want:  `resource kind "thing" is already used by type "thing"`,
		},
		{
			name: "duplicate stored scope",
			types: []s.TypeDecl{simple("thing"), s.NewType("other", s.GrantSurface("access"),
				grant("thing:read", s.GrantSpec{}),
				s.Permission("read", s.Rel("thing:read"), s.AnswersScope("thing:read")),
			)},
			want: `stored scope "thing:read" is already declared by type "thing": grant "thing:read"`,
		},
		{
			name:  "duplicate member name",
			types: []s.TypeDecl{simple("thing", s.Attr("read", s.FactBool))},
			want:  `type "thing": permission "read" reuses the name of a attr`,
		},
		{
			name:  "unknown parent",
			types: []s.TypeDecl{simple("thing", s.Parent("missing"))},
			want:  `type "thing": parent "missing" is not a declared type`,
		},
		{
			name:  "parent cycle",
			types: []s.TypeDecl{simple("a", s.Parent("b")), simple("b", s.Parent("a"))},
			want:  `type "a": parent chain forms a cycle`,
		},
		{
			name: "anchor outside parent chain",
			types: []s.TypeDecl{simple("org"), s.NewType("thing", s.GrantSurface("access"),
				grant("thing:read", s.GrantSpec{AnchoredAt: []string{"org"}}),
				s.Permission("read", s.Rel("thing:read"), s.AnswersScope("thing:read")),
			)},
			want: `anchor "org" is neither $self nor a type on the parent chain []`,
		},
		{
			name:  "missing stored scope",
			types: []s.TypeDecl{simple("thing", s.Grant("", s.GrantSpec{Description: "other"}))},
			want:  `type "thing": grant with empty name`,
		},
		{
			name:  "malformed stored scope",
			types: []s.TypeDecl{simple("thing", s.Block("thing", s.BlockSpec{}))},
			want:  `stored scope "thing" must have the form <resource>:<action>`,
		},
		{
			name:  "block without blocked_ action",
			types: []s.TypeDecl{simple("thing", s.Block("thing:deny", s.BlockSpec{}))},
			want:  `stored scope "thing:deny" must be a blocked_* scope`,
		},
		{
			name:  "block without an action",
			types: []s.TypeDecl{simple("thing", s.Block("thing:blocked_", s.BlockSpec{}))},
			want:  `stored scope "thing:blocked_" names no action after blocked_`,
		},
		{
			name:  "grant on a blocking scope",
			types: []s.TypeDecl{simple("thing", grant("thing:blocked_read", s.GrantSpec{}))},
			want:  `stored scope "thing:blocked_read" is a blocking scope; declare it with Block`,
		},
		{
			name:  "missing grant surface",
			types: []s.TypeDecl{s.NewType("thing", grant("thing:read", s.GrantSpec{}), s.Permission("read", s.Rel("thing:read"), s.AnswersScope("thing:read")))},
			want:  `no GrantSurface on the relation or its type`,
		},
		{
			name:  "unknown grant surface",
			types: []s.TypeDecl{simple("thing", s.GrantSurface("billing"))},
			want:  `grant surface "billing" is not registered`,
		},
		{
			name:  "unknown role",
			types: []s.TypeDecl{simple("thing", grant("thing:write", s.GrantSpec{DefaultFor: []string{"owner"}}), s.Permission("write", s.Rel("thing:write"), s.AnswersScope("thing:write")))},
			want:  `role "owner" is not registered`,
		},
		{
			name:  "unknown condition",
			types: []s.TypeDecl{simple("thing", grant("thing:write", s.GrantSpec{Conditions: []string{"tools"}}), s.Permission("write", s.Rel("thing:write"), s.AnswersScope("thing:write")))},
			want:  `condition "tools" is not registered`,
		},
		{
			name:  "condition key the relation does not accept",
			types: []s.TypeDecl{simple("thing", grant("thing:write", s.GrantSpec{Conditions: []string{"mcp_tools"}}), s.Permission("write", s.Rel("thing:write"), s.AnswersScope("thing:write")))},
			want:  `condition "mcp_tools" reads selector key "tool", which the relation does not accept`,
		},
		{
			name:  "reserved selector key",
			types: []s.TypeDecl{simple("thing", s.SelectorKeys("resource_id"))},
			want:  `selector keys [resource_id] must be distinct and exclude resource_kind and resource_id`,
		},
		{
			name:  "unknown attr type",
			types: []s.TypeDecl{simple("thing", s.Attr("owner", "person"))},
			want:  `attr "owner" has unknown fact type "person"`,
		},
		{
			name:  "derived from a non-string fact",
			types: []s.TypeDecl{simple("thing", s.Attr("flag", s.FactBool), s.Derived("author", s.NonEmptyPrincipal(s.FactUser, "flag")))},
			want:  `derived fact "author" reads attr "flag" of type "bool"; it needs a string`,
		},
		{
			name:  "guard reads incompatible fact",
			types: []s.TypeDecl{simple("thing", s.Attr("name", s.FactString), s.Guard("unnamed", s.IsNull("name")))},
			want:  `guard "unnamed": is_null cannot read fact "name" of type "string"`,
		},
		{
			name:  "empty All guard",
			types: []s.TypeDecl{simple("thing", s.Guard("always", s.All()))},
			want:  `guard "always": All needs at least one condition`,
		},
		{
			name:  "Rel to a block",
			types: []s.TypeDecl{simple("thing", s.Block("thing:blocked_read", s.BlockSpec{}), s.Permission("denied", s.Rel("thing:blocked_read")))},
			want:  `Rel names block "thing:blocked_read"; apply blocks with ExcludedBy`,
		},
		{
			name:  "ExcludedBy a grant",
			types: []s.TypeDecl{simple("thing", s.Permission("guarded", s.Rel("thing:read"), s.ExcludedBy("thing:read")))},
			want:  `ExcludedBy names grant "thing:read"; it needs a Block`,
		},
		{
			name:  "Is on a non-user fact",
			types: []s.TypeDecl{simple("thing", s.Attr("name", s.FactString), s.Permission("named", s.Is("name")))},
			want:  `Is needs a "user" fact, but "name" is "string"`,
		},
		{
			name:  "Via through a non-reference fact",
			types: []s.TypeDecl{simple("thing", s.Attr("owner", s.FactUser), s.Permission("owned", s.Via("owner", "read")))},
			want:  `Via attr "owner" has type "user", which is not a declared type`,
		},
		{
			name: "Via to an undeclared permission",
			types: []s.TypeDecl{simple("other"), simple("thing",
				s.Attr("other", "other"), s.Permission("through", s.Via("other", "write")))},
			want: `Via names permission "write", which type "other" does not declare`,
		},
		{
			name:  "When on an unknown guard",
			types: []s.TypeDecl{simple("thing", s.Permission("guarded", s.When("missing", s.Rel("thing:read"))))},
			want:  `When names unknown guard "missing"`,
		},
		{
			name:  "missing expression",
			types: []s.TypeDecl{simple("thing", s.Permission("empty", s.Expr{}))},
			want:  `permission "empty": missing expression`,
		},
		{
			name:  "AnswersScope on a permission with ownership",
			types: []s.TypeDecl{s.NewType("thing", s.GrantSurface("access"), s.Attr("owner", s.FactUser), grant("thing:read", s.GrantSpec{}), s.Permission("read", s.Union(s.Rel("thing:read"), s.Is("owner")), s.AnswersScope("thing:read")))},
			want:  `a permission that answers a scope may use only Rel, Union, Minus and grant-only Perm`,
		},
		{
			name:  "grant nobody answers",
			types: []s.TypeDecl{simple("thing", grant("thing:write", s.GrantSpec{}))},
			want:  `grant "thing:write": no permission answers its scope "thing:write"`,
		},
		{
			name:  "scope answered twice",
			types: []s.TypeDecl{simple("thing", s.Permission("again", s.Rel("thing:read"), s.AnswersScope("thing:read")))},
			want:  `scope "thing:read" is already answered by permission "read"`,
		},
		{
			name:  "AnswersScope names a scope of another type",
			types: []s.TypeDecl{simple("other"), simple("thing", s.Permission("theirs", s.Rel("thing:read"), s.AnswersScope("other:read")))},
			want:  `AnswersScope names "other:read", which is not a grant of this type`,
		},
		{
			name:  "single AnyOf branch",
			types: []s.TypeDecl{simple("thing", s.Permission("only", s.AnyOf(s.Perm("read"))))},
			want:  `permission "only": AnyOf needs at least two branches`,
		},
		{
			name:  "unknown AnyOf branch",
			types: []s.TypeDecl{simple("thing", s.Permission("pair", s.AnyOf(s.Perm("read"), s.Perm("write"))))},
			want:  `type "thing" declares no permission "write"`,
		},
		{
			name:  "repeated AnyOf branch",
			types: []s.TypeDecl{simple("thing", s.Permission("pair", s.AnyOf(s.Perm("read"), s.Perm("read"))))},
			want:  `AnyOf lists Perm("read") twice`,
		},
		{
			name:  "veto without blocks",
			types: []s.TypeDecl{simple("thing", s.Permission("again", s.Rel("thing:read")), s.Permission("pair", s.AnyOf(s.Perm("read"), s.Perm("again")), s.AnyBlockVetoes()))},
			want:  `AnyBlockVetoes needs a branch with ExcludedBy`,
		},
		{
			name:  "AnyBlockVetoes on an ordinary permission",
			types: []s.TypeDecl{simple("thing", s.Permission("again", s.Rel("thing:read"), s.AnyBlockVetoes()))},
			want:  `permission "again": AnyBlockVetoes applies only to an AnyOf permission`,
		},
		{
			name:  "AnyOf inside another expression",
			types: []s.TypeDecl{simple("thing", s.Permission("again", s.Rel("thing:read")), s.Permission("nested", s.Union(s.Rel("thing:read"), s.AnyOf(s.Perm("read"), s.Perm("again")))))},
			want:  `permission "nested": AnyOf must be the permission's whole expression`,
		},
		{
			name:  "AnyOf branch that matches grants directly",
			types: []s.TypeDecl{simple("thing", s.Permission("again", s.Rel("thing:read")), s.Permission("pair", s.AnyOf(s.Perm("read"), s.Rel("thing:read"))))},
			want:  `an AnyOf branch must be Perm or Via, not rel`,
		},
		{
			name:  "AnyOf with its own block",
			types: []s.TypeDecl{simple("thing", s.Block("thing:blocked_a", s.BlockSpec{}), s.Permission("again", s.Rel("thing:read")), s.Permission("pair", s.AnyOf(s.Perm("read"), s.Perm("again")), s.ExcludedBy("thing:blocked_a")))},
			want:  `permission "pair": ExcludedBy does not apply to AnyOf; put it on the branch's own permission`,
		},
		{
			name:  "AnyOf Via that is both the parent and an Attr",
			types: []s.TypeDecl{simple("other"), simple("thing", s.Parent("other"), s.Attr("other", "other"), s.Permission("pair", s.AnyOf(s.Perm("read"), s.Via("other", "read"))))},
			want:  `Via names "other", which is both the parent type and an Attr; rename the Attr`,
		},
		{
			name:  "AnyOf with a description",
			types: []s.TypeDecl{simple("thing", s.Permission("again", s.Rel("thing:read")), s.Permission("pair", s.AnyOf(s.Perm("read"), s.Perm("again")), s.Describe("Either")))},
			want:  `permission "pair": Describe does not apply to AnyOf`,
		},
		{
			name:  "AnyOf Via to an unrelated type",
			types: []s.TypeDecl{simple("other"), simple("thing", s.Permission("pair", s.AnyOf(s.Perm("read"), s.Via("other", "read"))))},
			want:  `Via names "other", which is neither the parent type nor an Attr`,
		},
		{
			name:  "ExcludedBy given twice",
			types: []s.TypeDecl{simple("thing", s.Block("thing:blocked_a", s.BlockSpec{}), s.Block("thing:blocked_b", s.BlockSpec{}), s.Permission("guarded", s.Rel("thing:read"), s.ExcludedBy("thing:blocked_a"), s.ExcludedBy("thing:blocked_b")))},
			want:  `ExcludedBy is given more than once; declare broader blocks with BlockSpec.ImpliedBy`,
		},
		{
			name:  "AlsoGrantedBy on a plain permission",
			types: []s.TypeDecl{simple("thing", s.Permission("guarded", s.Rel("thing:read"), s.AlsoGrantedBy("thing:write")))},
			want:  `AlsoGrantedBy applies only to ScopePermission; write the Union in the expression`,
		},
		{
			name:  "ImpliedBy an unknown block",
			types: []s.TypeDecl{simple("thing", s.Block("thing:blocked_write", s.BlockSpec{ImpliedBy: []string{"missing"}}))},
			want:  `block "thing:blocked_write": ImpliedBy names unknown block "missing"`,
		},
		{
			name:  "ImpliedBy a grant",
			types: []s.TypeDecl{simple("thing", s.Block("thing:blocked_write", s.BlockSpec{ImpliedBy: []string{"thing:read"}}))},
			want:  `block "thing:blocked_write": ImpliedBy names grant "thing:read"; it needs a Block`,
		},
		{
			name:  "ImpliedBy itself",
			types: []s.TypeDecl{simple("thing", s.Block("thing:blocked_write", s.BlockSpec{ImpliedBy: []string{"thing:blocked_write"}}))},
			want:  `block "thing:blocked_write": ImpliedBy names the block itself`,
		},
		{
			name: "ImpliedBy cycle",
			types: []s.TypeDecl{simple("thing",
				s.Block("thing:blocked_a", s.BlockSpec{ImpliedBy: []string{"thing:blocked_b"}}),
				s.Block("thing:blocked_b", s.BlockSpec{ImpliedBy: []string{"thing:blocked_a"}}))},
			want: `block "thing:blocked_a": ImpliedBy forms a cycle back to "thing:blocked_a"`,
		},
		{
			name:  "permission cycle",
			types: []s.TypeDecl{simple("thing", s.Permission("a", s.Perm("b")), s.Permission("b", s.Union(s.Rel("thing:read"), s.Perm("a"))))},
			want:  `permission cycle: thing.a -> thing.b -> thing.a`,
		},
		{
			name: "cross-type permission cycle",
			types: []s.TypeDecl{
				simple("left", s.Attr("right", "right"), s.Permission("hop", s.Via("right", "hop"))),
				simple("right", s.Attr("left", "left"), s.Permission("hop", s.Via("left", "hop"))),
			},
			want: `permission cycle: left.hop -> right.hop -> left.hop`,
		},
		{
			name: "retired runtime scope still declared",
			cfg: s.Config{
				Roles: testConfig.Roles, GrantSurfaces: testConfig.GrantSurfaces,
				Conditions: testConfig.Conditions, RetiredRuntimeScopes: []string{"thing:read"},
			},
			types: []s.TypeDecl{simple("thing")},
			want:  `retired runtime scope "thing:read" is still declared`,
		},
		{
			name: "duplicate condition",
			cfg: s.Config{
				Roles: testConfig.Roles, GrantSurfaces: testConfig.GrantSurfaces,
				Conditions: []s.Condition{{Name: "c", SelectorKeys: nil}, {Name: "c", SelectorKeys: nil}},
			},
			types: []s.TypeDecl{simple("thing")},
			want:  `config: condition "c" registered twice`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			cfg := tt.cfg
			if cfg.Roles == nil {
				cfg = testConfig
			}
			_, err := s.Compile(cfg, tt.types...)
			var compileErr *s.CompileError
			require.ErrorAs(t, err, &compileErr)
			require.Contains(t, err.Error(), tt.want)
		})
	}
}

// A permission is excluded by its direct block and by every block that
// implies it, directly or through another block.
func TestCompile_ExpandsImpliedBlocks(t *testing.T) {
	t.Parallel()

	reg, err := s.Compile(testConfig, s.NewType("thing", s.GrantSurface("access"),
		s.Grant("thing:read", s.GrantSpec{Description: "read"}),
		s.Grant("thing:write", s.GrantSpec{Description: "write"}),
		s.Grant("thing:admin", s.GrantSpec{Description: "admin"}),
		s.Block("thing:blocked_read", s.BlockSpec{}),
		s.Block("thing:blocked_write", s.BlockSpec{ImpliedBy: []string{"thing:blocked_read"}}),
		s.Block("thing:blocked_admin", s.BlockSpec{ImpliedBy: []string{"thing:blocked_write"}}),
		s.Permission("read", s.Rel("thing:read"), s.AnswersScope("thing:read"), s.ExcludedBy("thing:blocked_read")),
		s.Permission("write", s.Rel("thing:write"), s.AnswersScope("thing:write"), s.ExcludedBy("thing:blocked_write")),
		s.Permission("administer", s.Rel("thing:admin"), s.AnswersScope("thing:admin"), s.ExcludedBy("thing:blocked_admin")),
	))
	require.NoError(t, err)

	admin, ok := reg.Permission(s.PermissionRef{Type: "thing", Permission: "administer"})
	require.True(t, ok)
	require.Equal(t, []string{"thing:blocked_admin", "thing:blocked_read", "thing:blocked_write"}, admin.ExcludedBy)

	blockedAdmin, ok := reg.Scope("thing:blocked_admin")
	require.True(t, ok)
	require.Equal(t, []string{"thing:blocked_read", "thing:blocked_write"}, blockedAdmin.SatisfiedBy)

	writeScope, ok := reg.Scope("thing:write")
	require.True(t, ok)
	require.Equal(t, "thing:blocked_write", writeScope.Exclusion)
}

func TestCompile_FactsKeepDeclarationOrder(t *testing.T) {
	t.Parallel()

	reg, err := s.Compile(testConfig, s.NewType("note", s.GrantSurface("access"),
		s.Attr("author_id", s.FactNullableString),
		s.Derived("author", s.NonEmptyPrincipal(s.FactUser, "author_id")),
		s.Attr("archived", s.FactBool),
	))
	require.NoError(t, err)
	note, ok := reg.Type("note")
	require.True(t, ok)
	require.Equal(t, []s.Fact{
		{Name: "author_id", Type: s.FactNullableString, DerivedFrom: ""},
		{Name: "author", Type: s.FactUser, DerivedFrom: "author_id"},
		{Name: "archived", Type: s.FactBool, DerivedFrom: ""},
	}, note.Facts)
}

// Grant and Block copy their specs' slices, so changing a caller's slice
// after building one cannot change the declaration.
func TestSpecs_CopyTheirSlices(t *testing.T) {
	t.Parallel()

	anchors := []string{"organization", s.Self}
	roles := []string{"admin"}
	impliedBy := []string{"thing:blocked_read"}
	reader := s.Grant("thing:read", s.GrantSpec{Description: "read", AnchoredAt: anchors, DefaultFor: roles})
	writeBlocked := s.Block("thing:blocked_write", s.BlockSpec{ImpliedBy: impliedBy})
	anchors[0] = "project"
	roles[0] = "member"
	impliedBy[0] = "missing"

	reg, err := s.Compile(testConfig,
		s.NewType("organization", s.GrantSurface("access")),
		s.NewType("thing", s.Parent("organization"), s.GrantSurface("access"),
			reader,
			s.Grant("thing:write", s.GrantSpec{Description: "write"}),
			s.Block("thing:blocked_read", s.BlockSpec{}),
			writeBlocked,
			s.Permission("read", s.Rel("thing:read"), s.AnswersScope("thing:read"), s.ExcludedBy("thing:blocked_read")),
			s.Permission("write", s.Rel("thing:write"), s.AnswersScope("thing:write"), s.ExcludedBy("thing:blocked_write")),
		),
	)
	require.NoError(t, err)
	read, ok := reg.Scope("thing:read")
	require.True(t, ok)
	require.Equal(t, []string{"organization", s.Self}, read.Anchors)
	require.Equal(t, []string{"admin"}, read.Roles)
	require.Equal(t, "thing:blocked_read", read.Exclusion)
	write, ok := reg.Permission(s.PermissionRef{Type: "thing", Permission: "write"})
	require.True(t, ok)
	require.Equal(t, []string{"thing:blocked_write", "thing:blocked_read"}, write.ExcludedBy)
}
