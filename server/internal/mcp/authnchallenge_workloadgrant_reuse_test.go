package mcp_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/speakeasy-api/gram/server/internal/auth/deviceagenttoken"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
	"github.com/stretchr/testify/require"

	workloadpolicyrepo "github.com/speakeasy-api/gram/server/internal/workloadpolicy/repo"
)

func (f workloadGrantFixture) setAssertionReuse(t *testing.T, allow bool) {
	t.Helper()
	err := testrepo.New(f.ti.conn).SetWorkloadIssuerAssertionReuseFixture(t.Context(), testrepo.SetWorkloadIssuerAssertionReuseFixtureParams{AllowAssertionReuse: allow, ID: f.issuerID, OrganizationID: f.fx.orgID})
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
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	const count = 4
	results := make(chan workloadExchangeResult, count)
	var wg sync.WaitGroup
	for range count {
		wg.Go(func() { results <- f.exchangeResult(ctx, assertion) })
	}
	wg.Wait()
	close(results)
	for result := range results {
		require.NoError(t, result.err)
		require.Equal(t, http.StatusOK, result.response.Code, result.response.Body.String())
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
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	result := make(chan workloadExchangeResult, 1)
	go func() { result <- f.exchangeResult(ctx, assertion) }()
	// The exchange has verified with opt-in, and now waits on the same lock
	// as issuer policy edits. Commit the disable before its final policy read.
	require.Eventually(t, func() bool {
		blocked, err := testrepo.New(f.ti.conn).WorkloadIssuerWriteIsBlockedFixture(t.Context())
		return err == nil && blocked
	}, 10*time.Second, 10*time.Millisecond)
	err = testrepo.New(tx).SetWorkloadIssuerAssertionReuseFixture(t.Context(), testrepo.SetWorkloadIssuerAssertionReuseFixtureParams{AllowAssertionReuse: false, ID: f.issuerID, OrganizationID: f.fx.orgID})
	require.NoError(t, err)
	require.NoError(t, tx.Commit(t.Context()))
	select {
	case w := <-result:
		require.NoError(t, w.err)
		requireWorkloadGrantRefused(t, w.response)
	case <-time.After(10 * time.Second):
		t.Fatal("exchange did not finish after policy edit")
	}
	require.Len(t, f.workloadSessions(t), 1)
}

// Worker goroutines return errors; only the test goroutine may call FailNow.
type workloadExchangeResult struct {
	response *httptest.ResponseRecorder
	err      error
}

func (f workloadGrantFixture) exchangeResult(ctx context.Context, assertion string) workloadExchangeResult {
	form := url.Values{"grant_type": {workloadGrantJWTBearer}, "assertion": {assertion}, "resource": {f.resource}}
	slug := f.fx.toolset.McpSlug.String
	req := httptest.NewRequestWithContext(ctx, http.MethodPost, "/mcp/"+slug+"/token", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	route := chi.NewRouteContext()
	route.URLParams.Add("mcpSlug", slug)
	req = req.WithContext(context.WithValue(ctx, chi.RouteCtxKey, route))
	response := httptest.NewRecorder()
	err := f.ti.service.HandleToken(response, req)
	return workloadExchangeResult{response: response, err: err}
}

func TestWorkloadAssertionGrant_SingleConnectionPool(t *testing.T) {
	t.Parallel()
	f := newWorkloadGrantFixtureWithPool(t, testenv.NewLogger(t), "", func(config *pgxpool.Config) {
		config.MaxConns = 1
		config.MinConns = 0
	})
	f.setAssertionReuse(t, true)
	assertion := f.assertion(t, f.advertisedIssuer)
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	result := f.exchangeResult(ctx, assertion)
	require.NoError(t, result.err)
	require.Equal(t, http.StatusOK, result.response.Code, result.response.Body.String())
}

func TestWorkloadAssertionGrant_DeviceDisableDuringVerificationFailsClosed(t *testing.T) {
	t.Parallel()
	f := newDeviceAgentGrantFixture(t)
	f.setAssertionReuse(t, true)
	assertion := f.assertion(t, f.tokenURL)
	first := f.exchangeDeviceAgent(t, assertion)
	require.Equal(t, http.StatusOK, first.Code, first.Body.String())
	tx, err := f.ti.conn.Begin(t.Context())
	require.NoError(t, err)
	defer func() { _ = tx.Rollback(t.Context()) }()
	_, err = workloadpolicyrepo.New(tx).LockWorkloadIssuerForWrite(t.Context(), workloadpolicyrepo.LockWorkloadIssuerForWriteParams{OrganizationID: f.fx.orgID, ID: f.issuerID})
	require.NoError(t, err)
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	results := make(chan workloadExchangeResult, 1)
	go func() {
		form := url.Values{"grant_type": {workloadGrantJWTBearer}, "assertion": {assertion}}
		req := httptest.NewRequestWithContext(ctx, http.MethodPost, f.tokenURL, strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		route := chi.NewRouteContext()
		route.URLParams.Add(deviceagenttoken.TokenPathOrganizationParam, f.fx.orgID)
		req = req.WithContext(context.WithValue(ctx, chi.RouteCtxKey, route))
		response := httptest.NewRecorder()
		err := f.ti.service.HandleDeviceAgentToken(response, req)
		results <- workloadExchangeResult{response: response, err: err}
	}()
	require.Eventually(t, func() bool {
		blocked, err := testrepo.New(f.ti.conn).WorkloadIssuerWriteIsBlockedFixture(t.Context())
		return err == nil && blocked
	}, 10*time.Second, 10*time.Millisecond)
	err = testrepo.New(tx).SetWorkloadIssuerAssertionReuseFixture(t.Context(), testrepo.SetWorkloadIssuerAssertionReuseFixtureParams{AllowAssertionReuse: false, ID: f.issuerID, OrganizationID: f.fx.orgID})
	require.NoError(t, err)
	require.NoError(t, tx.Commit(t.Context()))
	select {
	case result := <-results:
		require.NoError(t, result.err)
		requireWorkloadGrantRefused(t, result.response)
	case <-ctx.Done():
		t.Fatal("device exchange did not finish after policy edit")
	}
}
