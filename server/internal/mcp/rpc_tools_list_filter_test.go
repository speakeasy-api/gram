package mcp

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/externalmcp"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/workos"
)

// rbacAlwaysEnabled mirrors authztest.RBACAlwaysEnabled. Inlined here so this
// internal test file does not depend on the test helper package.
func rbacAlwaysEnabled(context.Context, string) (bool, error) { return true, nil }

func enterpriseSessionCtx(orgID string) context.Context {
	sessionID := "sess-" + uuid.NewString()
	return contextvalues.SetAuthContext(context.Background(), &contextvalues.AuthContext{
		ActiveOrganizationID:  orgID,
		UserID:                "user-" + uuid.NewString(),
		ExternalUserID:        "",
		APIKeyID:              "",
		SessionID:             &sessionID,
		ProjectID:             nil,
		OrganizationSlug:      "",
		Email:                 nil,
		AccountType:           "enterprise",
		HasActiveSubscription: false,
		Whitelisted:           false,
		ProjectSlug:           nil,
		APIKeyScopes:          nil,
		IsAdmin:               false,
	})
}

func newFilterTestEngine(t *testing.T) *authz.Engine {
	t.Helper()
	return authz.NewEngine(testLogger(), nil, rbacAlwaysEnabled, workos.NewStubClient(), cache.NoopCache)
}

func makeToolEntry(name string, annotations *externalmcp.ToolAnnotations) *toolListEntry {
	return &toolListEntry{
		Name:        name,
		Description: "",
		InputSchema: nil,
		Annotations: annotations,
		Meta:        nil,
	}
}

func toolNamesOf(entries []*toolListEntry) []string {
	out := make([]string, len(entries))
	for i, e := range entries {
		out[i] = e.Name
	}
	return out
}

func boolPtr(v bool) *bool { return &v }

func annoReadOnly() *externalmcp.ToolAnnotations {
	return &externalmcp.ToolAnnotations{
		Title:           "",
		ReadOnlyHint:    boolPtr(true),
		DestructiveHint: nil,
		IdempotentHint:  nil,
		OpenWorldHint:   nil,
	}
}

func annoDestructive() *externalmcp.ToolAnnotations {
	return &externalmcp.ToolAnnotations{
		Title:           "",
		ReadOnlyHint:    nil,
		DestructiveHint: boolPtr(true),
		IdempotentHint:  nil,
		OpenWorldHint:   nil,
	}
}

// TestFilterToolsByAuthz_PublicMCPSkipsFiltering ensures the filter is a no-op
// when the toolset is public, even if RBAC is otherwise active.
func TestFilterToolsByAuthz_PublicMCPSkipsFiltering(t *testing.T) {
	t.Parallel()

	engine := newFilterTestEngine(t)
	ctx := authz.GrantsToContext(enterpriseSessionCtx("org_x"), nil)
	tools := []*toolListEntry{makeToolEntry("a", nil), makeToolEntry("b", nil)}

	got, err := filterToolsByAuthz(ctx, testLogger(), engine, "ts_pub", true, true, tools)
	require.NoError(t, err)
	require.Equal(t, tools, got)
}

// TestFilterToolsByAuthz_UnauthenticatedSkipsFiltering ensures unauthenticated
// callers bypass filtering — they're handled earlier by the connection guard.
func TestFilterToolsByAuthz_UnauthenticatedSkipsFiltering(t *testing.T) {
	t.Parallel()

	engine := newFilterTestEngine(t)
	ctx := enterpriseSessionCtx("org_x")
	tools := []*toolListEntry{makeToolEntry("a", nil)}

	got, err := filterToolsByAuthz(ctx, testLogger(), engine, "ts_priv", false, false, tools)
	require.NoError(t, err)
	require.Equal(t, tools, got)
}

// TestFilterToolsByAuthz_NilEngineSkipsFiltering ensures a nil engine is a
// no-op — defensive in case a future caller wires it through unset.
func TestFilterToolsByAuthz_NilEngineSkipsFiltering(t *testing.T) {
	t.Parallel()

	ctx := enterpriseSessionCtx("org_x")
	tools := []*toolListEntry{makeToolEntry("a", nil)}

	got, err := filterToolsByAuthz(ctx, testLogger(), nil, "ts_priv", false, true, tools)
	require.NoError(t, err)
	require.Equal(t, tools, got)
}

// TestFilterToolsByAuthz_FiltersToGrantedTools verifies the filter loop drops
// forbidden tools and keeps allowed ones based on per-tool grants.
func TestFilterToolsByAuthz_FiltersToGrantedTools(t *testing.T) {
	t.Parallel()

	toolsetID := uuid.NewString()
	engine := newFilterTestEngine(t)
	ctx := authz.GrantsToContext(enterpriseSessionCtx("org_filters"), []authz.Grant{
		{
			Scope: authz.ScopeMCPConnect,
			Selector: authz.Selector{
				"resource_kind": "mcp",
				"resource_id":   toolsetID,
				"tool":          "allowed_tool",
			},
		},
	})

	tools := []*toolListEntry{
		makeToolEntry("allowed_tool", nil),
		makeToolEntry("forbidden_tool", nil),
		makeToolEntry("another_forbidden", nil),
	}

	got, err := filterToolsByAuthz(ctx, testLogger(), engine, toolsetID, false, true, tools)
	require.NoError(t, err)
	require.Equal(t, []string{"allowed_tool"}, toolNamesOf(got))
}

// TestFilterToolsByAuthz_ServerLevelGrantReturnsAll verifies a grant scoped to
// just the toolset (no tool dimension) lets every tool through.
func TestFilterToolsByAuthz_ServerLevelGrantReturnsAll(t *testing.T) {
	t.Parallel()

	toolsetID := uuid.NewString()
	engine := newFilterTestEngine(t)
	ctx := authz.GrantsToContext(enterpriseSessionCtx("org_server"), []authz.Grant{
		{Scope: authz.ScopeMCPConnect, Selector: authz.NewSelector(authz.ScopeMCPConnect, toolsetID)},
	})

	tools := []*toolListEntry{
		makeToolEntry("tool_one", nil),
		makeToolEntry("tool_two", nil),
		makeToolEntry("tool_three", nil),
	}

	got, err := filterToolsByAuthz(ctx, testLogger(), engine, toolsetID, false, true, tools)
	require.NoError(t, err)
	require.Equal(t, []string{"tool_one", "tool_two", "tool_three"}, toolNamesOf(got))
}

// TestFilterToolsByAuthz_NoMatchingGrantsFiltersAll verifies a grant for a
// different toolset doesn't bleed through — every tool is dropped.
func TestFilterToolsByAuthz_NoMatchingGrantsFiltersAll(t *testing.T) {
	t.Parallel()

	engine := newFilterTestEngine(t)
	ctx := authz.GrantsToContext(enterpriseSessionCtx("org_no_match"), []authz.Grant{
		{Scope: authz.ScopeMCPConnect, Selector: authz.NewSelector(authz.ScopeMCPConnect, uuid.NewString())},
	})

	tools := []*toolListEntry{makeToolEntry("x", nil), makeToolEntry("y", nil)}

	got, err := filterToolsByAuthz(ctx, testLogger(), engine, uuid.NewString(), false, true, tools)
	require.NoError(t, err)
	require.Empty(t, got)
}

// TestFilterToolsByAuthz_DispositionDerivedFromAnnotations verifies the loop
// derives a disposition from each tool's annotations before checking the
// grant — a destructive tool is dropped under a read_only-scoped grant.
func TestFilterToolsByAuthz_DispositionDerivedFromAnnotations(t *testing.T) {
	t.Parallel()

	toolsetID := uuid.NewString()
	engine := newFilterTestEngine(t)
	ctx := authz.GrantsToContext(enterpriseSessionCtx("org_disposition"), []authz.Grant{
		{
			Scope: authz.ScopeMCPConnect,
			Selector: authz.Selector{
				"resource_kind": "mcp",
				"resource_id":   toolsetID,
				"disposition":   "read_only",
			},
		},
	})

	tools := []*toolListEntry{
		makeToolEntry("safe_tool", annoReadOnly()),
		makeToolEntry("dangerous_tool", annoDestructive()),
		makeToolEntry("no_annotations", nil),
	}

	got, err := filterToolsByAuthz(ctx, testLogger(), engine, toolsetID, false, true, tools)
	require.NoError(t, err)
	require.Equal(t, []string{"safe_tool"}, toolNamesOf(got))
}

// TestFilterToolsByAuthz_NonForbiddenErrorsPropagate verifies that an
// unexpected (non-forbidden) authz error is wrapped as CodeUnexpected and
// returned to the caller rather than silently swallowed.
func TestFilterToolsByAuthz_NonForbiddenErrorsPropagate(t *testing.T) {
	t.Parallel()

	engine := newFilterTestEngine(t)
	// No auth context at all — ShouldEnforce returns CodeUnauthorized,
	// which the filter wraps as CodeUnexpected and propagates.
	ctx := context.Background()
	tools := []*toolListEntry{makeToolEntry("any", nil)}

	got, err := filterToolsByAuthz(ctx, testLogger(), engine, uuid.NewString(), false, true, tools)
	require.Error(t, err)
	require.Nil(t, got)

	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeUnexpected, oopsErr.Code)
}

// TestFilterToolsByAuthz_RBACDisabledOrgReturnsAll verifies orgs without RBAC
// enabled keep every tool: ShouldEnforce returns false on a non-enterprise
// account so Require is a no-op.
func TestFilterToolsByAuthz_RBACDisabledOrgReturnsAll(t *testing.T) {
	t.Parallel()

	engine := newFilterTestEngine(t)
	// Non-enterprise auth context — ShouldEnforce returns false on AccountType.
	sessionID := "sess-noent"
	ctx := contextvalues.SetAuthContext(context.Background(), &contextvalues.AuthContext{
		ActiveOrganizationID:  "org_free",
		UserID:                "user_free",
		ExternalUserID:        "",
		APIKeyID:              "",
		SessionID:             &sessionID,
		ProjectID:             nil,
		OrganizationSlug:      "",
		Email:                 nil,
		AccountType:           "free",
		HasActiveSubscription: false,
		Whitelisted:           false,
		ProjectSlug:           nil,
		APIKeyScopes:          nil,
		IsAdmin:               false,
	})

	tools := []*toolListEntry{makeToolEntry("a", nil), makeToolEntry("b", nil)}

	got, err := filterToolsByAuthz(ctx, testLogger(), engine, uuid.NewString(), false, true, tools)
	require.NoError(t, err)
	require.Equal(t, tools, got)
}
