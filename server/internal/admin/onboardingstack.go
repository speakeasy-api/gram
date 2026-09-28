package admin

import (
	"context"
	"fmt"

	gen "github.com/speakeasy-api/gram/server/gen/admin"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/organizations"
)

func (s *Service) ListOnboardingSteps(ctx context.Context, _ *gen.ListOnboardingStepsPayload) (*gen.AdminOnboardingStepList, error) {
	if _, ok := contextvalues.GetAdminAuthContext(ctx); !ok {
		return nil, oops.C(oops.CodeUnauthorized)
	}
	steps, err := organizations.ListOnboardingSteps(ctx, s.db)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list onboarding steps").LogError(ctx, s.logger)
	}
	return &gen.AdminOnboardingStepList{Steps: steps}, nil
}

func (s *Service) GetOnboardingStackOptions(ctx context.Context, _ *gen.GetOnboardingStackOptionsPayload) (*gen.AdminOnboardingStackOptions, error) {
	if _, ok := contextvalues.GetAdminAuthContext(ctx); !ok {
		return nil, oops.C(oops.CodeUnauthorized)
	}
	options, err := organizations.LoadOnboardingStackOptions(ctx, s.db)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "load onboarding stack options").LogError(ctx, s.logger)
	}
	return options, nil
}

func (s *Service) GetOrganizationOnboardingStack(ctx context.Context, payload *gen.GetOrganizationOnboardingStackPayload) (*gen.AdminOnboardingStack, error) {
	if _, ok := contextvalues.GetAdminAuthContext(ctx); !ok {
		return nil, oops.C(oops.CodeUnauthorized)
	}
	id, err := s.canonicalAdminOrganizationForRequest(ctx, payload.OrganizationID)
	if err != nil {
		return nil, err
	}
	stack, err := organizations.LoadOnboardingStack(ctx, s.db, id)
	if err != nil {
		return nil, fmt.Errorf("load organization onboarding stack: %w", err)
	}
	return stack, nil
}

func (s *Service) SetOrganizationOnboardingStack(ctx context.Context, payload *gen.SetOrganizationOnboardingStackPayload) (*gen.AdminOnboardingStack, error) {
	if _, ok := contextvalues.GetAdminAuthContext(ctx); !ok {
		return nil, oops.C(oops.CodeUnauthorized)
	}
	id, err := s.canonicalAdminOrganizationForRequest(ctx, payload.OrganizationID)
	if err != nil {
		return nil, err
	}
	input := organizations.OnboardingStackInput{Vendors: make([]organizations.OnboardingStackVendorInput, 0, len(payload.Vendors)), MdmVendor: payload.MdmVendor, MdmVendorName: payload.MdmVendorName}
	for _, vendor := range payload.Vendors {
		input.Vendors = append(input.Vendors, organizations.OnboardingStackVendorInput{Vendor: vendor.Vendor, PlanSlug: vendor.PlanSlug})
	}
	actor, name, _ := adminActor(ctx)
	stack, err := organizations.SaveOnboardingStack(ctx, s.db, s.audit, id, input, actor, name)
	if err != nil {
		return nil, fmt.Errorf("save organization onboarding stack: %w", err)
	}
	return stack, nil
}
