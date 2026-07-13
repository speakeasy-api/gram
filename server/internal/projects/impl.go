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
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/otel/trace"
	goahttp "goa.design/goa/v3/http"
	"goa.design/goa/v3/security"

	srv "github.com/speakeasy-api/gram/server/gen/http/projects/server"
	gen "github.com/speakeasy-api/gram/server/gen/projects"
	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/auth"
	"github.com/speakeasy-api/gram/server/internal/auth/sessions"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/background"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	envrepo "github.com/speakeasy-api/gram/server/internal/environments/repo"
	"github.com/speakeasy-api/gram/server/internal/middleware"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/plugins"
	"github.com/speakeasy-api/gram/server/internal/projects/repo"
	tenv "github.com/speakeasy-api/gram/server/internal/temporal"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

type Service struct {
	tracer               trace.Tracer
	logger               *slog.Logger
	db                   *pgxpool.Pool
	repo                 *repo.Queries
	envRepo              *envrepo.Queries
	sessions             *sessions.Manager
	auth                 *auth.Auth
	authz                *authz.Engine
	audit                *audit.Logger
	temporalEnv          *tenv.Environment
	pluginsGitHubEnabled bool
}

var _ gen.Service = (*Service)(nil)

func NewService(
	logger *slog.Logger,
	tracerProvider trace.TracerProvider,
	db *pgxpool.Pool,
	sessions *sessions.Manager,
	authzEngine *authz.Engine,
	auditLogger *audit.Logger,
	temporalEnv *tenv.Environment,
	pluginsGitHubEnabled bool,
) *Service {
	logger = logger.With(attr.SlogComponent("projects"))

	return &Service{
		tracer:               tracerProvider.Tracer("github.com/speakeasy-api/gram/server/internal/projects"),
		logger:               logger,
		db:                   db,
		repo:                 repo.New(db),
		envRepo:              envrepo.New(db),
		sessions:             sessions,
		auth:                 auth.New(logger, db, sessions, authzEngine),
		authz:                authzEngine,
		audit:                auditLogger,
		temporalEnv:          temporalEnv,
		pluginsGitHubEnabled: pluginsGitHubEnabled,
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

func (s *Service) GetProject(ctx context.Context, payload *gen.GetProjectPayload) (*gen.GetProjectResult, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ActiveOrganizationID == "" {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	slug := string(payload.Slug)
	proj, err := s.repo.GetProjectBySlug(ctx, repo.GetProjectBySlugParams{
		Slug:           slug,
		OrganizationID: authCtx.ActiveOrganizationID,
	})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return nil, oops.C(oops.CodeNotFound)
	case err != nil:
		return nil, oops.E(oops.CodeUnexpected, err, "error getting project by slug").LogError(ctx, s.logger, attr.SlogProjectSlug(slug), attr.SlogOrganizationID(authCtx.ActiveOrganizationID))
	}

	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeProjectRead, ResourceKind: "", ResourceID: proj.ID.String(), Dimensions: nil}); err != nil {
		var shareableErr *oops.ShareableError
		if errors.As(err, &shareableErr) && shareableErr.Code == oops.CodeForbidden {
			return nil, oops.C(oops.CodeNotFound)
		}
		return nil, err
	}

	return &gen.GetProjectResult{
		Project: &gen.Project{
			ID:             proj.ID.String(),
			Name:           proj.Name,
			Slug:           types.Slug(proj.Slug),
			OrganizationID: proj.OrganizationID,
			LogoAssetID:    conv.FromNullableUUID(proj.LogoAssetID),
			CreatedAt:      proj.CreatedAt.Time.Format(time.RFC3339),
			UpdatedAt:      proj.UpdatedAt.Time.Format(time.RFC3339),
		},
	}, nil
}

func (s *Service) CreateProject(ctx context.Context, payload *gen.CreateProjectPayload) (*gen.CreateProjectResult, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.SessionID == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	if payload.OrganizationID != authCtx.ActiveOrganizationID {
		return nil, oops.E(oops.CodeForbidden, nil, "organization does not match active organization context")
	}

	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeOrgAdmin, ResourceKind: "", ResourceID: payload.OrganizationID, Dimensions: nil}); err != nil {
		return nil, err
	}

	userInfo, _, err := s.sessions.GetUserInfo(ctx, authCtx.UserID)
	if err != nil {
		return nil, fmt.Errorf("get session user info: %w", err)
	}

	if orgIdx := slices.IndexFunc(userInfo.Organizations, func(org sessions.Organization) bool {
		return org.ID == payload.OrganizationID
	}); orgIdx == -1 {
		return nil, oops.C(oops.CodeForbidden)
	}

	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error accessing projects").LogError(ctx, s.logger)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })

	pr := s.repo.WithTx(dbtx)
	er := s.envRepo.WithTx(dbtx)

	prj, err := pr.CreateProject(ctx, repo.CreateProjectParams{
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
		return nil, oops.E(oops.CodeUnexpected, err, "database error creating project").LogError(ctx, s.logger, attr.SlogOrganizationID(payload.OrganizationID), attr.SlogProjectName(payload.Name))
	case err != nil:
		return nil, oops.E(oops.CodeUnexpected, err, "unexpected error creating project").LogError(ctx, s.logger, attr.SlogOrganizationID(payload.OrganizationID), attr.SlogProjectName(payload.Name))
	}

	_, err = er.CreateEnvironment(ctx, envrepo.CreateEnvironmentParams{
		OrganizationID: payload.OrganizationID,
		ProjectID:      prj.ID,
		Name:           "Default",
		Slug:           "default",
		Description:    conv.ToPGText("Default project for organization"),
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error creating default environment").LogError(ctx, s.logger)
	}

	// Provision the project's Default plugin through the shared helper so it
	// takes the same create-and-seed path (including the org-wildcard audience
	// default) as the lazy-heal callers — no separate seeding to drift.
	ensured, err := plugins.EnsureDefaultPlugin(ctx, dbtx, payload.OrganizationID, prj.ID)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error creating default plugin").LogError(ctx, s.logger)
	}

	if ensured.Created {
		if err := s.audit.LogPluginCreate(ctx, dbtx, audit.LogPluginCreateEvent{
			OrganizationID:   payload.OrganizationID,
			ProjectID:        prj.ID,
			Actor:            urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID),
			ActorDisplayName: authCtx.Email,
			ActorSlug:        nil,
			PluginID:         ensured.Plugin.ID,
			PluginName:       ensured.Plugin.Name,
			PluginSlug:       ensured.Plugin.Slug,
		}); err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "error creating default plugin audit log").LogError(ctx, s.logger)
		}
	}

	if err := s.audit.LogProjectCreate(ctx, dbtx, audit.LogProjectCreateEvent{
		OrganizationID: payload.OrganizationID,
		ProjectID:      prj.ID,

		Actor:            urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID),
		ActorDisplayName: authCtx.Email,
		ActorSlug:        nil,

		ProjectName: prj.Name,
		ProjectSlug: prj.Slug,
	}); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error creating project creation audit log").LogError(ctx, s.logger)
	}

	if err := dbtx.Commit(ctx); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error saving project creation").LogError(ctx, s.logger)
	}

	// Best-effort: the marketplace repo isn't required for the project to
	// exist. No GitHub collaborators are added here — we don't have a
	// customer GitHub username yet; that's supplied later via the dashboard
	// publish/marketplace-settings flow. Uses a non-cancelable derived ctx so
	// the request returning (or its caller disconnecting) right after commit
	// can't drop the enqueue.
	if s.pluginsGitHubEnabled {
		enqueueCtx := context.WithoutCancel(ctx)
		if _, err := background.ExecutePluginInitialPublishWorkflow(enqueueCtx, s.temporalEnv, plugins.PublishProjectInput{
			ProjectID:       prj.ID,
			CreatedByUserID: authCtx.UserID,
			CommitMessage:   "Initial marketplace publish",
			SkipIfUnchanged: false,
		}); err != nil {
			s.logger.WarnContext(ctx, "failed to enqueue initial plugin publish", attr.SlogError(err))
		}
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

	if payload.OrganizationID != authCtx.ActiveOrganizationID {
		return nil, oops.E(oops.CodeForbidden, nil, "organization does not match active organization context")
	}

	userInfo, _, err := s.sessions.GetUserInfo(ctx, authCtx.UserID)
	if err != nil {
		return nil, fmt.Errorf("get session user info: %w", err)
	}

	if orgIdx := slices.IndexFunc(userInfo.Organizations, func(org sessions.Organization) bool {
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

	projectIDs := make([]string, 0, len(projects))
	for _, project := range projects {
		projectIDs = append(projectIDs, project.ID.String())
	}

	checks := make([]authz.Check, len(projectIDs))
	for i, id := range projectIDs {
		checks[i] = authz.Check{Scope: authz.ScopeProjectRead, ResourceID: id, ResourceKind: "", Dimensions: nil}
	}
	allowedProjectIDs, err := s.authz.Filter(ctx, checks)
	if err != nil {
		return nil, err
	}

	allowedProjects := make(map[string]struct{}, len(allowedProjectIDs))
	for _, projectID := range allowedProjectIDs {
		allowedProjects[projectID] = struct{}{}
	}

	entries := make([]*gen.ProjectEntry, 0, len(projects))
	for _, project := range projects {
		if _, ok := allowedProjects[project.ID.String()]; !ok {
			continue
		}

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

	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeProjectWrite, ResourceKind: "", ResourceID: authCtx.ProjectID.String(), Dimensions: nil}); err != nil {
		return nil, err
	}

	assetID, err := uuid.Parse(payload.AssetID)
	if err != nil {
		return nil, oops.E(oops.CodeInvalid, err, "error parsing asset ID").LogError(ctx, s.logger)
	}

	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error accessing projects").LogError(ctx, s.logger)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })

	pr := s.repo.WithTx(dbtx)

	existingRow, err := pr.GetProjectByID(ctx, *authCtx.ProjectID)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error getting project").LogError(ctx, s.logger, attr.SlogProjectID(authCtx.ProjectID.String()))
	}

	existing := toProject(existingRow)

	updatedProject, err := pr.UploadProjectLogo(ctx, repo.UploadProjectLogoParams{
		ProjectID:   *authCtx.ProjectID,
		LogoAssetID: uuid.NullUUID{UUID: assetID, Valid: true},
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error updating project logo").LogError(ctx, s.logger)
	}

	projectResponse := toProject(updatedProject)

	if err := s.audit.LogProjectUpdate(ctx, dbtx, audit.LogProjectUpdateEvent{
		OrganizationID: updatedProject.OrganizationID,
		ProjectID:      updatedProject.ID,

		Actor:            urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID),
		ActorDisplayName: authCtx.Email,
		ActorSlug:        nil,

		ProjectName: updatedProject.Name,
		ProjectSlug: updatedProject.Slug,

		ProjectSnapshotBefore: existing,
		ProjectSnapshotAfter:  projectResponse,
	}); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error creating project update audit log").LogError(ctx, s.logger)
	}

	if err := dbtx.Commit(ctx); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error saving project").LogError(ctx, s.logger)
	}

	return &gen.SetProjectLogoResult{
		Project: projectResponse,
	}, nil
}

func (s *Service) ListAllowedOrigins(ctx context.Context, payload *gen.ListAllowedOriginsPayload) (*gen.ListAllowedOriginsResult, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeProjectRead, ResourceKind: "", ResourceID: authCtx.ProjectID.String(), Dimensions: nil}); err != nil {
		return nil, err
	}

	allowedOrigins, err := s.repo.ListAllowedOriginsByProjectID(ctx, *authCtx.ProjectID)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error listing allowed origins").LogError(ctx, s.logger, attr.SlogProjectID(authCtx.ProjectID.String()))
	}

	entries := make([]*gen.AllowedOrigin, 0, len(allowedOrigins))
	for _, origin := range allowedOrigins {
		entries = append(entries, &gen.AllowedOrigin{
			ID:        origin.ID.String(),
			ProjectID: origin.ProjectID.String(),
			Origin:    origin.Origin,
			Status:    origin.Status,
			CreatedAt: origin.CreatedAt.Time.Format(time.RFC3339),
			UpdatedAt: origin.UpdatedAt.Time.Format(time.RFC3339),
		})
	}

	return &gen.ListAllowedOriginsResult{
		AllowedOrigins: entries,
	}, nil
}

func (s *Service) UpsertAllowedOrigin(ctx context.Context, payload *gen.UpsertAllowedOriginPayload) (*gen.UpsertAllowedOriginResult, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeProjectWrite, ResourceKind: "", ResourceID: authCtx.ProjectID.String(), Dimensions: nil}); err != nil {
		return nil, err
	}

	// Use the status from payload or default to "pending"
	status := payload.Status
	if status == "" {
		status = "pending"
	}

	allowedOrigin, err := s.repo.UpsertAllowedOrigin(ctx, repo.UpsertAllowedOriginParams{
		ProjectID: *authCtx.ProjectID,
		Origin:    payload.Origin,
		Status:    status,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error upserting allowed origin").LogError(ctx, s.logger, attr.SlogProjectID(authCtx.ProjectID.String()))
	}

	return &gen.UpsertAllowedOriginResult{
		AllowedOrigin: &gen.AllowedOrigin{
			ID:        allowedOrigin.ID.String(),
			ProjectID: allowedOrigin.ProjectID.String(),
			Origin:    allowedOrigin.Origin,
			Status:    allowedOrigin.Status,
			CreatedAt: allowedOrigin.CreatedAt.Time.Format(time.RFC3339),
			UpdatedAt: allowedOrigin.UpdatedAt.Time.Format(time.RFC3339),
		},
	}, nil
}

func (s *Service) DeleteProject(ctx context.Context, payload *gen.DeleteProjectPayload) error {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.SessionID == nil {
		return oops.C(oops.CodeUnauthorized)
	}

	projectID, err := uuid.Parse(payload.ID)
	if err != nil {
		return oops.E(oops.CodeInvalid, err, "invalid project id").LogError(ctx, s.logger)
	}

	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeOrgAdmin, ResourceKind: "", ResourceID: authCtx.ActiveOrganizationID, Dimensions: nil}); err != nil {
		return err
	}

	err = s.auth.CheckProjectAccess(ctx, s.logger, projectID)
	if err != nil {
		return oops.E(oops.CodeForbidden, err, "forbidden")
	}

	project, err := s.repo.GetProjectByID(ctx, projectID)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return nil
	case err != nil:
		return oops.E(oops.CodeUnexpected, err, "error retrieving project").LogError(ctx, s.logger)
	}

	// The first project (ordered by id ASC) is the default project
	if project.Slug == "default" {
		return oops.E(oops.CodeInvalid, nil, "cannot delete the default project")
	}

	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "error accessing projects").LogError(ctx, s.logger)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })

	pr := s.repo.WithTx(dbtx)

	_, err = pr.DeleteProject(ctx, projectID)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return nil // Return successfully even if the project was already deleted
	case err != nil:
		return oops.E(oops.CodeUnexpected, err, "error deleting project").LogError(ctx, s.logger, attr.SlogProjectID(payload.ID))
	}

	if err := s.audit.LogProjectDelete(ctx, dbtx, audit.LogProjectDeleteEvent{
		OrganizationID: authCtx.ActiveOrganizationID,
		ProjectID:      projectID,

		Actor:            urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID),
		ActorDisplayName: authCtx.Email,
		ActorSlug:        nil,

		ProjectName: project.Name,
		ProjectSlug: project.Slug,
	}); err != nil {
		return oops.E(oops.CodeUnexpected, err, "error creating project deletion audit log").LogError(ctx, s.logger)
	}

	if err := dbtx.Commit(ctx); err != nil {
		return oops.E(oops.CodeUnexpected, err, "error saving project deletion").LogError(ctx, s.logger)
	}

	return nil
}

func (s *Service) SetOrganizationWhitelist(ctx context.Context, payload *gen.SetOrganizationWhitelistPayload) error {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil {
		return oops.C(oops.CodeUnauthorized)
	}

	// Check that the API key is from the speakeasy-team organization
	const speakeasyTeamOrgID = "5a25158b-24dc-4d49-b03d-e85acfbea59c"
	if authCtx.ActiveOrganizationID != speakeasyTeamOrgID {
		return oops.E(oops.CodeUnauthorized, nil, "only speakeasy-team can set organization whitelist status").LogError(ctx, s.logger, attr.SlogOrganizationID(authCtx.ActiveOrganizationID))
	}

	err := s.repo.SetOrganizationWhitelist(ctx, repo.SetOrganizationWhitelistParams{
		OrganizationID: payload.OrganizationID,
		Whitelisted:    payload.Whitelisted,
	})
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "error setting organization whitelist status").LogError(ctx, s.logger, attr.SlogOrganizationID(payload.OrganizationID))
	}

	return nil
}

func toProject(p repo.Project) *gen.Project {
	return &gen.Project{
		ID:             p.ID.String(),
		Name:           p.Name,
		Slug:           types.Slug(p.Slug),
		OrganizationID: p.OrganizationID,
		LogoAssetID:    conv.FromNullableUUID(p.LogoAssetID),
		CreatedAt:      p.CreatedAt.Time.Format(time.RFC3339),
		UpdatedAt:      p.UpdatedAt.Time.Format(time.RFC3339),
	}
}
