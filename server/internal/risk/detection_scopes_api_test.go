package risk_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/gen/risk"
	"github.com/speakeasy-api/gram/server/gen/types"
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
	require.Len(t, result.Categories, len(byKey), "categories must be unique by key; the Classify fallback entries must not leak")

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
	require.NotEmpty(t, custom.RecommendedScopeRationale)
	require.True(t, custom.RecommendedScopeApplicable)

	accountIdentity := byKey[string(categories.CategoryAccountIdentity)]
	require.NotNil(t, accountIdentity)
	require.Empty(t, accountIdentity.RecommendedScopeInclude)
	require.Empty(t, accountIdentity.RecommendedScopeExempt)
	require.NotEmpty(t, accountIdentity.RecommendedScopeRationale)
	require.False(t, accountIdentity.RecommendedScopeApplicable)
}

func TestCreateRiskPolicyDetectionScopesPersistsAndRoundTrips(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestRiskService(t)

	authCtx, _ := contextvalues.GetAuthContext(ctx)
	ctx = withExactAccessGrants(t, ctx, ti.conn,
		authz.Grant{Scope: authz.ScopeOrgAdmin, Selector: authz.NewSelector(authz.ScopeOrgAdmin, authCtx.ActiveOrganizationID)},
	)

	scopes := []*types.RiskDetectionScope{
		{Category: string(categories.CategoryPromptInjection), ScopeInclude: new(`kind == "user_message"`), ScopeExempt: nil},
		{Category: string(categories.CategoryCLIDestructive), ScopeInclude: nil, ScopeExempt: nil},
	}
	created, err := ti.service.CreateRiskPolicy(ctx, &risk.CreateRiskPolicyPayload{
		Name:            new("Detection Scopes"),
		DetectionScopes: scopes,
	})
	require.NoError(t, err)
	require.Equal(t, scopes, created.DetectionScopes)

	got, err := ti.service.GetRiskPolicy(ctx, &risk.GetRiskPolicyPayload{ID: created.ID})
	require.NoError(t, err)
	require.Equal(t, scopes, got.DetectionScopes)

	policyID, err := uuid.Parse(created.ID)
	require.NoError(t, err)
	row, err := riskrepo.New(ti.conn).GetRiskPolicy(ctx, riskrepo.GetRiskPolicyParams{
		ID:        policyID,
		ProjectID: *authCtx.ProjectID,
	})
	require.NoError(t, err)
	require.Equal(t, []ra.DetectionScopeConfig{
		{Category: "prompt_injection", ScopeInclude: `kind == "user_message"`, ScopeExempt: ""},
		{Category: "cli_destructive", ScopeInclude: "", ScopeExempt: ""},
	}, ra.DetectionScopesFromConfig(row.AnalyzerConfig))
}

func TestUpdateRiskPolicyDetectionScopesOmitPreservesEmptyClears(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestRiskService(t)

	authCtx, _ := contextvalues.GetAuthContext(ctx)
	ctx = withExactAccessGrants(t, ctx, ti.conn,
		authz.Grant{Scope: authz.ScopeOrgAdmin, Selector: authz.NewSelector(authz.ScopeOrgAdmin, authCtx.ActiveOrganizationID)},
	)

	scopes := []*types.RiskDetectionScope{
		{Category: string(categories.CategoryPromptInjection), ScopeInclude: nil, ScopeExempt: nil},
	}
	created, err := ti.service.CreateRiskPolicy(ctx, &risk.CreateRiskPolicyPayload{
		Name:            new("Detection Scope Update"),
		DetectionScopes: scopes,
	})
	require.NoError(t, err)

	renamed, err := ti.service.UpdateRiskPolicy(ctx, &risk.UpdateRiskPolicyPayload{
		ID:   created.ID,
		Name: "Renamed Detection Scope Update",
	})
	require.NoError(t, err)
	require.Equal(t, scopes, renamed.DetectionScopes)

	cleared, err := ti.service.UpdateRiskPolicy(ctx, &risk.UpdateRiskPolicyPayload{
		ID:              created.ID,
		Name:            "Renamed Detection Scope Update",
		DetectionScopes: []*types.RiskDetectionScope{},
	})
	require.NoError(t, err)
	require.Empty(t, cleared.DetectionScopes)
}

func TestRiskPolicyDetectionScopesValidation(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestRiskService(t)

	authCtx, _ := contextvalues.GetAuthContext(ctx)
	ctx = withExactAccessGrants(t, ctx, ti.conn,
		authz.Grant{Scope: authz.ScopeOrgAdmin, Selector: authz.NewSelector(authz.ScopeOrgAdmin, authCtx.ActiveOrganizationID)},
	)

	invalid := []struct {
		name   string
		scopes []*types.RiskDetectionScope
	}{
		{"null scope", []*types.RiskDetectionScope{nil}},
		{"unknown category", []*types.RiskDetectionScope{
			{Category: "not_a_category", ScopeInclude: nil, ScopeExempt: nil},
		}},
		{"session-scoped category", []*types.RiskDetectionScope{
			{Category: string(categories.CategoryAccountIdentity), ScopeInclude: nil, ScopeExempt: nil},
		}},
		{"duplicate category", []*types.RiskDetectionScope{
			{Category: string(categories.CategoryPII), ScopeInclude: nil, ScopeExempt: nil},
			{Category: string(categories.CategoryPII), ScopeInclude: nil, ScopeExempt: nil},
		}},
		{"bad CEL", []*types.RiskDetectionScope{
			{Category: string(categories.CategoryPII), ScopeInclude: new(`kind ==`), ScopeExempt: nil},
		}},
	}
	for _, tc := range invalid {
		_, err := ti.service.CreateRiskPolicy(ctx, &risk.CreateRiskPolicyPayload{
			Name:            new("Invalid Detection Scope"),
			DetectionScopes: tc.scopes,
		})
		requireOopsCode(t, err, oops.CodeInvalid)
	}

	created, err := ti.service.CreateRiskPolicy(ctx, &risk.CreateRiskPolicyPayload{
		Name: new("Valid Then Invalid Update"),
	})
	require.NoError(t, err)

	_, err = ti.service.UpdateRiskPolicy(ctx, &risk.UpdateRiskPolicyPayload{
		ID:   created.ID,
		Name: created.Name,
		DetectionScopes: []*types.RiskDetectionScope{
			{Category: "not_a_category", ScopeInclude: nil, ScopeExempt: nil},
		},
	})
	requireOopsCode(t, err, oops.CodeInvalid)
}

func TestRiskPolicyRejectsLegacyScopeFields(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestRiskService(t)

	authCtx, _ := contextvalues.GetAuthContext(ctx)
	ctx = withExactAccessGrants(t, ctx, ti.conn,
		authz.Grant{Scope: authz.ScopeOrgAdmin, Selector: authz.NewSelector(authz.ScopeOrgAdmin, authCtx.ActiveOrganizationID)},
	)

	_, err := ti.service.CreateRiskPolicy(ctx, &risk.CreateRiskPolicyPayload{
		Name:         new("Legacy Message Types"),
		MessageTypes: []string{"user_message"},
	})
	requireOopsCode(t, err, oops.CodeInvalid)

	_, err = ti.service.CreateRiskPolicy(ctx, &risk.CreateRiskPolicyPayload{
		Name:         new("Legacy Scope Include"),
		ScopeInclude: new(`kind == "user_message"`),
	})
	requireOopsCode(t, err, oops.CodeInvalid)

	// Empty values are how older clients said "no restriction"; they stay accepted.
	created, err := ti.service.CreateRiskPolicy(ctx, &risk.CreateRiskPolicyPayload{
		Name:         new("Legacy Empty Values"),
		MessageTypes: []string{},
		ScopeInclude: new(""),
		ScopeExempt:  new(""),
	})
	require.NoError(t, err)

	_, err = ti.service.UpdateRiskPolicy(ctx, &risk.UpdateRiskPolicyPayload{
		ID:          created.ID,
		Name:        created.Name,
		ScopeExempt: new(`content.matchText("x")`),
	})
	requireOopsCode(t, err, oops.CodeInvalid)
}
