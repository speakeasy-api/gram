//nolint:exhaustruct // Generated repository parameter types intentionally use documented zero-value optional fields.
package platformmcp

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/speakeasy-api/gram/server/internal/audit"
	mcpendpointsrepo "github.com/speakeasy-api/gram/server/internal/mcpendpoints/repo"
	mcpserversrepo "github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
	organizationsrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	platformrepo "github.com/speakeasy-api/gram/server/internal/platformmcp/repo"
	remotemcprepo "github.com/speakeasy-api/gram/server/internal/remotemcp/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
	usersessionsrepo "github.com/speakeasy-api/gram/server/internal/usersessions/repo"
)

const (
	operationRegisterCatalogMCP  = "register_catalog_mcp"
	receiptStatusPending         = "pending"
	receiptStatusSucceeded       = "succeeded"
	receiptResultRegistered      = "registered"
	receiptResultActiveCap       = "conflict:active_registration_cap"
	registrationStatusPending    = "pending"
	registrationStatusRegistered = "registered"
	receiptLifetime              = 24 * time.Hour
	platformMCPIssuerLifetime    = 14 * 24 * time.Hour
	maxMCPEndpointSlugLength     = 128
)

var (
	ErrRegistrationConflict = errors.New("platform mcp registration idempotency conflict")
	ErrRegistrationCap      = errors.New("platform mcp active registration cap reached")
	ErrRegistrationInvalid  = errors.New("invalid platform mcp registration input")
	ErrTargetIneligible     = errors.New("platform mcp registration target is ineligible")
)

// CatalogRegistrationRequest is the normalized desired state behind one
// register_catalog_mcp call. The caller resolves catalog details before passing
// this value to persistence; display metadata is deliberately not identity.
type CatalogRegistrationRequest struct {
	ProjectSlug       string
	SourceKind        string
	CatalogProvider   string
	CatalogReference  string
	ConfigurationHash string
	IdempotencyKey    string
	InputHash         string
}

type ResolvedProject struct {
	ID   uuid.UUID
	Name string
	Slug string
}

type OperationReceipt struct {
	ID                   uuid.UUID
	RegistrationID       uuid.NullUUID
	Status               string
	ResultCode           string
	InputHash            string
	ExpiresAt            time.Time
	Replayed             bool
	ConnectionID         uuid.UUID
	ConnectionGeneration uuid.UUID
}

// RegistrationStoreConfig carries values whose production defaults require
// explicit review before Platform catalog registration can be composed.
type RegistrationStoreConfig struct {
	ActiveRegistrationCap int64
}

// RegistrationStore owns the tenant-qualified receipt and desired-state
// persistence boundary. It does not fetch catalog data, call providers, or
// create project components.
type RegistrationStore struct {
	db                    *pgxpool.Pool
	activeRegistrationCap int64
}

func NewRegistrationStore(db *pgxpool.Pool, config RegistrationStoreConfig) (*RegistrationStore, error) {
	if db == nil || config.ActiveRegistrationCap <= 0 {
		return nil, ErrRegistrationInvalid
	}
	return &RegistrationStore{db: db, activeRegistrationCap: config.ActiveRegistrationCap}, nil
}

func (s *RegistrationStore) ResolveProject(ctx context.Context, organizationID, projectSlug string) (ResolvedProject, error) {
	if s == nil || s.db == nil || organizationID == "" || projectSlug == "" {
		return ResolvedProject{}, ErrRegistrationInvalid
	}
	row, err := platformrepo.New(s.db).ResolvePlatformMCPProjectBySlug(ctx, platformrepo.ResolvePlatformMCPProjectBySlugParams{
		OrganizationID: organizationID,
		Slug:           projectSlug,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return ResolvedProject{}, ErrRegistrationInvalid
	}
	if err != nil {
		return ResolvedProject{}, fmt.Errorf("resolve platform mcp registration project: %w", err)
	}
	return ResolvedProject{ID: row.ID, Name: row.Name, Slug: row.Slug}, nil
}

func (s *RegistrationStore) EligibleCatalogRegistrationTarget(ctx context.Context, organizationID string, project ResolvedProject) (bool, error) {
	if s == nil || s.db == nil || organizationID == "" || project.ID == uuid.Nil || project.Slug == "" {
		return false, ErrTargetIneligible
	}
	eligible, err := platformrepo.New(s.db).IsPlatformMCPCatalogRegistrationTargetEligible(ctx, platformrepo.IsPlatformMCPCatalogRegistrationTargetEligibleParams{
		ProjectID:      project.ID,
		OrganizationID: organizationID,
	})
	if err != nil {
		return false, fmt.Errorf("check platform mcp catalog registration target eligibility: %w", err)
	}
	return eligible, nil
}

// ResolveRegistrationPendingSecretFields projects the persisted secret-header
// state without reading or decrypting secret values. It is used for idempotent
// registration replays so the agent is not sent back to dashboard setup after
// the user has already completed it there.
func (s *RegistrationStore) ResolveRegistrationPendingSecretFields(ctx context.Context, principal Principal, project ResolvedProject, registrationID uuid.UUID, declared []CatalogConfigurationField) ([]CatalogConfigurationField, error) {
	if s == nil || s.db == nil || registrationID == uuid.Nil {
		return nil, ErrRegistrationInvalid
	}
	registration, err := lifecycleRegistration(ctx, platformrepo.New(s.db), principal, project.ID, registrationID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrRegistrationInvalid
	}
	if err != nil {
		return nil, fmt.Errorf("resolve platform mcp registration secret setup state: %w", err)
	}
	if registration.Status != registrationStatusRegistered || !registrationComponentsComplete(registration) || !registration.RemoteMcpServerID.Valid {
		return nil, ErrRegistrationInvalid
	}
	headers, err := remotemcprepo.New(s.db).ListServerHeaders(ctx, remotemcprepo.ListServerHeadersParams{
		RemoteMcpServerID: registration.RemoteMcpServerID.UUID,
		ProjectID:         project.ID,
	})
	if err != nil {
		return nil, fmt.Errorf("list platform mcp registration headers: %w", err)
	}
	configured := make(map[string]bool, len(headers))
	for _, header := range headers {
		configured[strings.ToLower(header.Name)] = header.IsSecret && header.Value.Valid && header.Value.String != ""
	}
	pending := make([]CatalogConfigurationField, 0, len(declared))
	for _, field := range declared {
		if field.Required && field.Secret && !configured[strings.ToLower(field.Name)] {
			pending = append(pending, field)
		}
	}
	return pending, nil
}

func (s *RegistrationStore) ResolveRegistrationCatalogIdentity(ctx context.Context, principal Principal, project ResolvedProject, registrationID uuid.UUID) (CatalogCandidate, error) {
	if s == nil || s.db == nil || registrationID == uuid.Nil {
		return CatalogCandidate{}, ErrRegistrationInvalid
	}
	registration, err := lifecycleRegistration(ctx, platformrepo.New(s.db), principal, project.ID, registrationID)
	if errors.Is(err, pgx.ErrNoRows) {
		return CatalogCandidate{}, ErrRegistrationInvalid
	}
	if err != nil {
		return CatalogCandidate{}, fmt.Errorf("resolve platform mcp registration catalogue identity: %w", err)
	}
	if registration.CatalogProvider == "" || registration.CatalogReference == "" {
		return CatalogCandidate{}, ErrRegistrationInvalid
	}
	return CatalogCandidate{ProviderKey: registration.CatalogProvider, CatalogRef: registration.CatalogReference}, nil
}

// ResolveRegistrationDashboardSetup derives the only dashboard continuation
// target from the lifecycle-bound private resources. The agent never receives
// the Remote MCP source URL or configuration values.
func (s *RegistrationStore) ResolveRegistrationDashboardSetup(ctx context.Context, principal Principal, project ResolvedProject, registrationID uuid.UUID) (RegistrationDashboardSetup, error) {
	if s == nil || s.db == nil || registrationID == uuid.Nil {
		return RegistrationDashboardSetup{}, ErrRegistrationInvalid
	}
	registration, err := lifecycleRegistration(ctx, platformrepo.New(s.db), principal, project.ID, registrationID)
	if errors.Is(err, pgx.ErrNoRows) {
		return RegistrationDashboardSetup{}, ErrRegistrationInvalid
	}
	if err != nil {
		return RegistrationDashboardSetup{}, fmt.Errorf("resolve platform mcp dashboard setup registration: %w", err)
	}
	if registration.Status != registrationStatusRegistered || !registrationComponentsComplete(registration) || !registration.McpServerID.Valid {
		return RegistrationDashboardSetup{}, ErrRegistrationInvalid
	}
	organization, err := organizationsrepo.New(s.db).GetOrganizationMetadata(ctx, principal.OrganizationID)
	if err != nil {
		return RegistrationDashboardSetup{}, fmt.Errorf("resolve platform mcp dashboard organization: %w", err)
	}
	server, err := mcpserversrepo.New(s.db).GetMCPServerByIDAndProjectID(ctx, mcpserversrepo.GetMCPServerByIDAndProjectIDParams{
		ID:        registration.McpServerID.UUID,
		ProjectID: project.ID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return RegistrationDashboardSetup{}, ErrRegistrationInvalid
	}
	if err != nil {
		return RegistrationDashboardSetup{}, fmt.Errorf("resolve platform mcp dashboard server: %w", err)
	}
	if !server.RemoteMcpServerID.Valid || server.RemoteMcpServerID.UUID != registration.RemoteMcpServerID.UUID || !server.UserSessionIssuerID.Valid || server.UserSessionIssuerID.UUID != registration.UserSessionIssuerID.UUID {
		return RegistrationDashboardSetup{}, ErrRegistrationInvalid
	}
	mcpServerRoute := server.Slug.String
	if !server.Slug.Valid || mcpServerRoute == "" {
		mcpServerRoute = server.ID.String()
	}
	if organization.Slug == "" || mcpServerRoute == "" {
		return RegistrationDashboardSetup{}, ErrRegistrationInvalid
	}
	return RegistrationDashboardSetup{OrganizationSlug: organization.Slug, MCPServerRoute: mcpServerRoute}, nil
}

// BeginReceipt atomically establishes the 24-hour idempotency boundary. It
// never creates a registration: callers first resolve catalog data outside a
// transaction, then use ConvergeRegistration to create or reuse the desired
// registration state.
func (s *RegistrationStore) BeginReceipt(ctx context.Context, principal Principal, project ResolvedProject, request CatalogRegistrationRequest, now time.Time) (OperationReceipt, error) {
	if s == nil || s.db == nil {
		return OperationReceipt{}, ErrUnavailable
	}
	if err := validateCatalogRegistrationRequest(principal, project, request); err != nil {
		return OperationReceipt{}, err
	}

	connectionID, generation, err := principalConnection(principal)
	if err != nil {
		return OperationReceipt{}, err
	}

	tx, err := s.db.Begin(ctx)
	if err != nil {
		return OperationReceipt{}, fmt.Errorf("begin platform mcp registration receipt: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	q := platformrepo.New(tx)
	lock := platformrepo.LockPlatformMCPOperationReceiptParams{
		OrganizationID: principal.OrganizationID,
		SubjectUrn:     userSubjectURN(principal.UserID),
		ProjectID:      project.ID.String(),
		Operation:      operationRegisterCatalogMCP,
		IdempotencyKey: request.IdempotencyKey,
	}
	if err := q.LockPlatformMCPOperationReceipt(ctx, lock); err != nil {
		return OperationReceipt{}, fmt.Errorf("lock platform mcp registration receipt: %w", err)
	}
	receiptLookup := platformrepo.GetPlatformMCPOperationReceiptParams{
		OrganizationID: principal.OrganizationID,
		SubjectUrn:     userSubjectURN(principal.UserID),
		ProjectID:      project.ID,
		Operation:      operationRegisterCatalogMCP,
		IdempotencyKey: request.IdempotencyKey,
	}
	if _, err := q.DeleteExpiredPlatformMCPOperationReceipt(ctx, platformrepo.DeleteExpiredPlatformMCPOperationReceiptParams(receiptLookup)); err != nil {
		return OperationReceipt{}, fmt.Errorf("reclaim expired platform mcp registration receipt: %w", err)
	}

	receipt, err := q.GetPlatformMCPOperationReceipt(ctx, receiptLookup)
	switch {
	case err == nil:
		if receipt.InputHash != request.InputHash {
			return OperationReceipt{}, ErrRegistrationConflict
		}
		if err := tx.Commit(ctx); err != nil {
			return OperationReceipt{}, fmt.Errorf("commit platform mcp registration receipt replay: %w", err)
		}
		return operationReceiptFromRow(receipt, true), nil
	case !errors.Is(err, pgx.ErrNoRows):
		return OperationReceipt{}, fmt.Errorf("load platform mcp registration receipt: %w", err)
	}

	receipt, err = q.CreatePlatformMCPOperationReceipt(ctx, platformrepo.CreatePlatformMCPOperationReceiptParams{
		OrganizationID:       principal.OrganizationID,
		ProjectID:            project.ID,
		RegistrationID:       uuid.NullUUID{},
		ConnectionID:         uuid.NullUUID{UUID: connectionID, Valid: true},
		ConnectionGeneration: uuid.NullUUID{UUID: generation, Valid: true},
		Operation:            operationRegisterCatalogMCP,
		IdempotencyKey:       request.IdempotencyKey,
		InputHash:            request.InputHash,
		Status:               receiptStatusPending,
		ResultCode:           pgtype.Text{},
		ExpiresAt:            timestamp(now.Add(receiptLifetime)),
	})
	if err != nil {
		return OperationReceipt{}, fmt.Errorf("create platform mcp registration receipt: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return OperationReceipt{}, fmt.Errorf("commit platform mcp registration receipt: %w", err)
	}
	return operationReceiptFromRow(receipt, false), nil
}

// ConvergeRegistration creates or reuses the active desired-state registration
// in one transaction. It intentionally leaves the receipt pending: only the
// later private-component convergence can complete the receipt and record the
// registration_succeeded milestone.
func (s *RegistrationStore) ConvergeRegistration(ctx context.Context, principal Principal, project ResolvedProject, request CatalogRegistrationRequest, receipt OperationReceipt) (OperationReceipt, error) {
	if s == nil || s.db == nil {
		return OperationReceipt{}, ErrUnavailable
	}
	if err := validateCatalogRegistrationRequest(principal, project, request); err != nil {
		return OperationReceipt{}, err
	}
	if receipt.ID == uuid.Nil {
		return OperationReceipt{}, ErrRegistrationInvalid
	}
	connectionID, generation, err := principalConnection(principal)
	if err != nil {
		return OperationReceipt{}, err
	}

	tx, err := s.db.Begin(ctx)
	if err != nil {
		return OperationReceipt{}, fmt.Errorf("begin platform mcp registration convergence: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	q := platformrepo.New(tx)
	receiptLock := platformrepo.LockPlatformMCPOperationReceiptParams{
		OrganizationID: principal.OrganizationID,
		SubjectUrn:     userSubjectURN(principal.UserID),
		ProjectID:      project.ID.String(),
		Operation:      operationRegisterCatalogMCP,
		IdempotencyKey: request.IdempotencyKey,
	}
	if err := q.LockPlatformMCPOperationReceipt(ctx, receiptLock); err != nil {
		return OperationReceipt{}, fmt.Errorf("lock platform mcp registration convergence receipt: %w", err)
	}
	storedReceipt, err := q.GetPlatformMCPOperationReceipt(ctx, platformrepo.GetPlatformMCPOperationReceiptParams{
		OrganizationID: receiptLock.OrganizationID,
		SubjectUrn:     receiptLock.SubjectUrn,
		ProjectID:      project.ID,
		Operation:      receiptLock.Operation,
		IdempotencyKey: receiptLock.IdempotencyKey,
	})
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return OperationReceipt{}, fmt.Errorf("load platform mcp registration convergence receipt: %w", err)
	}
	if errors.Is(err, pgx.ErrNoRows) || storedReceipt.ID != receipt.ID || storedReceipt.InputHash != request.InputHash {
		return OperationReceipt{}, ErrRegistrationInvalid
	}
	if storedReceipt.Status == receiptStatusSucceeded {
		if err := tx.Commit(ctx); err != nil {
			return OperationReceipt{}, fmt.Errorf("commit platform mcp registration convergence replay: %w", err)
		}
		if storedReceipt.ResultCode.String == receiptResultActiveCap {
			return operationReceiptFromRow(storedReceipt, true), ErrRegistrationCap
		}
		return operationReceiptFromRow(storedReceipt, true), nil
	}
	if storedReceipt.Status != receiptStatusPending {
		return OperationReceipt{}, ErrRegistrationInvalid
	}
	if _, err := q.LockLivePlatformMCPProjectForRegistration(ctx, platformrepo.LockLivePlatformMCPProjectForRegistrationParams{
		ProjectID:      project.ID,
		OrganizationID: principal.OrganizationID,
	}); errors.Is(err, pgx.ErrNoRows) {
		return OperationReceipt{}, ErrTargetIneligible
	} else if err != nil {
		return OperationReceipt{}, fmt.Errorf("lock live platform mcp registration project: %w", err)
	}
	if err := q.LockPlatformMCPProjectRegistrationQuota(ctx, platformrepo.LockPlatformMCPProjectRegistrationQuotaParams{
		OrganizationID: principal.OrganizationID,
		ProjectID:      project.ID.String(),
	}); err != nil {
		return OperationReceipt{}, fmt.Errorf("lock platform mcp registration quota: %w", err)
	}
	if err := q.LockPlatformMCPCatalogRegistration(ctx, platformrepo.LockPlatformMCPCatalogRegistrationParams{
		OrganizationID:   principal.OrganizationID,
		ProjectID:        project.ID.String(),
		SourceKind:       request.SourceKind,
		CatalogProvider:  request.CatalogProvider,
		CatalogReference: request.CatalogReference,
	}); err != nil {
		return OperationReceipt{}, fmt.Errorf("lock platform mcp catalog registration: %w", err)
	}
	eligible, err := q.IsPlatformMCPCatalogRegistrationTargetEligible(ctx, platformrepo.IsPlatformMCPCatalogRegistrationTargetEligibleParams{
		ProjectID:      project.ID,
		OrganizationID: principal.OrganizationID,
	})
	if err != nil {
		return OperationReceipt{}, fmt.Errorf("recheck platform mcp registration target eligibility: %w", err)
	}
	if !eligible {
		return OperationReceipt{}, ErrTargetIneligible
	}

	registration, err := q.GetActivePlatformMCPCatalogRegistration(ctx, platformrepo.GetActivePlatformMCPCatalogRegistrationParams{
		OrganizationID:   principal.OrganizationID,
		ProjectID:        project.ID,
		SourceKind:       request.SourceKind,
		CatalogProvider:  request.CatalogProvider,
		CatalogReference: request.CatalogReference,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		activeRegistrations, err := q.CountActiveRegisteredPlatformMCPCatalogRegistrations(ctx, platformrepo.CountActiveRegisteredPlatformMCPCatalogRegistrationsParams{
			OrganizationID: principal.OrganizationID,
			ProjectID:      project.ID,
		})
		if err != nil {
			return OperationReceipt{}, fmt.Errorf("count active platform mcp catalog registrations: %w", err)
		}
		if activeRegistrations >= s.activeRegistrationCap {
			deniedReceipt, err := q.CompletePlatformMCPOperationReceipt(ctx, platformrepo.CompletePlatformMCPOperationReceiptParams{
				RegistrationID: uuid.NullUUID{},
				Status:         receiptStatusSucceeded,
				ResultCode:     optionalText(receiptResultActiveCap),
				ID:             storedReceipt.ID,
				OrganizationID: principal.OrganizationID,
			})
			if err != nil {
				return OperationReceipt{}, fmt.Errorf("complete platform mcp registration cap receipt: %w", err)
			}
			if err := tx.Commit(ctx); err != nil {
				return OperationReceipt{}, fmt.Errorf("commit platform mcp registration cap receipt: %w", err)
			}
			return operationReceiptFromRow(deniedReceipt, receipt.Replayed), ErrRegistrationCap
		}
		registration, err = q.CreatePlatformMCPCatalogRegistration(ctx, platformrepo.CreatePlatformMCPCatalogRegistrationParams{
			OrganizationID:       principal.OrganizationID,
			ProjectID:            project.ID,
			SourceKind:           request.SourceKind,
			CatalogProvider:      request.CatalogProvider,
			CatalogReference:     request.CatalogReference,
			Status:               registrationStatusPending,
			ConnectionID:         uuid.NullUUID{UUID: connectionID, Valid: true},
			ConnectionGeneration: uuid.NullUUID{UUID: generation, Valid: true},
		})
		if err != nil {
			return OperationReceipt{}, fmt.Errorf("create platform mcp catalog registration: %w", err)
		}
	} else if err != nil {
		return OperationReceipt{}, fmt.Errorf("get active platform mcp catalog registration: %w", err)
	}
	ownedRegistration, err := lifecycleRegistration(ctx, q, principal, project.ID, registration.ID)
	if errors.Is(err, pgx.ErrNoRows) {
		return OperationReceipt{}, ErrRegistrationInvalid
	}
	if err != nil {
		return OperationReceipt{}, fmt.Errorf("authorize platform mcp registration convergence: %w", err)
	}
	if ownedRegistration.ID != registration.ID {
		return OperationReceipt{}, ErrRegistrationInvalid
	}

	attachedReceipt, err := q.AttachPlatformMCPOperationReceiptRegistration(ctx, platformrepo.AttachPlatformMCPOperationReceiptRegistrationParams{
		RegistrationID: uuid.NullUUID{UUID: registration.ID, Valid: true},
		ID:             storedReceipt.ID,
		OrganizationID: principal.OrganizationID,
	})
	if err != nil {
		return OperationReceipt{}, fmt.Errorf("attach platform mcp registration receipt: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return OperationReceipt{}, fmt.Errorf("commit platform mcp registration convergence: %w", err)
	}
	return operationReceiptFromRow(attachedReceipt, receipt.Replayed), nil
}

// CompleteRegistration creates the local, private component stack only after a
// reviewed catalog adapter has validated the remote endpoint. It does not call
// management handlers, create plugin rows, or publish packages.
// CompleteRegistration resolves private resources from one server-validated
// catalogue configuration. Tests and older internal callers use
// CompleteRegistrationWithRemoteURL to build the equivalent empty configuration.
func (s *RegistrationStore) CompleteRegistration(ctx context.Context, principal Principal, project ResolvedProject, request CatalogRegistrationRequest, receipt OperationReceipt, configuration resolvedCatalogConfiguration) (OperationReceipt, error) {
	if s == nil || s.db == nil {
		return OperationReceipt{}, ErrUnavailable
	}
	if err := validateCatalogRegistrationRequest(principal, project, request); err != nil || receipt.ID == uuid.Nil || !receipt.RegistrationID.Valid || !validRegistrationRemoteURL(configuration.remoteURL) {
		return OperationReceipt{}, ErrRegistrationInvalid
	}
	connectionID, generation, err := principalConnection(principal)
	if err != nil {
		return OperationReceipt{}, err
	}

	tx, err := s.db.Begin(ctx)
	if err != nil {
		return OperationReceipt{}, fmt.Errorf("begin platform mcp component convergence: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	q := platformrepo.New(tx)
	receiptLock := platformrepo.LockPlatformMCPOperationReceiptParams{
		OrganizationID: principal.OrganizationID,
		SubjectUrn:     userSubjectURN(principal.UserID),
		ProjectID:      project.ID.String(),
		Operation:      operationRegisterCatalogMCP,
		IdempotencyKey: request.IdempotencyKey,
	}
	if err := q.LockPlatformMCPOperationReceipt(ctx, receiptLock); err != nil {
		return OperationReceipt{}, fmt.Errorf("lock platform mcp component receipt: %w", err)
	}
	storedReceipt, err := q.GetPlatformMCPOperationReceipt(ctx, platformrepo.GetPlatformMCPOperationReceiptParams{
		OrganizationID: receiptLock.OrganizationID,
		SubjectUrn:     receiptLock.SubjectUrn,
		ProjectID:      project.ID,
		Operation:      receiptLock.Operation,
		IdempotencyKey: receiptLock.IdempotencyKey,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return OperationReceipt{}, ErrRegistrationInvalid
		}
		return OperationReceipt{}, fmt.Errorf("load platform mcp component receipt: %w", err)
	}
	if storedReceipt.ID != receipt.ID || storedReceipt.InputHash != request.InputHash || !storedReceipt.RegistrationID.Valid || storedReceipt.RegistrationID.UUID != receipt.RegistrationID.UUID {
		return OperationReceipt{}, ErrRegistrationInvalid
	}
	if storedReceipt.Status == receiptStatusSucceeded {
		if err := tx.Commit(ctx); err != nil {
			return OperationReceipt{}, fmt.Errorf("commit platform mcp component replay: %w", err)
		}
		return operationReceiptFromRow(storedReceipt, true), nil
	}
	if storedReceipt.Status != receiptStatusPending {
		return OperationReceipt{}, ErrRegistrationInvalid
	}
	if _, err := q.LockLivePlatformMCPProjectForRegistration(ctx, platformrepo.LockLivePlatformMCPProjectForRegistrationParams{
		ProjectID:      project.ID,
		OrganizationID: principal.OrganizationID,
	}); errors.Is(err, pgx.ErrNoRows) {
		return OperationReceipt{}, ErrTargetIneligible
	} else if err != nil {
		return OperationReceipt{}, fmt.Errorf("lock live platform mcp component project: %w", err)
	}
	if err := q.LockPlatformMCPProjectRegistrationQuota(ctx, platformrepo.LockPlatformMCPProjectRegistrationQuotaParams{
		OrganizationID: principal.OrganizationID,
		ProjectID:      project.ID.String(),
	}); err != nil {
		return OperationReceipt{}, fmt.Errorf("lock platform mcp component quota: %w", err)
	}
	if err := q.LockPlatformMCPCatalogRegistration(ctx, platformrepo.LockPlatformMCPCatalogRegistrationParams{
		OrganizationID:   principal.OrganizationID,
		ProjectID:        project.ID.String(),
		SourceKind:       request.SourceKind,
		CatalogProvider:  request.CatalogProvider,
		CatalogReference: request.CatalogReference,
	}); err != nil {
		return OperationReceipt{}, fmt.Errorf("lock platform mcp component registration: %w", err)
	}
	eligible, err := q.IsPlatformMCPCatalogRegistrationTargetEligible(ctx, platformrepo.IsPlatformMCPCatalogRegistrationTargetEligibleParams{
		ProjectID:      project.ID,
		OrganizationID: principal.OrganizationID,
	})
	if err != nil {
		return OperationReceipt{}, fmt.Errorf("recheck platform mcp registration target eligibility: %w", err)
	}
	if !eligible {
		return OperationReceipt{}, ErrTargetIneligible
	}
	registration, err := q.GetActivePlatformMCPCatalogRegistration(ctx, platformrepo.GetActivePlatformMCPCatalogRegistrationParams{
		OrganizationID:   principal.OrganizationID,
		ProjectID:        project.ID,
		SourceKind:       request.SourceKind,
		CatalogProvider:  request.CatalogProvider,
		CatalogReference: request.CatalogReference,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return OperationReceipt{}, ErrRegistrationInvalid
		}
		return OperationReceipt{}, fmt.Errorf("load platform mcp component registration: %w", err)
	}
	if registration.ID != storedReceipt.RegistrationID.UUID {
		return OperationReceipt{}, ErrRegistrationInvalid
	}
	ownedRegistration, err := lifecycleRegistration(ctx, q, principal, project.ID, registration.ID)
	if errors.Is(err, pgx.ErrNoRows) {
		return OperationReceipt{}, ErrRegistrationInvalid
	}
	if err != nil {
		return OperationReceipt{}, fmt.Errorf("authorize platform mcp component convergence: %w", err)
	}
	if ownedRegistration.ID != registration.ID {
		return OperationReceipt{}, ErrRegistrationInvalid
	}
	activeRegistrations, err := q.CountActiveRegisteredPlatformMCPCatalogRegistrations(ctx, platformrepo.CountActiveRegisteredPlatformMCPCatalogRegistrationsParams{
		OrganizationID: principal.OrganizationID,
		ProjectID:      project.ID,
	})
	if err != nil {
		return OperationReceipt{}, fmt.Errorf("count active platform mcp registrations before completion: %w", err)
	}
	if activeRegistrations >= s.activeRegistrationCap && !registrationComponentsComplete(registration) {
		if err := q.SoftDeletePendingPlatformMCPCatalogRegistration(ctx, platformrepo.SoftDeletePendingPlatformMCPCatalogRegistrationParams{
			RegistrationID: storedReceipt.RegistrationID.UUID,
			OrganizationID: principal.OrganizationID,
			ProjectID:      project.ID,
		}); err != nil {
			return OperationReceipt{}, fmt.Errorf("soft delete capped platform mcp registration: %w", err)
		}
		deniedReceipt, err := q.CompletePlatformMCPOperationReceipt(ctx, platformrepo.CompletePlatformMCPOperationReceiptParams{
			RegistrationID: uuid.NullUUID{},
			Status:         receiptStatusSucceeded,
			ResultCode:     optionalText(receiptResultActiveCap),
			ID:             storedReceipt.ID,
			OrganizationID: principal.OrganizationID,
		})
		if err != nil {
			return OperationReceipt{}, fmt.Errorf("complete capped platform mcp receipt: %w", err)
		}
		if err := tx.Commit(ctx); err != nil {
			return OperationReceipt{}, fmt.Errorf("commit capped platform mcp receipt: %w", err)
		}
		return operationReceiptFromRow(deniedReceipt, receipt.Replayed), ErrRegistrationCap
	}

	componentsCreated := false
	if registrationComponentsComplete(registration) {
		if registration.Status != registrationStatusRegistered {
			return OperationReceipt{}, ErrRegistrationInvalid
		}
	} else if registrationComponentsEmpty(registration) && registration.Status == registrationStatusPending {
		registration, err = s.createPrivateRegistrationComponents(ctx, tx, project, registration, configuration)
		if err != nil {
			return OperationReceipt{}, err
		}
		componentsCreated = true
	} else {
		return OperationReceipt{}, ErrRegistrationInvalid
	}

	completedReceipt, err := q.CompletePlatformMCPOperationReceipt(ctx, platformrepo.CompletePlatformMCPOperationReceiptParams{
		RegistrationID: storedReceipt.RegistrationID,
		Status:         receiptStatusSucceeded,
		ResultCode:     optionalText(receiptResultRegistered),
		ID:             storedReceipt.ID,
		OrganizationID: principal.OrganizationID,
	})
	if err != nil {
		return OperationReceipt{}, fmt.Errorf("complete platform mcp component receipt: %w", err)
	}
	if err := q.RecordPlatformMCPRegistrationSucceeded(ctx, platformrepo.RecordPlatformMCPRegistrationSucceededParams{
		OrganizationID:       principal.OrganizationID,
		ConnectionID:         uuid.NullUUID{UUID: connectionID, Valid: true},
		ConnectionGeneration: uuid.NullUUID{UUID: generation, Valid: true},
		ProjectID:            uuid.NullUUID{UUID: project.ID, Valid: true},
		McpKey:               registration.CatalogProvider + ":" + registration.CatalogReference,
		AttemptID:            uuid.NullUUID{UUID: registration.ID, Valid: true},
	}); err != nil {
		return OperationReceipt{}, fmt.Errorf("record platform mcp registration milestone: %w", err)
	}
	if componentsCreated {
		if err := audit.NewLogger().LogPlatformMcpRegistrationCreate(ctx, tx, audit.LogPlatformMcpRegistrationCreateEvent{
			OrganizationID:             principal.OrganizationID,
			ProjectID:                  project.ID,
			Actor:                      urn.NewPrincipal(urn.PrincipalTypeUser, principal.UserID),
			PlatformMcpRegistrationURN: urn.NewPlatformMcpRegistration(registration.ID),
			CatalogProvider:            registration.CatalogProvider,
			CatalogReference:           registration.CatalogReference,
			RemoteMcpServerURN:         urn.NewRemoteMcpServer(registration.RemoteMcpServerID.UUID),
			UserSessionIssuerURN:       urn.NewUserSessionIssuer(registration.UserSessionIssuerID.UUID),
			McpServerURN:               urn.NewMcpServer(registration.McpServerID.UUID),
			McpEndpointURN:             urn.NewMcpEndpoint(registration.McpEndpointID.UUID),
		}); err != nil {
			return OperationReceipt{}, fmt.Errorf("record platform mcp component convergence audit event: %w", err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return OperationReceipt{}, fmt.Errorf("commit platform mcp component convergence: %w", err)
	}
	return operationReceiptFromRow(completedReceipt, receipt.Replayed), nil
}

func (s *RegistrationStore) CompleteRegistrationWithRemoteURL(ctx context.Context, principal Principal, project ResolvedProject, request CatalogRegistrationRequest, receipt OperationReceipt, remoteURL string) (OperationReceipt, error) {
	return s.CompleteRegistration(ctx, principal, project, request, receipt, resolvedCatalogConfiguration{remoteURL: remoteURL})
}

func (s *RegistrationStore) createPrivateRegistrationComponents(ctx context.Context, tx pgx.Tx, project ResolvedProject, registration platformrepo.PlatformMcpCatalogRegistration, configuration resolvedCatalogConfiguration) (platformrepo.PlatformMcpCatalogRegistration, error) {
	remoteID, err := uuid.NewV7()
	if err != nil {
		return platformrepo.PlatformMcpCatalogRegistration{}, fmt.Errorf("generate platform mcp remote source id: %w", err)
	}
	serverID, err := uuid.NewV7()
	if err != nil {
		return platformrepo.PlatformMcpCatalogRegistration{}, fmt.Errorf("generate platform mcp server id: %w", err)
	}
	suffix, err := newRegistrationComponentSuffix()
	if err != nil {
		return platformrepo.PlatformMcpCatalogRegistration{}, err
	}
	organization, err := organizationsrepo.New(tx).GetOrganizationMetadata(ctx, registration.OrganizationID)
	if err != nil {
		return platformrepo.PlatformMcpCatalogRegistration{}, fmt.Errorf("load platform mcp component organization: %w", err)
	}
	remoteSlug := "platform-mcp-remote-" + suffix
	serverSlug := "platform-mcp-" + suffix
	displayName := configuration.displayName
	if displayName == "" {
		displayName = "MCP Catalogue server"
	}
	remote, err := remotemcprepo.New(tx).CreateServer(ctx, remotemcprepo.CreateServerParams{
		ID:            remoteID,
		ProjectID:     project.ID,
		Name:          optionalText(displayName + " source"),
		Slug:          optionalText(remoteSlug),
		TransportType: "streamable-http",
		Url:           configuration.remoteURL,
	})
	if err != nil {
		return platformrepo.PlatformMcpCatalogRegistration{}, fmt.Errorf("create platform mcp remote source: %w", err)
	}
	for _, header := range configuration.headers {
		// Secret header values are collected only by the secure dashboard path.
		// Reject a plaintext secret here rather than persisting it through the
		// non-encrypting repository directly. An empty static value is deliberate:
		// it satisfies the database's value-source invariant and remains pending
		// until the dashboard replaces it through the encryption boundary.
		if header.secret && header.value != "" {
			return platformrepo.PlatformMcpCatalogRegistration{}, ErrCatalogConfigurationRejected
		}
		value := optionalText(header.value)
		if header.secret {
			value = pgtype.Text{Valid: true}
		}
		if _, err := remotemcprepo.New(tx).CreateServerHeader(ctx, remotemcprepo.CreateServerHeaderParams{
			Name:                   header.name,
			Description:            optionalText(header.description),
			IsRequired:             header.required,
			IsSecret:               header.secret,
			Value:                  value,
			ValueFromRequestHeader: pgtype.Text{},
			RemoteMcpServerID:      remote.ID,
			ProjectID:              project.ID,
		}); err != nil {
			return platformrepo.PlatformMcpCatalogRegistration{}, fmt.Errorf("create platform mcp non-secret configuration header: %w", err)
		}
	}
	issuer, err := usersessionsrepo.New(tx).CreateUserSessionIssuer(ctx, usersessionsrepo.CreateUserSessionIssuerParams{
		ProjectID:          project.ID,
		Slug:               "platform-mcp-issuer-" + suffix,
		AuthnChallengeMode: "interactive",
		SessionDuration: pgtype.Interval{
			Microseconds: platformMCPIssuerLifetime.Microseconds(),
			Valid:        true,
		},
	})
	if err != nil {
		return platformrepo.PlatformMcpCatalogRegistration{}, fmt.Errorf("create platform mcp session issuer: %w", err)
	}
	server, err := mcpserversrepo.New(tx).CreateMCPServer(ctx, mcpserversrepo.CreateMCPServerParams{
		ID:                  serverID,
		ProjectID:           project.ID,
		Name:                optionalText(displayName),
		Slug:                optionalText(serverSlug),
		UserSessionIssuerID: uuid.NullUUID{UUID: issuer.ID, Valid: true},
		RemoteMcpServerID:   uuid.NullUUID{UUID: remote.ID, Valid: true},
		Visibility:          "private",
	})
	if err != nil {
		return platformrepo.PlatformMcpCatalogRegistration{}, fmt.Errorf("create platform mcp server: %w", err)
	}
	endpoint, err := mcpendpointsrepo.New(tx).CreateMCPEndpoint(ctx, mcpendpointsrepo.CreateMCPEndpointParams{
		ProjectID:   project.ID,
		McpServerID: server.ID,
		Slug:        platformMCPEndpointSlug(organization.Slug, suffix),
	})
	if err != nil {
		return platformrepo.PlatformMcpCatalogRegistration{}, fmt.Errorf("create platform mcp endpoint: %w", err)
	}
	updatedRegistration, err := platformrepo.New(tx).UpdatePlatformMCPCatalogRegistrationComponents(ctx, platformrepo.UpdatePlatformMCPCatalogRegistrationComponentsParams{
		Status:                 registrationStatusRegistered,
		RemoteMcpServerID:      uuid.NullUUID{UUID: remote.ID, Valid: true},
		RemoteMcpServerOwned:   true,
		UserSessionIssuerID:    uuid.NullUUID{UUID: issuer.ID, Valid: true},
		UserSessionIssuerOwned: true,
		McpServerID:            uuid.NullUUID{UUID: server.ID, Valid: true},
		McpServerOwned:         true,
		McpEndpointID:          uuid.NullUUID{UUID: endpoint.ID, Valid: true},
		McpEndpointOwned:       true,
		ID:                     registration.ID,
		OrganizationID:         registration.OrganizationID,
		ProjectID:              project.ID,
	})
	if err != nil {
		return platformrepo.PlatformMcpCatalogRegistration{}, fmt.Errorf("update platform mcp registration components: %w", err)
	}
	return updatedRegistration, nil
}

func validRegistrationRemoteURL(rawURL string) bool {
	parsed, err := url.Parse(rawURL)
	return err == nil && parsed.Scheme == "https" && parsed.Hostname() != "" && parsed.User == nil && parsed.Fragment == "" && !hasUnresolvedRemoteTemplate(rawURL)
}

func platformMCPEndpointSlug(organizationSlug, suffix string) string {
	suffixRunes := []rune("-platform-mcp-endpoint-" + suffix)
	organizationRunes := []rune(organizationSlug)
	return string(organizationRunes[:min(len(organizationRunes), maxMCPEndpointSlugLength-len(suffixRunes))]) + string(suffixRunes)
}

func newRegistrationComponentSuffix() (string, error) {
	value := make([]byte, 8)
	if _, err := rand.Read(value); err != nil {
		return "", fmt.Errorf("generate platform mcp component suffix: %w", err)
	}
	return hex.EncodeToString(value), nil
}

func registrationComponentsEmpty(registration platformrepo.PlatformMcpCatalogRegistration) bool {
	return !registration.RemoteMcpServerID.Valid && !registration.UserSessionIssuerID.Valid && !registration.McpServerID.Valid && !registration.McpEndpointID.Valid
}

func registrationComponentsComplete(registration platformrepo.PlatformMcpCatalogRegistration) bool {
	return registration.RemoteMcpServerID.Valid && registration.UserSessionIssuerID.Valid && registration.McpServerID.Valid && registration.McpEndpointID.Valid
}

func validateCatalogRegistrationRequest(principal Principal, project ResolvedProject, request CatalogRegistrationRequest) error {
	if principal.UserID == "" || principal.OrganizationID == "" || project.ID == uuid.Nil || project.Slug == "" || request.ProjectSlug == "" || request.ProjectSlug != project.Slug || request.SourceKind == "" || request.CatalogProvider == "" || request.CatalogReference == "" || request.IdempotencyKey == "" || len(request.IdempotencyKey) > 128 || request.InputHash == "" || request.InputHash != catalogRegistrationInputHash(request.ProjectSlug, request.SourceKind, request.CatalogProvider, request.CatalogReference, request.ConfigurationHash) {
		return ErrRegistrationInvalid
	}
	return nil
}

func principalConnection(principal Principal) (uuid.UUID, uuid.UUID, error) {
	connectionID, err := uuid.Parse(principal.ConnectionID)
	if err != nil {
		return uuid.Nil, uuid.Nil, fmt.Errorf("parse platform mcp registration connection: %w", err)
	}
	generation, err := uuid.Parse(principal.Generation)
	if err != nil {
		return uuid.Nil, uuid.Nil, fmt.Errorf("parse platform mcp registration generation: %w", err)
	}
	return connectionID, generation, nil
}

func userSubjectURN(userID string) string {
	return urn.NewUserSubject(userID).String()
}

func operationReceiptFromRow(row platformrepo.PlatformMcpOperationReceipt, replayed bool) OperationReceipt {
	return OperationReceipt{
		ID:                   row.ID,
		RegistrationID:       row.RegistrationID,
		Status:               row.Status,
		ResultCode:           row.ResultCode.String,
		InputHash:            row.InputHash,
		ExpiresAt:            row.ExpiresAt.Time,
		Replayed:             replayed,
		ConnectionID:         row.ConnectionID.UUID,
		ConnectionGeneration: row.ConnectionGeneration.UUID,
	}
}

func catalogConfigurationHash(values CatalogConfigurationValues) string {
	if len(values) == 0 {
		return ""
	}
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	var payload strings.Builder
	for _, key := range keys {
		payload.WriteString(key)
		payload.WriteByte(0)
		payload.WriteString(values[key])
		payload.WriteByte(0)
	}
	digest := sha256.Sum256([]byte(payload.String()))
	return hex.EncodeToString(digest[:])
}

func catalogRegistrationInputHash(projectSlug, sourceKind, catalogProvider, catalogReference string, configurationHash ...string) string {
	payload := projectSlug + "\x00" + sourceKind + "\x00" + catalogProvider + "\x00" + catalogReference
	if len(configurationHash) > 0 && configurationHash[0] != "" {
		payload += "\x00" + configurationHash[0]
	}
	digest := sha256.Sum256([]byte(payload))
	return hex.EncodeToString(digest[:])
}
