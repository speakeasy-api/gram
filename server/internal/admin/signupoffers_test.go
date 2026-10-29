package admin

import (
	"strings"
	"testing"

	gen "github.com/speakeasy-api/gram/server/gen/admin"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	"github.com/stretchr/testify/require"
)

func TestSignupOffers(t *testing.T) {
	t.Parallel()
	ctx, service, db := newTestAdminService(t)
	_, err := service.ListSignupOffers(ctx, &gen.ListSignupOffersPayload{})
	require.Error(t, err)
	_, err = service.CreateSignupOffer(ctx, &gen.CreateSignupOfferPayload{})
	require.Error(t, err)
	_, err = service.UpdateSignupOffer(ctx, &gen.UpdateSignupOfferPayload{})
	require.Error(t, err)
	ctx = contextvalues.SetAdminAuthContext(ctx, &contextvalues.AdminAuthContext{Email: "operator@example.test"})
	input := &gen.CreateSignupOfferPayload{Name: "Fixture offer", StripePromotionCodeID: "promo_fixture", SignupFlow: "tailscale_partner_gateway"}
	offer, err := service.CreateSignupOffer(ctx, input)
	require.NoError(t, err)
	require.False(t, offer.Enabled)
	require.NotEmpty(t, offer.CreatedAt)
	require.NotEmpty(t, offer.UpdatedAt)
	_, err = service.CreateSignupOffer(ctx, input)
	require.ErrorContains(t, err, "already has a signup offer")
	_, err = orgrepo.New(db).GetEnabledSignupOffer(ctx, "promo_fixture")
	require.Error(t, err)
	update := &gen.UpdateSignupOfferPayload{ID: offer.ID, Name: offer.Name, StripePromotionCodeID: offer.StripePromotionCodeID, SignupFlow: offer.SignupFlow, Enabled: true}
	enabled, err := service.UpdateSignupOffer(ctx, update)
	require.NoError(t, err)
	require.True(t, enabled.Enabled)
	_, err = orgrepo.New(db).GetEnabledSignupOffer(ctx, "promo_fixture")
	require.NoError(t, err)
	_, err = orgrepo.New(db).GetEnabledSignupOffer(ctx, "promo_FIXTURE")
	require.Error(t, err, "promotion ID matching must be exact")
	_, err = orgrepo.New(db).GetEnabledSignupOffer(ctx, "promo_unmapped")
	require.Error(t, err)
	update.Name = "Renamed offer"
	update.StripePromotionCodeID = "promo_changed"
	update.Enabled = false
	disabled, err := service.UpdateSignupOffer(ctx, update)
	require.NoError(t, err)
	require.False(t, disabled.Enabled)
	list, err := service.ListSignupOffers(ctx, &gen.ListSignupOffersPayload{})
	require.NoError(t, err)
	require.Len(t, list.Offers, 1)
	require.Equal(t, "Renamed offer", list.Offers[0].Name)
	require.Equal(t, "promo_changed", list.Offers[0].StripePromotionCodeID)
	_, err = orgrepo.New(db).GetEnabledSignupOffer(ctx, "promo_changed")
	require.Error(t, err)
	update.SignupFlow = "unsupported"
	_, err = service.UpdateSignupOffer(ctx, update)
	require.Error(t, err)
}

func TestSignupOfferValidation(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ name, promotion, flow string }{
		{" ", "promo_fixture", "tailscale_partner_gateway"},
		{"Fixture", "human-code", "tailscale_partner_gateway"},
		{"Fixture", " promo_fixture", "tailscale_partner_gateway"},
		{"Fixture", "promo_fixture", "other"},
	} {
		require.Error(t, validateSignupOffer(tc.name, tc.promotion, tc.flow))
	}
	require.NoError(t, validateSignupOffer("Fixture", "promo_fixture", "tailscale_partner_gateway"))
}

func TestSignupOfferValidationUnicode200Characters(t *testing.T) {
	t.Parallel()
	require.NoError(t, validateSignupOffer(strings.Repeat("界", 200), "promo_fixture", "tailscale_partner_gateway"))
}

func TestSignupOfferValidationUnicode201Characters(t *testing.T) {
	t.Parallel()
	require.Error(t, validateSignupOffer(strings.Repeat("界", 201), "promo_fixture", "tailscale_partner_gateway"))
}

func TestSignupOfferValidationWhitespaceName(t *testing.T) {
	t.Parallel()
	require.Error(t, validateSignupOffer(" \t\n\u2003", "promo_fixture", "tailscale_partner_gateway"))
}
