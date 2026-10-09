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
//  1. an unlocked, read-only lookup of an unexpired receipt for this key. A
//     completed receipt for exactly this input is answered without charging,
//     and a receipt that can only be refused — a different input under the
//     key, no completed result, or an unreadable one — is refused without
//     charging, with the same refusal the locked path gives;
//  2. otherwise, including when the lookup itself fails, charge, still
//     outside any transaction. The pre-check is only an optimisation, so a
//     failed read must not refuse a request the locked path could serve;
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
	replay, check := completedMutationReceipt(ctx, execution)
	switch check {
	case receiptReplay:
		return replay, nil
	case receiptKeyReused:
		return OperationReceipt{}, execution.Conflict(receiptKeyReusedMessage)
	case receiptIncomplete:
		return OperationReceipt{}, execution.Conflict(receiptIncompleteMessage)
	case receiptUnreadable:
		return OperationReceipt{}, execution.Unavailable(errStoredReceiptInvalid)
	case receiptMiss:
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

// receiptCheck is what a pre-check lookup found under a request's key. Every
// answer but receiptMiss is final: receipts are written and completed in one
// transaction and expire on the database clock, so a stored row cannot change
// before the locked path would read it.
type receiptCheck int

const (
	// receiptMiss is no unexpired receipt, or a failed read; the locked path
	// decides.
	receiptMiss receiptCheck = iota
	// receiptReplay is a completed receipt for exactly this input.
	receiptReplay
	// receiptKeyReused is a receipt for a different input under the key.
	receiptKeyReused
	// receiptIncomplete is a receipt with no completed result.
	receiptIncomplete
	// receiptUnreadable is a completed result the caller cannot decode.
	receiptUnreadable
)

const (
	receiptKeyReusedMessage  = "The idempotency key was already used with different input."
	receiptIncompleteMessage = "The matching mutation has no completed replay result."
)

var errStoredReceiptInvalid = errors.New("stored receipt payload is invalid")

// noReplay is the empty receipt a pre-check returns beside anything but
// receiptReplay.
var noReplay OperationReceipt

// completedMutationReceipt is the unlocked pre-check; see receiptCheck.
func completedMutationReceipt[T any](ctx context.Context, execution mutationReceiptExecution[T]) (OperationReceipt, receiptCheck) {
	if execution.DB == nil || execution.ValidateReplay == nil {
		return noReplay, receiptMiss
	}
	// A caller the locked path would refuse as malformed gets no answer here
	// either; it falls through to that refusal.
	if _, _, err := principalConnection(execution.Principal); err != nil {
		return noReplay, receiptMiss
	}
	row, err := platformrepo.New(execution.DB).GetUnexpiredPlatformMCPOperationReceipt(ctx, platformrepo.GetUnexpiredPlatformMCPOperationReceiptParams{
		OrganizationID: execution.Principal.OrganizationID, ProjectID: execution.Project.ID, Operation: execution.Operation, IdempotencyKey: execution.IdempotencyKey,
		UserID: conv.ToPGText(execution.Principal.UserID), SubjectUrn: userSubjectURN(execution.Principal.UserID),
	})
	if err != nil {
		return noReplay, receiptMiss
	}
	return replayableReceipt(row, execution.InputHash, execution.ValidateReplay)
}

// replayableReceipt judges a stored receipt against this request, in the same
// order the locked path does. Expiry is not judged here: the query that loaded
// the row already filtered it with the database clock, the same clock the
// locked path uses.
func replayableReceipt(row platformrepo.PlatformMcpOperationReceipt, inputHash string, valid func([]byte) bool) (OperationReceipt, receiptCheck) {
	switch {
	case row.InputHash != inputHash:
		return noReplay, receiptKeyReused
	case row.Status != receiptStatusSucceeded || len(row.ResultPayload) == 0:
		return noReplay, receiptIncomplete
	case !valid(row.ResultPayload):
		return noReplay, receiptUnreadable
	default:
		return operationReceiptFromRow(row, true), receiptReplay
	}
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
			return OperationReceipt{}, execution.Conflict(receiptKeyReusedMessage)
		}
		if stored.Status != receiptStatusSucceeded || len(stored.ResultPayload) == 0 {
			return OperationReceipt{}, execution.Conflict(receiptIncompleteMessage)
		}
		if !execution.ValidateReplay(stored.ResultPayload) {
			return OperationReceipt{}, execution.Unavailable(errStoredReceiptInvalid)
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
