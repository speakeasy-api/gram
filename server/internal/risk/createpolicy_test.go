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
	ctx = withExactAccessGrants(t, ctx, ti.conn, access.Grant{Scope: access.ScopeOrgAdmin, Selector: access.ForResource(authCtx.ActiveOrganizationID)})

	enabled := true
	result, err := ti.service.CreateRiskPolicy(ctx, &gen.CreateRiskPolicyPayload{
		Name:    "Test Policy",
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
	ctx = withExactAccessGrants(t, ctx, ti.conn, access.Grant{Scope: access.ScopeOrgAdmin, Selector: access.ForResource(authCtx.ActiveOrganizationID)})

	result, err := ti.service.CreateRiskPolicy(ctx, &gen.CreateRiskPolicyPayload{
		Name: "No Sources",
	})
	require.NoError(t, err)
	require.Equal(t, []string{"gitleaks"}, result.Sources)
	require.True(t, result.Enabled) // default enabled
}

func TestCreateRiskPolicy_EmptyName(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestRiskService(t)

	authCtx, _ := contextvalues.GetAuthContext(ctx)
	ctx = withExactAccessGrants(t, ctx, ti.conn, access.Grant{Scope: access.ScopeOrgAdmin, Selector: access.ForResource(authCtx.ActiveOrganizationID)})

	_, err := ti.service.CreateRiskPolicy(ctx, &gen.CreateRiskPolicyPayload{
		Name: "",
	})
	require.Error(t, err)
}

func TestCreateRiskPolicy_NameTooLong(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestRiskService(t)

	authCtx, _ := contextvalues.GetAuthContext(ctx)
	ctx = withExactAccessGrants(t, ctx, ti.conn, access.Grant{Scope: access.ScopeOrgAdmin, Selector: access.ForResource(authCtx.ActiveOrganizationID)})

	var longName strings.Builder
	for range 101 {
		longName.WriteString("a")
	}
	_, err := ti.service.CreateRiskPolicy(ctx, &gen.CreateRiskPolicyPayload{
		Name: longName.String(),
	})
	require.Error(t, err)
}

func TestCreateRiskPolicy_Unauthorized(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestRiskService(t)

	// Set up enterprise account with zero grants — RBAC should deny.
	ctx = withExactAccessGrants(t, ctx, ti.conn)

	_, err := ti.service.CreateRiskPolicy(ctx, &gen.CreateRiskPolicyPayload{
		Name: "Should Fail",
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
	ctx = withExactAccessGrants(t, ctx, ti.conn, access.Grant{Scope: access.ScopeOrgAdmin, Selector: access.ForResource(authCtx.ActiveOrganizationID)})

	enabled := false
	result, err := ti.service.CreateRiskPolicy(ctx, &gen.CreateRiskPolicyPayload{
		Name:    "Disabled Policy",
		Enabled: &enabled,
	})
	require.NoError(t, err)
	require.False(t, result.Enabled)
	require.Empty(t, ti.signaler.calls) // should not signal when disabled
}
