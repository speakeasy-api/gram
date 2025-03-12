package deployments

import (
	"context"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/oklog/ulid/v2"
	goahttp "goa.design/goa/v3/http"

	gen "github.com/speakeasy-api/gram/gen/deployments"
	srv "github.com/speakeasy-api/gram/gen/http/deployments/server"
	"github.com/speakeasy-api/gram/internal/conv"
	"github.com/speakeasy-api/gram/internal/deployments/repo"
)

type Service struct {
	logger *slog.Logger
	db     *pgxpool.Pool
	repo   *repo.Queries
}

var _ gen.Service = &Service{}

func NewService(logger *slog.Logger, db *pgxpool.Pool) *Service {
	return &Service{logger: logger, db: db, repo: repo.New(db)}
}

func Attach(mux goahttp.Muxer, service gen.Service) {
	endpoints := gen.NewEndpoints(service)
	srv.Mount(
		mux,
		srv.New(endpoints, mux, goahttp.RequestDecoder, goahttp.ResponseEncoder, nil, nil),
	)
}

func (s *Service) GetDeployment(ctx context.Context, form *gen.DeploymentGetForm) (*gen.DeploymentGetResult, error) {
	id, err := ulid.Parse(form.ID)
	if err != nil {
		return nil, err
	}

	deployment, err := s.repo.GetDeployment(ctx, id)
	if err != nil {
		return nil, err
	}

	return &gen.DeploymentGetResult{
		ID:              deployment.ID.String(),
		CreatedAt:       deployment.CreatedAt.Time.Format(time.RFC3339),
		OrganizationID:  deployment.OrganizationID.String(),
		WorkspaceID:     deployment.WorkspaceID.String(),
		UserID:          deployment.UserID.String(),
		ExternalID:      conv.FromPGText(deployment.ExternalID),
		ExternalURL:     conv.FromPGText(deployment.ExternalUrl),
		Openapi3p1Tools: []*gen.OpenAPI3P1ToolForm{},
	}, nil
}

func (s *Service) ListDeployments(context.Context, *gen.DeploymentListForm) (res *gen.DeploymentListResult, err error) {
	return &gen.DeploymentListResult{}, nil
}

func (s *Service) CreateDeployment(ctx context.Context, form *gen.DeploymentCreateForm) (*gen.DeploymentCreateResult, error) {
	deployment, err := s.repo.CreateDeployment(ctx, repo.CreateDeploymentParams{
		UserID:         ulid.Make(),
		OrganizationID: ulid.Make(),
		WorkspaceID:    ulid.Make(),
		ExternalID:     pgtype.Text{String: *form.ExternalID, Valid: true},
		ExternalUrl:    pgtype.Text{String: *form.ExternalURL, Valid: true},
	})
	if err != nil {
		return nil, err
	}

	return &gen.DeploymentCreateResult{
		ID:              deployment.ID.String(),
		CreatedAt:       deployment.CreatedAt.Time.Format(time.RFC3339),
		OrganizationID:  deployment.OrganizationID.String(),
		WorkspaceID:     deployment.WorkspaceID.String(),
		UserID:          deployment.UserID.String(),
		ExternalID:      conv.FromPGText(deployment.ExternalID),
		ExternalURL:     conv.FromPGText(deployment.ExternalUrl),
		Openapi3p1Tools: []*gen.OpenAPI3P1ToolForm{},
	}, nil
}
