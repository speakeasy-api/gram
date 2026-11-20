package authz

// Checks that are malformed rather than denied: a missing or wildcard
// resource id, an MCP check without a project, a tool or disposition on
// anything but a tool call. Where RBAC is enforced they fail with
// ErrInvalidCheck (a 500, not a 403); where it is skipped they are logged.

import (
	"bytes"
	"log/slog"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/workos"
)

func TestInvalidCheckIdentityIsRefusedEvenForRoot(t *testing.T) {
	t.Parallel()

	root := []Grant{NewGrant(ScopeRoot, WildcardResource)}
	cases := []struct {
		name  string
		check Check
	}{
		{
			name:  "unknown scope",
			check: Check{Scope: Scope("project:reed"), ResourceID: "project-one"},
		},
		{
			name:  "resource kind that is not the scope's kind",
			check: Check{Scope: ScopeProjectRead, ResourceKind: ResourceKindOrg, ResourceID: "project-one"},
		},
		{
			name:  "resource_id passed as a dimension",
			check: Check{Scope: ScopeEnvironmentRead, ResourceID: "env-one", Dimensions: map[string]string{SelectorKeyProjectID: "project-one", SelectorKeyResourceID: "env-two"}},
		},
		{
			name:  "resource_kind passed as a dimension",
			check: Check{Scope: ScopeEnvironmentRead, ResourceID: "env-one", Dimensions: map[string]string{SelectorKeyProjectID: "project-one", SelectorKeyResourceKind: ResourceKindProject}},
		},
		{
			name:  "dimension the scope does not declare",
			check: Check{Scope: ScopeProjectRead, ResourceID: "project-one", Dimensions: map[string]string{SelectorKeyTool: "search"}},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			allowed, err := GrantsAuthorize(root, tc.check)
			require.ErrorIs(t, err, ErrInvalidCheck)
			require.False(t, allowed)
		})
	}
}

func TestDeclaredDimensionsStayValid(t *testing.T) {
	t.Parallel()

	root := []Grant{NewGrant(ScopeRoot, WildcardResource)}
	checks := []Check{
		{Scope: ScopeProjectRead, ResourceKind: ResourceKindProject, ResourceID: "project-one"},
		Environment(EnvironmentIDFrom("env-one"), ProjectIDFrom("project-one")).Read(),
		MCPServer(MCPServerIDFrom("server-one"), ProjectIDFrom("project-one")).ToolCall("search", DispositionReadOnly),
		Org(OrgIDFrom("org-one")).HooksIngest(ProjectIDFrom("project-one")),
	}
	for _, check := range checks {
		allowed, err := GrantsAuthorize(root, check)
		require.NoError(t, err)
		require.True(t, allowed)
	}
}

// A caller whose enforcement is skipped still gets through with an invalid
// check for now, but the check is reported so it can be fixed before such
// checks are refused for every caller.
func TestInvalidCheckOnUnenforcedPathIsLoggedNotRefused(t *testing.T) {
	t.Parallel()

	var logs bytes.Buffer
	engine := NewEngine(slog.New(slog.NewTextHandler(&logs, nil)), nil, staticChallengeLogging(false), workos.NewStubClient())
	ctx := contextvalues.SetAuthContext(t.Context(), &contextvalues.AuthContext{
		ActiveOrganizationID: "org-one",
		UserID:               "user-one",
		SessionID:            nil,
	})

	enforce, err := engine.ShouldEnforce(ctx)
	require.NoError(t, err)
	require.False(t, enforce)

	require.NoError(t, engine.Require(ctx, Check{Scope: Scope("project:reed"), ResourceID: "project-one"}))
	require.Contains(t, logs.String(), "invalid authz check on a path that skips enforcement")

	logs.Reset()
	require.NoError(t, engine.Require(ctx, Check{Scope: ScopeProjectRead, ResourceID: "project-one"}))
	require.Empty(t, logs.String())
}

// A check that names a tool or a disposition has to be built as a tool call.
// Anything else naming one is refused, so it is never read as a connection.
func TestToolAndDispositionBelongToToolCalls(t *testing.T) {
	t.Parallel()

	root := []Grant{NewGrant(ScopeRoot, WildcardResource)}
	cases := []struct {
		name  string
		check Check
	}{
		{
			name:  "connect check with a tool added by hand",
			check: Check{Scope: ScopeMCPConnect, ResourceID: "server-one", Dimensions: map[string]string{SelectorKeyProjectID: "project-one", SelectorKeyTool: "search"}},
		},
		{
			name:  "connect check with a disposition added by hand",
			check: Check{Scope: ScopeMCPConnect, ResourceID: "server-one", Dimensions: map[string]string{SelectorKeyProjectID: "project-one", SelectorKeyDisposition: DispositionReadOnly}},
		},
		{
			name:  "server read with a tool",
			check: Check{Scope: ScopeMCPRead, ResourceID: "server-one", Dimensions: map[string]string{SelectorKeyProjectID: "project-one", SelectorKeyTool: "search"}},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			allowed, err := GrantsAuthorize(root, tc.check)
			require.ErrorIs(t, err, ErrInvalidCheck)
			require.False(t, allowed)
		})
	}
}

func TestToolCallConstructorsBuildToolCalls(t *testing.T) {
	t.Parallel()

	root := []Grant{NewGrant(ScopeRoot, WildcardResource)}
	server := uuid.New()
	project := uuid.New()
	checks := []Check{
		MCPServer(MCPServerIDOf(server), ProjectIDOf(project)).ToolCall("search", DispositionReadOnly),
		MCPServer(MCPServerIDOf(server), ProjectIDOf(project)).ToolCall("search", ""),
		MCPServer(MCPServerIDOf(server), ProjectIDOf(project)).ToolCall("search", DispositionReadOnly),
	}
	for _, check := range checks {
		require.True(t, check.isToolCall())
		allowed, err := GrantsAuthorize(root, check)
		require.NoError(t, err)
		require.True(t, allowed)
	}

	connect := MCPServer(MCPServerIDOf(server), ProjectIDOf(project)).Connect()
	require.False(t, connect.isToolCall())
	allowed, err := GrantsAuthorize(root, connect)
	require.NoError(t, err)
	require.True(t, allowed)
}

// A grant limited to read-only tools admits the connection and read-only
// calls, and nothing built as a tool call slips through as a connection.
func TestDispositionLimitedGrantSeparatesConnectFromCalls(t *testing.T) {
	t.Parallel()

	server := uuid.New()
	project := uuid.New()
	selector := NewSelector(ScopeMCPConnect, server.String())
	selector[SelectorKeyDisposition] = DispositionReadOnly
	grants := []Grant{NewGrantWithSelector(ScopeMCPConnect, selector)}
	ref := MCPServer(MCPServerIDOf(server), ProjectIDOf(project))

	allowed, err := GrantsAuthorize(grants, ref.Connect())
	require.NoError(t, err)
	require.True(t, allowed)

	allowed, err = GrantsAuthorize(grants, ref.ToolCall("search", DispositionReadOnly))
	require.NoError(t, err)
	require.True(t, allowed)

	allowed, err = GrantsAuthorize(grants, ref.ToolCall("delete_all", DispositionDestructive))
	require.NoError(t, err)
	require.False(t, allowed)

	// A tool with no annotations carries no disposition, and a grant limited
	// to a disposition never covers a call that does not carry one.
	allowed, err = GrantsAuthorize(grants, ref.ToolCall("mystery", ""))
	require.NoError(t, err)
	require.False(t, allowed)
}

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
				check := Check{Scope: scope, ResourceKind: "", ResourceID: "server-1", Dimensions: dimensions, exact: false, representedUser: "", probe: false, anyOf: nil, operation: operationDefault}
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
	require.NoError(t, validateInput(Check{Scope: ScopeProjectRead, ResourceKind: "", ResourceID: "project-1", Dimensions: nil, exact: false, representedUser: "", probe: false, anyOf: nil, operation: operationDefault}))
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

func TestGrantsAuthorizeRejectsEmptyResourceID(t *testing.T) {
	t.Parallel()

	allowed, err := GrantsAuthorize([]Grant{NewGrant(ScopeRoot, WildcardResource)}, Check{
		Scope: ScopeMCPConnect, ResourceKind: "", ResourceID: "", Dimensions: nil,
	})
	require.ErrorIs(t, err, ErrInvalidCheck)
	require.False(t, allowed)
}

func TestGrantsAuthorizeRejectsWildcardResourceID(t *testing.T) {
	t.Parallel()

	allowed, err := GrantsAuthorize([]Grant{NewGrant(ScopeRoot, WildcardResource)}, Check{
		Scope: ScopeMCPConnect, ResourceKind: "", ResourceID: WildcardResource, Dimensions: nil,
	})
	require.ErrorIs(t, err, ErrInvalidCheck)
	require.False(t, allowed)
}
