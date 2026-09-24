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
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/mcpendpoints"
	mcpendpointsrepo "github.com/speakeasy-api/gram/server/internal/mcpendpoints/repo"
	"github.com/speakeasy-api/gram/server/internal/mcpservers"
	"github.com/speakeasy-api/gram/server/internal/metamcp"
	"github.com/speakeasy-api/gram/server/internal/networkaccess"
	networkingressrepo "github.com/speakeasy-api/gram/server/internal/networkingress/repo"
	"github.com/speakeasy-api/gram/server/internal/oops"
	platformrepo "github.com/speakeasy-api/gram/server/internal/platformmcp/repo"
	"github.com/speakeasy-api/gram/server/internal/plugins"
	"github.com/speakeasy-api/gram/server/internal/shadowmcp/admission"
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
	now         func() time.Time
}

func NewMCPConnectionMutationService(db *pgxpool.Pool, settings *MCPConnectionSettingsService, endpoints MCPConnectionEndpointWriter, auditLogger *audit.Logger, authorizer *authz.Engine, eligibility networkaccess.EligibilityChecker, publication plugins.PublicationRequests, publisher plugins.PluginPublishSignaler) (*MCPConnectionMutationService, error) {
	if db == nil || settings == nil || endpoints == nil || auditLogger == nil || authorizer == nil || eligibility == nil {
		return nil, ErrMCPConnectionMutationInvalid
	}
	return &MCPConnectionMutationService{db: db, settings: settings, endpoints: endpoints, audit: auditLogger, authorizer: authorizer, eligibility: eligibility, publication: publication, publisher: publisher, now: time.Now}, nil
}

type SetMCPAddressInput struct {
	ProjectID       string                          `json:"project_id"`
	TargetKind      MCPConnectionSettingsTargetKind `json:"target_kind"`
	TargetID        string                          `json:"target_id"`
	EndpointID      string                          `json:"endpoint_id,omitempty"`
	Slug            string                          `json:"slug"`
	CustomDomainID  string                          `json:"custom_domain_id,omitempty"`
	ExpectedVersion string                          `json:"expected_version"`
	IdempotencyKey  string                          `json:"idempotency_key"`
	Confirmed       bool                            `json:"confirmed"`
}

type SetMCPNetworkAccessInput struct {
	ProjectID       string                          `json:"project_id"`
	TargetKind      MCPConnectionSettingsTargetKind `json:"target_kind"`
	TargetID        string                          `json:"target_id"`
	Mode            string                          `json:"mode"`
	ExpectedVersion string                          `json:"expected_version"`
	IdempotencyKey  string                          `json:"idempotency_key"`
	Confirmed       bool                            `json:"confirmed"`
}

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
}

func (s *MCPConnectionMutationService) SetAddress(ctx context.Context, principal Principal, input SetMCPAddressInput) (MCPConnectionMutationOutput, error) {
	if s == nil {
		return MCPConnectionMutationOutput{}, connectionMutationInvalid("The connection mutation service is unavailable.")
	}
	input.ProjectID, input.TargetID = strings.TrimSpace(input.ProjectID), strings.TrimSpace(input.TargetID)
	input.EndpointID, input.Slug = strings.TrimSpace(input.EndpointID), strings.TrimSpace(input.Slug)
	input.CustomDomainID, input.ExpectedVersion = strings.TrimSpace(input.CustomDomainID), strings.TrimSpace(input.ExpectedVersion)
	input.IdempotencyKey = strings.TrimSpace(input.IdempotencyKey)
	if !input.Confirmed {
		return MCPConnectionMutationOutput{}, connectionMutationInvalid("Confirm the exact project, target, endpoint address, and expected settings version before changing the address.")
	}
	kind, targetID, project, err := s.validateInput(ctx, principal, input.ProjectID, input.TargetKind, input.TargetID, input.ExpectedVersion, input.IdempotencyKey)
	if err != nil {
		return MCPConnectionMutationOutput{}, err
	}
	if input.Slug == "" || len(input.Slug) > 253 {
		return MCPConnectionMutationOutput{}, connectionMutationInvalid("Provide a valid endpoint slug.")
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
	normalized := connectionMutationInput{ProjectID: project.ID.String(), TargetKind: kind, TargetID: targetID.String(), ExpectedVersion: input.ExpectedVersion, EndpointID: input.EndpointID, Slug: input.Slug, CustomDomainID: input.CustomDomainID}
	result, receipt, err := s.execute(ctx, principal, project, operationSetMCPAddress, input.IdempotencyKey, normalized, func(ctx context.Context, tx pgx.Tx) (connectionMutationReceipt, error) {
		settings, err := s.lockAndCheck(ctx, tx, principal, project, kind, targetID, input.ExpectedVersion)
		if err != nil {
			return connectionMutationReceipt{}, err
		}
		if settings.NetworkMode == "private_only" {
			if settings.Ingress == nil || !settings.Ingress.Enabled || settings.Ingress.DNSName == "" {
				return connectionMutationReceipt{}, connectionMutationInvalid("A ready network ingress is required to change a private-only MCP address.")
			}
			if !endpointMatchesIngress(domainID, *settings.Ingress) {
				return connectionMutationReceipt{}, connectionMutationInvalid("A private-only MCP address must use the network ingress namespace.")
			}
		}
		authCtx := &contextvalues.AuthContext{ActiveOrganizationID: principal.OrganizationID, UserID: principal.UserID, ProjectID: &project.ID, OrganizationSlug: projectOrganizationSlug(ctx, s.db, principal.OrganizationID)}
		if authCtx.OrganizationSlug == "" {
			return connectionMutationReceipt{}, connectionMutationUnavailable(errors.New("organization slug unavailable"))
		}
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
				return connectionMutationReceipt{}, connectionMutationInvalid("Moving a domain-root endpoint requires the dashboard so its domain can be reconciled.")
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
	if !input.Confirmed {
		return MCPConnectionMutationOutput{}, connectionMutationInvalid("Confirm the exact project, target, requested network mode, and expected settings version before changing network access.")
	}
	mode, err := networkaccess.Parse(input.Mode)
	if err != nil {
		return MCPConnectionMutationOutput{}, connectionMutationInvalid("Mode must be public_only, dual, or private_only.")
	}
	kind, targetID, project, err := s.validateInput(ctx, principal, input.ProjectID, input.TargetKind, input.TargetID, input.ExpectedVersion, input.IdempotencyKey)
	if err != nil {
		return MCPConnectionMutationOutput{}, err
	}
	normalized := connectionMutationInput{ProjectID: project.ID.String(), TargetKind: kind, TargetID: targetID.String(), ExpectedVersion: input.ExpectedVersion, Mode: string(mode)}
	result, receipt, err := s.execute(ctx, principal, project, operationSetMCPNetworkAccess, input.IdempotencyKey, normalized, func(ctx context.Context, tx pgx.Tx) (connectionMutationReceipt, error) {
		finalize, err := s.eligibility.PrepareNetworkAccess(ctx, networkaccess.EligibilityInput{OrganizationID: principal.OrganizationID, Mode: mode})
		if err != nil {
			return connectionMutationReceipt{}, connectionMutationUnavailable(err)
		}
		if err := networkingressrepo.New(tx).AcquireNetworkIngressOrganizationLock(ctx, principal.OrganizationID); err != nil {
			return connectionMutationReceipt{}, fmt.Errorf("lock network ingress lifecycle: %w", err)
		}
		if _, err := s.lockAndCheck(ctx, tx, principal, project, kind, targetID, input.ExpectedVersion); err != nil {
			return connectionMutationReceipt{}, err
		}
		if kind == MCPConnectionSettingsMCPServer {
			_, err = mcpservers.UpdateMCPServerNetworkAccessModeInTransaction(ctx, tx, s.audit, mcpservers.LifecycleUpdateInput{OrganizationID: principal.OrganizationID, ProjectID: project.ID, ActorUserID: principal.UserID, ServerID: targetID}, mode, finalize)
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
		return connectionMutationReceipt{PublicationRequest: string(outcome)}, nil
	})
	if err != nil {
		return MCPConnectionMutationOutput{}, err
	}
	return s.finish(ctx, principal, project.ID, input.ProjectID, kind, targetID, result, receipt)
}

func (s *MCPConnectionMutationService) validateInput(ctx context.Context, principal Principal, projectID string, kind MCPConnectionSettingsTargetKind, targetID, expectedVersion, idempotencyKey string) (MCPConnectionSettingsTargetKind, uuid.UUID, ResolvedProject, error) {
	if s == nil || s.db == nil || s.settings == nil || s.endpoints == nil || s.audit == nil || s.authorizer == nil || s.eligibility == nil || s.now == nil || principal.OrganizationID == "" || principal.UserID == "" || len(idempotencyKey) == 0 || len(idempotencyKey) > 128 || len(expectedVersion) != sha256.Size*2 {
		return "", uuid.Nil, ResolvedProject{}, connectionMutationInvalid("The connection mutation request is incomplete or invalid.")
	}
	projectID = strings.TrimSpace(projectID)
	parsedProject, err := uuid.Parse(projectID)
	if err != nil || (kind != MCPConnectionSettingsMCPServer && kind != MCPConnectionSettingsGateway) {
		return "", uuid.Nil, ResolvedProject{}, connectionMutationInvalid("Provide an explicit project ID, target ID, and target kind.")
	}
	parsedTarget, err := uuid.Parse(strings.TrimSpace(targetID))
	if err != nil {
		return "", uuid.Nil, ResolvedProject{}, connectionMutationInvalid("Provide a valid exact target ID.")
	}
	project, err := platformrepo.New(s.db).ResolvePlatformMCPProjectByID(ctx, platformrepo.ResolvePlatformMCPProjectByIDParams{OrganizationID: principal.OrganizationID, ProjectID: parsedProject})
	if errors.Is(err, pgx.ErrNoRows) {
		return "", uuid.Nil, ResolvedProject{}, ErrMCPConnectionMutationMissing
	}
	if err != nil {
		return "", uuid.Nil, ResolvedProject{}, fmt.Errorf("resolve live connection mutation project: %w", err)
	}
	if err := s.authorizer.Require(ctx, authz.Check{Scope: authz.ScopeMCPWrite, ResourceID: project.ID.String()}); err != nil {
		return "", uuid.Nil, ResolvedProject{}, connectionMutationAuthorizationError(err, authz.ScopeMCPWrite)
	}
	if err := s.authorizer.Require(ctx, authz.Check{Scope: authz.ScopeOrgAdmin, ResourceID: principal.OrganizationID}); err != nil {
		return "", uuid.Nil, ResolvedProject{}, connectionMutationAuthorizationError(err, authz.ScopeOrgAdmin)
	}
	if _, err := s.settings.Get(ctx, principal, GetMCPConnectionSettingsInput{ProjectID: project.ID.String(), TargetKind: kind, TargetID: parsedTarget.String()}); err != nil {
		if errors.Is(err, ErrMCPConnectionSettingsNotFound) {
			return "", uuid.Nil, ResolvedProject{}, ErrMCPConnectionMutationMissing
		}
		return "", uuid.Nil, ResolvedProject{}, err
	}
	return kind, parsedTarget, ResolvedProject{ID: project.ID, Name: project.Name, Slug: project.Slug}, nil
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
		case oops.CodeInvalid:
			return connectionMutationInvalid("The selected endpoint or network access change is not available for this target.")
		case oops.CodeForbidden:
			return &RiskMutationError{Code: "forbidden", Message: "You do not have permission to change this MCP connection.", Cause: err}
		case oops.CodeConflict:
			return fmt.Errorf("%w: %s", ErrMCPConnectionMutationConflict, "The selected address is already in use or the target changed.")
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
