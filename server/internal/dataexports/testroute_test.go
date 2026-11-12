package dataexports_test

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/data_exports"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/authztest"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/dataexports"
	"github.com/speakeasy-api/gram/server/internal/oops"
	projectsrepo "github.com/speakeasy-api/gram/server/internal/projects/repo"
	"github.com/speakeasy-api/gram/server/internal/ratelimit"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

const testRouteSecret = "test-route-secret-value"

// testRouteDestination is a collector answering status, counting requests that carry the secret header.
func testRouteDestination(t *testing.T, status int) (*httptest.Server, *atomic.Int64) {
	t.Helper()
	return testRouteDestinationByPath(t, map[string]int{"/v1/logs": status, "/v1/traces": status, "/v1/metrics": status})
}

// testRouteDestinationByPath answers each OTLP path with its own status.
func testRouteDestinationByPath(t *testing.T, statuses map[string]int) (*httptest.Server, *atomic.Int64) {
	t.Helper()
	var received atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		status, ok := statuses[r.URL.Path]
		if !ok {
			status = http.StatusNotFound
		}
		if ok && r.Header.Get("Dd-Api-Key") == testRouteSecret {
			received.Add(1)
		}
		w.WriteHeader(status)
		_, _ = w.Write([]byte("response body " + testRouteSecret))
	}))
	t.Cleanup(server.Close)
	return server, &received
}

func createTestableRoute(t *testing.T, ctx context.Context, ti *testInstance, endpointURL, dataSource string) *gen.DataExportRoute {
	t.Helper()
	destination, err := ti.service.CreateDestination(ctx, &gen.CreateDestinationPayload{SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil,
		Name: "Collector", DestinationType: "otel", SensitiveData: "exclude",
		Otel: &gen.CreateOtelDestinationInput{EndpointURL: endpointURL, Headers: []*gen.CreateOtelDestinationHeaderInput{{Name: "DD-API-KEY", Value: testRouteSecret}}}})
	require.NoError(t, err)
	route, err := ti.service.CreateRoute(ctx, &gen.CreateRoutePayload{SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil,
		DataSource: dataSource, Enabled: true, OtelDestinationID: &destination.ID})
	require.NoError(t, err)
	return route
}

func testRoute(ctx context.Context, ti *testInstance, routeID string) (*gen.TestDataExportRouteResult, error) {
	result, err := ti.service.TestRoute(ctx, &gen.TestRoutePayload{ID: routeID, SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil})
	if err != nil {
		return nil, fmt.Errorf("test route: %w", err)
	}
	return result, nil
}

func TestTestRouteMapsDestinationAnswers(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		status  int
		outcome string
		message string
	}{
		{name: "accepted", status: http.StatusAccepted, outcome: "accepted", message: "The destination accepted the test event."},
		{name: "unauthorized", status: http.StatusUnauthorized, outcome: "refused", message: "The destination refused the test event. Check the API key, and for Datadog, that the endpoint matches your site."},
		{name: "forbidden", status: http.StatusForbidden, outcome: "refused", message: "The destination refused the test event. Check the API key, and for Datadog, that the endpoint matches your site."},
		{name: "not found", status: http.StatusNotFound, outcome: "not_found", message: "No logs endpoint at this address. Check the URL; Speakeasy adds /v1/logs."},
		{name: "server error", status: http.StatusInternalServerError, outcome: "error", message: "The destination returned an error (HTTP 500)."},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			ctx, ti := newTestService(t)
			server, received := testRouteDestination(t, tc.status)
			route := createTestableRoute(t, ctx, ti, server.URL, dataexports.DataSourceMCPConnectionLogs)
			before, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionDataExportRouteTest)
			require.NoError(t, err)

			result, err := testRoute(ctx, ti, route.ID)
			require.NoError(t, err)
			require.Equal(t, tc.outcome, result.Outcome)
			require.NotNil(t, result.StatusCode)
			require.Equal(t, tc.status, *result.StatusCode)
			require.Equal(t, tc.message, result.Message)
			require.NotContains(t, result.Message, testRouteSecret)
			require.Len(t, result.Signals, 1)
			require.Equal(t, "logs", result.Signals[0].Signal)
			require.Equal(t, tc.outcome, result.Signals[0].Outcome)
			require.Equal(t, tc.message, *result.Signals[0].Message)
			// One request, no retries, even for a retryable 500.
			require.EqualValues(t, 1, received.Load())

			after, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionDataExportRouteTest)
			require.NoError(t, err)
			require.Equal(t, before+1, after)
			entry, err := audittest.LatestAuditLogByAction(ctx, ti.conn, audit.ActionDataExportRouteTest)
			require.NoError(t, err)
			require.Equal(t, route.ID, entry.SubjectID)
			require.NotContains(t, string(entry.Metadata), testRouteSecret)
			metadata, err := audittest.DecodeAuditData(entry.Metadata)
			require.NoError(t, err)
			require.Equal(t, tc.outcome, metadata["outcome"])
			require.InDelta(t, float64(tc.status), metadata["status_code"], 0)
			require.Equal(t, *route.OtelDestinationID, metadata["otel_destination_id"])
		})
	}
}

func TestTestRouteReportsUnreachableDestination(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	endpoint := "http://" + listener.Addr().String()
	require.NoError(t, listener.Close())
	route := createTestableRoute(t, ctx, ti, endpoint, dataexports.DataSourceMCPConnectionLogs)

	result, err := testRoute(ctx, ti, route.ID)
	require.NoError(t, err)
	require.Equal(t, "unreachable", result.Outcome)
	require.Nil(t, result.StatusCode)
	require.Equal(t, "Speakeasy could not reach the destination. Check the URL, and that the address accepts connections from the internet.", result.Message)

	entry, err := audittest.LatestAuditLogByAction(ctx, ti.conn, audit.ActionDataExportRouteTest)
	require.NoError(t, err)
	metadata, err := audittest.DecodeAuditData(entry.Metadata)
	require.NoError(t, err)
	require.Equal(t, "unreachable", metadata["outcome"])
	require.NotContains(t, metadata, "status_code")
}

func TestTestRouteSendsALogForEveryLogsOnlySource(t *testing.T) {
	t.Parallel()

	for _, source := range []string{dataexports.DataSourceToolCallLogs, dataexports.DataSourceRiskFindings, dataexports.DataSourceMCPConnectionLogs} {
		t.Run(source, func(t *testing.T) {
			t.Parallel()

			ctx, ti := newTestService(t)
			server, received := testRouteDestinationByPath(t, map[string]int{"/v1/logs": http.StatusOK})
			route := createTestableRoute(t, ctx, ti, server.URL, source)

			result, err := testRoute(ctx, ti, route.ID)
			require.NoError(t, err)
			require.Equal(t, "accepted", result.Outcome)
			require.Equal(t, "The destination accepted the test event.", result.Message)
			require.Len(t, result.Signals, 1)
			require.Equal(t, "logs", result.Signals[0].Signal)
			require.EqualValues(t, 1, received.Load())
		})
	}
}

func TestTestRouteSendsEverySignalForProductTelemetry(t *testing.T) {
	t.Parallel()

	t.Run("all accepted", func(t *testing.T) {
		t.Parallel()

		ctx, ti := newTestService(t)
		server, received := testRouteDestination(t, http.StatusOK)
		route := createTestableRoute(t, ctx, ti, server.URL, dataexports.DataSourceProductTelemetry)

		result, err := testRoute(ctx, ti, route.ID)
		require.NoError(t, err)
		require.Equal(t, "accepted", result.Outcome)
		require.Equal(t, http.StatusOK, *result.StatusCode)
		require.Equal(t, "The destination accepted the test events for logs, traces and metrics.", result.Message)
		require.Len(t, result.Signals, 3)
		for i, signal := range []string{"logs", "traces", "metrics"} {
			require.Equal(t, signal, result.Signals[i].Signal)
			require.Equal(t, "accepted", result.Signals[i].Outcome)
		}
		require.EqualValues(t, 3, received.Load())
	})

	t.Run("traces not found", func(t *testing.T) {
		t.Parallel()

		ctx, ti := newTestService(t)
		server, received := testRouteDestinationByPath(t, map[string]int{"/v1/logs": http.StatusOK, "/v1/traces": http.StatusNotFound, "/v1/metrics": http.StatusAccepted})
		route := createTestableRoute(t, ctx, ti, server.URL, dataexports.DataSourceProductTelemetry)

		result, err := testRoute(ctx, ti, route.ID)
		require.NoError(t, err)
		require.Equal(t, "not_found", result.Outcome)
		require.Equal(t, http.StatusNotFound, *result.StatusCode)
		require.Equal(t, "The destination did not accept every test event. See each signal for details.", result.Message)
		require.Equal(t, "accepted", result.Signals[0].Outcome)
		require.Equal(t, "not_found", result.Signals[1].Outcome)
		require.Equal(t, "No traces endpoint at this address. Check the URL; Speakeasy adds /v1/traces.", *result.Signals[1].Message)
		require.Equal(t, "accepted", result.Signals[2].Outcome)
		require.Equal(t, http.StatusAccepted, *result.Signals[2].StatusCode)
		require.EqualValues(t, 3, received.Load())

		entry, err := audittest.LatestAuditLogByAction(ctx, ti.conn, audit.ActionDataExportRouteTest)
		require.NoError(t, err)
		metadata, err := audittest.DecodeAuditData(entry.Metadata)
		require.NoError(t, err)
		require.Equal(t, "not_found", metadata["outcome"])
		require.InDelta(t, float64(http.StatusNotFound), metadata["status_code"], 0)
		signals, ok := metadata["signals"].([]any)
		require.True(t, ok)
		require.Len(t, signals, 3)
		traces, ok := signals[1].(map[string]any)
		require.True(t, ok)
		require.Equal(t, "traces", traces["signal"])
		require.Equal(t, "not_found", traces["outcome"])
	})
}

func TestTestRouteRequiresADestination(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	route, err := createConnectionLogsRoute(ctx, ti)
	require.NoError(t, err)

	_, err = testRoute(ctx, ti, route.ID)
	requireOopsCode(t, err, oops.CodeInvalid)
}

func TestTestRouteRequiresOrgAdmin(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	server, received := testRouteDestination(t, http.StatusOK)
	route := createTestableRoute(t, ctx, ti, server.URL, dataexports.DataSourceMCPConnectionLogs)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	orgRead := authztest.WithExactGrants(t, ctx, authz.NewGrant(authz.ScopeOrgRead, authCtx.ActiveOrganizationID))
	_, err := testRoute(orgRead, ti, route.ID)
	requireOopsCode(t, err, oops.CodeForbidden)
	projectWrite := authztest.WithExactGrants(t, ctx, authz.NewGrant(authz.ScopeProjectWrite, authCtx.ProjectID.String()))
	_, err = testRoute(projectWrite, ti, route.ID)
	requireOopsCode(t, err, oops.CodeForbidden)
	require.Zero(t, received.Load())

	orgAdmin := authztest.WithExactGrants(t, ctx, authz.NewGrant(authz.ScopeOrgAdmin, authCtx.ActiveOrganizationID))
	result, err := testRoute(orgAdmin, ti, route.ID)
	require.NoError(t, err)
	require.Equal(t, "accepted", result.Outcome)
}

func TestTestRouteIsTenantScoped(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	server, received := testRouteDestination(t, http.StatusOK)
	route := createTestableRoute(t, ctx, ti, server.URL, dataexports.DataSourceMCPConnectionLogs)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	otherSlug := "data-exports-other-" + uuid.NewString()[:8]
	otherProject, err := projectsrepo.New(ti.conn).CreateProject(ctx, projectsrepo.CreateProjectParams{
		Name:           otherSlug,
		Slug:           otherSlug,
		OrganizationID: authCtx.ActiveOrganizationID,
	})
	require.NoError(t, err)
	otherProjectAuth := *authCtx
	otherProjectAuth.ProjectID = &otherProject.ID
	otherProjectAuth.ProjectSlug = &otherSlug
	_, err = testRoute(contextvalues.SetAuthContext(ctx, &otherProjectAuth), ti, route.ID)
	requireOopsCode(t, err, oops.CodeNotFound)

	otherOrgID := "org-" + uuid.NewString()
	otherOrgAuth := *authCtx
	otherOrgAuth.ActiveOrganizationID = otherOrgID
	otherOrgCtx := authztest.WithExactGrants(t, contextvalues.SetAuthContext(ctx, &otherOrgAuth), authz.NewGrant(authz.ScopeOrgAdmin, otherOrgID))
	_, err = testRoute(otherOrgCtx, ti, route.ID)
	requireOopsCode(t, err, oops.CodeNotFound)

	_, err = testRoute(ctx, ti, uuid.NewString())
	requireOopsCode(t, err, oops.CodeNotFound)
	require.Zero(t, received.Load())
}

func TestTestRouteIsRateLimitedPerRoute(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	server, received := testRouteDestination(t, http.StatusOK)
	route := createTestableRoute(t, ctx, ti, server.URL, dataexports.DataSourceMCPConnectionLogs)

	for range dataexports.RouteTestsPerMinute {
		_, err := testRoute(ctx, ti, route.ID)
		require.NoError(t, err)
	}
	_, err := testRoute(ctx, ti, route.ID)
	requireOopsCode(t, err, oops.CodeRateLimitExceeded)
	require.EqualValues(t, dataexports.RouteTestsPerMinute, received.Load())
}

// fixedTestSender answers every signal with status, or with its entry in
// results, counting the calls. Before answering it runs onSend, if set.
type fixedTestSender struct {
	status  int
	results map[dataexports.Signal]dataexports.SignalTestResult
	sent    atomic.Int64
	onSend  func()
}

func (s *fixedTestSender) SendTest(_ context.Context, sourceName string, _ dataexports.TestDestination) ([]dataexports.SignalTestResult, error) {
	s.sent.Add(1)
	if s.onSend != nil {
		s.onSend()
	}
	source, ok := dataexports.LookupSource(sourceName)
	if !ok {
		return nil, dataexports.ErrRouteTestUnsupported
	}
	results := make([]dataexports.SignalTestResult, len(source.Signals))
	for i, signal := range source.Signals {
		result, ok := s.results[signal]
		if !ok {
			result = dataexports.SignalTestResult{Signal: signal, StatusCode: s.status, Unreachable: false}
		}
		results[i] = result
	}
	return results, nil
}

func routeTestParams(t *testing.T, ctx context.Context, routeID string) dataexports.SendRouteTestParams {
	t.Helper()
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	return dataexports.SendRouteTestParams{
		OrganizationID:   authCtx.ActiveOrganizationID,
		ProjectID:        *authCtx.ProjectID,
		RouteID:          uuid.MustParse(routeID),
		Expect:           nil,
		Actor:            urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID),
		ActorDisplayName: nil,
	}
}

func routeTestLimits(t *testing.T, perRoute, perOrganization int) dataexports.RouteTestLimits {
	t.Helper()
	redisClient, err := infra.NewRedisClient(t, 0)
	require.NoError(t, err)
	store := ratelimit.NewRedisStore(redisClient)
	return dataexports.RouteTestLimits{
		Route:        ratelimit.New(store, dataexports.RouteTestLimitName, ratelimit.PerMinute(perRoute)),
		Organization: ratelimit.New(store, dataexports.RouteTestOrganizationLimitName+":"+uuid.NewString(), ratelimit.PerMinute(perOrganization)),
	}
}

func TestTestRouteIsRateLimitedPerOrganization(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	first := createTestableRoute(t, ctx, ti, "https://collector.example.com", dataexports.DataSourceMCPConnectionLogs)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	otherSlug := "data-exports-other-" + uuid.NewString()[:8]
	otherProject, err := projectsrepo.New(ti.conn).CreateProject(ctx, projectsrepo.CreateProjectParams{
		Name:           otherSlug,
		Slug:           otherSlug,
		OrganizationID: authCtx.ActiveOrganizationID,
	})
	require.NoError(t, err)
	otherAuth := *authCtx
	otherAuth.ProjectID = &otherProject.ID
	otherAuth.ProjectSlug = &otherSlug
	otherCtx := contextvalues.SetAuthContext(ctx, &otherAuth)
	second := createTestableRoute(t, otherCtx, ti, "https://collector.example.com", dataexports.DataSourceMCPConnectionLogs)
	sender := &fixedTestSender{status: http.StatusOK}
	core := dataexports.NewRouteTestCore(testenv.NewLogger(t), ti.conn, audit.NewLogger(), ti.enc, sender, routeTestLimits(t, dataexports.RouteTestsPerMinute, 2))
	require.NotNil(t, core)

	_, err = core.SendTest(ctx, routeTestParams(t, ctx, first.ID))
	require.NoError(t, err)
	_, err = core.SendTest(otherCtx, routeTestParams(t, otherCtx, second.ID))
	require.NoError(t, err)
	// The second route, in another project, still has its own allowance, but the organization does not.
	_, err = core.SendTest(otherCtx, routeTestParams(t, otherCtx, second.ID))
	require.ErrorIs(t, err, dataexports.ErrRouteTestRateLimited)
	require.EqualValues(t, 2, sender.sent.Load())
}

func TestTestRouteFailsClosedOnALimiterError(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	server, received := testRouteDestination(t, http.StatusOK)
	route := createTestableRoute(t, ctx, ti, server.URL, dataexports.DataSourceMCPConnectionLogs)
	sender := &fixedTestSender{status: http.StatusOK}
	limits := routeTestLimits(t, dataexports.RouteTestsPerMinute, dataexports.RouteTestsPerOrganizationPerMinute)
	// An unusable rate makes Allow return an error, as a Redis outage does.
	limits.Organization = ratelimit.New(ratelimit.NewRedisStore(nil), dataexports.RouteTestOrganizationLimitName, ratelimit.PerMinute(0))
	core := dataexports.NewRouteTestCore(testenv.NewLogger(t), ti.conn, audit.NewLogger(), ti.enc, sender, limits)
	require.NotNil(t, core)

	_, err := core.SendTest(ctx, routeTestParams(t, ctx, route.ID))
	require.ErrorIs(t, err, dataexports.ErrRouteTestUnavailable)
	require.Zero(t, sender.sent.Load())
	require.Zero(t, received.Load())
	count, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionDataExportRouteTest)
	require.NoError(t, err)
	require.Zero(t, count)
}

func TestTestRouteReturnsTheOutcomeWhenTheAuditFails(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	route := createTestableRoute(t, ctx, ti, "https://collector.example.com", dataexports.DataSourceMCPConnectionLogs)
	pool, err := pgxpool.New(ctx, ti.conn.Config().ConnString())
	require.NoError(t, err)
	// Closing the pool mid-send makes the audit insert after the send fail.
	sender := &fixedTestSender{status: http.StatusAccepted, onSend: pool.Close}
	core := dataexports.NewRouteTestCore(testenv.NewLogger(t), pool, audit.NewLogger(), ti.enc, sender, routeTestLimits(t, dataexports.RouteTestsPerMinute, dataexports.RouteTestsPerOrganizationPerMinute))
	require.NotNil(t, core)

	result, err := core.SendTest(ctx, routeTestParams(t, ctx, route.ID))
	require.NoError(t, err)
	require.Equal(t, dataexports.TestOutcomeAccepted, result.Outcome)
	require.Equal(t, http.StatusAccepted, result.StatusCode)
	count, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionDataExportRouteTest)
	require.NoError(t, err)
	require.Zero(t, count)
}

func TestTestRouteTakesTheWorstSignal(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name       string
		results    map[dataexports.Signal]dataexports.SignalTestResult
		outcome    dataexports.TestOutcome
		statusCode int
	}{
		{name: "unreachable beats refused", results: map[dataexports.Signal]dataexports.SignalTestResult{
			dataexports.SignalLogs:    {Signal: dataexports.SignalLogs, StatusCode: http.StatusOK, Unreachable: false},
			dataexports.SignalTraces:  {Signal: dataexports.SignalTraces, StatusCode: http.StatusForbidden, Unreachable: false},
			dataexports.SignalMetrics: {Signal: dataexports.SignalMetrics, StatusCode: 0, Unreachable: true},
		}, outcome: dataexports.TestOutcomeUnreachable, statusCode: 0},
		{name: "refused beats not found", results: map[dataexports.Signal]dataexports.SignalTestResult{
			dataexports.SignalLogs:    {Signal: dataexports.SignalLogs, StatusCode: http.StatusNotFound, Unreachable: false},
			dataexports.SignalTraces:  {Signal: dataexports.SignalTraces, StatusCode: http.StatusOK, Unreachable: false},
			dataexports.SignalMetrics: {Signal: dataexports.SignalMetrics, StatusCode: http.StatusUnauthorized, Unreachable: false},
		}, outcome: dataexports.TestOutcomeRefused, statusCode: http.StatusUnauthorized},
		{name: "not found beats error", results: map[dataexports.Signal]dataexports.SignalTestResult{
			dataexports.SignalLogs:    {Signal: dataexports.SignalLogs, StatusCode: http.StatusBadGateway, Unreachable: false},
			dataexports.SignalTraces:  {Signal: dataexports.SignalTraces, StatusCode: http.StatusNotFound, Unreachable: false},
			dataexports.SignalMetrics: {Signal: dataexports.SignalMetrics, StatusCode: http.StatusOK, Unreachable: false},
		}, outcome: dataexports.TestOutcomeNotFound, statusCode: http.StatusNotFound},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			ctx, ti := newTestService(t)
			route := createTestableRoute(t, ctx, ti, "https://collector.example.com", dataexports.DataSourceProductTelemetry)
			sender := &fixedTestSender{status: http.StatusOK, results: tc.results}
			core := dataexports.NewRouteTestCore(testenv.NewLogger(t), ti.conn, audit.NewLogger(), ti.enc, sender, routeTestLimits(t, dataexports.RouteTestsPerMinute, dataexports.RouteTestsPerOrganizationPerMinute))
			require.NotNil(t, core)

			result, err := core.SendTest(ctx, routeTestParams(t, ctx, route.ID))
			require.NoError(t, err)
			require.Equal(t, tc.outcome, result.Outcome)
			require.Equal(t, tc.statusCode, result.StatusCode)
			require.Equal(t, "The destination did not accept every test event. See each signal for details.", result.Message)
			require.Len(t, result.Signals, 3)
			require.EqualValues(t, 1, sender.sent.Load())
		})
	}
}
