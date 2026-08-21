package otel

import (
	"context"
	"log/slog"

	"github.com/jackc/pgx/v5/pgxpool"
	otelv1 "github.com/speakeasy-api/gram/infra/gen/gram/otel/v1"
	"github.com/speakeasy-api/gram/infra/pkg/gcp"
	"go.opentelemetry.io/otel/trace"
	goahttp "goa.design/goa/v3/http"
	"goa.design/goa/v3/security"

	srv "github.com/speakeasy-api/gram/server/gen/http/otel/server"
	gen "github.com/speakeasy-api/gram/server/gen/otel"
	"github.com/speakeasy-api/gram/server/internal/auth"
	"github.com/speakeasy-api/gram/server/internal/auth/sessions"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/constants"
	"github.com/speakeasy-api/gram/server/internal/middleware"
)

const maxOTLPExportBytes = 20 * constants.MiB
const otelProvenanceSource = "speakeasy"

type Service struct {
	logger        *slog.Logger
	tracer        trace.Tracer
	auth          *auth.Auth
	logPublisher  gcp.Publisher[*otelv1.InboundLogRecord]
	spanPublisher gcp.Publisher[*otelv1.InboundSpan]
}

var _ gen.Service = (*Service)(nil)
var _ gen.Auther = (*Service)(nil)

func NewService(
	logger *slog.Logger,
	tracerProvider trace.TracerProvider,
	db *pgxpool.Pool,
	sessions *sessions.Manager,
	authzEngine *authz.Engine,
	spanPublisher gcp.Publisher[*otelv1.InboundSpan],
	logPublisher gcp.Publisher[*otelv1.InboundLogRecord],
) *Service {
	return &Service{
		logger:        logger,
		tracer:        tracerProvider.Tracer("github.com/speakeasy-api/gram/server/internal/otel"),
		auth:          auth.New(logger, db, sessions, authzEngine),
		logPublisher:  logPublisher,
		spanPublisher: spanPublisher,
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
