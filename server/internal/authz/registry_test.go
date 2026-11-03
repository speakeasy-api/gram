package authz

import (
	"go/ast"
	"go/parser"
	"go/token"
	"strconv"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestBlockedScope(t *testing.T) {
	t.Parallel()

	require.Equal(t, ScopeOrgBlockedRead, BlockedScope(ScopeOrgRead))
	require.Equal(t, ScopeOrgBlockedAdmin, BlockedScope(ScopeOrgAdmin))
	require.Equal(t, ScopeProjectBlockedWrite, BlockedScope(ScopeProjectWrite))
}

func TestResolveScopeRegistry_derivesBlocklistTwins(t *testing.T) {
	t.Parallel()

	resolved := resolveScopeRegistry([]ScopeDef{
		{Scope: "thing:read", ResourceKind: "thing", Block: &BlockDef{AgentRuntimeSafeSince: 4}, AgentRuntimeSafeSince: 1},
		{Scope: "thing:write", ResourceKind: "thing", Implies: []Scope{"thing:read"}, Block: &BlockDef{}},
	})

	read := resolved["thing:read"]
	require.Equal(t, scopeVisibilityUserVisible, read.visibility)
	require.Equal(t, []Scope{"thing:write"}, read.satisfiedBy)
	require.Equal(t, Scope("thing:blocked_read"), read.exclusion)
	require.Equal(t, 1, read.agentRuntimeSafeSince)

	blockedRead := resolved["thing:blocked_read"]
	require.Equal(t, scopeVisibilityInternal, blockedRead.visibility)
	require.Equal(t, "thing", blockedRead.resourceKind)
	require.Empty(t, blockedRead.satisfiedBy)
	require.Empty(t, blockedRead.exclusion)
	require.Equal(t, 4, blockedRead.agentRuntimeSafeSince)

	// write implies read, so blocking read also blocks write.
	require.Equal(t, []Scope{"thing:blocked_read"}, resolved["thing:blocked_write"].satisfiedBy)
}

func TestResolveScopeRegistry_withoutBlockHasNoTwin(t *testing.T) {
	t.Parallel()

	resolved := resolveScopeRegistry([]ScopeDef{{Scope: "thing:use", ResourceKind: "thing"}})
	require.Empty(t, resolved["thing:use"].exclusion)
	require.NotContains(t, resolved, Scope("thing:blocked_use"))
}

func TestResolveScopeRegistry_rejectsDuplicates(t *testing.T) {
	t.Parallel()

	require.Panics(t, func() {
		resolveScopeRegistry([]ScopeDef{{Scope: "thing:read"}, {Scope: "thing:read"}})
	})
	// A declared scope may not collide with another scope's derived twin.
	require.Panics(t, func() {
		resolveScopeRegistry([]ScopeDef{{Scope: "thing:read", Block: &BlockDef{}}, {Scope: "thing:blocked_read"}})
	})
}

func TestResolveScopeRegistry_rejectsImplyingUnregisteredScope(t *testing.T) {
	t.Parallel()

	require.Panics(t, func() {
		resolveScopeRegistry([]ScopeDef{{Scope: "thing:write", Implies: []Scope{"thing:read"}}})
	})
}

func TestWithRegistry_rejectsScopeStillInLegacyTable(t *testing.T) {
	t.Parallel()

	require.Panics(t, func() {
		//exhaustive:ignore
		legacy := map[Scope]Scope{ScopeOrgRead: ""}
		withRegistry("legacy", legacy, func(e *registeredScope) Scope { return e.exclusion })
	})
	require.Panics(t, func() {
		withRegistryRole(SystemRoleAdmin, []Scope{ScopeOrgAdmin})
	})
}

func TestAgentRuntimeSafeSince(t *testing.T) {
	t.Parallel()

	version, ok := AgentRuntimeSafeSince(ScopeOrgAdmin)
	require.True(t, ok)
	require.Zero(t, version, "org:admin is not agent-runtime-safe")

	_, ok = AgentRuntimeSafeSince(ScopeProjectRead)
	require.False(t, ok, "project:read is not in the registry yet")
}

// TestEveryScopeConstantIsInEveryTable replaces the exhaustive linter's map
// literal check, which cannot see scopes the registry merges in: every Scope
// constant must resolve in every table after the merge.
func TestEveryScopeConstantIsInEveryTable(t *testing.T) {
	t.Parallel()

	file, err := parser.ParseFile(token.NewFileSet(), "scopes.go", nil, 0)
	require.NoError(t, err)

	var constants []Scope
	for _, decl := range file.Decls {
		gen, ok := decl.(*ast.GenDecl)
		if !ok || gen.Tok != token.CONST {
			continue
		}
		for _, spec := range gen.Specs {
			value, ok := spec.(*ast.ValueSpec)
			if !ok || value.Type == nil || len(value.Values) != 1 {
				continue
			}
			if ident, ok := value.Type.(*ast.Ident); !ok || ident.Name != "Scope" {
				continue
			}
			lit, ok := value.Values[0].(*ast.BasicLit)
			require.True(t, ok, "scope constant %s must be a string literal", value.Names[0].Name)
			name, err := strconv.Unquote(lit.Value)
			require.NoError(t, err)
			constants = append(constants, Scope(name))
		}
	}
	require.NotEmpty(t, constants)

	for _, scope := range constants {
		require.Contains(t, scopeVisibilityByScope, scope)
		require.Contains(t, scopeExpansions, scope)
		require.Contains(t, scopeExclusions, scope)
		require.Contains(t, scopeGrantSurfaces, scope)
	}
}
