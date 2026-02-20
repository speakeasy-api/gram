package about

import (
	"bytes"
	"context"
	_ "embed"
	"io"
	"log/slog"

	"go.opentelemetry.io/otel/trace"
	goahttp "goa.design/goa/v3/http"

	gen "github.com/speakeasy-api/gram/server/gen/about"
	srv "github.com/speakeasy-api/gram/server/gen/http/about/server"
	"github.com/speakeasy-api/gram/server/internal/middleware"
)

type Service struct {
	logger *slog.Logger
	tracer trace.Tracer
}

var _ gen.Service = (*Service)(nil)

func NewService(logger *slog.Logger, tracerProvider trace.TracerProvider) *Service {
	return &Service{
		logger: logger,
		tracer: tracerProvider.Tracer("github.com/speakeasy-api/gram/server/internal/about"),
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

// Openapi implements about.Service.
func (s *Service) Openapi(context.Context) (res *gen.OpenapiResult, body io.ReadCloser, err error) {
	return &gen.OpenapiResult{
		ContentType:   "text/yaml",
		ContentLength: int64(len(openapiDoc)),
	}, io.NopCloser(bytes.NewReader(openapiDoc)), nil
}
