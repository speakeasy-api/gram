package platformmcp

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	platformrepo "github.com/speakeasy-api/gram/server/internal/platformmcp/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
)

// noReceiptCharge is the charge for a receipt store exercised directly, where
// no budget is under test.
func noReceiptCharge(context.Context) error { return nil }

type receiptProbeResult struct {
	Writes int `json:"writes"`
}

// receiptProbe drives executeChargedMutationReceipt with a counting charge and
// a counting write, so a test can see which of the two each call reached.
type receiptProbe struct {
	conn      *pgxpool.Pool
	principal Principal
	project   ResolvedProject
	now       func() time.Time
	charges   int
	writes    int
	exhausted bool
}

func newReceiptProbe(t *testing.T, name string) *receiptProbe {
	t.Helper()
	conn, err := platformMCPInfra.CloneTestDatabase(t, name)
	require.NoError(t, err)
	principal, project := seedRegistrationLifecycle(t, t.Context(), conn)
	return &receiptProbe{conn: conn, principal: principal, project: project, now: time.Now, charges: 0, writes: 0, exhausted: false}
}

func (p *receiptProbe) execute(ctx context.Context, key, input string) (OperationReceipt, error) {
	charge := func(context.Context) error {
		p.charges++
		if p.exhausted {
			return ErrOperationRateLimited
		}
		return nil
	}
	return executeChargedMutationReceipt(ctx, charge, mutationReceiptExecution[receiptProbeResult]{
		DB: p.conn, Now: p.now, Principal: p.principal, Project: p.project, Operation: operationCreateRiskPolicy,
		IdempotencyKey: key, InputHash: input, Label: "receipt probe",
		Invalid:     func(err error) error { return err },
		Conflict:    errors.New,
		Unavailable: func(err error) error { return err },
		ValidateReplay: func(payload []byte) bool {
			var result receiptProbeResult
			return json.Unmarshal(payload, &result) == nil && result.Writes > 0
		},
		EncodeResult: func(result receiptProbeResult) ([]byte, error) { return json.Marshal(result) },
		Mutate: func(context.Context, pgx.Tx) (receiptProbeResult, error) {
			p.writes++
			return receiptProbeResult{Writes: p.writes}, nil
		},
	})
}

// A replay does no work, so it must cost nothing: once the allowance is spent,
// a retry of a write that already committed still returns its stored result
// instead of rate_limited, and never reaches the charge at all.
func TestChargedMutationReceiptReplayIsFree(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	probe := newReceiptProbe(t, "platform_mcp_receipt_replay_free")

	first, err := probe.execute(ctx, "replay-free", "input-a")
	require.NoError(t, err)
	require.False(t, first.Replayed)
	require.Equal(t, 1, probe.charges)

	probe.exhausted = true
	replayed, err := probe.execute(ctx, "replay-free", "input-a")
	require.NoError(t, err, "a replay must not be refused by a spent allowance")
	require.True(t, replayed.Replayed)
	require.Equal(t, first.ID, replayed.ID)
	require.Equal(t, 1, probe.charges, "a replay must not be charged")
	require.Equal(t, 1, probe.writes)

	// A new request under the same spent allowance is still refused, and is
	// refused before any transaction does work.
	_, err = probe.execute(ctx, "new-key", "input-b")
	require.ErrorIs(t, err, ErrOperationRateLimited)
	require.Equal(t, 2, probe.charges)
	require.Equal(t, 1, probe.writes)
}

// The charge is a Redis round-trip and must never run while a PostgreSQL
// connection, and with it the receipt lock, is held.
func TestChargedMutationReceiptChargesOutsideAnyTransaction(t *testing.T) {
	t.Parallel()
	probe := newReceiptProbe(t, "platform_mcp_receipt_charge_outside_tx")

	var acquired []int32
	_, err := executeChargedMutationReceipt(t.Context(), func(context.Context) error {
		acquired = append(acquired, probe.conn.Stat().AcquiredConns())
		return nil
	}, mutationReceiptExecution[receiptProbeResult]{
		DB: probe.conn, Now: time.Now, Principal: probe.principal, Project: probe.project, Operation: operationCreateRiskPolicy,
		IdempotencyKey: "outside-tx", InputHash: "input", Label: "receipt probe",
		Invalid: func(err error) error { return err }, Conflict: errors.New, Unavailable: func(err error) error { return err },
		ValidateReplay: func([]byte) bool { return true },
		EncodeResult:   func(result receiptProbeResult) ([]byte, error) { return json.Marshal(result) },
		Mutate:         func(context.Context, pgx.Tx) (receiptProbeResult, error) { return receiptProbeResult{Writes: 1}, nil },
	})
	require.NoError(t, err)
	require.Equal(t, []int32{0}, acquired)
}

// Expiry is judged with the database clock on both paths. The application
// clock here lags the database by an hour, so a receipt the database already
// treats as expired still looks live to it. The pre-check must not replay that
// receipt: the locked path would reclaim it and apply the request again, and
// a pre-check replay would report a stale result for a change never made.
func TestChargedMutationReceiptExpiryUsesDatabaseClock(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	probe := newReceiptProbe(t, "platform_mcp_receipt_expiry_db_clock")
	probe.now = func() time.Time { return time.Now().Add(-time.Hour) }

	first, err := probe.execute(ctx, "expiring", "input-a")
	require.NoError(t, err)
	expiresAt, err := testrepo.New(probe.conn).ExpirePlatformMCPOperationReceiptFixture(ctx, first.ID)
	require.NoError(t, err)
	require.True(t, expiresAt.Time.After(probe.now()), "the lagging application clock still sees the stored expiry as the future")

	again, err := probe.execute(ctx, "expiring", "input-a")
	require.NoError(t, err)
	require.False(t, again.Replayed, "an expired receipt must not replay")
	require.NotEqual(t, first.ID, again.ID, "the expired receipt is reclaimed and a fresh one written")
	require.Equal(t, 2, probe.charges, "a request that is not a replay is charged")
	require.Equal(t, 2, probe.writes, "the request is applied again")
}

// The sweep removes expired receipts whether or not their key is ever written
// again — the per-key reclaim in the receipt transaction never reaches a key
// that is not — in bounded batches, and leaves unexpired receipts alone.
func TestDeleteExpiredPlatformMCPOperationReceiptsBatch(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	probe := newReceiptProbe(t, "platform_mcp_receipt_sweep")

	live, err := probe.execute(ctx, "live", "input")
	require.NoError(t, err)
	expired := make([]OperationReceipt, 0, 3)
	for _, key := range []string{"expired-1", "expired-2", "expired-3"} {
		receipt, err := probe.execute(ctx, key, "input")
		require.NoError(t, err)
		expired = append(expired, receipt)
	}
	fixtures := testrepo.New(probe.conn)
	for _, receipt := range expired {
		_, err := fixtures.ExpirePlatformMCPOperationReceiptFixture(ctx, receipt.ID)
		require.NoError(t, err)
	}

	queries := platformrepo.New(probe.conn)
	deleted, err := queries.DeleteExpiredPlatformMCPOperationReceiptsBatch(ctx, 2)
	require.NoError(t, err)
	require.Equal(t, int64(2), deleted, "a batch never deletes more than its size")
	deleted, err = queries.DeleteExpiredPlatformMCPOperationReceiptsBatch(ctx, 2)
	require.NoError(t, err)
	require.Equal(t, int64(1), deleted)
	deleted, err = queries.DeleteExpiredPlatformMCPOperationReceiptsBatch(ctx, 2)
	require.NoError(t, err)
	require.Zero(t, deleted)

	remaining, err := fixtures.ListPlatformMCPOperationReceiptIDsFixture(ctx, probe.principal.OrganizationID)
	require.NoError(t, err)
	require.Equal(t, []uuid.UUID{live.ID}, remaining)
}

// A failed pre-check read cannot tell a replay from a first attempt, so it is
// refused as unavailable before the charge rather than spending the allowance
// on what may have been a free replay.
func TestChargedMutationReceiptFailedPreCheckIsNotCharged(t *testing.T) {
	t.Parallel()
	probe := newReceiptProbe(t, "platform_mcp_receipt_failed_precheck")
	probe.conn.Close()

	_, err := probe.execute(t.Context(), "failed-read", "input")
	require.ErrorContains(t, err, "look up replay receipt", "the pre-check refuses the request")
	require.Zero(t, probe.charges, "a failed pre-check read must not be charged")
	require.Zero(t, probe.writes)
}

// A key that can only be refused is refused by the pre-check, with the locked
// path's own refusal, and costs nothing: a different input under a used key
// is a conflict however often it is retried.
func TestChargedMutationReceiptDeterministicRefusalIsFree(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	probe := newReceiptProbe(t, "platform_mcp_receipt_refusal_free")

	_, err := probe.execute(ctx, "reused", "input-a")
	require.NoError(t, err)
	require.Equal(t, 1, probe.charges)

	probe.exhausted = true
	_, err = probe.execute(ctx, "reused", "input-b")
	require.EqualError(t, err, receiptKeyReusedMessage, "the refusal is the conflict, not rate_limited")
	require.Equal(t, 1, probe.charges, "a refusal the stored receipt decides is not charged")
	require.Equal(t, 1, probe.writes)
}
