package activities_test

import (
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/speakeasy-api/gram/server/internal/background/activities"
	usagerepo "github.com/speakeasy-api/gram/server/internal/usage/repo"
	"github.com/stretchr/testify/require"
	"testing"
	"time"
)

func TestPartnerDailyAllocationsWaiveInitialDayAndPreserveTwoMonthlyCycles(t *testing.T) {
	t.Parallel()
	db, org, stripe, activity := setupAllocationTest(t, "partner_daily_cycles")
	anchor := time.Date(2027, time.December, 31, 13, 17, 0, 0, time.UTC)
	firstEnd := time.Date(2028, time.January, 31, 13, 17, 0, 0, time.UTC)
	end := time.Date(2028, time.February, 29, 13, 17, 0, 0, time.UTC)
	q := usagerepo.New(db)
	require.NoError(t, q.CreateStripeBillingMetadataFixture(t.Context(), usagerepo.CreateStripeBillingMetadataFixtureParams{OrganizationID: org, StripeCustomerID: pgtype.Text{String: "cus_" + org, Valid: true}}))
	_, err := q.ActivatePaygBillingMetadata(t.Context(), usagerepo.ActivatePaygBillingMetadataParams{OrganizationID: org, StripeCustomerID: pgtype.Text{String: "cus_" + org, Valid: true}, StripeSubscriptionID: pgtype.Text{String: "sub_" + org, Valid: true}, StripeBillingCycleAnchor: pgtype.Timestamptz{Time: anchor, Valid: true}, BillingCycleAnchorDay: 31})
	require.NoError(t, err)
	require.NoError(t, q.InsertOrganizationPromotionApplicationFixture(t.Context(), usagerepo.InsertOrganizationPromotionApplicationFixtureParams{OrganizationID: pgtype.Text{String: org, Valid: true}, StripePromotionCodeID: "promo_partner", StripeSubscriptionID: pgtype.Text{String: "sub_" + org, Valid: true}, RedeemedAt: pgtype.Timestamptz{Time: anchor, Valid: true}}))
	addStripeInvoice(t, db, stripe, org, "in_partner_first", anchor, firstEnd, "draft", 0)
	addStripeInvoice(t, db, stripe, org, "in_partner_second", firstEnd, end, "draft", 0)
	for day := anchor.Truncate(24 * time.Hour); day.Before(end.Truncate(24 * time.Hour)); day = day.AddDate(0, 0, 1) {
		putDailySpend(t, db, org, day, "1.000000")
	}
	require.NoError(t, activity.Do(t.Context(), activities.SettleStripeInvoiceAllocationsArgs{Now: firstEnd.Add(48 * time.Hour)}))
	now := end.Add(48 * time.Hour)
	require.NoError(t, activity.Do(t.Context(), activities.SettleStripeInvoiceAllocationsArgs{Now: now}))
	require.NoError(t, activity.Do(t.Context(), activities.SettleStripeInvoiceAllocationsArgs{Now: now.Add(time.Hour)}))
	rows := listChatAllocationFixtures(t, db, org)
	count := 0
	for _, row := range rows {
		if row.Seq == 1 {
			count++
		}
	}
	require.Equal(t, 59, count) // Jan 1 through Feb 28, including Jan 31 exactly once.
	seen := map[string]bool{}
	for _, row := range rows {
		if row.Seq != 1 {
			continue
		}
		require.NotEqual(t, "2027-12-31:chat", row.SourceKey)
		require.False(t, seen[row.SourceKey])
		seen[row.SourceKey] = true
		if row.SourceKey == "2028-01-31:chat" {
			require.Equal(t, "in_partner_second", row.OriginalInvoiceID.String)
		}
	}
	require.True(t, seen["2028-01-01:chat"])
	require.True(t, seen["2028-01-31:chat"])
	require.True(t, seen["2028-02-28:chat"])
	// Cancellation must not erase the original waiver cutoff before late settlement.
	_, err = q.DeactivatePaygBillingMetadata(t.Context(), usagerepo.DeactivatePaygBillingMetadataParams{OrganizationID: org, StripeCustomerID: pgtype.Text{String: "cus_" + org, Valid: true}, StripeSubscriptionID: pgtype.Text{String: "sub_" + org, Valid: true}})
	require.NoError(t, err)
	require.NoError(t, activity.Do(t.Context(), activities.SettleStripeInvoiceAllocationsArgs{Now: now.Add(2 * time.Hour)}))
	require.Len(t, listChatAllocationFixtures(t, db, org), len(rows))
	var cents int64
	for _, item := range stripe.itemInputs {
		cents += item.AmountCents
	}
	require.Equal(t, int64(5900), cents)
}
