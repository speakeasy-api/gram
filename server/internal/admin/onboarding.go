package admin

import (
	"context"
	"encoding/json"
	"fmt"

	gen "github.com/speakeasy-api/gram/server/gen/admin"
	adminserver "github.com/speakeasy-api/gram/server/gen/http/admin/server"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/organizations"
)

// A value field receives explicit null, unlike Goa's optional string pointer.
type onboardingPresetInput string

func (p *onboardingPresetInput) UnmarshalJSON(data []byte) error {
	var value string
	if err := json.Unmarshal(data, &value); err != nil {
		return fmt.Errorf("decode onboarding preset: %w", err)
	}
	if value != "gateway" && value != "security" {
		return fmt.Errorf("preset must be gateway or security; omit it to preserve the saved preset")
	}
	*p = onboardingPresetInput(value)
	return nil
}

type onboardingRequestBody struct {
	adminserver.SetOrganizationOnboardingRequestBody
	Preset onboardingPresetInput `json:"preset"`
}

func (s *Service) GetOrganizationOnboarding(ctx context.Context, payload *gen.GetOrganizationOnboardingPayload) (*gen.AdminOnboardingConfiguration, error) {
	if _, ok := contextvalues.GetAdminAuthContext(ctx); !ok {
		return nil, oops.C(oops.CodeUnauthorized)
	}
	id, err := s.canonicalAdminOrganizationForRequest(ctx, payload.OrganizationID)
	if err != nil {
		return nil, err
	}
	result, err := organizations.LoadOnboardingConfiguration(ctx, s.db, id)
	if err != nil {
		return nil, fmt.Errorf("load organization onboarding: %w", err)
	}
	return result, nil
}

func (s *Service) SetOrganizationOnboarding(ctx context.Context, payload *gen.SetOrganizationOnboardingPayload) (*gen.AdminOnboardingConfiguration, error) {
	if _, ok := contextvalues.GetAdminAuthContext(ctx); !ok {
		return nil, oops.C(oops.CodeUnauthorized)
	}
	id, err := s.canonicalAdminOrganizationForRequest(ctx, payload.OrganizationID)
	if err != nil {
		return nil, err
	}
	actor, name, _ := adminActor(ctx)
	result, err := organizations.SaveOnboardingConfiguration(ctx, s.db, s.audit, id, payload.VisibleTaskKeys, payload.Preset, actor, name)
	if err != nil {
		return nil, fmt.Errorf("save organization onboarding: %w", err)
	}
	return result, nil
}
