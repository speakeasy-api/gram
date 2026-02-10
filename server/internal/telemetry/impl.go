package telemetry

import (
	"context"
	"encoding/json"
	"log/slog"
	"math"
	"strconv"
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

// NewService creates a telemetry service.
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

	// The sessions and chatSessions parameters may be nil for callers that only need
	// telemetry emission (e.g., Temporal workers using CreateLog). When nil, the HTTP
	// API auth methods (APIKeyAuth, JWTAuth) will return unauthorized errors.
	var a *auth.Auth
	if sessions != nil {
		a = auth.New(logger, db, sessions)
	}

	return &Service{
		auth:         a,
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
	if s.auth == nil {
		return ctx, oops.E(oops.CodeUnauthorized, nil, "auth not configured")
	}
	return s.auth.Authorize(ctx, key, schema)
}

func (s *Service) JWTAuth(ctx context.Context, token string, schema *security.JWTScheme) (context.Context, error) {
	if s.chatSessions == nil {
		return ctx, oops.E(oops.CodeUnauthorized, nil, "chat sessions not configured")
	}
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
		return nil, oops.E(oops.CodeNotFound, nil, logsDisabledMsg)
	}

	// Extract SearchLogs-specific filter fields
	var traceID, deploymentID, functionID, severityText, httpRoute, httpMethod, serviceName, gramChatID, userID, externalUserID string
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
		userID = conv.PtrValOr(payload.Filter.UserID, "")
		externalUserID = conv.PtrValOr(payload.Filter.ExternalUserID, "")
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
		UserID:                 userID,
		ExternalUserID:         externalUserID,
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
		return nil, oops.E(oops.CodeNotFound, nil, logsDisabledMsg)
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
		return nil, oops.E(oops.CodeNotFound, nil, logsDisabledMsg)
	}

	var deploymentID, gramURN, userID, externalUserID string
	if payload.Filter != nil {
		deploymentID = conv.PtrValOr(payload.Filter.DeploymentID, "")
		gramURN = conv.PtrValOr(payload.Filter.GramUrn, "")
		userID = conv.PtrValOr(payload.Filter.UserID, "")
		externalUserID = conv.PtrValOr(payload.Filter.ExternalUserID, "")
	}

	items, err := s.chRepo.ListChats(ctx, repo.ListChatsParams{
		GramProjectID:    params.projectID,
		TimeStart:        params.timeStart,
		TimeEnd:          params.timeEnd,
		GramDeploymentID: deploymentID,
		GramURN:          gramURN,
		UserID:           userID,
		ExternalUserID:   externalUserID,
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

// SearchUsers retrieves user usage summaries grouped by user_id or external_user_id.
func (s *Service) SearchUsers(ctx context.Context, payload *telem_gen.SearchUsersPayload) (res *telem_gen.SearchUsersResult, err error) {
	params, err := s.prepareTelemetrySearch(ctx, payload.Limit, payload.Sort, payload.Cursor, &payload.Filter.From, &payload.Filter.To)
	if err != nil {
		return nil, err
	}

	if !params.enabled {
		return nil, oops.E(oops.CodeNotFound, nil, logsDisabledMsg)
	}

	deploymentID := conv.PtrValOr(payload.Filter.DeploymentID, "")

	groupBy := "user_id"
	if payload.UserType == "external" {
		groupBy = "external_user_id"
	}

	items, err := s.chRepo.SearchUsers(ctx, repo.SearchUsersParams{
		GramProjectID:    params.projectID,
		TimeStart:        params.timeStart,
		TimeEnd:          params.timeEnd,
		GramDeploymentID: deploymentID,
		GroupBy:          groupBy,
		SortOrder:        params.sortOrder,
		Cursor:           params.cursor,
		Limit:            params.limit + 1,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error searching users")
	}

	var nextCursor *string
	if len(items) > params.limit {
		nextCursor = &items[params.limit-1].UserID
		items = items[:params.limit]
	}

	users := make([]*telem_gen.UserSummary, len(items))
	for i, item := range items {
		// Build per-tool breakdown from the 3 maps
		tools := make([]*telem_gen.ToolUsage, 0, len(item.ToolCounts))
		for urn, count := range item.ToolCounts {
			tools = append(tools, &telem_gen.ToolUsage{
				Urn:          urn,
				Count:        int64(count),                       //nolint:gosec // Bounded count
				SuccessCount: int64(item.ToolSuccessCounts[urn]), //nolint:gosec // Bounded count
				FailureCount: int64(item.ToolFailureCounts[urn]), //nolint:gosec // Bounded count
			})
		}

		//nolint:gosec // Values are bounded counts that won't overflow int64
		users[i] = &telem_gen.UserSummary{
			UserID:              item.UserID,
			FirstSeenUnixNano:   strconv.FormatInt(item.FirstSeenUnixNano, 10),
			LastSeenUnixNano:    strconv.FormatInt(item.LastSeenUnixNano, 10),
			TotalChats:          int64(item.TotalChats),
			TotalChatRequests:   int64(item.TotalChatRequests),
			TotalInputTokens:    item.TotalInputTokens,
			TotalOutputTokens:   item.TotalOutputTokens,
			TotalTokens:         item.TotalTokens,
			AvgTokensPerRequest: sanitizeFloat64(item.AvgTokensPerReq),
			TotalToolCalls:      int64(item.TotalToolCalls),
			ToolCallSuccess:     int64(item.ToolCallSuccess),
			ToolCallFailure:     int64(item.ToolCallFailure),
			Tools:               tools,
		}
	}

	return &telem_gen.SearchUsersResult{
		Users:      users,
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
		return nil, oops.E(oops.CodeNotFound, nil, logsDisabledMsg)
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
		Metrics: &telem_gen.ProjectSummary{
			FirstSeenUnixNano:       strconv.FormatInt(metrics.FirstSeenUnixNano, 10),
			LastSeenUnixNano:        strconv.FormatInt(metrics.LastSeenUnixNano, 10),
			TotalInputTokens:        metrics.TotalInputTokens,
			TotalOutputTokens:       metrics.TotalOutputTokens,
			TotalTokens:             metrics.TotalTokens,
			AvgTokensPerRequest:     sanitizeFloat64(metrics.AvgTokensPerReq),
			TotalChatRequests:       int64(metrics.TotalChatRequests),
			AvgChatDurationMs:       sanitizeFloat64(metrics.AvgChatDurationMs),
			FinishReasonStop:        int64(metrics.FinishReasonStop),
			FinishReasonToolCalls:   int64(metrics.FinishReasonToolCalls),
			TotalToolCalls:          int64(metrics.TotalToolCalls),
			ToolCallSuccess:         int64(metrics.ToolCallSuccess),
			ToolCallFailure:         int64(metrics.ToolCallFailure),
			AvgToolDurationMs:       sanitizeFloat64(metrics.AvgToolDurationMs),
			TotalChats:              int64(metrics.TotalChats),
			DistinctModels:          int64(metrics.DistinctModels),
			DistinctProviders:       int64(metrics.DistinctProviders),
			Models:                  models,
			Tools:                   tools,
			ChatResolutionSuccess:   int64(metrics.ChatResolutionSuccess),
			ChatResolutionFailure:   int64(metrics.ChatResolutionFailure),
			ChatResolutionPartial:   int64(metrics.ChatResolutionPartial),
			ChatResolutionAbandoned: int64(metrics.ChatResolutionAbandoned),
			AvgChatResolutionScore:  sanitizeFloat64(metrics.AvgChatResolutionScore),
		},
		Enabled: true,
	}
}

// GetUserMetricsSummary retrieves aggregated metrics for a specific user.
func (s *Service) GetUserMetricsSummary(ctx context.Context, payload *telem_gen.GetUserMetricsSummaryPayload) (res *telem_gen.GetUserMetricsSummaryResult, err error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	logsEnabled, err := s.logsEnabled(ctx, authCtx.ActiveOrganizationID)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "unable to check if logs are enabled")
	}

	if !logsEnabled {
		return nil, oops.E(oops.CodeNotFound, nil, logsDisabledMsg)
	}

	// Validate that exactly one of user_id or external_user_id is provided
	userID := conv.PtrValOr(payload.UserID, "")
	externalUserID := conv.PtrValOr(payload.ExternalUserID, "")
	if userID == "" && externalUserID == "" {
		return nil, oops.E(oops.CodeBadRequest, nil, "either user_id or external_user_id is required")
	}
	if userID != "" && externalUserID != "" {
		return nil, oops.E(oops.CodeBadRequest, nil, "only one of user_id or external_user_id can be provided")
	}

	timeStart, timeEnd, err := parseTimeRange(&payload.From, &payload.To)
	if err != nil {
		return nil, err
	}

	metrics, err := s.chRepo.GetUserMetricsSummary(ctx, repo.GetUserMetricsSummaryParams{
		GramProjectID:  authCtx.ProjectID.String(),
		TimeStart:      timeStart,
		TimeEnd:        timeEnd,
		UserID:         userID,
		ExternalUserID: externalUserID,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error retrieving user metrics")
	}

	// Reuse the same helper as project metrics since the response format is identical
	projectResult := buildMetricsSummaryResult(*metrics)
	return &telem_gen.GetUserMetricsSummaryResult{
		Metrics: projectResult.Metrics,
		Enabled: true,
	}, nil
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

// GetObservabilityOverview retrieves aggregated observability metrics for the overview dashboard.
func (s *Service) GetObservabilityOverview(ctx context.Context, payload *telem_gen.GetObservabilityOverviewPayload) (res *telem_gen.GetObservabilityOverviewResult, err error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	logsEnabled, err := s.logsEnabled(ctx, authCtx.ActiveOrganizationID)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "unable to check if logs are enabled")
	}

	if !logsEnabled {
		return nil, oops.E(oops.CodeNotFound, nil, logsDisabledMsg)
	}

	timeStart, timeEnd, err := parseTimeRange(&payload.From, &payload.To)
	if err != nil {
		return nil, err
	}

	projectID := authCtx.ProjectID.String()
	externalUserID := conv.PtrValOr(payload.ExternalUserID, "")
	apiKeyID := conv.PtrValOr(payload.APIKeyID, "")

	// Use user-provided interval if specified, otherwise auto-calculate
	var intervalSeconds int64
	if payload.IntervalSeconds != nil && *payload.IntervalSeconds > 0 {
		intervalSeconds = *payload.IntervalSeconds
	} else {
		intervalSeconds = calculateInterval(timeStart, timeEnd)
	}

	// Calculate comparison period (same duration, immediately before)
	duration := timeEnd - timeStart
	comparisonStart := timeStart - duration
	comparisonEnd := timeStart

	// Fetch all data sequentially to avoid ClickHouse concurrent query limits
	summary, err := s.chRepo.GetOverviewSummary(ctx, repo.GetOverviewSummaryParams{
		GramProjectID:  projectID,
		TimeStart:      timeStart,
		TimeEnd:        timeEnd,
		ExternalUserID: externalUserID,
		APIKeyID:       apiKeyID,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error retrieving overview summary")
	}

	comparison, err := s.chRepo.GetOverviewSummary(ctx, repo.GetOverviewSummaryParams{
		GramProjectID:  projectID,
		TimeStart:      comparisonStart,
		TimeEnd:        comparisonEnd,
		ExternalUserID: externalUserID,
		APIKeyID:       apiKeyID,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error retrieving comparison summary")
	}

	var timeSeries []repo.TimeSeriesBucket
	if payload.IncludeTimeSeries {
		timeSeries, err = s.chRepo.GetTimeSeriesMetrics(ctx, repo.GetTimeSeriesMetricsParams{
			GramProjectID:   projectID,
			TimeStart:       timeStart,
			TimeEnd:         timeEnd,
			IntervalSeconds: intervalSeconds,
			ExternalUserID:  externalUserID,
			APIKeyID:        apiKeyID,
		})
		if err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "error retrieving time series")
		}
	}

	toolsByCount, err := s.chRepo.GetToolMetricsBreakdown(ctx, repo.GetToolMetricsBreakdownParams{
		GramProjectID:  projectID,
		TimeStart:      timeStart,
		TimeEnd:        timeEnd,
		ExternalUserID: externalUserID,
		APIKeyID:       apiKeyID,
		Limit:          10,
		SortBy:         "count",
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error retrieving tools by count")
	}

	toolsByFailure, err := s.chRepo.GetToolMetricsBreakdown(ctx, repo.GetToolMetricsBreakdownParams{
		GramProjectID:  projectID,
		TimeStart:      timeStart,
		TimeEnd:        timeEnd,
		ExternalUserID: externalUserID,
		APIKeyID:       apiKeyID,
		Limit:          10,
		SortBy:         "failure_rate",
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error retrieving tools by failure rate")
	}

	// Convert to API types
	return &telem_gen.GetObservabilityOverviewResult{
		Summary:               toObservabilitySummary(summary),
		Comparison:            toObservabilitySummary(comparison),
		TimeSeries:            toTimeSeriesBuckets(timeSeries),
		TopToolsByCount:       toToolMetrics(toolsByCount),
		TopToolsByFailureRate: toToolMetrics(toolsByFailure),
		IntervalSeconds:       intervalSeconds,
		Enabled:               true,
	}, nil
}

// calculateInterval determines the appropriate time bucket interval based on the time range.
// Returns interval in seconds.
func calculateInterval(timeStart, timeEnd int64) int64 {
	durationNanos := timeEnd - timeStart
	durationHours := durationNanos / (int64(time.Hour))

	switch {
	case durationHours <= 1:
		return 60 // 1 minute buckets
	case durationHours <= 24:
		return 900 // 15 minute buckets
	case durationHours <= 168: // 7 days
		return 3600 // 1 hour buckets
	case durationHours <= 720: // 30 days
		return 21600 // 6 hour buckets
	default:
		return 86400 // 1 day buckets for 90+ days
	}
}

// toObservabilitySummary converts repo summary to API type.
func toObservabilitySummary(summary *repo.OverviewSummary) *telem_gen.ObservabilitySummary {
	if summary == nil {
		return &telem_gen.ObservabilitySummary{
			TotalChats:           0,
			ResolvedChats:        0,
			FailedChats:          0,
			AvgSessionDurationMs: 0,
			AvgResolutionTimeMs:  0,
			TotalToolCalls:       0,
			FailedToolCalls:      0,
			AvgLatencyMs:         0,
		}
	}
	//nolint:gosec // Values are bounded counts that won't overflow int64
	return &telem_gen.ObservabilitySummary{
		TotalChats:           int64(summary.TotalChats),
		ResolvedChats:        int64(summary.ResolvedChats),
		FailedChats:          int64(summary.FailedChats),
		AvgSessionDurationMs: sanitizeFloat64(summary.AvgSessionDurationMs),
		AvgResolutionTimeMs:  sanitizeFloat64(summary.AvgResolutionTimeMs),
		TotalToolCalls:       int64(summary.TotalToolCalls),
		FailedToolCalls:      int64(summary.FailedToolCalls),
		AvgLatencyMs:         sanitizeFloat64(summary.AvgLatencyMs),
	}
}

// toTimeSeriesBuckets converts repo buckets to API type.
func toTimeSeriesBuckets(buckets []repo.TimeSeriesBucket) []*telem_gen.TimeSeriesBucket {
	if buckets == nil {
		return []*telem_gen.TimeSeriesBucket{}
	}
	result := make([]*telem_gen.TimeSeriesBucket, len(buckets))
	for i, b := range buckets {
		//nolint:gosec // Values are bounded counts that won't overflow int64
		result[i] = &telem_gen.TimeSeriesBucket{
			BucketTimeUnixNano:   strconv.FormatInt(b.BucketTimeUnixNano, 10),
			TotalChats:           int64(b.TotalChats),
			ResolvedChats:        int64(b.ResolvedChats),
			FailedChats:          int64(b.FailedChats),
			PartialChats:         int64(b.PartialChats),
			AbandonedChats:       int64(b.AbandonedChats),
			TotalToolCalls:       int64(b.TotalToolCalls),
			FailedToolCalls:      int64(b.FailedToolCalls),
			AvgToolLatencyMs:     sanitizeFloat64(b.AvgToolLatencyMs),
			AvgSessionDurationMs: sanitizeFloat64(b.AvgSessionDurationMs),
		}
	}
	return result
}

// toToolMetrics converts repo tool metrics to API type.
func toToolMetrics(tools []repo.ToolMetric) []*telem_gen.ToolMetric {
	if tools == nil {
		return []*telem_gen.ToolMetric{}
	}
	result := make([]*telem_gen.ToolMetric, len(tools))
	for i, t := range tools {
		//nolint:gosec // Values are bounded counts that won't overflow int64
		result[i] = &telem_gen.ToolMetric{
			GramUrn:      t.GramURN,
			CallCount:    int64(t.CallCount),
			SuccessCount: int64(t.SuccessCount),
			FailureCount: int64(t.FailureCount),
			AvgLatencyMs: sanitizeFloat64(t.AvgLatencyMs),
			FailureRate:  sanitizeFloat64(t.FailureRate),
		}
	}
	return result
}

// ListFilterOptions retrieves available filter options (API keys or users) for the observability dashboard.
func (s *Service) ListFilterOptions(ctx context.Context, payload *telem_gen.ListFilterOptionsPayload) (res *telem_gen.ListFilterOptionsResult, err error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	logsEnabled, err := s.logsEnabled(ctx, authCtx.ActiveOrganizationID)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "unable to check if logs are enabled")
	}

	if !logsEnabled {
		return nil, oops.E(oops.CodeNotFound, nil, logsDisabledMsg)
	}

	timeStart, timeEnd, err := parseTimeRange(&payload.From, &payload.To)
	if err != nil {
		return nil, err
	}

	options, err := s.chRepo.ListFilterOptions(ctx, repo.ListFilterOptionsParams{
		GramProjectID: authCtx.ProjectID.String(),
		TimeStart:     timeStart,
		TimeEnd:       timeEnd,
		FilterType:    payload.FilterType,
		Limit:         100,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error listing filter options")
	}

	// Convert to API types
	result := make([]*telem_gen.FilterOption, len(options))
	for i, opt := range options {
		//nolint:gosec // Values are bounded counts that won't overflow int64
		result[i] = &telem_gen.FilterOption{
			ID:    opt.ID,
			Label: opt.Label,
			Count: int64(opt.Count),
		}
	}

	return &telem_gen.ListFilterOptionsResult{
		Options: result,
		Enabled: true,
	}, nil
}
