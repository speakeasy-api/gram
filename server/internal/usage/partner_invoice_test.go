package usage

import (
	"github.com/jackc/pgx/v5/pgtype"
	stripeclient "github.com/speakeasy-api/gram/server/internal/thirdparty/stripe"
	"github.com/speakeasy-api/gram/server/internal/usage/repo"
	"github.com/stretchr/testify/require"
	"net/http"
	"testing"
	"time"
)

func TestPartnerInvoicePreservesExactMonthlyPeriodsAcrossLeapFebruary(t *testing.T) {
	t.Parallel()
	service, db := newStripeWebhookService(t, "customer_placeholder", nil)
	anchor := time.Date(2028, time.January, 31, 13, 17, 0, 0, time.UTC)
	configurePaygInvoiceIdentity(t, service, "subscription_partner", anchor)
	require.NoError(t, repo.New(db).InsertOrganizationPromotionApplicationFixture(t.Context(), repo.InsertOrganizationPromotionApplicationFixtureParams{OrganizationID: pgtype.Text{String: stripeWebhookOrganizationID, Valid: true}, StripePromotionCodeID: "promo_partner", StripeSubscriptionID: pgtype.Text{String: "subscription_partner", Valid: true}, RedeemedAt: finiteTimestamptz(anchor)}))
	invoice := &stripeclient.InvoiceState{ID: "invoice_partner_first", CustomerID: "customer_placeholder", SubscriptionID: "subscription_partner", Currency: "usd", BillingReason: "subscription_cycle", Status: "draft", ServicePeriodStart: anchor, ServicePeriodEnd: time.Date(2028, time.February, 29, 13, 17, 0, 0, time.UTC), FinalizedAt: time.Time{}, AmountRemaining: 0}
	configureInvoiceWebhook(t, service, "event_partner_first", invoice)
	require.Equal(t, http.StatusOK, serveStripeWebhook(service, "first").Code)
	invoice.ID = "invoice_partner_second"
	invoice.ServicePeriodStart = invoice.ServicePeriodEnd
	invoice.ServicePeriodEnd = time.Date(2028, time.March, 31, 13, 17, 0, 0, time.UTC)
	configureInvoiceWebhook(t, service, "event_partner_second", invoice)
	require.Equal(t, http.StatusOK, serveStripeWebhook(service, "second").Code)
	rows := stripeInvoices(t, db)
	require.Len(t, rows, 2)
	require.True(t, rows[0].ServicePeriodStart.Time.Equal(anchor))
	require.True(t, rows[1].ServicePeriodEnd.Time.Equal(invoice.ServicePeriodEnd))
}

func TestPartnerInvoiceRejectsChangedBillingTime(t *testing.T) {
	t.Parallel()
	service, db := newStripeWebhookService(t, "customer_placeholder", nil)
	anchor := time.Date(2028, time.January, 31, 13, 17, 0, 0, time.UTC)
	configurePaygInvoiceIdentity(t, service, "subscription_partner", anchor)
	require.NoError(t, repo.New(db).InsertOrganizationPromotionApplicationFixture(t.Context(), repo.InsertOrganizationPromotionApplicationFixtureParams{OrganizationID: pgtype.Text{String: stripeWebhookOrganizationID, Valid: true}, StripePromotionCodeID: "promo_partner", StripeSubscriptionID: pgtype.Text{String: "subscription_partner", Valid: true}, RedeemedAt: finiteTimestamptz(anchor)}))
	invoice := &stripeclient.InvoiceState{ID: "invoice_partner_changed", CustomerID: "customer_placeholder", SubscriptionID: "subscription_partner", Currency: "usd", BillingReason: "subscription_cycle", Status: "draft", ServicePeriodStart: anchor.Add(time.Hour), ServicePeriodEnd: time.Date(2028, time.February, 29, 14, 17, 0, 0, time.UTC), FinalizedAt: time.Time{}, AmountRemaining: 0}
	configureInvoiceWebhook(t, service, "event_partner_changed", invoice)
	require.Equal(t, http.StatusInternalServerError, serveStripeWebhook(service, "changed").Code)
	require.Empty(t, stripeInvoices(t, db))
}
