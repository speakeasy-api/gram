package authz

import (
	"bytes"
	"log/slog"
	"testing"

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
		EnvironmentCheck(ScopeEnvironmentRead, "env-one", "project-one"),
		MCPToolCallCheck("server-one", MCPToolCallDimensions{Tool: "search", Disposition: DispositionReadOnly, ProjectID: "project-one"}),
		HooksIngestCheck("org-one", "project-one"),
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
