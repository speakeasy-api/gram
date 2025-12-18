package telemetry

import (
	"context"
	"encoding/json"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	srv "github.com/speakeasy-api/gram/server/gen/http/logs/server"
	gen "github.com/speakeasy-api/gram/server/gen/logs"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/auth"
	"github.com/speakeasy-api/gram/server/internal/auth/sessions"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/middleware"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/telemetry/repo"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/trace"
	goahttp "goa.design/goa/v3/http"
	"goa.design/goa/v3/security"
)

type Service struct {
	tcm    ToolMetricsProvider
	db     *pgxpool.Pool
	tracer trace.Tracer
	logger *slog.Logger
	auth   *auth.Auth
}

var _ gen.Service = (*Service)(nil)
var _ gen.Auther = (*Service)(nil)

func NewService(logger *slog.Logger, db *pgxpool.Pool, sessions *sessions.Manager, tcm ToolMetricsProvider) *Service {
	logger = logger.With(attr.SlogComponent("logs"))

	return &Service{
		tracer: otel.Tracer("github.com/speakeasy-api/gram/server/internal/telemetry"),
		auth:   auth.New(logger, db, sessions),
		logger: logger,
		tcm:    tcm,
		db:     db,
	}
}

func Attach(mux goahttp.Muxer, service *Service) {
	endpoints := gen.NewEndpoints(service)
	endpoints.Use(middleware.MapErrors())
	endpoints.Use(middleware.TraceMethods(service.tracer))
	srv.Mount(
		mux,
		srv.New(endpoints, mux, goahttp.RequestDecoder, goahttp.ResponseEncoder, nil, nil),
	)
}

func (s *Service) APIKeyAuth(ctx context.Context, key string, schema *security.APIKeyScheme) (context.Context, error) {
	return s.auth.Authorize(ctx, key, schema)
}

func (s *Service) ListLogs(ctx context.Context, payload *gen.ListLogsPayload) (res *gen.ListToolLogResponse, err error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	projectID := authCtx.ProjectID

	logsEnabled, err := s.tcm.ShouldLog(ctx, authCtx.ActiveOrganizationID)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error checking if tool metrics logging is enabled").
			Log(ctx, s.logger, attr.SlogProjectID(projectID.String()))
	}

	if !logsEnabled {
		return &gen.ListToolLogResponse{Logs: make([]*gen.HTTPToolLog, 0), Pagination: &gen.PaginationResponse{
			PerPage:        conv.Ptr(0),
			HasNextPage:    conv.Ptr(false),
			NextPageCursor: conv.Ptr(""),
		}, Enabled: logsEnabled}, nil
	}

	// Parse time parameters with defaults
	tsStart := parseTimeOrDefault(payload.TsStart, time.Now().Add(-744*time.Hour).UTC())
	tsEnd := parseTimeOrDefault(payload.TsEnd, time.Now().UTC())

	id, err := uuid.NewV7()
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error generating default cursor").
			Log(ctx, s.logger, attr.SlogProjectID(projectID.String()))
	}

	options := repo.ListToolLogsOptions{
		ProjectID:  projectID.String(),
		TsStart:    tsStart,
		TsEnd:      tsEnd,
		Cursor:     conv.PtrValOr(payload.Cursor, id.String()),
		Status:     conv.PtrValOr(payload.Status, ""),
		ServerName: conv.PtrValOr(payload.ServerName, ""),
		ToolName:   conv.PtrValOr(payload.ToolName, ""),
		ToolType:   conv.PtrValOr(payload.ToolType, ""),
		ToolURNs:   payload.ToolUrns,
		Pagination: &repo.Pagination{
			PerPage:    payload.PerPage,
			Sort:       payload.Sort,
			Direction:  repo.PageDirection(payload.Direction),
			PrevCursor: "",
			NextCursor: "",
		},
	}
	options.SetDefaults()

	// Query logs from ClickHouse
	result, err := s.tcm.ListHTTPRequests(ctx, options)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error listing logs").
			Log(ctx, s.logger, attr.SlogProjectID(projectID.String()))
	}

	// write now the stub client is returning nil, ensure we don't panic
	if result == nil {
		return &gen.ListToolLogResponse{Logs: make([]*gen.HTTPToolLog, 0), Pagination: &gen.PaginationResponse{
			PerPage:        conv.Ptr(0),
			HasNextPage:    conv.Ptr(false),
			NextPageCursor: conv.Ptr(""),
		}, Enabled: logsEnabled}, nil
	}

	// Convert results to gen.HTTPToolLog
	logs := make([]*gen.HTTPToolLog, 0, len(result.Logs))
	for _, r := range result.Logs {
		logs = append(logs, toHTTPToolLog(r, projectID.String()))
	}

	// Convert pagination metadata to API format
	var nextPageCursor *string
	if result.Pagination.NextPageCursor != nil {
		nextPageCursor = result.Pagination.NextPageCursor
	}

	pp := &gen.PaginationResponse{
		PerPage:        &result.Pagination.PerPage,
		HasNextPage:    &result.Pagination.HasNextPage,
		NextPageCursor: nextPageCursor,
	}

	return &gen.ListToolLogResponse{Logs: logs, Pagination: pp, Enabled: logsEnabled}, nil
}

func parseTimeOrDefault(s *string, defaultTime time.Time) time.Time {
	if s == nil || *s == "" {
		return defaultTime
	}
	t, err := time.Parse(time.RFC3339, *s)
	if err != nil {
		return defaultTime
	}
	return t
}

func toHTTPToolLog(r repo.ToolHTTPRequest, projectId string) *gen.HTTPToolLog {
	return &gen.HTTPToolLog{
		ID:                conv.Ptr(r.ID),
		Ts:                r.Ts.Format(time.RFC3339),
		OrganizationID:    r.OrganizationID,
		ProjectID:         conv.Ptr(projectId),
		DeploymentID:      r.DeploymentID,
		ToolID:            r.ToolID,
		ToolUrn:           r.ToolURN,
		ToolType:          gen.ToolType(r.ToolType),
		TraceID:           r.TraceID,
		SpanID:            r.SpanID,
		HTTPServerURL:     r.HTTPServerURL,
		HTTPMethod:        r.HTTPMethod,
		HTTPRoute:         r.HTTPRoute,
		StatusCode:        r.StatusCode,
		DurationMs:        r.DurationMs,
		UserAgent:         r.UserAgent,
		RequestHeaders:    r.RequestHeaders,
		RequestBodyBytes:  conv.Ptr(r.RequestBodyBytes),
		ResponseHeaders:   r.ResponseHeaders,
		ResponseBodyBytes: conv.Ptr(r.ResponseBodyBytes),
	}
}

func (s *Service) ListToolExecutionLogs(ctx context.Context, payload *gen.ListToolExecutionLogsPayload) (res *gen.ListToolExecutionLogsResult, err error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	projectID := authCtx.ProjectID

	// Parse time parameters - use very wide bounds if not specified to allow querying all historical data
	// Unix epoch (Jan 1, 1970) as default start time
	tsStart := parseTimeOrDefault(payload.TsStart, time.Unix(0, 0).UTC())
	// Far future (year 2100) as default end time
	tsEnd := parseTimeOrDefault(payload.TsEnd, time.Date(2100, 1, 1, 0, 0, 0, 0, time.UTC))

	// Set defaults
	perPage := 20
	if payload.PerPage < 0 || payload.PerPage > 100 {
		return nil, oops.E(oops.CodeBadRequest, nil, "per page must be between 1 and 100")
	}
	if payload.PerPage > 0 {
		perPage = payload.PerPage
	}

	sortOrder := "desc"
	if payload.Sort != "desc" && payload.Sort != "asc" && payload.Sort != "" {
		return nil, oops.E(oops.CodeBadRequest, nil, "sort order must be one of 'asc' or 'desc'")
	}

	// if a non-empty sort string is passed we can assume it's a valid sort as we validated it above
	if payload.Sort != "" {
		sortOrder = payload.Sort
	}

	// Use nil UUID as sentinel for "no cursor" (first page)
	cursor := uuid.Nil.String()
	if payload.Cursor != nil && *payload.Cursor != "" {
		// Validate that cursor is a valid UUID
		if _, err := uuid.Parse(*payload.Cursor); err != nil {
			return nil, oops.E(oops.CodeBadRequest, err, "cursor must be a valid UUID")
		}
		cursor = *payload.Cursor
	}

	params := repo.ListToolLogsParams{
		ProjectID:    projectID.String(),
		TsStart:      tsStart,
		TsEnd:        tsEnd,
		DeploymentID: conv.PtrValOr(payload.DeploymentID, ""),
		FunctionID:   conv.PtrValOr(payload.FunctionID, ""),
		Instance:     conv.PtrValOr(payload.Instance, ""),
		Level:        conv.PtrValOr(payload.Level, ""),
		Source:       conv.PtrValOr(payload.Source, ""),
		SortOrder:    sortOrder,
		Cursor:       cursor,
		Limit:        perPage + 1, // +1 for detecting next page
	}

	result, err := s.tcm.ListToolLogs(ctx, params)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error listing tool execution logs").
			Log(ctx, s.logger, attr.SlogProjectID(projectID.String()))
	}

	if result == nil {
		return &gen.ListToolExecutionLogsResult{
			Logs: make([]*gen.ToolExecutionLog, 0),
			Pagination: &gen.PaginationResponse{
				PerPage:        conv.Ptr(0),
				HasNextPage:    conv.Ptr(false),
				NextPageCursor: conv.Ptr(""),
			},
		}, nil
	}

	// Convert results to gen.ToolExecutionLog
	logs := make([]*gen.ToolExecutionLog, 0, len(result.Logs))
	for _, r := range result.Logs {
		logs = append(logs, toToolExecutionLog(r))
	}

	// Convert pagination metadata to API format
	var nextPageCursor *string
	if result.Pagination.NextPageCursor != nil {
		nextPageCursor = result.Pagination.NextPageCursor
	}

	pp := &gen.PaginationResponse{
		PerPage:        &result.Pagination.PerPage,
		HasNextPage:    &result.Pagination.HasNextPage,
		NextPageCursor: nextPageCursor,
	}

	return &gen.ListToolExecutionLogsResult{Logs: logs, Pagination: pp}, nil
}

func toToolExecutionLog(r repo.ToolLog) *gen.ToolExecutionLog {
	return &gen.ToolExecutionLog{
		ID:           r.ID,
		Timestamp:    r.Timestamp.Format(time.RFC3339),
		Instance:     r.Instance,
		Level:        r.Level,
		Source:       r.Source,
		RawLog:       r.RawLog,
		Message:      r.Message,
		Attributes:   conv.Ptr(r.Attributes),
		ProjectID:    r.ProjectID,
		DeploymentID: r.DeploymentID,
		FunctionID:   r.FunctionID,
	}
}

// ListTelemetryLogs retrieves unified telemetry logs with pagination.
func (s *Service) ListTelemetryLogs(ctx context.Context, payload *gen.ListTelemetryLogsPayload) (res *gen.ListTelemetryLogsResult, err error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	projectID := authCtx.ProjectID

	limit := payload.Limit
	if limit < 1 || limit > 1000 {
		return nil, oops.E(oops.CodeBadRequest, nil, "limit must be between 1 and 1000")
	}

	sortOrder := "desc"
	if payload.Sort != "desc" && payload.Sort != "asc" && payload.Sort != "" {
		return nil, oops.E(oops.CodeBadRequest, nil, "sort order must be one of 'asc' or 'desc'")
	}

	// if a non-empty sort string is passed we can assume it's a valid sort as we validated it above
	if payload.Sort != "" {
		sortOrder = payload.Sort
	}

	// Empty string cursor for first page
	cursor := ""
	if payload.Cursor != nil {
		cursor = *payload.Cursor
	}

	// Default time range: all time if not specified
	now := time.Now()
	timeStart := conv.PtrValOr(payload.TimeStart, int64(0)) // Beginning of time (epoch)
	timeEnd := conv.PtrValOr(payload.TimeEnd, now.UnixNano())

	// Query with limit+1 to detect if there are more results
	items, err := s.tcm.ListTelemetryLogs(ctx, repo.ListTelemetryLogsParams{
		GramProjectID:          projectID.String(),
		TimeStart:              timeStart,
		TimeEnd:                timeEnd,
		GramURN:                conv.PtrValOr(payload.GramUrn, ""),
		TraceID:                conv.PtrValOr(payload.TraceID, ""),
		GramDeploymentID:       conv.PtrValOr(payload.DeploymentID, ""),
		GramFunctionID:         conv.PtrValOr(payload.FunctionID, ""),
		SeverityText:           conv.PtrValOr(payload.SeverityText, ""),
		HTTPResponseStatusCode: conv.PtrValOr(payload.HTTPStatusCode, 0),
		HTTPRoute:              conv.PtrValOr(payload.HTTPRoute, ""),
		HTTPRequestMethod:      conv.PtrValOr(payload.HTTPMethod, ""),
		ServiceName:            conv.PtrValOr(payload.ServiceName, ""),
		SortOrder:              sortOrder,
		Cursor:                 cursor,
		Limit:                  limit + 1, // +1 for overflow detection
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error listing telemetry logs")
	}

	// Compute next cursor using limit+1 pattern
	var nextCursor *string
	if len(items) > limit {
		// More results exist - set cursor to last item in the page
		nextCursor = conv.Ptr(items[limit-1].ID)
		// Trim to requested page size
		items = items[:limit]
	}

	// Convert repo models to Goa types
	telemetryLogs := make([]*gen.TelemetryLogRecord, len(items))
	for i, log := range items {
		record, err := toTelemetryLogPayload(log)
		if err != nil {
			return nil, err
		}
		telemetryLogs[i] = record
	}

	return &gen.ListTelemetryLogsResult{
		Logs:       telemetryLogs,
		NextCursor: nextCursor,
	}, nil
}

// ListTraces retrieves trace summaries with pagination.
func (s *Service) ListTraces(ctx context.Context, payload *gen.ListTracesPayload) (res *gen.ListTracesResult, err error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	projectID := authCtx.ProjectID

	// Validate and set limit (defaults handled by Goa)
	limit := payload.Limit
	if limit < 1 || limit > 1000 {
		return nil, oops.E(oops.CodeBadRequest, nil, "limit must be between 1 and 1000")
	}

	sortOrder := "desc"
	if payload.Sort != "desc" && payload.Sort != "asc" && payload.Sort != "" {
		return nil, oops.E(oops.CodeBadRequest, nil, "sort order must be one of 'asc' or 'desc'")
	}

	// if a non-empty sort string is passed we can assume it's a valid sort as we validated it above
	if payload.Sort != "" {
		sortOrder = payload.Sort
	}

	// Empty string cursor for first page
	cursor := ""
	if payload.Cursor != nil && *payload.Cursor != "" {
		cursor = *payload.Cursor
	}

	// Default time range: all time if not specified
	now := time.Now()
	timeStart := conv.PtrValOr(payload.TimeStart, int64(0)) // Beginning of time (epoch)
	timeEnd := conv.PtrValOr(payload.TimeEnd, now.UnixNano())

	// Query with limit+1 to detect if there are more results
	items, err := s.tcm.ListTraces(ctx, repo.ListTracesParams{
		GramProjectID:    projectID.String(),
		TimeStart:        timeStart,
		TimeEnd:          timeEnd,
		GramDeploymentID: conv.PtrValOr(payload.DeploymentID, ""),
		GramFunctionID:   conv.PtrValOr(payload.FunctionID, ""),
		SortOrder:        sortOrder,
		Cursor:           cursor,
		Limit:            limit + 1, // +1 for overflow detection
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error listing traces")
	}

	// Compute next cursor using limit+1 pattern
	var nextCursor *string
	if len(items) > limit {
		// More results exist - set cursor to last item's trace_id
		nextCursor = &items[limit-1].TraceID
		items = items[:limit]
	}

	// Convert repo models to Goa types
	traces := make([]*gen.TraceSummaryRecord, len(items))
	for i, item := range items {
		traces[i] = &gen.TraceSummaryRecord{
			TraceID:           item.TraceID,
			StartTimeUnixNano: item.StartTimeUnixNano,
			LogCount:          item.LogCount,
			HTTPStatusCode:    item.HTTPStatusCode,
			GramUrn:           item.GramURN,
		}
	}

	return &gen.ListTracesResult{
		Traces:     traces,
		NextCursor: nextCursor,
	}, nil
}

// ListLogsForTrace retrieves all logs for a specific trace ID.
func (s *Service) ListLogsForTrace(ctx context.Context, payload *gen.ListLogsForTracePayload) (res *gen.ListLogsForTraceResult, err error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	projectID := authCtx.ProjectID

	// Fetch all logs for the trace (no pagination - traces typically have limited logs)
	logs, err := s.tcm.ListLogsForTrace(ctx, repo.ListLogsForTraceParams{
		GramProjectID: projectID.String(),
		TraceID:       payload.TraceID,
		Limit:         1000, // Hard limit to prevent abuse
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error listing logs for trace")
	}

	// Convert repo models to Goa types
	telemetryLogs := make([]*gen.TelemetryLogRecord, len(logs))
	for i, log := range logs {
		record, err := toTelemetryLogPayload(log)
		if err != nil {
			return nil, err
		}
		telemetryLogs[i] = record
	}

	return &gen.ListLogsForTraceResult{
		Logs: telemetryLogs,
	}, nil
}

// toTelemetryLogPayload converts a ClickHouse telemetry log record to the API response format.
// It parses the JSON-encoded attributes and resource_attributes fields into proper JSON objects.
func toTelemetryLogPayload(log repo.TelemetryLog) (*gen.TelemetryLogRecord, error) {
	// Parse JSON attributes into objects
	var attributes any
	var resourceAttributes any

	if err := json.Unmarshal([]byte(log.Attributes), &attributes); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "failed to parse log attributes")
	}
	if err := json.Unmarshal([]byte(log.ResourceAttributes), &resourceAttributes); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "failed to parse resource attributes")
	}

	return &gen.TelemetryLogRecord{
		ID:                   log.ID,
		TimeUnixNano:         log.TimeUnixNano,
		ObservedTimeUnixNano: log.ObservedTimeUnixNano,
		SeverityText:         log.SeverityText,
		Body:                 log.Body,
		TraceID:              log.TraceID,
		SpanID:               log.SpanID,
		Attributes:           attributes,
		ResourceAttributes:   resourceAttributes,
		Service: &gen.ServiceInfo{
			Name:    log.ServiceName,
			Version: log.ServiceVersion,
		},
	}, nil
}
