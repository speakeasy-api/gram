package activities_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/background/activities"
	"github.com/speakeasy-api/gram/server/internal/billing"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

func TestWeeklyUsageSummary_ListTargetsAppliesBillingRecipientMatrix(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	db, err := infra.CloneTestDatabase(t, "weekly_usage_summary_recipient_matrix")
	require.NoError(t, err)
	enterpriseExplicit, _ := createAlertOrgWithAccountType(t, ctx, db, "enterprise@example.test", "", billing.TierEnterprise)
	createAlertOrgWithAccountType(t, ctx, db, "", "", billing.TierEnterprise)
	paygExplicit, _ := createAlertOrgWithAccountType(t, ctx, db, "payg@example.test", "", billing.TierPayg)
	paygAdmins, _ := createAlertOrgWithAccountType(t, ctx, db, "", "", billing.TierPayg)
	createAlertOrgWithAccountType(t, ctx, db, "free@example.test", "", billing.TierBase)

	activity := activities.NewWeeklyUsageSummary(testenv.NewLogger(t), db, nil, nil, nil)
	targets, err := activity.ListTargets(ctx)

	require.NoError(t, err)
	byID := make(map[string]activities.WeeklyUsageSummaryTarget, len(targets))
	for _, target := range targets {
		byID[target.OrganizationID] = target
	}
	require.Len(t, byID, 3)
	require.Equal(t, "enterprise@example.test", byID[enterpriseExplicit].AlertEmail)
	require.Equal(t, string(billing.TierEnterprise), byID[enterpriseExplicit].AccountType)
	require.Equal(t, "payg@example.test", byID[paygExplicit].AlertEmail)
	require.Equal(t, string(billing.TierPayg), byID[paygExplicit].AccountType)
	require.Empty(t, byID[paygAdmins].AlertEmail)
	require.Equal(t, string(billing.TierPayg), byID[paygAdmins].AccountType)
}
