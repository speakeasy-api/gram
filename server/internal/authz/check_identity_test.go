package authz

import (
	"bytes"
	"context"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/contextvalues"
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
