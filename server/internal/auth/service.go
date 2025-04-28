package auth

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/trace"
	goahttp "goa.design/goa/v3/http"
	"goa.design/goa/v3/security"

	gen "github.com/speakeasy-api/gram/gen/auth"
	srv "github.com/speakeasy-api/gram/gen/http/auth/server"
	"github.com/speakeasy-api/gram/internal/auth/sessions"
	"github.com/speakeasy-api/gram/internal/contextvalues"
	"github.com/speakeasy-api/gram/internal/conv"
	envRepo "github.com/speakeasy-api/gram/internal/environments/repo"
	"github.com/speakeasy-api/gram/internal/middleware"
	"github.com/speakeasy-api/gram/internal/oops"
	projectsRepo "github.com/speakeasy-api/gram/internal/projects/repo"
)

type AuthConfigurations struct {
	SpeakeasyServerAddress string
	GramServerURL          string
	SignInRedirectURL      string
}

// Service for gram dashboard authentication endpoints
type Service struct {
	tracer       trace.Tracer
	logger       *slog.Logger
	db           *pgxpool.Pool
	sessions     *sessions.Manager
	cfg          AuthConfigurations
	projectsRepo *projectsRepo.Queries
	envRepo      *envRepo.Queries
}

var _ gen.Service = (*Service)(nil)

func NewService(logger *slog.Logger, db *pgxpool.Pool, sessions *sessions.Manager, cfg AuthConfigurations) *Service {
	return &Service{
		tracer:       otel.Tracer("github.com/speakeasy-api/gram/internal/auth"),
		logger:       logger,
		db:           db,
		sessions:     sessions,
		cfg:          cfg,
		projectsRepo: projectsRepo.New(db),
		envRepo:      envRepo.New(db),
	}
}

func FormSignInRedirectURL(env string) string {
	switch env {
	case "local":
		return "http://localhost:5173/"
	case "test":
		return "" // TODO: Fill in once hosted
	case "prod":
		return "" // TODO: Fill in once hosted
	default:
		return ""
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
	return s.sessions.Authenticate(ctx, key, true) // TODO: canStubAuth is a temporary hack to allow us to limit auth stubbing to rpc/auth endpoints
}

func (s *Service) Callback(ctx context.Context, payload *gen.CallbackPayload) (res *gen.CallbackResult, err error) {
	redirectWithError := func(err error) (*gen.CallbackResult, error) {
		s.logger.ErrorContext(ctx, "signin error", slog.String("error", err.Error()))
		return &gen.CallbackResult{
			Location:      fmt.Sprintf("%s?signin_error=%s", s.cfg.SignInRedirectURL, err.Error()),
			SessionToken:  "",
			SessionCookie: "",
		}, nil
	}
	userInfo, err := s.sessions.GetUserInfoFromSpeakeasy(payload.IDToken)
	if err != nil {
		return redirectWithError(err)
	}

	activeOrganizationID := ""
	if len(userInfo.Organizations) > 0 {
		activeOrganizationID = userInfo.Organizations[0].ID

		// For admins we allow you to override the active organization returned by header if present
		// Otherwise we default speakeasy-self being the active organization if present
		if userInfo.Admin {
			adminOverride, _ := contextvalues.GetAdminOverrideFromContext(ctx)
			if adminOverride == "" {
				adminOverride = "speakeasy-self"
			}
			for _, org := range userInfo.Organizations {
				if org.Slug == adminOverride {
					activeOrganizationID = org.ID
					break
				}
			}
		}
	}

	session := sessions.Session{
		SessionID:            payload.IDToken,
		UserID:               userInfo.UserID,
		ActiveOrganizationID: activeOrganizationID,
	}

	if err := s.sessions.StoreSession(ctx, session); err != nil {
		return redirectWithError(err)
	}

	return &gen.CallbackResult{
		Location:      s.cfg.SignInRedirectURL,
		SessionToken:  session.SessionID,
		SessionCookie: session.SessionID,
	}, nil
}

func (s *Service) Login(context.Context) (res *gen.LoginResult, err error) {
	returnAddress := strings.TrimRight(s.cfg.GramServerURL, "/")

	values := url.Values{}
	values.Add("return_url", returnAddress+"/rpc/auth.callback")

	location := s.cfg.SpeakeasyServerAddress + "/v1/speakeasy_provider/login?" + values.Encode()

	return &gen.LoginResult{
		Location: location,
	}, nil
}

func (s *Service) SwitchScopes(ctx context.Context, payload *gen.SwitchScopesPayload) (res *gen.SwitchScopesResult, err error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.SessionID == nil {
		return nil, errors.New("session not found in context")
	}

	userInfo, err := s.sessions.GetUserInfo(ctx, authCtx.UserID, *authCtx.SessionID)
	if err != nil {
		return nil, err
	}

	if payload.OrganizationID != nil {
		orgFound := false
		for _, org := range userInfo.Organizations {
			if org.ID == *payload.OrganizationID {
				orgFound = true
				break
			}
		}
		if !orgFound {
			return nil, errors.New("organization not found")
		}
		authCtx.ActiveOrganizationID = *payload.OrganizationID
	}

	if err := s.sessions.UpdateSession(ctx, sessions.Session{
		SessionID:            *authCtx.SessionID,
		ActiveOrganizationID: authCtx.ActiveOrganizationID,
		UserID:               authCtx.UserID,
	}); err != nil {
		return nil, err
	}

	return &gen.SwitchScopesResult{
		SessionToken:  *authCtx.SessionID,
		SessionCookie: *authCtx.SessionID,
	}, nil
}

func (s *Service) Logout(ctx context.Context, payload *gen.LogoutPayload) (res *gen.LogoutResult, err error) {
	// Clears cookie and invalidates session
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.SessionID == nil {
		return nil, errors.New("session not found in context")
	}

	if err := s.sessions.InvalidateUserInfoCache(ctx, authCtx.UserID); err != nil {
		return nil, err
	}

	if err := s.sessions.ClearSession(ctx, sessions.Session{
		SessionID:            *authCtx.SessionID,
		ActiveOrganizationID: authCtx.ActiveOrganizationID,
		UserID:               authCtx.UserID,
	}); err != nil {
		return nil, err
	}
	return &gen.LogoutResult{SessionCookie: ""}, nil
}

func (s *Service) Info(ctx context.Context, payload *gen.InfoPayload) (res *gen.InfoResult, err error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.SessionID == nil {
		return nil, errors.New("session not found in context")
	}

	userInfo, err := s.sessions.GetUserInfo(ctx, authCtx.UserID, *authCtx.SessionID)
	if err != nil {
		return nil, err
	}

	// For admins we only return the active organization to avoid overloaded returns
	if userInfo.Admin {
		for _, org := range userInfo.Organizations {
			if org.ID == authCtx.ActiveOrganizationID {
				userInfo.Organizations = []gen.OrganizationEntry{org}
			}
		}
	}

	// Fully unpack the userInfo object
	organizations := make([]*gen.OrganizationEntry, 0, len(userInfo.Organizations))
	for _, org := range userInfo.Organizations {
		// TODO: Not the cleanest but a temporary measue while in POC phase.
		// This may actually be bettter executed from elsewhere
		projectRows, err := s.getProjectsOrSetupDefaults(ctx, org.ID)
		if err != nil {
			return nil, err
		}
		var orgProjects []*gen.ProjectEntry
		for _, project := range projectRows {
			orgProjects = append(orgProjects, &gen.ProjectEntry{
				ID:   project.ID.String(),
				Name: project.Name,
				Slug: gen.Slug(project.Slug),
			})
		}

		organizations = append(organizations, &gen.OrganizationEntry{
			ID:          org.ID,
			Name:        org.Name,
			Slug:        org.Slug,
			AccountType: org.AccountType,
			Projects:    orgProjects,
		})
	}

	return &gen.InfoResult{
		SessionToken:         *authCtx.SessionID,
		SessionCookie:        *authCtx.SessionID,
		ActiveOrganizationID: authCtx.ActiveOrganizationID,
		UserID:               userInfo.UserID,
		UserEmail:            userInfo.Email,
		Organizations:        organizations,
	}, nil
}

func (s *Service) getProjectsOrSetupDefaults(ctx context.Context, organizationID string) ([]projectsRepo.Project, error) {
	projects, err := s.projectsRepo.ListProjectsByOrganization(ctx, organizationID)
	if err != nil {
		return nil, oops.E(err, "error listing projects", "failed to list projects by organization").Log(ctx, s.logger)
	}

	if len(projects) == 0 {
		project, err := s.createDefaultProject(ctx, organizationID)
		if err != nil {
			return nil, oops.E(err, "error creating default project", "failed to create default project").Log(ctx, s.logger)
		}

		_, err = s.envRepo.CreateEnvironment(ctx, envRepo.CreateEnvironmentParams{
			OrganizationID: organizationID,
			ProjectID:      project.ID,
			Name:           "Default",
			Slug:           "default",
			Description:    conv.ToPGText("Default project for organization"),
		})
		if err != nil {
			return nil, oops.E(err, "error creating default environment", "failed to create default environment").Log(ctx, s.logger)
		}

		projects = append(projects, project)
	}

	return projects, nil
}

func (s *Service) createDefaultProject(ctx context.Context, organizationID string) (projectsRepo.Project, error) {
	project, err := s.projectsRepo.CreateProject(ctx, projectsRepo.CreateProjectParams{
		OrganizationID: organizationID,
		Name:           "Default",
		Slug:           "default",
	})
	if err != nil {
		if strings.Contains(err.Error(), "unique constraint") {
			return project, errors.New("project slug already exists")
		}
		return project, err
	}
	return project, nil
}
