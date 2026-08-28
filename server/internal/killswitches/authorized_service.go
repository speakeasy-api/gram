package killswitches

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/constants"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

// Customer request DTOs intentionally contain no organization, actor, scope
// override, or authorization bypass fields.
type AuthorizedActivatePrescriptionRequest struct {
	OperationID     uuid.UUID
	PrescriptionID  *PrescriptionID
	ExpectedVersion *int64
	Definition      DefinitionKey
	PrincipalKind   PrincipalKind
	PrincipalInput  string
	ResourceKind    ResourceKind
	Desired         DesiredVersionInput
}

type AuthorizedChangePrescriptionRequest struct {
	OperationID     uuid.UUID
	PrescriptionID  PrescriptionID
	ExpectedVersion int64
	Desired         DesiredVersionInput
}

type AuthorizedDeactivatePrescriptionRequest struct {
	OperationID     uuid.UUID
	PrescriptionID  PrescriptionID
	ExpectedVersion int64
}

type AuthorizedGetPrescriptionRequest struct {
	PrescriptionID PrescriptionID
}

type AuthorizedListPrescriptionsRequest struct {
	Limit   int32
	AfterID *PrescriptionID
}

type organizationAuthorizer interface {
	RequireUserOrganizationScope(context.Context, string, string, authz.Scope) error
}

// AuthorizedService is the customer-only adapter around GenericService. It
// authenticates and authorizes every call before deriving trusted tenancy and
// actor data. It deliberately has no evaluator dependency.
type AuthorizedService struct {
	generic    GenericService
	authorizer organizationAuthorizer
}

func NewAuthorizedService(generic GenericService, authorizer organizationAuthorizer) (*AuthorizedService, error) {
	if isNilInterface(generic) || isNilInterface(authorizer) {
		return nil, ErrInvalidArgument
	}
	return &AuthorizedService{generic: generic, authorizer: authorizer}, nil
}

func (s *AuthorizedService) ListDefinitions(ctx context.Context) ([]Definition, error) {
	if _, err := s.requireCustomerAdmin(ctx); err != nil {
		return nil, err
	}
	definitions, err := s.generic.ListDefinitions(ctx)
	if err != nil {
		return nil, fmt.Errorf("list authorized killswitch definitions: %w", err)
	}
	return definitions, nil
}

func (s *AuthorizedService) ActivatePrescription(ctx context.Context, request AuthorizedActivatePrescriptionRequest) (MutationResult, error) {
	principal, err := s.requireCustomerAdmin(ctx)
	if err != nil {
		return MutationResult{}, err
	}
	result, err := s.generic.ActivatePrescription(ctx, ActivatePrescriptionInput{
		MutationContext: MutationContext{
			OrganizationID:   principal.organizationID,
			ActorUserID:      principal.userID,
			ActorDisplayName: principal.email,
			OperationID:      request.OperationID,
		},
		PrescriptionID: request.PrescriptionID, ExpectedVersion: request.ExpectedVersion,
		Definition: request.Definition, PrincipalKind: request.PrincipalKind, PrincipalInput: request.PrincipalInput,
		ResourceKind: request.ResourceKind, Desired: request.Desired,
	})
	if err != nil {
		return MutationResult{}, fmt.Errorf("activate authorized killswitch prescription: %w", err)
	}
	return result, nil
}

func (s *AuthorizedService) ChangePrescription(ctx context.Context, request AuthorizedChangePrescriptionRequest) (MutationResult, error) {
	principal, err := s.requireCustomerAdmin(ctx)
	if err != nil {
		return MutationResult{}, err
	}
	result, err := s.generic.ChangePrescription(ctx, ChangePrescriptionRequest{
		MutationContext: MutationContext{OrganizationID: principal.organizationID, ActorUserID: principal.userID, ActorDisplayName: principal.email, OperationID: request.OperationID},
		PrescriptionID:  request.PrescriptionID, ExpectedVersion: request.ExpectedVersion, Desired: request.Desired,
	})
	if err != nil {
		return MutationResult{}, fmt.Errorf("change authorized killswitch prescription: %w", err)
	}
	return result, nil
}

func (s *AuthorizedService) DeactivatePrescription(ctx context.Context, request AuthorizedDeactivatePrescriptionRequest) (MutationResult, error) {
	principal, err := s.requireCustomerAdmin(ctx)
	if err != nil {
		return MutationResult{}, err
	}
	result, err := s.generic.DeactivatePrescription(ctx, DeactivatePrescriptionRequest{
		MutationContext: MutationContext{OrganizationID: principal.organizationID, ActorUserID: principal.userID, ActorDisplayName: principal.email, OperationID: request.OperationID},
		PrescriptionID:  request.PrescriptionID, ExpectedVersion: request.ExpectedVersion,
	})
	if err != nil {
		return MutationResult{}, fmt.Errorf("deactivate authorized killswitch prescription: %w", err)
	}
	return result, nil
}

func (s *AuthorizedService) GetPrescription(ctx context.Context, request AuthorizedGetPrescriptionRequest) (CurrentPrescription, error) {
	principal, err := s.requireCustomerAdmin(ctx)
	if err != nil {
		return CurrentPrescription{}, err
	}
	prescription, err := s.generic.GetPrescription(ctx, GetPrescriptionRequest{OrganizationID: principal.organizationID, PrescriptionID: request.PrescriptionID})
	if err != nil {
		return CurrentPrescription{}, fmt.Errorf("get authorized killswitch prescription: %w", err)
	}
	return prescription, nil
}

func (s *AuthorizedService) ListPrescriptions(ctx context.Context, request AuthorizedListPrescriptionsRequest) (ListPrescriptionsResult, error) {
	principal, err := s.requireCustomerAdmin(ctx)
	if err != nil {
		return ListPrescriptionsResult{}, err
	}
	result, err := s.generic.ListPrescriptions(ctx, ListPrescriptionsRequest{OrganizationID: principal.organizationID, Limit: request.Limit, AfterID: request.AfterID})
	if err != nil {
		return ListPrescriptionsResult{}, fmt.Errorf("list authorized killswitch prescriptions: %w", err)
	}
	return result, nil
}

type customerPrincipal struct {
	organizationID OrganizationID
	userID         string
	email          string
}

func (s *AuthorizedService) requireCustomerAdmin(ctx context.Context) (customerPrincipal, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || !contextvalues.HasValidatedGramSession(ctx) {
		return customerPrincipal{}, oops.C(oops.CodeUnauthorized)
	}
	if authCtx.SessionID == nil || *authCtx.SessionID == "" || authCtx.ActiveOrganizationID == "" || authCtx.UserID == "" || authCtx.Email == nil || strings.TrimSpace(*authCtx.Email) == "" {
		return customerPrincipal{}, oops.C(oops.CodeUnauthorized)
	}
	if authCtx.APIKeyID != "" || authCtx.APIKeyName != "" || len(authCtx.APIKeyScopes) != 0 || authCtx.OrgWidePluginHooksKey {
		return customerPrincipal{}, oops.C(oops.CodeForbidden)
	}
	if _, ok := contextvalues.GetAssistantPrincipal(ctx); ok {
		return customerPrincipal{}, oops.C(oops.CodeForbidden)
	}
	if _, ok := contextvalues.GetOAuthClientID(ctx); ok {
		return customerPrincipal{}, oops.C(oops.CodeForbidden)
	}
	if _, ok := contextvalues.GetActingSurface(ctx); ok {
		return customerPrincipal{}, oops.C(oops.CodeForbidden)
	}
	if contextvalues.IsSupportSession(ctx) || contextvalues.IsLegacyImpersonatedSession(ctx) {
		return customerPrincipal{}, oops.C(oops.CodeForbidden)
	}
	if _, ok := contextvalues.GetRBACScopeOverride(ctx); ok {
		return customerPrincipal{}, oops.C(oops.CodeForbidden)
	}
	if authCtx.ActiveOrganizationID == constants.DemoOrganizationID {
		return customerPrincipal{}, oops.C(oops.CodeForbidden)
	}
	if err := s.authorizer.RequireUserOrganizationScope(ctx, authCtx.ActiveOrganizationID, authCtx.UserID, authz.ScopeOrgAdmin); err != nil {
		return customerPrincipal{}, fmt.Errorf("authorize killswitch customer administrator: %w", err)
	}
	return customerPrincipal{organizationID: OrganizationID(authCtx.ActiveOrganizationID), userID: authCtx.UserID, email: strings.TrimSpace(*authCtx.Email)}, nil
}
