package telemetry

import (
	"context"
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
