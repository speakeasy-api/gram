package plugins

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"maps"
	"os"
	"regexp"
	"sort"
	"strconv"
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

	redisCache "github.com/go-redis/cache/v9"

	srv "github.com/speakeasy-api/gram/server/gen/http/plugins/server"
	gen "github.com/speakeasy-api/gram/server/gen/plugins"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/auth"
	"github.com/speakeasy-api/gram/server/internal/auth/sessions"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/feature"
	keysrepo "github.com/speakeasy-api/gram/server/internal/keys/repo"
	"github.com/speakeasy-api/gram/server/internal/marketplace"
	mcpmetarepo "github.com/speakeasy-api/gram/server/internal/mcpmetadata/repo"
	"github.com/speakeasy-api/gram/server/internal/mcpservers/visibility"
	"github.com/speakeasy-api/gram/server/internal/middleware"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/plugins/repo"
	"github.com/speakeasy-api/gram/server/internal/productfeatures"
	projectsrepo "github.com/speakeasy-api/gram/server/internal/projects/repo"
	ghclient "github.com/speakeasy-api/gram/server/internal/thirdparty/github"
	toolsetsrepo "github.com/speakeasy-api/gram/server/internal/toolsets/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// GitHub usernames: 1-39 chars, starts with alphanumeric, alphanumeric or hyphen.
// Strict enough to prevent path traversal in API URL construction.
var validGitHubUsername = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9-]{0,38}$`)

// GitHubPublisher is the interface for creating repos and pushing files to GitHub.
type GitHubPublisher interface {
	CreateRepo(ctx context.Context, installationID int64, org, name string, private bool) error
	PushFiles(ctx context.Context, installationID int64, owner, repo, branch, commitMsg string, files map[string][]byte) (string, error)
	AddCollaborator(ctx context.Context, installationID int64, owner, repo, username, permission string) error
	HasDirectCollaborator(ctx context.Context, installationID int64, owner, repo string) (bool, error)
	// GetRepoFiles returns the current published files so a publish can carry an
	// unchanged plugin component (hooks or MCP) verbatim into a fresh push,
	// leaving the other component's files and configuration untouched. Returns
	// ghclient.ErrRepoNotFound when nothing has been published yet.
	GetRepoFiles(ctx context.Context, installationID int64, owner, repo, branch string) (map[string][]byte, error)
}

// GitHubConfig holds the configured GitHub client and the Gram-owned org
// where plugin repos are created. Nil means GitHub publishing is disabled.
type GitHubConfig struct {
	Client         GitHubPublisher
	Org            string
	InstallationID int64
}

// GitHubConfigInput is the deployment-time configuration for plugin
// GitHub publishing. All fields must be set together (the feature is on) or
// all must be unset (the feature is off). Pass to NewGitHubConfig.
//
// Client is constructed by the caller (typically once in cmd/gram, then
// shared with other consumers like the marketplace proxy that need to mint
// installation tokens against the same App).
type GitHubConfigInput struct {
	Client         *ghclient.Client
	Org            string
	InstallationID int64
}

// NewGitHubConfig validates a GitHubConfigInput holistically and returns:
//   - (nil, nil)        when no fields are set: feature is disabled
//   - (config, nil)     when all fields are set: feature is enabled
//   - (nil, error)      when only some fields are set: deployment is misconfigured
//
// The all-or-nothing check prevents the silent-disable footgun where setting
// two of three inputs (e.g. forgetting GRAM_PLUGINS_GITHUB_ORG) leaves the
// deployment running with publishing inexplicably off.
func NewGitHubConfig(in GitHubConfigInput) (*GitHubConfig, error) {
	set := 0
	missing := []string{}
	if in.Client != nil {
		set++
	} else {
		missing = append(missing, "plugins-github-client")
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
	case 3:
		return &GitHubConfig{
			Client:         in.Client,
			Org:            in.Org,
			InstallationID: in.InstallationID,
		}, nil
	default:
		return nil, fmt.Errorf("plugin github publishing requires client, plugins-github-org, plugins-github-installation-id; missing: %s", strings.Join(missing, ", "))
	}
}

type Service struct {
	tracer    trace.Tracer
	logger    *slog.Logger
	db        *pgxpool.Pool
	repo      *repo.Queries
	auth      *auth.Auth
	authz     *authz.Engine
	audit     *audit.Logger
	cache     cache.Cache
	github    *GitHubConfig
	serverURL string
	keyPrefix string
	// features drives the phased hooks rollout gate applied to every publish (see
	// publishProject). Both the automated publisher (NewPublisher) and the
	// dashboard service (NewService) set it, so interactive publishes are gated
	// too. A nil provider fails CLOSED — non-canary orgs are treated as not
	// eligible and carry their existing hooks — so a missing provider can never
	// force-advance an org.
	features feature.Provider
}

var _ gen.Service = (*Service)(nil)
var _ gen.Auther = (*Service)(nil)

func NewService(
	logger *slog.Logger,
	tracerProvider trace.TracerProvider,
	db *pgxpool.Pool,
	sessions *sessions.Manager,
	cacheImpl cache.Cache,
	authzEngine *authz.Engine,
	auditLogger *audit.Logger,
	github *GitHubConfig,
	env string,
	serverURL string,
	features feature.Provider,
) *Service {
	logger = logger.With(attr.SlogComponent("plugins"))

	return &Service{
		tracer:    tracerProvider.Tracer("github.com/speakeasy-api/gram/server/internal/plugins"),
		logger:    logger,
		db:        db,
		repo:      repo.New(db),
		auth:      auth.New(logger, db, sessions, authzEngine),
		authz:     authzEngine,
		audit:     auditLogger,
		cache:     cacheImpl,
		github:    github,
		serverURL: serverURL,
		keyPrefix: auth.APIKeyPrefix(env),
		// features gates human/dashboard-initiated hook-output changes (marketplace
		// rename via UpdateMarketplaceSettings, observability-mode toggle via
		// productfeatures) on the phased hooks rollout, mirroring the automated
		// publisher. Fail-closed when nil: non-canary orgs defer those changes.
		features: features,
	}
}

func NewPublisher(
	logger *slog.Logger,
	db *pgxpool.Pool,
	auditLogger *audit.Logger,
	github *GitHubConfig,
	env string,
	serverURL string,
	features feature.Provider,
) *Service {
	logger = logger.With(attr.SlogComponent("plugins"))

	return &Service{
		tracer:    nil,
		logger:    logger,
		db:        db,
		repo:      repo.New(db),
		auth:      nil,
		authz:     nil,
		audit:     auditLogger,
		cache:     nil,
		github:    github,
		serverURL: serverURL,
		keyPrefix: auth.APIKeyPrefix(env),
		features:  features,
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

	// Projects created before the Default plugin existed never got one
	// provisioned. Heal that lazily here so the dashboard always has a
	// plugin to publish to, mirroring the AttachToDefaultPlugin callers in
	// toolsets/mcpendpoints.
	if err := s.ensureDefaultPlugin(ctx, ac); err != nil {
		return nil, err
	}

	rows, err := s.repo.ListPlugins(ctx, repo.ListPluginsParams{
		OrganizationID: ac.ActiveOrganizationID,
		ProjectID:      *ac.ProjectID,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list plugins").LogError(ctx, s.logger)
	}

	pluginIDs := make([]uuid.UUID, len(rows))
	for i, r := range rows {
		pluginIDs[i] = r.ID
	}
	allServers, err := s.repo.ListPluginServersByPluginIDs(ctx, repo.ListPluginServersByPluginIDsParams{
		PluginIds: pluginIDs,
		ProjectID: *ac.ProjectID,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list plugin servers").LogError(ctx, s.logger)
	}
	serversByPlugin := make(map[uuid.UUID][]repo.PluginServer, len(rows))
	for _, srv := range allServers {
		serversByPlugin[srv.PluginID] = append(serversByPlugin[srv.PluginID], srv)
	}

	plugins := make([]*gen.Plugin, 0, len(rows))
	for _, r := range rows {
		servers := serversByPlugin[r.ID]
		genServers := make([]*gen.PluginServer, 0, len(servers))
		for _, srv := range servers {
			genServers = append(genServers, pluginServerToGen(srv))
		}

		plugins = append(plugins, &gen.Plugin{
			ID:              r.ID.String(),
			Name:            r.Name,
			Slug:            r.Slug,
			Description:     conv.FromPGText[string](r.Description),
			IsDefault:       conv.FromPGBool[bool](r.IsDefault),
			ServerCount:     &r.ServerCount,
			AssignmentCount: &r.AssignmentCount,
			Servers:         genServers,
			Assignments:     nil,
			CreatedAt:       formatTime(r.CreatedAt),
			UpdatedAt:       formatTime(r.UpdatedAt),
		})
	}

	return &gen.ListPluginsResult{Plugins: plugins}, nil
}

// ensureDefaultPlugin provisions the project's Default plugin if it doesn't
// exist yet, covering projects created before CreateProject started
// provisioning one. No-ops (no audit event, no error) when the plugin
// already exists, or when the caller lacks the admin scope that plugin
// creation normally requires (CreatePlugin/AddPluginServer) — a read-only
// viewer loading the dashboard shouldn't be able to trigger a write.
func (s *Service) ensureDefaultPlugin(ctx context.Context, ac *contextvalues.AuthContext) error {
	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeOrgAdmin, ResourceKind: "", ResourceID: ac.ActiveOrganizationID, Dimensions: nil}); err != nil {
		return nil
	}

	tx, err := s.db.Begin(ctx)
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "begin transaction").LogError(ctx, s.logger)
	}
	defer o11y.NoLogDefer(func() error { return tx.Rollback(ctx) })

	ensured, err := EnsureDefaultPlugin(ctx, tx, ac.ActiveOrganizationID, *ac.ProjectID)
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "ensure default plugin").LogError(ctx, s.logger)
	}
	if !ensured.Created {
		return nil
	}

	if err := s.audit.LogPluginCreate(ctx, tx, audit.LogPluginCreateEvent{
		OrganizationID:   ac.ActiveOrganizationID,
		ProjectID:        *ac.ProjectID,
		Actor:            urn.NewPrincipal(urn.PrincipalTypeUser, ac.UserID),
		ActorDisplayName: ac.Email,
		ActorSlug:        nil,
		PluginID:         ensured.Plugin.ID,
		PluginName:       ensured.Plugin.Name,
		PluginSlug:       ensured.Plugin.Slug,
	}); err != nil {
		return oops.E(oops.CodeUnexpected, err, "audit log default plugin create").LogError(ctx, s.logger)
	}

	if err := tx.Commit(ctx); err != nil {
		return oops.E(oops.CodeUnexpected, err, "commit transaction").LogError(ctx, s.logger)
	}
	return nil
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
		return nil, oops.E(oops.CodeBadRequest, err, "invalid plugin id").LogError(ctx, s.logger)
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
		return nil, oops.E(oops.CodeUnexpected, err, "get plugin").LogError(ctx, s.logger)
	}

	servers, err := s.repo.ListPluginServers(ctx, pluginID)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list plugin servers").LogError(ctx, s.logger)
	}

	assignments, err := s.repo.ListPluginAssignments(ctx, pluginID)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list plugin assignments").LogError(ctx, s.logger)
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

	tx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "begin transaction").LogError(ctx, s.logger)
	}
	defer o11y.NoLogDefer(func() error { return tx.Rollback(ctx) })

	plugin, err := s.repo.WithTx(tx).CreatePlugin(ctx, repo.CreatePluginParams{
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
		return nil, oops.E(oops.CodeUnexpected, err, "create plugin").LogError(ctx, s.logger)
	}

	if err := s.audit.LogPluginCreate(ctx, tx, audit.LogPluginCreateEvent{
		OrganizationID:   ac.ActiveOrganizationID,
		ProjectID:        *ac.ProjectID,
		Actor:            urn.NewPrincipal(urn.PrincipalTypeUser, ac.UserID),
		ActorDisplayName: ac.Email,
		ActorSlug:        nil,
		PluginID:         plugin.ID,
		PluginName:       plugin.Name,
		PluginSlug:       plugin.Slug,
	}); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "audit log plugin create").LogError(ctx, s.logger)
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "commit transaction").LogError(ctx, s.logger)
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
		return nil, oops.E(oops.CodeBadRequest, err, "invalid plugin id").LogError(ctx, s.logger)
	}

	slug := conv.ToSlug(payload.Slug)
	if slug == "" || slug != payload.Slug {
		return nil, oops.E(oops.CodeBadRequest, nil, "invalid slug: must be non-empty and contain only lowercase alphanumeric characters and hyphens")
	}

	tx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "begin transaction").LogError(ctx, s.logger)
	}
	defer o11y.NoLogDefer(func() error { return tx.Rollback(ctx) })

	txRepo := s.repo.WithTx(tx)

	before, err := txRepo.GetPlugin(ctx, repo.GetPluginParams{
		ID:             pluginID,
		OrganizationID: ac.ActiveOrganizationID,
		ProjectID:      *ac.ProjectID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, oops.C(oops.CodeNotFound)
		}
		return nil, oops.E(oops.CodeUnexpected, err, "load plugin").LogError(ctx, s.logger)
	}

	plugin, err := txRepo.UpdatePlugin(ctx, repo.UpdatePluginParams{
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
		return nil, oops.E(oops.CodeUnexpected, err, "update plugin").LogError(ctx, s.logger)
	}

	if err := s.audit.LogPluginUpdate(ctx, tx, audit.LogPluginUpdateEvent{
		OrganizationID:   ac.ActiveOrganizationID,
		ProjectID:        *ac.ProjectID,
		Actor:            urn.NewPrincipal(urn.PrincipalTypeUser, ac.UserID),
		ActorDisplayName: ac.Email,
		ActorSlug:        nil,
		PluginID:         plugin.ID,
		PluginName:       plugin.Name,
		PluginSlug:       plugin.Slug,
		SnapshotBefore: &audit.PluginSnapshot{
			Name:        before.Name,
			Slug:        before.Slug,
			Description: conv.FromPGText[string](before.Description),
		},
		SnapshotAfter: &audit.PluginSnapshot{
			Name:        plugin.Name,
			Slug:        plugin.Slug,
			Description: conv.FromPGText[string](plugin.Description),
		},
	}); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "audit log plugin update").LogError(ctx, s.logger)
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "commit transaction").LogError(ctx, s.logger)
	}

	servers, err := s.repo.ListPluginServers(ctx, pluginID)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list plugin servers").LogError(ctx, s.logger)
	}

	assignments, err := s.repo.ListPluginAssignments(ctx, pluginID)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list plugin assignments").LogError(ctx, s.logger)
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
		return oops.E(oops.CodeBadRequest, err, "invalid plugin id").LogError(ctx, s.logger)
	}

	// Verify the plugin belongs to this project before mutating.
	plugin, err := s.repo.GetPlugin(ctx, repo.GetPluginParams{ID: pluginID, OrganizationID: ac.ActiveOrganizationID, ProjectID: *ac.ProjectID})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return oops.C(oops.CodeNotFound)
		}
		return oops.E(oops.CodeUnexpected, err, "verify plugin ownership").LogError(ctx, s.logger)
	}

	tx, err := s.db.Begin(ctx)
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "begin transaction").LogError(ctx, s.logger)
	}
	defer o11y.NoLogDefer(func() error { return tx.Rollback(ctx) })

	txRepo := s.repo.WithTx(tx)

	if err := txRepo.SoftDeletePluginServers(ctx, pluginID); err != nil {
		return oops.E(oops.CodeUnexpected, err, "soft-delete plugin servers").LogError(ctx, s.logger)
	}

	if _, err := txRepo.RemoveAllPluginAssignments(ctx, pluginID); err != nil {
		return oops.E(oops.CodeUnexpected, err, "remove plugin assignments").LogError(ctx, s.logger)
	}

	if err := txRepo.DeletePlugin(ctx, repo.DeletePluginParams{
		ID:             pluginID,
		OrganizationID: ac.ActiveOrganizationID,
		ProjectID:      *ac.ProjectID,
	}); err != nil {
		return oops.E(oops.CodeUnexpected, err, "delete plugin").LogError(ctx, s.logger)
	}

	if err := s.audit.LogPluginDelete(ctx, tx, audit.LogPluginDeleteEvent{
		OrganizationID:   ac.ActiveOrganizationID,
		ProjectID:        *ac.ProjectID,
		Actor:            urn.NewPrincipal(urn.PrincipalTypeUser, ac.UserID),
		ActorDisplayName: ac.Email,
		ActorSlug:        nil,
		PluginID:         plugin.ID,
		PluginName:       plugin.Name,
		PluginSlug:       plugin.Slug,
	}); err != nil {
		return oops.E(oops.CodeUnexpected, err, "audit log plugin delete").LogError(ctx, s.logger)
	}

	if err := tx.Commit(ctx); err != nil {
		return oops.E(oops.CodeUnexpected, err, "commit transaction").LogError(ctx, s.logger)
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
		return nil, oops.E(oops.CodeBadRequest, err, "invalid plugin id").LogError(ctx, s.logger)
	}

	backend, err := parseServerBackend(payload.ToolsetID, payload.McpServerID)
	if err != nil {
		return nil, err
	}

	// Verify the plugin belongs to this project.
	plugin, err := s.repo.GetPlugin(ctx, repo.GetPluginParams{ID: pluginID, OrganizationID: ac.ActiveOrganizationID, ProjectID: *ac.ProjectID})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, oops.C(oops.CodeNotFound)
		}
		return nil, oops.E(oops.CodeUnexpected, err, "verify plugin ownership").LogError(ctx, s.logger)
	}

	displayName := ""
	if payload.DisplayName != nil {
		displayName = strings.TrimSpace(*payload.DisplayName)
	}

	if backend.mcpServerID.Valid {
		// Verify the mcp_server exists in this project and is publishable.
		server, mcpErr := s.repo.GetMcpServerForPluginServer(ctx, repo.GetMcpServerForPluginServerParams{
			McpServerID: backend.mcpServerID.UUID,
			ProjectID:   *ac.ProjectID,
		})
		if mcpErr != nil {
			if errors.Is(mcpErr, pgx.ErrNoRows) {
				return nil, oops.E(oops.CodeBadRequest, nil, "mcp server not found")
			}
			return nil, oops.E(oops.CodeUnexpected, mcpErr, "verify mcp server").LogError(ctx, s.logger)
		}
		if server.Visibility == visibility.Disabled || !server.HasEndpoint {
			return nil, oops.E(oops.CodeBadRequest, nil, "mcp server is disabled or has no published endpoint")
		}
		if displayName == "" {
			// mcpServerDisplayName always returns a non-empty value (name, then
			// slug, then the UUID id), so display_name is guaranteed set here.
			displayName = mcpServerDisplayName(server)
		}
	} else {
		// Verify the toolset exists and belongs to the same project.
		toolset, tErr := toolsetsrepo.New(s.db).GetToolsetByIDAndProject(ctx, toolsetsrepo.GetToolsetByIDAndProjectParams{
			ID:        backend.toolsetID.UUID,
			ProjectID: *ac.ProjectID,
		})
		if tErr != nil {
			if errors.Is(tErr, pgx.ErrNoRows) {
				return nil, oops.E(oops.CodeBadRequest, nil, "toolset not found")
			}
			return nil, oops.E(oops.CodeUnexpected, tErr, "verify toolset").LogError(ctx, s.logger)
		}
		if !toolset.McpEnabled || !toolset.McpSlug.Valid || toolset.McpSlug.String == "" {
			return nil, oops.E(oops.CodeBadRequest, nil, "toolset does not have MCP enabled")
		}
		if displayName == "" {
			// toolsets.name is NOT NULL CHECK (name <> ''), so display_name is
			// guaranteed non-empty here.
			displayName = toolset.Name
		}
	}

	tx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "begin transaction").LogError(ctx, s.logger)
	}
	defer o11y.NoLogDefer(func() error { return tx.Rollback(ctx) })

	row, err := s.repo.WithTx(tx).AddPluginServer(ctx, repo.AddPluginServerParams{
		PluginID:    pluginID,
		ToolsetID:   backend.toolsetID,
		McpServerID: backend.mcpServerID,
		DisplayName: displayName,
		Policy:      payload.Policy,
		SortOrder:   payload.SortOrder,
	})
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == pgerrcode.UniqueViolation {
			switch pgErr.ConstraintName {
			case "plugin_servers_plugin_id_toolset_id_key", "plugin_servers_plugin_id_mcp_server_id_key":
				return nil, oops.E(oops.CodeConflict, nil, "this server has already been added to the plugin")
			default:
				return nil, oops.E(oops.CodeConflict, nil, "a server with this display name already exists in the plugin")
			}
		}
		return nil, oops.E(oops.CodeUnexpected, err, "add plugin server").LogError(ctx, s.logger)
	}

	toolsetURN, mcpServerURN := backend.auditURNs()
	if err := s.audit.LogPluginServerAdd(ctx, tx, audit.LogPluginServerAddEvent{
		OrganizationID:    ac.ActiveOrganizationID,
		ProjectID:         *ac.ProjectID,
		Actor:             urn.NewPrincipal(urn.PrincipalTypeUser, ac.UserID),
		ActorDisplayName:  ac.Email,
		ActorSlug:         nil,
		PluginID:          plugin.ID,
		PluginName:        plugin.Name,
		PluginSlug:        plugin.Slug,
		ServerID:          row.ID,
		ServerDisplayName: row.DisplayName,
		ServerPolicy:      row.Policy,
		ServerSortOrder:   row.SortOrder,
		ToolsetURN:        toolsetURN,
		McpServerURN:      mcpServerURN,
	}); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "audit log plugin server add").LogError(ctx, s.logger)
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "commit transaction").LogError(ctx, s.logger)
	}

	return pluginServerToGen(row), nil
}

// serverBackend identifies which backend a plugin server targets. Exactly one
// of toolsetID / mcpServerID is Valid, mirroring the toolset_id XOR
// mcp_server_id plugin_servers row.
type serverBackend struct {
	toolsetID   uuid.NullUUID
	mcpServerID uuid.NullUUID
}

// parseServerBackend validates that exactly one of toolset_id / mcp_server_id
// was supplied and parses the provided id. The XOR is enforced here in the
// handler because the Goa payload accepts both as optional for wire-compat with
// existing toolset-only callers.
func parseServerBackend(toolsetID, mcpServerID *string) (serverBackend, error) {
	hasToolset := toolsetID != nil && *toolsetID != ""
	hasMcpServer := mcpServerID != nil && *mcpServerID != ""

	switch {
	case hasToolset == hasMcpServer:
		return serverBackend{}, oops.E(oops.CodeBadRequest, nil, "provide exactly one of toolset_id or mcp_server_id")
	case hasToolset:
		id, err := uuid.Parse(*toolsetID)
		if err != nil {
			return serverBackend{}, oops.E(oops.CodeBadRequest, err, "invalid toolset_id")
		}
		return serverBackend{toolsetID: uuid.NullUUID{UUID: id, Valid: true}, mcpServerID: uuid.NullUUID{UUID: uuid.Nil, Valid: false}}, nil
	default:
		id, err := uuid.Parse(*mcpServerID)
		if err != nil {
			return serverBackend{}, oops.E(oops.CodeBadRequest, err, "invalid mcp_server_id")
		}
		return serverBackend{toolsetID: uuid.NullUUID{UUID: uuid.Nil, Valid: false}, mcpServerID: uuid.NullUUID{UUID: id, Valid: true}}, nil
	}
}

// auditURNs returns the subject URN for whichever backend is set, leaving the
// other nil, for the backend-aware plugin-server audit events.
func (b serverBackend) auditURNs() (*urn.Toolset, *urn.McpServer) {
	if b.mcpServerID.Valid {
		u := urn.NewMcpServer(b.mcpServerID.UUID)
		return nil, &u
	}
	u := urn.NewToolset(b.toolsetID.UUID)
	return &u, nil
}

// mcpServerDisplayName derives a default plugin-server display name from an
// mcp_server, preferring its name, then slug, then id.
func mcpServerDisplayName(server repo.GetMcpServerForPluginServerRow) string {
	if name := conv.FromPGText[string](server.Name); name != nil && *name != "" {
		return *name
	}
	if slug := conv.FromPGText[string](server.Slug); slug != nil && *slug != "" {
		return *slug
	}
	return server.ID.String()
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
		return nil, oops.E(oops.CodeBadRequest, err, "invalid server id").LogError(ctx, s.logger)
	}
	pluginID, err := uuid.Parse(payload.PluginID)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid plugin id").LogError(ctx, s.logger)
	}

	// Verify the plugin belongs to this project.
	plugin, err := s.repo.GetPlugin(ctx, repo.GetPluginParams{ID: pluginID, OrganizationID: ac.ActiveOrganizationID, ProjectID: *ac.ProjectID})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, oops.C(oops.CodeNotFound)
		}
		return nil, oops.E(oops.CodeUnexpected, err, "verify plugin ownership").LogError(ctx, s.logger)
	}

	tx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "begin transaction").LogError(ctx, s.logger)
	}
	defer o11y.NoLogDefer(func() error { return tx.Rollback(ctx) })

	row, err := s.repo.WithTx(tx).UpdatePluginServer(ctx, repo.UpdatePluginServerParams{
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
		return nil, oops.E(oops.CodeUnexpected, err, "update plugin server").LogError(ctx, s.logger)
	}

	if err := s.audit.LogPluginServerUpdate(ctx, tx, audit.LogPluginServerUpdateEvent{
		OrganizationID:    ac.ActiveOrganizationID,
		ProjectID:         *ac.ProjectID,
		Actor:             urn.NewPrincipal(urn.PrincipalTypeUser, ac.UserID),
		ActorDisplayName:  ac.Email,
		ActorSlug:         nil,
		PluginID:          plugin.ID,
		PluginName:        plugin.Name,
		PluginSlug:        plugin.Slug,
		ServerID:          row.ID,
		ServerDisplayName: row.DisplayName,
		ServerPolicy:      row.Policy,
		ServerSortOrder:   row.SortOrder,
	}); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "audit log plugin server update").LogError(ctx, s.logger)
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "commit transaction").LogError(ctx, s.logger)
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
		return oops.E(oops.CodeBadRequest, err, "invalid server id").LogError(ctx, s.logger)
	}
	pluginID, err := uuid.Parse(payload.PluginID)
	if err != nil {
		return oops.E(oops.CodeBadRequest, err, "invalid plugin id").LogError(ctx, s.logger)
	}

	// Verify the plugin belongs to this project.
	plugin, err := s.repo.GetPlugin(ctx, repo.GetPluginParams{ID: pluginID, OrganizationID: ac.ActiveOrganizationID, ProjectID: *ac.ProjectID})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return oops.C(oops.CodeNotFound)
		}
		return oops.E(oops.CodeUnexpected, err, "verify plugin ownership").LogError(ctx, s.logger)
	}

	tx, err := s.db.Begin(ctx)
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "begin transaction").LogError(ctx, s.logger)
	}
	defer o11y.NoLogDefer(func() error { return tx.Rollback(ctx) })

	row, err := s.repo.WithTx(tx).RemovePluginServer(ctx, repo.RemovePluginServerParams{
		ID:       serverID,
		PluginID: pluginID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return oops.C(oops.CodeNotFound)
		}
		return oops.E(oops.CodeUnexpected, err, "remove plugin server").LogError(ctx, s.logger)
	}

	toolsetURN, mcpServerURN := serverBackend{toolsetID: row.ToolsetID, mcpServerID: row.McpServerID}.auditURNs()
	if err := s.audit.LogPluginServerRemove(ctx, tx, audit.LogPluginServerRemoveEvent{
		OrganizationID:   ac.ActiveOrganizationID,
		ProjectID:        *ac.ProjectID,
		Actor:            urn.NewPrincipal(urn.PrincipalTypeUser, ac.UserID),
		ActorDisplayName: ac.Email,
		ActorSlug:        nil,
		PluginID:         plugin.ID,
		PluginName:       plugin.Name,
		PluginSlug:       plugin.Slug,
		ServerID:         serverID,
		ToolsetURN:       toolsetURN,
		McpServerURN:     mcpServerURN,
	}); err != nil {
		return oops.E(oops.CodeUnexpected, err, "audit log plugin server remove").LogError(ctx, s.logger)
	}

	if err := tx.Commit(ctx); err != nil {
		return oops.E(oops.CodeUnexpected, err, "commit transaction").LogError(ctx, s.logger)
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
		return nil, oops.E(oops.CodeBadRequest, err, "invalid plugin id").LogError(ctx, s.logger)
	}

	// Verify the plugin belongs to this project.
	plugin, err := s.repo.GetPlugin(ctx, repo.GetPluginParams{ID: pluginID, OrganizationID: ac.ActiveOrganizationID, ProjectID: *ac.ProjectID})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, oops.C(oops.CodeNotFound)
		}
		return nil, oops.E(oops.CodeUnexpected, err, "verify plugin ownership").LogError(ctx, s.logger)
	}

	// Normalize and validate every principal URN through urn.ParsePrincipal so
	// the typed wrapper is the single source of truth on what a principal is.
	// The wildcard is a literal token (not a typed URN), so it takes a
	// fast-path. Email IDs are lowercased here so the device-agent endpoint
	// can match a lowercased lookup deterministically.
	urns := make([]string, 0, len(payload.PrincipalUrns))
	seenURNs := make(map[string]struct{}, len(payload.PrincipalUrns))
	for _, raw := range payload.PrincipalUrns {
		var principalURN string
		if raw == urn.PrincipalWildcard {
			principalURN = raw
		} else {
			normalized := raw
			if addr, ok := strings.CutPrefix(raw, string(urn.PrincipalTypeEmail)+":"); ok {
				normalized = string(urn.PrincipalTypeEmail) + ":" + conv.NormalizeEmail(addr)
			}
			parsed, err := urn.ParsePrincipal(normalized)
			if err != nil {
				return nil, oops.E(oops.CodeBadRequest, err, "invalid principal URN: %s", raw)
			}
			principalURN = parsed.String()
		}

		if _, ok := seenURNs[principalURN]; ok {
			continue
		}
		seenURNs[principalURN] = struct{}{}
		urns = append(urns, principalURN)
	}

	tx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "begin transaction").LogError(ctx, s.logger)
	}
	defer o11y.NoLogDefer(func() error { return tx.Rollback(ctx) })

	txRepo := s.repo.WithTx(tx)

	if _, err := txRepo.RemoveAllPluginAssignments(ctx, pluginID); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "remove existing assignments").LogError(ctx, s.logger)
	}

	assignments := make([]*gen.PluginAssignment, 0, len(urns))
	for _, u := range urns {
		row, err := txRepo.AddPluginAssignment(ctx, repo.AddPluginAssignmentParams{
			PluginID:       pluginID,
			OrganizationID: ac.ActiveOrganizationID,
			PrincipalUrn:   u,
		})
		if err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "add plugin assignment").LogError(ctx, s.logger)
		}
		assignments = append(assignments, pluginAssignmentToGen(row))
	}

	if err := s.audit.LogPluginAssignmentsSet(ctx, tx, audit.LogPluginAssignmentsSetEvent{
		OrganizationID:   ac.ActiveOrganizationID,
		ProjectID:        *ac.ProjectID,
		Actor:            urn.NewPrincipal(urn.PrincipalTypeUser, ac.UserID),
		ActorDisplayName: ac.Email,
		ActorSlug:        nil,
		PluginID:         plugin.ID,
		PluginName:       plugin.Name,
		PluginSlug:       plugin.Slug,
		PrincipalURNs:    urns,
	}); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "audit log plugin assignments set").LogError(ctx, s.logger)
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "commit transaction").LogError(ctx, s.logger)
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
		return nil, nil, oops.E(oops.CodeBadRequest, err, "invalid plugin id").LogError(ctx, s.logger)
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
		return nil, nil, oops.E(oops.CodeUnexpected, err, "get plugin").LogError(ctx, s.logger)
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

	projectSlug := ""
	if ac.ProjectSlug != nil {
		projectSlug = *ac.ProjectSlug
	}
	cfg := s.generateConfig(ctx, ac.ActiveOrganizationID, ac.OrganizationSlug, projectSlug, *ac.ProjectID)

	files, err := GenerateSinglePluginPackage(*pluginInfo, cfg, payload.Platform)
	if err != nil {
		return nil, nil, oops.E(oops.CodeUnexpected, err, "generate plugin package").LogError(ctx, s.logger)
	}

	var buf bytes.Buffer
	if err := writePluginZip(&buf, files); err != nil {
		return nil, nil, oops.E(oops.CodeUnexpected, err, "build plugin zip").LogError(ctx, s.logger)
	}

	return &gen.DownloadPluginPackageResult{
		ContentType:        "application/zip",
		ContentDisposition: fmt.Sprintf(`attachment; filename="%s.zip"`, dbPlugin.Slug),
	}, io.NopCloser(bytes.NewReader(buf.Bytes())), nil
}

// DownloadObservabilityPlugin returns a ZIP of the per-org observability
// plugin for direct installation. Mints a fresh hooks-scoped API key per
// download and embeds it in speakeasy.json — the org's API Keys page will
// accumulate one row per download, which admins can audit and revoke
// independently of the publish-bundled key. The plugin contents are
// otherwise identical to what PublishPlugins ships in the GitHub marketplace.
func (s *Service) DownloadObservabilityPlugin(ctx context.Context, payload *gen.DownloadObservabilityPluginPayload) (*gen.DownloadObservabilityPluginResult, io.ReadCloser, error) {
	ac, err := s.authContext(ctx)
	if err != nil {
		return nil, nil, oops.C(oops.CodeUnauthorized)
	}

	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeOrgAdmin, ResourceKind: "", ResourceID: ac.ActiveOrganizationID, Dimensions: nil}); err != nil {
		return nil, nil, err
	}

	if ac.ProjectSlug == nil {
		return nil, nil, oops.E(oops.CodeUnauthorized, nil, "observability plugin download requires a session-authenticated context")
	}

	candidate, err := s.buildPluginAPIKeyCandidate(auth.APIKeyScopeHooks, "hooks-download")
	if err != nil {
		return nil, nil, oops.E(oops.CodeUnexpected, err, "build hooks api key").LogError(ctx, s.logger)
	}

	if err := s.persistDownloadAPIKey(ctx, ac, candidate); err != nil {
		return nil, nil, oops.E(oops.CodeUnexpected, err, "persist hooks api key").LogError(ctx, s.logger)
	}

	cfg := s.generateConfig(ctx, ac.ActiveOrganizationID, ac.OrganizationSlug, *ac.ProjectSlug, *ac.ProjectID)
	cfg.HooksAPIKey = candidate.fullKey

	files, err := GenerateObservabilityPluginPackage(cfg, payload.Platform)
	if err != nil {
		return nil, nil, oops.E(oops.CodeUnexpected, err, "generate observability plugin package").LogError(ctx, s.logger)
	}

	var buf bytes.Buffer
	if err := writePluginZip(&buf, files); err != nil {
		return nil, nil, oops.E(oops.CodeUnexpected, err, "build plugin zip").LogError(ctx, s.logger)
	}

	filename := "observability"
	switch payload.Platform {
	case "cursor":
		filename = "observability-cursor"
	case "codex":
		filename = "observability-codex"
	}
	return &gen.DownloadObservabilityPluginResult{
		ContentType:        "application/zip",
		ContentDisposition: fmt.Sprintf(`attachment; filename="%s.zip"`, filename),
	}, io.NopCloser(bytes.NewReader(buf.Bytes())), nil
}

func (s *Service) DownloadCodexInstallScript(ctx context.Context, payload *gen.DownloadCodexInstallScriptPayload) (*gen.DownloadCodexInstallScriptResult, io.ReadCloser, error) {
	ac, err := s.authContext(ctx)
	if err != nil {
		return nil, nil, oops.C(oops.CodeUnauthorized)
	}

	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeOrgAdmin, ResourceKind: "", ResourceID: ac.ActiveOrganizationID, Dimensions: nil}); err != nil {
		return nil, nil, err
	}

	if s.github == nil {
		return nil, nil, oops.E(oops.CodeBadRequest, nil, "GitHub publishing is not configured; publish plugins first to get a marketplace URL")
	}

	conn, err := s.repo.GetGitHubConnection(ctx, *ac.ProjectID)
	if err != nil {
		return nil, nil, oops.E(oops.CodeBadRequest, nil, "no published marketplace found; publish plugins first")
	}

	if !conn.MarketplaceToken.Valid || s.serverURL == "" {
		return nil, nil, oops.E(oops.CodeBadRequest, nil, "marketplace URL not available; publish plugins first")
	}

	marketplaceURL := fmt.Sprintf("%s%s%s.git", s.serverURL, marketplace.RoutePrefix, conn.MarketplaceToken.String)

	cfg := s.generateConfig(ctx, ac.ActiveOrganizationID, ac.OrganizationSlug, conv.PtrValOr(ac.ProjectSlug, ""), *ac.ProjectID)

	script, err := GenerateCodexInstallScript(marketplaceURL, cfg)
	if err != nil {
		return nil, nil, oops.E(oops.CodeUnexpected, err, "generate codex install script").LogError(ctx, s.logger)
	}

	return &gen.DownloadCodexInstallScriptResult{
		ContentType:        "text/x-shellscript",
		ContentDisposition: `attachment; filename="gram-codex-install.sh"`,
	}, io.NopCloser(bytes.NewReader(script)), nil
}

// writePluginZip serializes the file map as a deterministic ZIP, marking shell
// scripts executable so the bootstrapper runs after extraction. The GitHub
// publish path applies the same rule via Tree mode 100755 in
// thirdparty/github/repo.go; keep them in sync.
func writePluginZip(w io.Writer, files map[string][]byte) error {
	zw := zip.NewWriter(w)
	paths := make([]string, 0, len(files))
	for p := range files {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	for _, p := range paths {
		// Mirrors zip.Writer.Create's defaults: Method=Deflate, Modified=now.
		// SetMode below populates ExternalAttrs + CreatorVersion to mark the
		// entry as Unix-mode so the execute bit survives extraction. The
		// remaining fields are computed by the writer (sizes, CRC) or
		// intentionally zero (no comments / extra metadata).
		header := &zip.FileHeader{
			Name:               p,
			Comment:            "",
			NonUTF8:            false,
			CreatorVersion:     0,
			ReaderVersion:      0,
			Flags:              0,
			Method:             zip.Deflate,
			Modified:           time.Now(),
			ModifiedTime:       0,
			ModifiedDate:       0,
			CRC32:              0,
			CompressedSize:     0,
			UncompressedSize:   0,
			CompressedSize64:   0,
			UncompressedSize64: 0,
			Extra:              nil,
			ExternalAttrs:      0,
		}
		var mode os.FileMode = 0o644
		if strings.HasSuffix(p, ".sh") {
			mode = 0o755
		}
		header.SetMode(mode)
		f, err := zw.CreateHeader(header)
		if err != nil {
			return fmt.Errorf("create zip entry %q: %w", p, err)
		}
		if _, err := f.Write(files[p]); err != nil {
			return fmt.Errorf("write zip entry %q: %w", p, err)
		}
	}
	if err := zw.Close(); err != nil {
		return fmt.Errorf("close zip writer: %w", err)
	}
	return nil
}

// persistDownloadAPIKey writes a single hooks-scoped key for a plugin download.
// Distinct from persistPluginAPIKeys because it does not touch the GitHub
// connection record. API key creation is intentionally excluded from the audit
// log here — plugin asset downloads are automated and would otherwise flood the
// log with api_key:create events.
func (s *Service) persistDownloadAPIKey(ctx context.Context, ac *contextvalues.AuthContext, candidate pluginAPIKeyCandidate) error {
	projectID := uuid.NullUUID{UUID: *ac.ProjectID, Valid: true}
	_, err := keysrepo.New(s.db).CreateAPIKey(ctx, keysrepo.CreateAPIKeyParams{
		OrganizationID:  ac.ActiveOrganizationID,
		Name:            candidate.keyName,
		KeyHash:         candidate.keyHash,
		KeyPrefix:       candidate.keyPrefix,
		Scopes:          []string{candidate.scope.String()},
		CreatedByUserID: ac.UserID,
		ProjectID:       projectID,
	})
	if err != nil {
		return fmt.Errorf("create api key: %w", err)
	}
	return nil
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
		Configured:                s.github != nil,
		Connected:                 false,
		RepoOwner:                 nil,
		RepoName:                  nil,
		RepoURL:                   nil,
		MarketplaceURL:            nil,
		ClaudeObservabilityPlugin: nil,
		CodexObservabilityPlugin:  nil,
		HasCollaborators:          nil,
		UpToDate:                  nil,
		LastPublishedAt:           nil,
	}

	if s.github != nil {
		conn, err := s.repo.GetGitHubConnection(ctx, *ac.ProjectID)
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return nil, oops.E(oops.CodeUnexpected, err, "get github connection").LogError(ctx, s.logger)
		}
		if err == nil {
			result.Connected = true
			result.RepoOwner = &conn.RepoOwner
			result.RepoName = &conn.RepoName
			repoURL := fmt.Sprintf("https://github.com/%s/%s", conn.RepoOwner, conn.RepoName)
			result.RepoURL = &repoURL
			// The observability plugin slugs are org-name-derived (see naming
			// package); surface them so install UIs never re-derive the formula.
			slugCfg := GenerateConfig{
				OrgName:           s.resolveOrganizationName(ctx, ac.ActiveOrganizationID, ac.OrganizationSlug),
				OrgEmail:          "",
				OrgID:             "",
				ServerURL:         "",
				APIKey:            "",
				HooksAPIKey:       "",
				ProjectSlug:       "",
				IsDefaultProject:  false,
				Version:           "",
				MarketplaceName:   "",
				HooksOrgName:      "",
				ObservabilityMode: false,
				BrowserLogin:      false,
				InstallFailOpen:   false,
			}
			result.ClaudeObservabilityPlugin = conv.PtrEmpty(ClaudeObservabilitySlug(slugCfg))
			result.CodexObservabilityPlugin = conv.PtrEmpty(CodexObservabilitySlug(slugCfg))
			if conn.MarketplaceToken.Valid && s.serverURL != "" {
				marketplaceURL := fmt.Sprintf("%s%s%s.git", s.serverURL, marketplace.RoutePrefix, conn.MarketplaceToken.String)
				result.MarketplaceURL = &marketplaceURL
			}
			// The connection row is only ever written by a successful publish, so
			// updated_at is a faithful last-published timestamp.
			if conn.UpdatedAt.Valid {
				lastPublishedAt := formatTime(conn.UpdatedAt)
				result.LastPublishedAt = &lastPublishedAt
			}
			result.UpToDate = s.publishUpToDate(ctx, ac, conn)

			hasCollaborators, err := s.cachedHasDirectCollaborator(ctx, conn.RepoOwner, conn.RepoName)
			if err != nil {
				// Degrade rather than fail the whole status read — the
				// dashboard treats a missing value as "unknown", not "false".
				s.logger.WarnContext(ctx, "check repo collaborators", attr.SlogError(err))
			} else {
				result.HasCollaborators = &hasCollaborators
			}
		}
	}

	return result, nil
}

// hasCollaboratorsCacheTTL bounds how stale the collaborator flag can be.
// GetPublishStatus is polled by the dashboard on every page load/refetch, so
// checking GitHub live on each call burns installation rate limits for data
// that only changes on publish or (async, outside our control) invitation
// acceptance — a short cache absorbs that traffic while staying close enough
// to real-time for the UI to reflect a just-added collaborator promptly.
const hasCollaboratorsCacheTTL = 60 * time.Second

func collaboratorCacheKey(owner, repo string) string {
	return fmt.Sprintf("plugins:has-collaborator:%s/%s", owner, repo)
}

// cachedHasDirectCollaborator wraps GitHubPublisher.HasDirectCollaborator
// with a short-lived cache. Falls back to an uncached live call when no
// cache is configured (e.g. the publish-only worker instance from
// NewPublisher, which never serves GetPublishStatus).
func (s *Service) cachedHasDirectCollaborator(ctx context.Context, owner, repo string) (bool, error) {
	if s.cache == nil {
		hasCollaborators, err := s.github.Client.HasDirectCollaborator(ctx, s.github.InstallationID, owner, repo)
		if err != nil {
			return false, fmt.Errorf("check repo collaborators: %w", err)
		}
		return hasCollaborators, nil
	}

	key := collaboratorCacheKey(owner, repo)

	var cached bool
	switch err := s.cache.Get(ctx, key, &cached); {
	case err == nil:
		return cached, nil
	case errors.Is(err, redisCache.ErrCacheMiss):
		// Fall through to the live check below.
	default:
		s.logger.WarnContext(ctx, "read collaborator cache", attr.SlogError(err))
	}

	hasCollaborators, err := s.github.Client.HasDirectCollaborator(ctx, s.github.InstallationID, owner, repo)
	if err != nil {
		return false, fmt.Errorf("check repo collaborators: %w", err)
	}

	if err := s.cache.Set(ctx, key, &hasCollaborators, hasCollaboratorsCacheTTL); err != nil {
		s.logger.WarnContext(ctx, "write collaborator cache", attr.SlogError(err))
	}

	return hasCollaborators, nil
}

// publishUpToDate reports whether the project's current plugin state matches
// what was last published, by recomputing the live MCP fingerprint the same way
// publishProject does and comparing both it and the current hooks generator
// version to what the connection last recorded. It returns nil ("unknown") when
// freshness can't be determined — the connection predates the hooks/MCP split,
// or recomputing the fingerprint fails — so a transient compute error degrades
// the status read rather than failing it.
func (s *Service) publishUpToDate(ctx context.Context, ac *contextvalues.AuthContext, conn repo.PluginGithubConnection) *bool {
	// Connections published before the hooks/MCP split carry no stored MCP
	// fingerprints, so there's nothing to compare against.
	if len(conn.PublishedMcpFingerprints) == 0 {
		return nil
	}

	// The fingerprint embeds the project slug, so it must match the slug used at
	// publish time. Sessions carry it; fall back to the project row otherwise.
	projectSlug := conv.PtrValOr(ac.ProjectSlug, "")
	if projectSlug == "" {
		project, err := projectsrepo.New(s.db).GetProjectByID(ctx, *ac.ProjectID)
		if err != nil {
			s.logger.WarnContext(ctx, "publish freshness: get project", attr.SlogError(err))
			return nil
		}
		projectSlug = project.Slug
	}

	pluginInfos, err := s.resolvePluginInfos(ctx, *ac.ProjectID)
	if err != nil {
		// resolvePluginInfos already logged the underlying error.
		return nil
	}

	cfg := s.generateConfig(ctx, ac.ActiveOrganizationID, ac.OrganizationSlug, projectSlug, *ac.ProjectID)
	mcpFingerprints, err := MCPFingerprints(pluginInfos, cfg)
	if err != nil {
		s.logger.WarnContext(ctx, "publish freshness: compute mcp fingerprints", attr.SlogError(err))
		return nil
	}

	// Up to date only when both components match what was last published: the MCP
	// per-plugin fingerprints, the hooks generator version, and the hook-affecting
	// config (so a marketplace rename or observability-mode toggle that hasn't
	// propagated to the hooks subtree yet reads as stale rather than current).
	upToDate := maps.Equal(mcpFingerprints, decodeMCPFingerprints(conn.PublishedMcpFingerprints)) &&
		conv.FromPGTextOrEmpty[string](conn.PublishedHooksVersion) == hooksGeneratorVersion &&
		storedHooksConfigHash(conn.PublishedHooksConfig) == hooksConfigHash(hooksConfigSnapshot(cfg))
	return &upToDate
}

// decodeMCPFingerprints parses the JSON per-plugin fingerprint map stored on a
// connection. It returns nil on empty or malformed input, so a decode failure is
// treated as "nothing matches" — the safe direction, forcing a republish that
// backfills a valid value.
func decodeMCPFingerprints(raw []byte) map[string]string {
	if len(raw) == 0 {
		return nil
	}
	m := make(map[string]string)
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil
	}
	return m
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

	for _, u := range payload.GithubUsernames {
		if u == "" || !validGitHubUsername.MatchString(u) {
			return nil, oops.E(oops.CodeBadRequest, nil, "invalid github username: %q", u)
		}
	}

	// PublishPlugins is session-only — repo names embed the project slug,
	// which API key auth doesn't populate.
	if ac.ProjectSlug == nil {
		return nil, oops.E(oops.CodeUnauthorized, nil, "publish requires a session-authenticated context")
	}

	outcome, err := s.publishProject(ctx, publishProjectInput{
		ProjectID:        *ac.ProjectID,
		ProjectName:      "",
		ProjectSlug:      *ac.ProjectSlug,
		OrganizationID:   ac.ActiveOrganizationID,
		OrganizationSlug: ac.OrganizationSlug,
		Actor: publishActor{
			Principal:       urn.NewPrincipal(urn.PrincipalTypeUser, ac.UserID),
			DisplayName:     ac.Email,
			Slug:            nil,
			CreatedByUserID: ac.UserID,
		},
		GitHubUsernames: payload.GithubUsernames,
		CommitMessage:   "Update plugin packages",
		// A human clicked Publish: always republish so the manifest version
		// bumps and installed copies refresh. The hooks component is still gated
		// by the rollout inside publishProject — clicking Publish cannot force a
		// hooks upgrade onto an org the rollout hasn't cleared.
		SkipIfUnchanged: false,
	})
	if err != nil {
		return nil, err
	}

	return &gen.PublishPluginsResult{RepoURL: outcome.RepoURL}, nil
}

type PublishProjectInput struct {
	ProjectID       uuid.UUID
	CreatedByUserID string
	CommitMessage   string
	// SkipIfUnchanged short-circuits the publish when the project's current
	// fingerprint matches the one last published, avoiding a no-op GitHub
	// commit and fresh API keys. Set by the automated rollout; the dashboard
	// publish leaves it false so a human-initiated publish always refreshes.
	//
	// There is deliberately NO flag to opt a publish into (or out of) hooks-
	// version gating: every publish path is gated unconditionally inside
	// publishProject, so no caller can force a hooks upgrade onto an org the
	// rollout hasn't cleared. The only lever to advance hooks is the
	// FlagHooksRollout payload pin in PostHog (plus the hardcoded canary).
	SkipIfUnchanged bool
}

type PublishProjectResult struct {
	RepoURL string
	// Skipped is true when SkipIfUnchanged was set and the fingerprint matched,
	// so nothing was published.
	Skipped bool
}

func (s *Service) PublishProject(ctx context.Context, input PublishProjectInput) (*PublishProjectResult, error) {
	if s.github == nil {
		return nil, fmt.Errorf("github publishing is not configured")
	}
	if input.CreatedByUserID == "" {
		return nil, fmt.Errorf("created by user id is required")
	}

	project, err := projectsrepo.New(s.db).GetProjectWithOrganizationMetadata(ctx, input.ProjectID)
	if err != nil {
		return nil, fmt.Errorf("get project with organization metadata: %w", err)
	}

	actorDisplayName := "Gram"
	result, err := s.publishProject(ctx, publishProjectInput{
		ProjectID:        project.ProjectID,
		ProjectName:      project.ProjectName,
		ProjectSlug:      project.ProjectSlug,
		OrganizationID:   project.ID,
		OrganizationSlug: project.Slug,
		Actor: publishActor{
			Principal:       urn.NewPrincipal(urn.PrincipalTypeUser, "system"),
			DisplayName:     &actorDisplayName,
			Slug:            nil,
			CreatedByUserID: input.CreatedByUserID,
		},
		GitHubUsernames: nil,
		CommitMessage:   conv.Default(input.CommitMessage, "Update plugin packages"),
		SkipIfUnchanged: input.SkipIfUnchanged,
	})
	if err != nil {
		return nil, err
	}

	return &PublishProjectResult{RepoURL: result.RepoURL, Skipped: result.Skipped}, nil
}

// HooksRolloutEligible reports whether the organization is cleared to receive the
// current observability (hooks) generator version. Exposed for other services
// (e.g. productfeatures) that must decide, before persisting an org-level setting
// that changes generated hook output, whether that change can actually be
// published to the hooks subtree. Fails closed — see hooksRolloutEligible.
func (s *Service) HooksRolloutEligible(ctx context.Context, orgID, orgSlug string) bool {
	return s.hooksRolloutEligible(ctx, orgID, orgSlug)
}

// RepublishOrganizationProjects republishes every project in the organization
// that has a plugin GitHub connection. It is used when an org-level setting that
// affects generated output (e.g. observability mode) changes and must propagate
// to all of the org's published marketplaces. SkipIfUnchanged is set so only the
// components that actually changed are regenerated (the config-hash signal picks
// up the setting change for the hooks component without rotating MCP keys), and
// the hooks component stays phase-gated. Callers that require the hooks to update
// synchronously should verify HooksRolloutEligible first; this method never fails
// the caller for an ineligible org — the change is carried and the automated
// rollout applies it once the org is eligible. Returns the joined errors of any
// per-project publishes that failed.
func (s *Service) RepublishOrganizationProjects(ctx context.Context, orgID string) error {
	if s.github == nil {
		return nil
	}
	targets, err := s.repo.ListOrgPluginPublishTargets(ctx, orgID)
	if err != nil {
		return fmt.Errorf("list org plugin publish targets: %w", err)
	}
	var errs []error
	for _, t := range targets {
		if _, err := s.PublishProject(ctx, PublishProjectInput{
			ProjectID:       t.ProjectID,
			CreatedByUserID: t.CreatedByUserID,
			CommitMessage:   "Update observability settings",
			SkipIfUnchanged: true,
		}); err != nil {
			errs = append(errs, fmt.Errorf("republish project %s: %w", t.ProjectID, err))
		}
	}
	return errors.Join(errs...)
}

type publishActor struct {
	Principal       urn.Principal
	DisplayName     *string
	Slug            *string
	CreatedByUserID string
}

type publishProjectInput struct {
	ProjectID        uuid.UUID
	ProjectName      string
	ProjectSlug      string
	OrganizationID   string
	OrganizationSlug string
	Actor            publishActor
	GitHubUsernames  []string
	CommitMessage    string
	SkipIfUnchanged  bool
}

// publishOutcome is the internal result of publishProject. Skipped is true when
// SkipIfUnchanged was set and the fingerprint matched, in which case no GitHub
// commit was made and RepoURL points at the existing repo (or is empty if the
// project has no connection yet).
// canaryHooksOrgSlugs always receive the current hooksGeneratorVersion
// immediately, bypassing the FlagHooksRollout payload. This is a code-side
// allowlist rather than PostHog group targeting on purpose: the provider
// returns no payload when PostHog is disabled or unreachable, and we never want
// such an outage to strand our own team on a stale hooks version.
var canaryHooksOrgSlugs = map[string]bool{
	"speakeasy-team": true,
}

// hooksRolloutEligible reports whether the org is cleared to receive the current
// hooksGeneratorVersion. Canary orgs always are. Otherwise the FlagHooksRollout
// payload — JSON {"version": N} naming the highest hooks version cleared for the
// org — must reach the current version. It fails closed: a missing provider,
// payload, parse error, or resolve error all mean "not eligible", so the org
// keeps its published hooks rather than rolling forward on an incomplete signal.
func (s *Service) hooksRolloutEligible(ctx context.Context, orgID, orgSlug string) bool {
	if canaryHooksOrgSlugs[orgSlug] {
		return true
	}
	if s.features == nil {
		return false
	}

	payload, err := s.features.FlagPayload(ctx, feature.FlagHooksRollout, orgID, feature.OrgProjectGroups(orgSlug, ""))
	if err != nil {
		s.logger.WarnContext(ctx, "resolve hooks rollout flag payload; carrying current hooks",
			attr.SlogOrganizationID(orgID),
			attr.SlogError(err),
		)
		return false
	}
	if len(payload) == 0 {
		return false
	}

	var pin struct {
		Version int `json:"version"`
	}
	if err := json.Unmarshal(payload, &pin); err != nil {
		s.logger.WarnContext(ctx, "parse hooks rollout flag payload; carrying current hooks",
			attr.SlogOrganizationID(orgID),
			attr.SlogError(err),
		)
		return false
	}

	// hooksGeneratorVersion is a compile-time numeric constant; a non-numeric
	// value would be a programming error, so treat it as "no one is eligible"
	// rather than silently rolling everyone forward.
	current, err := strconv.Atoi(hooksGeneratorVersion)
	if err != nil {
		return false
	}

	return pin.Version >= current
}

// publishOutcome is the internal result of publishProject. Skipped is true when
// SkipIfUnchanged was set and the fingerprint matched, in which case no GitHub
// commit was made and RepoURL points at the existing repo (or is empty if the
// project has no connection yet).
type publishOutcome struct {
	RepoURL string
	Skipped bool
	// HooksConfigDeferred is true when hook-output-affecting config changed (a
	// marketplace rename or observability-mode toggle) but the org isn't cleared
	// for the current hooks version, so the hooks subtree was carried unchanged
	// rather than regenerated (which would advance the org past the rollout gate).
	// The change applies automatically once the org becomes eligible. MCP and the
	// shared marketplace manifests still publish; only the observability hooks lag.
	HooksConfigDeferred bool
}

func (s *Service) publishProject(ctx context.Context, input publishProjectInput) (*publishOutcome, error) {
	pluginInfos, err := s.resolvePluginInfos(ctx, input.ProjectID)
	if err != nil {
		return nil, err
	}

	projectName := input.ProjectName
	if projectName == "" {
		project, err := projectsrepo.New(s.db).GetProjectByID(ctx, input.ProjectID)
		if err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "get project").LogError(ctx, s.logger)
		}
		projectName = project.Name
	}

	cfg := s.generateConfig(ctx, input.OrganizationID, input.OrganizationSlug, input.ProjectSlug, input.ProjectID)

	// The per-plugin MCP fingerprints and the hooks generator version are the two
	// independent rollout signals. Compute both up front so we can short-circuit
	// unchanged publishes before touching GitHub and persist them after a
	// successful push.
	mcpFingerprints, err := MCPFingerprints(pluginInfos, cfg)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "compute mcp fingerprints").LogError(ctx, s.logger)
	}
	mcpFingerprintsJSON, err := json.Marshal(mcpFingerprints)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "marshal mcp fingerprints").LogError(ctx, s.logger)
	}

	// GitHub repo owner/name are case-insensitive. Normalize at the boundary
	// so the rows we persist round-trip cleanly through the case-insensitive
	// unique index on (installation_id, LOWER(repo_owner), LOWER(repo_name)).
	repoOwner := strings.ToLower(s.github.Org)
	repoName := strings.ToLower(input.OrganizationSlug + "-" + input.ProjectSlug + "-plugins")
	repoURL := fmt.Sprintf("https://github.com/%s/%s", repoOwner, repoName)

	existing, connErr := s.repo.GetGitHubConnection(ctx, input.ProjectID)
	if connErr != nil && !errors.Is(connErr, pgx.ErrNoRows) {
		return nil, oops.E(oops.CodeUnexpected, connErr, "get github connection").LogError(ctx, s.logger)
	}
	firstPublish := errors.Is(connErr, pgx.ErrNoRows)

	// Decide which components to (re)generate. The hooks subtree changes only on
	// a hooksGeneratorVersion bump, so an MCP publish never touches it. A human
	// publish (SkipIfUnchanged == false) always refreshes MCP so installed copies
	// pick up a new manifest version, but still leaves hooks alone unless its
	// version bumped.
	mcpChanged := firstPublish || !input.SkipIfUnchanged ||
		!maps.Equal(mcpFingerprints, decodeMCPFingerprints(existing.PublishedMcpFingerprints))

	// Snapshot the hook-output-affecting config (resolved marketplace name,
	// observability mode, server URL, etc.). A rename or observability-mode toggle
	// changes generated hook content while leaving hooksGeneratorVersion untouched,
	// so this snapshot is the only signal that catches those; without it the hooks
	// subtree would be carried stale. The version and this config together decide
	// whether hooks must regenerate.
	currentHooksConfig := hooksConfigSnapshot(cfg)
	currentHooksConfigJSON, err := marshalHooksConfig(currentHooksConfig)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "marshal hooks config").LogError(ctx, s.logger)
	}
	currentHooksConfigHash := hooksConfigHash(currentHooksConfig)
	publishedHooksConfigHash := storedHooksConfigHash(existing.PublishedHooksConfig)

	// The hooks version + config this org should converge to. This gate is
	// UNCONDITIONAL — it is not an opt-in per call site — so no publish path (the
	// automated rollout, a dashboard publish, a marketplace rename, an
	// observability-mode toggle) can force a hooks change onto an org that the
	// rollout hasn't cleared. The single lever to advance an org is the
	// FlagHooksRollout payload pin in PostHog (plus the hardcoded canary); see
	// hooksRolloutEligible. When an org isn't eligible we carry its already-
	// published hooks verbatim (both version AND config), because regenerating
	// always lands on the current generator version and would advance it past the
	// gate. A first publish always gets the current values — there is no prior
	// hooks subtree to carry, so the gate only ever holds back UPGRADES.
	targetHooksVersion := hooksGeneratorVersion
	targetHooksConfigJSON := currentHooksConfigJSON
	targetHooksConfigHash := currentHooksConfigHash
	hooksConfigDeferred := false
	if !firstPublish && !s.hooksRolloutEligible(ctx, input.OrganizationID, input.OrganizationSlug) {
		targetHooksVersion = conv.FromPGTextOrEmpty[string](existing.PublishedHooksVersion)
		targetHooksConfigJSON = existing.PublishedHooksConfig
		targetHooksConfigHash = publishedHooksConfigHash
		if publishedHooksConfigHash != currentHooksConfigHash {
			// Hook-affecting config changed but the org isn't cleared for the
			// current hooks version, so we carry the published hooks rather than
			// regenerate (which would advance it). The change applies automatically
			// once the org becomes eligible; callers may surface the deferral.
			hooksConfigDeferred = true
			s.logger.InfoContext(ctx, "hooks config change deferred until org eligible for current hooks version",
				attr.SlogOrganizationID(input.OrganizationID))
		}
	}
	hooksChanged := firstPublish ||
		conv.FromPGTextOrEmpty[string](existing.PublishedHooksVersion) != targetHooksVersion ||
		publishedHooksConfigHash != targetHooksConfigHash

	if input.SkipIfUnchanged && !mcpChanged && !hooksChanged {
		return &publishOutcome{RepoURL: repoURL, Skipped: true, HooksConfigDeferred: hooksConfigDeferred}, nil
	}

	// When exactly one component changed, carry the other verbatim from the
	// existing repo so its files (and their embedded API key) are untouched.
	// Fetch the repo only in that case; a first publish or a both-components
	// change regenerates everything and needs no fetch.
	var existingFiles map[string][]byte
	if !firstPublish && (!mcpChanged || !hooksChanged) {
		existingFiles, err = s.github.Client.GetRepoFiles(ctx, s.github.InstallationID, repoOwner, repoName, "main")
		if err != nil {
			if !errors.Is(err, ghclient.ErrRepoNotFound) {
				return nil, oops.E(oops.CodeGatewayError, err, "get existing repo files").LogError(ctx, s.logger)
			}
			// The connection row exists but the repo is gone or empty; regenerate
			// both components rather than carrying stale files.
			existingFiles = nil
		}
	}

	// carry copies the given paths from the existing repo into dst; it reports
	// false when the fetch failed or any expected file is missing, so the caller
	// falls back to regenerating that component.
	carry := func(dst map[string][]byte, paths []string) bool {
		if existingFiles == nil {
			return false
		}
		staged := make(map[string][]byte, len(paths))
		for _, p := range paths {
			content, ok := existingFiles[p]
			if !ok {
				return false
			}
			staged[p] = content
		}
		maps.Copy(dst, staged)
		return true
	}

	files := make(map[string][]byte)
	var candidates []pluginAPIKeyCandidate

	// MCP component: carry when unchanged, otherwise regenerate with a fresh key.
	carriedMCP := false
	if !mcpChanged {
		paths, err := mcpFilePaths(pluginInfos, cfg)
		if err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "enumerate mcp files").LogError(ctx, s.logger)
		}
		carriedMCP = carry(files, paths)
	}
	if !carriedMCP {
		mcpCandidate, err := s.buildPluginAPIKeyCandidate(auth.APIKeyScopeConsumer, "mcp")
		if err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "build plugin mcp api key").LogError(ctx, s.logger)
		}
		cfg.APIKey = mcpCandidate.fullKey
		mcpFiles, err := generateMCPFiles(pluginInfos, cfg)
		if err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "generate mcp files").LogError(ctx, s.logger)
		}
		maps.Copy(files, mcpFiles)
		candidates = append(candidates, mcpCandidate)
	}

	// Hooks component: carry when the target version+config match what's
	// published (including gated orgs pinned to an older version), otherwise
	// regenerate with a fresh hooks key. The carry is prefix-based so it works
	// across generator versions with different file layouts — enumerating the
	// CURRENT generator's paths would fail against an older published layout
	// and silently regenerate past the rollout gate.
	carriedHooks := false
	carriedHooksOrgName := ""
	if !hooksChanged {
		carriedHooksOrgName, carriedHooks = carryHooksSubtree(files, existingFiles, targetHooksConfigJSON, cfg.OrgName)
	}
	if !carriedHooks {
		hooksCandidate, err := s.buildPluginAPIKeyCandidate(auth.APIKeyScopeHooks, "hooks")
		if err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "build plugin hooks api key").LogError(ctx, s.logger)
		}
		cfg.HooksAPIKey = hooksCandidate.fullKey
		hooksFiles, err := generateHooksFiles(cfg)
		if err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "generate hooks files").LogError(ctx, s.logger)
		}
		maps.Copy(files, hooksFiles)
		candidates = append(candidates, hooksCandidate)
		// What lands in the repo is the CURRENT generator's output; persist that
		// truthfully even when the rollout gate had pinned an older published
		// version — recording the stale version would make every subsequent
		// publish repeat this fallback and mint another hooks key. Reaching here
		// past the gate is only possible when the published subtree is missing
		// or unreadable, and regenerating is the one way to keep the repo
		// installable.
		if targetHooksVersion != hooksGeneratorVersion {
			s.logger.WarnContext(ctx, "published hooks subtree not carriable; regenerating at current hooks version despite rollout gate",
				attr.SlogOrganizationID(input.OrganizationID))
		}
		targetHooksVersion = hooksGeneratorVersion
		targetHooksConfigJSON = currentHooksConfigJSON
		hooksConfigDeferred = false
	}

	// Shared files (marketplace manifests + README) reference both components but
	// embed no per-publish secret or version, so they're always regenerated from
	// a config with non-empty key sentinels: only key presence matters here (it
	// selects the codex auth policy and lists the observability entry), and a
	// published repo always has both keys. Using sentinels keeps these files
	// byte-identical to what MCPFingerprint hashed.
	sharedCfg := cfg
	sharedCfg.APIKey = fingerprintAPIKeySentinel
	sharedCfg.HooksAPIKey = fingerprintHooksKeySentinel
	// A carried subtree keeps the directory names it was published under, which
	// diverge from cfg.OrgName after an org rename; point the regenerated
	// manifests' observability entries at the carried directories so they stay
	// resolvable.
	sharedCfg.HooksOrgName = carriedHooksOrgName
	sharedFiles, err := generateSharedFiles(pluginInfos, sharedCfg)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "generate shared files").LogError(ctx, s.logger)
	}
	maps.Copy(files, sharedFiles)

	if err := s.github.Client.CreateRepo(ctx, s.github.InstallationID, repoOwner, repoName, true); err != nil {
		return nil, oops.E(oops.CodeGatewayError, err, "create github repo").LogError(ctx, s.logger)
	}

	_, err = s.github.Client.PushFiles(
		ctx,
		s.github.InstallationID,
		repoOwner,
		repoName,
		"main",
		input.CommitMessage,
		files,
	)
	if err != nil {
		return nil, oops.E(oops.CodeGatewayError, err, "push plugin files to GitHub").LogError(ctx, s.logger)
	}

	for _, username := range input.GitHubUsernames {
		if err := s.github.Client.AddCollaborator(ctx, s.github.InstallationID, repoOwner, repoName, username, "pull"); err != nil {
			s.logger.WarnContext(ctx, "failed to add collaborator (non-fatal)",
				attr.SlogOrganizationID(input.OrganizationID),
				attr.SlogGitHubUsername(username),
				attr.SlogError(err),
			)
		}
	}

	// Bust the short-lived HasDirectCollaborator cache so the next
	// GetPublishStatus read reflects a just-added collaborator immediately
	// instead of the stale cached value for up to hasCollaboratorsCacheTTL.
	if len(input.GitHubUsernames) > 0 && s.cache != nil {
		if err := s.cache.Delete(ctx, collaboratorCacheKey(repoOwner, repoName)); err != nil {
			s.logger.WarnContext(ctx, "invalidate collaborator cache", attr.SlogError(err))
		}
	}

	pluginSlugs := make([]string, 0, len(pluginInfos))
	for _, p := range pluginInfos {
		pluginSlugs = append(pluginSlugs, p.Slug)
	}

	// Persist the API keys, audit logs, and github connection atomically only
	// after the GitHub publish has succeeded. This prevents leaking valid
	// credentials when GitHub fails. If this transaction itself fails, the
	// published repo contains key strings with no DB records — re-publish
	// overwrites them with fresh valid keys.
	if err := s.persistPluginAPIKeys(ctx, input, candidates, projectName, repoOwner, repoName, pluginSlugs, mcpFingerprintsJSON, targetHooksVersion, targetHooksConfigJSON); err != nil {
		if errors.Is(err, ErrGitHubRepoConflict) {
			return nil, oops.E(oops.CodeConflict, err, "persist plugin api keys").LogWarn(ctx, s.logger)
		}
		return nil, oops.E(oops.CodeUnexpected, err, "persist plugin api keys").LogError(ctx, s.logger)
	}

	return &publishOutcome{RepoURL: repoURL, Skipped: false, HooksConfigDeferred: hooksConfigDeferred}, nil
}

// carryHooksSubtree copies the published hooks (observability) subtree
// verbatim into dst by directory prefix (see hooksSubtreePrefixes). The
// prefixes derive from the published hooks config's org name — the org may
// have been renamed since publish — falling back to the current org name when
// the stored config predates the snapshot. Returns the org name the carried
// directories derive from (for GenerateConfig.HooksOrgName) and whether the
// carry succeeded; false means a platform's subtree is missing and the caller
// must regenerate.
func carryHooksSubtree(dst, existing map[string][]byte, publishedConfig []byte, currentOrgName string) (string, bool) {
	if len(existing) == 0 {
		return "", false
	}
	orgName := currentOrgName
	var hc HooksConfig
	if err := json.Unmarshal(publishedConfig, &hc); err == nil && hc.OrgName != "" {
		orgName = hc.OrgName
	}
	staged := make(map[string][]byte)
	for _, prefix := range hooksSubtreePrefixes(orgName) {
		found := false
		for p, content := range existing {
			if strings.HasPrefix(p, prefix) {
				staged[p] = content
				found = true
			}
		}
		if !found {
			return "", false
		}
	}
	maps.Copy(dst, staged)
	return orgName, true
}

// validMarketplaceName matches identifiers Claude Code, Cursor, and Codex
// accept as the marketplace name in marketplace.json — lowercase alphanumerics
// and hyphens, 1–64 chars, can't start or end with a hyphen.
var validMarketplaceName = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{0,62}[a-z0-9])?$`)

func (s *Service) GetMarketplaceSettings(ctx context.Context, payload *gen.GetMarketplaceSettingsPayload) (*gen.MarketplaceSettingsResult, error) {
	ac, err := s.authContext(ctx)
	if err != nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeOrgRead, ResourceKind: "", ResourceID: ac.ActiveOrganizationID, Dimensions: nil}); err != nil {
		return nil, err
	}

	var override string
	settings, err := s.repo.GetMarketplaceSettings(ctx, *ac.ProjectID)
	switch {
	case err == nil:
		override = conv.FromPGTextOrEmpty[string](settings.MarketplaceName)
	case errors.Is(err, pgx.ErrNoRows):
		// No row yet — leave override empty so the effective name is the default.
	default:
		return nil, oops.E(oops.CodeUnexpected, err, "get marketplace settings").LogError(ctx, s.logger)
	}

	defaultName := s.resolveDefaultMarketplaceName(ctx, ac.ActiveOrganizationID, ac.OrganizationSlug, *ac.ProjectID)

	effective := override
	if effective == "" {
		effective = defaultName
	}

	return &gen.MarketplaceSettingsResult{
		MarketplaceName: conv.PtrEmpty(override),
		DefaultName:     defaultName,
		EffectiveName:   effective,
	}, nil
}

func (s *Service) UpdateMarketplaceSettings(ctx context.Context, payload *gen.UpdateMarketplaceSettingsPayload) (*gen.UpdateMarketplaceSettingsResult, error) {
	ac, err := s.authContext(ctx)
	if err != nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeOrgAdmin, ResourceKind: "", ResourceID: ac.ActiveOrganizationID, Dimensions: nil}); err != nil {
		return nil, err
	}

	// Empty / whitespace-only input clears the override. A non-empty value must
	// be a valid marketplace slug for all three platforms.
	override := strings.TrimSpace(conv.PtrValOr(payload.MarketplaceName, ""))
	if override != "" && !validMarketplaceName.MatchString(override) {
		return nil, oops.E(oops.CodeBadRequest, nil, "invalid marketplace name: must be 1-64 chars of lowercase letters, digits, or hyphens, and may not start or end with a hyphen")
	}

	if _, err := s.repo.UpsertMarketplaceSettings(ctx, repo.UpsertMarketplaceSettingsParams{
		ProjectID:       *ac.ProjectID,
		MarketplaceName: conv.ToPGTextEmpty(override),
	}); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "upsert marketplace settings").LogError(ctx, s.logger)
	}

	// Republish only when GitHub is configured AND a connection already exists
	// for this project. A first-time publish goes through PublishPlugins so the
	// caller can supply collaborator usernames.
	republished := false
	hooksUpdateDeferred := false
	if s.github != nil && ac.ProjectSlug != nil {
		_, connErr := s.repo.GetGitHubConnection(ctx, *ac.ProjectID)
		switch {
		case connErr == nil:
			outcome, err := s.publishProject(ctx, publishProjectInput{
				ProjectID:        *ac.ProjectID,
				ProjectName:      "",
				ProjectSlug:      *ac.ProjectSlug,
				OrganizationID:   ac.ActiveOrganizationID,
				OrganizationSlug: ac.OrganizationSlug,
				Actor: publishActor{
					Principal:       urn.NewPrincipal(urn.PrincipalTypeUser, ac.UserID),
					DisplayName:     ac.Email,
					Slug:            nil,
					CreatedByUserID: ac.UserID,
				},
				GitHubUsernames: nil,
				CommitMessage:   "Update marketplace name",
				// A human changed the marketplace name: always republish so the
				// new name propagates to installed copies (MCP + marketplace.json).
				// The hooks component is gated by the rollout inside publishProject:
				// if the org isn't cleared, the new name still reaches MCP and the
				// marketplace manifests while the Codex hooks are carried and catch
				// up once eligible; the outcome reports that so we can tell the user.
				SkipIfUnchanged: false,
			})
			if err != nil {
				return nil, err
			}
			republished = true
			hooksUpdateDeferred = outcome.HooksConfigDeferred
		case errors.Is(connErr, pgx.ErrNoRows):
			// No published marketplace yet — settings saved, no republish.
		default:
			return nil, oops.E(oops.CodeUnexpected, connErr, "get github connection").LogError(ctx, s.logger)
		}
	}

	defaultName := s.resolveDefaultMarketplaceName(ctx, ac.ActiveOrganizationID, ac.OrganizationSlug, *ac.ProjectID)

	effective := override
	if effective == "" {
		effective = defaultName
	}

	return &gen.UpdateMarketplaceSettingsResult{
		Settings: &gen.MarketplaceSettingsResult{
			MarketplaceName: conv.PtrEmpty(override),
			DefaultName:     defaultName,
			EffectiveName:   effective,
		},
		Republished:         republished,
		HooksUpdateDeferred: &hooksUpdateDeferred,
	}, nil
}

// resolveDefaultMarketplaceName mirrors generateConfig's name resolution: prefer
// the human-readable org name from organization_metadata so the displayed
// default matches what the publish flow actually generates, falling back to the
// org slug from the auth context if the lookup fails. The project slug and
// default-ness are read from the project row (not the auth context, which some
// flows like project-scoped API keys leave unset) so non-default projects get
// their correct project-scoped name.
func (s *Service) resolveDefaultMarketplaceName(ctx context.Context, orgID, orgSlug string, projectID uuid.UUID) string {
	orgName := s.resolveOrganizationName(ctx, orgID, orgSlug)

	pctx, err := s.repo.GetProjectMarketplaceNameContext(ctx, projectID)
	if err != nil {
		// Without the project row we can't safely scope the name; the bare
		// org-derived default is the least-surprising fallback for display.
		s.logger.WarnContext(ctx, "failed to resolve project marketplace context, falling back to org default name",
			attr.SlogProjectID(projectID.String()),
			attr.SlogError(err),
		)
		return DefaultMarketplaceName(orgName, "", true)
	}
	return DefaultMarketplaceName(orgName, pctx.ProjectSlug, pctx.IsDefaultProject)
}

// resolveOrganizationName returns the org's display name, falling back to the
// auth-context slug when the org row is missing or unreadable. The name feeds
// the org-derived marketplace and observability-plugin slug formulas.
func (s *Service) resolveOrganizationName(ctx context.Context, orgID, orgSlug string) string {
	orgName := orgSlug
	switch fetched, err := s.repo.GetOrganizationName(ctx, orgID); {
	case err == nil:
		orgName = fetched
	case errors.Is(err, pgx.ErrNoRows):
		// Use the slug from auth context.
	default:
		s.logger.WarnContext(ctx, "failed to fetch organization name, falling back to slug",
			attr.SlogOrganizationID(orgID),
			attr.SlogError(err),
		)
	}
	return orgName
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
	scope     auth.APIKeyScope
}

// buildPluginAPIKeyCandidate generates an API key in memory without writing
// to the database. The caller must subsequently call persistPluginAPIKeys to
// commit the key. `purpose` is embedded in the key name so admins can tell
// distinct keys apart in the dashboard (e.g., "mcp" vs "hooks").
func (s *Service) buildPluginAPIKeyCandidate(scope auth.APIKeyScope, purpose string) (pluginAPIKeyCandidate, error) {
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
		keyName:   fmt.Sprintf("%s%s-%s-%s", auth.PluginAPIKeyNamePrefix, purpose, time.Now().UTC().Format("20060102-150405"), token[:6]),
		scope:     scope,
	}, nil
}

// ErrGitHubRepoConflict indicates the computed repo_owner/repo_name for this
// project's publish already belongs to a different, still-active project's
// GitHub connection (plugin_github_connections_installation_repo_key) — see
// upsertGitHubConnection, which self-heals the far more common case of a
// stale row from a soft-deleted project and only returns this when the
// blocking project is genuinely still active. Callers should treat this as a
// non-retryable, human-actionable condition rather than a transient failure.
var ErrGitHubRepoConflict = errors.New("github repo already connected to a different active project")

// upsertGitHubConnection wraps UpsertGitHubConnection with a SAVEPOINT so a
// plugin_github_connections_installation_repo_key conflict can self-heal in
// band instead of always surfacing to the caller. repoName is derived from
// org/project slugs (see publishProject), and projects_organization_id_slug_key
// is a partial unique index scoped to non-deleted projects — so a
// soft-deleted project's slug can be reused, but its plugin_github_connections
// row is never cleaned up (soft deletes don't cascade). When that's the
// blocking row, it's safe to reclaim: delete the stale connection and retry
// once. A conflict against a still-active project's connection can't be
// resolved here (its slug is legitimately still in use) and surfaces as
// ErrGitHubRepoConflict — this should be rare to impossible given active
// slugs are unique, but is not provably unreachable (e.g. two installations
// mapping to the same org externally), so it's handled rather than assumed
// away.
//
// Takes the raw transaction for the same reason as EnsureDefaultPlugin: a
// Postgres transaction aborts after any failed statement, so recovering from
// the expected unique-violation to run the reclaim + retry needs a savepoint.
func (s *Service) upsertGitHubConnection(ctx context.Context, tx pgx.Tx, params repo.UpsertGitHubConnectionParams) (repo.PluginGithubConnection, error) {
	q := repo.New(tx)

	const savepoint = "upsert_github_connection"
	if _, err := tx.Exec(ctx, "SAVEPOINT "+savepoint); err != nil {
		return repo.PluginGithubConnection{}, fmt.Errorf("begin savepoint: %w", err)
	}

	conn, err := q.UpsertGitHubConnection(ctx, params)
	if err == nil {
		if _, err := tx.Exec(ctx, "RELEASE SAVEPOINT "+savepoint); err != nil {
			return repo.PluginGithubConnection{}, fmt.Errorf("release savepoint: %w", err)
		}
		return conn, nil
	}

	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != pgerrcode.UniqueViolation || pgErr.ConstraintName != "plugin_github_connections_installation_repo_key" {
		return repo.PluginGithubConnection{}, fmt.Errorf("upsert github connection: %w", err)
	}

	if _, err := tx.Exec(ctx, "ROLLBACK TO SAVEPOINT "+savepoint); err != nil {
		return repo.PluginGithubConnection{}, fmt.Errorf("rollback savepoint after repo conflict: %w", err)
	}

	owner, err := q.GetGitHubConnectionOwner(ctx, repo.GetGitHubConnectionOwnerParams{
		InstallationID: params.InstallationID,
		RepoOwner:      params.RepoOwner,
		RepoName:       params.RepoName,
	})
	if err != nil {
		return repo.PluginGithubConnection{}, fmt.Errorf("%w: %s/%s (owner lookup failed: %w)", ErrGitHubRepoConflict, params.RepoOwner, params.RepoName, err)
	}
	if !owner.ProjectDeleted {
		return repo.PluginGithubConnection{}, fmt.Errorf("%w: %s/%s", ErrGitHubRepoConflict, params.RepoOwner, params.RepoName)
	}

	if err := q.DeleteGitHubConnection(ctx, owner.ProjectID); err != nil {
		return repo.PluginGithubConnection{}, fmt.Errorf("reclaim stale github connection: %w", err)
	}

	conn, err = q.UpsertGitHubConnection(ctx, params)
	if err != nil {
		return repo.PluginGithubConnection{}, fmt.Errorf("upsert github connection after reclaiming stale connection: %w", err)
	}
	return conn, nil
}

// persistPluginAPIKeys atomically writes one or more API keys, their audit
// log entries, the plugin publish audit log entry, and the GitHub
// connection record in a single transaction. All-or-nothing: if any
// candidate fails to insert, none are persisted.
func (s *Service) persistPluginAPIKeys(
	ctx context.Context,
	input publishProjectInput,
	candidates []pluginAPIKeyCandidate,
	projectName string,
	repoOwner, repoName string,
	pluginSlugs []string,
	mcpFingerprintsJSON []byte,
	hooksVersion string,
	hooksConfigJSON []byte,
) error {
	projectID := uuid.NullUUID{UUID: input.ProjectID, Valid: true}

	tx, err := s.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer o11y.NoLogDefer(func() error { return tx.Rollback(ctx) })

	keysQ := keysrepo.New(tx)
	for _, candidate := range candidates {
		scopes := []string{candidate.scope.String()}
		createdKey, err := keysQ.CreateAPIKey(ctx, keysrepo.CreateAPIKeyParams{
			OrganizationID:  input.OrganizationID,
			Name:            candidate.keyName,
			KeyHash:         candidate.keyHash,
			KeyPrefix:       candidate.keyPrefix,
			Scopes:          scopes,
			CreatedByUserID: input.Actor.CreatedByUserID,
			ProjectID:       projectID,
		})
		if err != nil {
			return fmt.Errorf("create api key %s: %w", candidate.keyName, err)
		}

		if err := s.audit.LogKeyCreate(ctx, tx, audit.LogKeyCreateEvent{
			OrganizationID:   input.OrganizationID,
			ProjectID:        projectID,
			Actor:            input.Actor.Principal,
			ActorDisplayName: input.Actor.DisplayName,
			ActorSlug:        input.Actor.Slug,
			KeyURN:           urn.NewAPIKey(createdKey.ID),
			KeyName:          candidate.keyName,
			Scopes:           scopes,
		}); err != nil {
			return fmt.Errorf("audit log key creation %s: %w", candidate.keyName, err)
		}
	}

	// Mint a candidate marketplace token for first-time publishes. The upsert
	// preserves any existing token via COALESCE, so passing a fresh value on
	// every publish never overwrites a token that's already minted — token
	// rotation goes through a dedicated path.
	candidateToken, err := generateMarketplaceToken()
	if err != nil {
		return fmt.Errorf("generate marketplace token: %w", err)
	}
	if _, err := s.upsertGitHubConnection(ctx, tx, repo.UpsertGitHubConnectionParams{
		ProjectID:                input.ProjectID,
		InstallationID:           s.github.InstallationID,
		RepoOwner:                repoOwner,
		RepoName:                 repoName,
		MarketplaceToken:         pgtype.Text{String: candidateToken, Valid: true},
		PublishedMcpFingerprints: mcpFingerprintsJSON,
		PublishedHooksVersion:    conv.ToPGText(hooksVersion),
		PublishedHooksConfig:     hooksConfigJSON,
	}); err != nil {
		return err
	}

	if err := s.audit.LogPluginPublish(ctx, tx, audit.LogPluginPublishEvent{
		OrganizationID:   input.OrganizationID,
		ProjectID:        input.ProjectID,
		ProjectName:      projectName,
		ProjectSlug:      input.ProjectSlug,
		Actor:            input.Actor.Principal,
		ActorDisplayName: input.Actor.DisplayName,
		ActorSlug:        input.Actor.Slug,
		PluginSlugs:      pluginSlugs,
		RepoOwner:        repoOwner,
		RepoName:         repoName,
	}); err != nil {
		return fmt.Errorf("audit log plugin publish: %w", err)
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
		return nil, oops.E(oops.CodeUnexpected, err, "list plugins with servers").LogError(ctx, s.logger)
	}

	// Remote MCP-backed (mcp_server) plugin servers are resolved by a separate
	// query and merged in below. Both backends are supported simultaneously
	// until the AGE-1902 cutover.
	mcpRows, err := s.repo.ListPluginsWithMcpServersForProject(ctx, projectID)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list plugins with mcp servers").LogError(ctx, s.logger)
	}

	// serverBuild carries the row's sort_order so the merged toolset- and
	// mcp_server-backed servers can be re-sorted per plugin (the per-query SQL
	// ordering is lost once the two result sets are combined).
	type serverBuild struct {
		info      PluginServerInfo
		sortOrder int32
	}
	type pluginBuild struct {
		info    PluginInfo
		servers []serverBuild
	}
	pluginMap := make(map[uuid.UUID]*pluginBuild)
	mcpMeta := mcpmetarepo.New(s.db)

	ensurePlugin := func(id uuid.UUID, name, slug string, description pgtype.Text) *pluginBuild {
		pb, ok := pluginMap[id]
		if !ok {
			pb = &pluginBuild{
				info: PluginInfo{
					Name:        name,
					Slug:        slug,
					Description: conv.FromPGTextOrEmpty[string](description),
					Servers:     nil,
				},
				servers: nil,
			}
			pluginMap[id] = pb
		}
		return pb
	}

	for _, r := range rows {
		pb := ensurePlugin(r.PluginID, r.PluginName, r.PluginSlug, r.PluginDescription)

		if mcpSlug := conv.FromPGText[string](r.ToolsetMcpSlug); mcpSlug != nil {
			mcpBase := s.serverURL
			if cd := conv.FromPGText[string](r.ToolsetCustomDomain); cd != nil {
				mcpBase = fmt.Sprintf("https://%s", *cd)
			}
			serverInfo := PluginServerInfo{
				DisplayName: r.ServerDisplayName,
				Policy:      r.ServerPolicy,
				MCPURL:      fmt.Sprintf("%s/mcp/%s", mcpBase, *mcpSlug),
				IsPublic:    r.ToolsetIsPublic,
				IsOAuth:     r.ToolsetIsOauth,
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
					return nil, oops.E(oops.CodeUnexpected, metaErr, "load mcp metadata for toolset").LogError(ctx, s.logger)
				default:
					envConfigs, envErr := mcpMeta.ListEnvironmentConfigs(ctx, metadata.ID)
					if envErr != nil {
						return nil, oops.E(oops.CodeUnexpected, envErr, "load environment configs for toolset").LogError(ctx, s.logger)
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
								attr.SlogToolsetID(r.ToolsetID.UUID.String()),
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

			pb.servers = append(pb.servers, serverBuild{info: serverInfo, sortOrder: r.ServerSortOrder})
		}
	}

	for _, m := range mcpRows {
		pb := ensurePlugin(m.PluginID, m.PluginName, m.PluginSlug, m.PluginDescription)

		// Custom-domain endpoints are served from the domain host; platform
		// endpoints from the Gram server URL. The query already resolved the
		// single preferred endpoint per server.
		mcpBase := s.serverURL
		if cd := conv.FromPGText[string](m.EndpointCustomDomain); cd != nil {
			mcpBase = fmt.Sprintf("https://%s", *cd)
		}
		// Remote MCP-backed servers authenticate via their user session issuer
		// (OAuth), so the generated config carries no static Authorization
		// header (IsOAuth). Environments are not yet wired to mcp_servers, so
		// there are no public env configs to surface.
		pb.servers = append(pb.servers, serverBuild{
			info: PluginServerInfo{
				DisplayName: m.ServerDisplayName,
				Policy:      m.ServerPolicy,
				MCPURL:      fmt.Sprintf("%s/mcp/%s", mcpBase, m.EndpointSlug),
				IsPublic:    false,
				IsOAuth:     true,
				EnvConfigs:  nil,
			},
			sortOrder: m.ServerSortOrder,
		})
	}

	pluginInfos := make([]PluginInfo, 0, len(pluginMap))
	for _, pb := range pluginMap {
		// Re-sort the merged toolset- and mcp_server-backed servers by
		// sort_order (then display name for a stable tiebreak) since combining
		// the two query result sets discards their per-query ordering.
		sort.SliceStable(pb.servers, func(i, j int) bool {
			if pb.servers[i].sortOrder != pb.servers[j].sortOrder {
				return pb.servers[i].sortOrder < pb.servers[j].sortOrder
			}
			return pb.servers[i].info.DisplayName < pb.servers[j].info.DisplayName
		})
		servers := make([]PluginServerInfo, 0, len(pb.servers))
		for _, sb := range pb.servers {
			servers = append(servers, sb.info)
		}
		pb.info.Servers = servers
		pluginInfos = append(pluginInfos, pb.info)
	}
	sort.Slice(pluginInfos, func(i, j int) bool {
		return pluginInfos[i].Slug < pluginInfos[j].Slug
	})
	return pluginInfos, nil
}

func (s *Service) generateConfig(ctx context.Context, orgID, orgSlug, projectSlug string, projectID uuid.UUID) GenerateConfig {
	cfg := GenerateConfig{
		OrgName:     orgSlug,
		OrgEmail:    "",
		OrgID:       orgID,
		ServerURL:   s.serverURL,
		APIKey:      "",
		HooksAPIKey: "",
		ProjectSlug: projectSlug,
		// Milliseconds rather than seconds so publishes close together (e.g. a
		// settings flip right after a publish) still mint distinct manifest
		// versions; the 13-digit patch also sorts numerically above the
		// 10-digit second epochs already in installed caches.
		Version:           fmt.Sprintf("%d", time.Now().UnixMilli()),
		MarketplaceName:   "",
		HooksOrgName:      "",
		IsDefaultProject:  s.isDefaultProject(ctx, projectID),
		ObservabilityMode: false,
		BrowserLogin:      false,
		InstallFailOpen:   false,
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
	settings, err := s.repo.GetMarketplaceSettings(ctx, projectID)
	switch {
	case err == nil:
		cfg.MarketplaceName = conv.FromPGTextOrEmpty[string](settings.MarketplaceName)
	case !errors.Is(err, pgx.ErrNoRows):
		s.logger.WarnContext(ctx, "failed to fetch marketplace settings, falling back to default",
			attr.SlogProjectID(projectID.String()),
			attr.SlogError(err),
		)
	}
	// observability_mode is the org-level non-blocking switch managed by the
	// productfeatures service against organization_features. When on, the
	// generated plugin emits async for every hook event. The read is an EXISTS
	// check so it never returns pgx.ErrNoRows; any error leaves the flag off,
	// keeping hooks blocking.
	observabilityMode, err := s.repo.IsOrganizationFeatureEnabled(ctx, repo.IsOrganizationFeatureEnabledParams{
		OrganizationID: orgID,
		FeatureName:    string(productfeatures.FeatureObservabilityMode),
	})
	if err != nil {
		s.logger.WarnContext(ctx, "failed to read observability mode flag, defaulting to blocking hooks",
			attr.SlogOrganizationID(orgID),
			attr.SlogError(err),
		)
	}
	cfg.ObservabilityMode = observabilityMode
	// hooks_browser_login is the org-level opt-in for the interactive browser
	// token exchange. Off (or unreadable), the generated plugin never opens a
	// browser: senders authenticate through env credentials, a previously
	// cached key, or the baked org-wide key.
	browserLogin, err := s.repo.IsOrganizationFeatureEnabled(ctx, repo.IsOrganizationFeatureEnabledParams{
		OrganizationID: orgID,
		FeatureName:    string(productfeatures.FeatureHooksBrowserLogin),
	})
	if err != nil {
		s.logger.WarnContext(ctx, "failed to read hooks browser login flag, defaulting to no browser login",
			attr.SlogOrganizationID(orgID),
			attr.SlogError(err),
		)
	}
	cfg.BrowserLogin = browserLogin
	// hooks_install_fail_open is the org-level install-failure policy for the
	// hooks binary bootstrap. Off (or unreadable), a distribution failure fails
	// closed — the same posture as every other hook failure in enforcement mode.
	installFailOpen, err := s.repo.IsOrganizationFeatureEnabled(ctx, repo.IsOrganizationFeatureEnabledParams{
		OrganizationID: orgID,
		FeatureName:    string(productfeatures.FeatureHooksInstallFailOpen),
	})
	if err != nil {
		s.logger.WarnContext(ctx, "failed to read hooks install-fail-open flag, defaulting to fail closed",
			attr.SlogOrganizationID(orgID),
			attr.SlogError(err),
		)
	}
	cfg.InstallFailOpen = installFailOpen
	return cfg
}

// isDefaultProject reports whether projectID is the org's default project (its
// oldest, by id ASC). Resolved identically to the device-agent endpoint so the
// project-scoped marketplace name the publish path stamps matches what the
// endpoint emits. On error it treats the project as non-default — the safe
// direction, since a stray bare org name colliding with the real default is
// worse than an extra project-scoped one.
func (s *Service) isDefaultProject(ctx context.Context, projectID uuid.UUID) bool {
	pctx, err := s.repo.GetProjectMarketplaceNameContext(ctx, projectID)
	if err != nil {
		s.logger.WarnContext(ctx, "failed to resolve org default project; treating as non-default",
			attr.SlogProjectID(projectID.String()),
			attr.SlogError(err),
		)
		return false
	}
	return pctx.IsDefaultProject
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
		IsDefault:       conv.FromPGBool[bool](p.IsDefault),
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
	result := &gen.PluginServer{
		ID:          s.ID.String(),
		ToolsetID:   nil,
		McpServerID: nil,
		DisplayName: s.DisplayName,
		Policy:      s.Policy,
		SortOrder:   s.SortOrder,
		CreatedAt:   formatTime(s.CreatedAt),
	}
	// Exactly one backend is set per row (DB XOR check); populate whichever.
	if s.ToolsetID.Valid {
		id := s.ToolsetID.UUID.String()
		result.ToolsetID = &id
	}
	if s.McpServerID.Valid {
		id := s.McpServerID.UUID.String()
		result.McpServerID = &id
	}
	return result
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
