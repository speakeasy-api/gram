package projects

import (
	"context"
	"log/slog"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/speakeasy-api/gram/internal/must"

	"github.com/speakeasy-api/gram/internal/projects/repo"
)

type Service struct {
	logger *slog.Logger
	db     *pgxpool.Pool
	repo   *repo.Queries
}

func NewService(logger *slog.Logger, db *pgxpool.Pool) *Service {
	return &Service{logger: logger, db: db, repo: repo.New(db)}
}

func (s *Service) ListProjectsByOrganizationID(ctx context.Context, organizationID string) ([]repo.Project, error) {
	return s.repo.ListProjectsByOrganization(ctx, must.Value(uuid.Parse(organizationID)))
}

func (s *Service) CreateProject(ctx context.Context, organizationID string) (repo.Project, error) {
	return s.repo.CreateProject(ctx, must.Value(uuid.Parse(organizationID)))
}

func (s *Service) GetProject(ctx context.Context, projectID string) (repo.Project, error) {
	return s.repo.GetProject(ctx, must.Value(uuid.Parse(projectID)))
}
