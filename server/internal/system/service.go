package system

import (
	"context"

	goahttp "goa.design/goa/v3/http"

	srv "github.com/speakeasy-api/gram/gen/http/system/server"
	gen "github.com/speakeasy-api/gram/gen/system"
)

type Service struct{}

var _ gen.Service = &Service{}

func NewService() *Service {
	return &Service{}
}

func Attach(mux goahttp.Muxer, service gen.Service) {
	endpoints := gen.NewEndpoints(service)
	srv.Mount(
		mux,
		srv.New(endpoints, mux, goahttp.RequestDecoder, goahttp.ResponseEncoder, nil, nil),
	)
}

func (s *Service) HealthCheck(context.Context) (res *gen.HealthCheckResult, err error) {
	return &gen.HealthCheckResult{
		Status: "ok",
	}, nil
}
