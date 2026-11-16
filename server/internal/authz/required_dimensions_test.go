package authz

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/workos"
)

func TestValidateInput_MCPChecksRequireProject(t *testing.T) {
	t.Parallel()

	for _, scope := range []Scope{ScopeMCPRead, ScopeMCPWrite, ScopeMCPConnect, ScopeMCPBlockedRead, ScopeMCPBlockedWrite, ScopeMCPBlockedConnect} {
		t.Run(string(scope), func(t *testing.T) {
			t.Parallel()

			for name, dimensions := range map[string]map[string]string{
				"no dimensions":     nil,
				"other dimension":   {SelectorKeyTool: "search"},
				"empty project":     {SelectorKeyProjectID: ""},
				"wildcard project":  {SelectorKeyProjectID: WildcardResource},
				"tool, no project":  {SelectorKeyTool: "search", SelectorKeyDisposition: DispositionReadOnly},
				"explicit mcp kind": nil,
			} {
				check := Check{Scope: scope, ResourceKind: "", ResourceID: "server-1", Dimensions: dimensions, selectorMatch: selectorMatchNormal, representedUser: "", probe: false, anyOf: nil, operation: operationDefault}
				if name == "explicit mcp kind" {
					check.ResourceKind = ResourceKindMCP
				}
				require.ErrorIs(t, validateInput(check), ErrInvalidCheck, name)
			}
			require.NoError(t, validateInput(mcpCheck(scope, "server-1", "project-1")))
		})
	}

	require.ErrorIs(t, validateInput(mcpToolCallCheck("server-1", mcpToolCallDimensions{Tool: "search", Disposition: "", ProjectID: ""})), ErrInvalidCheck)
	require.NoError(t, validateInput(MCPServer(MCPServerIDFrom("server-1"), ProjectIDFrom("project-1")).ToolCall("search", "")))
	// Kinds with no required dimension are unaffected.
	require.NoError(t, validateInput(Check{Scope: ScopeProjectRead, ResourceKind: "", ResourceID: "project-1", Dimensions: nil, selectorMatch: selectorMatchNormal, representedUser: "", probe: false, anyOf: nil, operation: operationDefault}))
}

func TestRequiredCheckDimensions(t *testing.T) {
	t.Parallel()

	require.Equal(t, []string{SelectorKeyProjectID}, RequiredCheckDimensions(ResourceKindMCP))
	require.Empty(t, RequiredCheckDimensions(ResourceKindProject))

	// Callers get a copy, never the table itself.
	got := RequiredCheckDimensions(ResourceKindMCP)
	got[0] = "changed"
	require.Equal(t, []string{SelectorKeyProjectID}, RequiredCheckDimensions(ResourceKindMCP))
}

func TestGrantsAuthorize_ProjectBoundedMCPGrantOtherProject(t *testing.T) {
	t.Parallel()

	grants := []Grant{NewGrantWithSelector(ScopeMCPWrite, Selector{
		SelectorKeyResourceKind: ResourceKindMCP,
		SelectorKeyResourceID:   WildcardResource,
		SelectorKeyProjectID:    "project-p",
	})}

	for name, tc := range map[string]struct {
		check   Check
		allowed bool
	}{
		"project-level, same project":  {MCPServers(ProjectIDFrom("project-p")).Write(), true},
		"server, same project":         {MCPServer(MCPServerIDFrom("server-1"), ProjectIDFrom("project-p")).Write(), true},
		"read implied, same project":   {MCPServer(MCPServerIDFrom("server-1"), ProjectIDFrom("project-p")).Read(), true},
		"project-level, other project": {MCPServers(ProjectIDFrom("project-q")).Write(), false},
		"server, other project":        {MCPServer(MCPServerIDFrom("server-1"), ProjectIDFrom("project-q")).Write(), false},
		"read implied, other project":  {MCPServer(MCPServerIDFrom("server-1"), ProjectIDFrom("project-q")).Read(), false},
	} {
		allowed, err := GrantsAuthorize(grants, tc.check)
		require.NoError(t, err, name)
		require.Equal(t, tc.allowed, allowed, name)
	}

	// Omitting the project used to skip the grant's bound; it now fails closed.
	allowed, err := GrantsAuthorize(grants, Check{Scope: ScopeMCPWrite, ResourceKind: "", ResourceID: "project-q", Dimensions: nil, selectorMatch: selectorMatchNormal, representedUser: "", probe: false, anyOf: nil, operation: operationDefault})
	require.ErrorIs(t, err, ErrInvalidCheck)
	require.False(t, allowed)
	require.False(t, GrantsSatisfy(grants, Check{Scope: ScopeMCPWrite, ResourceKind: "", ResourceID: "project-q", Dimensions: nil, selectorMatch: selectorMatchNormal, representedUser: "", probe: false, anyOf: nil, operation: operationDefault}))
}

func TestEngineRequire_ProjectBoundedMCPGrantOtherProject(t *testing.T) {
	t.Parallel()

	engine := NewEngine(testenv.NewLogger(t), nil, staticChallengeLogging(false), workos.NewStubClient())
	bounded := GrantsToContext(enterpriseSessionCtx(t), []Grant{NewGrantWithSelector(ScopeMCPWrite, Selector{
		SelectorKeyResourceKind: ResourceKindMCP,
		SelectorKeyResourceID:   WildcardResource,
		SelectorKeyProjectID:    "project-p",
	})})

	require.NoError(t, engine.Require(bounded, MCPServers(ProjectIDFrom("project-p")).Write()))

	var oopsErr *oops.ShareableError
	require.ErrorAs(t, engine.Require(bounded, MCPServers(ProjectIDFrom("project-q")).Write()), &oopsErr)
	require.Equal(t, oops.CodeForbidden, oopsErr.Code)

	require.ErrorAs(t, engine.Require(bounded, Check{Scope: ScopeMCPWrite, ResourceKind: "", ResourceID: "project-q", Dimensions: nil, selectorMatch: selectorMatchNormal, representedUser: "", probe: false, anyOf: nil, operation: operationDefault}), &oopsErr)
	require.Equal(t, oops.CodeUnexpected, oopsErr.Code, "a check without a project is invalid")

	allowed, err := engine.Filter(bounded, []Check{
		MCPServer(MCPServerIDFrom("server-1"), ProjectIDFrom("project-p")).Write(),
		MCPServer(MCPServerIDFrom("server-2"), ProjectIDFrom("project-q")).Write(),
	})
	require.NoError(t, err)
	require.Equal(t, []string{"server-1"}, allowed)

	matched, err := engine.FindMatched(bounded, []Check{
		MCPServer(MCPServerIDFrom("server-1"), ProjectIDFrom("project-p")).Write(),
		MCPServer(MCPServerIDFrom("server-2"), ProjectIDFrom("project-q")).Write(),
	})
	require.NoError(t, err)
	require.Equal(t, []bool{true, false}, matched)

	// A wildcard grant still reaches every project.
	unbounded := GrantsToContext(enterpriseSessionCtx(t), []Grant{NewGrant(ScopeMCPWrite, WildcardResource)})
	require.NoError(t, engine.Require(unbounded, MCPServers(ProjectIDFrom("project-q")).Write()))
}
