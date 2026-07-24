package mcpservers

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgerrcode"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/otel/trace"
	goahttp "goa.design/goa/v3/http"
	"goa.design/goa/v3/security"

	srv "github.com/speakeasy-api/gram/server/gen/http/mcp_servers/server"
	gen "github.com/speakeasy-api/gram/server/gen/mcp_servers"
	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/auth"
	"github.com/speakeasy-api/gram/server/internal/auth/sessions"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/background"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	environmentsrepo "github.com/speakeasy-api/gram/server/internal/environments/repo"
	"github.com/speakeasy-api/gram/server/internal/mcp/toolfilter"
	mcpendpointsrepo "github.com/speakeasy-api/gram/server/internal/mcpendpoints/repo"
	"github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
	"github.com/speakeasy-api/gram/server/internal/middleware"
	"github.com/speakeasy-api/gram/server/internal/mv"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/plugins"
	pluginsrepo "github.com/speakeasy-api/gram/server/internal/plugins/repo"
	remotemcprepo "github.com/speakeasy-api/gram/server/internal/remotemcp/repo"
	tenv "github.com/speakeasy-api/gram/server/internal/temporal"
	toolsetsrepo "github.com/speakeasy-api/gram/server/internal/toolsets/repo"
	tunneledmcprepo "github.com/speakeasy-api/gram/server/internal/tunneledmcp/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
	usersessionsrepo "github.com/speakeasy-api/gram/server/internal/usersessions/repo"
	variationsrepo "github.com/speakeasy-api/gram/server/internal/variations/repo"
)

type Service struct {
	tracer               trace.Tracer
	logger               *slog.Logger
	db                   *pgxpool.Pool
	auth                 *auth.Auth
	authz                *authz.Engine
	audit                *audit.Logger
	temporalEnv          *tenv.Environment
	pluginsGitHubEnabled bool
}

var _ gen.Service = (*Service)(nil)
var _ gen.Auther = (*Service)(nil)

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
	logger = logger.With(attr.SlogComponent("mcpservers"))

	return &Service{
		tracer:               tracerProvider.Tracer("github.com/speakeasy-api/gram/server/internal/mcpservers"),
		logger:               logger,
		db:                   db,
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

func (s *Service) CreateMcpServer(ctx context.Context, payload *gen.CreateMcpServerPayload) (*types.McpServer, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	if err := s.authz.Require(ctx, authz.MCPCheck(authz.ScopeMCPWrite, authCtx.ProjectID.String(), authCtx.ProjectID.String())); err != nil {
		return nil, err
	}

	logger := s.logger.With(attr.SlogProjectID(authCtx.ProjectID.String()))

	name := strings.TrimSpace(payload.Name)
	if name == "" {
		return nil, oops.E(oops.CodeBadRequest, nil, "name must be non-empty").LogError(ctx, logger)
	}

	ids, err := parseServerIDs(
		payload.EnvironmentID,
		payload.RemoteMcpServerID,
		payload.TunneledMcpServerID,
		payload.ToolsetID,
		payload.ToolVariationsGroupID,
	)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid mcp server").LogError(ctx, logger)
	}
	if err := validateServerBackendExclusivity(ids.RemoteMcpServerID, ids.TunneledMcpServerID, ids.ToolsetID); err != nil {
		return nil, oops.E(oops.CodeInvalid, err, "invalid mcp server").LogError(ctx, logger)
	}
	if err := validateTunneledMCPVisibility(ids, payload.Visibility); err != nil {
		return nil, oops.E(oops.CodeInvalid, err, "invalid mcp server").LogWarn(ctx, logger)
	}

	// Generate the server ID up front so the slug can include its suffix and
	// the row can be inserted in a single statement (no insert-then-update).
	serverID, err := uuid.NewV7()
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "generate server id").LogError(ctx, logger)
	}

	slug, err := computeServerSlug(name, serverID)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "compute server slug").LogError(ctx, logger)
	}

	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "begin transaction").LogError(ctx, logger)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })

	txRepo := repo.New(dbtx)

	if err := verifyServerReferenceOwnership(ctx, dbtx, *authCtx.ProjectID, ids); err != nil {
		return nil, oops.E(oops.CodeInvalid, err, "invalid mcp server").LogError(ctx, logger)
	}

	// Remote- and tunneled-backed servers carry a user_session_issuer for
	// their lifetime (mcp_servers_issuer_required_check). Mint it here in the
	// same transaction as the server row so a failed create can never leak an
	// orphan issuer.
	if ids.RemoteMcpServerID.Valid || ids.TunneledMcpServerID.Valid {
		ids.UserSessionIssuerID, err = mintServerUserSessionIssuer(ctx, dbtx, *authCtx.ProjectID, slug)
		if err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "mint mcp server issuer").LogError(ctx, logger)
		}
	}

	server, err := txRepo.CreateMCPServer(ctx, repo.CreateMCPServerParams{
		ID:                    serverID,
		ProjectID:             *authCtx.ProjectID,
		Name:                  conv.ToPGText(name),
		Slug:                  conv.ToPGText(slug),
		EnvironmentID:         ids.EnvironmentID,
		UserSessionIssuerID:   ids.UserSessionIssuerID,
		RemoteMcpServerID:     ids.RemoteMcpServerID,
		TunneledMcpServerID:   ids.TunneledMcpServerID,
		ToolsetID:             ids.ToolsetID,
		ToolVariationsGroupID: ids.ToolVariationsGroupID,
		Visibility:            string(payload.Visibility),
	})
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == pgerrcode.UniqueViolation {
			return nil, oops.E(oops.CodeConflict, err, "mcp server slug already in use").LogError(ctx, logger)
		}
		return nil, oops.E(oops.CodeUnexpected, err, "create mcp server").LogError(ctx, logger)
	}

	if err := s.audit.LogMcpServerCreate(ctx, dbtx, audit.LogMcpServerCreateEvent{
		OrganizationID:   authCtx.ActiveOrganizationID,
		ProjectID:        *authCtx.ProjectID,
		Actor:            urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID),
		ActorDisplayName: authCtx.Email,
		ActorSlug:        nil,
		McpServerURN:     urn.NewMcpServer(server.ID),
		McpServerName:    name,
		McpServerSlug:    slug,
	}); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "log mcp server creation").LogError(ctx, logger)
	}

	if err := dbtx.Commit(ctx); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "commit transaction").LogError(ctx, logger)
	}

	return mv.BuildMcpServerView(server), nil
}

func (s *Service) GetMcpServer(ctx context.Context, payload *gen.GetMcpServerPayload) (*types.McpServer, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	if err := s.authz.Require(ctx, authz.MCPCheck(authz.ScopeMCPRead, authCtx.ProjectID.String(), authCtx.ProjectID.String())); err != nil {
		return nil, err
	}

	idProvided := payload.ID != nil && *payload.ID != ""
	slugProvided := payload.Slug != nil && *payload.Slug != ""
	if !idProvided && !slugProvided {
		return nil, oops.E(oops.CodeBadRequest, nil, "id or slug is required").LogError(ctx, s.logger)
	}
	if idProvided && slugProvided {
		return nil, oops.E(oops.CodeBadRequest, nil, "id and slug are mutually exclusive").LogError(ctx, s.logger)
	}

	r := repo.New(s.db)

	var server repo.McpServer
	var err error
	if idProvided {
		serverID, parseErr := uuid.Parse(*payload.ID)
		if parseErr != nil {
			return nil, oops.E(oops.CodeBadRequest, parseErr, "invalid mcp server id").LogError(ctx, s.logger)
		}
		server, err = r.GetMCPServerByIDAndProjectID(ctx, repo.GetMCPServerByIDAndProjectIDParams{
			ID:        serverID,
			ProjectID: *authCtx.ProjectID,
		})
	} else {
		server, err = r.GetMCPServerBySlug(ctx, repo.GetMCPServerBySlugParams{
			Slug:      conv.ToPGText(*payload.Slug),
			ProjectID: *authCtx.ProjectID,
		})
	}
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, oops.E(oops.CodeNotFound, err, "mcp server not found").LogError(ctx, s.logger)
		}
		return nil, oops.E(oops.CodeUnexpected, err, "get mcp server").LogError(ctx, s.logger)
	}

	return mv.BuildMcpServerView(server), nil
}

func (s *Service) ListToolFilters(ctx context.Context, payload *gen.ListToolFiltersPayload) (*types.ListToolFiltersResult, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	if err := s.authz.Require(ctx, authz.MCPCheck(authz.ScopeMCPRead, authCtx.ProjectID.String(), authCtx.ProjectID.String())); err != nil {
		return nil, err
	}

	idProvided := payload.ID != nil && *payload.ID != ""
	slugProvided := payload.Slug != nil && *payload.Slug != ""
	if !idProvided && !slugProvided {
		return nil, oops.E(oops.CodeBadRequest, nil, "id or slug is required").LogError(ctx, s.logger)
	}
	if idProvided && slugProvided {
		return nil, oops.E(oops.CodeBadRequest, nil, "id and slug are mutually exclusive").LogError(ctx, s.logger)
	}

	r := repo.New(s.db)

	var server repo.McpServer
	var err error
	if idProvided {
		serverID, parseErr := uuid.Parse(*payload.ID)
		if parseErr != nil {
			return nil, oops.E(oops.CodeBadRequest, parseErr, "invalid mcp server id").LogError(ctx, s.logger)
		}
		server, err = r.GetMCPServerByIDAndProjectID(ctx, repo.GetMCPServerByIDAndProjectIDParams{
			ID:        serverID,
			ProjectID: *authCtx.ProjectID,
		})
	} else {
		server, err = r.GetMCPServerBySlug(ctx, repo.GetMCPServerBySlugParams{
			Slug:      conv.ToPGText(*payload.Slug),
			ProjectID: *authCtx.ProjectID,
		})
	}
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, oops.E(oops.CodeNotFound, err, "mcp server not found").LogError(ctx, s.logger)
		}
		return nil, oops.E(oops.CodeUnexpected, err, "get mcp server").LogError(ctx, s.logger)
	}

	// Only toolset-backed servers expose a tool list to filter. Remote-backed
	// servers have no toolset tools here (their Tools tab is separate, future
	// work), so report filtering disabled with no scopes.
	if !server.ToolsetID.Valid {
		return toolfilter.BuildView(nil, nil, nil), nil
	}

	toolset, err := toolsetsrepo.New(s.db).GetToolsetByIDAndProject(ctx, toolsetsrepo.GetToolsetByIDAndProjectParams{
		ID:        server.ToolsetID.UUID,
		ProjectID: *authCtx.ProjectID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, oops.E(oops.CodeNotFound, err, "mcp server backing toolset not found").LogError(ctx, s.logger)
		}
		return nil, oops.E(oops.CodeUnexpected, err, "get mcp server backing toolset").LogError(ctx, s.logger)
	}

	// Resolution chain: the mcp_servers value takes precedence over the
	// toolset's own column.
	var mcpServerGroupID *uuid.UUID
	if server.ToolVariationsGroupID.Valid {
		mcpServerGroupID = &server.ToolVariationsGroupID.UUID
	}
	var toolsetGroupID *uuid.UUID
	if toolset.ToolVariationsGroupID.Valid {
		toolsetGroupID = &toolset.ToolVariationsGroupID.UUID
	}
	resolved := toolfilter.ResolveGroupID(mcpServerGroupID, toolsetGroupID)
	if resolved == nil {
		return toolfilter.BuildView(nil, nil, nil), nil
	}

	// DescribeToolset applies the resolved group's variation overrides to the
	// tools, so the derived effective tags match the runtime ?tags= result.
	described, err := mv.DescribeToolset(ctx, s.logger, s.db, mv.ProjectID(*authCtx.ProjectID), mv.ToolsetSlug(toolset.Slug), nil, resolved)
	if err != nil {
		return nil, err
	}

	groupName, err := mv.ToolVariationsGroupName(ctx, s.logger, s.db, *resolved, *authCtx.ProjectID)
	if err != nil {
		return nil, err
	}

	return toolfilter.BuildView(described.Tools, resolved, groupName), nil
}

func (s *Service) ListMcpServers(ctx context.Context, payload *gen.ListMcpServersPayload) (*gen.ListMcpServersResult, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	if err := s.authz.Require(ctx, authz.MCPCheck(authz.ScopeMCPRead, authCtx.ProjectID.String(), authCtx.ProjectID.String())); err != nil {
		return nil, err
	}

	logger := s.logger.With(attr.SlogProjectID(authCtx.ProjectID.String()))

	remoteMcpServerID, err := conv.PtrToNullUUID(payload.RemoteMcpServerID)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid remote_mcp_server_id").LogError(ctx, logger)
	}
	tunneledMcpServerID, err := conv.PtrToNullUUID(payload.TunneledMcpServerID)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid tunneled_mcp_server_id").LogWarn(ctx, logger)
	}
	toolsetID, err := conv.PtrToNullUUID(payload.ToolsetID)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid toolset_id").LogError(ctx, logger)
	}
	if backendFilterCount(remoteMcpServerID, tunneledMcpServerID, toolsetID) > 1 {
		return nil, oops.E(oops.CodeInvalid, nil, "at most one of remote_mcp_server_id, tunneled_mcp_server_id, or toolset_id may be provided").LogWarn(ctx, logger)
	}

	servers, err := repo.New(s.db).ListMCPServersByProjectID(ctx, repo.ListMCPServersByProjectIDParams{
		ProjectID:           *authCtx.ProjectID,
		RemoteMcpServerID:   remoteMcpServerID,
		TunneledMcpServerID: tunneledMcpServerID,
		ToolsetID:           toolsetID,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list mcp servers").LogError(ctx, logger)
	}

	return &gen.ListMcpServersResult{McpServers: mv.BuildMcpServerListView(servers)}, nil
}

func (s *Service) ListMcpServersForOrg(ctx context.Context, payload *gen.ListMcpServersForOrgPayload) (*gen.ListMcpServersResult, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeOrgRead, ResourceKind: "", ResourceID: authCtx.ActiveOrganizationID, Dimensions: nil}); err != nil {
		return nil, err
	}

	logger := s.logger.With(attr.SlogOrganizationID(authCtx.ActiveOrganizationID))

	servers, err := repo.New(s.db).ListMCPServersByOrganizationID(ctx, authCtx.ActiveOrganizationID)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list mcp servers for organization").LogError(ctx, logger)
	}

	return &gen.ListMcpServersResult{McpServers: mv.BuildMcpServerListView(servers)}, nil
}

func (s *Service) UpdateMcpServer(ctx context.Context, payload *gen.UpdateMcpServerPayload) (*types.McpServer, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	if err := s.authz.Require(ctx, authz.MCPCheck(authz.ScopeMCPWrite, authCtx.ProjectID.String(), authCtx.ProjectID.String())); err != nil {
		return nil, err
	}

	logger := s.logger.With(attr.SlogProjectID(authCtx.ProjectID.String()))

	serverID, err := uuid.Parse(payload.ID)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid mcp server id").LogError(ctx, logger)
	}

	ids, err := parseServerIDs(
		payload.EnvironmentID,
		payload.RemoteMcpServerID,
		payload.TunneledMcpServerID,
		payload.ToolsetID,
		payload.ToolVariationsGroupID,
	)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid mcp server").LogError(ctx, logger)
	}
	if err := validateServerBackendExclusivity(ids.RemoteMcpServerID, ids.TunneledMcpServerID, ids.ToolsetID); err != nil {
		return nil, oops.E(oops.CodeInvalid, err, "invalid mcp server").LogError(ctx, logger)
	}
	if err := validateTunneledMCPVisibility(ids, payload.Visibility); err != nil {
		return nil, oops.E(oops.CodeInvalid, err, "invalid mcp server").LogWarn(ctx, logger)
	}

	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "begin transaction").LogError(ctx, logger)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })

	txRepo := repo.New(dbtx)

	existing, err := txRepo.GetMCPServerByIDAndProjectID(ctx, repo.GetMCPServerByIDAndProjectIDParams{
		ID:        serverID,
		ProjectID: *authCtx.ProjectID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, oops.E(oops.CodeNotFound, err, "mcp server not found").LogError(ctx, logger)
		}
		return nil, oops.E(oops.CodeUnexpected, err, "get mcp server").LogError(ctx, logger)
	}

	beforeView := mv.BuildMcpServerView(existing)

	if err := verifyServerReferenceOwnership(ctx, dbtx, *authCtx.ProjectID, ids); err != nil {
		return nil, oops.E(oops.CodeInvalid, err, "invalid mcp server").LogError(ctx, logger)
	}

	// Resolve name: nil = leave existing; non-nil = trim and require non-empty.
	name := existing.Name
	if payload.Name != nil {
		trimmed := strings.TrimSpace(*payload.Name)
		if trimmed == "" {
			return nil, oops.E(oops.CodeBadRequest, nil, "name must be non-empty").LogError(ctx, logger)
		}
		name = conv.ToPGText(trimmed)
	}

	// Always recompute slug from the post-update name so it tracks the name
	// even when the name didn't change (idempotent).
	slug, err := computeServerSlug(conv.FromPGTextOrEmpty[string](name), serverID)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "compute server slug").LogError(ctx, logger)
	}

	updated, err := txRepo.UpdateMCPServer(ctx, repo.UpdateMCPServerParams{
		Name:          name,
		Slug:          conv.ToPGText(slug),
		EnvironmentID: ids.EnvironmentID,
		// Always NULL: the query COALESCEs to the stored issuer, which is
		// attached at create time for the server's lifetime.
		UserSessionIssuerID:   uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		RemoteMcpServerID:     ids.RemoteMcpServerID,
		TunneledMcpServerID:   ids.TunneledMcpServerID,
		ToolsetID:             ids.ToolsetID,
		ToolVariationsGroupID: ids.ToolVariationsGroupID,
		Visibility:            string(payload.Visibility),
		ID:                    serverID,
		ProjectID:             *authCtx.ProjectID,
	})
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == pgerrcode.UniqueViolation {
			return nil, oops.E(oops.CodeConflict, err, "mcp server slug already in use").LogError(ctx, logger)
		}
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, oops.E(oops.CodeNotFound, err, "mcp server not found").LogError(ctx, logger)
		}
		return nil, oops.E(oops.CodeUnexpected, err, "update mcp server").LogError(ctx, logger)
	}

	afterView := mv.BuildMcpServerView(updated)

	if err := s.audit.LogMcpServerUpdate(ctx, dbtx, audit.LogMcpServerUpdateEvent{
		OrganizationID:          authCtx.ActiveOrganizationID,
		ProjectID:               *authCtx.ProjectID,
		Actor:                   urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID),
		ActorDisplayName:        authCtx.Email,
		ActorSlug:               nil,
		McpServerURN:            urn.NewMcpServer(updated.ID),
		McpServerName:           conv.FromPGTextOrEmpty[string](updated.Name),
		McpServerSlug:           conv.FromPGTextOrEmpty[string](updated.Slug),
		McpServerSnapshotBefore: beforeView,
		McpServerSnapshotAfter:  afterView,
	}); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "log mcp server update").LogError(ctx, logger)
	}

	// A server that was just enabled is publishable if it already has an
	// endpoint — attach it to the Default plugin so it reaches the
	// auto-published marketplace. This is the counterpart to
	// mcpendpoints.CreateMcpEndpoint's attach-on-first-endpoint, which skips
	// servers still disabled at endpoint-creation time (the dashboard's
	// remote MCP flow pre-stages an endpoint while the server is parked
	// disabled for auth configuration, so enabling is what completes
	// publishability there).
	pluginCreated := false
	if existing.Visibility == VisibilityDisabled && updated.Visibility != VisibilityDisabled {
		pluginCreated, err = s.attachToDefaultPlugin(ctx, dbtx, authCtx, updated)
		if err != nil {
			return nil, err
		}
	}

	if err := dbtx.Commit(ctx); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "commit transaction").LogError(ctx, logger)
	}

	s.triggerInitialPublishIfNeeded(ctx, authCtx, pluginCreated)

	return afterView, nil
}

// attachToDefaultPlugin adds a just-enabled mcp_server to the project's
// Default plugin so it's included in the auto-published marketplace without
// a human visiting the Plugins page. Mirrors AddPluginServer's own
// publishability check: a server with no live endpoint isn't publishable and
// is skipped (mcpendpoints.CreateMcpEndpoint attaches it later when it gets
// its first endpoint while enabled). Already-attached servers are an
// idempotent no-op. Returns pluginCreated=true if this call lazily created
// the Default plugin (project predates the feature) — callers should enqueue
// an initial publish for it, but only after their own transaction commits,
// since this runs pre-commit and the DB writes could still roll back.
func (s *Service) attachToDefaultPlugin(ctx context.Context, dbtx pgx.Tx, authCtx *contextvalues.AuthContext, server repo.McpServer) (bool, error) {
	endpoints, err := mcpendpointsrepo.New(dbtx).ListMCPEndpointsByMCPServerID(ctx, mcpendpointsrepo.ListMCPEndpointsByMCPServerIDParams{
		ProjectID:   *authCtx.ProjectID,
		McpServerID: server.ID,
	})
	if err != nil {
		return false, oops.E(oops.CodeUnexpected, err, "list mcp server endpoints").LogError(ctx, s.logger)
	}
	if len(endpoints) == 0 {
		return false, nil
	}

	pluginCreated, err := plugins.AttachToDefaultPluginAudited(ctx, dbtx, s.audit, authCtx, plugins.AttachToDefaultPluginParams{
		OrganizationID: authCtx.ActiveOrganizationID,
		ProjectID:      *authCtx.ProjectID,
		ToolsetID:      uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		McpServerID:    uuid.NullUUID{UUID: server.ID, Valid: true},
		DisplayName:    ServerDisplayName(server),
	})
	if err != nil {
		return false, oops.E(oops.CodeUnexpected, err, "attach mcp server to default plugin").LogError(ctx, s.logger)
	}

	return pluginCreated, nil
}

// triggerInitialPublishIfNeeded enqueues the first-time GitHub marketplace
// publish for a project whose Default plugin was just lazily created. Must
// only be called after the triggering transaction has committed — enqueuing
// before commit risks Temporal running against state that a later failure
// in the same transaction rolls back. Best-effort: a non-cancelable ctx
// since the request returning shouldn't drop the enqueue.
func (s *Service) triggerInitialPublishIfNeeded(ctx context.Context, authCtx *contextvalues.AuthContext, pluginCreated bool) {
	if !pluginCreated || !s.pluginsGitHubEnabled {
		return
	}

	enqueueCtx := context.WithoutCancel(ctx)
	if _, err := background.ExecutePluginInitialPublishWorkflow(enqueueCtx, s.temporalEnv, plugins.PublishProjectInput{
		ProjectID:       *authCtx.ProjectID,
		CreatedByUserID: authCtx.UserID,
		CommitMessage:   "Initial marketplace publish",
		SkipIfUnchanged: false,
	}); err != nil {
		s.logger.WarnContext(ctx, "failed to enqueue initial plugin publish", attr.SlogError(err))
	}
}

// ServerDisplayName derives a default plugin-server display name from an
// mcp_server, preferring its name, then slug, then id.
func ServerDisplayName(server repo.McpServer) string {
	if name := conv.FromPGText[string](server.Name); name != nil && *name != "" {
		return *name
	}
	if slug := conv.FromPGText[string](server.Slug); slug != nil && *slug != "" {
		return *slug
	}
	return server.ID.String()
}

func (s *Service) DeleteMcpServer(ctx context.Context, payload *gen.DeleteMcpServerPayload) error {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return oops.C(oops.CodeUnauthorized)
	}

	if err := s.authz.Require(ctx, authz.MCPCheck(authz.ScopeMCPWrite, authCtx.ProjectID.String(), authCtx.ProjectID.String())); err != nil {
		return err
	}

	logger := s.logger.With(attr.SlogProjectID(authCtx.ProjectID.String()))

	serverID, err := uuid.Parse(payload.ID)
	if err != nil {
		return oops.E(oops.CodeBadRequest, err, "invalid mcp server id").LogError(ctx, logger)
	}

	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "begin transaction").LogError(ctx, logger)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })

	txRepo := repo.New(dbtx)

	deleted, err := txRepo.DeleteMCPServer(ctx, repo.DeleteMCPServerParams{
		ID:        serverID,
		ProjectID: *authCtx.ProjectID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return oops.E(oops.CodeNotFound, err, "mcp server not found").LogError(ctx, logger)
		}
		return oops.E(oops.CodeUnexpected, err, "delete mcp server").LogError(ctx, logger)
	}

	// The mcp_endpoints.mcp_server_id FK has ON DELETE CASCADE, but that only
	// fires for hard deletes. Soft-delete endpoints explicitly so callers don't
	// resolve to a tombstoned mcp server after this commits.
	deletedEndpoints, err := mcpendpointsrepo.New(dbtx).SoftDeleteMCPEndpointsByMCPServerID(ctx, mcpendpointsrepo.SoftDeleteMCPEndpointsByMCPServerIDParams{
		McpServerID: deleted.ID,
		ProjectID:   *authCtx.ProjectID,
	})
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "delete child mcp endpoints").LogError(ctx, logger)
	}

	for _, endpoint := range deletedEndpoints {
		if err := s.audit.LogMcpEndpointDelete(ctx, dbtx, audit.LogMcpEndpointDeleteEvent{
			OrganizationID:   authCtx.ActiveOrganizationID,
			ProjectID:        *authCtx.ProjectID,
			Actor:            urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID),
			ActorDisplayName: authCtx.Email,
			ActorSlug:        nil,
			McpEndpointURN:   urn.NewMcpEndpoint(endpoint.ID),
			Slug:             endpoint.Slug,
		}); err != nil {
			return oops.E(oops.CodeUnexpected, err, "log mcp endpoint deletion").LogError(ctx, logger)
		}
	}

	// Detach the server from any plugins (Default or manually curated). The
	// (plugin_id, display_name) unique index only excludes soft-deleted rows,
	// so a live attachment left behind would keep holding the display name and
	// block a later same-named server from ever attaching — i.e. from being
	// enabled at all via UpdateMcpServer's attach-on-enable path.
	detachedPluginServers, err := pluginsrepo.New(dbtx).SoftDeletePluginServersByMCPServerID(ctx, pluginsrepo.SoftDeletePluginServersByMCPServerIDParams{
		ProjectID:   *authCtx.ProjectID,
		McpServerID: uuid.NullUUID{UUID: deleted.ID, Valid: true},
	})
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "detach mcp server from plugins").LogError(ctx, logger)
	}

	deletedServerURN := urn.NewMcpServer(deleted.ID)
	for _, pluginServer := range detachedPluginServers {
		if err := s.audit.LogPluginServerRemove(ctx, dbtx, audit.LogPluginServerRemoveEvent{
			OrganizationID:   authCtx.ActiveOrganizationID,
			ProjectID:        *authCtx.ProjectID,
			Actor:            urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID),
			ActorDisplayName: authCtx.Email,
			ActorSlug:        nil,
			PluginID:         pluginServer.PluginID,
			PluginName:       pluginServer.PluginName,
			PluginSlug:       pluginServer.PluginSlug,
			ServerID:         pluginServer.ID,
			ToolsetURN:       nil,
			McpServerURN:     &deletedServerURN,
		}); err != nil {
			return oops.E(oops.CodeUnexpected, err, "log mcp server plugin detachment").LogError(ctx, logger)
		}
	}

	// Remote- and tunneled-backed servers own the issuer minted with them.
	// An issuer may also be referenced by another server or toolset, so only
	// cascade once this deletion leaves it without an active owner.
	if deleted.UserSessionIssuerID.Valid {
		userSessionsRepo := usersessionsrepo.New(dbtx)
		hasActiveOwner, err := userSessionsRepo.UserSessionIssuerHasActiveOwner(ctx, usersessionsrepo.UserSessionIssuerHasActiveOwnerParams{
			ProjectID:           *authCtx.ProjectID,
			UserSessionIssuerID: deleted.UserSessionIssuerID.UUID,
		})
		if err != nil {
			return oops.E(oops.CodeUnexpected, err, "check user session issuer ownership").LogError(ctx, logger)
		}

		if !hasActiveOwner {
			deletedIssuer, err := userSessionsRepo.DeleteUserSessionIssuer(ctx, usersessionsrepo.DeleteUserSessionIssuerParams{
				ID:        deleted.UserSessionIssuerID.UUID,
				ProjectID: *authCtx.ProjectID,
			})
			switch {
			case errors.Is(err, pgx.ErrNoRows):
				// A missing issuer must not block server deletion.
			case err != nil:
				return oops.E(oops.CodeUnexpected, err, "delete mcp server issuer").LogError(ctx, logger)
			default:
				if err := userSessionsRepo.DeleteRemoteSessionClientAttachmentsForUserSessionIssuer(ctx, usersessionsrepo.DeleteRemoteSessionClientAttachmentsForUserSessionIssuerParams{
					UserSessionIssuerID: deletedIssuer.ID,
					ProjectID:           *authCtx.ProjectID,
				}); err != nil {
					return oops.E(oops.CodeUnexpected, err, "delete mcp server issuer client attachments").LogError(ctx, logger)
				}

				if _, err := userSessionsRepo.SoftDeleteUserSessionsByIssuerID(ctx, deletedIssuer.ID); err != nil {
					return oops.E(oops.CodeUnexpected, err, "delete mcp server issuer sessions").LogError(ctx, logger)
				}

				if _, err := userSessionsRepo.SoftDeleteUserSessionConsentsByIssuerID(ctx, deletedIssuer.ID); err != nil {
					return oops.E(oops.CodeUnexpected, err, "delete mcp server issuer consents").LogError(ctx, logger)
				}

				if err := s.audit.LogUserSessionIssuerDelete(ctx, dbtx, audit.LogUserSessionIssuerDeleteEvent{
					OrganizationID:       authCtx.ActiveOrganizationID,
					ProjectID:            *authCtx.ProjectID,
					Actor:                urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID),
					ActorDisplayName:     authCtx.Email,
					ActorSlug:            nil,
					UserSessionIssuerURN: urn.NewUserSessionIssuer(deletedIssuer.ID),
					Slug:                 deletedIssuer.Slug,
				}); err != nil {
					return oops.E(oops.CodeUnexpected, err, "log mcp server issuer deletion").LogError(ctx, logger)
				}
			}
		}
	}

	if err := s.audit.LogMcpServerDelete(ctx, dbtx, audit.LogMcpServerDeleteEvent{
		OrganizationID:   authCtx.ActiveOrganizationID,
		ProjectID:        *authCtx.ProjectID,
		Actor:            urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID),
		ActorDisplayName: authCtx.Email,
		ActorSlug:        nil,
		McpServerURN:     urn.NewMcpServer(deleted.ID),
		McpServerName:    conv.FromPGTextOrEmpty[string](deleted.Name),
		McpServerSlug:    conv.FromPGTextOrEmpty[string](deleted.Slug),
	}); err != nil {
		return oops.E(oops.CodeUnexpected, err, "log mcp server deletion").LogError(ctx, logger)
	}

	if err := dbtx.Commit(ctx); err != nil {
		return oops.E(oops.CodeUnexpected, err, "commit transaction").LogError(ctx, logger)
	}

	return nil
}

// serverIDs bundles the optional UUID references on the mcp_servers
// create/update payloads so they can be passed around without a long
// positional argument list.
type serverIDs struct {
	EnvironmentID uuid.NullUUID
	// Set by mintServerUserSessionIssuer during create, never parsed from a payload.
	UserSessionIssuerID   uuid.NullUUID
	RemoteMcpServerID     uuid.NullUUID
	TunneledMcpServerID   uuid.NullUUID
	ToolsetID             uuid.NullUUID
	ToolVariationsGroupID uuid.NullUUID
}

// parseServerIDs parses the optional UUID payload fields into a
// serverIDs struct. Any malformed UUID surfaces with a field-specific error.
func parseServerIDs(
	environmentIDStr *string,
	remoteMcpServerIDStr *string,
	tunneledMcpServerIDStr *string,
	toolsetIDStr *string,
	toolVariationsGroupIDStr *string,
) (serverIDs, error) {
	var (
		ids serverIDs
		err error
	)

	if ids.EnvironmentID, err = conv.PtrToNullUUID(environmentIDStr); err != nil {
		return serverIDs{}, fmt.Errorf("invalid environment_id: %w", err)
	}
	if ids.RemoteMcpServerID, err = conv.PtrToNullUUID(remoteMcpServerIDStr); err != nil {
		return serverIDs{}, fmt.Errorf("invalid remote_mcp_server_id: %w", err)
	}
	if ids.TunneledMcpServerID, err = conv.PtrToNullUUID(tunneledMcpServerIDStr); err != nil {
		return serverIDs{}, fmt.Errorf("invalid tunneled_mcp_server_id: %w", err)
	}
	if ids.ToolsetID, err = conv.PtrToNullUUID(toolsetIDStr); err != nil {
		return serverIDs{}, fmt.Errorf("invalid toolset_id: %w", err)
	}
	if ids.ToolVariationsGroupID, err = conv.PtrToNullUUID(toolVariationsGroupIDStr); err != nil {
		return serverIDs{}, fmt.Errorf("invalid tool_variations_group_id: %w", err)
	}

	return ids, nil
}

func validateServerBackendExclusivity(remoteMcpServerID, tunneledMcpServerID, toolsetID uuid.NullUUID) error {
	if backendFilterCount(remoteMcpServerID, tunneledMcpServerID, toolsetID) != 1 {
		return fmt.Errorf("exactly one of remote_mcp_server_id, tunneled_mcp_server_id, or toolset_id must be provided")
	}
	return nil
}

func validateTunneledMCPVisibility(ids serverIDs, visibility types.McpServerVisibility) error {
	if ids.TunneledMcpServerID.Valid && string(visibility) == VisibilityPublic {
		return fmt.Errorf("tunneled MCP servers cannot be public")
	}
	return nil
}

func backendFilterCount(ids ...uuid.NullUUID) int {
	count := 0
	for _, id := range ids {
		if id.Valid {
			count++
		}
	}
	return count
}

// verifyServerReferenceOwnership checks that every non-null referenced
// resource belongs to the caller's project. The raw FK constraints only
// enforce existence, not tenancy, so this closes a cross-project leak.
//
// Each check delegates to the owning package's project-scoped Get*ByID query
// and treats sql.ErrNoRows as "not in this project", matching the pattern used
// elsewhere in the codebase (e.g. toolsets -> environments).
func verifyServerReferenceOwnership(
	ctx context.Context,
	dbtx pgx.Tx,
	projectID uuid.UUID,
	ids serverIDs,
) error {
	if ids.EnvironmentID.Valid {
		if _, err := environmentsrepo.New(dbtx).GetEnvironmentByID(ctx, environmentsrepo.GetEnvironmentByIDParams{
			ID:        ids.EnvironmentID.UUID,
			ProjectID: projectID,
		}); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return fmt.Errorf("environment_id does not reference a resource in this project")
			}
			return fmt.Errorf("check environment ownership: %w", err)
		}
	}

	if ids.RemoteMcpServerID.Valid {
		if _, err := remotemcprepo.New(dbtx).GetServerByID(ctx, remotemcprepo.GetServerByIDParams{
			ID:        ids.RemoteMcpServerID.UUID,
			ProjectID: projectID,
		}); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return fmt.Errorf("remote_mcp_server_id does not reference a resource in this project")
			}
			return fmt.Errorf("check remote mcp server ownership: %w", err)
		}
	}

	if ids.TunneledMcpServerID.Valid {
		if _, err := tunneledmcprepo.New(dbtx).GetServerByID(ctx, tunneledmcprepo.GetServerByIDParams{
			ID:        ids.TunneledMcpServerID.UUID,
			ProjectID: projectID,
		}); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return fmt.Errorf("tunneled_mcp_server_id does not reference a resource in this project")
			}
			return fmt.Errorf("check tunneled mcp server ownership: %w", err)
		}
	}

	if ids.ToolsetID.Valid {
		if _, err := toolsetsrepo.New(dbtx).GetToolsetByIDAndProject(ctx, toolsetsrepo.GetToolsetByIDAndProjectParams{
			ID:        ids.ToolsetID.UUID,
			ProjectID: projectID,
		}); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return fmt.Errorf("toolset_id does not reference a resource in this project")
			}
			return fmt.Errorf("check toolset ownership: %w", err)
		}
	}

	if ids.ToolVariationsGroupID.Valid {
		if _, err := variationsrepo.New(dbtx).GetToolVariationsGroupByID(ctx, variationsrepo.GetToolVariationsGroupByIDParams{
			ID:        ids.ToolVariationsGroupID.UUID,
			ProjectID: projectID,
		}); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return fmt.Errorf("tool_variations_group_id does not reference a resource in this project")
			}
			return fmt.Errorf("check tool variations group ownership: %w", err)
		}
	}

	return nil
}
