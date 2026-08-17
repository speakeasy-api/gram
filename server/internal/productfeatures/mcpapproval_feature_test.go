package productfeatures_test

import (
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/features"
	accessrepo "github.com/speakeasy-api/gram/server/internal/access/repo"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/productfeatures"
	featurerepo "github.com/speakeasy-api/gram/server/internal/productfeatures/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

// An organization whose system roles were seeded before the mcp_approval
// scopes existed holds no grants for them, and the role seeder never revisits
// a role that already has grants — so enabling the feature must provision
// them, or every approval surface answers 403.
func TestProductFeaturesService_EnableMCPApprovalPatchesExistingRBACGrants(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestProductFeaturesService(t)
	organizationID := activeOrganizationID(t, ctx)
	seedOrganization(t, ctx, ti.conn, organizationID)
	require.NoError(t, authz.SeedSystemRoleGrants(ctx, ti.conn, organizationID))

	q := accessrepo.New(ti.conn)
	admin := systemRolePrincipal(t, ctx, q, authz.SystemRoleAdmin)
	member := systemRolePrincipal(t, ctx, q, authz.SystemRoleMember)
	deleteGrant(t, ctx, q, organizationID, admin, authz.ScopeMCPApprovalRead, authz.WildcardResource)
	deleteGrant(t, ctx, q, organizationID, admin, authz.ScopeMCPApprovalDecide, authz.WildcardResource)
	upsertGrant(t, ctx, q, organizationID, admin, authz.ScopeRiskPolicyEvaluate, "policy-custom")

	err := ti.service.SetProductFeature(ctx, &gen.SetProductFeaturePayload{
		FeatureName: string(productfeatures.FeatureMCPApproval),
		Enabled:     true,
	})
	require.NoError(t, err)

	grantsAfterEnable := organizationGrantKeys(t, ctx, q, organizationID)
	require.Equal(t, 1, grantsAfterEnable[grantKey(admin, authz.ScopeMCPApprovalRead, authz.WildcardResource)])
	require.Equal(t, 1, grantsAfterEnable[grantKey(admin, authz.ScopeMCPApprovalDecide, authz.WildcardResource)])
	require.Equal(t, 1, grantsAfterEnable[grantKey(admin, authz.ScopeRiskPolicyEvaluate, "policy-custom")])
	// Reviewing and deciding server access is an admin surface; members gain
	// nothing from the enable.
	require.Zero(t, grantsAfterEnable[grantKey(member, authz.ScopeMCPApprovalRead, authz.WildcardResource)])
	require.Zero(t, grantsAfterEnable[grantKey(member, authz.ScopeMCPApprovalDecide, authz.WildcardResource)])

	err = ti.service.SetProductFeature(ctx, &gen.SetProductFeaturePayload{
		FeatureName: string(productfeatures.FeatureMCPApproval),
		Enabled:     true,
	})
	require.NoError(t, err)
	require.Equal(t, grantsAfterEnable, organizationGrantKeys(t, ctx, q, organizationID))

	err = ti.service.SetProductFeature(ctx, &gen.SetProductFeaturePayload{
		FeatureName: string(productfeatures.FeatureMCPApproval),
		Enabled:     false,
	})
	require.NoError(t, err)
	require.Equal(t, grantsAfterEnable, organizationGrantKeys(t, ctx, q, organizationID))
}

// The production repair path: the feature flag already exists but the role
// grants do not. Re-enabling must provision the grants even though the flag
// write itself is a no-op.
func TestProductFeaturesService_ReenableMCPApprovalRepairsMissingGrants(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestProductFeaturesService(t)
	organizationID := activeOrganizationID(t, ctx)
	seedOrganization(t, ctx, ti.conn, organizationID)
	require.NoError(t, authz.SeedSystemRoleGrants(ctx, ti.conn, organizationID))

	// The flag lands through the generic path, the way affected organizations
	// got it before enablement provisioned grants.
	_, err := featurerepo.New(ti.conn).EnableFeature(ctx, featurerepo.EnableFeatureParams{
		OrganizationID: organizationID,
		FeatureName:    string(productfeatures.FeatureMCPApproval),
	})
	require.NoError(t, err)

	q := accessrepo.New(ti.conn)
	admin := systemRolePrincipal(t, ctx, q, authz.SystemRoleAdmin)
	deleteGrant(t, ctx, q, organizationID, admin, authz.ScopeMCPApprovalRead, authz.WildcardResource)
	deleteGrant(t, ctx, q, organizationID, admin, authz.ScopeMCPApprovalDecide, authz.WildcardResource)

	err = ti.service.SetProductFeature(ctx, &gen.SetProductFeaturePayload{
		FeatureName: string(productfeatures.FeatureMCPApproval),
		Enabled:     true,
	})
	require.NoError(t, err)

	grants := organizationGrantKeys(t, ctx, q, organizationID)
	require.Equal(t, 1, grants[grantKey(admin, authz.ScopeMCPApprovalRead, authz.WildcardResource)])
	require.Equal(t, 1, grants[grantKey(admin, authz.ScopeMCPApprovalDecide, authz.WildcardResource)])
}

func TestEnableMCPApprovalTx_RequiresExistingOrganization(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestProductFeaturesService(t)
	tx := testenv.BeginTx(t, ctx, ti.conn)
	err := productfeatures.EnableMCPApprovalTx(ctx, tx, "org_missing_mcp_approval_lock")
	require.ErrorIs(t, err, pgx.ErrNoRows)
}

func TestEnableMCPApprovalTx_RollsBackWithCallerTransaction(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestProductFeaturesService(t)
	organizationID := activeOrganizationID(t, ctx)
	seedOrganization(t, ctx, ti.conn, organizationID)
	tx := testenv.BeginTx(t, ctx, ti.conn)

	require.NoError(t, productfeatures.EnableMCPApprovalTx(ctx, tx, organizationID))
	enabled, err := featurerepo.New(tx).IsFeatureEnabled(ctx, featurerepo.IsFeatureEnabledParams{
		OrganizationID: organizationID,
		FeatureName:    string(productfeatures.FeatureMCPApproval),
	})
	require.NoError(t, err)
	require.True(t, enabled)
	require.NoError(t, tx.Rollback(ctx))

	enabled, err = featurerepo.New(ti.conn).IsFeatureEnabled(ctx, featurerepo.IsFeatureEnabledParams{
		OrganizationID: organizationID,
		FeatureName:    string(productfeatures.FeatureMCPApproval),
	})
	require.NoError(t, err)
	require.False(t, enabled)
}
