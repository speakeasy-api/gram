package admin

import (
	"github.com/jackc/pgx/v5/pgtype"
	gen "github.com/speakeasy-api/gram/server/gen/admin"
	usagerepo "github.com/speakeasy-api/gram/server/internal/usage/repo"
	"github.com/stretchr/testify/require"
	"testing"
	"time"
)

func TestSetStripeSubscriptionCannotOverwriteRedeemedPartnerAnchor(t *testing.T) {
	t.Parallel()
	ctx, svc, db := newTestAdminService(t)
	fake := enableStripeCustomerLookup(svc)
	anchor := time.Date(2028, time.January, 31, 13, 17, 0, 0, time.UTC)
	fake.subscriptionByID = subscriptionFor("cus_partner", "sub_replacement", anchor.Add(time.Hour))
	seedPaygCustomer(t, ctx, db, "org_partner_anchor", "partner-anchor", "cus_partner")
	q := usagerepo.New(db)
	_, err := q.ActivatePaygBillingMetadata(ctx, usagerepo.ActivatePaygBillingMetadataParams{OrganizationID: "org_partner_anchor", StripeCustomerID: pgtype.Text{String: "cus_partner", Valid: true}, StripeSubscriptionID: pgtype.Text{String: "sub_original", Valid: true}, StripeBillingCycleAnchor: pgtype.Timestamptz{Time: anchor, Valid: true}, BillingCycleAnchorDay: 31})
	require.NoError(t, err)
	require.NoError(t, q.InsertOrganizationPromotionApplicationFixture(ctx, usagerepo.InsertOrganizationPromotionApplicationFixtureParams{OrganizationID: pgtype.Text{String: "org_partner_anchor", Valid: true}, StripePromotionCodeID: "promo_partner", StripeSubscriptionID: pgtype.Text{String: "sub_original", Valid: true}, RedeemedAt: pgtype.Timestamptz{Time: anchor, Valid: true}}))
	_, err = q.DeactivatePaygBillingMetadata(ctx, usagerepo.DeactivatePaygBillingMetadataParams{OrganizationID: "org_partner_anchor", StripeCustomerID: pgtype.Text{String: "cus_partner", Valid: true}, StripeSubscriptionID: pgtype.Text{String: "sub_original", Valid: true}})
	require.NoError(t, err)
	_, err = svc.SetStripeSubscription(ctx, &gen.SetStripeSubscriptionPayload{OrganizationID: "org_partner_anchor", StripeSubscriptionID: "sub_replacement"})
	require.Error(t, err)
	metadata, err := q.GetBillingMetadata(ctx, "org_partner_anchor")
	require.NoError(t, err)
	require.False(t, metadata.StripeSubscriptionID.Valid)
	require.True(t, metadata.StripeBillingCycleAnchor.Time.Equal(anchor))
}
