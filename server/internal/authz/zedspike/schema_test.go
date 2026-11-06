package zedspike

import (
	"context"
	"strings"
	"testing"

	"github.com/authzed/spicedb/pkg/development"
	devinterface "github.com/authzed/spicedb/pkg/proto/developer/v1"
	"github.com/stretchr/testify/require"
)

// allowedWarnings are SpiceDB developer warnings the schema accepts.
// arrow-references-relation asks to point arrows at permissions rather than
// relations; the direct organization edges are a decided design, so the
// hint fires on every child grant.
var allowedWarnings = map[string]bool{
	"arrow-references-relation": true,
}

// warningName reads the name SpiceDB appends to a warning message in
// parentheses.
func warningName(message string) string {
	open := strings.LastIndex(message, "(")
	if open < 0 || !strings.HasSuffix(message, ")") {
		return ""
	}
	return message[open+1 : len(message)-1]
}

func newDevContext(t *testing.T) *development.DevContext {
	t.Helper()
	devCtx, devErrs, err := development.NewDevContext(context.Background(), &devinterface.RequestContext{
		Schema: SchemaSource(),
	})
	require.NoError(t, err)
	require.Nil(t, devErrs, "SpiceDB rejected rbac.zed: %v", devErrs)
	t.Cleanup(devCtx.Dispose)
	return devCtx
}

// TestSchemaCompiles runs SpiceDB's compiler and its type-system validation.
func TestSchemaCompiles(t *testing.T) {
	t.Parallel()
	compiled, err := CompileSchema()
	require.NoError(t, err)
	require.Len(t, compiled.ObjectDefinitions, 14)
	require.Len(t, compiled.CaveatDefinitions, 9)
	newDevContext(t)
}

// TestSchemaHasNoWarnings fails on any SpiceDB developer warning outside the
// allow-list. Compile and validation accept an arrow to a misspelled
// relation; only the warnings catch it.
func TestSchemaHasNoWarnings(t *testing.T) {
	t.Parallel()
	devCtx := newDevContext(t)
	warnings, err := development.GetWarnings(context.Background(), devCtx)
	require.NoError(t, err)
	for _, w := range warnings {
		if allowedWarnings[warningName(w.GetMessage())] {
			continue
		}
		t.Errorf("rbac.zed:%d: %s", w.GetLine(), w.GetMessage())
	}
}

// TestRegistryMatchesTables compiles the schema into the registry, which
// fails when rbac.zed and the catalogue, edges, facts or answers disagree.
func TestRegistryMatchesTables(t *testing.T) {
	t.Parallel()
	reg, err := LoadRegistry()
	require.NoError(t, err)
	require.Equal(t, RelationDirect, reg.Types["mcp_server"].Relations["mcp_connect_direct"].Kind)
	require.Equal(t, RelationOwnBlock, reg.Types["organization"].Relations["mcp_blocked_connect_own"].Kind)
	require.Equal(t, RelationEdge, reg.Types["chat"].Relations["project"].Kind)
	require.Equal(t, RelationFact, reg.Types["chat"].Relations["agent"].Kind)
}

// TestTypedMethodsNamePublicPermissions fails when a typed method names a
// permission the schema lacks or a building block, and when a public
// permission has no typed method.
func TestTypedMethodsNamePublicPermissions(t *testing.T) {
	t.Parallel()
	reg, err := LoadRegistry()
	require.NoError(t, err)

	named := map[PermissionKey]bool{}
	for _, c := range typedChecks() {
		def := reg.Types[c.resourceType]
		require.NotNil(t, def, "%s: no type %s", c, c.resourceType)
		perm := def.Permissions[c.permission]
		require.NotNil(t, perm, "%s: no permission %s", c, c.permission)
		require.False(t, perm.Internal, "%s names a building block", c)
		named[PermissionKey{c.resourceType, c.permission}] = true
	}

	// Public permissions reached only through another permission: the old
	// tables still answer chat:read and chat:write grants, is_owner is the
	// target of chat's agent arrow, and publish_plugin is one branch of
	// plugin_publication.
	reachedIndirectly := map[PermissionKey]bool{
		{"project", "publish_plugin"}: true,
		{"chat", "read_grant"}:        true,
		{"chat", "write_grant"}:       true,
		{"agent", "is_owner"}:         true,
	}
	for _, def := range reg.Types {
		for _, perm := range def.Permissions {
			key := PermissionKey{def.Name, perm.Name}
			if perm.Internal || reachedIndirectly[key] {
				continue
			}
			require.True(t, named[key], "%s#%s has no typed method", def.Name, perm.Name)
		}
	}
}

// TestLandRows pins how rows become relationships.
func TestLandRows(t *testing.T) {
	t.Parallel()
	reg, err := LoadRegistry()
	require.NoError(t, err)

	cases := []struct {
		row  GrantRow
		want []string
	}{
		{row(memberRole, ScopeMCPConnect, mcpRow("*", projectP)),
			[]string{"project:projP#mcp_connect@role:organization_members#member[mcp_tools_allow]"}},
		{row(memberRole, ScopeMCPConnect, mcpRow("*", "")),
			[]string{"organization:org1#mcp_connect@role:organization_members#member[mcp_tools_allow]"}},
		{row("user:dana", ScopeMCPConnect, mcpRow("S", projectP)),
			[]string{"mcp_server:S#mcp_connect@user:dana[mcp_tools_allow]", "mcp_server:S#mcp_connect_direct@user:dana[mcp_tools_direct]"}},
		{row("user:dana", ScopeMCPBlockedConnect, mcpRow("S", projectP)),
			[]string{"mcp_server:S#mcp_blocked_connect_own@user:dana[mcp_tools_block]"}},
		{row("user:all", ScopeMCPBlockedConnect, mcpRow("S", projectP)),
			[]string{"mcp_server:S#mcp_blocked_connect@user:*[mcp_tools_block]"}},
		{row("api_key:k1", ScopeMCPConnect, mcpRow("*", projectP)),
			[]string{"project:projP#mcp_connect@api_key:k1"}},
		{row("user:all", ScopeMCPAuth, DefaultForEveryone[0].Selector),
			[]string{"organization:org1#mcp_auth@user:*[auth_client_match]"}},
	}
	for _, tc := range cases {
		landed, err := reg.Land(org, tc.row)
		require.NoError(t, err)
		var got []string
		for _, rel := range landed {
			got = append(got, rel.String())
		}
		require.Equal(t, tc.want, got)
	}

	_, err = reg.Land(org, row(memberRole, ScopeSkillRead, map[string]string{"resource_kind": "skill", "resource_id": projectP, "project_id": projectP}))
	require.ErrorIs(t, err, ErrUnmigratedRow)
}
