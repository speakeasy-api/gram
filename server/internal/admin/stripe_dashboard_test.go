package admin

import (
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/admin"
	"github.com/speakeasy-api/gram/server/internal/conv"
	usagerepo "github.com/speakeasy-api/gram/server/internal/usage/repo"
)

func TestGetOrganization_StripeDashboardURLs(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name             string
		apiKey           string
		customerID       *string
		subscriptionID   *string
		wantCustomer     *string
		wantSubscription *string
	}{
		{
			name:             "live key with customer and subscription",
			apiKey:           "sk_live_placeholder",
			customerID:       new("cus_example"),
			subscriptionID:   new("sub_example"),
			wantCustomer:     new("https://dashboard.stripe.com/customers/cus_example"),
			wantSubscription: new("https://dashboard.stripe.com/subscriptions/sub_example"),
		},
		{
			name:             "test key with customer and subscription",
			apiKey:           "sk_test_placeholder",
			customerID:       new("cus_example"),
			subscriptionID:   new("sub_example"),
			wantCustomer:     new("https://dashboard.stripe.com/test/customers/cus_example"),
			wantSubscription: new("https://dashboard.stripe.com/test/subscriptions/sub_example"),
		},
		{
			name:             "restricted test key",
			apiKey:           "rk_test_placeholder",
			customerID:       new("cus_example"),
			subscriptionID:   nil,
			wantCustomer:     new("https://dashboard.stripe.com/test/customers/cus_example"),
			wantSubscription: nil,
		},
		{
			name:             "customer without subscription",
			apiKey:           "sk_live_placeholder",
			customerID:       new("cus_example"),
			subscriptionID:   nil,
			wantCustomer:     new("https://dashboard.stripe.com/customers/cus_example"),
			wantSubscription: nil,
		},
		{
			name:             "no billing identifiers",
			apiKey:           "sk_live_placeholder",
			customerID:       nil,
			subscriptionID:   nil,
			wantCustomer:     nil,
			wantSubscription: nil,
		},
		{
			name:             "stripe not configured",
			apiKey:           "unset",
			customerID:       new("cus_example"),
			subscriptionID:   new("sub_example"),
			wantCustomer:     nil,
			wantSubscription: nil,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			ctx, svc, conn := newTestAdminService(t)
			svc.SetStripeDashboardMode(tc.apiKey)
			seedOrg(t, ctx, conn, orgFixture{id: "org_stripe_link", name: "Stripe Link Co", slug: "stripe-link-co"})
			if tc.customerID != nil {
				require.NoError(t, usagerepo.New(conn).CreateStripeSubscriptionBillingMetadataFixture(ctx, usagerepo.CreateStripeSubscriptionBillingMetadataFixtureParams{
					OrganizationID:       "org_stripe_link",
					StripeCustomerID:     conv.PtrToPGText(tc.customerID),
					StripeSubscriptionID: conv.PtrToPGText(tc.subscriptionID),
				}))
			}

			org, err := svc.GetOrganization(ctx, &gen.GetOrganizationPayload{IDOrSlug: "org_stripe_link"})
			require.NoError(t, err)
			require.Equal(t, tc.wantCustomer, org.StripeCustomerDashboardURL)
			require.Equal(t, tc.wantSubscription, org.StripeSubscriptionDashboardURL)
		})
	}
}

func TestListOrganizations_StripeDashboardURLs(t *testing.T) {
	t.Parallel()

	ctx, svc, conn := newTestAdminService(t)
	svc.SetStripeDashboardMode("sk_test_placeholder")
	seedOrg(t, ctx, conn, orgFixture{id: "org_stripe_list", name: "Stripe List Co", slug: "stripe-list-co"})
	require.NoError(t, usagerepo.New(conn).CreateStripeSubscriptionBillingMetadataFixture(ctx, usagerepo.CreateStripeSubscriptionBillingMetadataFixtureParams{
		OrganizationID:       "org_stripe_list",
		StripeCustomerID:     conv.ToPGText("cus_list"),
		StripeSubscriptionID: conv.ToPGText("sub_list"),
	}))

	res, err := svc.ListOrganizations(ctx, &gen.ListOrganizationsPayload{Q: new("org_stripe_list")})
	require.NoError(t, err)
	require.Len(t, res.Organizations, 1)
	require.Equal(t, "https://dashboard.stripe.com/test/customers/cus_list", *res.Organizations[0].StripeCustomerDashboardURL)
	require.Equal(t, "https://dashboard.stripe.com/test/subscriptions/sub_list", *res.Organizations[0].StripeSubscriptionDashboardURL)
}

func TestSetStripeDashboardMode(t *testing.T) {
	t.Parallel()

	cases := []struct {
		apiKey string
		want   string
	}{
		{apiKey: "sk_live_placeholder", want: "https://dashboard.stripe.com"},
		{apiKey: "rk_live_placeholder", want: "https://dashboard.stripe.com"},
		{apiKey: " sk_test_placeholder\n", want: "https://dashboard.stripe.com/test"},
		{apiKey: "rk_test_placeholder", want: "https://dashboard.stripe.com/test"},
		{apiKey: "pk_live_placeholder", want: ""},
		{apiKey: "unset", want: ""},
		{apiKey: "", want: ""},
	}
	for _, tc := range cases {
		svc := &Service{}
		svc.SetStripeDashboardMode(tc.apiKey)
		require.Equal(t, tc.want, svc.stripeDashboardBase, tc.apiKey)
	}
}

func TestStripeDashboardURL_EscapesPathSegment(t *testing.T) {
	t.Parallel()

	svc := &Service{stripeDashboardBase: stripeDashboardOrigin}
	link := svc.stripeCustomerDashboardURL(conv.ToPGText("cus/1?a"))
	require.NotNil(t, link)
	require.Equal(t, "https://dashboard.stripe.com/customers/cus%2F1%3Fa", *link)
	require.Nil(t, svc.stripeSubscriptionDashboardURL(pgtype.Text{}))
}
