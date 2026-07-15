package risk_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/risk"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/risk/policybypass"
	riskrepo "github.com/speakeasy-api/gram/server/internal/risk/repo"
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

func TestCreateRiskPolicy_ShadowMCPAllowedURLsAreAtomic(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestRiskService(t)
	name := "Shadow MCP Block"
	created, err := ti.service.CreateRiskPolicy(ctx, &gen.CreateRiskPolicyPayload{
		Name:    &name,
		Sources: []string{"shadow_mcp"},
		Action:  "block",
		ShadowMcpAllowedUrls: []string{
			"HTTPS://GITHUB.EXAMPLE.COM:443/mcp?token=ignored",
			"https://linear.example.com/sse",
		},
	})
	require.NoError(t, err)
	require.Equal(t, []string{
		"https://github.example.com/mcp",
		"https://linear.example.com/sse",
	}, shadowMCPPolicyAllowedURLs(t, ctx, ti.conn, created.ID))
}

func TestCreateRiskPolicy_ShadowMCPReconcileFailureRollsBack(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestRiskService(t)
	ti.reconcileShadowMCPPolicyURLs = func(context.Context, riskrepo.DBTX, policybypass.ReconcilePolicyURLsInput) error {
		return errors.New("injected grant failure")
	}
	name := "Rolled Back Shadow MCP"
	_, err := ti.service.CreateRiskPolicy(ctx, &gen.CreateRiskPolicyPayload{
		Name:                 &name,
		Sources:              []string{"shadow_mcp"},
		Action:               "block",
		ShadowMcpAllowedUrls: []string{"https://github.example.com/mcp"},
	})
	require.ErrorContains(t, errors.Unwrap(err), "injected grant failure")
	require.False(t, riskPolicyExistsByName(t, ctx, ti.conn, name))
}

func TestCreateRiskPolicy_ShadowMCPAllowedURLsRejectInvalidState(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestRiskService(t)
	name := "Shadow MCP Flag"
	_, err := ti.service.CreateRiskPolicy(ctx, &gen.CreateRiskPolicyPayload{
		Name:                 &name,
		Sources:              []string{"shadow_mcp"},
		Action:               "flag",
		ShadowMcpAllowedUrls: []string{"https://github.example.com/mcp"},
	})
	require.ErrorContains(t, err, "shadow mcp allowed urls require an enabled blocking shadow mcp policy")
	require.False(t, riskPolicyExistsByName(t, ctx, ti.conn, name))
}

func TestCreateRiskPolicy_ShadowMCPAllowedURLsRejectInvalidURL(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestRiskService(t)
	name := "Shadow MCP Invalid URL"
	_, err := ti.service.CreateRiskPolicy(ctx, &gen.CreateRiskPolicyPayload{
		Name:                 &name,
		Sources:              []string{"shadow_mcp"},
		Action:               "block",
		ShadowMcpAllowedUrls: []string{"not a shadow mcp url"},
	})
	require.ErrorContains(t, err, "invalid shadow mcp allowed urls")
	require.False(t, riskPolicyExistsByName(t, ctx, ti.conn, name))
}
