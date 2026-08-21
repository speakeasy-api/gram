package platformmcp

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"

	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/speakeasy-api/gram/server/internal/conv"
	mcpserversrepo "github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
	platformrepo "github.com/speakeasy-api/gram/server/internal/platformmcp/repo"
)

const (
	operationUpdateMCPMetadata   = "update_mcp_metadata"
	receiptResultMetadataUpdated = "metadata_updated"
)

var ErrLifecycleMetadataInvalid = errors.New("invalid platform mcp metadata update")

type UpdateMCPMetadataInput struct {
	ProjectSlug     string
	RegistrationID  string
	MCPID           string
	Name            string
	ExpectedVersion string
	IdempotencyKey  string
}

type UpdateMCPMetadataResult struct {
	Project        ResolvedProject
	RegistrationID string
	MCPID          string
	Name           string
	Slug           string
	Visibility     string
	Version        string
	Receipt        OperationReceipt
}

// LifecycleMetadataService updates the display metadata of a complete
// Platform-owned MCP registration. It shares the narrow mcpservers transaction
// command used by dashboard updates, but never selects or changes plugin
// attachments.
type LifecycleMetadataService struct {
	db      *pgxpool.Pool
	updater LifecycleMetadataUpdater
	key     []byte
	now     func() time.Time
}

// LifecycleMetadataUpdater is supplied by server composition so Platform MCP
// invokes the same narrow mcpservers command as the dashboard without importing
// the dashboard package back into this boundary.
type LifecycleMetadataUpdater func(ctx context.Context, tx pgx.Tx, existing mcpserversrepo.McpServer, input LifecycleMetadataUpdate) (mcpserversrepo.McpServer, error)

type LifecycleMetadataUpdate struct {
	OrganizationID string
	ProjectID      uuid.UUID
	ActorUserID    string
	ServerID       uuid.UUID
	Name           string
}

func NewLifecycleMetadataService(db *pgxpool.Pool, updater LifecycleMetadataUpdater, keyMaterial string) (*LifecycleMetadataService, error) {
	if db == nil || updater == nil || keyMaterial == "" {
		return nil, ErrLifecycleMetadataInvalid
	}
	return &LifecycleMetadataService{db: db, updater: updater, key: lifecycleMetadataVersionKey(keyMaterial), now: time.Now}, nil
}

func (s *LifecycleMetadataService) Update(ctx context.Context, principal Principal, input UpdateMCPMetadataInput) (UpdateMCPMetadataResult, error) {
	if s == nil || s.db == nil || s.updater == nil || len(s.key) == 0 || principal.OrganizationID == "" || principal.UserID == "" || input.ProjectSlug == "" || input.RegistrationID == "" || input.MCPID == "" || strings.TrimSpace(input.Name) == "" || len(strings.TrimSpace(input.Name)) > 256 || strings.ContainsAny(input.Name, "\r\n") || input.ExpectedVersion == "" || input.IdempotencyKey == "" {
		return UpdateMCPMetadataResult{}, ErrLifecycleMetadataInvalid
	}
	if len(input.IdempotencyKey) > 128 {
		return UpdateMCPMetadataResult{}, ErrLifecycleMetadataInvalid
	}
	registrationID, err := uuid.Parse(input.RegistrationID)
	if err != nil {
		return UpdateMCPMetadataResult{}, ErrLifecycleMetadataInvalid
	}
	mcpID, err := uuid.Parse(input.MCPID)
	if err != nil {
		return UpdateMCPMetadataResult{}, ErrLifecycleMetadataInvalid
	}
	project, err := NewRegistrationStore(s.db, RegistrationStoreConfig{ActiveRegistrationCap: 1})
	if err != nil {
		return UpdateMCPMetadataResult{}, err
	}
	resolvedProject, err := project.ResolveProject(ctx, principal.OrganizationID, input.ProjectSlug)
	if err != nil {
		return UpdateMCPMetadataResult{}, err
	}
	inputHash := lifecycleMetadataInputHash(resolvedProject.ID, registrationID, mcpID, strings.TrimSpace(input.Name), input.ExpectedVersion)
	connectionID, generation, err := principalConnection(principal)
	if err != nil {
		return UpdateMCPMetadataResult{}, err
	}

	tx, err := s.db.Begin(ctx)
	if err != nil {
		return UpdateMCPMetadataResult{}, fmt.Errorf("begin platform mcp metadata update: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	q := platformrepo.New(tx)
	receiptLookup := platformrepo.GetPlatformMCPOperationReceiptParams{
		OrganizationID: principal.OrganizationID,
		UserID:         conv.ToPGText(principal.UserID),
		SubjectUrn:     userSubjectURN(principal.UserID),
		ProjectID:      resolvedProject.ID,
		Operation:      operationUpdateMCPMetadata,
		IdempotencyKey: input.IdempotencyKey,
	}
	if err := q.LockPlatformMCPOperationReceipt(ctx, platformrepo.LockPlatformMCPOperationReceiptParams{
		OrganizationID: principal.OrganizationID,
		SubjectUrn:     userSubjectURN(principal.UserID),
		ProjectID:      resolvedProject.ID.String(),
		Operation:      operationUpdateMCPMetadata,
		IdempotencyKey: input.IdempotencyKey,
	}); err != nil {
		return UpdateMCPMetadataResult{}, fmt.Errorf("lock platform mcp metadata receipt: %w", err)
	}
	if _, err := q.DeleteExpiredPlatformMCPOperationReceipt(ctx, platformrepo.DeleteExpiredPlatformMCPOperationReceiptParams(receiptLookup)); err != nil {
		return UpdateMCPMetadataResult{}, fmt.Errorf("reclaim platform mcp metadata receipt: %w", err)
	}
	storedReceipt, err := q.GetPlatformMCPOperationReceipt(ctx, receiptLookup)
	if err == nil {
		if storedReceipt.InputHash != inputHash {
			return UpdateMCPMetadataResult{}, ErrRegistrationConflict
		}
		if storedReceipt.Status != receiptStatusSucceeded || !storedReceipt.RegistrationID.Valid || storedReceipt.RegistrationID.UUID != registrationID {
			return UpdateMCPMetadataResult{}, ErrLifecycleMetadataInvalid
		}
		result, err := s.currentResult(ctx, principal, resolvedProject, registrationID, mcpID, operationReceiptFromRow(storedReceipt, true))
		if err != nil {
			return UpdateMCPMetadataResult{}, err
		}
		if err := tx.Commit(ctx); err != nil {
			return UpdateMCPMetadataResult{}, fmt.Errorf("commit platform mcp metadata replay: %w", err)
		}
		return result, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return UpdateMCPMetadataResult{}, fmt.Errorf("load platform mcp metadata receipt: %w", err)
	}

	registration, err := lifecycleRegistration(ctx, q, principal, resolvedProject.ID, registrationID)
	if errors.Is(err, pgx.ErrNoRows) {
		return UpdateMCPMetadataResult{}, ErrLifecycleMetadataInvalid
	}
	if err != nil {
		return UpdateMCPMetadataResult{}, err
	}
	if registration.Status != registrationStatusRegistered || !registrationComponentsComplete(registration) || !registration.RemoteMcpServerOwned || !registration.UserSessionIssuerOwned || !registration.McpServerOwned || !registration.McpEndpointOwned || !registration.McpServerID.Valid || registration.McpServerID.UUID != mcpID {
		return UpdateMCPMetadataResult{}, ErrLifecycleMetadataInvalid
	}
	server, err := mcpserversrepo.New(tx).LockMCPServerByIDAndProjectID(ctx, mcpserversrepo.LockMCPServerByIDAndProjectIDParams{ID: mcpID, ProjectID: resolvedProject.ID})
	if errors.Is(err, pgx.ErrNoRows) {
		return UpdateMCPMetadataResult{}, ErrLifecycleMetadataInvalid
	}
	if err != nil {
		return UpdateMCPMetadataResult{}, fmt.Errorf("lock platform mcp metadata target: %w", err)
	}
	if !s.validVersion(server, input.ExpectedVersion) {
		return UpdateMCPMetadataResult{}, ErrRegistrationConflict
	}

	receipt, err := q.CreatePlatformMCPOperationReceipt(ctx, platformrepo.CreatePlatformMCPOperationReceiptParams{
		OrganizationID:       principal.OrganizationID,
		ProjectID:            resolvedProject.ID,
		RegistrationID:       uuid.NullUUID{UUID: registrationID, Valid: true},
		ConnectionID:         connectionID,
		ConnectionGeneration: generation,
		UserID:               conv.ToPGText(principal.UserID),
		ActingSurface:        conv.ToPGText(string(principal.surface())),
		Operation:            operationUpdateMCPMetadata,
		IdempotencyKey:       input.IdempotencyKey,
		InputHash:            inputHash,
		Status:               receiptStatusPending,
		ResultCode:           pgtype.Text{String: "", Valid: false},
		ExpiresAt:            pgtype.Timestamptz{Time: s.now().Add(receiptLifetime), InfinityModifier: pgtype.Finite, Valid: true},
	})
	if err != nil {
		return UpdateMCPMetadataResult{}, fmt.Errorf("create platform mcp metadata receipt: %w", err)
	}
	updated, err := s.updater(ctx, tx, server, LifecycleMetadataUpdate{
		OrganizationID: principal.OrganizationID,
		ProjectID:      resolvedProject.ID,
		ActorUserID:    principal.UserID,
		ServerID:       mcpID,
		Name:           strings.TrimSpace(input.Name),
	})
	if err != nil {
		return UpdateMCPMetadataResult{}, fmt.Errorf("update platform mcp metadata: %w", err)
	}
	completed, err := q.CompletePlatformMCPOperationReceipt(ctx, platformrepo.CompletePlatformMCPOperationReceiptParams{
		RegistrationID: uuid.NullUUID{UUID: registrationID, Valid: true},
		Status:         receiptStatusSucceeded,
		ResultCode:     conv.ToPGText(receiptResultMetadataUpdated),
		ID:             receipt.ID,
		OrganizationID: principal.OrganizationID,
	})
	if err != nil {
		return UpdateMCPMetadataResult{}, fmt.Errorf("complete platform mcp metadata receipt: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return UpdateMCPMetadataResult{}, fmt.Errorf("commit platform mcp metadata update: %w", err)
	}
	return UpdateMCPMetadataResult{
		Project: resolvedProject, RegistrationID: registrationID.String(), MCPID: updated.ID.String(), Name: lifecycleMCPDisplayName(updated), Slug: updated.Slug.String, Visibility: updated.Visibility, Version: s.version(updated), Receipt: operationReceiptFromRow(completed, false),
	}, nil
}

func (s *LifecycleMetadataService) currentResult(ctx context.Context, principal Principal, project ResolvedProject, registrationID, mcpID uuid.UUID, receipt OperationReceipt) (UpdateMCPMetadataResult, error) {
	registration, err := lifecycleRegistration(ctx, platformrepo.New(s.db), principal, project.ID, registrationID)
	if err != nil || registration.Status != registrationStatusRegistered || !registrationComponentsComplete(registration) || !registration.RemoteMcpServerOwned || !registration.UserSessionIssuerOwned || !registration.McpServerOwned || !registration.McpEndpointOwned || !registration.McpServerID.Valid || registration.McpServerID.UUID != mcpID {
		return UpdateMCPMetadataResult{}, ErrLifecycleMetadataInvalid
	}
	server, err := mcpserversrepo.New(s.db).GetMCPServerByIDAndProjectID(ctx, mcpserversrepo.GetMCPServerByIDAndProjectIDParams{ID: mcpID, ProjectID: project.ID})
	if err != nil {
		return UpdateMCPMetadataResult{}, ErrLifecycleMetadataInvalid
	}
	return UpdateMCPMetadataResult{Project: project, RegistrationID: registrationID.String(), MCPID: mcpID.String(), Name: lifecycleMCPDisplayName(server), Slug: server.Slug.String, Visibility: server.Visibility, Version: s.version(server), Receipt: receipt}, nil
}

func (s *LifecycleMetadataService) version(server mcpserversrepo.McpServer) string {
	return lifecycleMetadataVersion(s.key, server.ID.String(), server.ProjectID.String(), lifecycleMCPDisplayName(server), server.Slug.String, server.Visibility)
}

func (s *LifecycleMetadataService) validVersion(server mcpserversrepo.McpServer, value string) bool {
	return hmac.Equal([]byte(s.version(server)), []byte(value))
}

func lifecycleMetadataInputHash(projectID, registrationID, mcpID uuid.UUID, name, version string) string {
	digest := sha256.Sum256([]byte(strings.Join([]string{projectID.String(), registrationID.String(), mcpID.String(), name, version}, "\x00")))
	return base64.RawURLEncoding.EncodeToString(digest[:])
}

// lifecycleMetadataVersion is an opaque optimistic-concurrency token over the
// persisted fields this D1 slice can change. It is not an authorization token;
// live authorization and registration ownership are always checked separately.
func lifecycleMetadataVersionKey(keyMaterial string) []byte {
	digest := sha256.Sum256([]byte("platform-mcp-lifecycle-metadata:" + keyMaterial))
	return digest[:]
}

func lifecycleMetadataVersion(key []byte, mcpID, projectID, name, slug, visibility string) string {
	mac := hmac.New(sha256.New, key)
	_, _ = mac.Write([]byte(strings.Join([]string{mcpID, projectID, name, slug, visibility}, "\x00")))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

func lifecycleMCPDisplayName(server mcpserversrepo.McpServer) string {
	if server.Name.Valid && server.Name.String != "" {
		return server.Name.String
	}
	if server.Slug.Valid && server.Slug.String != "" {
		return server.Slug.String
	}
	return server.ID.String()
}
