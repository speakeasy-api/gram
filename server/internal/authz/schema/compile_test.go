package schema_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	s "github.com/speakeasy-api/gram/server/internal/authz/schema"
)

var testConfig = s.Config{
	Roles:         []string{"admin", "member"},
	WriteSurfaces: []string{"access", "risk_policy"},
	Conditions: []s.Condition{
		{Name: "mcp_tools", SelectorKeys: []string{"tool", "disposition"}},
	},
	RetiredRuntimeScopes: []string{"mcp_approval:read"},
}

func organizationType() s.TypeDecl {
	return s.NewType("organization", s.StoredKind("org"), s.WrittenBy("access"),
		s.Grant("reader", s.StoredAs("org:read"), s.Describe("Read the organization"), s.DefaultFor("admin", "member")),
		s.Grant("admin", s.StoredAs("org:admin"), s.Describe("Administer the organization"), s.DefaultFor("admin")),
		s.Block("read_blocked", s.StoredAs("org:blocked_read")),
		s.Block("admin_blocked", s.StoredAs("org:blocked_admin")),
		s.Permission("read", s.Union(s.Rel("reader"), s.Rel("admin")),
			s.Checks("org:read"), s.ExcludedBy("read_blocked")),
		s.Permission("administer", s.Rel("admin"),
			s.Checks("org:admin"), s.ExcludedBy("admin_blocked", "read_blocked")),
	)
}

func projectType() s.TypeDecl {
	return s.NewType("project", s.Parent("organization"), s.WrittenBy("access"),
		s.Grant("reader", s.StoredAs("project:read"), s.AnchoredAt("organization", s.Self), s.Describe("Read the project")),
		s.Grant("publisher", s.StoredAs("plugin:write"), s.Describe("Publish plugins")),
		s.Block("publish_blocked", s.StoredAs("plugin:blocked_write")),
		s.Permission("read", s.Rel("reader"), s.Checks("project:read")),
		s.Permission("publish_plugin", s.Rel("publisher"), s.Checks("plugin:write"), s.ExcludedBy("publish_blocked")),
		s.Alternatives("plugin_publication",
			s.Alternative("project", "publish_plugin"),
			s.Alternative("organization", "administer"),
			s.VetoBlocks()),
	)
}

func mcpServerType() s.TypeDecl {
	return s.NewType("mcp_server", s.Parent("project"), s.StoredKind("mcp"), s.WrittenBy("access"),
		s.SelectorKeys("project_id", "tool", "disposition"),
		s.Grant("viewer", s.StoredAs("mcp:read"), s.AnchoredAt("organization", "project", s.Self),
			s.Conditions("mcp_tools"), s.Describe("Inspect server configuration"), s.RuntimeSafeSince(1)),
		s.Grant("editor", s.StoredAs("mcp:write"), s.AnchoredAt("organization", "project", s.Self),
			s.Conditions("mcp_tools"), s.Describe("Change server configuration")),
		s.Grant("connector", s.StoredAs("mcp:connect"), s.AnchoredAt("organization", "project", s.Self),
			s.Conditions("mcp_tools"), s.Describe("Connect and invoke permitted tools")),
		s.Block("connect_blocked", s.StoredAs("mcp:blocked_connect"), s.Conditions("mcp_tools")),
		s.Permission("read", s.Union(s.Rel("viewer"), s.Rel("editor")), s.Checks("mcp:read")),
		s.Permission("edit", s.Rel("editor"), s.Checks("mcp:write")),
		s.Permission("connect", s.Union(s.Rel("connector"), s.Perm("read")),
			s.Checks("mcp:connect"), s.ExcludedBy("connect_blocked")),
	)
}

func agentType() s.TypeDecl {
	return s.NewType("agent", s.Parent("organization"), s.WrittenBy("access"),
		s.Attr("owner", s.FactUser),
		s.Attr("deleted", s.FactBool),
		s.Attr("owner_reassignment_required_at", s.FactNullableTimestamp),
		s.Guard("current", s.All(s.IsFalse("deleted"), s.IsNull("owner_reassignment_required_at"))),
		s.Permission("is_owner", s.When("current", s.Is("owner"))),
	)
}

func chatType() s.TypeDecl {
	return s.NewType("chat", s.Parent("project"), s.WrittenBy("access"),
		s.Attr("internal_user_id", s.FactNullableString),
		s.Attr("external_user_id", s.FactNullableString),
		s.Attr("agent", "agent"),
		s.Derived("author", s.NonEmptyPrincipal(s.FactUser, "internal_user_id")),
		s.Guard("agent_only", s.All(
			s.IsNull("internal_user_id"),
			s.NullOrEmpty("external_user_id"),
			s.Present("agent"),
		)),
		s.Grant("reader", s.StoredAs("chat:read"), s.AnchoredAt("organization", s.Self),
			s.Sensitive(), s.Describe("Read session transcripts")),
		s.Grant("writer", s.StoredAs("chat:write"), s.AnchoredAt("organization", s.Self),
			s.Describe("Change session transcripts")),
		s.Permission("read_grant", s.Union(s.Rel("reader"), s.Rel("writer")), s.Checks("chat:read")),
		s.Permission("write_grant", s.Rel("writer"), s.Checks("chat:write")),
		s.Permission("read", s.Union(
			s.Perm("read_grant"), s.Is("author"),
			s.When("agent_only", s.Via("agent", "is_owner")),
		)),
		s.Permission("write", s.Union(s.Perm("write_grant"), s.Is("author"))),
	)
}

func riskPolicyType() s.TypeDecl {
	return s.NewType("risk_policy", s.WrittenBy("risk_policy"),
		s.Grant("included", s.StoredAs("risk_policy:evaluate"), s.Describe("Apply the policy")),
		s.Grant("bypassed", s.StoredAs("risk_policy:bypass"), s.Describe("Skip the policy")),
		s.Permission("evaluate", s.Minus(s.Rel("included"), s.Rel("bypassed")), s.Checks("risk_policy:evaluate")),
		s.Permission("bypass", s.Rel("bypassed"), s.Checks("risk_policy:bypass")),
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
	require.Equal(t, "viewer", mcpRead.Relation)
	require.Equal(t, "mcp", mcpRead.ResourceKind)
	require.Equal(t, s.MatchingPermissive, mcpRead.Matching)
	require.Equal(t, s.VisibilityUserVisible, mcpRead.Visibility)
	require.Equal(t, "access", mcpRead.WrittenBy)
	require.Equal(t, []string{"organization", "project", s.Self}, mcpRead.Anchors)
	require.Equal(t, []string{"mcp_tools"}, mcpRead.Conditions)
	require.Equal(t, []string{"disposition", "project_id", "tool"}, mcpRead.SelectorKeys)
	require.Equal(t, 1, mcpRead.RuntimeSafeSince)

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
	require.Empty(t, read.Checks)
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
	require.Equal(t, "editor", edit.Expr.Name())
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
	require.True(t, alts.VetoBlocks)
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
	require.Equal(t, "connector", permAgain.Expr.Operands()[0].Name())

	require.NotEqual(t, "mutated", reg.Scopes()[0].Name)
	require.NotEqual(t, "mutated", reg.Types()[0].Permissions[0])
}

func TestCompile_ReportsEveryProblem(t *testing.T) {
	t.Parallel()

	_, err := s.Compile(testConfig,
		s.NewType("broken", s.WrittenBy("access"),
			s.Grant("reader", s.StoredAs("broken:read")),
			s.Permission("read", s.Rel("missing"), s.Checks("broken:read")),
		),
	)
	var compileErr *s.CompileError
	require.ErrorAs(t, err, &compileErr)
	require.Len(t, compileErr.Problems, 3)
	require.Contains(t, err.Error(), `type "broken": grant "reader": user-visible grant needs a Describe`)
	require.Contains(t, err.Error(), `type "broken": permission "read": Rel names unknown relation "missing"`)
	require.Contains(t, err.Error(), `type "broken": permission "read": Checks "broken:read" but no Rel("reader") satisfies it`)
}

func TestCompile_RejectsInvalidDeclarations(t *testing.T) {
	t.Parallel()

	grant := func(name, scope string, opts ...s.RelationOption) s.TypeOption {
		return s.Grant(name, append([]s.RelationOption{s.StoredAs(scope), s.Describe(name)}, opts...)...)
	}
	simple := func(name string, opts ...s.TypeOption) s.TypeDecl {
		base := []s.TypeOption{
			s.WrittenBy("access"),
			grant("reader", name+":read"),
			s.Permission("read", s.Rel("reader"), s.Checks(name+":read")),
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
			types: []s.TypeDecl{simple("thing"), s.NewType("other", s.WrittenBy("access"),
				grant("reader", "thing:read"),
				s.Permission("read", s.Rel("reader"), s.Checks("thing:read")),
			)},
			want: `stored scope "thing:read" is already declared by type "thing": grant "reader"`,
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
			types: []s.TypeDecl{simple("org"), s.NewType("thing", s.WrittenBy("access"),
				grant("reader", "thing:read", s.AnchoredAt("org")),
				s.Permission("read", s.Rel("reader"), s.Checks("thing:read")),
			)},
			want: `anchor "org" is neither $self nor a type on the parent chain []`,
		},
		{
			name:  "missing stored scope",
			types: []s.TypeDecl{simple("thing", s.Grant("other", s.Describe("other")))},
			want:  `type "thing": grant "other": missing StoredAs`,
		},
		{
			name:  "malformed stored scope",
			types: []s.TypeDecl{simple("thing", s.Block("blocked", s.StoredAs("thing")))},
			want:  `stored scope "thing" must have the form <resource>:<action>`,
		},
		{
			name:  "block without blocked_ action",
			types: []s.TypeDecl{simple("thing", s.Block("blocked", s.StoredAs("thing:deny")))},
			want:  `stored scope "thing:deny" must be a blocked_* scope`,
		},
		{
			name:  "grant on a blocking scope",
			types: []s.TypeDecl{simple("thing", grant("deny", "thing:blocked_read"))},
			want:  `stored scope "thing:blocked_read" is a blocking scope; declare it with Block`,
		},
		{
			name:  "missing write surface",
			types: []s.TypeDecl{s.NewType("thing", grant("reader", "thing:read"), s.Permission("read", s.Rel("reader"), s.Checks("thing:read")))},
			want:  `no WrittenBy surface on the relation or its type`,
		},
		{
			name:  "unknown write surface",
			types: []s.TypeDecl{simple("thing", s.WrittenBy("billing"))},
			want:  `write surface "billing" is not registered`,
		},
		{
			name:  "unknown role",
			types: []s.TypeDecl{simple("thing", grant("writer", "thing:write", s.DefaultFor("owner")), s.Permission("write", s.Rel("writer"), s.Checks("thing:write")))},
			want:  `role "owner" is not registered`,
		},
		{
			name:  "block as role default",
			types: []s.TypeDecl{simple("thing", s.Block("blocked", s.StoredAs("thing:blocked_read"), s.DefaultFor("admin")))},
			want:  `blocks cannot be role defaults`,
		},
		{
			name:  "unknown condition",
			types: []s.TypeDecl{simple("thing", grant("writer", "thing:write", s.Conditions("tools")), s.Permission("write", s.Rel("writer"), s.Checks("thing:write")))},
			want:  `condition "tools" is not registered`,
		},
		{
			name:  "condition key the relation does not accept",
			types: []s.TypeDecl{simple("thing", grant("writer", "thing:write", s.Conditions("mcp_tools")), s.Permission("write", s.Rel("writer"), s.Checks("thing:write")))},
			want:  `condition "mcp_tools" reads selector key "tool", which the relation does not accept`,
		},
		{
			name:  "reserved selector key",
			types: []s.TypeDecl{simple("thing", s.SelectorKeys("resource_id"))},
			want:  `selector keys [resource_id] must be distinct and exclude resource_kind and resource_id`,
		},
		{
			name:  "subject scoped grant",
			types: []s.TypeDecl{simple("thing", grant("writer", "thing:write", s.SubjectScoped("author")), s.Permission("write", s.Rel("writer"), s.Checks("thing:write")))},
			want:  `SubjectScoped is not supported by this foundation`,
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
			types: []s.TypeDecl{simple("thing", s.Block("blocked", s.StoredAs("thing:blocked_read")), s.Permission("denied", s.Rel("blocked")))},
			want:  `Rel names block "blocked"; apply blocks with ExcludedBy`,
		},
		{
			name:  "ExcludedBy a grant",
			types: []s.TypeDecl{simple("thing", s.Permission("guarded", s.Rel("reader"), s.ExcludedBy("reader")))},
			want:  `ExcludedBy names grant "reader"; it needs a Block`,
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
			types: []s.TypeDecl{simple("thing", s.Permission("guarded", s.When("missing", s.Rel("reader"))))},
			want:  `When names unknown guard "missing"`,
		},
		{
			name:  "missing expression",
			types: []s.TypeDecl{simple("thing", s.Permission("empty", s.Expr{}))},
			want:  `permission "empty": missing expression`,
		},
		{
			name:  "Checks a permission with ownership",
			types: []s.TypeDecl{s.NewType("thing", s.WrittenBy("access"), s.Attr("owner", s.FactUser), grant("reader", "thing:read"), s.Permission("read", s.Union(s.Rel("reader"), s.Is("owner")), s.Checks("thing:read")))},
			want:  `a permission that Checks a scope may use only Rel, Union, Minus and grant-only Perm`,
		},
		{
			name:  "grant nobody checks",
			types: []s.TypeDecl{simple("thing", grant("writer", "thing:write"))},
			want:  `grant "writer": no permission Checks its scope "thing:write"`,
		},
		{
			name:  "scope checked twice",
			types: []s.TypeDecl{simple("thing", s.Permission("again", s.Rel("reader"), s.Checks("thing:read")))},
			want:  `scope "thing:read" is already checked by permission "read"`,
		},
		{
			name:  "Checks a scope of another type",
			types: []s.TypeDecl{simple("other"), simple("thing", s.Permission("theirs", s.Rel("reader"), s.Checks("other:read")))},
			want:  `Checks names "other:read", which is not a grant of this type`,
		},
		{
			name:  "single alternative",
			types: []s.TypeDecl{simple("thing", s.Alternatives("only", s.Alternative("thing", "read")))},
			want:  `alternatives "only": needs at least two alternatives`,
		},
		{
			name:  "unknown alternative",
			types: []s.TypeDecl{simple("thing", s.Alternatives("pair", s.Alternative("thing", "read"), s.Alternative("thing", "write")))},
			want:  `type "thing" declares no permission "write"`,
		},
		{
			name:  "veto without blocks",
			types: []s.TypeDecl{simple("thing", s.Permission("again", s.Rel("reader")), s.Alternatives("pair", s.Alternative("thing", "read"), s.Alternative("thing", "again"), s.VetoBlocks()))},
			want:  `VetoBlocks needs an alternative with ExcludedBy`,
		},
		{
			name:  "permission cycle",
			types: []s.TypeDecl{simple("thing", s.Permission("a", s.Perm("b")), s.Permission("b", s.Union(s.Rel("reader"), s.Perm("a"))))},
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
				Roles: testConfig.Roles, WriteSurfaces: testConfig.WriteSurfaces,
				Conditions: testConfig.Conditions, RetiredRuntimeScopes: []string{"thing:read"},
			},
			types: []s.TypeDecl{simple("thing")},
			want:  `retired runtime scope "thing:read" is still declared`,
		},
		{
			name: "duplicate condition",
			cfg: s.Config{
				Roles: testConfig.Roles, WriteSurfaces: testConfig.WriteSurfaces,
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

func TestCompile_RejectsConflictingBlockExpansions(t *testing.T) {
	t.Parallel()

	_, err := s.Compile(testConfig, s.NewType("thing", s.WrittenBy("access"),
		s.Grant("reader", s.StoredAs("thing:read"), s.Describe("read")),
		s.Grant("writer", s.StoredAs("thing:write"), s.Describe("write")),
		s.Block("read_blocked", s.StoredAs("thing:blocked_read")),
		s.Block("write_blocked", s.StoredAs("thing:blocked_write")),
		s.Block("all_blocked", s.StoredAs("thing:blocked_all")),
		s.Permission("read", s.Rel("reader"), s.Checks("thing:read"), s.ExcludedBy("read_blocked", "all_blocked")),
		s.Permission("write", s.Rel("writer"), s.Checks("thing:write"), s.ExcludedBy("read_blocked", "write_blocked")),
	))
	require.ErrorContains(t, err, `block "read_blocked" directly excludes several permissions with different broader blocks ([thing:blocked_all] and [thing:blocked_write])`)
}
