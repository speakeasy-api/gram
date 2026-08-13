//nolint:exhaustruct,wrapcheck // Service defaults use optional zero values and preserve typed persistence errors.
package platformmcp

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"
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
	CompleteRegistration(ctx context.Context, principal Principal, project ResolvedProject, request CatalogRegistrationRequest, receipt OperationReceipt, configuration resolvedCatalogConfiguration) (OperationReceipt, error)
	ResolveRegistrationPendingSecretFields(ctx context.Context, principal Principal, project ResolvedProject, registrationID uuid.UUID, declared []CatalogConfigurationField) ([]CatalogConfigurationField, error)
	ResolveRegistrationCatalogIdentity(ctx context.Context, principal Principal, project ResolvedProject, registrationID uuid.UUID) (CatalogCandidate, error)
	ResolveRegistrationDashboardSetup(ctx context.Context, principal Principal, project ResolvedProject, registrationID uuid.UUID) (RegistrationDashboardSetup, error)
	IssueSetupHandoff(ctx context.Context, principal Principal, binding SetupHandoffBinding, now time.Time) (IssuedSetupHandoff, error)
}

type RegisterCatalogMCPInput struct {
	ProjectSlug     string
	ProviderKey     string
	CatalogRef      string
	NonSecretConfig CatalogConfigurationValues
	IdempotencyKey  string
}

type IssueSetupHandoffInput struct {
	ProjectSlug    string
	RegistrationID string
	ProviderKey    string
	CatalogRef     string
}

type RegisterCatalogMCPResult struct {
	Project             ResolvedProject
	ProviderKey         string
	CatalogRef          string
	SetupIntent         string
	Receipt             OperationReceipt
	Registration        string
	SecretFieldsPending []CatalogConfigurationField
}

// RegistrationDashboardSetup is the persisted, server-owned dashboard target
// for a registered Remote MCP source. It deliberately contains no upstream URL,
// OAuth metadata, header value, or other provider material.
type RegistrationDashboardSetup struct {
	OrganizationSlug string
	MCPServerRoute   string
}

// RegistrationService is the handler-facing boundary for one reviewed catalog
// registration. Catalog identity is validated before persistence, and the
// normalized input hash is computed here rather than trusted from an MCP caller.
type RegistrationService struct {
	catalog                    Catalog
	gate                       CatalogRegistrationGateChecker
	store                      RegistrationPersistence
	now                        func() time.Time
	readiness                  *ReadinessService
	dashboardURL               *url.URL
	identityProviderAttachment CatalogIdentityProviderAttachment
	budgets                    OperationBudgets
	telemetry                  LifecycleTelemetry
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

// WithIdentityProviderAttachment enables confirmed agent-side provider
// attachment using trusted lifecycle persistence and server-owned OAuth calls.
func (s *RegistrationService) WithIdentityProviderAttachment(attachment CatalogIdentityProviderAttachment) *RegistrationService {
	if s != nil {
		s.identityProviderAttachment = attachment
	}
	return s
}

// WithDashboardURL supplies the configured dashboard origin used only to build
// trusted same-origin setup links for persisted registrations.
func (s *RegistrationService) WithDashboardURL(dashboardURL *url.URL) *RegistrationService {
	if s != nil && dashboardURL != nil && dashboardURL.Scheme == "https" && dashboardURL.Host != "" && dashboardURL.User == nil {
		dashboardURLCopy := *dashboardURL
		s.dashboardURL = &dashboardURLCopy
	}
	return s
}

// RegistrationCatalogIdentity resolves the server-owned catalog identity for a
// lifecycle-bound registration. It keeps management adapters from depending on
// RegistrationService persistence internals.
func (s *RegistrationService) RegistrationCatalogIdentity(ctx context.Context, principal Principal, project ResolvedProject, registrationID uuid.UUID) (CatalogCandidate, error) {
	if s == nil || s.store == nil {
		return CatalogCandidate{}, ErrRegistrationUnavailable
	}
	return s.store.ResolveRegistrationCatalogIdentity(ctx, principal, project, registrationID)
}

// DashboardSetupURL returns the existing Remote MCP server Authentication
// settings page for a browser-catalogue registration. This is the browser-only
// fallback for provider attachment; callers cannot provide an endpoint, source,
// or credential.
func (s *RegistrationService) DashboardSetupURL(ctx context.Context, principal Principal, input IssueSetupHandoffInput) (string, error) {
	if s == nil || s.catalog == nil || s.gate == nil || s.store == nil || s.dashboardURL == nil || input.ProjectSlug == "" || input.RegistrationID == "" || input.ProviderKey == "" || input.CatalogRef == "" {
		return "", ErrRegistrationUnavailable
	}
	if !isBrowserCatalogProviderKey(input.ProviderKey) {
		return "", ErrCatalogRejected
	}
	enabled, err := s.gate.Enabled(ctx, principal.OrganizationID, input.ProjectSlug)
	if err != nil {
		return "", fmt.Errorf("check dashboard setup gate: %w", err)
	}
	if !enabled {
		return "", ErrRegistrationUnavailable
	}
	catalog, err := s.catalog.Inspect(ctx, input.ProviderKey, input.CatalogRef)
	if err != nil {
		return "", fmt.Errorf("inspect dashboard setup catalog candidate: %w", err)
	}
	if catalog.ProviderKey != input.ProviderKey || catalog.CatalogRef != input.CatalogRef || catalog.SetupIntent == "" || catalog.Transport != "streamable-http" {
		return "", ErrCatalogRejected
	}
	registrationID, err := uuid.Parse(input.RegistrationID)
	if err != nil {
		return "", ErrSetupHandoffInvalid
	}
	project, err := s.store.ResolveProject(ctx, principal.OrganizationID, input.ProjectSlug)
	if err != nil {
		return "", fmt.Errorf("resolve dashboard setup project: %w", err)
	}
	if err := s.requireEligibleTarget(ctx, principal.OrganizationID, project); err != nil {
		return "", err
	}
	persisted, err := s.store.ResolveRegistrationCatalogIdentity(ctx, principal, project, registrationID)
	if err != nil {
		return "", err
	}
	if persisted.ProviderKey != catalog.ProviderKey || persisted.CatalogRef != catalog.CatalogRef {
		return "", ErrCatalogRejected
	}
	setup, err := s.store.ResolveRegistrationDashboardSetup(ctx, principal, project, registrationID)
	if err != nil {
		return "", err
	}
	if setup.OrganizationSlug == "" || setup.MCPServerRoute == "" {
		return "", ErrRegistrationUnavailable
	}
	return s.dashboardURL.JoinPath(setup.OrganizationSlug, "projects", project.Slug, "mcp", "x", setup.MCPServerRoute, "settings").String() + "#authentication", nil
}

// DashboardAuthorizationURL returns the Inspect page only after the upstream
// provider is attached. Inspect owns the visible Connect/Authorize action.
func (s *RegistrationService) DashboardAuthorizationURL(ctx context.Context, principal Principal, projectSlug, registrationID string) (string, error) {
	if s == nil || s.store == nil || s.dashboardURL == nil || projectSlug == "" || registrationID == "" {
		return "", ErrRegistrationUnavailable
	}
	parsedID, err := uuid.Parse(registrationID)
	if err != nil {
		return "", ErrSetupHandoffInvalid
	}
	project, err := s.store.ResolveProject(ctx, principal.OrganizationID, projectSlug)
	if err != nil {
		return "", fmt.Errorf("resolve authorization setup project: %w", err)
	}
	setup, err := s.store.ResolveRegistrationDashboardSetup(ctx, principal, project, parsedID)
	if err != nil {
		return "", err
	}
	if setup.OrganizationSlug == "" || setup.MCPServerRoute == "" {
		return "", ErrRegistrationUnavailable
	}
	return s.dashboardURL.JoinPath(setup.OrganizationSlug, "projects", project.Slug, "mcp", "x", setup.MCPServerRoute, "inspect").String(), nil
}

// AttachDefaultIdentityProvider attaches the one OAuth provider advertised by
// the lifecycle-bound Remote MCP. The caller supplies no provider identity,
// OAuth configuration, client ID, secret, code, or token.
func (s *RegistrationService) AttachDefaultIdentityProvider(ctx context.Context, principal Principal, projectSlug, registrationID string) (CatalogIdentityProviderAttachmentResult, error) {
	if s == nil || s.gate == nil || s.store == nil || s.identityProviderAttachment == nil || !s.budgets.SetupStart.valid() || projectSlug == "" || registrationID == "" {
		return CatalogIdentityProviderAttachmentResult{}, ErrIdentityProviderAttachmentUnavailable
	}
	if err := s.budgets.SetupStart.Allow(ctx, principal); err != nil {
		return CatalogIdentityProviderAttachmentResult{}, err
	}
	parsedID, err := uuid.Parse(registrationID)
	if err != nil {
		return CatalogIdentityProviderAttachmentResult{}, ErrSetupHandoffInvalid
	}
	enabled, err := s.gate.Enabled(ctx, principal.OrganizationID, projectSlug)
	if err != nil {
		return CatalogIdentityProviderAttachmentResult{}, fmt.Errorf("check identity-provider attachment gate: %w", err)
	}
	if !enabled {
		return CatalogIdentityProviderAttachmentResult{}, ErrRegistrationUnavailable
	}
	project, err := s.store.ResolveProject(ctx, principal.OrganizationID, projectSlug)
	if err != nil {
		return CatalogIdentityProviderAttachmentResult{}, fmt.Errorf("resolve identity-provider attachment project: %w", err)
	}
	if err := s.requireEligibleTarget(ctx, principal.OrganizationID, project); err != nil {
		return CatalogIdentityProviderAttachmentResult{}, err
	}
	candidate, err := s.store.ResolveRegistrationCatalogIdentity(ctx, principal, project, parsedID)
	if err != nil {
		return CatalogIdentityProviderAttachmentResult{}, err
	}
	if !isBrowserCatalogProviderKey(candidate.ProviderKey) {
		return CatalogIdentityProviderAttachmentResult{}, ErrIdentityProviderAttachmentUnsupported
	}
	return s.identityProviderAttachment.Attach(ctx, principal, project, parsedID)
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

// IssueSetupHandoffForRegistration derives the persisted catalogue identity from
// the exact workflow-bound registration. Dashboard callers never supply a
// provider key, catalogue reference, endpoint, or credential.
func (s *RegistrationService) IssueSetupHandoffForRegistration(ctx context.Context, principal Principal, projectSlug, registrationID string) (IssuedSetupHandoff, error) {
	if s == nil || s.store == nil || projectSlug == "" || registrationID == "" {
		return IssuedSetupHandoff{}, ErrRegistrationUnavailable
	}
	parsedID, err := uuid.Parse(registrationID)
	if err != nil {
		return IssuedSetupHandoff{}, ErrSetupHandoffInvalid
	}
	project, err := s.store.ResolveProject(ctx, principal.OrganizationID, projectSlug)
	if err != nil {
		return IssuedSetupHandoff{}, fmt.Errorf("resolve setup handoff project: %w", err)
	}
	candidate, err := s.store.ResolveRegistrationCatalogIdentity(ctx, principal, project, parsedID)
	if err != nil {
		return IssuedSetupHandoff{}, err
	}
	return s.IssueSetupHandoff(ctx, principal, IssueSetupHandoffInput{
		ProjectSlug:    projectSlug,
		RegistrationID: registrationID,
		ProviderKey:    candidate.ProviderKey,
		CatalogRef:     candidate.CatalogRef,
	})
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
	configuration, err := catalog.resolveConfiguration(input.NonSecretConfig)
	if err != nil {
		return RegisterCatalogMCPResult{}, ErrCatalogConfigurationRejected
	}
	configuration.displayName = catalogDisplayName(catalog)
	configurationHash := catalogConfigurationHash(input.NonSecretConfig)
	request := CatalogRegistrationRequest{
		ProjectSlug:       project.Slug,
		SourceKind:        "catalog",
		CatalogProvider:   catalog.ProviderKey,
		CatalogReference:  catalog.CatalogRef,
		ConfigurationHash: configurationHash,
		IdempotencyKey:    input.IdempotencyKey,
		InputHash:         catalogRegistrationInputHash(project.Slug, "catalog", catalog.ProviderKey, catalog.CatalogRef, configurationHash),
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
		receipt, err = s.store.CompleteRegistration(ctx, principal, project, request, receipt, configuration)
		if err != nil {
			return RegisterCatalogMCPResult{}, fmt.Errorf("complete catalog registration: %w", err)
		}
	}
	if !receipt.RegistrationID.Valid {
		s.telemetry.Record(ctx, LifecycleEvent{Operation: "registration", Phase: "complete", Outcome: "unavailable", State: ""})
		return RegisterCatalogMCPResult{}, ErrRegistrationUnavailable
	}
	pendingSecretFields := configuration.pendingSecretFields
	if receipt.Replayed && len(pendingSecretFields) > 0 {
		pendingSecretFields, err = s.store.ResolveRegistrationPendingSecretFields(ctx, principal, project, receipt.RegistrationID.UUID, configuration.pendingSecretFields)
		if err != nil {
			return RegisterCatalogMCPResult{}, fmt.Errorf("resolve persisted catalog registration secret setup state: %w", err)
		}
	}

	s.telemetry.Record(ctx, LifecycleEvent{Operation: "registration", Phase: "complete", Outcome: "succeeded", State: ""})
	return RegisterCatalogMCPResult{
		Project:             project,
		ProviderKey:         catalog.ProviderKey,
		CatalogRef:          catalog.CatalogRef,
		SetupIntent:         catalog.SetupIntent,
		Receipt:             receipt,
		Registration:        receipt.RegistrationID.UUID.String(),
		SecretFieldsPending: append([]CatalogConfigurationField(nil), pendingSecretFields...),
	}, nil
}

func catalogDisplayName(catalog CatalogDetails) string {
	if name := strings.TrimSpace(catalog.Name); name != "" {
		return name
	}
	return strings.TrimSpace(catalog.CatalogRef)
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
