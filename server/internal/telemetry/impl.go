package telemetry

import (
	"context"
	"encoding/json"
	"log/slog"
	"math"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/jackc/pgx/v5/pgxpool"
	telem_srv "github.com/speakeasy-api/gram/server/gen/http/telemetry/server"
	telem_gen "github.com/speakeasy-api/gram/server/gen/telemetry"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/auth"
	"github.com/speakeasy-api/gram/server/internal/auth/chatsessions"
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

const logsDisabledMsg = "logs are not enabled for this organization"

type Service struct {
	auth         *auth.Auth
	db           *pgxpool.Pool
	chConn       clickhouse.Conn
	chRepo       *repo.Queries
	logger       *slog.Logger
	tracer       trace.Tracer
	posthog      PosthogClient
	chatSessions *chatsessions.Manager
	logsEnabled  LogsEnabled
}

var _ telem_gen.Service = (*Service)(nil)
var _ telem_gen.Auther = (*Service)(nil)

func NewService(
	logger *slog.Logger,
	db *pgxpool.Pool,
	chConn clickhouse.Conn,
	sessions *sessions.Manager,
	chatSessions *chatsessions.Manager,
	logsEnabled LogsEnabled,
	posthogClient PosthogClient) *Service {
	logger = logger.With(attr.SlogComponent("logs"))
	chRepo := repo.New(chConn)

	return &Service{
		auth:         auth.New(logger, db, sessions),
		db:           db,
		chConn:       chConn,
		chRepo:       chRepo,
		logger:       logger,
		logsEnabled:  logsEnabled,
		tracer:       otel.Tracer("github.com/speakeasy-api/gram/server/internal/telemetry"),
		posthog:      posthogClient,
		chatSessions: chatSessions,
	}
}

func Attach(mux goahttp.Muxer, service *Service) {
	endpoints := telem_gen.NewEndpoints(service)

	endpoints.Use(middleware.MapErrors())
	endpoints.Use(middleware.TraceMethods(service.tracer))

	telem_srv.Mount(
		mux,
		telem_srv.New(endpoints, mux, goahttp.RequestDecoder, goahttp.ResponseEncoder, nil, nil),
	)
}

func (s *Service) APIKeyAuth(ctx context.Context, key string, schema *security.APIKeyScheme) (context.Context, error) {
	return s.auth.Authorize(ctx, key, schema)
}

func (s *Service) JWTAuth(ctx context.Context, token string, schema *security.JWTScheme) (context.Context, error) {
	return s.chatSessions.Authorize(ctx, token)
}

// SearchLogs retrieves unified telemetry logs with pagination.
func (s *Service) SearchLogs(ctx context.Context, payload *telem_gen.SearchLogsPayload) (res *telem_gen.SearchLogsResult, err error) {
	var from, to *string
	if payload.Filter != nil {
		from, to = payload.Filter.From, payload.Filter.To
	}

	params, err := s.prepareTelemetrySearch(ctx, payload.Limit, payload.Sort, payload.Cursor, from, to)
	if err != nil {
		return nil, err
	}

	if !params.enabled {
		return nil, oops.E(oops.CodeLogsDisabled, nil, logsDisabledMsg)
	}

	// Extract SearchLogs-specific filter fields
	var traceID, deploymentID, functionID, severityText, httpRoute, httpMethod, serviceName, gramChatID string
	var httpStatusCode int32
	var gramURNs []string
	if payload.Filter != nil {
		// Handle both gram_urn (single) and gram_urns (array) for backwards compatibility
		gramURNs = resolveGramURNs(payload.Filter.GramUrn, payload.Filter.GramUrns)
		traceID = conv.PtrValOr(payload.Filter.TraceID, "")
		deploymentID = conv.PtrValOr(payload.Filter.DeploymentID, "")
		functionID = conv.PtrValOr(payload.Filter.FunctionID, "")
		severityText = conv.PtrValOr(payload.Filter.SeverityText, "")
		httpStatusCode = conv.PtrValOr(payload.Filter.HTTPStatusCode, 0)
		httpRoute = conv.PtrValOr(payload.Filter.HTTPRoute, "")
		httpMethod = conv.PtrValOr(payload.Filter.HTTPMethod, "")
		serviceName = conv.PtrValOr(payload.Filter.ServiceName, "")
		gramChatID = conv.PtrValOr(payload.Filter.GramChatID, "")
	}

	// Query with limit+1 to detect if there are more results
	items, err := s.chRepo.ListTelemetryLogs(ctx, repo.ListTelemetryLogsParams{
		GramProjectID:          params.projectID,
		TimeStart:              params.timeStart,
		TimeEnd:                params.timeEnd,
		GramURNs:               gramURNs,
		TraceID:                traceID,
		GramDeploymentID:       deploymentID,
		GramFunctionID:         functionID,
		SeverityText:           severityText,
		HTTPResponseStatusCode: httpStatusCode,
		HTTPRoute:              httpRoute,
		HTTPRequestMethod:      httpMethod,
		ServiceName:            serviceName,
		GramChatID:             gramChatID,
		SortOrder:              params.sortOrder,
		Cursor:                 params.cursor,
		Limit:                  params.limit + 1,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error listing telemetry logs")
	}

	// Compute next cursor using limit+1 pattern
	var nextCursor *string
	if len(items) > params.limit {
		nextCursor = conv.Ptr(items[params.limit-1].ID)
		items = items[:params.limit]
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
		Enabled:    true,
		NextCursor: nextCursor,
	}, nil
}

// SearchToolCalls retrieves tool call summaries with pagination.
func (s *Service) SearchToolCalls(ctx context.Context, payload *telem_gen.SearchToolCallsPayload) (res *telem_gen.SearchToolCallsResult, err error) {
	var from, to *string
	if payload.Filter != nil {
		from, to = payload.Filter.From, payload.Filter.To
	}

	params, err := s.prepareTelemetrySearch(ctx, payload.Limit, payload.Sort, payload.Cursor, from, to)
	if err != nil {
		return nil, err
	}

	if !params.enabled {
		return nil, oops.E(oops.CodeLogsDisabled, nil, logsDisabledMsg)
	}

	// Extract SearchToolCalls-specific filter fields
	var deploymentID, functionID, gramURN string
	if payload.Filter != nil {
		deploymentID = conv.PtrValOr(payload.Filter.DeploymentID, "")
		functionID = conv.PtrValOr(payload.Filter.FunctionID, "")
		gramURN = conv.PtrValOr(payload.Filter.GramUrn, "")
	}

	// Query with limit+1 to detect if there are more results
	items, err := s.chRepo.ListTraces(ctx, repo.ListTracesParams{
		GramProjectID:    params.projectID,
		TimeStart:        params.timeStart,
		TimeEnd:          params.timeEnd,
		GramDeploymentID: deploymentID,
		GramFunctionID:   functionID,
		GramURN:          gramURN,
		SortOrder:        params.sortOrder,
		Cursor:           params.cursor,
		Limit:            params.limit + 1,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error listing traces")
	}

	// Compute next cursor using limit+1 pattern
	var nextCursor *string
	if len(items) > params.limit {
		nextCursor = &items[params.limit-1].TraceID
		items = items[:params.limit]
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
		Enabled:    true,
		NextCursor: nextCursor,
	}, nil
}

// SearchChats retrieves chat session summaries with pagination.
func (s *Service) SearchChats(ctx context.Context, payload *telem_gen.SearchChatsPayload) (res *telem_gen.SearchChatsResult, err error) {
	var from, to *string
	if payload.Filter != nil {
		from, to = payload.Filter.From, payload.Filter.To
	}

	params, err := s.prepareTelemetrySearch(ctx, payload.Limit, payload.Sort, payload.Cursor, from, to)
	if err != nil {
		return nil, err
	}

	if !params.enabled {
		return nil, oops.E(oops.CodeLogsDisabled, nil, logsDisabledMsg)
	}

	var deploymentID, gramURN string
	if payload.Filter != nil {
		deploymentID = conv.PtrValOr(payload.Filter.DeploymentID, "")
		gramURN = conv.PtrValOr(payload.Filter.GramUrn, "")
	}

	items, err := s.chRepo.ListChats(ctx, repo.ListChatsParams{
		GramProjectID:    params.projectID,
		TimeStart:        params.timeStart,
		TimeEnd:          params.timeEnd,
		GramDeploymentID: deploymentID,
		GramURN:          gramURN,
		SortOrder:        params.sortOrder,
		Cursor:           params.cursor,
		Limit:            params.limit + 1,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error listing chats")
	}

	var nextCursor *string
	if len(items) > params.limit {
		nextCursor = &items[params.limit-1].GramChatID
		items = items[:params.limit]
	}

	chats := make([]*telem_gen.ChatSummary, len(items))
	for i, item := range items {
		chats[i] = &telem_gen.ChatSummary{
			GramChatID:        item.GramChatID,
			StartTimeUnixNano: item.StartTimeUnixNano,
			EndTimeUnixNano:   item.EndTimeUnixNano,
			LogCount:          item.LogCount,
			ToolCallCount:     item.ToolCallCount,
			MessageCount:      item.MessageCount,
			DurationSeconds:   sanitizeFloat64(item.DurationSeconds),
			Status:            item.Status,
			UserID:            item.UserID,
			Model:             item.Model,
			TotalInputTokens:  item.TotalInputTokens,
			TotalOutputTokens: item.TotalOutputTokens,
			TotalTokens:       item.TotalTokens,
		}
	}

	return &telem_gen.SearchChatsResult{
		Chats:      chats,
		Enabled:    true,
		NextCursor: nextCursor,
	}, nil
}

// GetProjectMetricsSummary retrieves aggregated metrics for an entire project.
func (s *Service) GetProjectMetricsSummary(ctx context.Context, payload *telem_gen.GetProjectMetricsSummaryPayload) (res *telem_gen.GetMetricsSummaryResult, err error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	logsEnabled, err := s.logsEnabled(ctx, authCtx.ActiveOrganizationID)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "unable to check if logs are enabled")
	}

	if !logsEnabled {
		return nil, oops.E(oops.CodeLogsDisabled, nil, logsDisabledMsg)
	}

	timeStart, timeEnd, err := parseTimeRange(&payload.From, &payload.To)
	if err != nil {
		return nil, err
	}

	metrics, err := s.chRepo.GetMetricsSummary(ctx, repo.GetMetricsSummaryParams{
		GramProjectID: authCtx.ProjectID.String(),
		TimeStart:     timeStart,
		TimeEnd:       timeEnd,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error retrieving project metrics")
	}

	return buildMetricsSummaryResult(*metrics), nil
}

// buildMetricsSummaryResult converts repo metrics to the API response format.
func buildMetricsSummaryResult(metrics repo.MetricsSummaryRow) *telem_gen.GetMetricsSummaryResult {
	// Convert models map to ModelUsage slice
	models := make([]*telem_gen.ModelUsage, 0, len(metrics.Models))
	for name, count := range metrics.Models {
		models = append(models, &telem_gen.ModelUsage{
			Name:  name,
			Count: int64(count), //nolint:gosec // Bounded count
		})
	}

	// Convert tool maps to ToolUsage slice
	tools := make([]*telem_gen.ToolUsage, 0, len(metrics.ToolCounts))
	for urn, count := range metrics.ToolCounts {
		tools = append(tools, &telem_gen.ToolUsage{
			Urn:          urn,
			Count:        int64(count),                          //nolint:gosec // Bounded count
			SuccessCount: int64(metrics.ToolSuccessCounts[urn]), //nolint:gosec // Bounded count
			FailureCount: int64(metrics.ToolFailureCounts[urn]), //nolint:gosec // Bounded count
		})
	}

	//nolint:gosec // Values are bounded counts that won't overflow int64
	return &telem_gen.GetMetricsSummaryResult{
		Metrics: &telem_gen.Metrics{
			TotalInputTokens:      metrics.TotalInputTokens,
			TotalOutputTokens:     metrics.TotalOutputTokens,
			TotalTokens:           metrics.TotalTokens,
			AvgTokensPerRequest:   sanitizeFloat64(metrics.AvgTokensPerReq),
			TotalChatRequests:     int64(metrics.TotalChatRequests),
			AvgChatDurationMs:     sanitizeFloat64(metrics.AvgChatDurationMs),
			FinishReasonStop:      int64(metrics.FinishReasonStop),
			FinishReasonToolCalls: int64(metrics.FinishReasonToolCalls),
			TotalToolCalls:        int64(metrics.TotalToolCalls),
			ToolCallSuccess:       int64(metrics.ToolCallSuccess),
			ToolCallFailure:       int64(metrics.ToolCallFailure),
			AvgToolDurationMs:     sanitizeFloat64(metrics.AvgToolDurationMs),
			TotalChats:            int64(metrics.TotalChats),
			DistinctModels:        int64(metrics.DistinctModels),
			DistinctProviders:     int64(metrics.DistinctProviders),
			Models:                models,
			Tools:                 tools,
		},
		Enabled: true,
	}
}

// searchParams contains common validated parameters for telemetry search endpoints.
type searchParams struct {
	projectID string
	enabled   bool
	limit     int
	sortOrder string
	cursor    string
	timeStart int64
	timeEnd   int64
}

// prepareTelemetrySearch validates and prepares common search parameters.
func (s *Service) prepareTelemetrySearch(ctx context.Context, limit int, sort string, cursor *string, from, to *string) (*searchParams, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	logsEnabled, err := s.logsEnabled(ctx, authCtx.ActiveOrganizationID)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "checking if logs enabled")
	}

	if limit < 1 || limit > 1000 {
		return nil, oops.E(oops.CodeBadRequest, nil, "limit must be between 1 and 1000")
	}

	sortOrder := "desc"
	if sort != "" && sort != "desc" && sort != "asc" {
		return nil, oops.E(oops.CodeBadRequest, nil, "sort order must be one of 'asc' or 'desc'")
	}
	if sort != "" {
		sortOrder = sort
	}

	cursorVal := ""
	if cursor != nil {
		cursorVal = *cursor
	}

	timeStart, timeEnd, err := parseTimeRange(from, to)
	if err != nil {
		return nil, err
	}

	return &searchParams{
		projectID: authCtx.ProjectID.String(),
		enabled:   logsEnabled,
		limit:     limit,
		sortOrder: sortOrder,
		cursor:    cursorVal,
		timeStart: timeStart,
		timeEnd:   timeEnd,
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

// resolveGramURNs handles backwards compatibility between gram_urn (single) and gram_urns (array).
// If gram_urns is provided, it takes precedence; otherwise gram_urn is used as a single-element array.
func resolveGramURNs(gramURN *string, gramURNs []string) []string {
	if len(gramURNs) > 0 {
		return gramURNs
	}
	if gramURN != nil && *gramURN != "" {
		return []string{*gramURN}
	}
	return nil
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

// sanitizeFloat64 returns 0 for NaN or Inf values which cannot be JSON-encoded.
func sanitizeFloat64(v float64) float64 {
	if math.IsNaN(v) || math.IsInf(v, 0) {
		return 0
	}
	return v
}

// CaptureEvent captures a telemetry event and forwards it to PostHog.
func (s *Service) CaptureEvent(ctx context.Context, payload *telem_gen.CaptureEventPayload) (res *telem_gen.CaptureEventResult, err error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	// Use provided distinct_id or default to organization ID
	distinctID := authCtx.ActiveOrganizationID
	if payload.DistinctID != nil && *payload.DistinctID != "" {
		distinctID = *payload.DistinctID
	}

	// Build event properties
	properties := make(map[string]interface{})
	if payload.Properties != nil {
		properties = payload.Properties
	}

	if authCtx.Email != nil {
		properties["email"] = *authCtx.Email
	}
	if authCtx.ProjectSlug != nil {
		properties["project_slug"] = *authCtx.ProjectSlug
	}
	properties["organization_slug"] = authCtx.OrganizationSlug
	properties["user_id"] = authCtx.UserID
	properties["external_user_id"] = authCtx.ExternalUserID

	// Capture event in PostHog
	if err := s.posthog.CaptureEvent(ctx, payload.Event, distinctID, properties); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "failed to capture event").
			Log(ctx, s.logger,
				attr.SlogEvent(payload.Event),
			)
	}

	s.logger.DebugContext(ctx, "captured telemetry event",
		attr.SlogEvent(payload.Event),
	)

	return &telem_gen.CaptureEventResult{
		Success: true,
	}, nil
}
