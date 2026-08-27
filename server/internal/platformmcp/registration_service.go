//nolint:exhaustruct,wrapcheck // Service defaults use optional zero values and preserve typed persistence errors.
package platformmcp

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"
	"unicode"

	"github.com/google/uuid"
)

var ErrRegistrationUnavailable = errors.New("platform mcp catalog registration unavailable")

const directRemoteDisplayNameMaxBytes = 256

type CatalogRegistrationGateChecker interface {
	Enabled(ctx context.Context, organizationID, projectSlug string) (bool, error)
	EnabledOrganization(ctx context.Context, organizationID string) (bool, error)
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
	directRemoteInspector      DirectRemoteInspector
	lifecycleMetadata          *LifecycleMetadataService
	lifecycleVisibility        *LifecycleVisibilityService
	clientAdmission            *ClientAdmissionService
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
			Catalog:           OperationBudget{Connection: nil, Organization: nil},
			Registration:      OperationBudget{Connection: nil, Organization: nil},
			Handoff:           OperationBudget{Connection: nil, Organization: nil},
			SetupStart:        OperationBudget{Connection: nil, Organization: nil},
			Repair:            OperationBudget{Connection: nil, Organization: nil},
			RiskMutations:     OperationBudget{Connection: nil, Organization: nil},
			LifecycleMetadata: OperationBudget{Connection: nil, Organization: nil},
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

// WithDirectRemoteInspector enables direct user-supplied remote MCP admission.
// The inspector is server-composed because it owns Guardian-backed egress.
func (s *RegistrationService) WithDirectRemoteInspector(inspector DirectRemoteInspector) *RegistrationService {
	if s != nil {
		s.directRemoteInspector = inspector
	}
	return s
}

// WithLifecycleMetadata enables narrow, Platform-owned MCP display-name updates.
// The command is injected from server composition to share the dashboard domain
// implementation without importing mcpservers into Platform MCP.
func (s *RegistrationService) WithLifecycleMetadata(metadata *LifecycleMetadataService) *RegistrationService {
	if s != nil {
		s.lifecycleMetadata = metadata
	}
	return s
}

func (s *RegistrationService) UpdateMCPMetadata(ctx context.Context, principal Principal, input UpdateMCPMetadataInput) (UpdateMCPMetadataResult, error) {
	if s == nil || s.gate == nil || s.lifecycleMetadata == nil || !s.budgets.LifecycleMetadata.valid() {
		return UpdateMCPMetadataResult{}, ErrRegistrationUnavailable
	}
	if err := s.budgets.LifecycleMetadata.Allow(ctx, principal); err != nil {
		return UpdateMCPMetadataResult{}, err
	}
	enabled, err := s.gate.Enabled(ctx, principal.OrganizationID, input.ProjectSlug)
	if err != nil {
		return UpdateMCPMetadataResult{}, fmt.Errorf("check metadata update gate: %w", err)
	}
	if !enabled {
		return UpdateMCPMetadataResult{}, ErrRegistrationUnavailable
	}
	return s.lifecycleMetadata.Update(ctx, principal, input)
}

// WithClientAdmission enables reading and setting the CIMD client admission
// policy of a registered MCP's session issuer over MCP, which is otherwise
// reachable only from the dashboard's Authentication settings.
func (s *RegistrationService) WithClientAdmission(clientAdmission *ClientAdmissionService) *RegistrationService {
	if s != nil {
		s.clientAdmission = clientAdmission
	}
	return s
}

func (s *RegistrationService) GetClientAdmission(ctx context.Context, principal Principal, projectSlug, registrationID string) (ClientAdmission, error) {
	project, parsedID, err := s.clientAdmissionTarget(ctx, principal, projectSlug, registrationID)
	if err != nil {
		return ClientAdmission{}, err
	}
	return s.clientAdmission.Get(ctx, principal, project, parsedID)
}

// SetClientAdmission applies a confirmed admission-mode change. Confirmation is
// enforced at the tool boundary; this path assumes it and only verifies that
// the caller may act on the registration.
func (s *RegistrationService) SetClientAdmission(ctx context.Context, principal Principal, projectSlug, registrationID, mode string) (ClientAdmission, error) {
	project, parsedID, err := s.clientAdmissionTarget(ctx, principal, projectSlug, registrationID)
	if err != nil {
		return ClientAdmission{}, err
	}
	return s.clientAdmission.Set(ctx, principal, project, parsedID, mode)
}

// clientAdmissionTarget applies the shared preconditions of both admission
// paths: an available deployment, the caller's operation budget, the rollout
// gate, and an eligible project.
func (s *RegistrationService) clientAdmissionTarget(ctx context.Context, principal Principal, projectSlug, registrationID string) (ResolvedProject, uuid.UUID, error) {
	if s == nil || s.gate == nil || s.store == nil || !s.clientAdmission.valid() || !s.budgets.LifecycleMetadata.valid() || projectSlug == "" || registrationID == "" {
		return ResolvedProject{}, uuid.Nil, ErrClientAdmissionUnavailable
	}
	parsedID, err := uuid.Parse(registrationID)
	if err != nil {
		return ResolvedProject{}, uuid.Nil, ErrClientAdmissionInvalid
	}
	if err := s.budgets.LifecycleMetadata.Allow(ctx, principal); err != nil {
		return ResolvedProject{}, uuid.Nil, err
	}
	enabled, err := s.gate.Enabled(ctx, principal.OrganizationID, projectSlug)
	if err != nil {
		return ResolvedProject{}, uuid.Nil, fmt.Errorf("check client admission gate: %w", err)
	}
	if !enabled {
		return ResolvedProject{}, uuid.Nil, ErrRegistrationUnavailable
	}
	project, err := s.store.ResolveProject(ctx, principal.OrganizationID, projectSlug)
	if err != nil {
		return ResolvedProject{}, uuid.Nil, fmt.Errorf("resolve client admission project: %w", err)
	}
	return project, parsedID, nil
}

func (s *RegistrationService) WithLifecycleVisibility(visibility *LifecycleVisibilityService) *RegistrationService {
	if s != nil {
		s.lifecycleVisibility = visibility
	}
	return s
}

func (s *RegistrationService) DisableMCP(ctx context.Context, principal Principal, input UpdateMCPVisibilityInput) (UpdateMCPVisibilityResult, error) {
	if s == nil || s.lifecycleVisibility == nil {
		return UpdateMCPVisibilityResult{}, ErrRegistrationUnavailable
	}
	return s.lifecycleVisibility.Disable(ctx, principal, input)
}

func (s *RegistrationService) EnableMCP(ctx context.Context, principal Principal, input UpdateMCPVisibilityInput) (UpdateMCPVisibilityResult, error) {
	if s == nil || s.lifecycleVisibility == nil {
		return UpdateMCPVisibilityResult{}, ErrRegistrationUnavailable
	}
	return s.lifecycleVisibility.Enable(ctx, principal, input)
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
	if s == nil || s.gate == nil || s.store == nil || s.dashboardURL == nil || input.ProjectSlug == "" || input.RegistrationID == "" || input.ProviderKey == "" || input.CatalogRef == "" {
		return "", ErrRegistrationUnavailable
	}
	if !isBrowserCatalogProviderKey(input.ProviderKey) && input.ProviderKey != directRemoteProviderKey {
		return "", ErrCatalogRejected
	}
	enabled, err := s.gate.Enabled(ctx, principal.OrganizationID, input.ProjectSlug)
	if err != nil {
		return "", fmt.Errorf("check dashboard setup gate: %w", err)
	}
	if !enabled {
		return "", ErrRegistrationUnavailable
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
	if persisted.ProviderKey != input.ProviderKey || persisted.CatalogRef != input.CatalogRef {
		return "", ErrCatalogRejected
	}
	if isBrowserCatalogProviderKey(input.ProviderKey) {
		if s.catalog == nil {
			return "", ErrRegistrationUnavailable
		}
		catalog, err := s.catalog.Inspect(ctx, input.ProviderKey, input.CatalogRef)
		if err != nil {
			return "", fmt.Errorf("inspect dashboard setup catalog candidate: %w", err)
		}
		if catalog.ProviderKey != persisted.ProviderKey || catalog.CatalogRef != persisted.CatalogRef || catalog.SetupIntent == "" || catalog.Transport != "streamable-http" {
			return "", ErrCatalogRejected
		}
	}
	return s.dashboardSettingsURL(ctx, principal, project, registrationID)
}

func (s *RegistrationService) dashboardSettingsURL(ctx context.Context, principal Principal, project ResolvedProject, registrationID uuid.UUID) (string, error) {
	if s == nil || s.store == nil || s.dashboardURL == nil || registrationID == uuid.Nil {
		return "", ErrRegistrationUnavailable
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
	if !isBrowserCatalogProviderKey(candidate.ProviderKey) && candidate.ProviderKey != directRemoteProviderKey {
		return CatalogIdentityProviderAttachmentResult{}, ErrIdentityProviderAttachmentUnsupported
	}
	return s.identityProviderAttachment.Attach(ctx, principal, project, parsedID)
}

func (s *RegistrationService) IssueSetupHandoff(ctx context.Context, principal Principal, input IssueSetupHandoffInput) (IssuedSetupHandoff, error) {
	if s == nil || s.gate == nil || s.store == nil || !s.budgets.Handoff.valid() || input.ProjectSlug == "" || input.RegistrationID == "" || input.ProviderKey == "" || input.CatalogRef == "" {
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
	project, err := s.store.ResolveProject(ctx, principal.OrganizationID, input.ProjectSlug)
	if err != nil {
		return IssuedSetupHandoff{}, fmt.Errorf("resolve setup handoff project: %w", err)
	}
	if err := s.requireEligibleTarget(ctx, principal.OrganizationID, project); err != nil {
		return IssuedSetupHandoff{}, err
	}
	candidate, err := s.store.ResolveRegistrationCatalogIdentity(ctx, principal, project, registrationID)
	if err != nil {
		return IssuedSetupHandoff{}, fmt.Errorf("resolve setup handoff registration identity: %w", err)
	}
	if candidate.ProviderKey != input.ProviderKey || candidate.CatalogRef != input.CatalogRef {
		return IssuedSetupHandoff{}, ErrCatalogRejected
	}
	intent := ""
	if candidate.ProviderKey == directRemoteProviderKey {
		intent = "dashboard_source_settings"
	} else {
		if s.catalog == nil {
			return IssuedSetupHandoff{}, ErrRegistrationUnavailable
		}
		catalog, err := s.catalog.Inspect(ctx, candidate.ProviderKey, candidate.CatalogRef)
		if err != nil {
			return IssuedSetupHandoff{}, fmt.Errorf("inspect setup handoff catalog candidate: %w", err)
		}
		if catalog.ProviderKey != candidate.ProviderKey || catalog.CatalogRef != candidate.CatalogRef || catalog.SetupIntent == "" || catalog.Transport != "streamable-http" {
			return IssuedSetupHandoff{}, ErrCatalogRejected
		}
		intent = catalog.SetupIntent
	}
	issued, err := s.store.IssueSetupHandoff(ctx, principal, SetupHandoffBinding{
		ProjectID:        project.ID,
		RegistrationID:   registrationID,
		ProviderKey:      candidate.ProviderKey,
		CatalogReference: candidate.CatalogRef,
		Intent:           intent,
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

type RegisterRemoteMCPInput struct {
	ProjectSlug    string
	RemoteURL      string
	DisplayName    string
	IdempotencyKey string
}

type RegisterRemoteMCPResult struct {
	Project           ResolvedProject
	RemoteURL         string
	Receipt           OperationReceipt
	Registration      string
	NextAction        string
	DashboardSetupURL string
}

// RegisterRemoteMCP re-inspects the exact URL before persistence. An inspection
// result from an earlier MCP call is deliberately not trusted as admission
// evidence, and no caller can supply credentials or headers.
func (s *RegistrationService) RegisterRemoteMCP(ctx context.Context, principal Principal, input RegisterRemoteMCPInput) (RegisterRemoteMCPResult, error) {
	if s == nil || s.gate == nil || s.store == nil || s.directRemoteInspector == nil || !s.budgets.Registration.valid() || input.ProjectSlug == "" || input.RemoteURL == "" || input.IdempotencyKey == "" {
		return RegisterRemoteMCPResult{}, ErrRegistrationUnavailable
	}
	if err := s.budgets.Registration.Allow(ctx, principal); err != nil {
		return RegisterRemoteMCPResult{}, err
	}
	enabled, err := s.gate.Enabled(ctx, principal.OrganizationID, input.ProjectSlug)
	if err != nil {
		return RegisterRemoteMCPResult{}, fmt.Errorf("check direct remote registration gate: %w", err)
	}
	if !enabled {
		return RegisterRemoteMCPResult{}, ErrRegistrationUnavailable
	}
	inspection, err := s.directRemoteInspector.Inspect(ctx, input.RemoteURL)
	if err != nil {
		return RegisterRemoteMCPResult{}, err
	}
	if inspection.CanonicalURL == "" || inspection.Transport != "streamable-http" || inspection.Trust != "user_supplied_unreviewed" {
		return RegisterRemoteMCPResult{}, ErrDirectRemoteRejected
	}
	displayName := strings.TrimSpace(input.DisplayName)
	if len(displayName) > directRemoteDisplayNameMaxBytes || strings.IndexFunc(displayName, isDirectRemoteDisplayNameBreak) >= 0 {
		return RegisterRemoteMCPResult{}, ErrRegistrationInvalid
	}
	project, err := s.store.ResolveProject(ctx, principal.OrganizationID, input.ProjectSlug)
	if err != nil {
		return RegisterRemoteMCPResult{}, fmt.Errorf("resolve direct remote registration project: %w", err)
	}
	if err := s.requireEligibleTarget(ctx, principal.OrganizationID, project); err != nil {
		return RegisterRemoteMCPResult{}, err
	}
	if displayName == "" {
		displayName = inspection.CanonicalURL
	}
	if len(displayName) > directRemoteDisplayNameMaxBytes || strings.IndexFunc(displayName, isDirectRemoteDisplayNameBreak) >= 0 {
		return RegisterRemoteMCPResult{}, ErrRegistrationInvalid
	}
	configurationHash := catalogConfigurationHash(CatalogConfigurationValues{"name": displayName})
	request := CatalogRegistrationRequest{
		ProjectSlug:       project.Slug,
		SourceKind:        directRemoteSourceKind,
		CatalogProvider:   directRemoteProviderKey,
		CatalogReference:  inspection.CanonicalURL,
		ConfigurationHash: configurationHash,
		IdempotencyKey:    input.IdempotencyKey,
		InputHash:         catalogRegistrationInputHash(project.Slug, directRemoteSourceKind, directRemoteProviderKey, inspection.CanonicalURL, configurationHash),
	}
	receipt, err := s.store.BeginReceipt(ctx, principal, project, request, s.now())
	if err != nil {
		return RegisterRemoteMCPResult{}, fmt.Errorf("begin direct remote registration receipt: %w", err)
	}
	if !receipt.Replayed || receipt.Status == receiptStatusPending {
		receipt, err = s.store.ConvergeRegistration(ctx, principal, project, request, receipt)
		if err != nil {
			return RegisterRemoteMCPResult{}, fmt.Errorf("converge direct remote registration: %w", err)
		}
	}
	if receipt.ResultCode == receiptResultActiveCap {
		return RegisterRemoteMCPResult{}, ErrRegistrationCap
	}
	if !receipt.RegistrationID.Valid {
		return RegisterRemoteMCPResult{}, ErrRegistrationUnavailable
	}
	if receipt.Status == receiptStatusPending {
		receipt, err = s.store.CompleteRegistration(ctx, principal, project, request, receipt, resolvedCatalogConfiguration{remoteURL: inspection.CanonicalURL, displayName: displayName})
		if err != nil {
			return RegisterRemoteMCPResult{}, fmt.Errorf("complete direct remote registration: %w", err)
		}
	}
	setupURL := ""
	nextAction := "ready"
	if inspection.RequiresDashboardSetup {
		setupURL, err = s.dashboardSettingsURL(ctx, principal, project, receipt.RegistrationID.UUID)
		if err != nil {
			return RegisterRemoteMCPResult{}, fmt.Errorf("resolve direct remote dashboard setup: %w", err)
		}
		nextAction = "secure_dashboard_setup_required"
	}
	s.telemetry.Record(ctx, LifecycleEvent{Operation: "direct_remote_registration", Phase: "complete", Outcome: "succeeded", State: ""})
	return RegisterRemoteMCPResult{Project: project, RemoteURL: inspection.CanonicalURL, Receipt: receipt, Registration: receipt.RegistrationID.UUID.String(), NextAction: nextAction, DashboardSetupURL: setupURL}, nil
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

// isDirectRemoteDisplayNameBreak rejects values that change the structure of a
// rendered line or terminal message. A remote display name crosses dashboard,
// plugin, audit, and MCP surfaces, so accepting only CR/LF is insufficient.
func isDirectRemoteDisplayNameBreak(value rune) bool {
	return unicode.IsControl(value) || value == '\u2028' || value == '\u2029'
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
