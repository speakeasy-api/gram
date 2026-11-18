package usage

import (
	"fmt"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	httpusage "github.com/speakeasy-api/gram/server/gen/http/usage/server"
	gen "github.com/speakeasy-api/gram/server/gen/usage"
	stripeclient "github.com/speakeasy-api/gram/server/internal/thirdparty/stripe"
	"github.com/speakeasy-api/gram/server/internal/usage/repo"
	"github.com/stretchr/testify/require"
	goahttp "goa.design/goa/v3/http"
)

func TestCreateStripeCheckoutLightFormReplay(t *testing.T) {
	t.Parallel()
	testCreateStripeCheckoutFormReplay(t, "light", true, false)
}

func TestCreateStripeCheckoutDarkFormReplay(t *testing.T) {
	t.Parallel()
	testCreateStripeCheckoutFormReplay(t, "dark", false, true)
}

func testCreateStripeCheckoutFormReplay(t *testing.T, theme string, light, dark bool) {
	t.Helper()
	ti := newStripeCheckoutTestInstance(t)
	ti.service.now = func() time.Time { return time.Date(2026, time.August, 14, 12, 0, 0, 0, time.UTC) }
	first, err := ti.service.CreateStripeCheckout(ti.adminContext(t), &gen.CreateStripeCheckoutPayload{Theme: theme})
	require.NoError(t, err)
	second, err := ti.service.CreateStripeCheckout(ti.adminContext(t), &gen.CreateStripeCheckoutPayload{})
	require.NoError(t, err)
	require.Equal(t, first, second)
	_, _, inputs := ti.stripe.snapshot()
	require.Len(t, inputs, 2)
	require.Equal(t, inputs[0], inputs[1])
	require.True(t, inputs[0].EmbeddedForm)
	require.Equal(t, light, inputs[0].EmbeddedBrandingV1)
	require.Equal(t, dark, inputs[0].EmbeddedBrandingDarkV1)
	require.Equal(t, "https://app.example.test/"+ti.orgSlug+"/billing?session_id={CHECKOUT_SESSION_ID}", inputs[0].ReturnURL)
}

func TestCreateStripeCheckoutRecoversHistoricalLightEmbedded(t *testing.T) {
	t.Parallel()
	testCreateStripeCheckoutHistoricalReceipt(t, "embedded:branding-v1:none", true, true, false)
}

func TestCreateStripeCheckoutRecoversHistoricalDarkEmbedded(t *testing.T) {
	t.Parallel()
	testCreateStripeCheckoutHistoricalReceipt(t, "embedded:branding-dark-v1:none", true, false, true)
}

func TestCreateStripeCheckoutRecoversHistoricalUnbrandedEmbedded(t *testing.T) {
	t.Parallel()
	testCreateStripeCheckoutHistoricalReceipt(t, "embedded:none", true, false, false)
}

func TestCreateStripeCheckoutRecoversHistoricalHosted(t *testing.T) {
	t.Parallel()
	testCreateStripeCheckoutHistoricalReceipt(t, "none", false, false, false)
}

func testCreateStripeCheckoutHistoricalReceipt(t *testing.T, suffix string, embedded, light, dark bool) {
	t.Helper()
	ti := newStripeCheckoutTestInstance(t)
	now := time.Date(2026, time.August, 14, 12, 0, 0, 0, time.UTC)
	ti.service.now = func() time.Time { return now }
	// Fixed persisted pre-form receipts, independent of the current intent constructor.
	anchor := time.Date(2026, time.August, 15, 0, 0, 0, 0, time.UTC)
	expires := time.Date(2026, time.August, 14, 23, 55, 0, 0, time.UTC)
	key := fmt.Sprintf("checkout-session:%s:1786752000:1786751700:%s", ti.orgID, suffix)
	queries := repo.New(ti.db)
	_, err := queries.StoreStripeCustomer(t.Context(), repo.StoreStripeCustomerParams{
		OrganizationID: ti.orgID, StripeCustomerID: pgtype.Text{String: "cus_test", Valid: true},
	})
	require.NoError(t, err)
	_, err = queries.PrepareStripeCheckoutIntent(t.Context(), repo.PrepareStripeCheckoutIntentParams{
		OrganizationID: ti.orgID, StripeCustomerID: "cus_test", PreparedAt: finiteTimestamptz(now), TrialFingerprint: "none",
		StripeCheckoutIdempotencyKey: key, StripeCheckoutBillingCycleAnchor: finiteTimestamptz(anchor),
		StripeCheckoutExpiresAt: finiteTimestamptz(expires),
		StripeCheckoutTrialEnd:  pgtype.Timestamptz{}, ReplaceLifecycleIntentKey: pgtype.Text{}, ReplaceExpiredSessionID: pgtype.Text{},
	})
	require.NoError(t, err)
	expected := stripeclient.CreateCheckoutSessionInput{
		CustomerID: "cus_test", OrganizationID: ti.orgID, OrganizationSlug: ti.orgSlug,
		IdempotencyKey: key, BillingCycleAnchor: anchor, ExpiresAt: expires,
		EmbeddedBrandingV1: light, EmbeddedBrandingDarkV1: dark,
		EmbeddedForm: false, PromotionCodeID: "", TrialEnd: nil, ReturnURL: "", SuccessURL: "", CancelURL: "",
	}
	if embedded {
		expected.ReturnURL = "https://app.example.test/" + ti.orgSlug + "/billing?session_id={CHECKOUT_SESSION_ID}"
	} else {
		expected.SuccessURL = "https://app.example.test/" + ti.orgSlug + "/billing"
		expected.CancelURL = "https://app.example.test/" + ti.orgSlug + "/billing"
	}
	// Stripe accepted the original request, but its session ID was not persisted.
	original, err := ti.stripe.CreateCheckoutSession(t.Context(), expected)
	require.NoError(t, err)
	checkout, err := ti.service.CreateStripeCheckout(ti.adminContext(t), &gen.CreateStripeCheckoutPayload{Theme: "dark"})
	require.NoError(t, err)
	require.NotEmpty(t, checkout.ClientSecret)
	_, _, inputs := ti.stripe.snapshot()
	require.Len(t, inputs, 3)
	require.Equal(t, expected, inputs[1], "recovery must replay the historical Stripe input exactly")
	require.Equal(t, []string{original.ID}, ti.stripe.expiredCheckoutIDs)
	require.NotEqual(t, key, inputs[2].IdempotencyKey)
	replacement := expected
	replacement.IdempotencyKey = fmt.Sprintf("checkout-session:%s:1786752000:1786751940:form-prototype:branding-dark-v1:return-aHR0cHM6Ly9hcHAuZXhhbXBsZS50ZXN0:none", ti.orgID)
	replacement.ExpiresAt = time.Date(2026, time.August, 14, 23, 59, 0, 0, time.UTC)
	replacement.ReturnURL = "https://app.example.test/" + ti.orgSlug + "/billing?session_id={CHECKOUT_SESSION_ID}"
	replacement.SuccessURL = ""
	replacement.CancelURL = ""
	replacement.EmbeddedForm = true
	replacement.EmbeddedBrandingV1 = false
	replacement.EmbeddedBrandingDarkV1 = true
	require.Equal(t, replacement, inputs[2])
	replay, err := ti.service.CreateStripeCheckout(ti.adminContext(t), &gen.CreateStripeCheckoutPayload{Theme: "light"})
	require.NoError(t, err)
	require.Equal(t, checkout, replay)
	_, _, inputs = ti.stripe.snapshot()
	require.Len(t, inputs, 4)
	require.Equal(t, replacement, inputs[3])
	require.Equal(t, []string{original.ID}, ti.stripe.expiredCheckoutIDs)
}

func TestCheckoutThemePayloadOldBodylessRequest(t *testing.T) {
	t.Parallel()
	testCheckoutThemePayload(t, "", "light", false)
}

func TestCheckoutThemePayloadExplicitLight(t *testing.T) {
	t.Parallel()
	testCheckoutThemePayload(t, "?theme=light", "light", false)
}

func TestCheckoutThemePayloadDark(t *testing.T) {
	t.Parallel()
	testCheckoutThemePayload(t, "?theme=dark", "dark", false)
}

func TestCheckoutThemePayloadInvalid(t *testing.T) {
	t.Parallel()
	testCheckoutThemePayload(t, "?theme=system", "", true)
}

func testCheckoutThemePayload(t *testing.T, tcQuery string, tcWant string, tcInvalid bool) {
	t.Helper()
	decode := httpusage.DecodeCreateStripeCheckoutRequest(goahttp.NewMuxer(), goahttp.RequestDecoder)
	payload, err := decode(httptest.NewRequest("POST", "/rpc/usage.createStripeCheckout"+tcQuery, nil))
	if tcInvalid {
		require.Error(t, err)
		return
	}
	require.NoError(t, err)
	require.Equal(t, tcWant, payload.Theme)
}
