package telemetry

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	telem_srv "github.com/speakeasy-api/gram/server/gen/http/telemetry/server"
	telem_gen "github.com/speakeasy-api/gram/server/gen/telemetry"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/auth"
	"github.com/speakeasy-api/gram/server/internal/auth/chatsessions"
	"github.com/speakeasy-api/gram/server/internal/auth/sessions"
	"github.com/speakeasy-api/gram/server/internal/authz"
	chatRepo "github.com/speakeasy-api/gram/server/internal/chat/repo"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	hooksRepo "github.com/speakeasy-api/gram/server/internal/hooks/repo"
	"github.com/speakeasy-api/gram/server/internal/middleware"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/telemetry/repo"
	"go.opentelemetry.io/otel/trace"
	goahttp "goa.design/goa/v3/http"
	"goa.design/goa/v3/security"
	"golang.org/x/sync/errgroup"
)

const logsDisabledMsg = "logs are not enabled for this organization"

type Service struct {
	auth                  *auth.Auth
	db                    *pgxpool.Pool
	chatRepo              *chatRepo.Queries
	hooksRepo             *hooksRepo.Queries
	chConn                clickhouse.Conn
	chRepo                *repo.Queries
	logger                *slog.Logger
	tracer                trace.Tracer
	posthog               PosthogClient
	chatSessions          *chatsessions.Manager
	logsEnabled           FeatureChecker
	sessionCaptureEnabled FeatureChecker
	authz                 *authz.Engine
}

var _ telem_gen.Service = (*Service)(nil)
var _ telem_gen.Auther = (*Service)(nil)

// NewService creates a telemetry service.
func NewService(
	logger *slog.Logger,
	tracerProvider trace.TracerProvider,
	db *pgxpool.Pool,
	chConn clickhouse.Conn,
	sessions *sessions.Manager,
	chatSessions *chatsessions.Manager,
	logsEnabled FeatureChecker,
	sessionCaptureEnabled FeatureChecker,
	posthogClient PosthogClient,
	authzEngine *authz.Engine,
) *Service {
	logger = logger.With(attr.SlogComponent("telemetry"))
	chRepo := repo.New(chConn)

	// The sessions and chatSessions parameters may be nil for callers that only need
	// telemetry emission (e.g., Temporal workers using CreateLog). When nil, the HTTP
	// API auth methods (APIKeyAuth, JWTAuth) will return unauthorized errors.
	var a *auth.Auth
	if sessions != nil {
		a = auth.New(logger, db, sessions, authzEngine)
	}

	return &Service{
		auth:                  a,
		db:                    db,
		chatRepo:              chatRepo.New(db),
		hooksRepo:             hooksRepo.New(db),
		chConn:                chConn,
		chRepo:                chRepo,
		logger:                logger,
		logsEnabled:           logsEnabled,
		sessionCaptureEnabled: sessionCaptureEnabled,
		tracer:                tracerProvider.Tracer("github.com/speakeasy-api/gram/server/internal/telemetry"),
		posthog:               posthogClient,
		chatSessions:          chatSessions,
		authz:                 authzEngine,
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

// CheckLogsEnabled returns whether logs are enabled for the given organization.
// Returns an error suitable for returning from API endpoints if logs are disabled.
func (s *Service) CheckLogsEnabled(ctx context.Context, organizationID string) error {
	enabled, err := s.logsEnabled(ctx, organizationID)
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "unable to check if logs are enabled")
	}
	if !enabled {
		return oops.E(oops.CodeNotFound, nil, logsDisabledMsg)
	}
	return nil
}

// SearchLogs retrieves unified telemetry logs with pagination.
func (s *Service) SearchLogs(ctx context.Context, payload *telem_gen.SearchLogsPayload) (res *telem_gen.SearchLogsResult, err error) {
	// Prefer top-level from/to; fall back to deprecated filter fields.
	from := payload.From
	to := payload.To
	if from == nil && payload.Filter != nil {
		from = payload.Filter.From
	}
	if to == nil && payload.Filter != nil {
		to = payload.Filter.To
	}

	params, err := s.prepareTelemetrySearch(ctx, payload.Limit, payload.Sort, payload.Cursor, from, to)
	if err != nil {
		return nil, err
	}

	// Extract SearchLogs-specific filter fields (deprecated filter path).
	var traceID, deploymentID, functionID, severityText, httpRoute, httpMethod, serviceName, gramChatID, userID, externalUserID, eventSource string
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
		eventSource = conv.PtrValOr(payload.Filter.EventSource, "")
	}

	// New top-level filters supersede filter.attribute_filters.
	attributeFilters := toRepoAttributeFilters(payload.Filters)

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
		EventSource:            eventSource,
		AttributeFilters:       attributeFilters,
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
		nextCursor = new(items[params.limit-1].ID)
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

	// Extract SearchToolCalls-specific filter fields
	var deploymentID, functionID, gramURN, eventSource string
	if payload.Filter != nil {
		deploymentID = conv.PtrValOr(payload.Filter.DeploymentID, "")
		functionID = conv.PtrValOr(payload.Filter.FunctionID, "")
		gramURN = conv.PtrValOr(payload.Filter.GramUrn, "")
		eventSource = conv.PtrValOr(payload.Filter.EventSource, "")
	}

	// Query with limit+1 to detect if there are more results
	items, err := s.chRepo.ListToolTraces(ctx, repo.ListToolTracesParams{
		GramProjectID:    params.projectID,
		TimeStart:        params.timeStart,
		TimeEnd:          params.timeEnd,
		GramDeploymentID: deploymentID,
		GramFunctionID:   functionID,
		GramURN:          gramURN,
		EventSource:      eventSource,
		SortOrder:        params.sortOrder,
		Cursor:           params.cursor,
		Limit:            params.limit + 1,
	})
	if err != nil {
		s.logger.ErrorContext(ctx, "error listing tool traces", attr.SlogError(err))
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
			StartTimeUnixNano: strconv.FormatInt(item.StartTimeUnixNano, 10),
			LogCount:          item.LogCount,
			HTTPStatusCode:    item.HTTPStatusCode,
			GramUrn:           item.GramURN,
			ToolName:          item.ToolName,
			ToolSource:        item.ToolSource,
			EventSource:       item.EventSource,
		}
	}

	return &telem_gen.SearchToolCallsResult{
		ToolCalls:  toolCalls,
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
			StartTimeUnixNano: strconv.FormatInt(item.StartTimeUnixNano, 10),
			EndTimeUnixNano:   strconv.FormatInt(item.EndTimeUnixNano, 10),
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
		NextCursor: nextCursor,
	}, nil
}

// SearchUsers retrieves user usage summaries grouped by user_id or external_user_id.
func (s *Service) SearchUsers(ctx context.Context, payload *telem_gen.SearchUsersPayload) (res *telem_gen.SearchUsersResult, err error) {
	params, err := s.prepareTelemetrySearch(ctx, payload.Limit, payload.Sort, payload.Cursor, &payload.Filter.From, &payload.Filter.To)
	if err != nil {
		return nil, err
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
			UserID:                   item.UserID,
			FirstSeenUnixNano:        strconv.FormatInt(item.FirstSeenUnixNano, 10),
			LastSeenUnixNano:         strconv.FormatInt(item.LastSeenUnixNano, 10),
			TotalChats:               int64(item.TotalChats),
			TotalChatRequests:        int64(item.TotalChatRequests),
			TotalInputTokens:         item.TotalInputTokens,
			TotalOutputTokens:        item.TotalOutputTokens,
			TotalTokens:              item.TotalTokens,
			CacheReadInputTokens:     item.CacheReadInputTokens,
			CacheCreationInputTokens: item.CacheCreationInputTokens,
			AvgTokensPerRequest:      sanitizeFloat64(item.AvgTokensPerReq),
			TotalCost:                sanitizeFloat64(item.TotalCost),
			TotalToolCalls:           int64(item.TotalToolCalls),
			ToolCallSuccess:          int64(item.ToolCallSuccess),
			ToolCallFailure:          int64(item.ToolCallFailure),
			Tools:                    tools,
		}
	}

	return &telem_gen.SearchUsersResult{
		Users:      users,
		NextCursor: nextCursor,
	}, nil
}

// GetProjectMetricsSummary retrieves aggregated metrics for an entire project.
func (s *Service) GetProjectMetricsSummary(ctx context.Context, payload *telem_gen.GetProjectMetricsSummaryPayload) (res *telem_gen.GetMetricsSummaryResult, err error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeProjectRead, ResourceID: authCtx.ProjectID.String()}); err != nil {
		return nil, err
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
			FirstSeenUnixNano:        strconv.FormatInt(metrics.FirstSeenUnixNano, 10),
			LastSeenUnixNano:         strconv.FormatInt(metrics.LastSeenUnixNano, 10),
			TotalInputTokens:         metrics.TotalInputTokens,
			TotalOutputTokens:        metrics.TotalOutputTokens,
			TotalTokens:              metrics.TotalTokens,
			CacheReadInputTokens:     metrics.CacheReadInputTokens,
			CacheCreationInputTokens: metrics.CacheCreationInputTokens,
			AvgTokensPerRequest:      sanitizeFloat64(metrics.AvgTokensPerReq),
			TotalCost:                sanitizeFloat64(metrics.TotalCost),
			TotalChatRequests:        int64(metrics.TotalChatRequests),
			AvgChatDurationMs:        sanitizeFloat64(metrics.AvgChatDurationMs),
			FinishReasonStop:         int64(metrics.FinishReasonStop),
			FinishReasonToolCalls:    int64(metrics.FinishReasonToolCalls),
			TotalToolCalls:           int64(metrics.TotalToolCalls),
			ToolCallSuccess:          int64(metrics.ToolCallSuccess),
			ToolCallFailure:          int64(metrics.ToolCallFailure),
			AvgToolDurationMs:        sanitizeFloat64(metrics.AvgToolDurationMs),
			TotalChats:               int64(metrics.TotalChats),
			DistinctModels:           int64(metrics.DistinctModels),
			DistinctProviders:        int64(metrics.DistinctProviders),
			Models:                   models,
			Tools:                    tools,
			ChatResolutionSuccess:    int64(metrics.ChatResolutionSuccess),
			ChatResolutionFailure:    int64(metrics.ChatResolutionFailure),
			ChatResolutionPartial:    int64(metrics.ChatResolutionPartial),
			ChatResolutionAbandoned:  int64(metrics.ChatResolutionAbandoned),
			AvgChatResolutionScore:   sanitizeFloat64(metrics.AvgChatResolutionScore),
		},
	}
}

// GetUserMetricsSummary retrieves aggregated metrics for a specific user.
func (s *Service) GetUserMetricsSummary(ctx context.Context, payload *telem_gen.GetUserMetricsSummaryPayload) (res *telem_gen.GetUserMetricsSummaryResult, err error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeProjectRead, ResourceID: authCtx.ProjectID.String()}); err != nil {
		return nil, err
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
	}, nil
}

// searchParams contains common validated parameters for telemetry search endpoints.
type searchParams struct {
	projectID      string
	organizationID string
	limit          int
	sortOrder      string
	cursor         string
	timeStart      int64
	timeEnd        int64
}

// prepareTelemetrySearch validates and prepares common search parameters.
func (s *Service) prepareTelemetrySearch(ctx context.Context, limit int, sort string, cursor *string, from, to *string) (*searchParams, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeProjectRead, ResourceID: authCtx.ProjectID.String()}); err != nil {
		return nil, err
	}

	logsEnabled, err := s.logsEnabled(ctx, authCtx.ActiveOrganizationID)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "checking if logs enabled")
	}
	if !logsEnabled {
		return nil, oops.E(oops.CodeNotFound, nil, logsDisabledMsg)
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
		projectID:      authCtx.ProjectID.String(),
		organizationID: authCtx.ActiveOrganizationID,
		limit:          limit,
		sortOrder:      sortOrder,
		cursor:         cursorVal,
		timeStart:      timeStart,
		timeEnd:        timeEnd,
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

	// Validate that from < to to prevent unsigned integer overflow in ClickHouse queries
	if timeStart >= timeEnd {
		return 0, 0, oops.E(oops.CodeBadRequest, nil, "'from' time must be before 'to' time")
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
		TimeUnixNano:         strconv.FormatInt(log.TimeUnixNano, 10),
		ObservedTimeUnixNano: strconv.FormatInt(log.ObservedTimeUnixNano, 10),
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

// toRepoAttributeFilters converts generated Goa log filters to repo types.
func toRepoAttributeFilters(filters []*telem_gen.LogFilter) []repo.AttributeFilter {
	if len(filters) == 0 {
		return nil
	}
	result := make([]repo.AttributeFilter, 0, len(filters))
	for _, f := range filters {
		if f == nil {
			continue
		}
		result = append(result, repo.AttributeFilter{
			Path:   f.Path,
			Op:     f.Operator,
			Values: f.Values,
		})
	}
	return result
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
	properties := make(map[string]any)
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

	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeProjectRead, ResourceID: authCtx.ProjectID.String()}); err != nil {
		return nil, err
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
	toolsetSlug := conv.PtrValOr(payload.ToolsetSlug, "")

	// Auto-calculate interval based on time range
	intervalSeconds := calculateInterval(timeStart, timeEnd)

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
		ToolsetSlug:    toolsetSlug,
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
		ToolsetSlug:    toolsetSlug,
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
			ToolsetSlug:     toolsetSlug,
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
		ToolsetSlug:    toolsetSlug,
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
		ToolsetSlug:    toolsetSlug,
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
	}, nil
}

// GetProjectOverview retrieves project-level overview metrics including total chats, tool calls,
// active servers/users, and top lists. This endpoint does not support filtering.
func (s *Service) GetProjectOverview(ctx context.Context, payload *telem_gen.GetProjectOverviewPayload) (res *telem_gen.GetProjectOverviewResult, err error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeProjectRead, ResourceID: authCtx.ProjectID.String()}); err != nil {
		return nil, err
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

	// Calculate comparison period (same duration as current period, ending at current period start)
	duration := timeEnd - timeStart
	comparisonStart := timeStart - duration
	comparisonEnd := timeStart

	// Convert timestamps for PostgreSQL queries
	timeStartPG := pgtype.Timestamptz{Time: time.Unix(0, timeStart), Valid: true, InfinityModifier: pgtype.Finite}
	timeEndPG := pgtype.Timestamptz{Time: time.Unix(0, timeEnd), Valid: true, InfinityModifier: pgtype.Finite}
	comparisonStartPG := pgtype.Timestamptz{Time: time.Unix(0, comparisonStart), Valid: true, InfinityModifier: pgtype.Finite}
	comparisonEndPG := pgtype.Timestamptz{Time: time.Unix(0, comparisonEnd), Valid: true, InfinityModifier: pgtype.Finite}

	// Determine metrics mode: Check if session capture is enabled
	sessionCaptureEnabled, err := s.sessionCaptureEnabled(ctx, authCtx.ActiveOrganizationID)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "unable to check if session capture is enabled")
	}

	sessionMode := sessionCaptureEnabled
	metricsMode := "tool_call"
	if sessionMode {
		metricsMode = "session"
	}

	// Fetch chat metrics from PostgreSQL for current period
	chatMetrics, err := s.chatRepo.GetChatMetricsSummary(ctx, chatRepo.GetChatMetricsSummaryParams{
		ProjectID: *authCtx.ProjectID,
		TimeStart: timeStartPG,
		TimeEnd:   timeEndPG,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error retrieving chat metrics summary")
	}

	// Fetch tool call metrics from ClickHouse for current period (no filters)
	toolMetrics, err := s.chRepo.GetOverviewSummary(ctx, repo.GetOverviewSummaryParams{
		GramProjectID:  projectID,
		TimeStart:      timeStart,
		TimeEnd:        timeEnd,
		ExternalUserID: "",
		APIKeyID:       "",
		ToolsetSlug:    "",
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error retrieving tool call metrics")
	}

	// Fetch comparison period metrics from PostgreSQL
	chatMetricsComparison, err := s.chatRepo.GetChatMetricsSummary(ctx, chatRepo.GetChatMetricsSummaryParams{
		ProjectID: *authCtx.ProjectID,
		TimeStart: comparisonStartPG,
		TimeEnd:   comparisonEndPG,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error retrieving comparison chat metrics")
	}

	// Fetch comparison period tool metrics from ClickHouse
	toolMetricsComparison, err := s.chRepo.GetOverviewSummary(ctx, repo.GetOverviewSummaryParams{
		GramProjectID:  projectID,
		TimeStart:      comparisonStart,
		TimeEnd:        comparisonEnd,
		ExternalUserID: "",
		APIKeyID:       "",
		ToolsetSlug:    "",
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error retrieving comparison tool call metrics")
	}

	// Get active counts and user/session data based on metrics mode
	var activeServersCount int64
	var activeUsersCount int64
	var topUsers []*telem_gen.TopUser
	var llmClientBreakdown []*telem_gen.LLMClientUsage

	// Active servers count - always from ClickHouse hooks data
	activeServersRaw, err := s.chRepo.GetActiveCounts(ctx, repo.GetActiveCountsParams{
		GramProjectID:  projectID,
		TimeStart:      timeStart,
		TimeEnd:        timeEnd,
		ExternalUserID: "",
		APIKeyID:       "",
		ToolsetSlug:    "",
		SessionMode:    sessionMode,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error retrieving active server counts")
	}
	activeServersCount = int64(activeServersRaw.ActiveServersCount) //nolint:gosec // Bounded count that won't overflow int64

	if sessionMode {
		// Use PostgreSQL for session-based metrics
		activeUsersCount, err = s.chatRepo.GetActiveUserCountByMessages(ctx, chatRepo.GetActiveUserCountByMessagesParams{
			ProjectID: *authCtx.ProjectID,
			TimeStart: timeStartPG,
			TimeEnd:   timeEndPG,
		})
		if err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "error retrieving active user count from PG")
		}

		topUsersPG, err := s.chatRepo.GetTopUsersByMessages(ctx, chatRepo.GetTopUsersByMessagesParams{
			ProjectID:   *authCtx.ProjectID,
			TimeStart:   timeStartPG,
			TimeEnd:     timeEndPG,
			ResultLimit: 10,
		})
		if err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "error retrieving top users from PG")
		}
		topUsers = toTopUsersFromPG(topUsersPG)

		llmClientsPG, err := s.chatRepo.GetLLMClientBreakdownByMessages(ctx, chatRepo.GetLLMClientBreakdownByMessagesParams{
			ProjectID: *authCtx.ProjectID,
			TimeStart: timeStartPG,
			TimeEnd:   timeEndPG,
		})
		if err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "error retrieving LLM client breakdown from PG")
		}
		llmClientBreakdown = toLLMClientUsageFromPG(llmClientsPG)
	} else {
		// Use ClickHouse for tool-call-based metrics
		activeUsersCount = int64(activeServersRaw.ActiveUsersCount) //nolint:gosec // Bounded count that won't overflow int64

		topUsersCH, err := s.chRepo.GetTopUsers(ctx, repo.GetTopUsersParams{
			GramProjectID:  projectID,
			TimeStart:      timeStart,
			TimeEnd:        timeEnd,
			ExternalUserID: "",
			APIKeyID:       "",
			ToolsetSlug:    "",
			Limit:          10,
			SessionMode:    false,
		})
		if err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "error retrieving top users from CH")
		}
		topUsers = toTopUsers(topUsersCH)

		llmClientsCH, err := s.chRepo.GetLLMClientBreakdown(ctx, repo.GetLLMClientBreakdownParams{
			GramProjectID:  projectID,
			TimeStart:      timeStart,
			TimeEnd:        timeEnd,
			ExternalUserID: "",
			APIKeyID:       "",
			ToolsetSlug:    "",
			SessionMode:    false,
		})
		if err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "error retrieving LLM client breakdown from CH")
		}
		llmClientBreakdown = toLLMClientUsage(llmClientsCH)
	}

	// Get top servers - always from ClickHouse hooks data
	topServers, err := s.chRepo.GetTopServers(ctx, repo.GetTopServersParams{
		GramProjectID:  projectID,
		TimeStart:      timeStart,
		TimeEnd:        timeEnd,
		ExternalUserID: "",
		APIKeyID:       "",
		ToolsetSlug:    "",
		Limit:          10,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error retrieving top servers")
	}

	// Get server name overrides from PostgreSQL
	serverNameOverrides, err := s.hooksRepo.ListHooksServerNameOverrides(ctx, *authCtx.ProjectID)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error retrieving server name overrides")
	}

	// Build a map for quick lookup: raw_server_name -> display_name
	overrideMap := make(map[string]string, len(serverNameOverrides))
	for _, override := range serverNameOverrides {
		overrideMap[override.RawServerName] = override.DisplayName
	}

	// Apply overrides to top servers
	topServersWithOverrides := applyServerNameOverrides(topServers, overrideMap)

	// Convert to API types - build summaries with nested fields
	return &telem_gen.GetProjectOverviewResult{
		Summary: buildProjectOverviewSummary(
			chatMetrics,
			toolMetrics,
			activeServersCount,
			activeUsersCount,
			topUsers,
			toTopServers(topServersWithOverrides),
			llmClientBreakdown,
		),
		Comparison: buildProjectOverviewSummary(
			chatMetricsComparison,
			toolMetricsComparison,
			0, // Don't need active counts for comparison
			0,
			nil, // Don't need top lists for comparison
			nil,
			nil,
		),
		MetricsMode: metricsMode,
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

// buildObservabilitySummary builds an ObservabilitySummary from multiple data sources.
// Chat metrics come from PostgreSQL, tool metrics from ClickHouse, and additional data from both.
// toObservabilitySummary converts repo summary to API type.
func toObservabilitySummary(summary *repo.OverviewSummary) *telem_gen.ObservabilitySummary {
	if summary == nil {
		return &telem_gen.ObservabilitySummary{
			TotalChats:               0,
			ResolvedChats:            0,
			FailedChats:              0,
			AvgSessionDurationMs:     0,
			AvgResolutionTimeMs:      0,
			TotalInputTokens:         0,
			TotalOutputTokens:        0,
			TotalTokens:              0,
			CacheReadInputTokens:     0,
			CacheCreationInputTokens: 0,
			TotalCost:                0,
			TotalToolCalls:           0,
			FailedToolCalls:          0,
			AvgLatencyMs:             0,
		}
	}
	//nolint:gosec // Values are bounded counts that won't overflow int64
	return &telem_gen.ObservabilitySummary{
		TotalChats:               int64(summary.TotalChats),
		ResolvedChats:            int64(summary.ResolvedChats),
		FailedChats:              int64(summary.FailedChats),
		AvgSessionDurationMs:     sanitizeFloat64(summary.AvgSessionDurationMs),
		AvgResolutionTimeMs:      sanitizeFloat64(summary.AvgResolutionTimeMs),
		TotalInputTokens:         summary.TotalInputTokens,
		TotalOutputTokens:        summary.TotalOutputTokens,
		TotalTokens:              summary.TotalTokens,
		CacheReadInputTokens:     summary.CacheReadInputTokens,
		CacheCreationInputTokens: summary.CacheCreationInputTokens,
		TotalCost:                sanitizeFloat64(summary.TotalCost),
		TotalToolCalls:           int64(summary.TotalToolCalls),
		FailedToolCalls:          int64(summary.FailedToolCalls),
		AvgLatencyMs:             sanitizeFloat64(summary.AvgLatencyMs),
	}
}

// buildProjectOverviewSummary builds a ProjectOverviewSummary from multiple data sources.
// Chat metrics come from PostgreSQL, tool metrics from ClickHouse, and additional data from both.
func buildProjectOverviewSummary(
	chatMetrics chatRepo.GetChatMetricsSummaryRow,
	toolMetrics *repo.OverviewSummary,
	activeServersCount int64,
	activeUsersCount int64,
	topUsers []*telem_gen.TopUser,
	topServers []*telem_gen.TopServer,
	llmClientBreakdown []*telem_gen.LLMClientUsage,
) *telem_gen.ProjectOverviewSummary {
	// Get tool metrics from ClickHouse (or defaults if nil)
	var totalToolCalls, failedToolCalls int64
	if toolMetrics != nil {
		totalToolCalls = int64(toolMetrics.TotalToolCalls)   //nolint:gosec // Bounded count that won't overflow int64
		failedToolCalls = int64(toolMetrics.FailedToolCalls) //nolint:gosec // Bounded count that won't overflow int64
	}

	// Ensure arrays are non-nil
	if topUsers == nil {
		topUsers = []*telem_gen.TopUser{}
	}
	if topServers == nil {
		topServers = []*telem_gen.TopServer{}
	}
	if llmClientBreakdown == nil {
		llmClientBreakdown = []*telem_gen.LLMClientUsage{}
	}

	return &telem_gen.ProjectOverviewSummary{
		// Chat metrics from PostgreSQL
		TotalChats:    chatMetrics.TotalChats,
		ResolvedChats: chatMetrics.ResolvedChats,
		FailedChats:   chatMetrics.FailedChats,
		// Tool metrics from ClickHouse
		TotalToolCalls:  totalToolCalls,
		FailedToolCalls: failedToolCalls,
		// Activity counts and top lists
		ActiveServersCount: activeServersCount,
		ActiveUsersCount:   activeUsersCount,
		TopUsers:           topUsers,
		TopServers:         topServers,
		LlmClientBreakdown: llmClientBreakdown,
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
			BucketTimeUnixNano:       strconv.FormatInt(b.BucketTimeUnixNano, 10),
			TotalChats:               int64(b.TotalChats),
			ResolvedChats:            int64(b.ResolvedChats),
			FailedChats:              int64(b.FailedChats),
			PartialChats:             int64(b.PartialChats),
			AbandonedChats:           int64(b.AbandonedChats),
			TotalInputTokens:         b.TotalInputTokens,
			TotalOutputTokens:        b.TotalOutputTokens,
			TotalTokens:              b.TotalTokens,
			CacheReadInputTokens:     b.CacheReadInputTokens,
			CacheCreationInputTokens: b.CacheCreationInputTokens,
			TotalCost:                sanitizeFloat64(b.TotalCost),
			TotalToolCalls:           int64(b.TotalToolCalls),
			FailedToolCalls:          int64(b.FailedToolCalls),
			AvgToolLatencyMs:         sanitizeFloat64(b.AvgToolLatencyMs),
			AvgSessionDurationMs:     sanitizeFloat64(b.AvgSessionDurationMs),
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

// toTopUsers converts repo top users to API type.
func toTopUsers(users []repo.TopUser) []*telem_gen.TopUser {
	if users == nil {
		return []*telem_gen.TopUser{}
	}
	result := make([]*telem_gen.TopUser, len(users))
	for i, u := range users {
		//nolint:gosec // Values are bounded counts that won't overflow int64
		result[i] = &telem_gen.TopUser{
			UserID:        u.UserID,
			UserType:      u.UserType,
			ActivityCount: int64(u.ActivityCount),
		}
	}
	return result
}

// applyServerNameOverrides applies display name overrides to server names.
// Merges entries with the same display name after applying overrides and re-sorts by count.
func applyServerNameOverrides(servers []repo.TopServer, overrideMap map[string]string) []repo.TopServer {
	if len(overrideMap) == 0 {
		return servers
	}

	// Apply overrides and aggregate counts by display name
	counts := make(map[string]uint64)
	for _, s := range servers {
		displayName := s.ServerName
		if override, ok := overrideMap[s.ServerName]; ok {
			displayName = override
		}
		counts[displayName] += s.ToolCallCount
	}

	// Convert back to slice
	result := make([]repo.TopServer, 0, len(counts))
	for name, count := range counts {
		result = append(result, repo.TopServer{
			ServerName:    name,
			ToolCallCount: count,
		})
	}

	// Re-sort by ToolCallCount descending
	sort.Slice(result, func(i, j int) bool {
		return result[i].ToolCallCount > result[j].ToolCallCount
	})

	return result
}

// toTopServers converts repo top servers to API type.
func toTopServers(servers []repo.TopServer) []*telem_gen.TopServer {
	if servers == nil {
		return []*telem_gen.TopServer{}
	}
	result := make([]*telem_gen.TopServer, len(servers))
	for i, s := range servers {
		//nolint:gosec // Values are bounded counts that won't overflow int64
		result[i] = &telem_gen.TopServer{
			ServerName:    s.ServerName,
			ToolCallCount: int64(s.ToolCallCount),
		}
	}
	return result
}

// toLLMClientUsage converts repo LLM client usage to API type.
func toLLMClientUsage(clients []repo.LLMClientUsage) []*telem_gen.LLMClientUsage {
	if clients == nil {
		return []*telem_gen.LLMClientUsage{}
	}
	result := make([]*telem_gen.LLMClientUsage, len(clients))
	for i, c := range clients {
		//nolint:gosec // Values are bounded counts that won't overflow int64
		result[i] = &telem_gen.LLMClientUsage{
			ClientName:    c.ClientName,
			ActivityCount: int64(c.ActivityCount),
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

	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeProjectRead, ResourceID: authCtx.ProjectID.String()}); err != nil {
		return nil, err
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
	}, nil
}

// ListAttributeKeys retrieves distinct attribute keys from telemetry logs for the current project.
func (s *Service) ListAttributeKeys(ctx context.Context, payload *telem_gen.ListAttributeKeysPayload) (res *telem_gen.ListAttributeKeysResult, err error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeProjectRead, ResourceID: authCtx.ProjectID.String()}); err != nil {
		return nil, err
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

	rawKeys, err := s.chRepo.ListAttributeKeys(ctx, repo.ListAttributeKeysParams{
		GramProjectID: authCtx.ProjectID.String(),
		TimeStart:     timeStart,
		TimeEnd:       timeEnd,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error listing attribute keys")
	}

	// Translate raw attribute paths to display keys:
	// "app.region" → "@region", everything else stays as-is.
	keys := make([]string, 0, len(rawKeys))
	for _, k := range rawKeys {
		if after, ok := strings.CutPrefix(k, "app."); ok {
			keys = append(keys, "@"+after)
			continue
		}

		keys = append(keys, k)
	}
	sort.Strings(keys)

	return &telem_gen.ListAttributeKeysResult{
		Keys: keys,
	}, nil
}

// GetHooksSummary returns aggregated hooks metrics grouped by server
func (s *Service) GetHooksSummary(ctx context.Context, payload *telem_gen.GetHooksSummaryPayload) (res *telem_gen.GetHooksSummaryResult, err error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeProjectRead, ResourceID: authCtx.ProjectID.String()}); err != nil {
		return nil, err
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

	attributeFilters := toRepoAttributeFilters(payload.Filters)
	typesToInclude := payload.TypesToInclude

	// Compute time series bucket size: 5 min for ≤24h windows, 60 min otherwise
	const fiveMinNs = int64(5 * 60 * 1e9)
	const sixtyMinNs = int64(60 * 60 * 1e9)
	bucketSizeNs := sixtyMinNs
	if timeEnd-timeStart <= int64(24*60*60*1e9) {
		bucketSizeNs = fiveMinNs
	}

	// Run all six independent ClickHouse queries in parallel
	var (
		serverRows       []repo.HooksServerSummaryRow
		userRows         []repo.HooksUserSummaryRow
		skillRows        []repo.SkillSummaryRow
		breakdownRows    []repo.HooksBreakdownRow
		timeSeriesPoints []repo.HooksTimeSeriesPoint
		sessionCount     int64
	)
	eg, egCtx := errgroup.WithContext(ctx)
	projectID := authCtx.ProjectID.String()

	eg.Go(func() error {
		var err error
		serverRows, err = s.chRepo.GetHooksSummary(egCtx, repo.GetHooksSummaryParams{
			GramProjectID:  projectID,
			TimeStart:      timeStart,
			TimeEnd:        timeEnd,
			Filters:        attributeFilters,
			TypesToInclude: typesToInclude,
		})
		if err != nil {
			return fmt.Errorf("get hooks server summary: %w", err)
		}
		return nil
	})
	eg.Go(func() error {
		var err error
		userRows, err = s.chRepo.GetHooksUserSummary(egCtx, repo.GetHooksUserSummaryParams{
			GramProjectID:  projectID,
			TimeStart:      timeStart,
			TimeEnd:        timeEnd,
			Filters:        attributeFilters,
			TypesToInclude: typesToInclude,
		})
		if err != nil {
			return fmt.Errorf("get hooks user summary: %w", err)
		}
		return nil
	})
	eg.Go(func() error {
		var err error
		skillRows, err = s.chRepo.GetSkillsSummary(egCtx, repo.GetSkillsSummaryParams{
			GramProjectID:  projectID,
			TimeStart:      timeStart,
			TimeEnd:        timeEnd,
			Filters:        attributeFilters,
			TypesToInclude: typesToInclude,
		})
		if err != nil {
			return fmt.Errorf("get skills summary: %w", err)
		}
		return nil
	})
	eg.Go(func() error {
		var err error
		breakdownRows, err = s.chRepo.GetHooksBreakdown(egCtx, repo.GetHooksBreakdownParams{
			GramProjectID:  projectID,
			TimeStart:      timeStart,
			TimeEnd:        timeEnd,
			Filters:        attributeFilters,
			TypesToInclude: typesToInclude,
		})
		if err != nil {
			return fmt.Errorf("get hooks breakdown: %w", err)
		}
		return nil
	})
	eg.Go(func() error {
		var err error
		timeSeriesPoints, err = s.chRepo.GetHooksTimeSeries(egCtx, repo.GetHooksTimeSeriesParams{
			GramProjectID:  projectID,
			TimeStart:      timeStart,
			TimeEnd:        timeEnd,
			BucketSizeNs:   bucketSizeNs,
			Filters:        attributeFilters,
			TypesToInclude: typesToInclude,
		})
		if err != nil {
			return fmt.Errorf("get hooks time series: %w", err)
		}
		return nil
	})
	eg.Go(func() error {
		var err error
		sessionCount, err = s.chRepo.GetHooksSessionCount(egCtx, repo.GetHooksSessionCountParams{
			GramProjectID:  projectID,
			TimeStart:      timeStart,
			TimeEnd:        timeEnd,
			Filters:        attributeFilters,
			TypesToInclude: typesToInclude,
		})
		if err != nil {
			return fmt.Errorf("get hooks session count: %w", err)
		}
		return nil
	})
	if err := eg.Wait(); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error fetching hooks summary data")
	}

	// Transform server rows into response
	servers := make([]*telem_gen.HooksServerSummary, 0, len(serverRows))
	var totalEvents, totalSessions int64
	for _, row := range serverRows {
		servers = append(servers, &telem_gen.HooksServerSummary{
			ServerName:   row.ServerName,
			EventCount:   int64(row.EventCount),   //nolint:gosec // Bounded count
			UniqueTools:  int64(row.UniqueTools),  //nolint:gosec // Bounded count
			SuccessCount: int64(row.SuccessCount), //nolint:gosec // Bounded count
			FailureCount: int64(row.FailureCount), //nolint:gosec // Bounded count
			FailureRate:  row.FailureRate,
		})
		totalEvents += int64(row.EventCount) //nolint:gosec // Bounded count
	}

	// Transform user rows into response
	users := make([]*telem_gen.HooksUserSummary, 0, len(userRows))
	for _, row := range userRows {
		users = append(users, &telem_gen.HooksUserSummary{
			UserEmail:    row.UserEmail,
			EventCount:   int64(row.EventCount),   //nolint:gosec // Bounded count
			UniqueTools:  int64(row.UniqueTools),  //nolint:gosec // Bounded count
			SuccessCount: int64(row.SuccessCount), //nolint:gosec // Bounded count
			FailureCount: int64(row.FailureCount), //nolint:gosec // Bounded count
			FailureRate:  row.FailureRate,
		})
	}

	// Transform skills rows into response
	skills := make([]*telem_gen.SkillSummary, 0, len(skillRows))
	for _, row := range skillRows {
		skills = append(skills, &telem_gen.SkillSummary{
			SkillName:   row.SkillName,
			UseCount:    int64(row.UseCount),    //nolint:gosec // Bounded count
			UniqueUsers: int64(row.UniqueUsers), //nolint:gosec // Bounded count
		})
	}

	// Transform breakdown rows into response
	breakdown := make([]*telem_gen.HooksBreakdownRow, 0, len(breakdownRows))
	for _, row := range breakdownRows {
		breakdown = append(breakdown, &telem_gen.HooksBreakdownRow{
			UserEmail:    row.UserEmail,
			ServerName:   row.ServerName,
			HookSource:   row.HookSource,
			ToolName:     row.ToolName,
			EventCount:   int64(row.EventCount),   //nolint:gosec // Bounded count
			FailureCount: int64(row.FailureCount), //nolint:gosec // Bounded count
		})
	}

	// Transform time series points into response
	timeSeries := make([]*telem_gen.HooksTimeSeriesPoint, 0, len(timeSeriesPoints))
	for _, pt := range timeSeriesPoints {
		timeSeries = append(timeSeries, &telem_gen.HooksTimeSeriesPoint{
			BucketStartNs: strconv.FormatInt(pt.BucketStartNs, 10),
			ServerName:    pt.ServerName,
			UserEmail:     pt.UserEmail,
			EventCount:    int64(pt.EventCount),   //nolint:gosec // Bounded count
			FailureCount:  int64(pt.FailureCount), //nolint:gosec // Bounded count
		})
	}

	totalSessions = sessionCount

	return &telem_gen.GetHooksSummaryResult{
		Servers:       servers,
		Users:         users,
		Skills:        skills,
		TotalEvents:   totalEvents,
		TotalSessions: totalSessions,
		Breakdown:     breakdown,
		TimeSeries:    timeSeries,
	}, nil
}

// ListHooksTraces retrieves hook trace summaries with pagination and filtering.
// Uses materialized columns for efficient querying while accessing user_email from JSON.
func (s *Service) ListHooksTraces(ctx context.Context, payload *telem_gen.ListHooksTracesPayload) (res *telem_gen.ListHooksTracesResult, err error) {
	params, err := s.prepareTelemetrySearch(ctx, payload.Limit, payload.Sort, payload.Cursor, &payload.From, &payload.To)
	if err != nil {
		return nil, err
	}

	// Convert attribute filters
	attributeFilters := toRepoAttributeFilters(payload.Filters)

	// Query with limit+1 to detect if there are more results
	items, err := s.chRepo.ListHooksTraces(ctx, repo.ListHooksTracesParams{
		GramProjectID:  params.projectID,
		TimeStart:      params.timeStart,
		TimeEnd:        params.timeEnd,
		Filters:        attributeFilters,
		TypesToInclude: payload.TypesToInclude,
		SortOrder:      params.sortOrder,
		Cursor:         params.cursor,
		Limit:          params.limit + 1,
	})
	if err != nil {
		s.logger.ErrorContext(ctx, "error listing hooks traces", attr.SlogError(err))
		return nil, oops.E(oops.CodeUnexpected, err, "error listing hooks traces")
	}

	// Compute next cursor using limit+1 pattern
	var nextCursor *string
	if len(items) > params.limit {
		nextCursor = &items[params.limit-1].TraceID
		items = items[:params.limit]
	}

	// Convert repo models to Goa types
	traces := make([]*telem_gen.HookTraceSummary, len(items))
	for i, item := range items {
		traces[i] = &telem_gen.HookTraceSummary{
			TraceID:           item.TraceID,
			StartTimeUnixNano: strconv.FormatInt(item.StartTimeUnixNano, 10),
			LogCount:          item.LogCount,
			HookStatus:        item.HookStatus,
			BlockReason:       item.BlockReason,
			GramUrn:           item.GramURN,
			ToolName:          item.ToolName,
			ToolSource:        item.ToolSource,
			EventSource:       item.EventSource,
			UserEmail:         item.UserEmail,
			HookSource:        item.HookSource,
			SkillName:         item.SkillName,
		}
	}

	return &telem_gen.ListHooksTracesResult{
		Traces:     traces,
		NextCursor: nextCursor,
	}, nil
}

// GetChatMetricsByIDs retrieves token and cost metrics for specific chat IDs.
// This is used by the chat service to enrich chat overview data with metrics from ClickHouse.
func (s *Service) GetChatMetricsByIDs(ctx context.Context, projectID string, chatIDs []string) (map[string]repo.ChatMetricsRow, error) {
	if s.chRepo == nil {
		return make(map[string]repo.ChatMetricsRow), nil
	}

	result, err := s.chRepo.GetChatMetricsByIDs(ctx, repo.GetChatMetricsByIDsParams{
		GramProjectID: projectID,
		ChatIDs:       chatIDs,
	})
	if err != nil {
		return nil, fmt.Errorf("get chat metrics by ids: %w", err)
	}
	return result, nil
}

// toTopUsersFromPG converts PostgreSQL top users to API type.
func toTopUsersFromPG(users []chatRepo.GetTopUsersByMessagesRow) []*telem_gen.TopUser {
	if users == nil {
		return []*telem_gen.TopUser{}
	}
	result := make([]*telem_gen.TopUser, len(users))
	for i, u := range users {
		userID := ""
		if u.UserID.Valid {
			userID = u.UserID.String
		}
		result[i] = &telem_gen.TopUser{
			UserID:        userID,
			UserType:      u.UserType,
			ActivityCount: u.MessageCount,
		}
	}
	return result
}

// toLLMClientUsageFromPG converts PostgreSQL LLM client breakdown to API type.
func toLLMClientUsageFromPG(clients []chatRepo.GetLLMClientBreakdownByMessagesRow) []*telem_gen.LLMClientUsage {
	if clients == nil {
		return []*telem_gen.LLMClientUsage{}
	}
	result := make([]*telem_gen.LLMClientUsage, len(clients))
	for i, c := range clients {
		result[i] = &telem_gen.LLMClientUsage{
			ClientName:    c.ClientName,
			ActivityCount: c.MessageCount,
		}
	}
	return result
}
