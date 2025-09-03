package projects

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgerrcode"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/trace"
	goahttp "goa.design/goa/v3/http"
	"goa.design/goa/v3/security"

	genAuth "github.com/speakeasy-api/gram/server/gen/auth"
	srv "github.com/speakeasy-api/gram/server/gen/http/projects/server"
	gen "github.com/speakeasy-api/gram/server/gen/projects"
	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/auth"
	"github.com/speakeasy-api/gram/server/internal/auth/sessions"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	envrepo "github.com/speakeasy-api/gram/server/internal/environments/repo"
	"github.com/speakeasy-api/gram/server/internal/middleware"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/projects/repo"
)

type Service struct {
	tracer   trace.Tracer
	logger   *slog.Logger
	db       *pgxpool.Pool
	repo     *repo.Queries
	envRepo  *envrepo.Queries
	sessions *sessions.Manager
	auth     *auth.Auth
}

var _ gen.Service = (*Service)(nil)

func NewService(logger *slog.Logger, db *pgxpool.Pool, sessions *sessions.Manager) *Service {
	logger = logger.With(attr.SlogComponent("projects"))

	return &Service{
		tracer:   otel.Tracer("github.com/speakeasy-api/gram/server/internal/projects"),
		logger:   logger,
		db:       db,
		repo:     repo.New(db),
		envRepo:  envrepo.New(db),
		sessions: sessions,
		auth:     auth.New(logger, db, sessions),
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

func (s *Service) APIKeyAuth(ctx context.Context, key string, schema *security.APIKeyScheme) (context.Context, error) {
	return s.auth.Authorize(ctx, key, schema)
}

func (s *Service) CreateProject(ctx context.Context, payload *gen.CreateProjectPayload) (*gen.CreateProjectResult, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.SessionID == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	userInfo, _, err := s.sessions.GetUserInfo(ctx, authCtx.UserID, *authCtx.SessionID)
	if err != nil {
		return nil, fmt.Errorf("get session user info: %w", err)
	}

	if orgIdx := slices.IndexFunc(userInfo.Organizations, func(org genAuth.OrganizationEntry) bool {
		return org.ID == payload.OrganizationID
	}); orgIdx == -1 {
		return nil, oops.C(oops.CodeForbidden)
	}

	prj, err := s.repo.CreateProject(ctx, repo.CreateProjectParams{
		OrganizationID: payload.OrganizationID,
		Name:           payload.Name,
		Slug:           conv.ToSlug(payload.Name),
	})
	var pgErr *pgconn.PgError
	switch {
	case errors.As(err, &pgErr):
		if pgErr.Code == pgerrcode.UniqueViolation {
			return nil, oops.E(oops.CodeConflict, err, "project slug already exists")
		}
		return nil, oops.E(oops.CodeUnexpected, err, "database error creating project").Log(ctx, s.logger, attr.SlogOrganizationID(payload.OrganizationID), attr.SlogProjectName(payload.Name))
	case err != nil:
		return nil, oops.E(oops.CodeUnexpected, err, "unexpected error creating project").Log(ctx, s.logger, attr.SlogOrganizationID(payload.OrganizationID), attr.SlogProjectName(payload.Name))
	}

	_, err = s.envRepo.CreateEnvironment(ctx, envrepo.CreateEnvironmentParams{
		OrganizationID: payload.OrganizationID,
		ProjectID:      prj.ID,
		Name:           "Default",
		Slug:           "default",
		Description:    conv.ToPGText("Default project for organization"),
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error creating default environment").Log(ctx, s.logger)
	}

	project := &gen.CreateProjectResult{
		Project: &gen.Project{
			ID:             prj.ID.String(),
			Name:           prj.Name,
			Slug:           types.Slug(prj.Slug),
			OrganizationID: prj.OrganizationID,
			LogoAssetID:    nil,
			CreatedAt:      prj.CreatedAt.Time.Format(time.RFC3339),
			UpdatedAt:      prj.UpdatedAt.Time.Format(time.RFC3339),
		},
	}

	return project, nil
}

func (s *Service) ListProjects(ctx context.Context, payload *gen.ListProjectsPayload) (res *gen.ListProjectsResult, err error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.SessionID == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	userInfo, _, err := s.sessions.GetUserInfo(ctx, authCtx.UserID, *authCtx.SessionID)
	if err != nil {
		return nil, fmt.Errorf("get session user info: %w", err)
	}

	if orgIdx := slices.IndexFunc(userInfo.Organizations, func(org genAuth.OrganizationEntry) bool {
		return org.ID == payload.OrganizationID
	}); orgIdx == -1 {
		return nil, oops.C(oops.CodeForbidden)
	}

	if payload.OrganizationID == "" {
		return nil, oops.E(oops.CodeInvalid, nil, "organization id is required")
	}

	projects, err := s.repo.ListProjectsByOrganization(ctx, payload.OrganizationID)
	if err != nil {
		return nil, fmt.Errorf("list projects by organization %s: %w", payload.OrganizationID, err)
	}

	entries := make([]*gen.ProjectEntry, 0, len(projects))
	for _, project := range projects {
		entries = append(entries, &gen.ProjectEntry{
			ID:   project.ID.String(),
			Name: project.Name,
			Slug: types.Slug(project.Slug),
		})
	}

	return &gen.ListProjectsResult{
		Projects: entries,
	}, nil
}

func (s *Service) SetLogo(ctx context.Context, payload *gen.SetLogoPayload) (res *gen.SetProjectLogoResult, err error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	assetID, err := uuid.Parse(payload.AssetID)
	if err != nil {
		return nil, oops.E(oops.CodeInvalid, err, "error parsing asset ID").Log(ctx, s.logger)
	}

	updatedProject, err := s.repo.UploadProjectLogo(ctx, repo.UploadProjectLogoParams{
		ProjectID:   *authCtx.ProjectID,
		LogoAssetID: uuid.NullUUID{UUID: assetID, Valid: true},
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error updating project logo").Log(ctx, s.logger)
	}

	projectResponse := &gen.Project{
		ID:             updatedProject.ID.String(),
		Name:           updatedProject.Name,
		Slug:           types.Slug(updatedProject.Slug),
		OrganizationID: updatedProject.OrganizationID,
		LogoAssetID:    conv.FromNullableUUID(updatedProject.LogoAssetID),
		CreatedAt:      updatedProject.CreatedAt.Time.Format(time.RFC3339),
		UpdatedAt:      updatedProject.UpdatedAt.Time.Format(time.RFC3339),
	}

	return &gen.SetProjectLogoResult{
		Project: projectResponse,
	}, nil
}
