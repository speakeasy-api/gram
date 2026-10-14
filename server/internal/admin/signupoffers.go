package admin

import (
	"context"
	"errors"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgerrcode"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	gen "github.com/speakeasy-api/gram/server/gen/admin"
	"github.com/speakeasy-api/gram/server/internal/admin/repo"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

var signupPromotionID = regexp.MustCompile(`^promo_[A-Za-z0-9_]+$`)

func validateSignupOffer(name, promotionID, flow string) error {
	if strings.TrimSpace(name) == "" || utf8.RuneCountInString(name) > 200 || len(promotionID) > 255 || !signupPromotionID.MatchString(promotionID) || flow != "tailscale_partner_gateway" {
		return oops.E(oops.CodeBadRequest, errors.New("invalid signup offer"), "Provide a name, exact Stripe promotion code ID and supported signup flow.")
	}
	return nil
}

func signupOfferView(offer repo.SignupOffer) *gen.AdminSignupOffer {
	return &gen.AdminSignupOffer{ID: offer.ID.String(), Name: offer.Name, StripePromotionCodeID: offer.StripePromotionCodeID, SignupFlow: offer.SignupFlow, Enabled: offer.Enabled, CreatedAt: offer.CreatedAt.Time.Format(time.RFC3339Nano), UpdatedAt: offer.UpdatedAt.Time.Format(time.RFC3339Nano)}
}

func (s *Service) signupOfferError(ctx context.Context, err error) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return oops.C(oops.CodeNotFound)
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == pgerrcode.UniqueViolation {
		return oops.E(oops.CodeConflict, err, "This Stripe promotion code ID already has a signup offer.")
	}
	return oops.E(oops.CodeUnexpected, err, "manage signup offer").LogError(ctx, s.logger)
}

func (s *Service) ListSignupOffers(ctx context.Context, _ *gen.ListSignupOffersPayload) (*gen.ListSignupOffersResult, error) {
	if _, ok := contextvalues.GetAdminAuthContext(ctx); !ok {
		return nil, oops.C(oops.CodeUnauthorized)
	}
	offers, err := repo.New(s.db).ListSignupOffers(ctx)
	if err != nil {
		return nil, s.signupOfferError(ctx, err)
	}
	result := &gen.ListSignupOffersResult{Offers: make([]*gen.AdminSignupOffer, 0, len(offers))}
	for _, offer := range offers {
		result.Offers = append(result.Offers, signupOfferView(offer))
	}
	return result, nil
}

func (s *Service) CreateSignupOffer(ctx context.Context, p *gen.CreateSignupOfferPayload) (*gen.AdminSignupOffer, error) {
	if _, ok := contextvalues.GetAdminAuthContext(ctx); !ok {
		return nil, oops.C(oops.CodeUnauthorized)
	}
	if err := validateSignupOffer(p.Name, p.StripePromotionCodeID, p.SignupFlow); err != nil {
		return nil, err
	}
	offer, err := repo.New(s.db).CreateSignupOffer(ctx, repo.CreateSignupOfferParams{Name: strings.TrimSpace(p.Name), StripePromotionCodeID: p.StripePromotionCodeID, SignupFlow: p.SignupFlow})
	if err != nil {
		return nil, s.signupOfferError(ctx, err)
	}
	return signupOfferView(offer), nil
}

func (s *Service) UpdateSignupOffer(ctx context.Context, p *gen.UpdateSignupOfferPayload) (*gen.AdminSignupOffer, error) {
	if _, ok := contextvalues.GetAdminAuthContext(ctx); !ok {
		return nil, oops.C(oops.CodeUnauthorized)
	}
	if err := validateSignupOffer(p.Name, p.StripePromotionCodeID, p.SignupFlow); err != nil {
		return nil, err
	}
	id, err := uuid.Parse(p.ID)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid signup offer id")
	}
	offer, err := repo.New(s.db).UpdateSignupOffer(ctx, repo.UpdateSignupOfferParams{ID: id, Name: strings.TrimSpace(p.Name), StripePromotionCodeID: p.StripePromotionCodeID, SignupFlow: p.SignupFlow, Enabled: p.Enabled})
	if err != nil {
		return nil, s.signupOfferError(ctx, err)
	}
	return signupOfferView(offer), nil
}
