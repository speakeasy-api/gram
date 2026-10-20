package auth

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"

	"github.com/jackc/pgx/v5"
	gen "github.com/speakeasy-api/gram/server/gen/auth"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/oops"
	orgRepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

type PartnerPromotionCodeResolver interface {
	ResolveGatewayPromotionCode(context.Context, string) (string, error)
}

func partnerSignupUnavailable() error {
	return oops.E(oops.CodeInvalid, errors.New("partner signup unavailable"), "This partner signup code is invalid or currently unavailable. Please retry or contact support.")
}

func (s *Service) resolvePartnerSignup(ctx context.Context, code string) (string, error) {
	code = strings.ToLower(strings.TrimSpace(code))
	if code == "" {
		return "", nil
	}
	if len(code) > 80 || s.partnerPromotionCodeResolver == nil {
		return "", partnerSignupUnavailable()
	}
	promotionCodeID, err := s.partnerPromotionCodeResolver.ResolveGatewayPromotionCode(ctx, code)
	if err != nil || promotionCodeID == "" {
		s.logger.ErrorContext(ctx, "partner signup promotion resolution failed", attr.SlogError(err))
		return "", partnerSignupUnavailable()
	}
	if _, err := s.orgRepo.GetEnabledSignupOffer(ctx, promotionCodeID); err != nil {
		return "", partnerSignupUnavailable()
	}
	return promotionCodeID, nil
}

func (s *Service) persistPartnerSignupTx(ctx context.Context, tx pgx.Tx, orgID, userID, email, promotionCodeID string) error {
	if _, err := orgRepo.New(tx).GetEnabledSignupOffer(ctx, promotionCodeID); err != nil {
		return partnerSignupUnavailable()
	}
	count, err := orgRepo.New(tx).AttributePartnerSignup(ctx, orgRepo.AttributePartnerSignupParams{
		OrganizationID: conv.ToPGText(orgID), StripePromotionCodeID: promotionCodeID,
	})
	if err != nil {
		return fmt.Errorf("record partner signup: %w", err)
	}
	if count != 1 {
		return errors.New("organization already has partner attribution")
	}
	org, err := orgRepo.New(tx).GetOrganizationMetadata(ctx, orgID)
	if err != nil {
		return fmt.Errorf("load partner signup audit subject: %w", err)
	}
	if err := s.auditLogger.LogOrganizationPartnerSignupAttributed(ctx, tx, audit.LogOrganizationPartnerSignupAttributedEvent{
		OrganizationID: orgID, Actor: urn.NewPrincipal(urn.PrincipalTypeUser, userID), ActorDisplayName: conv.PtrEmpty(email), OrganizationName: org.Name, OrganizationSlug: org.Slug,
		Attribution: audit.OrganizationPartnerSignupSnapshot{StripePromotionCodeID: promotionCodeID},
	}); err != nil {
		return fmt.Errorf("audit partner signup attribution: %w", err)
	}
	return nil
}

// hasPartnerSignup recognizes both pending and redeemed applications. A lookup
// failure must not turn a promotion signup into an ordinary trial signup.
func (s *Service) hasPartnerSignup(ctx context.Context, orgID string) (bool, error) {
	_, err := s.orgRepo.GetOrganizationPromotionApplication(ctx, orgID)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("load organization promotion application: %w", err)
	}
	return true, nil
}

func (s *Service) notifySignupTrialStarted(ctx context.Context, orgID string, partnerSignup bool) error {
	if partnerSignup {
		return nil
	}
	if err := s.trialNotifier.TrialStarted(ctx, orgID); err != nil {
		return fmt.Errorf("notify signup trial started: %w", err)
	}
	return nil
}

func (s *Service) signupDestination(ctx context.Context, payload *gen.CallbackPayload, org orgRepo.OrganizationMetadatum, partnerSignup bool) string {
	if partnerSignup {
		return strings.TrimRight(s.platformHostURL(ctx, s.cfg.SignInRedirectURL), "/") + "/" + org.Slug + "/billing"
	}
	return s.callbackRedirectURL(ctx, payload)
}

func (s *Service) redirectPartnerSignupError(ctx context.Context, payload *gen.CallbackPayload, err error, intent *signupIntent) (*gen.CallbackResult, error) {
	result, resultErr := s.redirectSignupError(ctx, payload, err)
	if resultErr != nil || result == nil || intent == nil || intent.PartnerCode == "" {
		return result, resultErr
	}
	location, parseErr := url.Parse(result.Location)
	if parseErr != nil {
		return result, resultErr
	}
	query := location.Query()
	query.Set("partner_code", intent.PartnerCode)
	if intent.OrgName != "" {
		query.Set("org_name", intent.OrgName)
	}
	location.RawQuery = query.Encode()
	result.Location = location.String()
	return result, resultErr
}

func (s *Service) signupRegisterDestination(ctx context.Context, payload *gen.CallbackPayload, intent *signupIntent) string {
	if intent == nil || intent.PartnerCode == "" {
		return s.callbackRedirectURL(ctx, payload)
	}
	return strings.TrimRight(s.platformHostURL(ctx, s.cfg.SignInRedirectURL), "/") + "/register?" + url.Values{"partner_code": {intent.PartnerCode}}.Encode()
}
