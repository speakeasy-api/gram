package platformmcp

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
)

var ErrRegistrationUnavailable = errors.New("platform mcp catalog registration unavailable")

type CatalogRegistrationGateChecker interface {
	Enabled(ctx context.Context, organizationID, projectSlug string) (bool, error)
}

type RegistrationPersistence interface {
	ResolveProject(ctx context.Context, organizationID, projectSlug string) (ResolvedProject, error)
	EligibleCatalogRegistrationTarget(ctx context.Context, organizationID string, project ResolvedProject) (bool, error)
	BeginReceipt(ctx context.Context, principal Principal, project ResolvedProject, request CatalogRegistrationRequest, now time.Time) (OperationReceipt, error)
	ConvergeRegistration(ctx context.Context, principal Principal, project ResolvedProject, request CatalogRegistrationRequest, receipt OperationReceipt) (OperationReceipt, error)
	CompleteRegistration(ctx context.Context, principal Principal, project ResolvedProject, request CatalogRegistrationRequest, receipt OperationReceipt, remoteURL string) (OperationReceipt, error)
	IssueSetupHandoff(ctx context.Context, principal Principal, binding SetupHandoffBinding, now time.Time) (IssuedSetupHandoff, error)
}

type RegisterCatalogMCPInput struct {
	ProjectSlug    string
	ProviderKey    string
	CatalogRef     string
	IdempotencyKey string
}

type IssueSetupHandoffInput struct {
	ProjectSlug    string
	RegistrationID string
	ProviderKey    string
	CatalogRef     string
}

type RegisterCatalogMCPResult struct {
	Project      ResolvedProject
	ProviderKey  string
	CatalogRef   string
	SetupIntent  string
	Receipt      OperationReceipt
	Registration string
}

// RegistrationService is the handler-facing boundary for one reviewed catalog
// registration. Catalog identity is validated before persistence, and the
// normalized input hash is computed here rather than trusted from an MCP caller.
type RegistrationService struct {
	catalog   Catalog
	gate      CatalogRegistrationGateChecker
	store     RegistrationPersistence
	now       func() time.Time
	readiness *ReadinessService
	budgets   OperationBudgets
	telemetry LifecycleTelemetry
}

func NewRegistrationService(catalog Catalog, gate CatalogRegistrationGateChecker, store RegistrationPersistence) *RegistrationService {
	return &RegistrationService{
		catalog:   catalog,
		gate:      gate,
		store:     store,
		now:       time.Now,
		readiness: nil,
		telemetry: noopLifecycleTelemetry{},
		budgets: OperationBudgets{
			Catalog:      OperationBudget{Connection: nil, Organization: nil},
			Registration: OperationBudget{Connection: nil, Organization: nil},
			Handoff:      OperationBudget{Connection: nil, Organization: nil},
			SetupStart:   OperationBudget{Connection: nil, Organization: nil},
			Repair:       OperationBudget{Connection: nil, Organization: nil},
		},
	}
}

func (s *RegistrationService) WithOperationBudgets(budgets OperationBudgets) *RegistrationService {
	if s != nil {
		s.budgets = budgets
	}
	return s
}

func (s *RegistrationService) WithTelemetry(telemetry LifecycleTelemetry) *RegistrationService {
	if s != nil && telemetry != nil {
		s.telemetry = telemetry
	}
	return s
}

func (s *RegistrationService) WithReadiness(readiness *ReadinessService) *RegistrationService {
	if s != nil {
		s.readiness = readiness
	}
	return s
}

func (s *RegistrationService) IssueSetupHandoff(ctx context.Context, principal Principal, input IssueSetupHandoffInput) (IssuedSetupHandoff, error) {
	if s == nil || s.catalog == nil || s.gate == nil || s.store == nil || !s.budgets.Handoff.valid() || input.ProjectSlug == "" || input.RegistrationID == "" || input.ProviderKey == "" || input.CatalogRef == "" {
		return IssuedSetupHandoff{}, ErrRegistrationUnavailable
	}
	registrationID, err := uuid.Parse(input.RegistrationID)
	if err != nil {
		return IssuedSetupHandoff{}, ErrSetupHandoffInvalid
	}
	if err := s.budgets.Handoff.Allow(ctx, principal); err != nil {
		return IssuedSetupHandoff{}, err
	}
	enabled, err := s.gate.Enabled(ctx, principal.OrganizationID, input.ProjectSlug)
	if err != nil {
		return IssuedSetupHandoff{}, fmt.Errorf("check catalog registration gate: %w", err)
	}
	if !enabled {
		return IssuedSetupHandoff{}, ErrRegistrationUnavailable
	}
	catalog, err := s.catalog.Inspect(ctx, input.ProviderKey, input.CatalogRef)
	if err != nil {
		return IssuedSetupHandoff{}, fmt.Errorf("inspect setup handoff catalog candidate: %w", err)
	}
	if catalog.ProviderKey != input.ProviderKey || catalog.CatalogRef != input.CatalogRef || catalog.SetupIntent == "" || catalog.Transport != "streamable-http" {
		return IssuedSetupHandoff{}, ErrCatalogRejected
	}
	project, err := s.store.ResolveProject(ctx, principal.OrganizationID, input.ProjectSlug)
	if err != nil {
		return IssuedSetupHandoff{}, fmt.Errorf("resolve setup handoff project: %w", err)
	}
	if err := s.requireEligibleTarget(ctx, principal.OrganizationID, project); err != nil {
		return IssuedSetupHandoff{}, err
	}
	issued, err := s.store.IssueSetupHandoff(ctx, principal, SetupHandoffBinding{
		ProjectID:        project.ID,
		RegistrationID:   registrationID,
		ProviderKey:      catalog.ProviderKey,
		CatalogReference: catalog.CatalogRef,
		Intent:           catalog.SetupIntent,
	}, s.now())
	if err != nil {
		s.telemetry.Record(ctx, LifecycleEvent{Operation: "provider_setup", Phase: "handoff", Outcome: lifecycleOutcome(err), State: ""})
		return IssuedSetupHandoff{}, fmt.Errorf("issue platform mcp setup handoff: %w", err)
	}
	s.telemetry.Record(ctx, LifecycleEvent{Operation: "provider_setup", Phase: "handoff", Outcome: "succeeded", State: ""})
	return issued, nil
}

func (s *RegistrationService) RegisterCatalogMCP(ctx context.Context, principal Principal, input RegisterCatalogMCPInput) (RegisterCatalogMCPResult, error) {
	if s == nil || s.catalog == nil || s.gate == nil || s.store == nil || !s.budgets.Registration.valid() || input.ProjectSlug == "" || input.ProviderKey == "" || input.CatalogRef == "" || input.IdempotencyKey == "" {
		return RegisterCatalogMCPResult{}, ErrRegistrationUnavailable
	}
	if err := s.budgets.Registration.Allow(ctx, principal); err != nil {
		return RegisterCatalogMCPResult{}, err
	}

	enabled, err := s.gate.Enabled(ctx, principal.OrganizationID, input.ProjectSlug)
	if err != nil {
		return RegisterCatalogMCPResult{}, fmt.Errorf("check catalog registration gate: %w", err)
	}
	if !enabled {
		return RegisterCatalogMCPResult{}, ErrRegistrationUnavailable
	}

	catalog, err := s.catalog.Inspect(ctx, input.ProviderKey, input.CatalogRef)
	if err != nil {
		return RegisterCatalogMCPResult{}, fmt.Errorf("inspect registration catalog candidate: %w", err)
	}
	if catalog.ProviderKey != input.ProviderKey || catalog.CatalogRef != input.CatalogRef || catalog.SetupIntent == "" || catalog.Transport != "streamable-http" {
		return RegisterCatalogMCPResult{}, ErrCatalogRejected
	}

	project, err := s.store.ResolveProject(ctx, principal.OrganizationID, input.ProjectSlug)
	if err != nil {
		return RegisterCatalogMCPResult{}, fmt.Errorf("resolve catalog registration project: %w", err)
	}
	if err := s.requireEligibleTarget(ctx, principal.OrganizationID, project); err != nil {
		return RegisterCatalogMCPResult{}, err
	}
	request := CatalogRegistrationRequest{
		ProjectSlug:      project.Slug,
		SourceKind:       "catalog",
		CatalogProvider:  catalog.ProviderKey,
		CatalogReference: catalog.CatalogRef,
		IdempotencyKey:   input.IdempotencyKey,
		InputHash:        catalogRegistrationInputHash(project.Slug, "catalog", catalog.ProviderKey, catalog.CatalogRef),
	}
	receipt, err := s.store.BeginReceipt(ctx, principal, project, request, s.now())
	if err != nil {
		return RegisterCatalogMCPResult{}, fmt.Errorf("begin catalog registration receipt: %w", err)
	}
	if !receipt.Replayed || receipt.Status == receiptStatusPending {
		receipt, err = s.store.ConvergeRegistration(ctx, principal, project, request, receipt)
		if err != nil {
			return RegisterCatalogMCPResult{}, fmt.Errorf("converge catalog registration: %w", err)
		}
	}
	if receipt.ResultCode == receiptResultActiveCap {
		return RegisterCatalogMCPResult{}, ErrRegistrationCap
	}
	if !receipt.RegistrationID.Valid {
		return RegisterCatalogMCPResult{}, ErrRegistrationUnavailable
	}
	if receipt.Status == receiptStatusPending {
		if catalog.remoteURL == "" {
			return RegisterCatalogMCPResult{}, ErrRegistrationUnavailable
		}
		receipt, err = s.store.CompleteRegistration(ctx, principal, project, request, receipt, catalog.remoteURL)
		if err != nil {
			return RegisterCatalogMCPResult{}, fmt.Errorf("complete catalog registration: %w", err)
		}
	}
	if !receipt.RegistrationID.Valid {
		s.telemetry.Record(ctx, LifecycleEvent{Operation: "registration", Phase: "complete", Outcome: "unavailable", State: ""})
		return RegisterCatalogMCPResult{}, ErrRegistrationUnavailable
	}

	s.telemetry.Record(ctx, LifecycleEvent{Operation: "registration", Phase: "complete", Outcome: "succeeded", State: ""})
	return RegisterCatalogMCPResult{
		Project:      project,
		ProviderKey:  catalog.ProviderKey,
		CatalogRef:   catalog.CatalogRef,
		SetupIntent:  catalog.SetupIntent,
		Receipt:      receipt,
		Registration: receipt.RegistrationID.UUID.String(),
	}, nil
}

func (s *RegistrationService) requireEligibleTarget(ctx context.Context, organizationID string, project ResolvedProject) error {
	eligible, err := s.store.EligibleCatalogRegistrationTarget(ctx, organizationID, project)
	if err != nil {
		return fmt.Errorf("check platform mcp catalog registration target eligibility: %w", err)
	}
	if !eligible {
		return ErrTargetIneligible
	}
	return nil
}
