package admin

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	gen "github.com/speakeasy-api/gram/server/gen/admin"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/organizations"
)

func parseOnboardingID(value string) (uuid.UUID, error) {
	id, err := uuid.Parse(value)
	if err != nil {
		return uuid.Nil, oops.E(oops.CodeBadRequest, err, "invalid id")
	}
	return id, nil
}

func (s *Service) ListOnboardingUseCases(ctx context.Context, _ *gen.ListOnboardingUseCasesPayload) (*gen.AdminOnboardingUseCaseList, error) {
	if _, ok := contextvalues.GetAdminAuthContext(ctx); !ok {
		return nil, oops.C(oops.CodeUnauthorized)
	}
	list, err := organizations.ListOnboardingUseCases(ctx, s.db)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list onboarding use cases").LogError(ctx, s.logger)
	}
	return list, nil
}

func (s *Service) CreateOnboardingUseCase(ctx context.Context, payload *gen.CreateOnboardingUseCasePayload) (*gen.AdminOnboardingUseCase, error) {
	if _, ok := contextvalues.GetAdminAuthContext(ctx); !ok {
		return nil, oops.C(oops.CodeUnauthorized)
	}
	useCase, err := organizations.CreateOnboardingUseCase(ctx, s.db, payload.Slug, payload.Name, conv.PtrValOr(payload.Description, ""))
	if err != nil {
		return nil, fmt.Errorf("create onboarding use case: %w", err)
	}
	return useCase, nil
}

func (s *Service) UpdateOnboardingUseCase(ctx context.Context, payload *gen.UpdateOnboardingUseCasePayload) (*gen.AdminOnboardingUseCase, error) {
	if _, ok := contextvalues.GetAdminAuthContext(ctx); !ok {
		return nil, oops.C(oops.CodeUnauthorized)
	}
	id, err := parseOnboardingID(payload.UseCaseID)
	if err != nil {
		return nil, err
	}
	useCase, err := organizations.UpdateOnboardingUseCase(ctx, s.db, id, payload.Name, conv.PtrValOr(payload.Description, ""))
	if err != nil {
		return nil, fmt.Errorf("update onboarding use case: %w", err)
	}
	return useCase, nil
}

func (s *Service) DeleteOnboardingUseCase(ctx context.Context, payload *gen.DeleteOnboardingUseCasePayload) (*gen.AdminOnboardingUseCaseList, error) {
	if _, ok := contextvalues.GetAdminAuthContext(ctx); !ok {
		return nil, oops.C(oops.CodeUnauthorized)
	}
	id, err := parseOnboardingID(payload.UseCaseID)
	if err != nil {
		return nil, err
	}
	list, err := organizations.DeleteOnboardingUseCase(ctx, s.db, id)
	if err != nil {
		return nil, fmt.Errorf("delete onboarding use case: %w", err)
	}
	return list, nil
}

func (s *Service) ListOnboardingPlaybooks(ctx context.Context, payload *gen.ListOnboardingPlaybooksPayload) (*gen.AdminOnboardingPlaybookList, error) {
	if _, ok := contextvalues.GetAdminAuthContext(ctx); !ok {
		return nil, oops.C(oops.CodeUnauthorized)
	}
	var organizationID *string
	if payload.OrganizationID != nil {
		id, err := s.canonicalAdminOrganizationForRequest(ctx, *payload.OrganizationID)
		if err != nil {
			return nil, err
		}
		organizationID = &id
	}
	list, err := organizations.ListOnboardingPlaybooks(ctx, s.db, organizationID)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list onboarding playbooks").LogError(ctx, s.logger)
	}
	return list, nil
}

func (s *Service) CreateOnboardingPlaybook(ctx context.Context, payload *gen.CreateOnboardingPlaybookPayload) (*gen.AdminOnboardingPlaybook, error) {
	if _, ok := contextvalues.GetAdminAuthContext(ctx); !ok {
		return nil, oops.C(oops.CodeUnauthorized)
	}
	var useCaseID *uuid.UUID
	if payload.UseCaseID != nil {
		id, err := parseOnboardingID(*payload.UseCaseID)
		if err != nil {
			return nil, err
		}
		useCaseID = &id
	}
	var organizationID *string
	if payload.OrganizationID != nil {
		id, err := s.canonicalAdminOrganizationForRequest(ctx, *payload.OrganizationID)
		if err != nil {
			return nil, err
		}
		organizationID = &id
	}
	playbook, err := organizations.CreateOnboardingPlaybook(ctx, s.db, organizations.OnboardingPlaybookInput{
		UseCaseID: useCaseID, OrganizationID: organizationID,
		Name: payload.Name, Description: conv.PtrValOr(payload.Description, ""), IsDefault: conv.PtrValOr(payload.IsDefault, false), StepSlugs: payload.StepSlugs,
	})
	if err != nil {
		return nil, fmt.Errorf("create onboarding playbook: %w", err)
	}
	return playbook, nil
}

func (s *Service) UpdateOnboardingPlaybook(ctx context.Context, payload *gen.UpdateOnboardingPlaybookPayload) (*gen.AdminOnboardingPlaybook, error) {
	if _, ok := contextvalues.GetAdminAuthContext(ctx); !ok {
		return nil, oops.C(oops.CodeUnauthorized)
	}
	id, err := parseOnboardingID(payload.PlaybookID)
	if err != nil {
		return nil, err
	}
	playbook, err := organizations.UpdateOnboardingPlaybook(ctx, s.db, id, payload.Name, conv.PtrValOr(payload.Description, ""), conv.PtrValOr(payload.IsDefault, false), payload.StepSlugs)
	if err != nil {
		return nil, fmt.Errorf("update onboarding playbook: %w", err)
	}
	return playbook, nil
}

func (s *Service) DeleteOnboardingPlaybook(ctx context.Context, payload *gen.DeleteOnboardingPlaybookPayload) (*gen.AdminOnboardingPlaybookList, error) {
	if _, ok := contextvalues.GetAdminAuthContext(ctx); !ok {
		return nil, oops.C(oops.CodeUnauthorized)
	}
	id, err := parseOnboardingID(payload.PlaybookID)
	if err != nil {
		return nil, err
	}
	list, err := organizations.DeleteOnboardingPlaybook(ctx, s.db, id)
	if err != nil {
		return nil, fmt.Errorf("delete onboarding playbook: %w", err)
	}
	return list, nil
}

func (s *Service) CloneOnboardingPlaybook(ctx context.Context, payload *gen.CloneOnboardingPlaybookPayload) (*gen.AdminOnboardingPlaybook, error) {
	if _, ok := contextvalues.GetAdminAuthContext(ctx); !ok {
		return nil, oops.C(oops.CodeUnauthorized)
	}
	organizationID, err := s.canonicalAdminOrganizationForRequest(ctx, payload.OrganizationID)
	if err != nil {
		return nil, err
	}
	playbookID, err := parseOnboardingID(payload.PlaybookID)
	if err != nil {
		return nil, err
	}
	playbook, err := organizations.CloneOnboardingPlaybook(ctx, s.db, organizationID, playbookID, payload.Name)
	if err != nil {
		return nil, fmt.Errorf("clone onboarding playbook: %w", err)
	}
	return playbook, nil
}

func (s *Service) GetOrganizationOnboardingPlaybook(ctx context.Context, payload *gen.GetOrganizationOnboardingPlaybookPayload) (*gen.AdminOrganizationOnboardingPlaybook, error) {
	if _, ok := contextvalues.GetAdminAuthContext(ctx); !ok {
		return nil, oops.C(oops.CodeUnauthorized)
	}
	organizationID, err := s.canonicalAdminOrganizationForRequest(ctx, payload.OrganizationID)
	if err != nil {
		return nil, err
	}
	result, err := organizations.LoadOrganizationOnboardingPlaybook(ctx, s.db, organizationID)
	if err != nil {
		return nil, fmt.Errorf("load organization onboarding playbook: %w", err)
	}
	return result, nil
}

func (s *Service) AssignOrganizationOnboardingPlaybook(ctx context.Context, payload *gen.AssignOrganizationOnboardingPlaybookPayload) (*gen.AdminOrganizationOnboardingPlaybook, error) {
	if _, ok := contextvalues.GetAdminAuthContext(ctx); !ok {
		return nil, oops.C(oops.CodeUnauthorized)
	}
	organizationID, err := s.canonicalAdminOrganizationForRequest(ctx, payload.OrganizationID)
	if err != nil {
		return nil, err
	}
	var playbookID *uuid.UUID
	if payload.PlaybookID != nil {
		id, err := parseOnboardingID(*payload.PlaybookID)
		if err != nil {
			return nil, err
		}
		playbookID = &id
	}
	actor, name, _ := adminActor(ctx)
	result, err := organizations.AssignOrganizationOnboardingPlaybook(ctx, s.db, s.audit, organizationID, playbookID, actor, name)
	if err != nil {
		return nil, fmt.Errorf("assign organization onboarding playbook: %w", err)
	}
	return result, nil
}
