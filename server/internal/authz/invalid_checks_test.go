package authz

// Checks that are malformed rather than denied: a missing or wildcard
// resource id, an MCP check without a project, a tool or disposition on
// anything but a tool call. Where RBAC is enforced they fail with
// ErrInvalidCheck (a 500, not a 403); where it is skipped they are logged.

import (
	"bytes"
	"context"
	"log/slog"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/testenv"
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

// In production a caller whose enforcement is skipped, such as a legacy API
// key, still gets through with an invalid check, and the check is logged so
// the handler can be fixed.
func TestInvalidCheckOnUnenforcedPathIsLoggedInProduction(t *testing.T) {
	t.Parallel()

	var logs bytes.Buffer
	engine := NewEngine(slog.New(slog.NewTextHandler(&logs, nil)), nil, staticChallengeLogging(false), workos.NewStubClient())
	engine.refuseInvalidChecks = false
	ctx := unenforcedContext(t)

	require.NoError(t, engine.Require(ctx, Check{Scope: Scope("project:reed"), ResourceID: "project-one"}))
	require.Contains(t, logs.String(), "invalid authz check on a path that skips enforcement")

	logs.Reset()
	require.NoError(t, engine.Require(ctx, Check{Scope: ScopeProjectRead, ResourceID: "project-one"}))
	require.Empty(t, logs.String())
}

// In tests and local dev the same invalid check fails, through every entry
// point, so the handler bug is caught before it ships.
func TestInvalidCheckOnUnenforcedPathFailsInTestsAndDev(t *testing.T) {
	t.Parallel()

	for _, devMode := range []bool{false, true} {
		engine := NewEngine(testenv.NewLogger(t), nil, staticChallengeLogging(false), workos.NewStubClient(), EngineOpts{DevMode: devMode})
		ctx := unenforcedContext(t)
		typo := Check{Scope: Scope("project:reed"), ResourceID: "project-one"}

		require.Error(t, engine.Require(ctx, typo))
		require.Error(t, engine.RequireAny(ctx, typo))
		_, err := engine.Evaluate(ctx, typo)
		require.Error(t, err)
		_, err = engine.Can(ctx, AnyOf(typo, typo))
		require.Error(t, err)
		_, err = engine.Filter(ctx, []Check{typo})
		require.Error(t, err)
		_, err = engine.FindMatched(ctx, []Check{typo})
		require.Error(t, err)

		require.NoError(t, engine.Require(ctx, Check{Scope: ScopeProjectRead, ResourceID: "project-one"}))
	}
}

// unenforcedContext is a caller without a dashboard session, which the
// engine does not enforce.
func unenforcedContext(t *testing.T) context.Context {
	t.Helper()

	ctx := contextvalues.SetAuthContext(t.Context(), &contextvalues.AuthContext{
		ActiveOrganizationID: "org-one",
		UserID:               "user-one",
		SessionID:            nil,
	})
	engine := NewEngine(testenv.NewLogger(t), nil, staticChallengeLogging(false), workos.NewStubClient())
	enforce, err := engine.ShouldEnforce(ctx)
	require.NoError(t, err)
	require.False(t, enforce)
	return ctx
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
		mcpToolCallCheck(server.String(), mcpToolCallDimensions{Tool: "search", Disposition: DispositionReadOnly, ProjectID: project.String()}),
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

// A block on destructive tools has to take away every call that might be
// destructive. A call with no disposition might be, so the block applies; an
// unannotated tool is not, so the allow stands; and a disposition the registry
// does not know is an invalid check rather than a call the block cannot match.
func TestToolCallDispositionAgainstDispositionBlock(t *testing.T) {
	t.Parallel()

	server := uuid.New()
	project := uuid.New()
	blocked := NewSelector(ScopeMCPBlockedConnect, server.String())
	blocked[SelectorKeyDisposition] = DispositionDestructive
	grants := []Grant{
		NewGrant(ScopeMCPConnect, server.String()),
		NewGrantWithSelector(ScopeMCPBlockedConnect, blocked),
	}
	ref := MCPServer(MCPServerIDOf(server), ProjectIDOf(project))

	cases := []struct {
		name        string
		disposition string
		allowed     bool
		invalid     bool
	}{
		{name: "absent", disposition: "", allowed: false, invalid: false},
		{name: "unannotated", disposition: DispositionUnannotated, allowed: true, invalid: false},
		{name: "unrecognized", disposition: "dangerous", allowed: false, invalid: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			allowed, err := GrantsAuthorize(grants, ref.ToolCall("delete_all", tc.disposition))
			if tc.invalid {
				require.ErrorIs(t, err, ErrInvalidCheck)
			} else {
				require.NoError(t, err)
			}
			require.Equal(t, tc.allowed, allowed)
		})
	}
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

func TestAnyOf_OnlyAsTheSingleCheckOfRequireOrCan(t *testing.T) {
	t.Parallel()
	engine := NewEngine(testenv.NewLogger(t), nil, staticChallengeLogging(false), workos.NewStubClient())
	anyOf := AnyOf(Project(ProjectIDFrom("project-one")).Write(), Project(ProjectIDFrom("project-one")).Read())

	// A misuse is refused whether or not RBAC is enforced for the caller.
	for name, ctx := range shapeTestContexts(t, []Grant{NewGrant(ScopeRoot, WildcardResource)}) {
		requireInvalid := func(err error) {
			t.Helper()
			var shareable *oops.ShareableError
			require.ErrorAs(t, err, &shareable, name)
			require.NotEqual(t, oops.CodeForbidden, shareable.Code, "%s: a misused AnyOf is a bad check, not a denial", name)
		}
		requireInvalid(engine.Require(ctx, anyOf, Project(ProjectIDFrom("project-one")).Read()))
		requireInvalid(engine.Require(ctx, AnyOf(anyOf)))
		_, err := engine.Evaluate(ctx, anyOf)
		requireInvalid(err)
		_, err = engine.Filter(ctx, []Check{anyOf})
		requireInvalid(err)
		_, err = engine.FindMatched(ctx, []Check{anyOf})
		requireInvalid(err)
		_, err = engine.Can(ctx, anyOf, Project(ProjectIDFrom("project-one")).Read())
		requireInvalid(err)
		_, err = engine.Can(ctx, AnyOf(anyOf))
		requireInvalid(err)
		requireInvalid(engine.Require(ctx, AnyOf()))
		_, err = engine.Can(ctx, AnyOf())
		requireInvalid(err)
	}
}

// A probe is a discovery question: Can and Filter answer it, Require refuses
// it, alone or inside AnyOf, whether or not RBAC is enforced.
func TestProbe_RefusedByRequire(t *testing.T) {
	t.Parallel()
	engine := NewEngine(testenv.NewLogger(t), nil, staticChallengeLogging(false), workos.NewStubClient())
	probe := MCPServer(MCPServerIDFrom("server-1"), ProjectIDFrom("project-1")).Connect().Probe()

	for name, ctx := range shapeTestContexts(t, []Grant{NewGrant(ScopeMCPConnect, "server-1")}) {
		can, err := engine.Can(ctx, probe)
		require.NoError(t, err, name)
		require.True(t, can, name)
		ids, err := engine.Filter(ctx, []Check{probe})
		require.NoError(t, err, name)
		require.Equal(t, []string{"server-1"}, ids, name)

		for call, err := range map[string]error{
			"require":               engine.Require(ctx, probe),
			"require any":           engine.RequireAny(ctx, probe),
			"require any unblocked": engine.RequireAnyUnblocked(ctx, probe),
			"anyof alternative":     engine.Require(ctx, AnyOf(probe)),
			"probed anyof":          engine.Require(ctx, AnyOf(MCPServer(MCPServerIDFrom("server-1"), ProjectIDFrom("project-1")).Connect()).Probe()),
		} {
			require.ErrorIs(t, err, ErrInvalidCheck, "%s: %s", name, call)
		}
	}
}

// shapeTestContexts returns a caller RBAC is enforced for, holding grants,
// and one it is skipped for (no session).
func shapeTestContexts(t *testing.T, grants []Grant) map[string]context.Context {
	t.Helper()
	return map[string]context.Context{
		"enforced": GrantsToContext(enterpriseSessionCtx(t), grants),
		"skipped":  contextvalues.SetAuthContext(t.Context(), &contextvalues.AuthContext{ActiveOrganizationID: "org_123", UserID: sessionUserID, AccountType: "enterprise"}),
	}
}
