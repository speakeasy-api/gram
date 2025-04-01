package projects

import (
	"context"
	"errors"
	"log/slog"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/speakeasy-api/gram/internal/conv"
	"github.com/speakeasy-api/gram/internal/must"

	"github.com/speakeasy-api/gram/internal/projects/repo"
)

var ErrProjectNameExists = errors.New("project name already exists")

type Service struct {
	logger *slog.Logger
	db     *pgxpool.Pool
	repo   *repo.Queries
}

func NewService(logger *slog.Logger, db *pgxpool.Pool) *Service {
	return &Service{logger: logger, db: db, repo: repo.New(db)}
}

func (s *Service) GetProjectsOrCreateDefault(ctx context.Context, organizationID string) ([]repo.Project, error) {
	projects, err := s.repo.ListProjectsByOrganization(ctx, must.Value(uuid.Parse(organizationID)))
	if err != nil {
		return nil, err
	}

	if len(projects) == 0 {
		project, err := s.CreateProject(ctx, organizationID, "Default")
		if err != nil {
			return nil, err
		}
		projects = append(projects, project)
	}

	return projects, nil
}

func (s *Service) CreateProject(ctx context.Context, organizationID, name string) (repo.Project, error) {
	project, err := s.repo.CreateProject(ctx, repo.CreateProjectParams{
		OrganizationID: must.Value(uuid.Parse(organizationID)),
		Name:           name,
		Slug:           conv.ToSlug(name),
	})
	if err != nil {
		if strings.Contains(err.Error(), "unique constraint") {
			return project, errors.New("project slug already exists")
		}
	}
	return project, nil
}

func (s *Service) GetProject(ctx context.Context, projectID string) (repo.Project, error) {
	return s.repo.GetProject(ctx, must.Value(uuid.Parse(projectID)))
}
