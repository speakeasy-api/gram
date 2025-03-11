package deployments

import (
	"context"
	"database/sql"
	"time"

	"github.com/oklog/ulid/v2"
	gen "github.com/speakeasy-api/gram/gen/deployments"
)

type Service struct {
	db *sql.DB
}

var _ gen.Service = &Service{}

func NewService(db *sql.DB) *Service {
	return &Service{db: db}
}

// GetDeployment implements deployments.Service.
func (s *Service) GetDeployment(context.Context, *gen.DeploymentGetForm) (*gen.DeploymentGetResult, error) {
	return &gen.DeploymentGetResult{
		ID:              "123",
		CreatedAt:       time.Now().Format(time.RFC3339),
		Openapi3p1Tools: []*gen.OpenAPI3P1ToolForm{},
	}, nil
}

// ListDeployments implements deployments.Service.
func (s *Service) ListDeployments(context.Context, *gen.DeploymentListForm) (res *gen.DeploymentListResult, err error) {
	return &gen.DeploymentListResult{}, nil
}

func (s *Service) CreateDeployment(ctx context.Context, form *gen.DeploymentCreateForm) (*gen.DeploymentCreateResult, error) {
	return &gen.DeploymentCreateResult{
		ID:              ulid.Make().String(),
		CreatedAt:       time.Now().Format(time.RFC3339),
		ExternalID:      form.ExternalID,
		ExternalURL:     form.ExternalURL,
		Openapi3p1Tools: []*gen.OpenAPI3P1ToolForm{},
	}, nil
}
