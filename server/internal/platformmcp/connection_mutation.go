//nolint:exhaustruct // Mutation inputs only set fields applicable to the selected operation.
package platformmcp

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/constants"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	customdomainsrepo "github.com/speakeasy-api/gram/server/internal/customdomains/repo"
	"github.com/speakeasy-api/gram/server/internal/mcpendpoints"
	mcpendpointsrepo "github.com/speakeasy-api/gram/server/internal/mcpendpoints/repo"
	"github.com/speakeasy-api/gram/server/internal/mcpservers"
	mcpserversrepo "github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
	"github.com/speakeasy-api/gram/server/internal/metamcp"
	"github.com/speakeasy-api/gram/server/internal/networkaccess"
	networkingressrepo "github.com/speakeasy-api/gram/server/internal/networkingress/repo"
	"github.com/speakeasy-api/gram/server/internal/oops"
	organizationsrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	platformrepo "github.com/speakeasy-api/gram/server/internal/platformmcp/repo"
	"github.com/speakeasy-api/gram/server/internal/plugins"
	"github.com/speakeasy-api/gram/server/internal/shadowmcp/admission"
	"github.com/speakeasy-api/gram/server/internal/toolsets"
)

const (
	operationSetMCPAddress       = "set_mcp_address"
	operationSetMCPNetworkAccess = "set_mcp_network_access"
)

var (
	ErrMCPConnectionMutationInvalid  = errors.New("invalid MCP connection mutation")
	ErrMCPConnectionMutationConflict = errors.New("MCP connection settings changed")
	ErrMCPConnectionMutationMissing  = errors.New("MCP connection target not found")
)

type MCPConnectionEndpointWriter interface {
	CreateMcpEndpointInTransaction(context.Context, pgx.Tx, mcpendpoints.CreateMcpEndpointInTransactionInput) (*types.McpEndpoint, error)
	UpdateMcpEndpointAddressInTransaction(context.Context, pgx.Tx, mcpendpoints.UpdateMcpEndpointAddressInput) (*types.McpEndpoint, []uuid.UUID, error)
}

type MCPConnectionMutationService struct {
	db          *pgxpool.Pool
	settings    *MCPConnectionSettingsService
	endpoints   MCPConnectionEndpointWriter
	audit       *audit.Logger
	authorizer  *authz.Engine
	eligibility networkaccess.EligibilityChecker
	publication plugins.PublicationRequests
	publisher   plugins.PluginPublishSignaler
	reconcile   func(context.Context, []uuid.UUID) error
	now         func() time.Time
}

// WithCustomDomainReconcile sets how custom domains whose root a mutation cleared are reconciled after commit.
func (s *MCPConnectionMutationService) WithCustomDomainReconcile(reconcile func(context.Context, []uuid.UUID) error) *MCPConnectionMutationService {
	s.reconcile = reconcile
	return s
}

func NewMCPConnectionMutationService(db *pgxpool.Pool, settings *MCPConnectionSettingsService, endpoints MCPConnectionEndpointWriter, auditLogger *audit.Logger, authorizer *authz.Engine, eligibility networkaccess.EligibilityChecker, publication plugins.PublicationRequests, publisher plugins.PluginPublishSignaler) (*MCPConnectionMutationService, error) {
	if db == nil || settings == nil || endpoints == nil || auditLogger == nil || authorizer == nil || eligibility == nil {
		return nil, ErrMCPConnectionMutationInvalid
	}
	return &MCPConnectionMutationService{db: db, settings: settings, endpoints: endpoints, audit: auditLogger, authorizer: authorizer, eligibility: eligibility, publication: publication, publisher: publisher, reconcile: nil, now: time.Now}, nil
}

type SetMCPAddressInput struct {
	ProjectID       string                          `json:"project_id" jsonschema:"project ID that owns the target"`
	TargetKind      MCPConnectionSettingsTargetKind `json:"target_kind" jsonschema:"exact target type: mcp_server or gateway"`
	TargetID        string                          `json:"target_id" jsonschema:"ID of the exact MCP server or gateway"`
	EndpointID      string                          `json:"endpoint_id,omitempty" jsonschema:"exact endpoint ID from get_mcp_connection_settings to change; omit to add a new endpoint"`
	Slug            string                          `json:"slug" jsonschema:"endpoint slug; on the platform domain it starts with the organization slug and a hyphen"`
	CustomDomainID  string                          `json:"custom_domain_id,omitempty" jsonschema:"ID of the organization's custom domain for the endpoint; omit for the platform domain"`
	ExpectedVersion string                          `json:"expected_version" jsonschema:"version from the latest get_mcp_connection_settings for this target"`
	IdempotencyKey  string                          `json:"idempotency_key" jsonschema:"caller-chosen key, at most 128 characters, reused only to retry this exact change"`
	Confirmed       bool                            `json:"confirmed" jsonschema:"false to preview the change without writing; true only after the user confirms the exact change the preview shows"`
}

type SetMCPNetworkAccessInput struct {
	ProjectID       string                          `json:"project_id" jsonschema:"project ID that owns the target"`
	TargetKind      MCPConnectionSettingsTargetKind `json:"target_kind" jsonschema:"exact target type: mcp_server or gateway"`
	TargetID        string                          `json:"target_id" jsonschema:"ID of the exact MCP server or gateway"`
	Mode            string                          `json:"mode" jsonschema:"requested network access: public_only, dual, or private_only"`
	ExpectedVersion string                          `json:"expected_version" jsonschema:"version from the latest get_mcp_connection_settings for this target"`
	IdempotencyKey  string                          `json:"idempotency_key" jsonschema:"caller-chosen key, at most 128 characters, reused only to retry this exact change"`
	Confirmed       bool                            `json:"confirmed" jsonschema:"false to preview the change without writing; true only after the user confirms the exact change the preview shows"`
}

// MCPConnectionMutationPreview is what an unconfirmed call reports: the current settings and the exact change a
// confirmed call with the same input would make. Nothing is written for it.
type MCPConnectionMutationPreview struct {
	ProjectID         string                          `json:"project_id"`
	TargetKind        MCPConnectionSettingsTargetKind `json:"target_kind"`
	TargetID          string                          `json:"target_id"`
	Name              string                          `json:"name"`
	Version           string                          `json:"version"`
	Change            string                          `json:"change"`
	EndpointBefore    *MCPConnectionEndpoint          `json:"endpoint_before,omitempty"`
	EndpointAfter     *MCPConnectionEndpoint          `json:"endpoint_after,omitempty"`
	NetworkModeBefore string                          `json:"network_mode_before"`
	NetworkModeAfter  string                          `json:"network_mode_after"`
	Endpoints         []MCPConnectionEndpoint         `json:"endpoints"`
	Ingress           *MCPConnectionIngress           `json:"ingress,omitempty"`
	PluginMemberships []MCPConnectionPluginMembership `json:"plugin_memberships"`
}

// connectionMutationPreviewError is the confirmation_required refusal of an unconfirmed call.
type connectionMutationPreviewError struct {
	Preview MCPConnectionMutationPreview
}

func (e *connectionMutationPreviewError) Error() string {
	return "Nothing was changed. Show the user the exact target and the change in this preview, then call again with the same input, the same idempotency key, and confirmed: true once they confirm."
}

func (e *connectionMutationPreviewError) Unwrap() error { return ErrMCPConnectionMutationInvalid }

type MCPConnectionMutationOutput struct {
	Settings           *MCPConnectionSettings  `json:"settings,omitempty"`
	SnapshotScope      string                  `json:"snapshot_scope"`
	PublicationRequest string                  `json:"publication_request"`
	PublishSignal      string                  `json:"publish_signal"`
	Receipt            RiskMutationToolReceipt `json:"receipt"`
}

type connectionMutationInput struct {
	ProjectID       string                          `json:"project_id"`
	TargetKind      MCPConnectionSettingsTargetKind `json:"target_kind"`
	TargetID        string                          `json:"target_id"`
	ExpectedVersion string                          `json:"expected_version"`
	EndpointID      string                          `json:"endpoint_id,omitempty"`
	Slug            string                          `json:"slug,omitempty"`
	CustomDomainID  string                          `json:"custom_domain_id,omitempty"`
	Mode            string                          `json:"mode,omitempty"`
}

type connectionMutationReceipt struct {
	PublicationRequest string `json:"publication_request"`
	// ClearedRootDomainIDs are reconciled after commit, so a replay can retry a failed reconcile.
	ClearedRootDomainIDs []uuid.UUID `json:"cleared_root_domain_ids,omitempty"`
}

func (s *MCPConnectionMutationService) SetAddress(ctx context.Context, principal Principal, input SetMCPAddressInput) (MCPConnectionMutationOutput, error) {
	if s == nil {
		return MCPConnectionMutationOutput{}, connectionMutationInvalid("The connection mutation service is unavailable.")
	}
	input.ProjectID, input.TargetID = strings.TrimSpace(input.ProjectID), strings.TrimSpace(input.TargetID)
	input.EndpointID, input.Slug = strings.TrimSpace(input.EndpointID), strings.TrimSpace(input.Slug)
	input.CustomDomainID, input.ExpectedVersion = strings.TrimSpace(input.CustomDomainID), strings.TrimSpace(input.ExpectedVersion)
	input.IdempotencyKey = strings.TrimSpace(input.IdempotencyKey)
	kind, targetID, project, current, err := s.validateInput(ctx, principal, input.ProjectID, input.TargetKind, input.TargetID, input.ExpectedVersion, input.IdempotencyKey)
	if err != nil {
		return MCPConnectionMutationOutput{}, err
	}
	if !constants.SlugPatternRE.MatchString(input.Slug) {
		return MCPConnectionMutationOutput{}, connectionMutationInvalid("Provide an endpoint slug of 1 to 128 lowercase letters, digits, hyphens or underscores.")
	}
	endpointID := uuid.Nil
	if input.EndpointID != "" {
		endpointID, err = uuid.Parse(input.EndpointID)
		if err != nil {
			return MCPConnectionMutationOutput{}, connectionMutationInvalid("Provide the exact endpoint ID from the latest connection settings.")
		}
	}
	domainID := uuid.NullUUID{}
	if input.CustomDomainID != "" {
		parsed, parseErr := uuid.Parse(input.CustomDomainID)
		if parseErr != nil {
			return MCPConnectionMutationOutput{}, connectionMutationInvalid("Provide a valid custom domain ID.")
		}
		domainID = uuid.NullUUID{UUID: parsed, Valid: true}
	}
	if (endpointID == uuid.Nil) != (input.EndpointID == "") {
		return MCPConnectionMutationOutput{}, connectionMutationInvalid("Provide either no endpoint ID to create an address or the exact endpoint ID to update.")
	}
	if !input.Confirmed {
		return MCPConnectionMutationOutput{}, s.addressPreview(ctx, principal, current, input.ExpectedVersion, endpointID, input.Slug, domainID)
	}
	normalized := connectionMutationInput{ProjectID: project.ID.String(), TargetKind: kind, TargetID: targetID.String(), ExpectedVersion: input.ExpectedVersion, EndpointID: input.EndpointID, Slug: input.Slug, CustomDomainID: input.CustomDomainID}
	result, receipt, err := s.execute(ctx, principal, project, operationSetMCPAddress, input.IdempotencyKey, normalized, func(ctx context.Context, tx pgx.Tx) (connectionMutationReceipt, error) {
		settings, err := s.lockAndCheck(ctx, tx, principal, project, kind, targetID, input.ExpectedVersion)
		if err != nil {
			return connectionMutationReceipt{}, err
		}
		if err := checkPrivateOnlyAddress(settings, domainID); err != nil {
			return connectionMutationReceipt{}, err
		}
		organization, err := organizationsrepo.New(tx).GetOrganizationMetadata(ctx, principal.OrganizationID)
		if err != nil || organization.Slug == "" {
			return connectionMutationReceipt{}, connectionMutationUnavailable(errors.Join(errors.New("organization slug unavailable"), err))
		}
		if err := checkPlatformSlug(organization.Slug, input.Slug, domainID); err != nil {
			return connectionMutationReceipt{}, err
		}
		// Account type drives the same slug prefix rule a dashboard session applies.
		authCtx := &contextvalues.AuthContext{ActiveOrganizationID: principal.OrganizationID, UserID: principal.UserID, ProjectID: &project.ID, OrganizationSlug: organization.Slug, AccountType: organization.GramAccountType}
		if endpointID == uuid.Nil {
			var create mcpendpoints.CreateMcpEndpointInTransactionInput
			create.AuthContext, create.CustomDomainID, create.Slug = authCtx, domainID, input.Slug
			if kind == MCPConnectionSettingsMCPServer {
				create.McpServerID = uuid.NullUUID{UUID: targetID, Valid: true}
			} else {
				create.MetaMcpServerID = uuid.NullUUID{UUID: targetID, Valid: true}
			}
			if _, err := s.endpoints.CreateMcpEndpointInTransaction(ctx, tx, create); err != nil {
				return connectionMutationReceipt{}, classifyConnectionMutationError(err)
			}
		} else {
			current, err := mcpendpointsrepo.New(tx).GetMCPEndpointByID(ctx, mcpendpointsrepo.GetMCPEndpointByIDParams{ID: endpointID, ProjectID: project.ID})
			if errors.Is(err, pgx.ErrNoRows) || err == nil && !endpointBelongsToTarget(&current, kind, targetID) {
				return connectionMutationReceipt{}, ErrMCPConnectionMutationMissing
			}
			if err != nil {
				return connectionMutationReceipt{}, fmt.Errorf("load exact connection endpoint: %w", err)
			}
			endpointInput := mcpendpoints.UpdateMcpEndpointAddressInput{AuthContext: authCtx, EndpointID: endpointID, CustomDomainID: domainID, Slug: input.Slug}
			if _, domains, err := s.endpoints.UpdateMcpEndpointAddressInTransaction(ctx, tx, endpointInput); err != nil {
				return connectionMutationReceipt{}, classifyConnectionMutationError(err)
			} else if len(domains) != 0 {
				return connectionMutationReceipt{}, connectionMutationInvalid(rootMoveMessage)
			}
		}
		outcome, err := s.publication.ProjectWithOutcome(ctx, tx, principal.OrganizationID, project.ID, principal.UserID)
		if err != nil {
			return connectionMutationReceipt{}, fmt.Errorf("request MCP address publication: %w", err)
		}
		return connectionMutationReceipt{PublicationRequest: string(outcome)}, nil
	})
	if err != nil {
		return MCPConnectionMutationOutput{}, err
	}
	return s.finish(ctx, principal, project.ID, input.ProjectID, kind, targetID, result, receipt)
}

func (s *MCPConnectionMutationService) SetNetworkAccess(ctx context.Context, principal Principal, input SetMCPNetworkAccessInput) (MCPConnectionMutationOutput, error) {
	if s == nil {
		return MCPConnectionMutationOutput{}, connectionMutationInvalid("The connection mutation service is unavailable.")
	}
	input.ProjectID, input.TargetID = strings.TrimSpace(input.ProjectID), strings.TrimSpace(input.TargetID)
	input.Mode, input.ExpectedVersion = strings.TrimSpace(input.Mode), strings.TrimSpace(input.ExpectedVersion)
	input.IdempotencyKey = strings.TrimSpace(input.IdempotencyKey)
	mode, err := networkaccess.Parse(input.Mode)
	if err != nil {
		return MCPConnectionMutationOutput{}, connectionMutationInvalid("Mode must be public_only, dual, or private_only.")
	}
	kind, targetID, project, current, err := s.validateInput(ctx, principal, input.ProjectID, input.TargetKind, input.TargetID, input.ExpectedVersion, input.IdempotencyKey)
	if err != nil {
		return MCPConnectionMutationOutput{}, err
	}
	if !input.Confirmed {
		return MCPConnectionMutationOutput{}, networkAccessPreview(current, input.ExpectedVersion, mode)
	}
	normalized := connectionMutationInput{ProjectID: project.ID.String(), TargetKind: kind, TargetID: targetID.String(), ExpectedVersion: input.ExpectedVersion, Mode: string(mode)}
	result, receipt, err := s.execute(ctx, principal, project, operationSetMCPNetworkAccess, input.IdempotencyKey, normalized, func(ctx context.Context, tx pgx.Tx) (connectionMutationReceipt, error) {
		var clearedRootDomainIDs []uuid.UUID
		prepared, err := s.eligibility.PrepareNetworkAccess(ctx, networkaccess.EligibilityInput{OrganizationID: principal.OrganizationID, Mode: mode})
		if err != nil {
			return connectionMutationReceipt{}, privateAccessUnavailable(err)
		}
		finalize := networkaccess.NewAdmissionFinalizer(func(ctx context.Context, tx pgx.Tx) error {
			if err := prepared.Finalize(ctx, tx); err != nil {
				return privateAccessUnavailable(err)
			}
			return nil
		})
		if err := networkingressrepo.New(tx).AcquireNetworkIngressOrganizationLock(ctx, principal.OrganizationID); err != nil {
			return connectionMutationReceipt{}, fmt.Errorf("lock network ingress lifecycle: %w", err)
		}
		if _, err := s.lockAndCheck(ctx, tx, principal, project, kind, targetID, input.ExpectedVersion); err != nil {
			return connectionMutationReceipt{}, err
		}
		if kind == MCPConnectionSettingsMCPServer {
			server, lookupErr := mcpserversrepo.New(tx).GetMCPServerByIDAndProjectID(ctx, mcpserversrepo.GetMCPServerByIDAndProjectIDParams{ID: targetID, ProjectID: project.ID})
			if lookupErr != nil {
				return connectionMutationReceipt{}, classifyConnectionMutationError(lookupErr)
			}
			if server.ToolsetID.Valid && server.ToolsetID.UUID == server.ID {
				if err := finalize.Finalize(ctx, tx); err != nil {
					return connectionMutationReceipt{}, fmt.Errorf("finalize network access admission: %w", err)
				}
				actor := &contextvalues.AuthContext{ActiveOrganizationID: principal.OrganizationID, UserID: principal.UserID, ProjectID: &project.ID}
				clearedRootDomainIDs, err = toolsets.SetHostedNetworkAccessInTransaction(ctx, tx, s.audit, actor, targetID, mode)
			} else {
				_, err = mcpservers.UpdateMCPServerNetworkAccessModeInTransaction(ctx, tx, s.audit, mcpservers.LifecycleUpdateInput{OrganizationID: principal.OrganizationID, ProjectID: project.ID, ActorUserID: principal.UserID, ServerID: targetID}, mode, finalize)
			}
		} else {
			_, err = metamcp.UpdateMetaMCPServerNetworkAccessModeInTransaction(ctx, tx, s.audit, principal.OrganizationID, project.ID, principal.UserID, nil, targetID, mode, finalize)
		}
		if err != nil {
			return connectionMutationReceipt{}, classifyConnectionMutationError(err)
		}
		outcome, err := s.publication.ProjectWithOutcome(ctx, tx, principal.OrganizationID, project.ID, principal.UserID)
		if err != nil {
			return connectionMutationReceipt{}, fmt.Errorf("request MCP network access publication: %w", err)
		}
		return connectionMutationReceipt{PublicationRequest: string(outcome), ClearedRootDomainIDs: clearedRootDomainIDs}, nil
	})
	if err != nil {
		return MCPConnectionMutationOutput{}, err
	}
	if s.reconcile != nil && len(result.ClearedRootDomainIDs) > 0 {
		if err := s.reconcile(ctx, result.ClearedRootDomainIDs); err != nil {
			return MCPConnectionMutationOutput{}, fmt.Errorf("reconcile hosted MCP custom domains: %w", err)
		}
	}
	return s.finish(ctx, principal, project.ID, input.ProjectID, kind, targetID, result, receipt)
}

func (s *MCPConnectionMutationService) validateInput(ctx context.Context, principal Principal, projectID string, kind MCPConnectionSettingsTargetKind, targetID, expectedVersion, idempotencyKey string) (MCPConnectionSettingsTargetKind, uuid.UUID, ResolvedProject, MCPConnectionSettings, error) {
	fail := func(err error) (MCPConnectionSettingsTargetKind, uuid.UUID, ResolvedProject, MCPConnectionSettings, error) {
		return "", uuid.Nil, ResolvedProject{}, MCPConnectionSettings{}, err
	}
	if s == nil || s.db == nil || s.settings == nil || s.endpoints == nil || s.audit == nil || s.authorizer == nil || s.eligibility == nil || s.now == nil || principal.OrganizationID == "" || principal.UserID == "" || len(idempotencyKey) == 0 || len(idempotencyKey) > 128 || len(expectedVersion) != sha256.Size*2 {
		return fail(connectionMutationInvalid("The connection mutation request is incomplete or invalid."))
	}
	projectID = strings.TrimSpace(projectID)
	parsedProject, err := uuid.Parse(projectID)
	if err != nil || (kind != MCPConnectionSettingsMCPServer && kind != MCPConnectionSettingsGateway) {
		return fail(connectionMutationInvalid("Provide an explicit project ID, target ID, and target kind."))
	}
	parsedTarget, err := uuid.Parse(strings.TrimSpace(targetID))
	if err != nil {
		return fail(connectionMutationInvalid("Provide a valid exact target ID."))
	}
	project, err := platformrepo.New(s.db).ResolvePlatformMCPProjectByID(ctx, platformrepo.ResolvePlatformMCPProjectByIDParams{OrganizationID: principal.OrganizationID, ProjectID: parsedProject})
	if errors.Is(err, pgx.ErrNoRows) {
		return fail(ErrMCPConnectionMutationMissing)
	}
	if err != nil {
		return fail(fmt.Errorf("resolve live connection mutation project: %w", err))
	}
	if err := s.authorizer.Require(ctx, authz.Check{Scope: authz.ScopeMCPWrite, ResourceID: project.ID.String()}); err != nil {
		return fail(connectionMutationAuthorizationError(err, authz.ScopeMCPWrite))
	}
	if err := s.authorizer.Require(ctx, authz.Check{Scope: authz.ScopeOrgAdmin, ResourceID: principal.OrganizationID}); err != nil {
		return fail(connectionMutationAuthorizationError(err, authz.ScopeOrgAdmin))
	}
	settings, err := s.settings.Get(ctx, principal, GetMCPConnectionSettingsInput{ProjectID: project.ID.String(), TargetKind: kind, TargetID: parsedTarget.String()})
	if errors.Is(err, ErrMCPConnectionSettingsNotFound) {
		return fail(ErrMCPConnectionMutationMissing)
	}
	if err != nil {
		return fail(err)
	}
	return kind, parsedTarget, ResolvedProject{ID: project.ID, Name: project.Name, Slug: project.Slug}, settings, nil
}

// addressPreview reports the address change a confirmed call would make, against the settings it would lock. It
// refuses what the confirmed call would refuse from this data; the endpoint writer's rules run only on confirm.
func (s *MCPConnectionMutationService) addressPreview(ctx context.Context, principal Principal, current MCPConnectionSettings, expectedVersion string, endpointID uuid.UUID, slug string, domainID uuid.NullUUID) error {
	if !hmac.Equal([]byte(current.Version), []byte(expectedVersion)) {
		return ErrMCPConnectionMutationConflict
	}
	organization, err := organizationsrepo.New(s.db).GetOrganizationMetadata(ctx, principal.OrganizationID)
	if err != nil {
		return connectionMutationUnavailable(err)
	}
	preview := newConnectionMutationPreview(current)
	after := &MCPConnectionEndpoint{Slug: slug}
	if domainID.Valid {
		domain, err := customdomainsrepo.New(s.db).GetCustomDomainByIDAndOrganization(ctx, customdomainsrepo.GetCustomDomainByIDAndOrganizationParams{ID: domainID.UUID, OrganizationID: principal.OrganizationID})
		if errors.Is(err, pgx.ErrNoRows) {
			return connectionMutationInvalid("Provide the ID of one of the organization's custom domains.")
		}
		if err != nil {
			return connectionMutationUnavailable(err)
		}
		after.CustomDomainID, after.Domain = domainID.UUID.String(), domain.Domain
	}
	if err := checkPlatformSlug(organization.Slug, slug, domainID); err != nil {
		return err
	}
	if err := checkPrivateOnlyAddress(current, domainID); err != nil {
		return err
	}
	if endpointID == uuid.Nil {
		preview.Change = "add_endpoint"
	} else {
		preview.Change = "change_endpoint"
		for i := range current.Endpoints {
			if current.Endpoints[i].ID == endpointID.String() {
				before := current.Endpoints[i]
				preview.EndpointBefore = &before
			}
		}
		if preview.EndpointBefore == nil {
			return ErrMCPConnectionMutationMissing
		}
		sameDomain := after.CustomDomainID == preview.EndpointBefore.CustomDomainID
		if preview.EndpointBefore.IsDomainRoot && !sameDomain {
			return connectionMutationInvalid(rootMoveMessage)
		}
		after.ID, after.IsDomainRoot = endpointID.String(), preview.EndpointBefore.IsDomainRoot
	}
	preview.EndpointAfter = after
	return &connectionMutationPreviewError{Preview: preview}
}

// checkPlatformSlug keeps an address on the platform domain under the organization's slug prefix.
func checkPlatformSlug(organizationSlug, slug string, domainID uuid.NullUUID) error {
	if !domainID.Valid && !strings.HasPrefix(slug, organizationSlug+"-") {
		return connectionMutationInvalid(fmt.Sprintf("A platform address slug must start with the organization slug %q followed by a hyphen.", organizationSlug))
	}
	return nil
}

const rootMoveMessage = "Moving a domain-root endpoint requires the dashboard so its domain can be reconciled."

// checkPrivateOnlyAddress keeps a private-only target's addresses inside its network ingress namespace.
func checkPrivateOnlyAddress(settings MCPConnectionSettings, domainID uuid.NullUUID) error {
	if settings.NetworkMode != "private_only" {
		return nil
	}
	if settings.Ingress == nil || !settings.Ingress.Enabled || settings.Ingress.DNSName == "" {
		return connectionMutationInvalid("A ready network ingress is required to change a private-only MCP address.")
	}
	if !endpointMatchesIngress(domainID, *settings.Ingress) {
		return connectionMutationInvalid("A private-only MCP address must use the network ingress namespace.")
	}
	return nil
}

// networkAccessPreview reports the network mode change a confirmed call would make.
func networkAccessPreview(current MCPConnectionSettings, expectedVersion string, mode networkaccess.Mode) error {
	if !hmac.Equal([]byte(current.Version), []byte(expectedVersion)) {
		return ErrMCPConnectionMutationConflict
	}
	preview := newConnectionMutationPreview(current)
	preview.Change = "set_network_access"
	preview.NetworkModeAfter = string(mode)
	return &connectionMutationPreviewError{Preview: preview}
}

func newConnectionMutationPreview(current MCPConnectionSettings) MCPConnectionMutationPreview {
	return MCPConnectionMutationPreview{
		ProjectID: current.ProjectID, TargetKind: current.TargetKind, TargetID: current.TargetID, Name: current.Name, Version: current.Version,
		NetworkModeBefore: current.NetworkMode, NetworkModeAfter: current.NetworkMode,
		Endpoints: current.Endpoints, Ingress: current.Ingress, PluginMemberships: current.PluginMemberships,
	}
}

func (s *MCPConnectionMutationService) lockAndCheck(ctx context.Context, tx pgx.Tx, principal Principal, project ResolvedProject, kind MCPConnectionSettingsTargetKind, targetID uuid.UUID, expectedVersion string) (MCPConnectionSettings, error) {
	if err := networkingressrepo.New(tx).AcquireNetworkIngressOrganizationLock(ctx, principal.OrganizationID); err != nil {
		return MCPConnectionSettings{}, fmt.Errorf("lock network ingress lifecycle: %w", err)
	}
	if err := admission.LockProject(ctx, tx, project.ID); err != nil {
		return MCPConnectionSettings{}, fmt.Errorf("lock connection settings project: %w", err)
	}
	settings, err := s.settings.GetInTx(ctx, tx, principal, GetMCPConnectionSettingsInput{ProjectID: project.ID.String(), TargetKind: kind, TargetID: targetID.String()})
	if errors.Is(err, ErrMCPConnectionSettingsNotFound) {
		return MCPConnectionSettings{}, ErrMCPConnectionMutationMissing
	}
	if err != nil {
		return MCPConnectionSettings{}, err
	}
	if !hmac.Equal([]byte(settings.Version), []byte(expectedVersion)) {
		return MCPConnectionSettings{}, ErrMCPConnectionMutationConflict
	}
	return settings, nil
}

func (s *MCPConnectionMutationService) execute(ctx context.Context, principal Principal, project ResolvedProject, operation, key string, normalized connectionMutationInput, mutate func(context.Context, pgx.Tx) (connectionMutationReceipt, error)) (connectionMutationReceipt, OperationReceipt, error) {
	payload, err := json.Marshal(normalized)
	if err != nil {
		return connectionMutationReceipt{}, OperationReceipt{}, connectionMutationInvalid("The connection mutation could not be normalized.")
	}
	digest := sha256.Sum256(append([]byte("platform-mcp-connection-mutation-v1\x00"+operation+"\x00"), payload...))
	receipt, err := executeMutationReceipt(ctx, mutationReceiptExecution[connectionMutationReceipt]{
		DB: s.db, Now: s.now, Principal: principal, Project: project, Operation: operation, IdempotencyKey: key, InputHash: hex.EncodeToString(digest[:]), Label: "MCP connection mutation",
		Invalid: func(error) error { return connectionMutationInvalid("The connection mutation request is invalid.") }, Conflict: func(message string) error { return fmt.Errorf("%w: %s", ErrMCPConnectionMutationConflict, message) }, Unavailable: connectionMutationUnavailable,
		ValidateReplay: validConnectionMutationReceipt, EncodeResult: encodeConnectionMutationReceipt, Mutate: mutate,
	})
	if err != nil {
		return connectionMutationReceipt{}, OperationReceipt{}, err
	}
	result, err := decodeConnectionMutationReceipt(receipt.ResultPayload)
	return result, receipt, err
}

func (s *MCPConnectionMutationService) finish(ctx context.Context, principal Principal, projectID uuid.UUID, projectIDText string, kind MCPConnectionSettingsTargetKind, targetID uuid.UUID, stored connectionMutationReceipt, receipt OperationReceipt) (MCPConnectionMutationOutput, error) {
	output := MCPConnectionMutationOutput{PublicationRequest: stored.PublicationRequest, PublishSignal: "not_requested", Receipt: riskMutationToolReceipt(receipt)}
	if stored.PublicationRequest != string(plugins.ProjectPublicationEnqueued) {
		if s.publisher == nil {
			output.PublishSignal = "unavailable"
		} else if err := plugins.SignalPluginPublishAfterRequest(ctx, s.publisher, plugins.ProjectPublicationRequestOutcome(stored.PublicationRequest), projectID, principal.UserID); err != nil {
			output.PublishSignal = "request_failed"
		} else {
			output.PublishSignal = "best_effort_requested"
		}
	}
	settings, err := s.settings.Get(ctx, principal, GetMCPConnectionSettingsInput{ProjectID: projectIDText, TargetKind: kind, TargetID: targetID.String()})
	if err != nil {
		output.SnapshotScope = "verification_unavailable"
		return output, nil
	}
	output.Settings = &settings
	output.SnapshotScope = "fresh_read_after_commit"
	return output, nil
}

func endpointMatchesIngress(domainID uuid.NullUUID, ingress MCPConnectionIngress) bool {
	switch ingress.NamespaceKind {
	case "platform":
		return !domainID.Valid
	case "custom_domain":
		return domainID.Valid && domainID.UUID.String() == ingress.CustomDomainID
	default:
		return false
	}
}

func endpointBelongsToTarget(endpoint *mcpendpointsrepo.McpEndpoint, kind MCPConnectionSettingsTargetKind, targetID uuid.UUID) bool {
	if endpoint == nil {
		return false
	}
	if kind == MCPConnectionSettingsMCPServer {
		return endpoint.McpServerID.Valid && endpoint.McpServerID.UUID == targetID && !endpoint.MetaMcpServerID.Valid
	}
	return endpoint.MetaMcpServerID.Valid && endpoint.MetaMcpServerID.UUID == targetID && !endpoint.McpServerID.Valid
}

func projectOrganizationSlug(ctx context.Context, db *pgxpool.Pool, organizationID string) string {
	organization, err := NewPostgresOrganizationSlugResolver(db).OrganizationSlug(ctx, organizationID)
	if err != nil {
		return ""
	}
	return organization
}

func classifyConnectionMutationError(err error) error {
	if shareable, ok := errors.AsType[*oops.ShareableError](err); ok {
		switch shareable.Code {
		case oops.CodeNotFound:
			return ErrMCPConnectionMutationMissing
		case oops.CodeInvalid, oops.CodeBadRequest:
			return &RiskMutationError{Code: "invalid_request", Message: shareable.Error(), Cause: errors.Join(ErrMCPConnectionMutationInvalid, err)}
		case oops.CodeForbidden:
			return &RiskMutationError{Code: "forbidden", Message: "You do not have permission to change this MCP connection.", Cause: err}
		case oops.CodeConflict:
			return &RiskMutationError{Code: "conflict", Message: shareable.Error(), Cause: errors.Join(ErrMCPConnectionMutationConflict, err)}
		default:
			return connectionMutationUnavailable(err)
		}
	}
	return fmt.Errorf("mutate MCP connection settings: %w", err)
}

func connectionMutationAuthorizationError(err error, scope authz.Scope) error {
	if shareable, ok := errors.AsType[*oops.ShareableError](err); ok && shareable.Code == oops.CodeForbidden {
		return &ExternalAuthorizationError{RequiredScope: string(scope), cause: err}
	}
	return err
}

func connectionMutationInvalid(message string) error {
	return &RiskMutationError{Code: "invalid_request", Message: message, Cause: ErrMCPConnectionMutationInvalid}
}

// privateAccessUnavailable refuses a private mode the organization could not be admitted to. The admission check
// does not tell a missing entitlement or ingress from a passing failure, so the message covers both.
func privateAccessUnavailable(cause error) error {
	return &RiskMutationError{Code: "invalid_request", Message: "Private network access could not be admitted for this organization. It needs private networking switched on and an enabled network ingress: check get_network_ingress, and if it reports ready, retry later.", Cause: errors.Join(ErrMCPConnectionMutationInvalid, cause)}
}

func connectionMutationUnavailable(cause error) error {
	return &RiskMutationError{Code: unavailableCode, Message: "MCP connection settings are temporarily unavailable.", Cause: errors.Join(ErrUnavailable, cause)}
}

func validConnectionMutationReceipt(payload []byte) bool {
	var result connectionMutationReceipt
	if json.Unmarshal(payload, &result) != nil {
		return false
	}
	return result.PublicationRequest == string(plugins.ProjectPublicationEmissionDisabled) || result.PublicationRequest == string(plugins.ProjectPublicationNotConfigured) || result.PublicationRequest == string(plugins.ProjectPublicationEnqueued)
}

func encodeConnectionMutationReceipt(result connectionMutationReceipt) ([]byte, error) {
	if !validConnectionMutationReceipt(mustJSON(result)) {
		return nil, connectionMutationUnavailable(errors.New("invalid connection mutation receipt"))
	}
	payload, err := json.Marshal(result)
	if err != nil {
		return nil, fmt.Errorf("encode connection mutation receipt: %w", err)
	}
	return payload, nil
}

func decodeConnectionMutationReceipt(payload []byte) (connectionMutationReceipt, error) {
	var result connectionMutationReceipt
	if !validConnectionMutationReceipt(payload) {
		return result, connectionMutationUnavailable(errors.New("invalid stored connection mutation receipt"))
	}
	if err := json.Unmarshal(payload, &result); err != nil {
		return result, fmt.Errorf("decode connection mutation receipt: %w", err)
	}
	return result, nil
}

func mustJSON(value any) []byte {
	payload, _ := json.Marshal(value)
	return payload
}
