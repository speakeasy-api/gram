package platformmcp

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/speakeasy-api/gram/server/internal/conv"
	platformrepo "github.com/speakeasy-api/gram/server/internal/platformmcp/repo"
)

type mutationReceiptExecution[T any] struct {
	DB             *pgxpool.Pool
	Now            func() time.Time
	Principal      Principal
	Project        ResolvedProject
	Operation      string
	IdempotencyKey string
	InputHash      string
	Label          string
	Invalid        func(error) error
	Conflict       func(string) error
	Unavailable    func(error) error
	ValidateReplay func([]byte) bool
	EncodeResult   func(T) ([]byte, error)
	Mutate         func(context.Context, pgx.Tx) (T, error)
}

// executeMutationReceipt owns the common idempotency transaction used by
// access-affecting Platform MCP mutations: advisory lock, exact replay,
// pending receipt, domain+audit callback, result persistence, and one commit.
func executeMutationReceipt[T any](ctx context.Context, execution mutationReceiptExecution[T]) (OperationReceipt, error) {
	if execution.DB == nil || execution.Now == nil || execution.Mutate == nil || execution.EncodeResult == nil || execution.ValidateReplay == nil || execution.Invalid == nil || execution.Conflict == nil || execution.Unavailable == nil || execution.Principal.OrganizationID == "" || execution.Principal.UserID == "" || execution.Project.ID == uuid.Nil || execution.Project.Slug == "" || execution.Operation == "" || execution.IdempotencyKey == "" || len(execution.IdempotencyKey) > 128 || execution.InputHash == "" || execution.Label == "" {
		return OperationReceipt{}, execution.Unavailable(errors.New("invalid mutation receipt execution"))
	}
	connectionID, generation, err := principalConnection(execution.Principal)
	if err != nil {
		return OperationReceipt{}, execution.Invalid(err)
	}
	tx, err := execution.DB.Begin(ctx)
	if err != nil {
		return OperationReceipt{}, fmt.Errorf("begin %s receipt: %w", execution.Label, err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	q := platformrepo.New(tx)
	lock := platformrepo.LockPlatformMCPOperationReceiptParams{OrganizationID: execution.Principal.OrganizationID, SubjectUrn: userSubjectURN(execution.Principal.UserID), ProjectID: execution.Project.ID.String(), Operation: execution.Operation, IdempotencyKey: execution.IdempotencyKey}
	if err := q.LockPlatformMCPOperationReceipt(ctx, lock); err != nil {
		return OperationReceipt{}, fmt.Errorf("lock %s receipt: %w", execution.Label, err)
	}
	lookup := platformrepo.GetPlatformMCPOperationReceiptParams{OrganizationID: execution.Principal.OrganizationID, UserID: conv.ToPGText(execution.Principal.UserID), SubjectUrn: userSubjectURN(execution.Principal.UserID), ProjectID: execution.Project.ID, Operation: execution.Operation, IdempotencyKey: execution.IdempotencyKey}
	if _, err := q.DeleteExpiredPlatformMCPOperationReceipt(ctx, platformrepo.DeleteExpiredPlatformMCPOperationReceiptParams(lookup)); err != nil {
		return OperationReceipt{}, fmt.Errorf("reclaim expired %s receipt: %w", execution.Label, err)
	}
	stored, err := q.GetPlatformMCPOperationReceipt(ctx, lookup)
	switch {
	case err == nil:
		if stored.InputHash != execution.InputHash {
			return OperationReceipt{}, execution.Conflict("The idempotency key was already used with different input.")
		}
		if stored.Status != receiptStatusSucceeded || len(stored.ResultPayload) == 0 {
			return OperationReceipt{}, execution.Conflict("The matching mutation has no completed replay result.")
		}
		if !execution.ValidateReplay(stored.ResultPayload) {
			return OperationReceipt{}, execution.Unavailable(errors.New("stored receipt payload is invalid"))
		}
		if err := tx.Commit(ctx); err != nil {
			return OperationReceipt{}, fmt.Errorf("commit %s replay: %w", execution.Label, err)
		}
		return operationReceiptFromRow(stored, true), nil
	case !errors.Is(err, pgx.ErrNoRows):
		return OperationReceipt{}, fmt.Errorf("load %s receipt: %w", execution.Label, err)
	}
	created, err := q.CreatePlatformMCPOperationReceipt(ctx, platformrepo.CreatePlatformMCPOperationReceiptParams{
		OrganizationID: execution.Principal.OrganizationID, ProjectID: execution.Project.ID, RegistrationID: uuid.NullUUID{UUID: uuid.Nil, Valid: false}, ConnectionID: connectionID, ConnectionGeneration: generation,
		UserID: conv.ToPGText(execution.Principal.UserID), ActingSurface: conv.ToPGText(string(execution.Principal.surface())), Operation: execution.Operation, IdempotencyKey: execution.IdempotencyKey,
		InputHash: execution.InputHash, Status: receiptStatusPending, ResultCode: pgtype.Text{String: "", Valid: false}, ResultPayload: nil, ExpiresAt: timestamp(execution.Now().UTC().Add(receiptLifetime)),
	})
	if err != nil {
		return OperationReceipt{}, fmt.Errorf("create %s receipt: %w", execution.Label, err)
	}
	result, err := execution.Mutate(ctx, tx)
	if err != nil {
		return OperationReceipt{}, err
	}
	payload, err := execution.EncodeResult(result)
	if err != nil {
		return OperationReceipt{}, err
	}
	completed, err := q.CompletePlatformMCPOperationReceipt(ctx, platformrepo.CompletePlatformMCPOperationReceiptParams{RegistrationID: uuid.NullUUID{UUID: uuid.Nil, Valid: false}, Status: receiptStatusSucceeded, ResultCode: conv.ToPGText("succeeded"), ResultPayload: payload, ID: created.ID, OrganizationID: execution.Principal.OrganizationID})
	if err != nil {
		return OperationReceipt{}, fmt.Errorf("complete %s receipt: %w", execution.Label, err)
	}
	if err := tx.Commit(ctx); err != nil {
		return OperationReceipt{}, fmt.Errorf("commit %s mutation: %w", execution.Label, err)
	}
	return operationReceiptFromRow(completed, false), nil
}
