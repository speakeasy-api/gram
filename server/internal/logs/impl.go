package logs

import (
	"context"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	srv "github.com/speakeasy-api/gram/server/gen/http/logs/server"
	gen "github.com/speakeasy-api/gram/server/gen/logs"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/auth"
	"github.com/speakeasy-api/gram/server/internal/auth/sessions"
	"github.com/speakeasy-api/gram/server/internal/middleware"
	"github.com/speakeasy-api/gram/server/internal/oops"
	tm "github.com/speakeasy-api/gram/server/internal/thirdparty/toolmetrics"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/trace"
	goahttp "goa.design/goa/v3/http"
	"goa.design/goa/v3/security"
)

type Service struct {
	tcm    tm.ToolMetricsClient
	db     *pgxpool.Pool
	tracer trace.Tracer
	logger *slog.Logger
	auth   *auth.Auth
}

var _ gen.Service = (*Service)(nil)
var _ gen.Auther = (*Service)(nil)

func NewService(logger *slog.Logger, db *pgxpool.Pool, sessions *sessions.Manager, tcm tm.ToolMetricsClient) *Service {
	logger = logger.With(attr.SlogComponent("logs"))

	return &Service{
		tracer: otel.Tracer("github.com/speakeasy-api/gram/server/internal/logs"),
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

func (s Service) ListLogs(ctx context.Context, payload *gen.ListLogsPayload) (res *gen.ListToolLogResult, err error) {
	// Parse time parameters with defaults
	tsStart := parseTimeOrDefault(payload.TsStart, time.Now().Add(-48*time.Hour).UTC())
	tsEnd := parseTimeOrDefault(payload.TsEnd, time.Now().UTC())
	cursor := parseTimeOrDefault(payload.Cursor, time.Now().UTC())

	// Build pagination request
	pagination := &tm.PaginationRequest{
		PerPage:    payload.PerPage,
		Direction:  tm.PageDirection(payload.Direction),
		Sort:       payload.Sort,
		PrevCursor: "",
		NextCursor: "",
	}
	pagination.SetDefaults()

	//nolint:sloglint // debug logging for pagination
	s.logger.InfoContext(ctx, "request payload",
		attr.SlogProjectID(payload.ProjectID),
		slog.Int("pagination_per_page", pagination.PerPage),
		slog.String("pagination_direction", string(pagination.Direction)),
		slog.String("pagination_sort", pagination.Sort),
		slog.String("pagination_cursor", pagination.Cursor()),
		slog.String("pagination_prev_cursor", pagination.PrevCursor),
		slog.String("pagination_next_cursor", pagination.NextCursor),
		slog.Int("pagination_limit", pagination.Limit()),
		slog.Time("ts_start", tsStart),
		slog.Time("ts_end", tsEnd),
		slog.Time("cursor", cursor))

	// Query logs from ClickHouse
	result, err := s.tcm.List(ctx, payload.ProjectID, tsStart, tsEnd, cursor, pagination)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error listing logs").
			Log(ctx, s.logger, attr.SlogProjectID(payload.ProjectID))
	}

	// Convert results to gen.HTTPToolLog
	logs := make([]*gen.HTTPToolLog, 0, len(result.Logs))
	for _, r := range result.Logs {
		logs = append(logs, toHTTPToolLog(r))
	}

	// Convert pagination metadata to API format
	var nextPageCursor *string
	if result.Pagination.NextPageCursor != nil {
		c := result.Pagination.NextPageCursor.Format(time.RFC3339)
		nextPageCursor = &c
	}

	pp := &gen.PaginationResult{
		PerPage:        &result.Pagination.PerPage,
		HasNextPage:    &result.Pagination.HasNextPage,
		NextPageCursor: nextPageCursor,
	}

	return &gen.ListToolLogResult{Logs: logs, Pagination: pp}, nil
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

func toHTTPToolLog(r tm.ToolHTTPRequest) *gen.HTTPToolLog {
	return &gen.HTTPToolLog{
		Ts:                r.Ts.Format(time.RFC3339),
		OrganizationID:    r.OrganizationID,
		ProjectID:         r.ProjectID,
		DeploymentID:      r.DeploymentID,
		ToolID:            r.ToolID,
		ToolUrn:           r.ToolURN,
		ToolType:          gen.ToolType(r.ToolType),
		TraceID:           r.TraceID,
		SpanID:            r.SpanID,
		HTTPMethod:        r.HTTPMethod,
		HTTPRoute:         r.HTTPRoute,
		StatusCode:        uint32(r.StatusCode),
		DurationMs:        r.DurationMs,
		UserAgent:         r.UserAgent,
		ClientIpv4:        r.ClientIPv4,
		RequestHeaders:    r.RequestHeaders,
		RequestBody:       r.RequestBody,
		RequestBodySkip:   r.RequestBodySkip,
		RequestBodyBytes:  &r.RequestBodyBytes,
		ResponseHeaders:   r.ResponseHeaders,
		ResponseBody:      r.ResponseBody,
		ResponseBodySkip:  r.ResponseBodySkip,
		ResponseBodyBytes: &r.ResponseBodyBytes,
	}
}

func (s Service) APIKeyAuth(ctx context.Context, key string, schema *security.APIKeyScheme) (context.Context, error) {
	return s.auth.Authorize(ctx, key, schema)
}
