package projects

import (
	"context"
	"errors"
	"log/slog"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/speakeasy-api/gram/internal/conv"

	envrepo "github.com/speakeasy-api/gram/internal/environments/repo"
	"github.com/speakeasy-api/gram/internal/projects/repo"
)

var ErrProjectNameExists = errors.New("project name already exists")

type Service struct {
	logger  *slog.Logger
	db      *pgxpool.Pool
	repo    *repo.Queries
	envRepo *envrepo.Queries
}

func NewService(logger *slog.Logger, db *pgxpool.Pool) *Service {
	return &Service{logger: logger, db: db, repo: repo.New(db), envRepo: envrepo.New(db)}
}

func (s *Service) GetProjectsOrSetuptDefaults(ctx context.Context, organizationID string) ([]repo.Project, error) {
	projects, err := s.repo.ListProjectsByOrganization(ctx, organizationID)
	if err != nil {
		s.logger.ErrorContext(ctx, "failed to list projects by organization", slog.String("error", err.Error()))
		return nil, err
	}

	if len(projects) == 0 {
		project, err := s.CreateProject(ctx, organizationID, "Default")
		if err != nil {
			s.logger.ErrorContext(ctx, "failed to create default project", slog.String("error", err.Error()))
			return nil, err
		}

		// each project has a default environment
		_, err = s.envRepo.CreateEnvironment(ctx, envrepo.CreateEnvironmentParams{
			OrganizationID: organizationID,
			ProjectID:      project.ID,
			Name:           "Default",
			Slug:           "default",
		})
		if err != nil {
			s.logger.ErrorContext(ctx, "failed to create default environment", slog.String("error", err.Error()))
			return nil, err
		}

		projects = append(projects, project)
	}

	return projects, nil
}

func (s *Service) CreateProject(ctx context.Context, organizationID, name string) (repo.Project, error) {
	project, err := s.repo.CreateProject(ctx, repo.CreateProjectParams{
		OrganizationID: organizationID,
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
