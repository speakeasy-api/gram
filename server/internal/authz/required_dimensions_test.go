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
				check := Check{Scope: scope, ResourceKind: "", ResourceID: "server-1", Dimensions: dimensions, selectorMatch: selectorMatchNormal, representedUser: ""}
				if name == "explicit mcp kind" {
					check.ResourceKind = ResourceKindMCP
				}
				require.ErrorIs(t, validateInput(check), ErrInvalidCheck, name)
			}
			require.NoError(t, validateInput(MCPCheck(scope, "server-1", "project-1")))
		})
	}

	require.ErrorIs(t, validateInput(MCPToolCallCheck("server-1", MCPToolCallDimensions{Tool: "search", Disposition: "", ProjectID: ""})), ErrInvalidCheck)
	require.NoError(t, validateInput(MCPToolCallCheck("server-1", MCPToolCallDimensions{Tool: "search", Disposition: "", ProjectID: "project-1"})))
	// Kinds with no required dimension are unaffected.
	require.NoError(t, validateInput(Check{Scope: ScopeProjectRead, ResourceKind: "", ResourceID: "project-1", Dimensions: nil, selectorMatch: selectorMatchNormal, representedUser: ""}))
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
		"project-level, same project":  {MCPCheck(ScopeMCPWrite, "project-p", "project-p"), true},
		"server, same project":         {MCPCheck(ScopeMCPWrite, "server-1", "project-p"), true},
		"read implied, same project":   {MCPCheck(ScopeMCPRead, "server-1", "project-p"), true},
		"project-level, other project": {MCPCheck(ScopeMCPWrite, "project-q", "project-q"), false},
		"server, other project":        {MCPCheck(ScopeMCPWrite, "server-1", "project-q"), false},
		"read implied, other project":  {MCPCheck(ScopeMCPRead, "server-1", "project-q"), false},
	} {
		allowed, err := GrantsAuthorize(grants, tc.check)
		require.NoError(t, err, name)
		require.Equal(t, tc.allowed, allowed, name)
	}

	// Omitting the project used to skip the grant's bound; it now fails closed.
	allowed, err := GrantsAuthorize(grants, Check{Scope: ScopeMCPWrite, ResourceKind: "", ResourceID: "project-q", Dimensions: nil, selectorMatch: selectorMatchNormal, representedUser: ""})
	require.ErrorIs(t, err, ErrInvalidCheck)
	require.False(t, allowed)
	require.False(t, GrantsSatisfy(grants, Check{Scope: ScopeMCPWrite, ResourceKind: "", ResourceID: "project-q", Dimensions: nil, selectorMatch: selectorMatchNormal, representedUser: ""}))
}

func TestEngineRequire_ProjectBoundedMCPGrantOtherProject(t *testing.T) {
	t.Parallel()

	engine := NewEngine(testenv.NewLogger(t), nil, staticChallengeLogging(false), workos.NewStubClient())
	bounded := GrantsToContext(enterpriseSessionCtx(t), []Grant{NewGrantWithSelector(ScopeMCPWrite, Selector{
		SelectorKeyResourceKind: ResourceKindMCP,
		SelectorKeyResourceID:   WildcardResource,
		SelectorKeyProjectID:    "project-p",
	})})

	require.NoError(t, engine.Require(bounded, MCPCheck(ScopeMCPWrite, "project-p", "project-p")))

	var oopsErr *oops.ShareableError
	require.ErrorAs(t, engine.Require(bounded, MCPCheck(ScopeMCPWrite, "project-q", "project-q")), &oopsErr)
	require.Equal(t, oops.CodeForbidden, oopsErr.Code)

	require.ErrorAs(t, engine.Require(bounded, Check{Scope: ScopeMCPWrite, ResourceKind: "", ResourceID: "project-q", Dimensions: nil, selectorMatch: selectorMatchNormal, representedUser: ""}), &oopsErr)
	require.Equal(t, oops.CodeUnexpected, oopsErr.Code, "a check without a project is invalid")

	allowed, err := engine.Filter(bounded, []Check{
		MCPCheck(ScopeMCPWrite, "server-1", "project-p"),
		MCPCheck(ScopeMCPWrite, "server-2", "project-q"),
	})
	require.NoError(t, err)
	require.Equal(t, []string{"server-1"}, allowed)

	matched, err := engine.FindMatched(bounded, []Check{
		MCPCheck(ScopeMCPWrite, "server-1", "project-p"),
		MCPCheck(ScopeMCPWrite, "server-2", "project-q"),
	})
	require.NoError(t, err)
	require.Equal(t, []bool{true, false}, matched)

	// A wildcard grant still reaches every project.
	unbounded := GrantsToContext(enterpriseSessionCtx(t), []Grant{NewGrant(ScopeMCPWrite, WildcardResource)})
	require.NoError(t, engine.Require(unbounded, MCPCheck(ScopeMCPWrite, "project-q", "project-q")))
}
