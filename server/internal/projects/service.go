package projects

import (
	"context"
	"errors"
	"log/slog"
	"slices"
	"time"

	"github.com/jackc/pgerrcode"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/trace"
	goahttp "goa.design/goa/v3/http"
	"goa.design/goa/v3/security"

	genAuth "github.com/speakeasy-api/gram/gen/auth"
	srv "github.com/speakeasy-api/gram/gen/http/projects/server"
	gen "github.com/speakeasy-api/gram/gen/projects"
	"github.com/speakeasy-api/gram/internal/auth"
	"github.com/speakeasy-api/gram/internal/auth/sessions"
	"github.com/speakeasy-api/gram/internal/contextvalues"
	"github.com/speakeasy-api/gram/internal/conv"
	envrepo "github.com/speakeasy-api/gram/internal/environments/repo"
	"github.com/speakeasy-api/gram/internal/middleware"
	"github.com/speakeasy-api/gram/internal/projects/repo"
)

var ErrProjectNameExists = errors.New("project name already exists")

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
	return &Service{
		tracer:   otel.Tracer("github.com/speakeasy-api/gram/internal/projects"),
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
		return nil, errors.New("session not found in context")
	}

	userInfo, err := s.sessions.GetUserInfo(ctx, authCtx.UserID, *authCtx.SessionID)
	if err != nil {
		return nil, err
	}

	if orgIdx := slices.IndexFunc(userInfo.Organizations, func(org genAuth.OrganizationEntry) bool {
		return org.ID == payload.OrganizationID
	}); orgIdx == -1 {
		return nil, errors.New("unauthorized")
	}

	prj, err := s.repo.CreateProject(ctx, repo.CreateProjectParams{
		OrganizationID: payload.OrganizationID,
		Name:           payload.Name,
		Slug:           conv.ToSlug(string(payload.Slug)),
	})
	var pgErr *pgconn.PgError
	switch {
	case errors.As(err, &pgErr):
		if pgErr.Code == pgerrcode.UniqueViolation {
			return nil, errors.New("project slug already exists")
		}
		return nil, err
	case err != nil:
		return nil, err
	}

	project := &gen.CreateProjectResult{
		Project: &gen.Project{
			ID:             prj.ID.String(),
			Name:           prj.Name,
			Slug:           gen.Slug(prj.Slug),
			OrganizationID: prj.OrganizationID,
			CreatedAt:      prj.CreatedAt.Time.Format(time.RFC3339),
			UpdatedAt:      prj.UpdatedAt.Time.Format(time.RFC3339),
		},
	}

	return project, nil
}

func (s *Service) ListProjects(ctx context.Context, payload *gen.ListProjectsPayload) (res *gen.ListProjectsResult, err error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.SessionID == nil {
		return nil, errors.New("session not found in context")
	}

	userInfo, err := s.sessions.GetUserInfo(ctx, authCtx.UserID, *authCtx.SessionID)
	if err != nil {
		return nil, err
	}

	if orgIdx := slices.IndexFunc(userInfo.Organizations, func(org genAuth.OrganizationEntry) bool {
		return org.ID == payload.OrganizationID
	}); orgIdx == -1 {
		return nil, errors.New("unauthorized")
	}

	if payload.OrganizationID == "" {
		return nil, errors.New("organization id is required")
	}

	projects, err := s.repo.ListProjectsByOrganization(ctx, payload.OrganizationID)
	if err != nil {
		return nil, err
	}

	entries := make([]*gen.ProjectEntry, 0, len(projects))
	for _, project := range projects {
		entries = append(entries, &gen.ProjectEntry{
			ID:   project.ID.String(),
			Name: project.Name,
			Slug: gen.Slug(project.Slug),
		})
	}

	return &gen.ListProjectsResult{
		Projects: entries,
	}, nil
}
