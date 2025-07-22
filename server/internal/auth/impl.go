package auth

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"regexp"
	"strings"

	"github.com/jackc/pgerrcode"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/trace"
	goahttp "goa.design/goa/v3/http"
	"goa.design/goa/v3/security"

	gen "github.com/speakeasy-api/gram/server/gen/auth"
	srv "github.com/speakeasy-api/gram/server/gen/http/auth/server"
	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/auth/sessions"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	envRepo "github.com/speakeasy-api/gram/server/internal/environments/repo"
	"github.com/speakeasy-api/gram/server/internal/middleware"
	"github.com/speakeasy-api/gram/server/internal/oops"
	orgRepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	projectsRepo "github.com/speakeasy-api/gram/server/internal/projects/repo"
)

const gramWaitlistTypeForm = "https://speakeasyapi.typeform.com/to/h6WJdwWr"

type AuthConfigurations struct {
	SpeakeasyServerAddress string
	GramServerURL          string
	SignInRedirectURL      string
	Environment            string
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
	orgRepo      *orgRepo.Queries
}

var _ gen.Service = (*Service)(nil)

func NewService(logger *slog.Logger, db *pgxpool.Pool, sessions *sessions.Manager, cfg AuthConfigurations) *Service {
	return &Service{
		tracer:       otel.Tracer("github.com/speakeasy-api/gram/server/internal/auth"),
		logger:       logger,
		db:           db,
		sessions:     sessions,
		cfg:          cfg,
		projectsRepo: projectsRepo.New(db),
		envRepo:      envRepo.New(db),
		orgRepo:      orgRepo.New(db),
	}
}

func FormSignInRedirectURL(siteURL string) string {
	return siteURL
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
	userInfo, err := s.sessions.GetUserInfoFromSpeakeasy(ctx, payload.IDToken)
	if err != nil {
		return redirectWithError(err)
	}

	if !userInfo.Admin && !userInfo.UserWhitelisted {
		return &gen.CallbackResult{
			Location:      gramWaitlistTypeForm,
			SessionToken:  "",
			SessionCookie: "",
		}, nil
	}

	session := sessions.Session{
		SessionID:            payload.IDToken,
		UserID:               userInfo.UserID,
		ActiveOrganizationID: "",
	}

	if len(userInfo.Organizations) == 0 {
		if err := s.sessions.StoreSession(ctx, session); err != nil {
			return redirectWithError(err)
		}

		return &gen.CallbackResult{
			Location:      s.cfg.SignInRedirectURL,
			SessionToken:  session.SessionID,
			SessionCookie: session.SessionID,
		}, nil
	}

	activeOrg := userInfo.Organizations[0]

	// For speakeasy users and admins we default speakeasy-team being the active organization if present
	// For admins we allow you to override the active organization returned by header if present
	if strings.HasSuffix(userInfo.Email, "@speakeasy.com") || strings.HasSuffix(userInfo.Email, "@speakeasyapi.dev") || userInfo.Admin {
		override := "speakeasy-team"
		if userInfo.Admin {
			if adminOverride, _ := contextvalues.GetAdminOverrideFromContext(ctx); adminOverride != "" {
				override = adminOverride
			}
		}
		for _, org := range userInfo.Organizations {
			if org.Slug == override {
				activeOrg = org
				break
			}
		}
	}

	if _, err := s.orgRepo.UpsertOrganizationMetadata(ctx, orgRepo.UpsertOrganizationMetadataParams{
		ID:   activeOrg.ID,
		Name: activeOrg.Name,
		Slug: activeOrg.Slug,
	}); err != nil {
		return redirectWithError(err)
	}

	session.ActiveOrganizationID = activeOrg.ID
	if err := s.sessions.StoreSession(ctx, session); err != nil {
		return redirectWithError(err)
	}

	return &gen.CallbackResult{
		Location:      s.cfg.SignInRedirectURL,
		SessionToken:  session.SessionID,
		SessionCookie: session.SessionID,
	}, nil
}

func (s *Service) Login(ctx context.Context) (res *gen.LoginResult, err error) {
	if s.sessions.IsUnsafeLocalDevelopment() {
		err = errors.New("calling rpc.login for local development stubbed auth is not supported because stubbed auth implies always being logged in. Reaching this point suggests a problem with dashboard authentication")
		s.logger.ErrorContext(ctx, "signin error", slog.String("error", err.Error()))
		return &gen.LoginResult{
			Location: fmt.Sprintf("%s?signin_error=%s", s.cfg.SignInRedirectURL, err.Error()),
		}, nil
	}

	returnAddress := strings.TrimRight(s.cfg.GramServerURL, "/")

	// Get the request context to access the Host
	requestCtx, ok := contextvalues.GetRequestContext(ctx)
	if ok && requestCtx != nil && strings.Contains(requestCtx.Host, "speakeasyapi.vercel.app") && s.cfg.Environment == "dev" {
		// For preview builds, use the request host with https protocol
		returnAddress = "https://" + requestCtx.Host
	}

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
		return nil, oops.C(oops.CodeUnauthorized)
	}

	userInfo, _, err := s.sessions.GetUserInfo(ctx, authCtx.UserID, *authCtx.SessionID)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error getting user info").Log(ctx, s.logger)
	}

	selectedOrg := authCtx.ActiveOrganizationID
	if payload.OrganizationID != nil {
		selectedOrg = *payload.OrganizationID
	}

	orgFound := false
	for _, org := range userInfo.Organizations {
		if org.ID == selectedOrg {
			orgFound = true
			if _, err := s.orgRepo.UpsertOrganizationMetadata(ctx, orgRepo.UpsertOrganizationMetadataParams{
				ID:   org.ID,
				Name: org.Name,
				Slug: org.Slug,
			}); err != nil {
				return nil, oops.E(oops.CodeUnexpected, err, "error upserting organization metadata").Log(ctx, s.logger)
			}
			break
		}
	}
	if !orgFound {
		return nil, oops.E(oops.CodeInvalid, nil, "organization not found in user info")
	}
	authCtx.ActiveOrganizationID = selectedOrg

	if err := s.sessions.UpdateSession(ctx, sessions.Session{
		SessionID:            *authCtx.SessionID,
		ActiveOrganizationID: authCtx.ActiveOrganizationID,
		UserID:               authCtx.UserID,
	}); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error updating auth session").Log(ctx, s.logger)
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
		return nil, oops.C(oops.CodeUnauthorized)
	}

	if err := s.sessions.InvalidateUserInfoCache(ctx, authCtx.UserID); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error invalidating user").Log(ctx, s.logger)
	}

	if err := s.sessions.ClearSession(ctx, sessions.Session{
		SessionID:            *authCtx.SessionID,
		ActiveOrganizationID: authCtx.ActiveOrganizationID,
		UserID:               authCtx.UserID,
	}); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error clearing session").Log(ctx, s.logger)
	}
	return &gen.LogoutResult{SessionCookie: ""}, nil
}

func (s *Service) Info(ctx context.Context, payload *gen.InfoPayload) (res *gen.InfoResult, err error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.SessionID == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	userInfo, fromCache, err := s.sessions.GetUserInfo(ctx, authCtx.UserID, *authCtx.SessionID)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error getting user info").Log(ctx, s.logger)
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
				Slug: types.Slug(project.Slug),
			})
		}

		// write through organization metadata when not from cache to keep entries updated
		// TODO: there may be a better place to do this
		if !fromCache {
			if _, err := s.orgRepo.UpsertOrganizationMetadata(ctx, orgRepo.UpsertOrganizationMetadataParams{
				ID:   org.ID,
				Name: org.Name,
				Slug: org.Slug,
			}); err != nil {
				return nil, oops.E(oops.CodeUnexpected, err, "error upserting organization metadata").Log(ctx, s.logger)
			}
		}

		organizations = append(organizations, &gen.OrganizationEntry{
			ID:                 org.ID,
			Name:               org.Name,
			Slug:               org.Slug,
			SsoConnectionID:    org.SsoConnectionID,
			UserWorkspaceSlugs: org.UserWorkspaceSlugs,
			Projects:           orgProjects,
		})
	}

	return &gen.InfoResult{
		SessionToken:         *authCtx.SessionID,
		SessionCookie:        *authCtx.SessionID,
		ActiveOrganizationID: authCtx.ActiveOrganizationID,
		GramAccountType:      authCtx.AccountType,
		UserID:               userInfo.UserID,
		UserEmail:            userInfo.Email,
		UserSignature:        userInfo.UserPylonSignature,
		UserDisplayName:      userInfo.DisplayName,
		UserPhotoURL:         userInfo.PhotoURL,
		IsAdmin:              userInfo.Admin,
		Organizations:        organizations,
	}, nil
}

func (s *Service) Register(ctx context.Context, payload *gen.RegisterPayload) (err error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.SessionID == nil {
		return oops.C(oops.CodeUnauthorized)
	}

	if authCtx.ActiveOrganizationID != "" {
		return oops.E(oops.CodeInvalid, errors.New("user already has an active organization"), "user already has an active organization")
	}

	if payload.OrgName == "" {
		return oops.E(oops.CodeInvalid, errors.New("org name is required"), "org name is required")
	}

	// Only allow alphanumeric characters, spaces, hyphens, and underscores
	validOrgNameRegex := regexp.MustCompile(`^[a-zA-Z0-9\s-_]+$`)
	if !validOrgNameRegex.MatchString(payload.OrgName) {
		return oops.E(oops.CodeInvalid, errors.New("organization name contains invalid characters"), "organization name contains invalid characters")
	}

	info, err := s.sessions.CreateOrgFromSpeakeasy(ctx, *authCtx.SessionID, payload.OrgName)
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "error creating org").Log(ctx, s.logger)
	}

	if len(info.Organizations) == 0 {
		return oops.E(oops.CodeUnexpected, errors.New("no organizations returned from speakeasy"), "no organizations returned from speakeasy")
	}

	// invalid to insure we pull in the new org info on the next auth.info call
	if err := s.sessions.InvalidateUserInfoCache(ctx, authCtx.UserID); err != nil {
		return oops.E(oops.CodeUnexpected, err, "error invalidating user").Log(ctx, s.logger)
	}

	session := sessions.Session{
		SessionID:            *authCtx.SessionID,
		UserID:               authCtx.UserID,
		ActiveOrganizationID: info.Organizations[0].ID,
	}

	if err := s.sessions.StoreSession(ctx, session); err != nil {
		return oops.E(oops.CodeUnexpected, err, "error storing session").Log(ctx, s.logger)
	}

	if _, err := s.orgRepo.UpsertOrganizationMetadata(ctx, orgRepo.UpsertOrganizationMetadataParams{
		ID:   info.Organizations[0].ID,
		Name: info.Organizations[0].Name,
		Slug: info.Organizations[0].Slug,
	}); err != nil {
		return oops.E(oops.CodeUnexpected, err, "error upserting organization metadata").Log(ctx, s.logger)
	}

	return nil
}

func (s *Service) getProjectsOrSetupDefaults(ctx context.Context, organizationID string) ([]projectsRepo.Project, error) {
	projects, err := s.projectsRepo.ListProjectsByOrganization(ctx, organizationID)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error listing projects").Log(ctx, s.logger)
	}

	if len(projects) == 0 {
		project, err := s.createDefaultProject(ctx, organizationID)
		if err != nil {
			return nil, err
		}

		_, err = s.envRepo.CreateEnvironment(ctx, envRepo.CreateEnvironmentParams{
			OrganizationID: organizationID,
			ProjectID:      project.ID,
			Name:           "Default",
			Slug:           "default",
			Description:    conv.ToPGText("Default project for organization"),
		})
		if err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "error creating default environment").Log(ctx, s.logger)
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
	var pgErr *pgconn.PgError
	var empty projectsRepo.Project
	if err != nil {
		if errors.As(err, &pgErr) && pgErr.Code == pgerrcode.UniqueViolation {
			return empty, oops.E(oops.CodeConflict, nil, "project already exists")
		}
		return empty, oops.E(oops.CodeUnexpected, err, "error creating default project").Log(ctx, s.logger)
	}

	return project, nil
}
