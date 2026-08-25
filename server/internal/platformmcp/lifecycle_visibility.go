package platformmcp

import (
	"context"
	"crypto/hmac"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/conv"
	mcpserversrepo "github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
	platformrepo "github.com/speakeasy-api/gram/server/internal/platformmcp/repo"
)

const (
	operationDisableMCP = "disable_mcp"
	operationEnableMCP  = "enable_mcp"
)

var (
	ErrLifecycleVisibilityInvalid     = errors.New("invalid platform mcp visibility update")
	ErrLifecycleVisibilityUnavailable = errors.New("platform mcp visibility is unavailable for this target")
)

type UpdateMCPVisibilityInput struct {
	ProjectSlug     string
	RegistrationID  string
	MCPID           string
	ExpectedVersion string
	IdempotencyKey  string
}

type UpdateMCPVisibilityResult struct {
	Project        ResolvedProject
	RegistrationID string
	MCPID          string
	Visibility     string
	Version        string
	Receipt        OperationReceipt
	Readiness      MCPReadiness
	Published      bool
}

// LifecycleVisibilityLocker is composed from mcpservers to preserve its domain
// -> root endpoint -> MCP server lock order before the Platform service locks
// the target server.
type LifecycleVisibilityLocker func(context.Context, pgx.Tx, string, uuid.UUID, uuid.UUID) error

// LifecycleVisibilityUpdater is composed from mcpservers so Platform MCP uses
// the same transactional visibility/audit primitive as the dashboard while
// intentionally bypassing dashboard-only attach-to-Default behavior.
type LifecycleVisibilityUpdater func(context.Context, pgx.Tx, mcpserversrepo.McpServer, LifecycleVisibilityUpdate) (LifecycleVisibilityUpdateResult, error)

type LifecycleVisibilityUpdateResult struct {
	Server               mcpserversrepo.McpServer
	ClearedRootDomainIDs []uuid.UUID
}

type LifecycleVisibilityUpdate struct {
	OrganizationID string
	ProjectID      uuid.UUID
	ActorUserID    string
	ServerID       uuid.UUID
	Visibility     string
}

type LifecycleVisibilityService struct {
	db        *pgxpool.Pool
	audit     *audit.Logger
	locker    LifecycleVisibilityLocker
	updater   LifecycleVisibilityUpdater
	publisher ProjectPublisher
	reconcile func(context.Context, []uuid.UUID) error
	readiness *ReadinessService
	key       []byte
	now       func() time.Time
}

func NewLifecycleVisibilityService(db *pgxpool.Pool, auditLogger *audit.Logger, locker LifecycleVisibilityLocker, updater LifecycleVisibilityUpdater, publisher ProjectPublisher, reconcile func(context.Context, []uuid.UUID) error, readiness *ReadinessService, keyMaterial string) (*LifecycleVisibilityService, error) {
	if db == nil || auditLogger == nil || locker == nil || updater == nil || reconcile == nil || readiness == nil || keyMaterial == "" {
		return nil, ErrLifecycleVisibilityInvalid
	}
	return &LifecycleVisibilityService{db: db, audit: auditLogger, locker: locker, updater: updater, publisher: publisher, reconcile: reconcile, readiness: readiness, key: lifecycleMetadataVersionKey(keyMaterial), now: time.Now}, nil
}

func (s *LifecycleVisibilityService) Disable(ctx context.Context, principal Principal, input UpdateMCPVisibilityInput) (UpdateMCPVisibilityResult, error) {
	return s.update(ctx, principal, input, "private", "disabled", operationDisableMCP)
}

func (s *LifecycleVisibilityService) Enable(ctx context.Context, principal Principal, input UpdateMCPVisibilityInput) (UpdateMCPVisibilityResult, error) {
	return s.update(ctx, principal, input, "disabled", "private", operationEnableMCP)
}

func (s *LifecycleVisibilityService) update(ctx context.Context, principal Principal, input UpdateMCPVisibilityInput, from, to, operation string) (UpdateMCPVisibilityResult, error) {
	if s == nil || s.db == nil || s.audit == nil || s.updater == nil || s.readiness == nil || len(s.key) == 0 || principal.OrganizationID == "" || principal.UserID == "" || input.ProjectSlug == "" || input.RegistrationID == "" || input.MCPID == "" || input.ExpectedVersion == "" || input.IdempotencyKey == "" || len(input.IdempotencyKey) > 128 {
		return UpdateMCPVisibilityResult{}, ErrLifecycleVisibilityInvalid
	}
	registrationID, err := uuid.Parse(input.RegistrationID)
	if err != nil {
		return UpdateMCPVisibilityResult{}, ErrLifecycleVisibilityInvalid
	}
	mcpID, err := uuid.Parse(input.MCPID)
	if err != nil {
		return UpdateMCPVisibilityResult{}, ErrLifecycleVisibilityInvalid
	}
	store, err := NewRegistrationStore(s.db, RegistrationStoreConfig{ActiveRegistrationCap: 1})
	if err != nil {
		return UpdateMCPVisibilityResult{}, err
	}
	project, err := store.ResolveProject(ctx, principal.OrganizationID, input.ProjectSlug)
	if err != nil {
		return UpdateMCPVisibilityResult{}, err
	}
	inputHash := lifecycleVisibilityInputHash(project.ID, registrationID, mcpID, input.ExpectedVersion, operation)
	connectionID, generation, err := principalConnection(principal)
	if err != nil {
		return UpdateMCPVisibilityResult{}, err
	}

	tx, err := s.db.Begin(ctx)
	if err != nil {
		return UpdateMCPVisibilityResult{}, fmt.Errorf("begin platform mcp visibility update: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	q := platformrepo.New(tx)
	receiptLookup := platformrepo.GetPlatformMCPOperationReceiptParams{OrganizationID: principal.OrganizationID, UserID: conv.ToPGText(principal.UserID), SubjectUrn: userSubjectURN(principal.UserID), ProjectID: project.ID, Operation: operation, IdempotencyKey: input.IdempotencyKey}
	if err := q.LockPlatformMCPOperationReceipt(ctx, platformrepo.LockPlatformMCPOperationReceiptParams{OrganizationID: principal.OrganizationID, SubjectUrn: userSubjectURN(principal.UserID), ProjectID: project.ID.String(), Operation: operation, IdempotencyKey: input.IdempotencyKey}); err != nil {
		return UpdateMCPVisibilityResult{}, fmt.Errorf("lock platform mcp visibility receipt: %w", err)
	}
	if _, err := q.DeleteExpiredPlatformMCPOperationReceipt(ctx, platformrepo.DeleteExpiredPlatformMCPOperationReceiptParams(receiptLookup)); err != nil {
		return UpdateMCPVisibilityResult{}, fmt.Errorf("reclaim platform mcp visibility receipt: %w", err)
	}
	storedReceipt, err := q.GetPlatformMCPOperationReceipt(ctx, receiptLookup)
	if err == nil {
		if storedReceipt.InputHash != inputHash || storedReceipt.Status != receiptStatusSucceeded || !storedReceipt.RegistrationID.Valid || storedReceipt.RegistrationID.UUID != registrationID {
			return UpdateMCPVisibilityResult{}, ErrRegistrationConflict
		}
		result, err := s.current(ctx, principal, project, registrationID, mcpID, operationReceiptFromRow(storedReceipt, true))
		if err != nil {
			return UpdateMCPVisibilityResult{}, err
		}
		if err := tx.Commit(ctx); err != nil {
			return UpdateMCPVisibilityResult{}, fmt.Errorf("commit platform mcp visibility replay: %w", err)
		}
		return result, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return UpdateMCPVisibilityResult{}, fmt.Errorf("load platform mcp visibility receipt: %w", err)
	}

	registration, err := lifecycleRegistration(ctx, q, principal, project.ID, registrationID)
	if errors.Is(err, pgx.ErrNoRows) {
		return UpdateMCPVisibilityResult{}, ErrLifecycleVisibilityUnavailable
	}
	if err != nil {
		return UpdateMCPVisibilityResult{}, err
	}
	if registration.Status != registrationStatusRegistered || !registrationComponentsComplete(registration) || !registration.McpServerID.Valid || registration.McpServerID.UUID != mcpID {
		return UpdateMCPVisibilityResult{}, ErrLifecycleVisibilityUnavailable
	}
	if to == "disabled" {
		if err := s.locker(ctx, tx, principal.OrganizationID, project.ID, mcpID); err != nil {
			return UpdateMCPVisibilityResult{}, fmt.Errorf("lock platform mcp visibility dependencies: %w", err)
		}
	}
	server, err := mcpserversrepo.New(tx).LockMCPServerByIDAndProjectID(ctx, mcpserversrepo.LockMCPServerByIDAndProjectIDParams{ID: mcpID, ProjectID: project.ID})
	if errors.Is(err, pgx.ErrNoRows) {
		return UpdateMCPVisibilityResult{}, ErrLifecycleVisibilityUnavailable
	}
	if err != nil {
		return UpdateMCPVisibilityResult{}, fmt.Errorf("lock platform mcp visibility target: %w", err)
	}
	if server.Visibility != from || !hmac.Equal([]byte(lifecycleMetadataVersion(s.key, server.ID.String(), server.ProjectID.String(), lifecycleMCPDisplayName(server), server.Slug.String, server.Visibility)), []byte(input.ExpectedVersion)) {
		return UpdateMCPVisibilityResult{}, ErrRegistrationConflict
	}
	receipt, err := q.CreatePlatformMCPOperationReceipt(ctx, platformrepo.CreatePlatformMCPOperationReceiptParams{OrganizationID: principal.OrganizationID, ProjectID: project.ID, RegistrationID: uuid.NullUUID{UUID: registrationID, Valid: true}, ConnectionID: connectionID, ConnectionGeneration: generation, UserID: conv.ToPGText(principal.UserID), ActingSurface: conv.ToPGText(string(principal.surface())), Operation: operation, IdempotencyKey: input.IdempotencyKey, InputHash: inputHash, Status: receiptStatusPending, ResultCode: pgtype.Text{String: "", Valid: false}, ExpiresAt: pgtype.Timestamptz{Time: s.now().Add(receiptLifetime), InfinityModifier: pgtype.Finite, Valid: true}})
	if err != nil {
		return UpdateMCPVisibilityResult{}, fmt.Errorf("create platform mcp visibility receipt: %w", err)
	}
	visibilityResult, err := s.updater(ctx, tx, server, LifecycleVisibilityUpdate{OrganizationID: principal.OrganizationID, ProjectID: project.ID, ActorUserID: principal.UserID, ServerID: mcpID, Visibility: to})
	if err != nil {
		return UpdateMCPVisibilityResult{}, fmt.Errorf("update platform mcp visibility: %w", err)
	}
	completed, err := q.CompletePlatformMCPOperationReceipt(ctx, platformrepo.CompletePlatformMCPOperationReceiptParams{RegistrationID: uuid.NullUUID{UUID: registrationID, Valid: true}, Status: receiptStatusSucceeded, ResultCode: conv.ToPGText(to), ID: receipt.ID, OrganizationID: principal.OrganizationID})
	if err != nil {
		return UpdateMCPVisibilityResult{}, fmt.Errorf("complete platform mcp visibility receipt: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return UpdateMCPVisibilityResult{}, fmt.Errorf("commit platform mcp visibility update: %w", err)
	}

	updated := visibilityResult.Server
	result := UpdateMCPVisibilityResult{Project: project, RegistrationID: registrationID.String(), MCPID: updated.ID.String(), Visibility: updated.Visibility, Version: lifecycleMetadataVersion(s.key, updated.ID.String(), updated.ProjectID.String(), lifecycleMCPDisplayName(updated), updated.Slug.String, updated.Visibility), Receipt: operationReceiptFromRow(completed, false), Readiness: MCPReadiness{State: "unknown", CheckedAt: "", ExpiresAt: ""}, Published: false}
	if err := s.reconcile(ctx, visibilityResult.ClearedRootDomainIDs); err != nil {
		return UpdateMCPVisibilityResult{}, fmt.Errorf("reconcile platform mcp custom domains: %w", err)
	}
	if to == "private" {
		// Visibility has already committed. Readiness is an observed post-commit
		// outcome, so a transient probe failure must not make the completed enable
		// look uncommitted or consume its idempotency receipt as an error.
		_, readiness, found, readinessErr := s.readiness.GetReadiness(ctx, principal, project.Slug, registrationID.String(), true)
		if readinessErr == nil && found {
			result.Readiness = MCPReadiness{State: string(readiness.State), CheckedAt: readinessTimestamp(readiness.CheckedAt), ExpiresAt: readinessTimestamp(readiness.ExpiresAt)}
		}
	}
	if s.publisher != nil {
		result.Published = s.publisher(ctx, project.ID, principal.UserID, "Update Platform MCP visibility") == nil
	}
	return result, nil
}

func (s *LifecycleVisibilityService) current(ctx context.Context, principal Principal, project ResolvedProject, registrationID, mcpID uuid.UUID, receipt OperationReceipt) (UpdateMCPVisibilityResult, error) {
	registration, err := lifecycleRegistration(ctx, platformrepo.New(s.db), principal, project.ID, registrationID)
	if errors.Is(err, pgx.ErrNoRows) {
		return UpdateMCPVisibilityResult{}, ErrLifecycleVisibilityUnavailable
	}
	if err != nil {
		return UpdateMCPVisibilityResult{}, err
	}
	if registration.Status != registrationStatusRegistered || !registrationComponentsComplete(registration) || !registration.McpServerID.Valid || registration.McpServerID.UUID != mcpID {
		return UpdateMCPVisibilityResult{}, ErrLifecycleVisibilityUnavailable
	}
	server, err := mcpserversrepo.New(s.db).GetMCPServerByIDAndProjectID(ctx, mcpserversrepo.GetMCPServerByIDAndProjectIDParams{ID: mcpID, ProjectID: project.ID})
	if err != nil {
		return UpdateMCPVisibilityResult{}, ErrLifecycleVisibilityInvalid
	}
	return UpdateMCPVisibilityResult{Project: project, RegistrationID: registrationID.String(), MCPID: mcpID.String(), Visibility: server.Visibility, Version: lifecycleMetadataVersion(s.key, server.ID.String(), server.ProjectID.String(), lifecycleMCPDisplayName(server), server.Slug.String, server.Visibility), Receipt: receipt, Readiness: MCPReadiness{State: "unknown", CheckedAt: "", ExpiresAt: ""}, Published: false}, nil
}

func lifecycleVisibilityInputHash(projectID, registrationID, mcpID uuid.UUID, version, operation string) string {
	return lifecycleMetadataInputHash(projectID, registrationID, mcpID, operation, version)
}
