package admin

import (
	"context"
	"time"

	gen "github.com/speakeasy-api/gram/server/gen/admin"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/hooksrollout"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/plugins"
)

// adminSourceLegacyFlag is how the admin API names hooksrollout.SourceUnset:
// with no pin to apply, the plugin publisher falls back to the hooks-rollout
// PostHog flag.
const adminSourceLegacyFlag gen.AdminHooksRolloutSource = "legacy_flag"

func (s *Service) GetHooksRollout(ctx context.Context, _ *gen.GetHooksRolloutPayload) (*gen.AdminHooksRollout, error) {
	return s.hooksRolloutResult(ctx)
}

func (s *Service) SetHooksRolloutDefault(ctx context.Context, payload *gen.SetHooksRolloutDefaultPayload) (*gen.AdminHooksRollout, error) {
	setBy, err := hooksRolloutOperator(ctx)
	if err != nil {
		return nil, err
	}
	if err := s.validateHooksRolloutVersion(ctx, payload.Version); err != nil {
		return nil, err
	}
	if err := hooksrollout.NewStore(s.db).SetDefault(ctx, payload.Version, setBy); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "set default hooks rollout pin").LogError(ctx, s.logger)
	}
	s.logger.InfoContext(ctx, "set default hooks rollout pin",
		attr.SlogHooksRolloutVersion(payload.Version), attr.SlogAuthUserEmail(setBy))
	return s.hooksRolloutResult(ctx)
}

func (s *Service) GetOrganizationHooksRollout(ctx context.Context, payload *gen.GetOrganizationHooksRolloutPayload) (*gen.AdminOrganizationHooksRollout, error) {
	organization, err := s.canonicalAdminOrganization(ctx, payload.OrganizationID)
	if err != nil {
		return nil, err
	}
	return s.organizationHooksRolloutResult(ctx, organization.ID, organization.Slug)
}

func (s *Service) SetOrganizationHooksRollout(ctx context.Context, payload *gen.SetOrganizationHooksRolloutPayload) (*gen.AdminOrganizationHooksRollout, error) {
	setBy, err := hooksRolloutOperator(ctx)
	if err != nil {
		return nil, err
	}
	organization, err := s.canonicalAdminOrganization(ctx, payload.OrganizationID)
	if err != nil {
		return nil, err
	}
	if err := s.validateHooksRolloutVersion(ctx, payload.Version); err != nil {
		return nil, err
	}
	if err := hooksrollout.NewStore(s.db).SetOrganizationOverride(ctx, organization.ID, payload.Version, setBy); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "set organization hooks rollout pin").LogError(ctx, s.logger)
	}
	s.logger.InfoContext(ctx, "set organization hooks rollout pin",
		attr.SlogOrganizationID(organization.ID), attr.SlogHooksRolloutVersion(payload.Version), attr.SlogAuthUserEmail(setBy))
	return s.organizationHooksRolloutResult(ctx, organization.ID, organization.Slug)
}

func (s *Service) ClearOrganizationHooksRollout(ctx context.Context, payload *gen.ClearOrganizationHooksRolloutPayload) (*gen.AdminOrganizationHooksRollout, error) {
	setBy, err := hooksRolloutOperator(ctx)
	if err != nil {
		return nil, err
	}
	organization, err := s.canonicalAdminOrganization(ctx, payload.OrganizationID)
	if err != nil {
		return nil, err
	}
	if err := hooksrollout.NewStore(s.db).ClearOrganizationOverride(ctx, organization.ID, setBy); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "clear organization hooks rollout pin").LogError(ctx, s.logger)
	}
	s.logger.InfoContext(ctx, "cleared organization hooks rollout pin",
		attr.SlogOrganizationID(organization.ID), attr.SlogAuthUserEmail(setBy))
	return s.organizationHooksRolloutResult(ctx, organization.ID, organization.Slug)
}

// hooksRolloutOperator names the staff operator recorded against a pin change.
// Every admin session carries an email, so a missing one is refused rather
// than recorded anonymously.
func hooksRolloutOperator(ctx context.Context) (string, error) {
	_, _, email := adminActor(ctx)
	if email == nil || *email == "" {
		return "", oops.E(oops.CodeUnauthorized, nil, "admin session has no operator email")
	}
	return *email, nil
}

// validateHooksRolloutVersion refuses a pin above the version this build
// publishes: such a pin would clear future generator bumps before anyone
// decided to roll them out.
func (s *Service) validateHooksRolloutVersion(ctx context.Context, version int) error {
	current, err := plugins.CurrentHooksGeneratorVersion()
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "read hooks generator version").LogError(ctx, s.logger)
	}
	if version > current {
		return oops.E(oops.CodeBadRequest, nil, "version %d is above the current hooks generator version %d", version, current).LogWarn(ctx, s.logger)
	}
	return nil
}

func (s *Service) hooksRolloutResult(ctx context.Context) (*gen.AdminHooksRollout, error) {
	current, err := plugins.CurrentHooksGeneratorVersion()
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "read hooks generator version").LogError(ctx, s.logger)
	}
	store := hooksrollout.NewStore(s.db)
	defaultPin, err := store.DefaultPin(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "read default hooks rollout pin").LogError(ctx, s.logger)
	}
	overrides, err := store.ListOverrides(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list hooks rollout overrides").LogError(ctx, s.logger)
	}
	changes, err := store.ListRecentChanges(ctx, hooksrollout.RecentChangesLimit)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list hooks rollout changes").LogError(ctx, s.logger)
	}

	overrideViews := make([]*gen.AdminHooksRolloutOverride, 0, len(overrides))
	for _, override := range overrides {
		overrideViews = append(overrideViews, &gen.AdminHooksRolloutOverride{
			OrganizationID:   override.OrganizationID,
			OrganizationName: override.OrganizationName,
			OrganizationSlug: override.OrganizationSlug,
			Pin:              hooksRolloutPinView(&override.Pin),
		})
	}
	changeViews := make([]*gen.AdminHooksRolloutChange, 0, len(changes))
	for _, change := range changes {
		var organizationID, organizationSlug *string
		if change.OrganizationID != "" {
			organizationID = new(change.OrganizationID)
			organizationSlug = new(change.OrganizationSlug)
		}
		changeViews = append(changeViews, &gen.AdminHooksRolloutChange{
			OrganizationID:   organizationID,
			OrganizationSlug: organizationSlug,
			Version:          change.Version,
			SetBy:            change.SetBy,
			SetAt:            change.SetAt.UTC().Format(time.RFC3339),
		})
	}

	return &gen.AdminHooksRollout{
		CurrentVersion:          current,
		DefaultPin:              hooksRolloutPinView(defaultPin),
		CanaryOrganizationSlugs: hooksrollout.CanaryOrganizationSlugs(),
		Overrides:               overrideViews,
		RecentChanges:           changeViews,
	}, nil
}

func (s *Service) organizationHooksRolloutResult(ctx context.Context, organizationID, organizationSlug string) (*gen.AdminOrganizationHooksRollout, error) {
	current, err := plugins.CurrentHooksGeneratorVersion()
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "read hooks generator version").LogError(ctx, s.logger)
	}
	pins, err := hooksrollout.NewStore(s.db).OrganizationPins(ctx, organizationID)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "read organization hooks rollout pins").LogError(ctx, s.logger)
	}

	result := &gen.AdminOrganizationHooksRollout{
		OrganizationID:   organizationID,
		CurrentVersion:   current,
		Override:         hooksRolloutPinView(pins.Override),
		DefaultPin:       hooksRolloutPinView(pins.Default),
		Source:           adminSourceLegacyFlag,
		EffectiveVersion: nil,
		Eligible:         nil,
	}
	if hooksrollout.IsCanary(organizationSlug) {
		result.Source = gen.AdminHooksRolloutSource(hooksrollout.SourceCanary)
		result.Eligible = new(true)
		return result, nil
	}
	if pin, source := pins.Effective(); pin != nil {
		result.Source = gen.AdminHooksRolloutSource(source)
		result.EffectiveVersion = new(pin.Version)
		result.Eligible = new(pin.Version >= current)
	}
	return result, nil
}

func hooksRolloutPinView(pin *hooksrollout.Pin) *gen.AdminHooksRolloutPin {
	if pin == nil {
		return nil
	}
	return &gen.AdminHooksRolloutPin{
		Version: pin.Version,
		SetBy:   pin.SetBy,
		SetAt:   pin.SetAt.UTC().Format(time.RFC3339),
	}
}
