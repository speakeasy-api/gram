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

// executeChargedMutationReceipt is the entry point for a write that spends an
// operation budget. A replay does no work, so it must cost nothing, and the
// budget is a Redis round-trip, so it must never be charged while a
// PostgreSQL connection or the receipt lock is held. The order is therefore:
//
//  1. an unlocked, read-only lookup of a completed, unexpired receipt for
//     exactly this request, answered without charging. A failed lookup is
//     refused as unavailable rather than charged, since it may have been a
//     replay;
//  2. on a miss, charge, still outside any transaction;
//  3. executeMutationReceipt, whose locked re-check still replays a duplicate
//     that committed concurrently. Two racing first attempts may then both be
//     charged, which errs on the side of the budget.
//
// Callers run their validation and authorization before this, so a caller
// cannot use the replay to probe for a target it may not see.
func executeChargedMutationReceipt[T any](ctx context.Context, charge func(context.Context) error, execution mutationReceiptExecution[T]) (OperationReceipt, error) {
	if charge == nil {
		return OperationReceipt{}, execution.Unavailable(errors.New("mutation receipt charge is missing"))
	}
	replay, ok, err := completedMutationReceipt(ctx, execution)
	if err != nil {
		return OperationReceipt{}, execution.Unavailable(err)
	}
	if ok {
		return replay, nil
	}
	if err := charge(ctx); err != nil {
		return OperationReceipt{}, err
	}
	return executeMutationReceipt(ctx, execution)
}

// chargeRerun charges the work that follows a committed receipt: a provider
// reconciliation, a publish or index signal. A first attempt already paid for
// it with its own charge, so it returns nil at once. A replay answers free, but
// re-running that work is real provider or Temporal traffic, so it is charged
// here and the caller skips or refuses the work when this fails. A retry loop
// then cannot send unbounded work, while a retry within the allowance still
// recovers a sync that failed after the commit. Like the main charge, it must
// run outside any transaction.
func chargeRerun(ctx context.Context, receipt OperationReceipt, charge func(context.Context) error) error {
	if !receipt.Replayed {
		return nil
	}
	return charge(ctx)
}

// noReplay is the empty receipt a pre-check returns beside false.
var noReplay OperationReceipt

// completedMutationReceipt is the unlocked pre-check. Anything but a completed,
// unexpired receipt for exactly this input — no receipt, a pending one, a
// different input under the same key, or an invalid payload — is a miss, and
// executeMutationReceipt decides it under its lock. A failed read is returned
// as an error instead: it cannot tell a replay from a first attempt, so the
// caller must not charge on it.
func completedMutationReceipt[T any](ctx context.Context, execution mutationReceiptExecution[T]) (OperationReceipt, bool, error) {
	if execution.DB == nil || execution.ValidateReplay == nil {
		return noReplay, false, nil
	}
	// A caller the locked path would refuse as malformed gets no replay here
	// either; it falls through to that refusal.
	if _, _, err := principalConnection(execution.Principal); err != nil {
		return noReplay, false, nil
	}
	row, err := platformrepo.New(execution.DB).GetUnexpiredPlatformMCPOperationReceipt(ctx, platformrepo.GetUnexpiredPlatformMCPOperationReceiptParams{
		OrganizationID: execution.Principal.OrganizationID, ProjectID: execution.Project.ID, Operation: execution.Operation, IdempotencyKey: execution.IdempotencyKey,
		UserID: conv.ToPGText(execution.Principal.UserID), SubjectUrn: userSubjectURN(execution.Principal.UserID),
	})
	return replayableReceipt(row, err, execution.InputHash, execution.ValidateReplay)
}

// replayableReceipt judges a pre-check lookup: whether the stored receipt
// answers this exact request, a plain miss, or a failed read, which is
// returned. Expiry is not judged here: the query that loaded the row already
// filtered it with the database clock, the same clock the locked path uses.
func replayableReceipt(row platformrepo.PlatformMcpOperationReceipt, lookupErr error, inputHash string, valid func([]byte) bool) (OperationReceipt, bool, error) {
	if errors.Is(lookupErr, pgx.ErrNoRows) {
		return noReplay, false, nil
	}
	if lookupErr != nil {
		return noReplay, false, fmt.Errorf("look up replay receipt: %w", lookupErr)
	}
	if row.InputHash != inputHash || row.Status != receiptStatusSucceeded || len(row.ResultPayload) == 0 || !valid(row.ResultPayload) {
		return noReplay, false, nil
	}
	return operationReceiptFromRow(row, true), true, nil
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
