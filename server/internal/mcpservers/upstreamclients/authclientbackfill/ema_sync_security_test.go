package authclientbackfill

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/stretchr/testify/require"
)

func TestSyncEMABinding_SerializesSharedRegistration(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	usi := f.userSessionIssuer()
	f.serverAt(gatewayResource, usi)
	client, issuer := f.emaClient()
	binding := f.binding(usi, issuer, client, gatewayResource, []string{"read"})
	first := testenv.BeginTx(t, t.Context(), f.pool)
	require.NoError(t, SyncEMABinding(t.Context(), first, f.orgID, binding))

	second := testenv.BeginTx(t, t.Context(), f.pool)
	bounded, cancel := context.WithTimeout(t.Context(), 200*time.Millisecond)
	defer cancel()
	// The first transaction has not committed its typed client. The helper must
	// wait on the source scope before taking the shared registration lock.
	err := SyncEMABinding(bounded, second, f.orgID, binding)
	require.ErrorContains(t, err, "lock EMA preparation scope")
	require.ErrorIs(t, bounded.Err(), context.DeadlineExceeded)
	_ = second.Rollback(t.Context()) // Cancellation can close the connection.
	require.NoError(t, first.Commit(t.Context()))

	retry := testenv.BeginTx(t, t.Context(), f.pool)
	require.NoError(t, SyncEMABinding(t.Context(), retry, f.orgID, binding))
	require.NoError(t, retry.Commit(t.Context()))
	require.Len(t, f.live(TypeEMA), 1)
}

func TestCopyOntoEMAClient_ReloadsOwnersAfterRegistrationLock(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	usi := f.userSessionIssuer()
	source, server := f.emaSourceAt(gatewayResource, usi)
	other, _ := f.emaSourceAt(gatewayResource, usi)
	f.staticHeader(source, "X-First-Key")
	f.staticHeader(other, "X-Second-Key")
	client, issuer := f.emaClient()
	binding := f.binding(usi, issuer, client, gatewayResource, []string{"read"})
	tx := testenv.BeginTx(t, t.Context(), f.pool)
	require.NoError(t, SyncEMABinding(t.Context(), tx, f.orgID, binding))
	require.NoError(t, tx.Commit(t.Context()))
	id := EMAClientID(f.orgID, issuer, client, gatewayResource)
	require.Empty(t, f.headers(id))

	tx = testenv.BeginTx(t, t.Context(), f.pool)
	report := newReport(true)
	// A caller read this single-server list before the second link committed.
	// Copying from it without re-reading would leak one source's secret to both.
	require.NoError(t, copyOntoEMAClient(withAtomicHeaderSync(t.Context()), tx, New(tx),
		Options{OrganizationID: f.orgID, Apply: true}, &report, id, true, []uuid.UUID{server}))
	require.NoError(t, tx.Commit(t.Context()))
	require.Empty(t, f.headers(id))
}

type refusedHeaderRetirement struct{ DBTX }

func (q refusedHeaderRetirement) Exec(ctx context.Context, query string, args ...any) (pgconn.CommandTag, error) {
	if strings.Contains(query, "-- name: RetireHeaderCopy") {
		return pgconn.NewCommandTag("UPDATE 0"), nil
	}
	return q.DBTX.Exec(ctx, query, args...)
}

func TestAtomicHeaderSync_RejectsSkippedRetirement(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	source := f.source()
	f.serverOn(source)
	f.staticHeader(source, "X-API-Key")
	f.sync(source)
	id := SourceClientID(f.orgID, SourceKindRemote, source, TypeStaticCredential)
	require.Len(t, f.headers(id), 1)
	tx := testenv.BeginTx(t, t.Context(), f.pool)
	report := newReport(true)
	_, raced, err := syncHeaders(withAtomicHeaderSync(t.Context()), tx, New(refusedHeaderRetirement{tx}),
		Options{OrganizationID: f.orgID, Apply: true}, &report, KindHeader, id, true,
		uuid.NullUUID{}, uuid.NullUUID{}, SourceKindRemote, nil)
	require.True(t, raced)
	require.ErrorContains(t, err, "credential copy ownership changed concurrently")
	require.NoError(t, tx.Rollback(t.Context()))
	require.Len(t, f.headers(id), 1)
}
