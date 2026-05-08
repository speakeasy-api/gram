package risk_test

import (
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/risk"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

func TestCreateRiskPolicy_Success(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestRiskService(t)

	authCtx, _ := contextvalues.GetAuthContext(ctx)
	ctx = withExactAccessGrants(t, ctx, ti.conn, authz.Grant{Scope: authz.ScopeOrgAdmin, Selector: authz.NewSelector(authz.ScopeOrgAdmin, authCtx.ActiveOrganizationID)})

	enabled := true
	result, err := ti.service.CreateRiskPolicy(ctx, &gen.CreateRiskPolicyPayload{
		Name:    new("Test Policy"),
		Sources: []string{"gitleaks"},
		Enabled: &enabled,
	})
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Equal(t, "Test Policy", result.Name)
	require.Equal(t, []string{"gitleaks"}, result.Sources)
	require.True(t, result.Enabled)
	require.Equal(t, int64(1), result.Version)
	require.NotEqual(t, uuid.Nil.String(), result.ID)

	// Should have signaled the drain workflow.
	require.Len(t, ti.signaler.calls, 1)
}

func TestCreateRiskPolicy_DefaultSources(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestRiskService(t)

	authCtx, _ := contextvalues.GetAuthContext(ctx)
	ctx = withExactAccessGrants(t, ctx, ti.conn, authz.Grant{Scope: authz.ScopeOrgAdmin, Selector: authz.NewSelector(authz.ScopeOrgAdmin, authCtx.ActiveOrganizationID)})

	result, err := ti.service.CreateRiskPolicy(ctx, &gen.CreateRiskPolicyPayload{
		Name: new("No Sources"),
	})
	require.NoError(t, err)
	require.Equal(t, []string{"gitleaks"}, result.Sources)
	require.True(t, result.Enabled) // default enabled
}

func TestCreateRiskPolicy_DestructiveToolSource(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestRiskService(t)

	authCtx, _ := contextvalues.GetAuthContext(ctx)
	ctx = withExactAccessGrants(t, ctx, ti.conn, authz.Grant{Scope: authz.ScopeOrgAdmin, Selector: authz.NewSelector(authz.ScopeOrgAdmin, authCtx.ActiveOrganizationID)})

	result, err := ti.service.CreateRiskPolicy(ctx, &gen.CreateRiskPolicyPayload{
		Sources: []string{"destructive_tool"},
		Action:  "flag",
	})
	require.NoError(t, err)
	require.Equal(t, []string{"destructive_tool"}, result.Sources)
	require.Equal(t, "Destructive Tool Scanner", result.Name)
}

func TestCreateRiskPolicy_DestructiveToolRejectsBlock(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestRiskService(t)

	authCtx, _ := contextvalues.GetAuthContext(ctx)
	ctx = withExactAccessGrants(t, ctx, ti.conn, authz.Grant{Scope: authz.ScopeOrgAdmin, Selector: authz.NewSelector(authz.ScopeOrgAdmin, authCtx.ActiveOrganizationID)})

	_, err := ti.service.CreateRiskPolicy(ctx, &gen.CreateRiskPolicyPayload{
		Sources: []string{"destructive_tool"},
		Action:  "block",
	})
	require.Error(t, err)
}

func TestCreateRiskPolicy_CLIDestructiveSource(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestRiskService(t)

	authCtx, _ := contextvalues.GetAuthContext(ctx)
	ctx = withExactAccessGrants(t, ctx, ti.conn, authz.Grant{Scope: authz.ScopeOrgAdmin, Selector: authz.NewSelector(authz.ScopeOrgAdmin, authCtx.ActiveOrganizationID)})

	result, err := ti.service.CreateRiskPolicy(ctx, &gen.CreateRiskPolicyPayload{
		Sources: []string{"cli_destructive"},
		Action:  "flag",
	})
	require.NoError(t, err)
	require.Equal(t, []string{"cli_destructive"}, result.Sources)
	require.Equal(t, "Destructive CLI Command Scanner", result.Name)
}

func TestCreateRiskPolicy_CLIDestructiveRejectsBlock(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestRiskService(t)

	authCtx, _ := contextvalues.GetAuthContext(ctx)
	ctx = withExactAccessGrants(t, ctx, ti.conn, authz.Grant{Scope: authz.ScopeOrgAdmin, Selector: authz.NewSelector(authz.ScopeOrgAdmin, authCtx.ActiveOrganizationID)})

	_, err := ti.service.CreateRiskPolicy(ctx, &gen.CreateRiskPolicyPayload{
		Sources: []string{"cli_destructive"},
		Action:  "block",
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "cli_destructive")
}

func TestCreateRiskPolicy_EmptyName(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestRiskService(t)

	authCtx, _ := contextvalues.GetAuthContext(ctx)
	ctx = withExactAccessGrants(t, ctx, ti.conn, authz.Grant{Scope: authz.ScopeOrgAdmin, Selector: authz.NewSelector(authz.ScopeOrgAdmin, authCtx.ActiveOrganizationID)})

	_, err := ti.service.CreateRiskPolicy(ctx, &gen.CreateRiskPolicyPayload{
		Name: new(""),
	})
	require.Error(t, err)
}

func TestCreateRiskPolicy_NameTooLong(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestRiskService(t)

	authCtx, _ := contextvalues.GetAuthContext(ctx)
	ctx = withExactAccessGrants(t, ctx, ti.conn, authz.Grant{Scope: authz.ScopeOrgAdmin, Selector: authz.NewSelector(authz.ScopeOrgAdmin, authCtx.ActiveOrganizationID)})

	var longName strings.Builder
	for range 101 {
		longName.WriteString("a")
	}
	_, err := ti.service.CreateRiskPolicy(ctx, &gen.CreateRiskPolicyPayload{
		Name: new(longName.String()),
	})
	require.Error(t, err)
}

func TestCreateRiskPolicy_Unauthorized(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestRiskService(t)

	// Set up enterprise account with zero grants — RBAC should deny.
	ctx = withExactAccessGrants(t, ctx, ti.conn)

	_, err := ti.service.CreateRiskPolicy(ctx, &gen.CreateRiskPolicyPayload{
		Name: new("Should Fail"),
	})
	require.Error(t, err)
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeForbidden, oopsErr.Code)
}

func TestCreateRiskPolicy_DisabledDoesNotSignal(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestRiskService(t)

	authCtx, _ := contextvalues.GetAuthContext(ctx)
	ctx = withExactAccessGrants(t, ctx, ti.conn, authz.Grant{Scope: authz.ScopeOrgAdmin, Selector: authz.NewSelector(authz.ScopeOrgAdmin, authCtx.ActiveOrganizationID)})

	enabled := false
	result, err := ti.service.CreateRiskPolicy(ctx, &gen.CreateRiskPolicyPayload{
		Name:    new("Disabled Policy"),
		Enabled: &enabled,
	})
	require.NoError(t, err)
	require.False(t, result.Enabled)
	require.Empty(t, ti.signaler.calls) // should not signal when disabled
}
