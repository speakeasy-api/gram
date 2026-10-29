package authclientbackfill

import (
	"context"
	"fmt"
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
	firstPID := first.Conn().PgConn().PID()
	secondPID := second.Conn().PgConn().PID()
	bounded, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() {
		err := SyncEMABinding(bounded, second, f.orgID, binding)
		if err == nil {
			err = second.Commit(bounded)
		}
		done <- err
	}()
	// Observe the second writer blocked by the first transaction, rather than
	// assuming it reached the preparation lock before a short deadline.
	require.Eventually(t, func() bool {
		var waiting bool
		err := f.pool.QueryRow(bounded, "SELECT $1::int = ANY(pg_blocking_pids($2::int))", firstPID, secondPID).Scan(&waiting) //nolint:glint // notestingrawsql: live backend lock instrumentation, not application data.
		return err == nil && waiting
	}, 5*time.Second, 10*time.Millisecond, "second preparation never blocked on the first writer")
	require.NoError(t, first.Commit(t.Context()))
	require.NoError(t, <-done)
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
	tag, err := q.DBTX.Exec(ctx, query, args...) //nolint:glint // notestingrawsql: fault injector delegates dynamically intercepted SQLc statements.
	if err != nil {
		return tag, fmt.Errorf("execute injected query: %w", err)
	}
	return tag, nil
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

func TestCopyOntoEMAClient_RetiresCopiesOfDeletedProject(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	usi := f.userSessionIssuer()
	source, server := f.emaSourceAt(gatewayResource, usi)
	f.staticHeader(source, "X-Enterprise-Key")
	client, issuer := f.emaClient()
	binding := f.binding(usi, issuer, client, gatewayResource, []string{"read"})
	tx := testenv.BeginTx(t, t.Context(), f.pool)
	require.NoError(t, SyncEMABinding(t.Context(), tx, f.orgID, binding))
	require.NoError(t, tx.Commit(t.Context()))
	id := EMAClientID(f.orgID, issuer, client, gatewayResource)
	require.Len(t, f.headers(id), 1)

	require.NoError(t, f.q.SoftDeleteProjectFixture(t.Context(), f.projectID))
	tx = testenv.BeginTx(t, t.Context(), f.pool)
	report := newReport(true)
	// A previously read owner list must not retain credentials after its
	// project disappears while the server itself remains undeleted.
	require.NoError(t, copyOntoEMAClient(withAtomicHeaderSync(t.Context()), tx, New(tx),
		Options{OrganizationID: f.orgID, Apply: true, PageSize: 0}, &report, id, true, []uuid.UUID{server}))
	require.NoError(t, tx.Commit(t.Context()))
	require.Empty(t, f.headers(id), "deleted project's copied credentials survived the owner reload")
}
