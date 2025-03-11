package system

import (
	"context"

	gen "github.com/speakeasy-api/gram/gen/system"
)

type Service struct{}

var _ gen.Service = &Service{}

func NewService() *Service {
	return &Service{}
}

func (s *Service) HealthCheck(context.Context) (res *gen.HealthCheckResult, err error) {
	return &gen.HealthCheckResult{
		Status: "ok",
	}, nil
}
