package productfeatures_test

import (
	"context"
	"fmt"
	"slices"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/features"
	accessrepo "github.com/speakeasy-api/gram/server/internal/access/repo"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	"github.com/speakeasy-api/gram/server/internal/productfeatures"
	featurerepo "github.com/speakeasy-api/gram/server/internal/productfeatures/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

func TestProductFeaturesService_SkillsDefaultsOffAndEnables(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestProductFeaturesService(t)
	result, err := ti.service.GetProductFeatures(ctx, &gen.GetProductFeaturesPayload{SessionToken: nil})
	require.NoError(t, err)
	require.False(t, result.SkillsEnabled)

	err = ti.service.SetProductFeature(ctx, &gen.SetProductFeaturePayload{
		FeatureName: string(productfeatures.FeatureSkills),
		Enabled:     true,
	})
	require.NoError(t, err)

	result, err = ti.service.GetProductFeatures(ctx, &gen.GetProductFeaturesPayload{SessionToken: nil})
	require.NoError(t, err)
	require.True(t, result.SkillsEnabled)
}

func TestProductFeaturesService_EnableSkillsPatchesExistingRBACGrants(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestProductFeaturesService(t)
	organizationID := activeOrganizationID(t, ctx)
	seedOrganization(t, ctx, ti.conn, organizationID)
	require.NoError(t, productfeatures.EnableRBACTx(ctx, ti.conn, organizationID))

	q := accessrepo.New(ti.conn)
	admin := systemRolePrincipal(t, ctx, q, authz.SystemRoleAdmin)
	member := systemRolePrincipal(t, ctx, q, authz.SystemRoleMember)
	deleteGrant(t, ctx, q, organizationID, admin, authz.ScopeSkillWrite, authz.WildcardResource)
	deleteGrant(t, ctx, q, organizationID, member, authz.ScopeSkillRead, authz.WildcardResource)
	upsertGrant(t, ctx, q, organizationID, admin, authz.ScopeRiskPolicyEvaluate, "policy-custom")
	upsertGrant(t, ctx, q, organizationID, member, authz.ScopeSkillBlockedRead, "project-excluded")

	err := ti.service.SetProductFeature(ctx, &gen.SetProductFeaturePayload{
		FeatureName: string(productfeatures.FeatureSkills),
		Enabled:     true,
	})
	require.NoError(t, err)

	grantsAfterEnable := organizationGrantKeys(t, ctx, q, organizationID)
	require.Equal(t, 1, grantsAfterEnable[grantKey(admin, authz.ScopeSkillRead, authz.WildcardResource)])
	require.Equal(t, 1, grantsAfterEnable[grantKey(admin, authz.ScopeSkillWrite, authz.WildcardResource)])
	require.Equal(t, 1, grantsAfterEnable[grantKey(member, authz.ScopeSkillRead, authz.WildcardResource)])
	require.Equal(t, 1, grantsAfterEnable[grantKey(admin, authz.ScopeRiskPolicyEvaluate, "policy-custom")])
	require.Equal(t, 1, grantsAfterEnable[grantKey(member, authz.ScopeSkillBlockedRead, "project-excluded")])

	err = ti.service.SetProductFeature(ctx, &gen.SetProductFeaturePayload{
		FeatureName: string(productfeatures.FeatureSkills),
		Enabled:     true,
	})
	require.NoError(t, err)
	require.Equal(t, grantsAfterEnable, organizationGrantKeys(t, ctx, q, organizationID))

	err = ti.service.SetProductFeature(ctx, &gen.SetProductFeaturePayload{
		FeatureName: string(productfeatures.FeatureSkills),
		Enabled:     false,
	})
	require.NoError(t, err)
	require.Equal(t, grantsAfterEnable, organizationGrantKeys(t, ctx, q, organizationID))

	result, err := ti.service.GetProductFeatures(ctx, &gen.GetProductFeaturesPayload{SessionToken: nil})
	require.NoError(t, err)
	require.False(t, result.SkillsEnabled)
}

func TestProductFeaturesService_SkillsBeforeRBACSeedsCompleteDefaults(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestProductFeaturesService(t)
	organizationID := activeOrganizationID(t, ctx)
	seedOrganization(t, ctx, ti.conn, organizationID)

	err := ti.service.SetProductFeature(ctx, &gen.SetProductFeaturePayload{
		FeatureName: string(productfeatures.FeatureSkills),
		Enabled:     true,
	})
	require.NoError(t, err)

	q := accessrepo.New(ti.conn)
	rows, err := q.ListPrincipalGrantsByOrg(ctx, accessrepo.ListPrincipalGrantsByOrgParams{
		OrganizationID: organizationID,
		PrincipalUrn:   "",
	})
	require.NoError(t, err)
	require.Empty(t, rows)

	tx := testenv.BeginTx(t, ctx, ti.conn)
	require.NoError(t, productfeatures.EnableRBACTx(ctx, tx, organizationID))
	require.NoError(t, tx.Commit(ctx))

	for _, roleSlug := range []string{authz.SystemRoleAdmin, authz.SystemRoleMember} {
		principal := systemRolePrincipal(t, ctx, q, roleSlug)
		requireSystemRoleDefaults(t, ctx, q, organizationID, principal, authz.SystemRoleGrants[roleSlug])
	}
}

func TestEnableSkillsTx_RollsBackWithCallerTransaction(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestProductFeaturesService(t)
	organizationID := activeOrganizationID(t, ctx)
	tx := testenv.BeginTx(t, ctx, ti.conn)

	require.NoError(t, productfeatures.EnableSkillsTx(ctx, tx, organizationID))
	enabled, err := featurerepo.New(tx).IsFeatureEnabled(ctx, featurerepo.IsFeatureEnabledParams{
		OrganizationID: organizationID,
		FeatureName:    string(productfeatures.FeatureSkills),
	})
	require.NoError(t, err)
	require.True(t, enabled)
	require.NoError(t, tx.Rollback(ctx))

	enabled, err = featurerepo.New(ti.conn).IsFeatureEnabled(ctx, featurerepo.IsFeatureEnabledParams{
		OrganizationID: organizationID,
		FeatureName:    string(productfeatures.FeatureSkills),
	})
	require.NoError(t, err)
	require.False(t, enabled)
}

func activeOrganizationID(t *testing.T, ctx context.Context) string {
	t.Helper()
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx)
	return authCtx.ActiveOrganizationID
}

func seedOrganization(t *testing.T, ctx context.Context, conn *pgxpool.Pool, organizationID string) {
	t.Helper()
	_, err := orgrepo.New(conn).UpsertOrganizationMetadata(ctx, orgrepo.UpsertOrganizationMetadataParams{
		ID:       organizationID,
		Name:     "Skills Test Organization",
		Slug:     organizationID,
		WorkosID: conv.PtrToPGText(conv.PtrEmpty("workos-" + organizationID)),
	})
	require.NoError(t, err)
}

func systemRolePrincipal(t *testing.T, ctx context.Context, q *accessrepo.Queries, roleSlug string) urn.Principal {
	t.Helper()
	role, err := q.GetGlobalRoleBySlug(ctx, roleSlug)
	require.NoError(t, err)
	return urn.NewPrincipal(urn.PrincipalTypeRole, "global:"+role.ID.String())
}

func upsertGrant(t *testing.T, ctx context.Context, q *accessrepo.Queries, organizationID string, principal urn.Principal, scope authz.Scope, resourceID string) {
	t.Helper()
	selector, err := authz.NewSelector(scope, resourceID).MarshalJSON()
	require.NoError(t, err)
	_, err = q.UpsertPrincipalGrant(ctx, accessrepo.UpsertPrincipalGrantParams{
		OrganizationID: organizationID,
		PrincipalUrn:   principal,
		Scope:          string(scope),
		Effect:         pgtype.Text{String: string(authz.PolicyEffectAllow), Valid: true},
		Selectors:      selector,
	})
	require.NoError(t, err)
}

func deleteGrant(t *testing.T, ctx context.Context, q *accessrepo.Queries, organizationID string, principal urn.Principal, scope authz.Scope, resourceID string) {
	t.Helper()
	rows, err := q.ListPrincipalGrantsByOrg(ctx, accessrepo.ListPrincipalGrantsByOrgParams{
		OrganizationID: organizationID,
		PrincipalUrn:   principal.String(),
	})
	require.NoError(t, err)
	for _, row := range rows {
		selector, err := authz.SelectorFromRow(row.Selectors)
		require.NoError(t, err)
		if row.Scope == string(scope) && selector.ResourceID() == resourceID {
			_, err := q.DeletePrincipalGrant(ctx, accessrepo.DeletePrincipalGrantParams{
				ID:             row.ID,
				OrganizationID: organizationID,
			})
			require.NoError(t, err)
			return
		}
	}
	require.Failf(t, "grant not found", "%s %s", scope, resourceID)
}

func organizationGrantKeys(t *testing.T, ctx context.Context, q *accessrepo.Queries, organizationID string) map[string]int {
	t.Helper()
	rows, err := q.ListPrincipalGrantsByOrg(ctx, accessrepo.ListPrincipalGrantsByOrgParams{
		OrganizationID: organizationID,
		PrincipalUrn:   "",
	})
	require.NoError(t, err)
	result := make(map[string]int, len(rows))
	for _, row := range rows {
		selector, err := authz.SelectorFromRow(row.Selectors)
		require.NoError(t, err)
		result[grantKey(row.PrincipalUrn, authz.Scope(row.Scope), selector.ResourceID())]++
	}
	return result
}

func grantKey(principal fmt.Stringer, scope authz.Scope, resourceID string) string {
	return principal.String() + "|" + string(scope) + "|" + resourceID
}

func requireSystemRoleDefaults(t *testing.T, ctx context.Context, q *accessrepo.Queries, organizationID string, principal urn.Principal, expected []*authz.RoleGrant) {
	t.Helper()
	rows, err := q.ListPrincipalGrantsByOrg(ctx, accessrepo.ListPrincipalGrantsByOrgParams{
		OrganizationID: organizationID,
		PrincipalUrn:   principal.String(),
	})
	require.NoError(t, err)
	require.Len(t, rows, len(expected))

	actualScopes := make([]string, 0, len(rows))
	for _, row := range rows {
		selector, err := authz.SelectorFromRow(row.Selectors)
		require.NoError(t, err)
		require.Equal(t, authz.WildcardResource, selector.ResourceID())
		actualScopes = append(actualScopes, row.Scope)
	}
	expectedScopes := make([]string, 0, len(expected))
	for _, grant := range expected {
		expectedScopes = append(expectedScopes, grant.Scope)
	}
	slices.Sort(actualScopes)
	slices.Sort(expectedScopes)
	require.Equal(t, expectedScopes, actualScopes)
}
