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
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	mcpserversrepo "github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
	"github.com/speakeasy-api/gram/server/internal/oops"
	platformrepo "github.com/speakeasy-api/gram/server/internal/platformmcp/repo"
)

const operationDeleteMCP = "delete_mcp"

const (
	// deleteMCPOutcomeDeleted means the server was removed from the project.
	deleteMCPOutcomeDeleted = "deleted"
	// deleteMCPOutcomeTurnedOff means a hosted toolset server was turned off
	// instead, exactly as the dashboard delete does: its id is the toolset id
	// and cannot be reused, so the toolset keeps it.
	deleteMCPOutcomeTurnedOff = "turned_off"
)

const (
	deleteMCPStateGone     = "gone"
	deleteMCPStateDisabled = "disabled"
	deleteMCPStateEnabled  = "enabled"
	// deleteMCPStateUnverified means the fresh read after a replay failed.
	deleteMCPStateUnverified = "verification_unavailable"
)

var (
	ErrLifecycleDeleteInvalid  = errors.New("invalid platform mcp delete")
	ErrLifecycleDeleteMissing  = errors.New("platform mcp delete target not found")
	ErrLifecycleDeleteConflict = errors.New("platform mcp delete target changed")
)

// MCPServerDeleter is composed from the dashboard's mcpServers.delete, so the
// Platform MCP removes a server exactly as the dashboard does: a remote or
// tunneled server is tombstoned with its orphaned issuer and audited, and a
// hosted toolset server is turned off through its toolset. It reads the acting
// user and project from the auth context on ctx and enforces mcp:write itself.
type MCPServerDeleter func(ctx context.Context, serverID uuid.UUID) error

type DeleteMCPInput struct {
	ProjectID       string `json:"project_id" jsonschema:"exact project ID that owns the MCP server, from find_mcp or get_mcp"`
	MCPID           string `json:"mcp_id" jsonschema:"exact MCP server ID from find_mcp or get_mcp; never a name or slug"`
	ExpectedVersion string `json:"expected_version" jsonschema:"opaque version from the immediately preceding find_mcp or get_mcp read of this server"`
	IdempotencyKey  string `json:"idempotency_key" jsonschema:"caller-generated idempotency key; reuse only to retry this exact delete"`
	Confirmed       bool   `json:"confirmed" jsonschema:"true only after the user confirms deleting this exact server"`
}

type DeleteMCPOutput struct {
	ProjectID string `json:"project_id"`
	MCPID     string `json:"mcp_id"`
	Name      string `json:"name"`
	Slug      string `json:"slug,omitempty"`
	// Outcome is what the delete did: deleted, or turned_off for a hosted
	// toolset server, which the dashboard also only turns off.
	Outcome string `json:"outcome"`
	// CurrentState is a fresh read after the delete: gone, disabled, or
	// enabled. On a replay it can differ from Outcome when someone changed the
	// server since, and is verification_unavailable when that read failed.
	CurrentState string                  `json:"current_state"`
	Receipt      RiskMutationToolReceipt `json:"receipt"`
	Message      string                  `json:"message"`
}

// deleteMCPSnapshot is the receipt payload. The server row is gone after a
// tombstone, so the name and slug are kept here to answer a replay.
type deleteMCPSnapshot struct {
	Name    string `json:"name"`
	Slug    string `json:"slug"`
	Outcome string `json:"outcome,omitempty"`
}

// LifecycleDeleteService deletes any MCP server in a project for an
// organization administrator. Unlike the other lifecycle tools it is not
// limited to servers this caller registered through the Platform MCP: it is
// the cleanup path for servers created in the dashboard or by other people.
type LifecycleDeleteService struct {
	db         *pgxpool.Pool
	authorizer *authz.Engine
	deleter    MCPServerDeleter
	key        []byte
	now        func() time.Time
}

func NewLifecycleDeleteService(db *pgxpool.Pool, authorizer *authz.Engine, deleter MCPServerDeleter, keyMaterial string) (*LifecycleDeleteService, error) {
	if db == nil || authorizer == nil || deleter == nil || keyMaterial == "" {
		return nil, ErrLifecycleDeleteInvalid
	}
	return &LifecycleDeleteService{db: db, authorizer: authorizer, deleter: deleter, key: lifecycleMetadataVersionKey(keyMaterial), now: time.Now}, nil
}

type deleteMCPClaim struct {
	receipt  platformrepo.PlatformMcpOperationReceipt
	snapshot deleteMCPSnapshot
	replay   bool
	resume   bool
	// applied means a resumed delete already took effect before its receipt
	// was completed, so the delete is not run again.
	applied bool
}

func (s *LifecycleDeleteService) Delete(ctx context.Context, principal Principal, input DeleteMCPInput) (DeleteMCPOutput, error) {
	if s == nil || s.db == nil || s.authorizer == nil || s.deleter == nil || len(s.key) == 0 {
		return DeleteMCPOutput{}, deleteMCPUnavailable(nil)
	}
	input.ProjectID, input.MCPID = strings.TrimSpace(input.ProjectID), strings.TrimSpace(input.MCPID)
	input.ExpectedVersion, input.IdempotencyKey = strings.TrimSpace(input.ExpectedVersion), strings.TrimSpace(input.IdempotencyKey)
	if !input.Confirmed {
		return DeleteMCPOutput{}, deleteMCPInvalid("Confirm deleting this exact server with the user before calling again with confirmed: true.")
	}
	if principal.OrganizationID == "" || principal.UserID == "" || input.ExpectedVersion == "" || input.IdempotencyKey == "" || len(input.IdempotencyKey) > 128 {
		return DeleteMCPOutput{}, deleteMCPInvalid("Provide the exact project ID, server ID, expected version, and an idempotency key.")
	}
	projectID, err := uuid.Parse(input.ProjectID)
	if err != nil {
		return DeleteMCPOutput{}, deleteMCPInvalid("Provide the exact project ID from find_mcp or get_mcp.")
	}
	mcpID, err := uuid.Parse(input.MCPID)
	if err != nil {
		return DeleteMCPOutput{}, deleteMCPInvalid("Provide the exact server ID from find_mcp or get_mcp.")
	}
	row, err := platformrepo.New(s.db).ResolvePlatformMCPProjectByID(ctx, platformrepo.ResolvePlatformMCPProjectByIDParams{OrganizationID: principal.OrganizationID, ProjectID: projectID})
	if errors.Is(err, pgx.ErrNoRows) {
		return DeleteMCPOutput{}, ErrLifecycleDeleteMissing
	}
	if err != nil {
		return DeleteMCPOutput{}, fmt.Errorf("resolve platform mcp delete project: %w", err)
	}
	project := ResolvedProject{ID: row.ID, Name: row.Name, Slug: row.Slug}
	// Org admin is checked here too, not only by the tool descriptor: this
	// path acts on servers the caller neither created nor registered.
	if err := s.authorizer.Require(ctx, authz.Check{Scope: authz.ScopeOrgAdmin, ResourceKind: "", ResourceID: principal.OrganizationID, Dimensions: nil}); err != nil {
		return DeleteMCPOutput{}, connectionMutationAuthorizationError(err, authz.ScopeOrgAdmin)
	}

	claim, err := s.claim(ctx, principal, project.ID, mcpID, input)
	if err != nil {
		return DeleteMCPOutput{}, err
	}
	if claim.replay {
		return s.replayResult(ctx, project.ID, mcpID, claim.snapshot, operationReceiptFromRow(claim.receipt, true)), nil
	}

	if !claim.applied {
		if err := s.deleter(deleteMCPActorContext(ctx, principal, project.ID), mcpID); err != nil {
			// A concurrent delete may have removed the server since the claim;
			// the fresh read below decides.
			if !claim.resume || !isOopsCode(err, oops.CodeNotFound) {
				return DeleteMCPOutput{}, classifyDeleteMCPError(err)
			}
		}
	}

	state, err := s.currentState(ctx, project.ID, mcpID)
	if err != nil {
		return DeleteMCPOutput{}, err
	}
	outcome := deleteMCPOutcomeDeleted
	switch state {
	case deleteMCPStateGone:
	case deleteMCPStateDisabled:
		outcome = deleteMCPOutcomeTurnedOff
	default:
		return DeleteMCPOutput{}, &RiskMutationError{Code: "not_verified", Message: "The server is still enabled after the delete. Read it again with get_mcp before retrying.", Cause: ErrLifecycleDeleteConflict}
	}
	claim.snapshot.Outcome = outcome
	payload, err := json.Marshal(claim.snapshot)
	if err != nil {
		return DeleteMCPOutput{}, fmt.Errorf("encode platform mcp delete receipt: %w", err)
	}
	completed, err := platformrepo.New(s.db).CompletePlatformMCPOperationReceipt(ctx, platformrepo.CompletePlatformMCPOperationReceiptParams{RegistrationID: uuid.NullUUID{UUID: uuid.Nil, Valid: false}, Status: receiptStatusSucceeded, ResultCode: conv.ToPGText(outcome), ResultPayload: payload, ID: claim.receipt.ID, OrganizationID: principal.OrganizationID})
	if err != nil {
		return DeleteMCPOutput{}, fmt.Errorf("complete platform mcp delete receipt: %w", err)
	}
	return deleteMCPOutput(project.ID, mcpID, claim.snapshot, outcome, state, operationReceiptFromRow(completed, false)), nil
}

// claim checks the idempotency receipt and, for a new delete, the expected
// version, then records a pending receipt. The dashboard delete runs in its
// own transaction afterwards, so a pending receipt is what lets a retry with
// the same key resume after a failure between the two.
func (s *LifecycleDeleteService) claim(ctx context.Context, principal Principal, projectID, mcpID uuid.UUID, input DeleteMCPInput) (deleteMCPClaim, error) {
	inputHash := deleteMCPInputHash(projectID, mcpID, input.ExpectedVersion)
	connectionID, generation, err := principalConnection(principal)
	if err != nil {
		return deleteMCPClaim{}, fmt.Errorf("parse platform mcp delete connection: %w", err)
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return deleteMCPClaim{}, fmt.Errorf("begin platform mcp delete: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	q := platformrepo.New(tx)
	lookup := platformrepo.GetPlatformMCPOperationReceiptParams{OrganizationID: principal.OrganizationID, UserID: conv.ToPGText(principal.UserID), SubjectUrn: userSubjectURN(principal.UserID), ProjectID: projectID, Operation: operationDeleteMCP, IdempotencyKey: input.IdempotencyKey}
	if err := q.LockPlatformMCPOperationReceipt(ctx, platformrepo.LockPlatformMCPOperationReceiptParams{OrganizationID: principal.OrganizationID, SubjectUrn: userSubjectURN(principal.UserID), ProjectID: projectID.String(), Operation: operationDeleteMCP, IdempotencyKey: input.IdempotencyKey}); err != nil {
		return deleteMCPClaim{}, fmt.Errorf("lock platform mcp delete receipt: %w", err)
	}
	if _, err := q.DeleteExpiredPlatformMCPOperationReceipt(ctx, platformrepo.DeleteExpiredPlatformMCPOperationReceiptParams(lookup)); err != nil {
		return deleteMCPClaim{}, fmt.Errorf("reclaim platform mcp delete receipt: %w", err)
	}
	stored, err := q.GetPlatformMCPOperationReceipt(ctx, lookup)
	switch {
	case err == nil:
		if stored.InputHash != inputHash {
			return deleteMCPClaim{}, deleteMCPConflict("This idempotency key was already used for a different delete. Use a fresh key.")
		}
		var snapshot deleteMCPSnapshot
		if err := json.Unmarshal(stored.ResultPayload, &snapshot); err != nil {
			return deleteMCPClaim{}, fmt.Errorf("decode platform mcp delete receipt: %w", err)
		}
		claim := deleteMCPClaim{receipt: stored, snapshot: snapshot, replay: false, resume: false, applied: false}
		switch stored.Status {
		case receiptStatusSucceeded:
			claim.replay = true
		case receiptStatusPending:
			applied, err := s.resumable(ctx, tx, projectID, mcpID, input.ExpectedVersion)
			if err != nil {
				return deleteMCPClaim{}, err
			}
			claim.resume, claim.applied = true, applied
		default:
			return deleteMCPClaim{}, deleteMCPConflict("This idempotency key belongs to a delete that did not finish. Read the server again and use a fresh key.")
		}
		if err := tx.Commit(ctx); err != nil {
			return deleteMCPClaim{}, fmt.Errorf("commit platform mcp delete replay: %w", err)
		}
		return claim, nil
	case !errors.Is(err, pgx.ErrNoRows):
		return deleteMCPClaim{}, fmt.Errorf("load platform mcp delete receipt: %w", err)
	}

	server, err := mcpserversrepo.New(tx).GetMCPServerByIDAndProjectID(ctx, mcpserversrepo.GetMCPServerByIDAndProjectIDParams{ID: mcpID, ProjectID: projectID})
	if errors.Is(err, pgx.ErrNoRows) {
		return deleteMCPClaim{}, ErrLifecycleDeleteMissing
	}
	if err != nil {
		return deleteMCPClaim{}, fmt.Errorf("load platform mcp delete target: %w", err)
	}
	if !hmac.Equal([]byte(s.version(server)), []byte(input.ExpectedVersion)) {
		return deleteMCPClaim{}, deleteMCPConflict("The server changed since it was read. Read it again with get_mcp and confirm with the user before retrying.")
	}
	snapshot := deleteMCPSnapshot{Name: lifecycleMCPDisplayName(server), Slug: server.Slug.String, Outcome: ""}
	payload, err := json.Marshal(snapshot)
	if err != nil {
		return deleteMCPClaim{}, fmt.Errorf("encode platform mcp delete receipt: %w", err)
	}
	receipt, err := q.CreatePlatformMCPOperationReceipt(ctx, platformrepo.CreatePlatformMCPOperationReceiptParams{OrganizationID: principal.OrganizationID, ProjectID: projectID, RegistrationID: uuid.NullUUID{UUID: uuid.Nil, Valid: false}, ConnectionID: connectionID, ConnectionGeneration: generation, UserID: conv.ToPGText(principal.UserID), ActingSurface: conv.ToPGText(string(principal.surface())), Operation: operationDeleteMCP, IdempotencyKey: input.IdempotencyKey, InputHash: inputHash, Status: receiptStatusPending, ResultCode: pgtype.Text{String: "", Valid: false}, ResultPayload: payload, ExpiresAt: pgtype.Timestamptz{Time: s.now().Add(receiptLifetime), InfinityModifier: pgtype.Finite, Valid: true}})
	if err != nil {
		return deleteMCPClaim{}, fmt.Errorf("create platform mcp delete receipt: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return deleteMCPClaim{}, fmt.Errorf("commit platform mcp delete claim: %w", err)
	}
	return deleteMCPClaim{receipt: receipt, snapshot: snapshot, replay: false, resume: false, applied: false}, nil
}

// resumable re-reads the server before a pending delete is retried, so a
// server changed since the first attempt is never deleted on a stale version.
// It reports whether the first attempt already took effect: the server is
// gone, or it is a hosted toolset server that is now turned off.
func (s *LifecycleDeleteService) resumable(ctx context.Context, tx pgx.Tx, projectID, mcpID uuid.UUID, expectedVersion string) (bool, error) {
	server, err := mcpserversrepo.New(tx).GetMCPServerByIDAndProjectID(ctx, mcpserversrepo.GetMCPServerByIDAndProjectIDParams{ID: mcpID, ProjectID: projectID})
	if errors.Is(err, pgx.ErrNoRows) {
		return true, nil
	}
	if err != nil {
		return false, fmt.Errorf("load platform mcp delete target: %w", err)
	}
	if hmac.Equal([]byte(s.version(server)), []byte(expectedVersion)) {
		return false, nil
	}
	if server.ToolsetID.Valid && server.ToolsetID.UUID == server.ID && server.Visibility == "disabled" {
		return true, nil
	}
	return false, deleteMCPConflict("The server changed since the interrupted delete. Read it again with get_mcp and confirm with the user before retrying with a fresh key.")
}

// replayResult answers a finished delete from its receipt. The delete is
// committed, so a failed fresh read must not hide it: the state is then
// reported as unverified rather than claimed.
func (s *LifecycleDeleteService) replayResult(ctx context.Context, projectID, mcpID uuid.UUID, snapshot deleteMCPSnapshot, receipt OperationReceipt) DeleteMCPOutput {
	state, err := s.currentState(ctx, projectID, mcpID)
	if err != nil {
		state = deleteMCPStateUnverified
	}
	return deleteMCPOutput(projectID, mcpID, snapshot, snapshot.Outcome, state, receipt)
}

// currentState is the post-delete read: a tombstoned server no longer loads,
// and a hosted toolset server loads as disabled.
func (s *LifecycleDeleteService) currentState(ctx context.Context, projectID, mcpID uuid.UUID) (string, error) {
	server, err := mcpserversrepo.New(s.db).GetMCPServerByIDAndProjectID(ctx, mcpserversrepo.GetMCPServerByIDAndProjectIDParams{ID: mcpID, ProjectID: projectID})
	if errors.Is(err, pgx.ErrNoRows) {
		return deleteMCPStateGone, nil
	}
	if err != nil {
		return "", fmt.Errorf("read platform mcp delete target: %w", err)
	}
	if server.Visibility == "disabled" {
		return deleteMCPStateDisabled, nil
	}
	return deleteMCPStateEnabled, nil
}

func (s *LifecycleDeleteService) version(server mcpserversrepo.McpServer) string {
	return lifecycleMetadataVersion(s.key, server.ID.String(), server.ProjectID.String(), lifecycleMCPDisplayName(server), server.Slug.String, server.Visibility)
}

func deleteMCPOutput(projectID, mcpID uuid.UUID, snapshot deleteMCPSnapshot, outcome, state string, receipt OperationReceipt) DeleteMCPOutput {
	message := "Deleted this server. People can no longer reach it and the gateway no longer lists it."
	if outcome == deleteMCPOutcomeTurnedOff {
		message = "This server is hosted from a toolset, so it was turned off rather than removed, as the dashboard does. People can no longer reach it and the gateway no longer lists it; the toolset itself is unchanged."
	}
	if receipt.Replayed {
		message = "This delete already happened; nothing was changed again. current_state is a fresh read."
	}
	return DeleteMCPOutput{ProjectID: projectID.String(), MCPID: mcpID.String(), Name: snapshot.Name, Slug: snapshot.Slug, Outcome: outcome, CurrentState: state, Receipt: riskMutationToolReceipt(receipt), Message: message}
}

// deleteMCPActorContext runs the dashboard delete as the calling user in the
// target project, keeping any richer identity already on ctx (such as the
// email the audit log records).
func deleteMCPActorContext(ctx context.Context, principal Principal, projectID uuid.UUID) context.Context {
	actor := &contextvalues.AuthContext{ActiveOrganizationID: principal.OrganizationID, UserID: principal.UserID, ProjectID: &projectID} //nolint:exhaustruct // Only the actor, organization and project apply when no richer identity is on ctx.
	if current, ok := contextvalues.GetAuthContext(ctx); ok && current != nil {
		scoped := *current
		scoped.ActiveOrganizationID, scoped.UserID, scoped.ProjectID = principal.OrganizationID, principal.UserID, &projectID
		actor = &scoped
	}
	return contextvalues.SetAuthContext(ctx, actor)
}

func deleteMCPInputHash(projectID, mcpID uuid.UUID, expectedVersion string) string {
	digest := sha256.Sum256([]byte(strings.Join([]string{"platform-mcp-delete-v1", projectID.String(), mcpID.String(), expectedVersion}, "\x00")))
	return hex.EncodeToString(digest[:])
}

func classifyDeleteMCPError(err error) error {
	shareable, ok := errors.AsType[*oops.ShareableError](err)
	if !ok {
		return fmt.Errorf("delete mcp server: %w", err)
	}
	switch shareable.Code {
	case oops.CodeForbidden, oops.CodeUnauthorized:
		return &ExternalAuthorizationError{RequiredScope: string(authz.ScopeMCPWrite), RequestAccessURL: "", cause: err}
	case oops.CodeNotFound:
		return ErrLifecycleDeleteMissing
	case oops.CodeConflict:
		return deleteMCPConflict("The server changed while it was being deleted. Read it again with get_mcp before retrying.")
	case oops.CodeInvalid, oops.CodeBadRequest:
		return &RiskMutationError{Code: "invalid_request", Message: "This server cannot be deleted here: " + shareable.Error() + ". Use the dashboard instead.", Cause: errors.Join(ErrLifecycleDeleteInvalid, err)}
	default:
		return fmt.Errorf("delete mcp server: %w", err)
	}
}

func deleteMCPInvalid(message string) error {
	return &RiskMutationError{Code: "invalid_request", Message: message, Cause: ErrLifecycleDeleteInvalid}
}

func deleteMCPConflict(message string) error {
	return &RiskMutationError{Code: "conflict", Message: message, Cause: ErrLifecycleDeleteConflict}
}

func deleteMCPUnavailable(cause error) error {
	return &RiskMutationError{Code: unavailableCode, Message: "Deleting MCP servers is unavailable on this server.", Cause: errors.Join(ErrUnavailable, cause)}
}
