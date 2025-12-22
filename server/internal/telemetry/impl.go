package telemetry

import (
	"context"
	"encoding/json"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	srv "github.com/speakeasy-api/gram/server/gen/http/logs/server"
	telem_srv "github.com/speakeasy-api/gram/server/gen/http/telemetry/server"
	gen "github.com/speakeasy-api/gram/server/gen/logs"
	telem_gen "github.com/speakeasy-api/gram/server/gen/telemetry"
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

	telemEndpoints := telem_gen.NewEndpoints(service)
	telemEndpoints.Use(middleware.MapErrors())
	telemEndpoints.Use(middleware.TraceMethods(service.tracer))
	telem_srv.Mount(
		mux,
		telem_srv.New(telemEndpoints, mux, goahttp.RequestDecoder, goahttp.ResponseEncoder, nil, nil),
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

// SearchLogs retrieves unified telemetry logs with pagination.
func (s *Service) SearchLogs(ctx context.Context, payload *telem_gen.SearchLogsPayload) (res *telem_gen.SearchLogsResult, err error) {
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

	from := int64(0)
	to := time.Now().UnixNano()

	// Extract filter values
	var gramURN, traceID, deploymentID, functionID, severityText, httpRoute, httpMethod, serviceName string
	var httpStatusCode int32
	if payload.Filter != nil {
		from, to, err = parseTimeRange(payload.Filter.From, payload.Filter.To)
		if err != nil {
			return nil, err
		}

		gramURN = conv.PtrValOr(payload.Filter.GramUrn, "")
		traceID = conv.PtrValOr(payload.Filter.TraceID, "")
		deploymentID = conv.PtrValOr(payload.Filter.DeploymentID, "")
		functionID = conv.PtrValOr(payload.Filter.FunctionID, "")
		severityText = conv.PtrValOr(payload.Filter.SeverityText, "")
		httpStatusCode = conv.PtrValOr(payload.Filter.HTTPStatusCode, 0)
		httpRoute = conv.PtrValOr(payload.Filter.HTTPRoute, "")
		httpMethod = conv.PtrValOr(payload.Filter.HTTPMethod, "")
		serviceName = conv.PtrValOr(payload.Filter.ServiceName, "")
	}

	// Query with limit+1 to detect if there are more results
	items, err := s.tcm.ListTelemetryLogs(ctx, repo.ListTelemetryLogsParams{
		GramProjectID:          projectID.String(),
		TimeStart:              from,
		TimeEnd:                to,
		GramURN:                gramURN,
		TraceID:                traceID,
		GramDeploymentID:       deploymentID,
		GramFunctionID:         functionID,
		SeverityText:           severityText,
		HTTPResponseStatusCode: httpStatusCode,
		HTTPRoute:              httpRoute,
		HTTPRequestMethod:      httpMethod,
		ServiceName:            serviceName,
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
	telemetryLogs := make([]*telem_gen.TelemetryLogRecord, len(items))
	for i, log := range items {
		record, err := toTelemetryLogPayload(log)
		if err != nil {
			return nil, err
		}
		telemetryLogs[i] = record
	}

	return &telem_gen.SearchLogsResult{
		Logs:       telemetryLogs,
		NextCursor: nextCursor,
	}, nil
}

// SearchToolCalls retrieves tool call summaries with pagination.
func (s *Service) SearchToolCalls(ctx context.Context, payload *telem_gen.SearchToolCallsPayload) (res *telem_gen.SearchToolCallsResult, err error) {
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

	from := int64(0)
	to := time.Now().UnixNano()

	// Extract filter values
	var deploymentID, functionID string
	if payload.Filter != nil {
		from, to, err = parseTimeRange(payload.Filter.From, payload.Filter.To)
		if err != nil {
			return nil, err
		}
		deploymentID = conv.PtrValOr(payload.Filter.DeploymentID, "")
		functionID = conv.PtrValOr(payload.Filter.FunctionID, "")
		// TODO: gram_urn filtering not yet supported for tool calls aggregation
	}

	// Query with limit+1 to detect if there are more results
	items, err := s.tcm.ListTraces(ctx, repo.ListTracesParams{
		GramProjectID:    projectID.String(),
		TimeStart:        from,
		TimeEnd:          to,
		GramDeploymentID: deploymentID,
		GramFunctionID:   functionID,
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
	toolCalls := make([]*telem_gen.ToolCallSummary, len(items))
	for i, item := range items {
		toolCalls[i] = &telem_gen.ToolCallSummary{
			TraceID:           item.TraceID,
			StartTimeUnixNano: item.StartTimeUnixNano,
			LogCount:          item.LogCount,
			HTTPStatusCode:    item.HTTPStatusCode,
			GramUrn:           item.GramURN,
		}
	}

	return &telem_gen.SearchToolCallsResult{
		ToolCalls:  toolCalls,
		NextCursor: nextCursor,
	}, nil
}

// parseTimeRange extracts and parses the time range from a telemetry filter.
// Returns Unix nanoseconds for start and end times.
// Defaults: start=0 (epoch), end=now
func parseTimeRange(from, to *string) (timeStart, timeEnd int64, err error) {
	timeStart = 0
	timeEnd = time.Now().UnixNano()

	if from != nil && *from != "" {
		fromTime, parseErr := time.Parse(time.RFC3339, *from)
		if parseErr != nil {
			return 0, 0, oops.E(oops.CodeBadRequest, parseErr, "invalid 'from' time format, expected ISO 8601 (e.g., '2025-12-19T10:00:00Z')")
		}
		timeStart = fromTime.UnixNano()
	}

	if to != nil && *to != "" {
		toTime, parseErr := time.Parse(time.RFC3339, *to)
		if parseErr != nil {
			return 0, 0, oops.E(oops.CodeBadRequest, parseErr, "invalid 'to' time format, expected ISO 8601 (e.g., '2025-12-19T11:00:00Z')")
		}
		timeEnd = toTime.UnixNano()
	}

	return timeStart, timeEnd, nil
}

// toTelemetryLogPayload converts a ClickHouse telemetry log record to the API response format.
// It parses the JSON-encoded attributes and resource_attributes fields into proper JSON objects.
func toTelemetryLogPayload(log repo.TelemetryLog) (*telem_gen.TelemetryLogRecord, error) {
	// Parse JSON attributes into objects
	var attributes any
	var resourceAttributes any

	if err := json.Unmarshal([]byte(log.Attributes), &attributes); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "failed to parse log attributes")
	}
	if err := json.Unmarshal([]byte(log.ResourceAttributes), &resourceAttributes); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "failed to parse resource attributes")
	}

	return &telem_gen.TelemetryLogRecord{
		ID:                   log.ID,
		TimeUnixNano:         log.TimeUnixNano,
		ObservedTimeUnixNano: log.ObservedTimeUnixNano,
		SeverityText:         log.SeverityText,
		Body:                 log.Body,
		TraceID:              log.TraceID,
		SpanID:               log.SpanID,
		Attributes:           attributes,
		ResourceAttributes:   resourceAttributes,
		Service: &telem_gen.ServiceInfo{
			Name:    log.ServiceName,
			Version: log.ServiceVersion,
		},
	}, nil
}
