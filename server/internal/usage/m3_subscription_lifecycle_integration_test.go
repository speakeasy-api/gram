package usage_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"
	goahttp "goa.design/goa/v3/http"

	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/authz"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/openrouter"
	openrouterrepo "github.com/speakeasy-api/gram/server/internal/thirdparty/openrouter/repo"
	stripeclient "github.com/speakeasy-api/gram/server/internal/thirdparty/stripe"
	"github.com/speakeasy-api/gram/server/internal/usage"
	usagerepo "github.com/speakeasy-api/gram/server/internal/usage/repo"
)

const (
	m3OrganizationID = "org_billing_validation"
	m3CustomerID     = "customer_placeholder"
)

type m3StripeWebhookClient struct {
	event    stripeclient.WebhookEvent
	checkout stripeclient.CheckoutSessionState
}

func (*m3StripeWebhookClient) CreateCustomer(context.Context, stripeclient.CreateCustomerInput) (*stripeclient.Customer, error) {
	return nil, errors.New("not implemented")
}

func (*m3StripeWebhookClient) CreateCheckoutSession(context.Context, stripeclient.CreateCheckoutSessionInput) (*stripeclient.CheckoutSession, error) {
	return nil, errors.New("not implemented")
}

func (c *m3StripeWebhookClient) GetCheckoutSession(context.Context, string) (*stripeclient.CheckoutSessionState, error) {
	state := c.checkout
	return &state, nil
}

func (*m3StripeWebhookClient) GetSubscription(context.Context, string) (*stripeclient.SubscriptionState, error) {
	return nil, errors.New("not implemented")
}

func (*m3StripeWebhookClient) SetSubscriptionCancelAtPeriodEnd(context.Context, stripeclient.SetSubscriptionCancelAtPeriodEndInput) (*stripeclient.SubscriptionState, error) {
	return nil, errors.New("not implemented")
}

func (*m3StripeWebhookClient) CreatePortalSession(context.Context, stripeclient.CreatePortalSessionInput) (*stripeclient.PortalSession, error) {
	return nil, errors.New("not implemented")
}

func (*m3StripeWebhookClient) CreateMeterEvent(context.Context, stripeclient.CreateMeterEventInput) error {
	return errors.New("not implemented")
}

func (*m3StripeWebhookClient) GetMeterEventSummary(context.Context, stripeclient.GetMeterEventSummaryInput) (float64, error) {
	return 0, errors.New("not implemented")
}

func (*m3StripeWebhookClient) GetInvoice(context.Context, string) (*stripeclient.InvoiceState, error) {
	return nil, errors.New("not implemented")
}

func (*m3StripeWebhookClient) CreateInvoiceItem(context.Context, stripeclient.CreateInvoiceItemInput) (*stripeclient.InvoiceItem, error) {
	return nil, errors.New("not implemented")
}

func (*m3StripeWebhookClient) CreateCreditNote(context.Context, stripeclient.CreateCreditNoteInput) (*stripeclient.CreditNote, error) {
	return nil, errors.New("not implemented")
}

func (*m3StripeWebhookClient) FindInvoiceItem(context.Context, stripeclient.FindInvoiceAllocationInput) (*stripeclient.InvoiceItem, error) {
	return nil, errors.New("not implemented")
}

func (*m3StripeWebhookClient) FindCreditNote(context.Context, stripeclient.FindInvoiceAllocationInput) (*stripeclient.CreditNote, error) {
	return nil, errors.New("not implemented")
}

func (c *m3StripeWebhookClient) VerifyWebhook([]byte, string) (*stripeclient.WebhookEvent, error) {
	event := c.event
	return &event, nil
}

func (*m3StripeWebhookClient) Catalog() stripeclient.Catalog {
	return stripeclient.Catalog{PriceIDTUM: "", MeterIDTUM: "", MeterEventName: "", PortalConfigurationID: ""}
}

func TestM3SubscriptionLossRecheckoutAndStaleReplayLifecycle(t *testing.T) {
	t.Parallel()

	db, err := usage.CloneM3BillingValidationDatabase(t, "m3_subscription_lifecycle")
	require.NoError(t, err)

	require.NoError(t, orgrepo.New(db).CreateOrganizationMetadata(t.Context(), orgrepo.CreateOrganizationMetadataParams{
		ID:   m3OrganizationID,
		Name: "Billing Validation Organization",
		Slug: "billing-validation",
	}))
	require.NoError(t, authz.SeedSystemRoleGrants(t.Context(), db, m3OrganizationID))
	require.NoError(t, usagerepo.New(db).CreateStripeBillingMetadataFixture(t.Context(), usagerepo.CreateStripeBillingMetadataFixtureParams{
		OrganizationID:   m3OrganizationID,
		StripeCustomerID: pgtype.Text{String: m3CustomerID, Valid: true},
	}))

	keys := openrouterrepo.New(db)
	for _, keyType := range openrouter.AllKeyTypes {
		_, err := keys.CreateOpenRouterAPIKey(t.Context(), openrouterrepo.CreateOpenRouterAPIKeyParams{
			OrganizationID: m3OrganizationID,
			KeyType:        string(keyType),
			KeyEncrypted:   pgtype.Text{},
			KeyHash:        "hash_placeholder_" + string(keyType),
			MonthlyCredits: 70,
		})
		require.NoError(t, err)
	}

	stripe := &m3StripeWebhookClient{}
	service, metrics := usage.NewM3StripeWebhookService(t, db, stripe)
	mux := goahttp.NewMuxer()
	usage.Attach(mux, service)

	serve := func(body string) {
		recorder := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodPost, "/rpc/stripe.webhook", strings.NewReader(body))
		request.Header.Set("Stripe-Signature", "signature_placeholder")
		mux.ServeHTTP(recorder, request)
		require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
	}
	checkout := func(eventID, subscriptionID string, anchor time.Time) {
		stripe.event = stripeclient.WebhookEvent{
			ID:             eventID,
			Type:           "checkout.session.completed",
			Created:        anchor.Add(-time.Hour),
			ObjectID:       "checkout_" + eventID,
			CustomerID:     m3CustomerID,
			SubscriptionID: subscriptionID,
		}
		stripe.checkout = stripeclient.CheckoutSessionState{
			ID:                     stripe.event.ObjectID,
			Status:                 "complete",
			CustomerID:             m3CustomerID,
			SubscriptionID:         subscriptionID,
			SubscriptionCustomerID: m3CustomerID,
			SubscriptionStatus:     "active",
			BillingCycleAnchor:     anchor,
		}
		serve(eventID)
	}
	deleted := func(eventID, subscriptionID string) {
		stripe.event = stripeclient.WebhookEvent{
			ID:         eventID,
			Type:       "customer.subscription.deleted",
			Created:    time.Date(2026, time.October, 1, 0, 0, 0, 0, time.UTC),
			ObjectID:   subscriptionID,
			CustomerID: m3CustomerID,
		}
		serve(eventID)
	}
	keyState := func(keyType openrouter.KeyType) openrouterrepo.OpenrouterApiKey {
		key, err := keys.GetOpenRouterAPIKey(t.Context(), openrouterrepo.GetOpenRouterAPIKeyParams{
			OrganizationID: m3OrganizationID,
			KeyType:        string(keyType),
		})
		require.NoError(t, err)
		return key
	}

	firstAnchor := time.Date(2026, time.September, 1, 0, 0, 0, 0, time.UTC)
	checkout("event_initial_checkout", "subscription_initial", firstAnchor)
	deleted("event_subscription_loss", "subscription_initial")
	require.EqualValues(t, 1, metrics.SubscriptionLosses())
	require.True(t, keyState(openrouter.KeyTypeChat).Disabled)
	require.Equal(t, []string{"billing_inactive"}, keyState(openrouter.KeyTypeChat).DisableCauses)
	require.False(t, keyState(openrouter.KeyTypeInternal).Disabled)
	require.EqualValues(t, 70, keyState(openrouter.KeyTypeInternal).MonthlyCredits)

	secondAnchor := time.Date(2026, time.October, 2, 0, 0, 0, 0, time.UTC)
	checkout("event_recheckout", "subscription_replacement", secondAnchor)
	require.False(t, keyState(openrouter.KeyTypeChat).Disabled)
	require.Empty(t, keyState(openrouter.KeyTypeChat).DisableCauses)
	require.False(t, keyState(openrouter.KeyTypeInternal).Disabled)
	require.EqualValues(t, 100, keyState(openrouter.KeyTypeChat).MonthlyCredits)
	require.EqualValues(t, 70, keyState(openrouter.KeyTypeInternal).MonthlyCredits)
	require.Equal(t, "hash_placeholder_chat", keyState(openrouter.KeyTypeChat).KeyHash)
	require.Equal(t, "hash_placeholder_internal", keyState(openrouter.KeyTypeInternal).KeyHash)

	deleted("event_stale_subscription_loss", "subscription_initial")
	serve("exact replay")
	require.EqualValues(t, 1, metrics.SubscriptionLosses(), "stale and replayed deletions do not repeat the metric")

	metadata, err := usagerepo.New(db).GetBillingMetadata(t.Context(), m3OrganizationID)
	require.NoError(t, err)
	require.Equal(t, "subscription_replacement", metadata.StripeSubscriptionID.String)
	require.True(t, metadata.StripeBillingCycleAnchor.Valid)
	require.True(t, secondAnchor.Equal(metadata.StripeBillingCycleAnchor.Time))
	organization, err := orgrepo.New(db).GetOrganizationMetadata(t.Context(), m3OrganizationID)
	require.NoError(t, err)
	require.Equal(t, "payg", organization.GramAccountType)
	require.True(t, organization.Whitelisted)
	require.False(t, keyState(openrouter.KeyTypeChat).Disabled)
	require.Empty(t, keyState(openrouter.KeyTypeChat).DisableCauses)
	require.False(t, keyState(openrouter.KeyTypeInternal).Disabled)

	receipts, err := usagerepo.New(db).CountStripeWebhookReceiptsFixture(t.Context(), m3OrganizationID)
	require.NoError(t, err)
	require.EqualValues(t, 4, receipts)
	activations, err := audittest.AuditLogCountByAction(t.Context(), db, audit.ActionOrganizationPaygActivated)
	require.NoError(t, err)
	require.EqualValues(t, 2, activations)
	deactivations, err := audittest.AuditLogCountByAction(t.Context(), db, audit.ActionOrganizationPaygDeactivated)
	require.NoError(t, err)
	require.EqualValues(t, 1, deactivations)
}
