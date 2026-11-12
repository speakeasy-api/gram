package platformmcp

import (
	"context"
	"encoding/json"
	"net/http"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/dataexports"
	"github.com/speakeasy-api/gram/server/internal/ratelimit"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/workos"
)

type recordingTestEventSender struct {
	mu     sync.Mutex
	status int
	sent   []dataexports.TestDestination
}

func (s *recordingTestEventSender) SendTest(_ context.Context, sourceName string, destination dataexports.TestDestination) ([]dataexports.SignalTestResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sent = append(s.sent, destination)
	source, ok := dataexports.LookupSource(sourceName)
	if !ok {
		return nil, dataexports.ErrRouteTestUnsupported
	}
	results := make([]dataexports.SignalTestResult, len(source.Signals))
	for i, signal := range source.Signals {
		results[i] = dataexports.SignalTestResult{Signal: signal, StatusCode: s.status, Unreachable: false}
	}
	return results, nil
}

func (s *recordingTestEventSender) count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.sent)
}

type dataExportTestEventFixture struct {
	dataExportToggleFixture
	sender *recordingTestEventSender
	tests  *DataExportTestEventService
	core   *dataexports.RouteTestCore
}

func newDataExportTestEventFixture(t *testing.T, ctx context.Context, database string, status int) dataExportTestEventFixture {
	t.Helper()
	f := newDataExportToggleFixture(t, ctx, database)
	redisClient, err := platformMCPInfra.NewRedisClient(t, 0)
	require.NoError(t, err)
	sender := &recordingTestEventSender{status: status}
	store := ratelimit.NewRedisStore(redisClient)
	core := dataexports.NewRouteTestCore(testenv.NewLogger(t), f.conn, audit.NewLogger(), testenv.NewEncryptionClient(t), sender, dataexports.RouteTestLimits{
		Route:        ratelimit.New(store, dataexports.RouteTestLimitName, ratelimit.PerMinute(dataexports.RouteTestsPerMinute)),
		Organization: ratelimit.New(store, dataexports.RouteTestOrganizationLimitName+":"+uuid.NewString(), ratelimit.PerMinute(dataexports.RouteTestsPerOrganizationPerMinute)),
	})
	require.NotNil(t, core)
	tests := NewDataExportTestEventService(testenv.NewLogger(t), f.conn, core, stubAuthorizer{err: nil})
	require.NotNil(t, tests)
	return dataExportTestEventFixture{dataExportToggleFixture: f, sender: sender, tests: tests, core: core}
}

func (f dataExportTestEventFixture) testInput(routeID uuid.UUID, dataSource string, destinationID uuid.UUID) SendDataExportTestEventInput {
	return SendDataExportTestEventInput{ProjectID: f.project.ID.String(), RouteID: routeID.String(), ExpectedDataSource: dataSource, ExpectedDestinationID: destinationID.String(), Confirmed: true}
}

func requireDataExportTestCode(t *testing.T, err error, code string) string {
	t.Helper()
	require.Error(t, err)
	result, ok := dataExportTestEventToolResult(err)
	require.True(t, ok, "error must map to a readable refusal: %v", err)
	require.True(t, result.IsError)
	text, ok := result.Content[0].(*mcp.TextContent)
	require.True(t, ok)
	var refusal struct {
		Code    string `json:"code"`
		Feature string `json:"feature"`
	}
	require.NoError(t, json.Unmarshal([]byte(text.Text), &refusal))
	require.Equal(t, code, refusal.Code, text.Text)
	require.Equal(t, dataExportTestFeature, refusal.Feature)
	return text.Text
}

func TestSendDataExportTestEventReportsTheAnswerAndAudits(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	f := newDataExportTestEventFixture(t, ctx, "platform_mcp_data_export_test_event", http.StatusForbidden)
	route := f.createRoute(t, ctx, dataexports.DataSourceMCPConnectionLogs, false, uuid.NullUUID{UUID: f.destination, Valid: true})

	output, err := f.tests.Send(ctx, f.principal, f.testInput(route.ID, dataexports.DataSourceMCPConnectionLogs, f.destination))
	require.NoError(t, err)
	require.Equal(t, "refused", output.Outcome)
	require.Equal(t, http.StatusForbidden, output.StatusCode)
	require.Contains(t, output.Message, "Check the API key")
	require.Equal(t, route.ID.String(), output.RouteID)
	require.Equal(t, dataexports.DataSourceMCPConnectionLogs, output.DataSource)
	require.Equal(t, f.destination.String(), output.DestinationID)
	require.Len(t, output.Signals, 1)
	require.Equal(t, "logs", output.Signals[0].Signal)
	require.Equal(t, "refused", output.Signals[0].Outcome)
	require.Equal(t, 1, f.sender.count())
	require.Equal(t, "https://otel.example.test/v1", f.sender.sent[0].EndpointURL)
	require.Equal(t, f.project.ID, f.sender.sent[0].ProjectID)
	require.EqualValues(t, 1, auditCount(t, ctx, f.conn, audit.ActionDataExportRouteTest))
	// Testing never changes the route.
	require.False(t, f.route(t, ctx, route.ID).Enabled)
}

func TestSendDataExportTestEventRefusesWithoutConfirmationOrAMatchingDestination(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	f := newDataExportTestEventFixture(t, ctx, "platform_mcp_data_export_test_event_confirm", http.StatusOK)
	route := f.createRoute(t, ctx, dataexports.DataSourceMCPConnectionLogs, true, uuid.NullUUID{UUID: f.destination, Valid: true})

	unconfirmed := f.testInput(route.ID, dataexports.DataSourceMCPConnectionLogs, f.destination)
	unconfirmed.Confirmed = false
	_, err := f.tests.Send(ctx, f.principal, unconfirmed)
	requireDataExportTestCode(t, err, "invalid_request")

	_, err = f.tests.Send(ctx, f.principal, f.testInput(route.ID, dataexports.DataSourceMCPConnectionLogs, uuid.New()))
	requireDataExportTestCode(t, err, "route_changed")

	_, err = f.tests.Send(ctx, f.principal, f.testInput(route.ID, dataexports.DataSourceRiskFindings, f.destination))
	requireDataExportTestCode(t, err, "route_changed")

	_, err = f.tests.Send(ctx, f.principal, f.testInput(route.ID, "unknown_source", f.destination))
	requireDataExportTestCode(t, err, "invalid_request")

	require.Zero(t, f.sender.count())
	require.Zero(t, auditCount(t, ctx, f.conn, audit.ActionDataExportRouteTest))
}

func TestSendDataExportTestEventReportsEverySignalForProductTelemetry(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	f := newDataExportTestEventFixture(t, ctx, "platform_mcp_data_export_test_event_signals", http.StatusOK)
	route := f.createRoute(t, ctx, dataexports.DataSourceProductTelemetry, true, uuid.NullUUID{UUID: f.destination, Valid: true})

	output, err := f.tests.Send(ctx, f.principal, f.testInput(route.ID, dataexports.DataSourceProductTelemetry, f.destination))
	require.NoError(t, err)
	require.Equal(t, "accepted", output.Outcome)
	require.Equal(t, "The destination accepted the test events for logs, traces and metrics.", output.Message)
	require.Equal(t, dataexports.DataSourceProductTelemetry, output.DataSource)
	require.Len(t, output.Signals, 3)
	for i, signal := range []string{"logs", "traces", "metrics"} {
		require.Equal(t, signal, output.Signals[i].Signal)
		require.Equal(t, "accepted", output.Signals[i].Outcome)
		require.Equal(t, http.StatusOK, output.Signals[i].StatusCode)
	}
	// One click spends one allowance, whatever the signal count.
	require.Equal(t, 1, f.sender.count())
	require.EqualValues(t, 1, auditCount(t, ctx, f.conn, audit.ActionDataExportRouteTest))
}

func TestSendDataExportTestEventRefusesANonAdminIdenticallyForRealAndInventedRoutes(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	f := newDataExportTestEventFixture(t, ctx, "platform_mcp_data_export_test_event_member", http.StatusOK)
	route := f.createRoute(t, ctx, dataexports.DataSourceMCPConnectionLogs, true, uuid.NullUUID{UUID: f.destination, Valid: true})
	require.NoError(t, authz.SeedSystemRoleGrants(ctx, f.conn, f.principal.OrganizationID))
	memberID := "member_" + uuid.NewString()
	seedPlatformMCPAuthorizationMember(t, ctx, f.conn, f.principal.OrganizationID, memberID, authz.SystemRoleMember)
	engine := authz.NewEngine(testenv.NewLogger(t), f.conn, func(context.Context, string) (bool, error) { return false, nil }, workos.NewStubClient())
	service := NewDataExportTestEventService(testenv.NewLogger(t), f.conn, f.core, NewLiveOrgAdminAuthorizer(f.conn, engine))
	member := Principal{UserID: memberID, OrganizationID: f.principal.OrganizationID, ConnectionID: uuid.NewString(), Generation: uuid.NewString(), ClientID: "client-test", Surface: SurfacePlatformMCP}

	_, realErr := service.Send(ctx, member, f.testInput(route.ID, dataexports.DataSourceMCPConnectionLogs, f.destination))
	_, inventedErr := service.Send(ctx, member, f.testInput(uuid.New(), dataexports.DataSourceMCPConnectionLogs, f.destination))
	require.Equal(t, requireDataExportToggleCode(t, realErr, "permission_denied"), requireDataExportToggleCode(t, inventedErr, "permission_denied"))
	require.Zero(t, f.sender.count())
}

func TestSendDataExportTestEventCannotReachAnotherOrganizationsRoute(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	f := newDataExportTestEventFixture(t, ctx, "platform_mcp_data_export_test_event_tenancy", http.StatusOK)
	otherPrincipal, otherProject := seedRegistrationLifecycle(t, ctx, f.conn)
	other := dataExportToggleFixture{conn: f.conn, service: f.service, principal: otherPrincipal, project: otherProject, destination: uuid.Nil}
	foreign := other.createRoute(t, ctx, dataexports.DataSourceMCPConnectionLogs, false, uuid.NullUUID{})

	_, err := f.tests.Send(ctx, f.principal, SendDataExportTestEventInput{ProjectID: otherProject.ID.String(), RouteID: foreign.ID.String(), ExpectedDataSource: dataexports.DataSourceMCPConnectionLogs, ExpectedDestinationID: f.destination.String(), Confirmed: true})
	notFound := requireDataExportTestCode(t, err, "not_found")
	_, err = f.tests.Send(ctx, f.principal, f.testInput(foreign.ID, dataexports.DataSourceMCPConnectionLogs, f.destination))
	require.Equal(t, notFound, requireDataExportTestCode(t, err, "not_found"))
	require.Zero(t, f.sender.count())
}

func TestSendDataExportTestEventIsRateLimitedPerRoute(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	f := newDataExportTestEventFixture(t, ctx, "platform_mcp_data_export_test_event_limit", http.StatusAccepted)
	route := f.createRoute(t, ctx, dataexports.DataSourceMCPConnectionLogs, true, uuid.NullUUID{UUID: f.destination, Valid: true})

	for range dataexports.RouteTestsPerMinute {
		output, err := f.tests.Send(ctx, f.principal, f.testInput(route.ID, dataexports.DataSourceMCPConnectionLogs, f.destination))
		require.NoError(t, err)
		require.Equal(t, "accepted", output.Outcome)
	}
	_, err := f.tests.Send(ctx, f.principal, f.testInput(route.ID, dataexports.DataSourceMCPConnectionLogs, f.destination))
	text := requireDataExportTestCode(t, err, "rate_limited")
	require.Contains(t, text, "Too many test events were sent just now. Try again in a minute.")
	require.Equal(t, dataexports.RouteTestsPerMinute, f.sender.count())
}

func TestSendDataExportTestEventRefusesWhenTheLimitCannotBeChecked(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	f := newDataExportTestEventFixture(t, ctx, "platform_mcp_data_export_test_event_limit_down", http.StatusAccepted)
	route := f.createRoute(t, ctx, dataexports.DataSourceMCPConnectionLogs, true, uuid.NullUUID{UUID: f.destination, Valid: true})
	// An unusable rate makes Allow return an error, as a Redis outage does.
	broken := ratelimit.New(ratelimit.NewRedisStore(nil), dataexports.RouteTestLimitName, ratelimit.PerMinute(0))
	core := dataexports.NewRouteTestCore(testenv.NewLogger(t), f.conn, audit.NewLogger(), testenv.NewEncryptionClient(t), f.sender, dataexports.RouteTestLimits{Route: broken, Organization: broken})
	require.NotNil(t, core)
	service := NewDataExportTestEventService(testenv.NewLogger(t), f.conn, core, stubAuthorizer{err: nil})

	_, err := service.Send(ctx, f.principal, f.testInput(route.ID, dataexports.DataSourceMCPConnectionLogs, f.destination))
	text := requireDataExportTestCode(t, err, unavailableCode)
	require.Contains(t, text, "Speakeasy can't send test events right now. Try again shortly.")
	require.Zero(t, f.sender.count())
	require.Zero(t, auditCount(t, ctx, f.conn, audit.ActionDataExportRouteTest))
}

func TestDataExportTestEventUnavailableManifestMatchesLive(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	f := newDataExportTestEventFixture(t, ctx, "platform_mcp_data_export_test_event_manifest", http.StatusOK)
	describe := func(service *DataExportTestEventService) Descriptor {
		registrar := newRegistrar(mcp.NewServer(&mcp.Implementation{Name: "data-export-test-event", Version: "0.0.1"}, nil))
		registerDataExportTestEventTool(registrar, service)
		descriptors := registrar.Descriptors()
		require.Len(t, descriptors, 1)
		return descriptors[0]
	}
	live := describe(f.tests)
	unavailable := describe(nil)
	require.Equal(t, sendDataExportTestEventToolName, unavailable.Name)
	require.Equal(t, live.Title, unavailable.Title)
	require.Equal(t, live.Description, unavailable.Description)
	require.Equal(t, live.Meta, unavailable.Meta)
	require.Equal(t, live.Annotations, unavailable.Annotations)
	require.Equal(t, live.InputSchema, unavailable.InputSchema)
	require.Equal(t, externalOnly, unavailable.Meta.Audiences)
	require.Equal(t, ExternalAuthorizationOrgAdmin, unavailable.Meta.Authorization)
	require.Equal(t, ProjectScopeExplicit, unavailable.Meta.ProjectScope)
	require.Contains(t, unavailable.Description, "confirmed: true")

	var schema struct {
		Required []string `json:"required"`
	}
	require.NoError(t, json.Unmarshal(unavailable.InputSchema, &schema))
	require.Subset(t, schema.Required, []string{"project_id", "route_id", "expected_data_source", "expected_destination_id", "confirmed"})

	refusal := invokeUnavailable(t, unavailable, map[string]any{"project_id": f.project.ID.String(), "route_id": uuid.NewString(), "expected_data_source": dataexports.DataSourceMCPConnectionLogs, "expected_destination_id": uuid.NewString(), "confirmed": true})
	require.Contains(t, refusal, `"feature":"data_export_test_event"`)
}
