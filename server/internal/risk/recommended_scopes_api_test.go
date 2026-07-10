package risk_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/gen/risk"
	"github.com/speakeasy-api/gram/server/internal/authz"
	ra "github.com/speakeasy-api/gram/server/internal/background/activities/risk_analysis"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/risk/categories"
	"github.com/speakeasy-api/gram/server/internal/risk/recommendedscopes"
	riskrepo "github.com/speakeasy-api/gram/server/internal/risk/repo"
)

func TestListRiskCategoriesRecommendedScopes(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestRiskService(t)

	authCtx, _ := contextvalues.GetAuthContext(ctx)
	ctx = withExactAccessGrants(t, ctx, ti.conn,
		authz.Grant{Scope: authz.ScopeOrgAdmin, Selector: authz.NewSelector(authz.ScopeOrgAdmin, authCtx.ActiveOrganizationID)},
	)

	result, err := ti.service.ListRiskCategories(ctx, &risk.ListRiskCategoriesPayload{})
	require.NoError(t, err)
	require.Equal(t, int64(recommendedscopes.Version), result.RecommendedScopesVersion)

	byKey := map[string]*risk.RiskCategoryDefinition{}
	for _, cat := range result.Categories {
		byKey[cat.Key] = cat
	}

	promptInjection := byKey[string(categories.CategoryPromptInjection)]
	require.NotNil(t, promptInjection)
	promptInjectionRec, ok := recommendedscopes.For(categories.CategoryPromptInjection)
	require.True(t, ok)
	require.Equal(t, promptInjectionRec.ScopeInclude, promptInjection.RecommendedScopeInclude)
	require.Equal(t, promptInjectionRec.ScopeExempt, promptInjection.RecommendedScopeExempt)
	require.Equal(t, promptInjectionRec.Rationale, promptInjection.RecommendedScopeRationale)
	require.Equal(t, promptInjectionRec.Applicable, promptInjection.RecommendedScopeApplicable)

	custom := byKey[string(categories.CategoryCustom)]
	require.NotNil(t, custom)
	require.Empty(t, custom.RecommendedScopeInclude)
	require.Empty(t, custom.RecommendedScopeExempt)
	require.Empty(t, custom.RecommendedScopeRationale)
	require.True(t, custom.RecommendedScopeApplicable)

	accountIdentity := byKey[string(categories.CategoryAccountIdentity)]
	require.NotNil(t, accountIdentity)
	require.Empty(t, accountIdentity.RecommendedScopeInclude)
	require.Empty(t, accountIdentity.RecommendedScopeExempt)
	require.NotEmpty(t, accountIdentity.RecommendedScopeRationale)
	require.False(t, accountIdentity.RecommendedScopeApplicable)
}

func TestCreateRiskPolicyDisabledRecommendedScopesPersistsAndRoundTrips(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestRiskService(t)

	authCtx, _ := contextvalues.GetAuthContext(ctx)
	ctx = withExactAccessGrants(t, ctx, ti.conn,
		authz.Grant{Scope: authz.ScopeOrgAdmin, Selector: authz.NewSelector(authz.ScopeOrgAdmin, authCtx.ActiveOrganizationID)},
	)

	disabled := []string{string(categories.CategoryPromptInjection), string(categories.CategoryCLIDestructive)}
	created, err := ti.service.CreateRiskPolicy(ctx, &risk.CreateRiskPolicyPayload{
		Name:                      new("Recommended Scope Opt-Outs"),
		DisabledRecommendedScopes: disabled,
	})
	require.NoError(t, err)
	require.Equal(t, disabled, created.DisabledRecommendedScopes)

	got, err := ti.service.GetRiskPolicy(ctx, &risk.GetRiskPolicyPayload{ID: created.ID})
	require.NoError(t, err)
	require.Equal(t, disabled, got.DisabledRecommendedScopes)

	policyID, err := uuid.Parse(created.ID)
	require.NoError(t, err)
	row, err := riskrepo.New(ti.conn).GetRiskPolicy(ctx, riskrepo.GetRiskPolicyParams{
		ID:        policyID,
		ProjectID: *authCtx.ProjectID,
	})
	require.NoError(t, err)
	require.Equal(t, disabled, ra.DisabledRecommendedScopesFromConfig(row.AnalyzerConfig))
}

func TestUpdateRiskPolicyDisabledRecommendedScopesOmitPreservesEmptyClears(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestRiskService(t)

	authCtx, _ := contextvalues.GetAuthContext(ctx)
	ctx = withExactAccessGrants(t, ctx, ti.conn,
		authz.Grant{Scope: authz.ScopeOrgAdmin, Selector: authz.NewSelector(authz.ScopeOrgAdmin, authCtx.ActiveOrganizationID)},
	)

	disabled := []string{string(categories.CategoryPromptInjection), string(categories.CategoryCLIDestructive)}
	created, err := ti.service.CreateRiskPolicy(ctx, &risk.CreateRiskPolicyPayload{
		Name:                      new("Recommended Scope Update"),
		DisabledRecommendedScopes: disabled,
	})
	require.NoError(t, err)

	renamed, err := ti.service.UpdateRiskPolicy(ctx, &risk.UpdateRiskPolicyPayload{
		ID:   created.ID,
		Name: "Renamed Recommended Scope Update",
	})
	require.NoError(t, err)
	require.Equal(t, disabled, renamed.DisabledRecommendedScopes)

	cleared, err := ti.service.UpdateRiskPolicy(ctx, &risk.UpdateRiskPolicyPayload{
		ID:                        created.ID,
		Name:                      "Renamed Recommended Scope Update",
		DisabledRecommendedScopes: []string{},
	})
	require.NoError(t, err)
	require.Empty(t, cleared.DisabledRecommendedScopes)
}

func TestRiskPolicyDisabledRecommendedScopesRejectsUnknownCategory(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestRiskService(t)

	authCtx, _ := contextvalues.GetAuthContext(ctx)
	ctx = withExactAccessGrants(t, ctx, ti.conn,
		authz.Grant{Scope: authz.ScopeOrgAdmin, Selector: authz.NewSelector(authz.ScopeOrgAdmin, authCtx.ActiveOrganizationID)},
	)

	_, err := ti.service.CreateRiskPolicy(ctx, &risk.CreateRiskPolicyPayload{
		Name:                      new("Unknown Recommended Scope"),
		DisabledRecommendedScopes: []string{"not_a_category"},
	})
	requireOopsCode(t, err, oops.CodeInvalid)

	created, err := ti.service.CreateRiskPolicy(ctx, &risk.CreateRiskPolicyPayload{
		Name: new("Known Recommended Scope"),
	})
	require.NoError(t, err)

	_, err = ti.service.UpdateRiskPolicy(ctx, &risk.UpdateRiskPolicyPayload{
		ID:                        created.ID,
		Name:                      created.Name,
		DisabledRecommendedScopes: []string{"not_a_category"},
	})
	requireOopsCode(t, err, oops.CodeInvalid)
}
