package admin

import (
	"context"
	"log/slog"

	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/otel/trace"
	goahttp "goa.design/goa/v3/http"

	"github.com/speakeasy-api/gram/glint/annotations"
	gen "github.com/speakeasy-api/gram/server/gen/admin"
	srv "github.com/speakeasy-api/gram/server/gen/http/admin/server"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/middleware"
)

// Stand in for upcoming real auth
type Auther any

type Service struct {
	annotations.Service[gen.Service, Auther]

	logger         *slog.Logger
	tracer         trace.Tracer
	db             *pgxpool.Pool
	guardianPolicy *guardian.Policy
}

func NewService(
	logger *slog.Logger,
	tracerProvider trace.TracerProvider,
	db *pgxpool.Pool,
	guardianPolicy *guardian.Policy,
) *Service {
	return &Service{
		logger:         logger,
		tracer:         tracerProvider.Tracer("github.com/speakeasy-api/gram/server/internal/admin"),
		db:             db,
		guardianPolicy: guardianPolicy,

		Service: annotations.Service[gen.Service, Auther]{},
	}
}

var _ gen.Service = (*Service)(nil)
var _ Auther = (*Service)(nil)

func Attach(mux goahttp.Muxer, service *Service) {
	endpoints := gen.NewEndpoints(service)
	endpoints.Use(middleware.MapErrors())
	endpoints.Use(middleware.TraceMethods(service.tracer))
	srv.Mount(
		mux,
		srv.New(endpoints, mux, goahttp.RequestDecoder, goahttp.ResponseEncoder, nil, nil),
	)
}

func (s *Service) Poke(context.Context) (*gen.PokeResult, error) {
	return &gen.PokeResult{OK: true}, nil
}
