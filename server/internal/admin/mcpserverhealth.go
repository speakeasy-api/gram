package admin

import (
	"context"

	gen "github.com/speakeasy-api/gram/server/gen/admin"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

func (s *Service) DescribeMcpServerHealth(ctx context.Context, payload *gen.DescribeMcpServerHealthPayload) (*gen.AdminMcpServerHealth, error) {
	return nil, oops.E(oops.CodeNotImplemented, nil, "not implemented")
}
