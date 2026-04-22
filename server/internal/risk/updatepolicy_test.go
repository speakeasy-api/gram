package risk_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/risk"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
)

func TestUpdateRiskPolicy_Success(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestRiskService(t)

	authCtx, _ := contextvalues.GetAuthContext(ctx)
	ctx = withExactAccessGrants(t, ctx, ti.conn,
		access.NewGrant(access.ScopeOrgAdmin, authCtx.ActiveOrganizationID),
	)

	enabled := true
	created, err := ti.service.CreateRiskPolicy(ctx, &gen.CreateRiskPolicyPayload{
		Name:    "Original",
		Sources: []string{"gitleaks"},
		Enabled: &enabled,
	})
	require.NoError(t, err)

	updated, err := ti.service.UpdateRiskPolicy(ctx, &gen.UpdateRiskPolicyPayload{
		ID:   created.ID,
		Name: "Renamed",
	})
	require.NoError(t, err)
	require.Equal(t, "Renamed", updated.Name)
	require.Equal(t, []string{"gitleaks"}, updated.Sources, "sources should be preserved")
	require.True(t, updated.Enabled, "enabled should be preserved")
	require.Equal(t, int64(1), updated.Version, "name-only change should not bump version")
}

func TestUpdateRiskPolicy_BumpsVersionOnSourcesChange(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestRiskService(t)

	authCtx, _ := contextvalues.GetAuthContext(ctx)
	ctx = withExactAccessGrants(t, ctx, ti.conn,
		access.NewGrant(access.ScopeOrgAdmin, authCtx.ActiveOrganizationID),
	)

	created, err := ti.service.CreateRiskPolicy(ctx, &gen.CreateRiskPolicyPayload{
		Name:    "Version Test",
		Sources: []string{"gitleaks"},
	})
	require.NoError(t, err)
	require.Equal(t, int64(1), created.Version)

	updated, err := ti.service.UpdateRiskPolicy(ctx, &gen.UpdateRiskPolicyPayload{
		ID:      created.ID,
		Name:    "Version Test",
		Sources: []string{"gitleaks", "llm"},
	})
	require.NoError(t, err)
	require.Equal(t, int64(2), updated.Version, "sources change should bump version")
}

func TestUpdateRiskPolicy_BumpsVersionOnEnabledChange(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestRiskService(t)

	authCtx, _ := contextvalues.GetAuthContext(ctx)
	ctx = withExactAccessGrants(t, ctx, ti.conn,
		access.NewGrant(access.ScopeOrgAdmin, authCtx.ActiveOrganizationID),
	)

	created, err := ti.service.CreateRiskPolicy(ctx, &gen.CreateRiskPolicyPayload{
		Name: "Toggle Test",
	})
	require.NoError(t, err)

	disabled := false
	updated, err := ti.service.UpdateRiskPolicy(ctx, &gen.UpdateRiskPolicyPayload{
		ID:      created.ID,
		Name:    "Toggle Test",
		Enabled: &disabled,
	})
	require.NoError(t, err)
	require.False(t, updated.Enabled)
	require.Equal(t, int64(2), updated.Version, "enabled change should bump version")
}

func TestUpdateRiskPolicy_PreservesFieldsWhenOmitted(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestRiskService(t)

	authCtx, _ := contextvalues.GetAuthContext(ctx)
	ctx = withExactAccessGrants(t, ctx, ti.conn,
		access.NewGrant(access.ScopeOrgAdmin, authCtx.ActiveOrganizationID),
	)

	disabled := false
	created, err := ti.service.CreateRiskPolicy(ctx, &gen.CreateRiskPolicyPayload{
		Name:    "Preserve Test",
		Sources: []string{"gitleaks"},
		Enabled: &disabled,
	})
	require.NoError(t, err)
	require.False(t, created.Enabled)

	// Update only name — enabled and sources should be preserved.
	updated, err := ti.service.UpdateRiskPolicy(ctx, &gen.UpdateRiskPolicyPayload{
		ID:   created.ID,
		Name: "New Name",
	})
	require.NoError(t, err)
	require.False(t, updated.Enabled, "enabled should remain false")
	require.Equal(t, []string{"gitleaks"}, updated.Sources, "sources should remain")
}
