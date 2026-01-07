package telemetry

import (
	"context"
	"encoding/json"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	telem_srv "github.com/speakeasy-api/gram/server/gen/http/telemetry/server"
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

var _ telem_gen.Service = (*Service)(nil)
var _ telem_gen.Auther = (*Service)(nil)

func NewService(logger *slog.Logger, db *pgxpool.Pool, sessions *sessions.Manager, tcm ToolMetricsProvider) *Service {
	logger = logger.With(attr.SlogComponent("telemetry"))

	return &Service{
		tracer: otel.Tracer("github.com/speakeasy-api/gram/server/internal/telemetry"),
		auth:   auth.New(logger, db, sessions),
		logger: logger,
		tcm:    tcm,
		db:     db,
	}
}

func Attach(mux goahttp.Muxer, service *Service) {
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
	var deploymentID, functionID, gramURN string
	if payload.Filter != nil {
		from, to, err = parseTimeRange(payload.Filter.From, payload.Filter.To)
		if err != nil {
			return nil, err
		}
		deploymentID = conv.PtrValOr(payload.Filter.DeploymentID, "")
		functionID = conv.PtrValOr(payload.Filter.FunctionID, "")
		gramURN = conv.PtrValOr(payload.Filter.GramUrn, "")
	}

	// Query with limit+1 to detect if there are more results
	items, err := s.tcm.ListTraces(ctx, repo.ListTracesParams{
		GramProjectID:    projectID.String(),
		TimeStart:        from,
		TimeEnd:          to,
		GramDeploymentID: deploymentID,
		GramFunctionID:   functionID,
		GramURN:          gramURN,
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
