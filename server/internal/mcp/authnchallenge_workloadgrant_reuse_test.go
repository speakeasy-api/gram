package mcp_test

import (
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	workloadpolicyrepo "github.com/speakeasy-api/gram/server/internal/workloadpolicy/repo"
)

func (f workloadGrantFixture) setAssertionReuse(t *testing.T, allow bool) {
	t.Helper()
	_, err := f.ti.conn.Exec(t.Context(), `UPDATE workload_issuers SET allow_assertion_reuse = $1 WHERE id = $2`, allow, f.issuerID)
	require.NoError(t, err)
}

func TestWorkloadAssertionGrant_ReusePolicyIsLive(t *testing.T) {
	t.Parallel()
	f := newWorkloadGrantFixture(t)
	f.setAssertionReuse(t, true)
	assertion := f.assertion(t, f.advertisedIssuer)
	first := f.exchange(t, assertion, f.resource)
	second := f.exchange(t, assertion, f.resource)
	f.requireWorkloadSession(t, first, f.advertisedIssuer)
	f.requireWorkloadSession(t, second, f.advertisedIssuer)
	require.NotEqual(t, accessTokenClaims(t, first.Body.Bytes())["jti"], accessTokenClaims(t, second.Body.Bytes())["jti"], "each exchange mints independent session authority")
	require.Len(t, f.workloadSessions(t), 2)
	f.setAssertionReuse(t, false)
	requireWorkloadGrantRefused(t, f.exchange(t, assertion, f.resource))
	require.Len(t, f.workloadSessions(t), 2)
}

func TestWorkloadAssertionGrant_ReuseStillRequiresCurrentAssignment(t *testing.T) {
	t.Parallel()
	f := newWorkloadGrantFixture(t)
	f.setAssertionReuse(t, true)
	assertion := f.assertion(t, f.advertisedIssuer)
	f.requireWorkloadSession(t, f.exchange(t, assertion, f.resource), f.advertisedIssuer)
	unassignWorkloadAgent(t, t.Context(), f.ti, f.assignmentID)
	requireWorkloadGrantRefused(t, f.exchange(t, assertion, f.resource))
	require.Len(t, f.workloadSessions(t), 1)
}

func TestWorkloadAssertionGrant_ConcurrentReuseMintsIndependentSessions(t *testing.T) {
	t.Parallel()
	f := newWorkloadGrantFixture(t)
	f.setAssertionReuse(t, true)
	assertion := f.assertion(t, f.advertisedIssuer)
	const count = 4
	results := make(chan *httptest.ResponseRecorder, count)
	var wg sync.WaitGroup
	for range count {
		wg.Go(func() { results <- f.exchange(t, assertion, f.resource) })
	}
	wg.Wait()
	close(results)
	for result := range results {
		require.Equal(t, http.StatusOK, result.Code, result.Body.String())
	}
	require.Len(t, f.workloadSessions(t), count)
	f.setAssertionReuse(t, false)
	requireWorkloadGrantRefused(t, f.exchange(t, assertion, f.resource))
}

func TestWorkloadAssertionGrant_DisableDuringVerificationFailsClosed(t *testing.T) {
	t.Parallel()
	f := newWorkloadGrantFixture(t)
	f.setAssertionReuse(t, true)
	assertion := f.assertion(t, f.advertisedIssuer)
	f.requireWorkloadSession(t, f.exchange(t, assertion, f.resource), f.advertisedIssuer)
	tx, err := f.ti.conn.Begin(t.Context())
	require.NoError(t, err)
	defer func() { _ = tx.Rollback(t.Context()) }()
	_, err = workloadpolicyrepo.New(tx).LockWorkloadIssuerForWrite(t.Context(), workloadpolicyrepo.LockWorkloadIssuerForWriteParams{OrganizationID: f.fx.orgID, ID: f.issuerID})
	require.NoError(t, err)
	result := make(chan *httptest.ResponseRecorder, 1)
	go func() { result <- f.exchange(t, assertion, f.resource) }()
	// The exchange has verified with opt-in, and now waits on the same lock
	// as issuer policy edits. Commit the disable before its final policy read.
	require.Eventually(t, func() bool {
		var blocked bool
		err := f.ti.conn.QueryRow(t.Context(), `SELECT EXISTS (SELECT 1 FROM pg_stat_activity WHERE datname = current_database() AND wait_event_type = 'Lock' AND query LIKE '%LockWorkloadIssuerForWrite%')`).Scan(&blocked)
		return err == nil && blocked
	}, 10*time.Second, 10*time.Millisecond)
	_, err = tx.Exec(t.Context(), `UPDATE workload_issuers SET allow_assertion_reuse = false WHERE id = $1`, f.issuerID)
	require.NoError(t, err)
	require.NoError(t, tx.Commit(t.Context()))
	select {
	case w := <-result:
		requireWorkloadGrantRefused(t, w)
	case <-time.After(10 * time.Second):
		t.Fatal("exchange did not finish after policy edit")
	}
	require.Len(t, f.workloadSessions(t), 1)
}
