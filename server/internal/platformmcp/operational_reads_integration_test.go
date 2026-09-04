package platformmcp

import (
	"context"
	"encoding/json"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	dataexportsrepo "github.com/speakeasy-api/gram/server/internal/dataexports/repo"
	telemetryrepo "github.com/speakeasy-api/gram/server/internal/telemetry/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

func mustParseURL(t *testing.T, raw string) *url.URL {
	t.Helper()
	parsed, err := url.Parse(raw)
	require.NoError(t, err)
	return parsed
}

func TestListDataExportsReturnsSafeStructuredConfiguration(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	conn, err := platformMCPInfra.CloneTestDatabase(t, "platform_mcp_data_exports")
	require.NoError(t, err)
	principal, project := seedRegistrationLifecycle(t, ctx, conn)
	encryptionClient := testenv.NewEncryptionClient(t)

	const secret = "Bearer test-secret-that-must-not-leave-storage"
	ciphertext, err := encryptionClient.Encrypt([]byte(`{"Authorization":"` + secret + `","X-Empty":""}`))
	require.NoError(t, err)
	queries := dataexportsrepo.New(conn)
	destination, err := queries.CreateOtelDestination(ctx, dataexportsrepo.CreateOtelDestinationParams{
		OrganizationID:   principal.OrganizationID,
		ProjectID:        project.ID,
		Name:             "Primary collector",
		EndpointUrl:      "https://otel.example.test/v1",
		HeadersEncrypted: pgtype.Text{String: ciphertext, Valid: true},
		SensitiveData:    pgtype.Text{String: "exclude", Valid: true},
	})
	require.NoError(t, err)
	route, err := queries.CreateDataExportRoute(ctx, dataexportsrepo.CreateDataExportRouteParams{
		OrganizationID:    principal.OrganizationID,
		ProjectID:         project.ID,
		DataSource:        "product_telemetry",
		Enabled:           true,
		OtelDestinationID: uuid.NullUUID{UUID: destination.ID, Valid: true},
	})
	require.NoError(t, err)

	reader := NewPostgresReader(testenv.NewLogger(t), conn).
		WithDataExports(encryptionClient, mustParseURL(t, "https://app.getgram.test"))
	output, err := reader.ListDataExports(ctx, principal, ListDataExportsInput{ProjectID: project.ID.String()})
	require.NoError(t, err)
	require.False(t, output.Truncated)
	require.Len(t, output.Destinations, 1)
	require.Len(t, output.Routes, 1)
	require.Equal(t, destination.ID.String(), output.Destinations[0].ID)
	require.Equal(t, project.Slug, output.Destinations[0].ProjectSlug)
	require.Equal(t, "https://otel.example.test/v1", output.Destinations[0].EndpointURL)
	require.Equal(t, []DataExportHeader{{Name: "Authorization", HasValue: true}, {Name: "X-Empty", HasValue: false}}, output.Destinations[0].Headers)
	require.Equal(t, route.ID.String(), output.Routes[0].ID)
	require.Equal(t, destination.ID.String(), output.Routes[0].DestinationID)
	require.True(t, output.Routes[0].Enabled)
	require.True(t, strings.HasSuffix(output.ManagementURL, "/data/exports"))

	encoded, err := json.Marshal(output)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), secret)
}

func TestCreateDataExportRequiresExplicitConfirmation(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	conn, err := platformMCPInfra.CloneTestDatabase(t, "platform_mcp_create_export_confirmation")
	require.NoError(t, err)
	principal, project := seedRegistrationLifecycle(t, ctx, conn)
	reader := NewPostgresReader(testenv.NewLogger(t), conn).
		WithDataExportMutations(audit.NewLogger(), mustParseURL(t, "https://app.getgram.test"))

	_, err = reader.CreateDataExport(ctx, principal, CreateDataExportInput{
		ProjectID:     project.ID.String(),
		Name:          "Confirmed collector",
		EndpointURL:   "https://otel.example.test/v1",
		DataSource:    "product_telemetry",
		SensitiveData: "exclude",
		Enabled:       nil,
		Confirmed:     false,
	})
	require.ErrorIs(t, err, ErrDataExportConfirmationRequired)

	queries := dataexportsrepo.New(conn)
	destinations, err := queries.ListOtelDestinations(ctx, dataexportsrepo.ListOtelDestinationsParams{
		OrganizationID: principal.OrganizationID,
		ProjectID:      project.ID,
	})
	require.NoError(t, err)
	routes, err := queries.ListDataExportRoutes(ctx, dataexportsrepo.ListDataExportRoutesParams{
		OrganizationID: principal.OrganizationID,
		ProjectID:      project.ID,
	})
	require.NoError(t, err)
	require.Empty(t, destinations)
	require.Empty(t, routes)
}

func TestCreateDataExportCommitsDestinationRouteAndAuditAtomically(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	conn, err := platformMCPInfra.CloneTestDatabase(t, "platform_mcp_create_export")
	require.NoError(t, err)
	principal, project := seedRegistrationLifecycle(t, ctx, conn)
	reader := NewPostgresReader(testenv.NewLogger(t), conn).
		WithDataExportMutations(audit.NewLogger(), mustParseURL(t, "https://app.getgram.test"))

	destinationAuditBefore, err := audittest.AuditLogCountByAction(ctx, conn, audit.ActionOtelDestinationCreate)
	require.NoError(t, err)
	routeAuditBefore, err := audittest.AuditLogCountByAction(ctx, conn, audit.ActionDataExportRouteCreate)
	require.NoError(t, err)

	enabled := false
	output, err := reader.CreateDataExport(ctx, principal, CreateDataExportInput{
		ProjectSlug:   project.Slug,
		Name:          "  Platform MCP collector  ",
		EndpointURL:   "https://otel.example.test/v1",
		DataSource:    "risk_findings",
		SensitiveData: "include",
		Enabled:       &enabled,
		Confirmed:     true,
	})
	require.NoError(t, err)
	require.Equal(t, "Platform MCP collector", output.Destination.Name)
	require.Equal(t, "https://otel.example.test/v1", output.Destination.EndpointURL)
	require.Equal(t, "include", output.Destination.SensitiveData)
	require.Empty(t, output.Destination.Headers)
	require.Equal(t, output.Destination.ID, output.Route.DestinationID)
	require.Equal(t, "risk_findings", output.Route.DataSource)
	require.False(t, output.Route.Enabled)
	require.True(t, strings.HasSuffix(output.ManagementURL, "/data/exports"))

	queries := dataexportsrepo.New(conn)
	destinations, err := queries.ListOtelDestinations(ctx, dataexportsrepo.ListOtelDestinationsParams{
		OrganizationID: principal.OrganizationID,
		ProjectID:      project.ID,
	})
	require.NoError(t, err)
	require.Len(t, destinations, 1)
	require.False(t, destinations[0].HeadersEncrypted.Valid)
	routes, err := queries.ListDataExportRoutes(ctx, dataexportsrepo.ListDataExportRoutesParams{
		OrganizationID: principal.OrganizationID,
		ProjectID:      project.ID,
	})
	require.NoError(t, err)
	require.Len(t, routes, 1)

	destinationAuditAfter, err := audittest.AuditLogCountByAction(ctx, conn, audit.ActionOtelDestinationCreate)
	require.NoError(t, err)
	require.Equal(t, destinationAuditBefore+1, destinationAuditAfter)
	routeAuditAfter, err := audittest.AuditLogCountByAction(ctx, conn, audit.ActionDataExportRouteCreate)
	require.NoError(t, err)
	require.Equal(t, routeAuditBefore+1, routeAuditAfter)
	destinationAudit, err := audittest.LatestAuditLogByAction(ctx, conn, audit.ActionOtelDestinationCreate)
	require.NoError(t, err)
	require.Equal(t, principal.UserID, destinationAudit.ActorID)

	_, err = reader.CreateDataExport(ctx, principal, CreateDataExportInput{
		ProjectID:   project.ID.String(),
		Name:        "Duplicate route destination",
		EndpointURL: "https://other.example.test/v1",
		DataSource:  "risk_findings",
		Confirmed:   true,
	})
	require.ErrorIs(t, err, ErrDataExportRouteConflict)
	destinations, err = queries.ListOtelDestinations(ctx, dataexportsrepo.ListOtelDestinationsParams{
		OrganizationID: principal.OrganizationID,
		ProjectID:      project.ID,
	})
	require.NoError(t, err)
	require.Len(t, destinations, 1, "the conflicting destination must roll back with its route")
}

type recordingRecentToolCallReader struct {
	params telemetryrepo.ListToolUsageTracesParams
	rows   []telemetryrepo.ToolUsageTraceSummary
	err    error
}

func (r *recordingRecentToolCallReader) ListToolUsageTraces(_ context.Context, params telemetryrepo.ListToolUsageTracesParams) ([]telemetryrepo.ToolUsageTraceSummary, error) {
	r.params = params
	return r.rows, r.err
}

func TestListRecentToolCallsUsesBoundedSafeSummaryProjection(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	conn, err := platformMCPInfra.CloneTestDatabase(t, "platform_mcp_recent_tool_calls")
	require.NoError(t, err)
	principal, project := seedRegistrationLifecycle(t, ctx, conn)
	fixedNow := time.Date(2026, 8, 21, 12, 0, 0, 0, time.UTC)
	statusCode := int32(500)
	client := "claude-code"
	telemetry := &recordingRecentToolCallReader{rows: []telemetryrepo.ToolUsageTraceSummary{{
		ID:                "summary-row",
		TraceID:           "trace-id",
		LogGroupKind:      "trace_id",
		LogGroupValue:     "trace-id",
		StartTimeUnixNano: fixedNow.Add(-time.Minute).UnixNano(),
		LogCount:          2,
		GramURN:           "tools:payments:refund",
		ToolName:          "refund",
		TargetType:        "hosted_mcp",
		TargetKind:        "server",
		TargetID:          "payments",
		TargetLabel:       "Payments",
		UserKey:           "private-user-key",
		UserLabel:         "person@example.test",
		UserKind:          "email",
		HookSource:        &client,
		EventSource:       "tool_call",
		HTTPStatusCode:    &statusCode,
		HookStatus:        nil,
		BlockReason:       nil,
		AccountType:       nil,
	}}}
	reader := NewPostgresReader(testenv.NewLogger(t), conn).
		WithRecentToolCalls(telemetry, mustParseURL(t, "https://app.getgram.test"))
	reader.recentToolCalls.now = func() time.Time { return fixedNow }

	output, err := reader.ListRecentToolCalls(ctx, principal, ListRecentToolCallsInput{ProjectSlug: project.Slug})
	require.NoError(t, err)
	require.Equal(t, project.ID.String(), output.ProjectID)
	require.Equal(t, DiagnosticWindowLastHour, output.Window.Window)
	require.False(t, output.More)
	require.Len(t, output.Calls, 1)
	require.Equal(t, "error", output.Calls[0].Outcome)
	require.Equal(t, "refund", output.Calls[0].ToolName)
	require.Equal(t, "claude-code", output.Calls[0].Client)
	require.True(t, strings.HasSuffix(output.ToolLogsURL, "/projects/"+project.Slug+"/logs"))

	require.Equal(t, project.ID.String(), telemetry.params.GramProjectID)
	require.Equal(t, fixedNow.Add(-time.Hour).UnixNano(), telemetry.params.TimeStart)
	require.Equal(t, fixedNow.UnixNano(), telemetry.params.TimeEnd)
	require.Equal(t, "desc", telemetry.params.SortOrder)
	require.Equal(t, defaultRecentToolCallLimit+1, telemetry.params.Limit)
	require.Empty(t, telemetry.params.Query)
	require.Empty(t, telemetry.params.Filters)

	encoded, err := json.Marshal(output)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), "private-user-key")
	require.NotContains(t, string(encoded), "person@example.test")
	require.NotContains(t, string(encoded), "trace-id")
}

func TestRecentToolCallTargetOmitsUnclassifiedShadowSource(t *testing.T) {
	t.Parallel()

	target := recentToolCallTarget(telemetryrepo.ToolUsageTraceSummary{
		TargetType:  telemetryrepo.ToolUsageTargetTypeShadowMCP,
		TargetLabel: "npx --yes package --token private",
	})
	require.Empty(t, target)
}
