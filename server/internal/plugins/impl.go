package plugins

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgerrcode"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/otel/trace"
	goahttp "goa.design/goa/v3/http"
	"goa.design/goa/v3/security"

	srv "github.com/speakeasy-api/gram/server/gen/http/plugins/server"
	gen "github.com/speakeasy-api/gram/server/gen/plugins"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/auth"
	"github.com/speakeasy-api/gram/server/internal/auth/sessions"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	keysrepo "github.com/speakeasy-api/gram/server/internal/keys/repo"
	mcpmetarepo "github.com/speakeasy-api/gram/server/internal/mcpmetadata/repo"
	"github.com/speakeasy-api/gram/server/internal/middleware"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/plugins/repo"
	ghclient "github.com/speakeasy-api/gram/server/internal/thirdparty/github"
	toolsetsrepo "github.com/speakeasy-api/gram/server/internal/toolsets/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

var validPrincipalURN = regexp.MustCompile(`^(\*|role:[a-zA-Z0-9_-]+|user:[a-zA-Z0-9_-]+)$`)

// GitHub usernames: 1-39 chars, starts with alphanumeric, alphanumeric or hyphen.
// Strict enough to prevent path traversal in API URL construction.
var validGitHubUsername = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9-]{0,38}$`)

// GitHubPublisher is the interface for creating repos and pushing files to GitHub.
type GitHubPublisher interface {
	CreateRepo(ctx context.Context, installationID int64, org, name string, private bool) error
	PushFiles(ctx context.Context, installationID int64, owner, repo, branch, commitMsg string, files map[string][]byte) (string, error)
	AddCollaborator(ctx context.Context, installationID int64, owner, repo, username, permission string) error
}

// GitHubConfig holds the configured GitHub client and the Gram-owned org
// where plugin repos are created. Nil means GitHub publishing is disabled.
type GitHubConfig struct {
	Client         GitHubPublisher
	Org            string
	InstallationID int64
}

// GitHubConfigInput is the raw deployment-time configuration for plugin
// GitHub publishing. All fields must be set together (the feature is on)
// or all must be unset (the feature is off). Pass to NewGitHubConfig.
type GitHubConfigInput struct {
	AppID          int64
	PrivateKey     string
	Org            string
	InstallationID int64
	HTTPClient     *guardian.HTTPClient
}

// NewGitHubConfig validates a GitHubConfigInput holistically and returns:
//   - (nil, nil)        when no fields are set: feature is disabled
//   - (config, nil)     when all fields are set: feature is enabled
//   - (nil, error)      when only some fields are set: deployment is misconfigured
//
// The all-or-nothing check prevents the silent-disable footgun where setting
// three of four env vars (e.g. forgetting GRAM_PLUGINS_GITHUB_APP_ID) leaves
// the deployment running with publishing inexplicably off.
func NewGitHubConfig(in GitHubConfigInput) (*GitHubConfig, error) {
	set := 0
	missing := []string{}
	if in.AppID != 0 {
		set++
	} else {
		missing = append(missing, "plugins-github-app-id")
	}
	if in.PrivateKey != "" {
		set++
	} else {
		missing = append(missing, "plugins-github-private-key")
	}
	if in.Org != "" {
		set++
	} else {
		missing = append(missing, "plugins-github-org")
	}
	if in.InstallationID != 0 {
		set++
	} else {
		missing = append(missing, "plugins-github-installation-id")
	}

	switch set {
	case 0:
		return nil, nil
	case 4:
		client, err := ghclient.NewClient(in.AppID, []byte(in.PrivateKey), in.HTTPClient)
		if err != nil {
			return nil, fmt.Errorf("create github client: %w", err)
		}
		return &GitHubConfig{
			Client:         client,
			Org:            in.Org,
			InstallationID: in.InstallationID,
		}, nil
	default:
		return nil, fmt.Errorf("plugin github publishing requires all of plugins-github-app-id, plugins-github-private-key, plugins-github-org, plugins-github-installation-id; missing: %s", strings.Join(missing, ", "))
	}
}

type Service struct {
	tracer    trace.Tracer
	logger    *slog.Logger
	db        *pgxpool.Pool
	repo      *repo.Queries
	auth      *auth.Auth
	authz     *authz.Engine
	github    *GitHubConfig
	serverURL string
	keyPrefix string
}

var _ gen.Service = (*Service)(nil)
var _ gen.Auther = (*Service)(nil)

func NewService(
	logger *slog.Logger,
	tracerProvider trace.TracerProvider,
	db *pgxpool.Pool,
	sessions *sessions.Manager,
	authzEngine *authz.Engine,
	github *GitHubConfig,
	env string,
	serverURL string,
) *Service {
	logger = logger.With(attr.SlogComponent("plugins"))

	return &Service{
		tracer:    tracerProvider.Tracer("github.com/speakeasy-api/gram/server/internal/plugins"),
		logger:    logger,
		db:        db,
		repo:      repo.New(db),
		auth:      auth.New(logger, db, sessions, authzEngine),
		authz:     authzEngine,
		github:    github,
		serverURL: serverURL,
		keyPrefix: auth.APIKeyPrefix(env),
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

// --- Plugin CRUD ---

func (s *Service) ListPlugins(ctx context.Context, payload *gen.ListPluginsPayload) (*gen.ListPluginsResult, error) {
	ac, err := s.authContext(ctx)
	if err != nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeOrgRead, ResourceKind: "", ResourceID: ac.ActiveOrganizationID, Dimensions: nil}); err != nil {
		return nil, err
	}

	rows, err := s.repo.ListPlugins(ctx, repo.ListPluginsParams{
		OrganizationID: ac.ActiveOrganizationID,
		ProjectID:      *ac.ProjectID,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list plugins").Log(ctx, s.logger)
	}

	plugins := make([]*gen.Plugin, 0, len(rows))
	for _, r := range rows {
		plugins = append(plugins, &gen.Plugin{
			ID:              r.ID.String(),
			Name:            r.Name,
			Slug:            r.Slug,
			Description:     conv.FromPGText[string](r.Description),
			ServerCount:     &r.ServerCount,
			AssignmentCount: &r.AssignmentCount,
			Servers:         nil,
			Assignments:     nil,
			CreatedAt:       formatTime(r.CreatedAt),
			UpdatedAt:       formatTime(r.UpdatedAt),
		})
	}

	return &gen.ListPluginsResult{Plugins: plugins}, nil
}

func (s *Service) GetPlugin(ctx context.Context, payload *gen.GetPluginPayload) (*gen.Plugin, error) {
	ac, err := s.authContext(ctx)
	if err != nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeOrgRead, ResourceKind: "", ResourceID: ac.ActiveOrganizationID, Dimensions: nil}); err != nil {
		return nil, err
	}

	pluginID, err := uuid.Parse(payload.ID)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid plugin id").Log(ctx, s.logger)
	}

	plugin, err := s.repo.GetPlugin(ctx, repo.GetPluginParams{
		ID:             pluginID,
		OrganizationID: ac.ActiveOrganizationID,
		ProjectID:      *ac.ProjectID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, oops.C(oops.CodeNotFound)
		}
		return nil, oops.E(oops.CodeUnexpected, err, "get plugin").Log(ctx, s.logger)
	}

	servers, err := s.repo.ListPluginServers(ctx, pluginID)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list plugin servers").Log(ctx, s.logger)
	}

	assignments, err := s.repo.ListPluginAssignments(ctx, pluginID)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list plugin assignments").Log(ctx, s.logger)
	}

	return pluginToGen(plugin, servers, assignments), nil
}

func (s *Service) CreatePlugin(ctx context.Context, payload *gen.CreatePluginPayload) (*gen.Plugin, error) {
	ac, err := s.authContext(ctx)
	if err != nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeOrgAdmin, ResourceKind: "", ResourceID: ac.ActiveOrganizationID, Dimensions: nil}); err != nil {
		return nil, err
	}

	var slug string
	if payload.Slug != nil && *payload.Slug != "" {
		slug = conv.ToSlug(*payload.Slug)
		if slug != *payload.Slug {
			return nil, oops.E(oops.CodeBadRequest, nil, "invalid slug: must be non-empty and contain only lowercase alphanumeric characters and hyphens")
		}
	} else {
		slug = conv.ToSlug(payload.Name)
	}
	if slug == "" {
		return nil, oops.E(oops.CodeBadRequest, nil, "plugin name must produce a valid slug")
	}

	plugin, err := s.repo.CreatePlugin(ctx, repo.CreatePluginParams{
		OrganizationID: ac.ActiveOrganizationID,
		ProjectID:      *ac.ProjectID,
		Name:           payload.Name,
		Slug:           slug,
		Description:    conv.PtrToPGText(payload.Description),
	})
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == pgerrcode.UniqueViolation {
			return nil, oops.E(oops.CodeConflict, nil, "a plugin with this slug already exists")
		}
		return nil, oops.E(oops.CodeUnexpected, err, "create plugin").Log(ctx, s.logger)
	}

	return pluginToGen(plugin, nil, nil), nil
}

func (s *Service) UpdatePlugin(ctx context.Context, payload *gen.UpdatePluginPayload) (*gen.Plugin, error) {
	ac, err := s.authContext(ctx)
	if err != nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeOrgAdmin, ResourceKind: "", ResourceID: ac.ActiveOrganizationID, Dimensions: nil}); err != nil {
		return nil, err
	}

	pluginID, err := uuid.Parse(payload.ID)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid plugin id").Log(ctx, s.logger)
	}

	slug := conv.ToSlug(payload.Slug)
	if slug == "" || slug != payload.Slug {
		return nil, oops.E(oops.CodeBadRequest, nil, "invalid slug: must be non-empty and contain only lowercase alphanumeric characters and hyphens")
	}

	plugin, err := s.repo.UpdatePlugin(ctx, repo.UpdatePluginParams{
		ID:             pluginID,
		OrganizationID: ac.ActiveOrganizationID,
		ProjectID:      *ac.ProjectID,
		Name:           payload.Name,
		Slug:           slug,
		Description:    conv.PtrToPGText(payload.Description),
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, oops.C(oops.CodeNotFound)
		}
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == pgerrcode.UniqueViolation {
			return nil, oops.E(oops.CodeConflict, nil, "a plugin with this slug already exists")
		}
		return nil, oops.E(oops.CodeUnexpected, err, "update plugin").Log(ctx, s.logger)
	}

	servers, err := s.repo.ListPluginServers(ctx, pluginID)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list plugin servers").Log(ctx, s.logger)
	}

	assignments, err := s.repo.ListPluginAssignments(ctx, pluginID)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list plugin assignments").Log(ctx, s.logger)
	}

	return pluginToGen(plugin, servers, assignments), nil
}

func (s *Service) DeletePlugin(ctx context.Context, payload *gen.DeletePluginPayload) error {
	ac, err := s.authContext(ctx)
	if err != nil {
		return oops.C(oops.CodeUnauthorized)
	}

	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeOrgAdmin, ResourceKind: "", ResourceID: ac.ActiveOrganizationID, Dimensions: nil}); err != nil {
		return err
	}

	pluginID, err := uuid.Parse(payload.ID)
	if err != nil {
		return oops.E(oops.CodeBadRequest, err, "invalid plugin id").Log(ctx, s.logger)
	}

	// Verify the plugin belongs to this project before mutating.
	if _, err := s.repo.GetPlugin(ctx, repo.GetPluginParams{ID: pluginID, OrganizationID: ac.ActiveOrganizationID, ProjectID: *ac.ProjectID}); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return oops.C(oops.CodeNotFound)
		}
		return oops.E(oops.CodeUnexpected, err, "verify plugin ownership").Log(ctx, s.logger)
	}

	tx, err := s.db.Begin(ctx)
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "begin transaction").Log(ctx, s.logger)
	}
	defer o11y.NoLogDefer(func() error { return tx.Rollback(ctx) })

	txRepo := s.repo.WithTx(tx)

	if err := txRepo.SoftDeletePluginServers(ctx, pluginID); err != nil {
		return oops.E(oops.CodeUnexpected, err, "soft-delete plugin servers").Log(ctx, s.logger)
	}

	if _, err := txRepo.RemoveAllPluginAssignments(ctx, pluginID); err != nil {
		return oops.E(oops.CodeUnexpected, err, "remove plugin assignments").Log(ctx, s.logger)
	}

	if err := txRepo.DeletePlugin(ctx, repo.DeletePluginParams{
		ID:             pluginID,
		OrganizationID: ac.ActiveOrganizationID,
		ProjectID:      *ac.ProjectID,
	}); err != nil {
		return oops.E(oops.CodeUnexpected, err, "delete plugin").Log(ctx, s.logger)
	}

	if err := tx.Commit(ctx); err != nil {
		return oops.E(oops.CodeUnexpected, err, "commit transaction").Log(ctx, s.logger)
	}
	return nil
}

// --- Plugin Servers ---

func (s *Service) AddPluginServer(ctx context.Context, payload *gen.AddPluginServerPayload) (*gen.PluginServer, error) {
	ac, err := s.authContext(ctx)
	if err != nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeOrgAdmin, ResourceKind: "", ResourceID: ac.ActiveOrganizationID, Dimensions: nil}); err != nil {
		return nil, err
	}

	pluginID, err := uuid.Parse(payload.PluginID)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid plugin id").Log(ctx, s.logger)
	}

	// Verify the plugin belongs to this project.
	if _, err := s.repo.GetPlugin(ctx, repo.GetPluginParams{ID: pluginID, OrganizationID: ac.ActiveOrganizationID, ProjectID: *ac.ProjectID}); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, oops.C(oops.CodeNotFound)
		}
		return nil, oops.E(oops.CodeUnexpected, err, "verify plugin ownership").Log(ctx, s.logger)
	}

	toolsetID, err := uuid.Parse(payload.ToolsetID)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid toolset id").Log(ctx, s.logger)
	}

	// Verify the toolset exists and belongs to the same project.
	toolset, err := toolsetsrepo.New(s.db).GetToolsetByIDAndProject(ctx, toolsetsrepo.GetToolsetByIDAndProjectParams{
		ID:        toolsetID,
		ProjectID: *ac.ProjectID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, oops.E(oops.CodeBadRequest, nil, "toolset not found")
		}
		return nil, oops.E(oops.CodeUnexpected, err, "verify toolset").Log(ctx, s.logger)
	}
	if !toolset.McpEnabled || !toolset.McpSlug.Valid || toolset.McpSlug.String == "" {
		return nil, oops.E(oops.CodeBadRequest, nil, "toolset does not have MCP enabled")
	}

	row, err := s.repo.AddPluginServer(ctx, repo.AddPluginServerParams{
		PluginID:    pluginID,
		ToolsetID:   toolsetID,
		DisplayName: payload.DisplayName,
		Policy:      payload.Policy,
		SortOrder:   payload.SortOrder,
	})
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == pgerrcode.UniqueViolation {
			return nil, oops.E(oops.CodeConflict, nil, "a server with this display name already exists in the plugin")
		}
		return nil, oops.E(oops.CodeUnexpected, err, "add plugin server").Log(ctx, s.logger)
	}

	return pluginServerToGen(row), nil
}

func (s *Service) UpdatePluginServer(ctx context.Context, payload *gen.UpdatePluginServerPayload) (*gen.PluginServer, error) {
	ac, err := s.authContext(ctx)
	if err != nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeOrgAdmin, ResourceKind: "", ResourceID: ac.ActiveOrganizationID, Dimensions: nil}); err != nil {
		return nil, err
	}

	serverID, err := uuid.Parse(payload.ID)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid server id").Log(ctx, s.logger)
	}
	pluginID, err := uuid.Parse(payload.PluginID)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid plugin id").Log(ctx, s.logger)
	}

	// Verify the plugin belongs to this project.
	if _, err := s.repo.GetPlugin(ctx, repo.GetPluginParams{ID: pluginID, OrganizationID: ac.ActiveOrganizationID, ProjectID: *ac.ProjectID}); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, oops.C(oops.CodeNotFound)
		}
		return nil, oops.E(oops.CodeUnexpected, err, "verify plugin ownership").Log(ctx, s.logger)
	}

	row, err := s.repo.UpdatePluginServer(ctx, repo.UpdatePluginServerParams{
		ID:          serverID,
		PluginID:    pluginID,
		DisplayName: payload.DisplayName,
		Policy:      payload.Policy,
		SortOrder:   payload.SortOrder,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, oops.C(oops.CodeNotFound)
		}
		return nil, oops.E(oops.CodeUnexpected, err, "update plugin server").Log(ctx, s.logger)
	}

	return pluginServerToGen(row), nil
}

func (s *Service) RemovePluginServer(ctx context.Context, payload *gen.RemovePluginServerPayload) error {
	ac, err := s.authContext(ctx)
	if err != nil {
		return oops.C(oops.CodeUnauthorized)
	}

	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeOrgAdmin, ResourceKind: "", ResourceID: ac.ActiveOrganizationID, Dimensions: nil}); err != nil {
		return err
	}

	serverID, err := uuid.Parse(payload.ID)
	if err != nil {
		return oops.E(oops.CodeBadRequest, err, "invalid server id").Log(ctx, s.logger)
	}
	pluginID, err := uuid.Parse(payload.PluginID)
	if err != nil {
		return oops.E(oops.CodeBadRequest, err, "invalid plugin id").Log(ctx, s.logger)
	}

	// Verify the plugin belongs to this project.
	if _, err := s.repo.GetPlugin(ctx, repo.GetPluginParams{ID: pluginID, OrganizationID: ac.ActiveOrganizationID, ProjectID: *ac.ProjectID}); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return oops.C(oops.CodeNotFound)
		}
		return oops.E(oops.CodeUnexpected, err, "verify plugin ownership").Log(ctx, s.logger)
	}

	if err := s.repo.RemovePluginServer(ctx, repo.RemovePluginServerParams{
		ID:       serverID,
		PluginID: pluginID,
	}); err != nil {
		return oops.E(oops.CodeUnexpected, err, "remove plugin server").Log(ctx, s.logger)
	}
	return nil
}

// --- Assignments ---

func (s *Service) SetPluginAssignments(ctx context.Context, payload *gen.SetPluginAssignmentsPayload) (*gen.SetPluginAssignmentsResult, error) {
	ac, err := s.authContext(ctx)
	if err != nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeOrgAdmin, ResourceKind: "", ResourceID: ac.ActiveOrganizationID, Dimensions: nil}); err != nil {
		return nil, err
	}

	pluginID, err := uuid.Parse(payload.PluginID)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid plugin id").Log(ctx, s.logger)
	}

	// Verify the plugin belongs to this project.
	if _, err := s.repo.GetPlugin(ctx, repo.GetPluginParams{ID: pluginID, OrganizationID: ac.ActiveOrganizationID, ProjectID: *ac.ProjectID}); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, oops.C(oops.CodeNotFound)
		}
		return nil, oops.E(oops.CodeUnexpected, err, "verify plugin ownership").Log(ctx, s.logger)
	}

	for _, urn := range payload.PrincipalUrns {
		if !validPrincipalURN.MatchString(urn) {
			return nil, oops.E(oops.CodeBadRequest, nil, "invalid principal URN: %s", urn)
		}
	}

	tx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "begin transaction").Log(ctx, s.logger)
	}
	defer o11y.NoLogDefer(func() error { return tx.Rollback(ctx) })

	txRepo := s.repo.WithTx(tx)

	if _, err := txRepo.RemoveAllPluginAssignments(ctx, pluginID); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "remove existing assignments").Log(ctx, s.logger)
	}

	assignments := make([]*gen.PluginAssignment, 0, len(payload.PrincipalUrns))
	for _, urn := range payload.PrincipalUrns {
		row, err := txRepo.AddPluginAssignment(ctx, repo.AddPluginAssignmentParams{
			PluginID:       pluginID,
			OrganizationID: ac.ActiveOrganizationID,
			PrincipalUrn:   urn,
		})
		if err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "add plugin assignment").Log(ctx, s.logger)
		}
		assignments = append(assignments, pluginAssignmentToGen(row))
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "commit transaction").Log(ctx, s.logger)
	}

	return &gen.SetPluginAssignmentsResult{Assignments: assignments}, nil
}

func (s *Service) DownloadPluginPackage(ctx context.Context, payload *gen.DownloadPluginPackagePayload) (*gen.DownloadPluginPackageResult, io.ReadCloser, error) {
	ac, err := s.authContext(ctx)
	if err != nil {
		return nil, nil, oops.C(oops.CodeUnauthorized)
	}

	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeOrgRead, ResourceKind: "", ResourceID: ac.ActiveOrganizationID, Dimensions: nil}); err != nil {
		return nil, nil, err
	}

	pluginID, err := uuid.Parse(payload.PluginID)
	if err != nil {
		return nil, nil, oops.E(oops.CodeBadRequest, err, "invalid plugin id").Log(ctx, s.logger)
	}

	// Look up the plugin to get its slug.
	dbPlugin, err := s.repo.GetPlugin(ctx, repo.GetPluginParams{
		ID:             pluginID,
		OrganizationID: ac.ActiveOrganizationID,
		ProjectID:      *ac.ProjectID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil, oops.C(oops.CodeNotFound)
		}
		return nil, nil, oops.E(oops.CodeUnexpected, err, "get plugin").Log(ctx, s.logger)
	}

	// Resolve all plugin infos and find the matching one.
	allInfos, err := s.resolvePluginInfos(ctx, *ac.ProjectID)
	if err != nil {
		return nil, nil, err
	}

	var pluginInfo *PluginInfo
	for i := range allInfos {
		if allInfos[i].Slug == dbPlugin.Slug {
			pluginInfo = &allInfos[i]
			break
		}
	}
	if pluginInfo == nil {
		// Plugin exists but has no servers — generate an empty plugin.
		pluginInfo = &PluginInfo{
			Name:        dbPlugin.Name,
			Slug:        dbPlugin.Slug,
			Description: conv.FromPGTextOrEmpty[string](dbPlugin.Description),
			Servers:     nil,
		}
	}

	cfg := s.generateConfig(ctx, ac.ActiveOrganizationID, ac.OrganizationSlug)

	files, err := GenerateSinglePluginPackage(*pluginInfo, cfg, payload.Platform)
	if err != nil {
		return nil, nil, oops.E(oops.CodeUnexpected, err, "generate plugin package").Log(ctx, s.logger)
	}

	var buf bytes.Buffer
	w := zip.NewWriter(&buf)
	// Sort paths for deterministic ZIP output.
	paths := make([]string, 0, len(files))
	for path := range files {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	for _, path := range paths {
		content := files[path]
		f, err := w.Create(path)
		if err != nil {
			return nil, nil, oops.E(oops.CodeUnexpected, err, "create zip entry").Log(ctx, s.logger)
		}
		if _, err := f.Write(content); err != nil {
			return nil, nil, oops.E(oops.CodeUnexpected, err, "write zip entry").Log(ctx, s.logger)
		}
	}
	if err := w.Close(); err != nil {
		return nil, nil, oops.E(oops.CodeUnexpected, err, "close zip writer").Log(ctx, s.logger)
	}

	return &gen.DownloadPluginPackageResult{
		ContentType:        "application/zip",
		ContentDisposition: fmt.Sprintf(`attachment; filename="%s.zip"`, dbPlugin.Slug),
	}, io.NopCloser(bytes.NewReader(buf.Bytes())), nil
}

// --- Publish & Distribution ---

func (s *Service) GetPublishStatus(ctx context.Context, payload *gen.GetPublishStatusPayload) (*gen.PublishStatusResult, error) {
	ac, err := s.authContext(ctx)
	if err != nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeOrgRead, ResourceKind: "", ResourceID: ac.ActiveOrganizationID, Dimensions: nil}); err != nil {
		return nil, err
	}

	result := &gen.PublishStatusResult{
		Configured: s.github != nil,
		Connected:  false,
		RepoOwner:  nil,
		RepoName:   nil,
		RepoURL:    nil,
	}

	if s.github != nil {
		conn, err := s.repo.GetGitHubConnection(ctx, *ac.ProjectID)
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return nil, oops.E(oops.CodeUnexpected, err, "get github connection").Log(ctx, s.logger)
		}
		if err == nil {
			result.Connected = true
			result.RepoOwner = &conn.RepoOwner
			result.RepoName = &conn.RepoName
			repoURL := fmt.Sprintf("https://github.com/%s/%s", conn.RepoOwner, conn.RepoName)
			result.RepoURL = &repoURL
		}
	}

	return result, nil
}

func (s *Service) PublishPlugins(ctx context.Context, payload *gen.PublishPluginsPayload) (*gen.PublishPluginsResult, error) {
	ac, err := s.authContext(ctx)
	if err != nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeOrgAdmin, ResourceKind: "", ResourceID: ac.ActiveOrganizationID, Dimensions: nil}); err != nil {
		return nil, err
	}

	if s.github == nil {
		return nil, oops.E(oops.CodeBadRequest, nil, "GitHub publishing is not configured")
	}

	pluginInfos, err := s.resolvePluginInfos(ctx, *ac.ProjectID)
	if err != nil {
		return nil, err
	}

	if len(pluginInfos) == 0 {
		return nil, oops.E(oops.CodeBadRequest, nil, "no plugins with servers to publish")
	}

	if payload.GithubUsername != nil && *payload.GithubUsername != "" && !validGitHubUsername.MatchString(*payload.GithubUsername) {
		return nil, oops.E(oops.CodeBadRequest, nil, "invalid github username")
	}

	// PublishPlugins is session-only — repo names embed the project slug,
	// which API key auth doesn't populate.
	if ac.ProjectSlug == nil {
		return nil, oops.E(oops.CodeUnauthorized, nil, "publish requires a session-authenticated context")
	}

	candidate, err := s.buildPluginAPIKeyCandidate()
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "build plugin api key").Log(ctx, s.logger)
	}

	cfg := s.generateConfig(ctx, ac.ActiveOrganizationID, ac.OrganizationSlug)
	cfg.APIKey = candidate.fullKey

	files, err := GeneratePluginPackages(pluginInfos, cfg)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "generate plugin packages").Log(ctx, s.logger)
	}

	// GitHub repo owner/name are case-insensitive. Normalize at the boundary
	// so the rows we persist round-trip cleanly through the case-insensitive
	// unique index on (installation_id, LOWER(repo_owner), LOWER(repo_name)).
	repoOwner := strings.ToLower(s.github.Org)
	repoName := strings.ToLower(ac.OrganizationSlug + "-" + *ac.ProjectSlug + "-plugins")

	if err := s.github.Client.CreateRepo(ctx, s.github.InstallationID, repoOwner, repoName, true); err != nil {
		return nil, oops.E(oops.CodeGatewayError, err, "create github repo").Log(ctx, s.logger)
	}

	_, err = s.github.Client.PushFiles(
		ctx,
		s.github.InstallationID,
		repoOwner,
		repoName,
		"main",
		"Update plugin packages",
		files,
	)
	if err != nil {
		return nil, oops.E(oops.CodeGatewayError, err, "push plugin files to GitHub").Log(ctx, s.logger)
	}

	if payload.GithubUsername != nil && *payload.GithubUsername != "" {
		if err := s.github.Client.AddCollaborator(ctx, s.github.InstallationID, repoOwner, repoName, *payload.GithubUsername, "pull"); err != nil {
			s.logger.WarnContext(ctx, "failed to add collaborator (non-fatal)",
				attr.SlogOrganizationID(ac.ActiveOrganizationID),
				attr.SlogError(err),
			)
		}
	}

	// Persist the API key, audit log, and github connection atomically only
	// after the GitHub publish has succeeded. This prevents leaking valid
	// consumer credentials when GitHub fails. If this transaction itself
	// fails, the published repo contains a key string with no DB record —
	// re-publish overwrites it with a fresh valid key.
	if err := s.persistPluginAPIKey(ctx, ac, candidate, repoOwner, repoName); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "persist plugin api key").Log(ctx, s.logger)
	}

	repoURL := fmt.Sprintf("https://github.com/%s/%s", repoOwner, repoName)
	return &gen.PublishPluginsResult{RepoURL: repoURL}, nil
}

// pluginAPIKeyCandidate is the in-memory shape of a generated plugin API key
// that has not yet been persisted to the database. It is built before GitHub
// publishing so the key can be embedded in the published files, and only
// persisted if the GitHub side succeeds.
type pluginAPIKeyCandidate struct {
	fullKey   string
	keyHash   string
	keyPrefix string
	keyName   string
}

// buildPluginAPIKeyCandidate generates an API key in memory without writing
// to the database. The caller must subsequently call persistPluginAPIKey to
// commit the key.
func (s *Service) buildPluginAPIKeyCandidate() (pluginAPIKeyCandidate, error) {
	tokenBytes := make([]byte, 32)
	if _, err := rand.Read(tokenBytes); err != nil {
		return pluginAPIKeyCandidate{}, fmt.Errorf("generate token: %w", err)
	}
	token := hex.EncodeToString(tokenBytes)
	fullKey := s.keyPrefix + token

	keyHash, err := auth.GetAPIKeyHash(fullKey)
	if err != nil {
		return pluginAPIKeyCandidate{}, fmt.Errorf("hash key: %w", err)
	}

	return pluginAPIKeyCandidate{
		fullKey:   fullKey,
		keyHash:   keyHash,
		keyPrefix: s.keyPrefix + token[:5],
		keyName:   fmt.Sprintf("plugins-%s-%s", time.Now().UTC().Format("20060102-150405"), token[:6]),
	}, nil
}

// persistPluginAPIKey atomically writes the API key, its audit log entry, and
// the GitHub connection record in one transaction.
func (s *Service) persistPluginAPIKey(
	ctx context.Context,
	ac *contextvalues.AuthContext,
	candidate pluginAPIKeyCandidate,
	repoOwner, repoName string,
) error {
	scopes := []string{auth.APIKeyScopeConsumer.String()}
	projectID := uuid.NullUUID{UUID: *ac.ProjectID, Valid: true}

	tx, err := s.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer o11y.NoLogDefer(func() error { return tx.Rollback(ctx) })

	createdKey, err := keysrepo.New(tx).CreateAPIKey(ctx, keysrepo.CreateAPIKeyParams{
		OrganizationID:  ac.ActiveOrganizationID,
		Name:            candidate.keyName,
		KeyHash:         candidate.keyHash,
		KeyPrefix:       candidate.keyPrefix,
		Scopes:          scopes,
		CreatedByUserID: ac.UserID,
		ProjectID:       projectID,
	})
	if err != nil {
		return fmt.Errorf("create api key: %w", err)
	}

	if err := audit.LogKeyCreate(ctx, tx, audit.LogKeyCreateEvent{
		OrganizationID:   ac.ActiveOrganizationID,
		ProjectID:        projectID,
		Actor:            urn.NewPrincipal(urn.PrincipalTypeUser, ac.UserID),
		ActorDisplayName: ac.Email,
		ActorSlug:        nil,
		KeyURN:           urn.NewAPIKey(createdKey.ID),
		KeyName:          candidate.keyName,
		Scopes:           scopes,
	}); err != nil {
		return fmt.Errorf("audit log key creation: %w", err)
	}

	if _, err := s.repo.WithTx(tx).UpsertGitHubConnection(ctx, repo.UpsertGitHubConnectionParams{
		ProjectID:      *ac.ProjectID,
		InstallationID: s.github.InstallationID,
		RepoOwner:      repoOwner,
		RepoName:       repoName,
	}); err != nil {
		return fmt.Errorf("upsert github connection: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit transaction: %w", err)
	}

	return nil
}

// --- Internal helpers ---

func (s *Service) resolvePluginInfos(ctx context.Context, projectID uuid.UUID) ([]PluginInfo, error) {
	rows, err := s.repo.ListPluginsWithServersForProject(ctx, projectID)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list plugins with servers").Log(ctx, s.logger)
	}

	type pluginBuild struct {
		info    PluginInfo
		servers []PluginServerInfo
	}
	pluginMap := make(map[uuid.UUID]*pluginBuild)
	mcpMeta := mcpmetarepo.New(s.db)

	for _, r := range rows {
		pb, ok := pluginMap[r.PluginID]
		if !ok {
			pb = &pluginBuild{
				info: PluginInfo{
					Name:        r.PluginName,
					Slug:        r.PluginSlug,
					Description: conv.FromPGTextOrEmpty[string](r.PluginDescription),
					Servers:     nil,
				},
				servers: nil,
			}
			pluginMap[r.PluginID] = pb
		}

		if mcpSlug := conv.FromPGText[string](r.ToolsetMcpSlug); mcpSlug != nil {
			serverInfo := PluginServerInfo{
				DisplayName: r.ServerDisplayName,
				Policy:      r.ServerPolicy,
				MCPURL:      fmt.Sprintf("%s/mcp/%s", s.serverURL, *mcpSlug),
				IsPublic:    r.ToolsetIsPublic,
				EnvConfigs:  nil,
			}

			// For public servers, load user-facing environment configs. A public
			// toolset without an mcp_metadata row simply has no user-provided
			// env vars — UpsertMetadata is explicit, not auto-created on publish.
			if r.ToolsetIsPublic {
				metadata, metaErr := mcpMeta.GetMetadataForToolset(ctx, r.ToolsetID)
				switch {
				case errors.Is(metaErr, pgx.ErrNoRows):
					// No metadata configured → no env configs to surface.
				case metaErr != nil:
					return nil, oops.E(oops.CodeUnexpected, metaErr, "load mcp metadata for toolset").Log(ctx, s.logger)
				default:
					envConfigs, envErr := mcpMeta.ListEnvironmentConfigs(ctx, metadata.ID)
					if envErr != nil {
						return nil, oops.E(oops.CodeUnexpected, envErr, "load environment configs for toolset").Log(ctx, s.logger)
					}
					for _, ec := range envConfigs {
						if ec.ProvidedBy != "user" {
							continue
						}
						// DisplayName ends up as both the HTTP header name and
						// the userConfig description in generated configs. The
						// env variable name is not a valid header substitute,
						// so skip configs with no HeaderDisplayName rather than
						// emit a broken header.
						headerName := conv.FromPGText[string](ec.HeaderDisplayName)
						if headerName == nil {
							s.logger.WarnContext(ctx, "skipping user env config with no header name",
								attr.SlogToolsetID(r.ToolsetID.String()),
								attr.SlogEnvVarName(ec.VariableName),
							)
							continue
						}
						serverInfo.EnvConfigs = append(serverInfo.EnvConfigs, ServerEnvConfig{
							VariableName: ec.VariableName,
							DisplayName:  *headerName,
						})
					}
				}
			}

			pb.servers = append(pb.servers, serverInfo)
		}
	}

	pluginInfos := make([]PluginInfo, 0, len(pluginMap))
	for _, pb := range pluginMap {
		pb.info.Servers = pb.servers
		pluginInfos = append(pluginInfos, pb.info)
	}
	sort.Slice(pluginInfos, func(i, j int) bool {
		return pluginInfos[i].Slug < pluginInfos[j].Slug
	})
	return pluginInfos, nil
}

func (s *Service) generateConfig(ctx context.Context, orgID, orgSlug string) GenerateConfig {
	cfg := GenerateConfig{
		OrgName:   orgSlug,
		OrgEmail:  "",
		ServerURL: s.serverURL,
		APIKey:    "",
	}
	orgName, err := s.repo.GetOrganizationName(ctx, orgID)
	switch {
	case err == nil:
		cfg.OrgName = orgName
	case !errors.Is(err, pgx.ErrNoRows):
		s.logger.WarnContext(ctx, "failed to fetch organization name, falling back to slug",
			attr.SlogOrganizationID(orgID),
			attr.SlogError(err),
		)
	}
	return cfg
}

func (s *Service) authContext(ctx context.Context) (*contextvalues.AuthContext, error) {
	ac, ok := contextvalues.GetAuthContext(ctx)
	if !ok || ac == nil || ac.ProjectID == nil {
		return nil, errors.New("missing auth context")
	}
	return ac, nil
}

// --- Conversion helpers ---

func pluginToGen(p repo.Plugin, servers []repo.PluginServer, assignments []repo.PluginAssignment) *gen.Plugin {
	result := &gen.Plugin{
		ID:              p.ID.String(),
		Name:            p.Name,
		Slug:            p.Slug,
		Description:     conv.FromPGText[string](p.Description),
		ServerCount:     nil,
		AssignmentCount: nil,
		Servers:         nil,
		Assignments:     nil,
		CreatedAt:       formatTime(p.CreatedAt),
		UpdatedAt:       formatTime(p.UpdatedAt),
	}

	if servers != nil {
		genServers := make([]*gen.PluginServer, 0, len(servers))
		for _, s := range servers {
			genServers = append(genServers, pluginServerToGen(s))
		}
		result.Servers = genServers
	}

	if assignments != nil {
		genAssignments := make([]*gen.PluginAssignment, 0, len(assignments))
		for _, a := range assignments {
			genAssignments = append(genAssignments, pluginAssignmentToGen(a))
		}
		result.Assignments = genAssignments
	}

	return result
}

func pluginServerToGen(s repo.PluginServer) *gen.PluginServer {
	return &gen.PluginServer{
		ID:          s.ID.String(),
		ToolsetID:   s.ToolsetID.String(),
		DisplayName: s.DisplayName,
		Policy:      s.Policy,
		SortOrder:   s.SortOrder,
		CreatedAt:   formatTime(s.CreatedAt),
	}
}

func pluginAssignmentToGen(a repo.PluginAssignment) *gen.PluginAssignment {
	return &gen.PluginAssignment{
		ID:           a.ID.String(),
		PrincipalUrn: a.PrincipalUrn,
		CreatedAt:    formatTime(a.CreatedAt),
	}
}

func formatTime(t pgtype.Timestamptz) string {
	if !t.Valid {
		return ""
	}
	return t.Time.UTC().Format(time.RFC3339)
}
