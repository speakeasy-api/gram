package mcpservers

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"slices"
	"strings"
	"time"

	"golang.org/x/net/publicsuffix"

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
	"github.com/speakeasy-api/gram/server/internal/access"
	"github.com/speakeasy-api/gram/server/internal/assets"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/auth"
	"github.com/speakeasy-api/gram/server/internal/auth/sessions"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/background"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	customdomainsrepo "github.com/speakeasy-api/gram/server/internal/customdomains/repo"
	environmentsrepo "github.com/speakeasy-api/gram/server/internal/environments/repo"
	"github.com/speakeasy-api/gram/server/internal/hostedmcp"
	"github.com/speakeasy-api/gram/server/internal/management/readmodel"
	"github.com/speakeasy-api/gram/server/internal/mcp/toolfilter"
	mcpendpointsrepo "github.com/speakeasy-api/gram/server/internal/mcpendpoints/repo"
	mcpmetadatarepo "github.com/speakeasy-api/gram/server/internal/mcpmetadata/repo"
	"github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
	metamcprepo "github.com/speakeasy-api/gram/server/internal/metamcp/repo"
	"github.com/speakeasy-api/gram/server/internal/middleware"
	"github.com/speakeasy-api/gram/server/internal/mv"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/plugins"
	remotemcprepo "github.com/speakeasy-api/gram/server/internal/remotemcp/repo"
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
	tenv "github.com/speakeasy-api/gram/server/internal/temporal"
	toolsetsrepo "github.com/speakeasy-api/gram/server/internal/toolsets/repo"
	tunneledmcprepo "github.com/speakeasy-api/gram/server/internal/tunneledmcp/repo"
	unproxiedmcprepo "github.com/speakeasy-api/gram/server/internal/unproxiedmcp/repo"
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
	dispositionCache     *ToolDispositionCache
	pluginsGitHubEnabled bool
	assets               *assets.Service
	// revoker handles grants orphaned by DeleteMcpServer's issuer cascade.
	revoker *remotesessions.UpstreamRevoker
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
	dispositionCache *ToolDispositionCache,
	pluginsGitHubEnabled bool,
	assetsService *assets.Service,
	revoker *remotesessions.UpstreamRevoker,
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
		dispositionCache:     dispositionCache,
		pluginsGitHubEnabled: pluginsGitHubEnabled,
		assets:               assetsService,
		revoker:              revoker,
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

	ids, err := parseServerIDs(serverIDStrings{
		EnvironmentID:         payload.EnvironmentID,
		RemoteMcpServerID:     payload.RemoteMcpServerID,
		TunneledMcpServerID:   payload.TunneledMcpServerID,
		ToolsetID:             payload.ToolsetID,
		UnproxiedMcpServerID:  payload.UnproxiedMcpServerID,
		ToolVariationsGroupID: payload.ToolVariationsGroupID,
	})
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid mcp server").LogError(ctx, logger)
	}
	if err := validateServerBackendExclusivity(ids); err != nil {
		return nil, oops.E(oops.CodeInvalid, err, "invalid mcp server").LogError(ctx, logger)
	}
	if err := requireStaffForUnproxiedBackend(ctx, authCtx, ids.UnproxiedMcpServerID, logger); err != nil {
		return nil, err
	}

	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "begin transaction").LogError(ctx, logger)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })

	// A new wrapper adopts what its toolset already holds.
	issuerID := uuid.NullUUID{UUID: uuid.Nil, Valid: false}
	if ids.ToolsetID.Valid {
		backing, err := toolsetsrepo.New(dbtx).LockToolsetByID(ctx, toolsetsrepo.LockToolsetByIDParams{ID: ids.ToolsetID.UUID, ProjectID: *authCtx.ProjectID})
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return nil, oops.E(oops.CodeInvalid, err, "toolset_id does not reference a resource in this project").LogError(ctx, logger)
			}
			return nil, oops.E(oops.CodeUnexpected, err, "lock backing toolset").LogError(ctx, logger)
		}
		issuerID = backing.UserSessionIssuerID
		if !ids.ToolVariationsGroupID.Valid {
			ids.ToolVariationsGroupID = backing.ToolVariationsGroupID
		}
	}

	if err := verifyServerReferenceOwnership(ctx, dbtx, *authCtx.ProjectID, ids); err != nil {
		return nil, oops.E(oops.CodeInvalid, err, "invalid mcp server").LogError(ctx, logger)
	}

	if err := verifyTunneledPublicConsent(ctx, dbtx, *authCtx.ProjectID, ids.TunneledMcpServerID, string(payload.Visibility)); err != nil {
		return nil, oops.E(oops.CodeInvalid, err, "invalid mcp server").LogWarn(ctx, logger)
	}

	server, err := CreateMCPServerInTransaction(ctx, dbtx, s.audit, MCPServerTransactionInput{
		OrganizationID:        authCtx.ActiveOrganizationID,
		ProjectID:             *authCtx.ProjectID,
		ActorUserID:           authCtx.UserID,
		ActorEmail:            authCtx.Email,
		Name:                  name,
		Visibility:            string(payload.Visibility),
		EnvironmentID:         ids.EnvironmentID,
		UserSessionIssuerID:   issuerID,
		RemoteMCPServerID:     ids.RemoteMcpServerID,
		TunneledMCPServerID:   ids.TunneledMcpServerID,
		ToolsetID:             ids.ToolsetID,
		UnproxiedMCPServerID:  ids.UnproxiedMcpServerID,
		ToolVariationsGroupID: ids.ToolVariationsGroupID,
	})
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == pgerrcode.UniqueViolation {
			return nil, oops.E(oops.CodeConflict, err, "%s", UniqueViolationMessage(pgErr)).LogError(ctx, logger)
		}
		return nil, oops.E(oops.CodeUnexpected, err, "create mcp server").LogError(ctx, logger)
	}
	if server.ToolsetID.Valid {
		if err := s.toolsetMirror(authCtx).SyncToolsetFromWrapper(ctx, dbtx, server); err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "mirror mcp server onto toolset").LogError(ctx, logger)
		}
		if issuerID.Valid {
			if err := remotesessions.ResyncMCPServerRemoteSessionIssuers(ctx, dbtx, authCtx.ActiveOrganizationID, *authCtx.ProjectID, []uuid.UUID{issuerID.UUID}); err != nil {
				return nil, oops.E(oops.CodeUnexpected, err, "resync mcp server remote session issuer").LogError(ctx, logger)
			}
		}
	}

	if err := dbtx.Commit(ctx); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "commit transaction").LogError(ctx, logger)
	}

	s.scheduleDefaultServerIcon(ctx, *authCtx.ProjectID, server.ID, ids)

	return mv.BuildMcpServerView(server), nil
}

// DefaultServerIconSetter schedules best-effort post-create icon discovery for
// remote-backed MCP servers.
type DefaultServerIconSetter interface {
	ScheduleDefaultRemoteServerIcon(ctx context.Context, projectID, mcpServerID, remoteMCPServerID uuid.UUID)
}

// ScheduleDefaultRemoteServerIcon gives remote-MCP provisioning the same
// post-commit icon behavior as CreateMcpServer without exposing its transaction
// command or requiring the remote workflow to duplicate favicon logic.
func (s *Service) ScheduleDefaultRemoteServerIcon(ctx context.Context, projectID, mcpServerID, remoteMCPServerID uuid.UUID) {
	if s == nil {
		return
	}
	s.scheduleDefaultServerIcon(ctx, projectID, mcpServerID, serverIDs{
		EnvironmentID:         uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		UserSessionIssuerID:   uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		RemoteMcpServerID:     uuid.NullUUID{UUID: remoteMCPServerID, Valid: true},
		TunneledMcpServerID:   uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		ToolsetID:             uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		UnproxiedMcpServerID:  uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		ToolVariationsGroupID: uuid.NullUUID{UUID: uuid.Nil, Valid: false},
	})
}

// scheduleDefaultServerIcon starts best-effort icon discovery after the create
// transaction commits. It uses a detached bounded context so the request can
// complete without waiting for a vendor favicon.
func (s *Service) scheduleDefaultServerIcon(ctx context.Context, projectID, mcpServerID uuid.UUID, ids serverIDs) {
	if !ids.UnproxiedMcpServerID.Valid && !ids.RemoteMcpServerID.Valid {
		return
	}
	bgCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
	logger := s.logger.With(attr.SlogProjectID(projectID.String()))
	go func() {
		defer cancel()
		s.setDefaultServerIcon(bgCtx, logger, projectID, mcpServerID, ids)
	}()
}

// setDefaultServerIcon fetches the vendor's favicon and sets it as the
// newly-created server's icon. Best-effort: any failure is logged and
// swallowed rather than surfaced, since a missing icon is cosmetic and must
// never fail the create call it's attached to.
func (s *Service) setDefaultServerIcon(ctx context.Context, logger *slog.Logger, projectID uuid.UUID, mcpServerID uuid.UUID, ids serverIDs) {
	var sourceURL string
	switch {
	case ids.UnproxiedMcpServerID.Valid:
		source, err := unproxiedmcprepo.New(s.db).GetServerByID(ctx, unproxiedmcprepo.GetServerByIDParams{
			ID:        ids.UnproxiedMcpServerID.UUID,
			ProjectID: projectID,
		})
		if err != nil {
			logger.ErrorContext(ctx, "load unproxied mcp server for default icon", attr.SlogError(err))
			return
		}
		sourceURL = source.Url
	case ids.RemoteMcpServerID.Valid:
		source, err := remotemcprepo.New(s.db).GetServerByID(ctx, remotemcprepo.GetServerByIDParams{
			ID:        ids.RemoteMcpServerID.UUID,
			ProjectID: projectID,
		})
		if err != nil {
			logger.ErrorContext(ctx, "load remote mcp server for default icon", attr.SlogError(err))
			return
		}
		sourceURL = source.Url
	default:
		return
	}

	vendorURL, err := url.Parse(sourceURL)
	if err != nil {
		logger.ErrorContext(ctx, "parse mcp server url for default icon", attr.SlogError(err))
		return
	}

	asset, err := s.assets.FetchImageAssetFromURL(ctx, vendorFaviconURL(vendorURL.Scheme, vendorURL.Host))
	if err != nil {
		// Some vendors only register a favicon against their registrable
		// domain, not the specific subdomain hosting the MCP endpoint (e.g.
		// mcp.figma.com has none, figma.com does) -- retry once against that
		// before giving up.
		if registrable, rErr := publicsuffix.EffectiveTLDPlusOne(vendorURL.Hostname()); rErr == nil && registrable != vendorURL.Host {
			asset, err = s.assets.FetchImageAssetFromURL(ctx, vendorFaviconURL(vendorURL.Scheme, registrable))
		}
	}
	if err != nil {
		logger.WarnContext(ctx, "fetch default favicon for mcp server", attr.SlogError(err))
		return
	}

	assetID, err := uuid.Parse(asset.ID)
	if err != nil {
		logger.ErrorContext(ctx, "parse default favicon asset id", attr.SlogError(err))
		return
	}

	// Writes mcp_metadata directly rather than through the mcpMetadata
	// service (which already imports this package, so importing it back here
	// would cycle) and skips its audit trail accordingly: this is a system
	// default, not a user-initiated edit. Uses the logo-only conditional
	// query rather than the general upsert: this runs on a detached
	// goroutine racing the create response, so a user could already be
	// saving Branding edits (or their own icon) by the time this lands, and
	// a full-record upsert would clobber them. pgx.ErrNoRows here just means
	// the row already had a logo (or a user edit raced ahead) -- not a
	// failure.
	if _, err := mcpmetadatarepo.New(s.db).SetDefaultLogoIfUnset(ctx, mcpmetadatarepo.SetDefaultLogoIfUnsetParams{
		McpServerID: uuid.NullUUID{UUID: mcpServerID, Valid: true},
		ProjectID:   projectID,
		LogoID:      uuid.NullUUID{UUID: assetID, Valid: true},
	}); err != nil && !errors.Is(err, pgx.ErrNoRows) {
		logger.ErrorContext(ctx, "save default favicon for mcp server", attr.SlogError(err))
	}
}

func vendorFaviconURL(scheme, host string) string {
	return fmt.Sprintf(
		"https://t0.gstatic.com/faviconV2?client=SOCIAL&type=FAVICON&fallback_opts=TYPE,SIZE,URL&url=%s&size=128",
		url.QueryEscape(scheme+"://"+host),
	)
}

// UniqueViolationMessage names which mcp_servers uniqueness a write tripped.
func UniqueViolationMessage(pgErr *pgconn.PgError) string {
	if pgErr.ConstraintName == "mcp_servers_toolset_id_key" {
		return "toolset already has an mcp server"
	}
	return "mcp server slug already in use"
}

// grantResourceID is the RBAC resource id for an mcp_servers row: the backing
// toolset id when toolset-backed, else the row id. These are the ids the
// serving path checks and the ids role-grant selectors name, so keying
// management checks on them makes server-scoped grants effective here too.
func grantResourceID(id uuid.UUID, toolsetID uuid.NullUUID) string {
	if toolsetID.Valid {
		return toolsetID.UUID.String()
	}
	return id.String()
}

// requireServerWriteUnlocked rejects a caller lacking mcp:write on the server
// before the mutation transaction takes any FOR UPDATE locks, so an
// unauthorized member cannot contend server-lifecycle locks. It keys on a
// non-locking read; UpdateMcpServer/DeleteMcpServer re-check against the
// locked row, which is authoritative if the backing changes concurrently.
func (s *Service) requireServerWriteUnlocked(ctx context.Context, serverID uuid.UUID, projectID uuid.UUID, logger *slog.Logger) error {
	server, err := repo.New(s.db).GetMCPServerByIDAndProjectID(ctx, repo.GetMCPServerByIDAndProjectIDParams{
		ID:        serverID,
		ProjectID: projectID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return oops.E(oops.CodeNotFound, err, "mcp server not found").LogError(ctx, logger)
		}
		return oops.E(oops.CodeUnexpected, err, "get mcp server").LogError(ctx, logger)
	}

	return s.authz.Require(ctx, authz.MCPCheck(authz.ScopeMCPWrite, grantResourceID(server.ID, server.ToolsetID), projectID.String()))
}

func (s *Service) GetMcpServer(ctx context.Context, payload *gen.GetMcpServerPayload) (*types.McpServer, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return nil, oops.C(oops.CodeUnauthorized)
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

	if err := s.authz.Require(ctx, authz.MCPCheck(authz.ScopeMCPRead, grantResourceID(server.ID, server.ToolsetID), authCtx.ProjectID.String())); err != nil {
		return nil, err
	}

	return mv.BuildMcpServerView(server), nil
}

func (s *Service) ListToolFilters(ctx context.Context, payload *gen.ListToolFiltersPayload) (*types.ListToolFiltersResult, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return nil, oops.C(oops.CodeUnauthorized)
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

	if err := s.authz.Require(ctx, authz.MCPCheck(authz.ScopeMCPRead, grantResourceID(server.ID, server.ToolsetID), authCtx.ProjectID.String())); err != nil {
		return nil, err
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
	unproxiedMcpServerID, err := conv.PtrToNullUUID(payload.UnproxiedMcpServerID)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid unproxied_mcp_server_id").LogError(ctx, logger)
	}
	if backendFilterCount(remoteMcpServerID, tunneledMcpServerID, toolsetID, unproxiedMcpServerID) > 1 {
		return nil, oops.E(oops.CodeInvalid, nil, "at most one of remote_mcp_server_id, tunneled_mcp_server_id, toolset_id, or unproxied_mcp_server_id may be provided").LogWarn(ctx, logger)
	}

	servers, err := readmodel.New(s.db).ListMCPServers(ctx, *authCtx.ProjectID, repo.ListMCPServersByProjectIDParams{
		ProjectID:            uuid.Nil,
		RemoteMcpServerID:    remoteMcpServerID,
		TunneledMcpServerID:  tunneledMcpServerID,
		ToolsetID:            toolsetID,
		UnproxiedMcpServerID: unproxiedMcpServerID,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list mcp servers").LogError(ctx, logger)
	}

	checks := make([]authz.Check, len(servers))
	for i, server := range servers {
		checks[i] = authz.MCPCheck(authz.ScopeMCPRead, grantResourceID(server.ID, server.ToolsetID), authCtx.ProjectID.String())
	}
	allowedIDs, err := s.authz.Filter(ctx, checks)
	if err != nil {
		return nil, err
	}
	allowedSet := make(map[string]struct{}, len(allowedIDs))
	for _, id := range allowedIDs {
		allowedSet[id] = struct{}{}
	}
	allowed := make([]repo.McpServer, 0, len(allowedIDs))
	for _, server := range servers {
		if _, ok := allowedSet[grantResourceID(server.ID, server.ToolsetID)]; ok {
			allowed = append(allowed, server)
		}
	}

	return &gen.ListMcpServersResult{McpServers: mv.BuildMcpServerListView(allowed)}, nil
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

	servers, err := readmodel.New(s.db).ListMCPServersForOrganization(ctx, authCtx.ActiveOrganizationID)
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

	logger := s.logger.With(attr.SlogProjectID(authCtx.ProjectID.String()))

	serverID, err := uuid.Parse(payload.ID)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid mcp server id").LogError(ctx, logger)
	}

	ids, err := parseServerIDs(serverIDStrings{
		EnvironmentID:         payload.EnvironmentID,
		RemoteMcpServerID:     payload.RemoteMcpServerID,
		TunneledMcpServerID:   payload.TunneledMcpServerID,
		ToolsetID:             payload.ToolsetID,
		UnproxiedMcpServerID:  payload.UnproxiedMcpServerID,
		ToolVariationsGroupID: payload.ToolVariationsGroupID,
	})
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid mcp server").LogError(ctx, logger)
	}
	if err := validateServerBackendExclusivity(ids); err != nil {
		return nil, oops.E(oops.CodeInvalid, err, "invalid mcp server").LogError(ctx, logger)
	}

	if err := s.requireServerWriteUnlocked(ctx, serverID, *authCtx.ProjectID, logger); err != nil {
		return nil, err
	}

	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "begin transaction").LogError(ctx, logger)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })

	txRepo := repo.New(dbtx)

	preread, err := lockServerBacking(ctx, dbtx, *authCtx.ProjectID, serverID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, oops.E(oops.CodeNotFound, err, "mcp server not found").LogError(ctx, logger)
		}
		return nil, oops.E(oops.CodeUnexpected, err, "lock backing toolset").LogError(ctx, logger)
	}

	if payload.Visibility == VisibilityDisabled {
		if err := LockMCPServerVisibilityDependencies(ctx, dbtx, authCtx.ActiveOrganizationID, *authCtx.ProjectID, serverID); err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "lock mcp server visibility dependencies").LogError(ctx, logger)
		}
	}
	existing, err := txRepo.LockMCPServerByIDAndProjectID(ctx, repo.LockMCPServerByIDAndProjectIDParams{
		ID:        serverID,
		ProjectID: *authCtx.ProjectID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, oops.E(oops.CodeNotFound, err, "mcp server not found").LogError(ctx, logger)
		}
		return nil, oops.E(oops.CodeUnexpected, err, "get mcp server").LogError(ctx, logger)
	}
	if existing.ToolsetID != preread.ToolsetID {
		return nil, oops.E(oops.CodeConflict, nil, "mcp server changed concurrently; retry the request").LogError(ctx, logger)
	}

	// Authorization keys on the row as it exists, not the backing the payload
	// may switch it to.
	if err := s.authz.Require(ctx, authz.MCPCheck(authz.ScopeMCPWrite, grantResourceID(existing.ID, existing.ToolsetID), authCtx.ProjectID.String())); err != nil {
		return nil, err
	}

	// The mirror binds a toolset and its wrapper for life.
	if ids.ToolsetID != existing.ToolsetID {
		return nil, oops.E(oops.CodeInvalid, nil, "a server's backing toolset cannot be changed; create a new server").LogWarn(ctx, logger)
	}

	// Only gate on staff when the unproxied backend reference is actually
	// changing: a non-staff project member with write access must still be
	// able to manage (rename, re-publish, delete) a server staff already
	// attached to an unproxied backend.
	if ids.UnproxiedMcpServerID != existing.UnproxiedMcpServerID {
		if err := requireStaffForUnproxiedBackend(ctx, authCtx, ids.UnproxiedMcpServerID, logger); err != nil {
			return nil, err
		}
	}

	if err := verifyServerReferenceOwnership(ctx, dbtx, *authCtx.ProjectID, ids); err != nil {
		return nil, oops.E(oops.CodeInvalid, err, "invalid mcp server").LogError(ctx, logger)
	}

	if err := verifyMetaMcpBackendUniqueness(ctx, dbtx, *authCtx.ProjectID, serverID, existing, ids, logger); err != nil {
		return nil, err
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
	slug, err := ComputeServerSlug(conv.FromPGTextOrEmpty[string](name), serverID)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "compute server slug").LogError(ctx, logger)
	}

	// NULL leaves the stored issuer untouched (the update query COALESCEs).
	// A backend switch onto remote/tunneled from a backend that never carried
	// one (toolset, unproxied) needs a fresh mint here, or the insert trips
	// mcp_servers_issuer_required_check.
	issuerID := uuid.NullUUID{UUID: uuid.Nil, Valid: false}
	if (ids.RemoteMcpServerID.Valid || ids.TunneledMcpServerID.Valid) && !existing.UserSessionIssuerID.Valid {
		issuerID, err = MintServerUserSessionIssuer(ctx, dbtx, authCtx.ActiveOrganizationID, *authCtx.ProjectID, slug)
		if err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "mint mcp server issuer").LogError(ctx, logger)
		}
	}

	lifecycleInput := LifecycleUpdateInput{
		OrganizationID:        authCtx.ActiveOrganizationID,
		ProjectID:             *authCtx.ProjectID,
		ActorUserID:           authCtx.UserID,
		ActorEmail:            authCtx.Email,
		ServerID:              serverID,
		Name:                  payload.Name,
		Visibility:            string(payload.Visibility),
		EnvironmentID:         ids.EnvironmentID,
		UserSessionIssuerID:   issuerID,
		RemoteMcpServerID:     ids.RemoteMcpServerID,
		TunneledMcpServerID:   ids.TunneledMcpServerID,
		ToolsetID:             ids.ToolsetID,
		UnproxiedMcpServerID:  ids.UnproxiedMcpServerID,
		ToolVariationsGroupID: ids.ToolVariationsGroupID,
	}
	var clearedRootDomainIDs []uuid.UUID
	var updated repo.McpServer
	if payload.Visibility == VisibilityDisabled {
		visibilityResult, updateErr := UpdateMCPServerVisibilityInTransaction(ctx, dbtx, s.audit, existing, lifecycleInput)
		updated, clearedRootDomainIDs, err = visibilityResult.Server, visibilityResult.ClearedRootDomainIDs, updateErr
	} else {
		updated, err = UpdateMCPServerLifecycleInTransaction(ctx, dbtx, s.audit, existing, lifecycleInput)
	}
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == pgerrcode.UniqueViolation {
			return nil, oops.E(oops.CodeConflict, err, "%s", UniqueViolationMessage(pgErr)).LogError(ctx, logger)
		}
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, oops.E(oops.CodeNotFound, err, "mcp server not found").LogError(ctx, logger)
		}
		return nil, oops.E(oops.CodeUnexpected, err, "update mcp server").LogError(ctx, logger)
	}

	// Check against the post-update row (not the payload): the update query
	// COALESCEs unset backend references, so this is the only state that
	// reliably says whether the server is now tunneled + public.
	if err := verifyTunneledPublicConsent(ctx, dbtx, *authCtx.ProjectID, updated.TunneledMcpServerID, updated.Visibility); err != nil {
		return nil, oops.E(oops.CodeInvalid, err, "invalid mcp server").LogWarn(ctx, logger)
	}

	if updated.ToolsetID.Valid {
		if err := s.toolsetMirror(authCtx).SyncToolsetFromWrapper(ctx, dbtx, updated); err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return nil, oops.E(oops.CodeUnexpected, err, "mirror mcp server onto toolset").LogError(ctx, logger)
		}
	}

	afterView := mv.BuildMcpServerView(updated)

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
	if err := s.reconcileMcpServerCustomDomains(ctx, clearedRootDomainIDs); err != nil {
		return nil, err
	}

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
	// Hosted servers are attached toolset-keyed until AIS-638 re-keys plugins.
	if server.ToolsetID.Valid {
		return false, nil
	}
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

	logger := s.logger.With(attr.SlogProjectID(authCtx.ProjectID.String()))

	serverID, err := uuid.Parse(payload.ID)
	if err != nil {
		return oops.E(oops.CodeBadRequest, err, "invalid mcp server id").LogError(ctx, logger)
	}

	if err := s.requireServerWriteUnlocked(ctx, serverID, *authCtx.ProjectID, logger); err != nil {
		return err
	}

	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "begin transaction").LogError(ctx, logger)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })

	preread, err := lockServerBacking(ctx, dbtx, *authCtx.ProjectID, serverID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return oops.E(oops.CodeNotFound, err, "mcp server not found").LogError(ctx, logger)
		}
		return oops.E(oops.CodeUnexpected, err, "lock backing toolset").LogError(ctx, logger)
	}

	locked, err := LockMCPServerForDelete(ctx, dbtx, *authCtx.ProjectID, serverID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return oops.E(oops.CodeNotFound, err, "mcp server not found").LogError(ctx, logger)
		}
		return oops.E(oops.CodeUnexpected, err, "lock mcp server for deletion").LogError(ctx, logger)
	}
	if locked.Server.ToolsetID != preread.ToolsetID {
		return oops.E(oops.CodeConflict, nil, "mcp server changed concurrently; retry the request").LogError(ctx, logger)
	}

	if err := s.authz.Require(ctx, authz.MCPCheck(authz.ScopeMCPWrite, grantResourceID(locked.Server.ID, locked.Server.ToolsetID), authCtx.ProjectID.String())); err != nil {
		return err
	}

	deleted, err := TombstoneMCPServerInTransaction(ctx, dbtx, s.audit, locked, TombstoneInput{
		OrganizationID: authCtx.ActiveOrganizationID,
		ProjectID:      *authCtx.ProjectID,
		ActorUserID:    authCtx.UserID,
		ActorEmail:     authCtx.Email,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return oops.E(oops.CodeNotFound, err, "mcp server not found").LogError(ctx, logger)
		}
		return oops.E(oops.CodeUnexpected, err, "delete mcp server").LogError(ctx, logger)
	}

	// The toolset outlives its wrapper as a build artifact; only its hosting
	// goes. Its issuer is the toolset's own, never minted with the wrapper, so
	// a hosted wrapper skips the issuer cascade entirely.
	if deleted.ToolsetID.Valid {
		if err := s.toolsetMirror(authCtx).ClearToolsetHosting(ctx, dbtx, *authCtx.ProjectID, deleted.ToolsetID.UUID); err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return oops.E(oops.CodeUnexpected, err, "clear toolset hosting").LogError(ctx, logger)
		}
	}

	// Remote- and tunneled-backed servers own the issuer minted with them.
	// An issuer may also be referenced by another server or toolset, so only
	// cascade once this deletion leaves it without an active owner.
	var orphanCreds []remotesessions.RevokedCredentials
	if deleted.UserSessionIssuerID.Valid && !deleted.ToolsetID.Valid {
		userSessionsRepo := usersessionsrepo.New(dbtx)
		// Lock the issuer row before the ownership check. A concurrent meta
		// MCP attach holds this same row lock while writing its reference, so
		// the statements below see any newly committed owner. A missing
		// issuer must not block server deletion, so ErrNoRows skips the
		// cascade entirely.
		_, lockErr := userSessionsRepo.LockUserSessionIssuer(ctx, usersessionsrepo.LockUserSessionIssuerParams{
			ID:             deleted.UserSessionIssuerID.UUID,
			ProjectID:      *authCtx.ProjectID,
			OrganizationID: authCtx.ActiveOrganizationID,
		})
		if lockErr != nil && !errors.Is(lockErr, pgx.ErrNoRows) {
			return oops.E(oops.CodeUnexpected, lockErr, "lock mcp server issuer").LogError(ctx, logger)
		}

		hasActiveOwner, err := userSessionsRepo.UserSessionIssuerHasActiveOwner(ctx, usersessionsrepo.UserSessionIssuerHasActiveOwnerParams{
			ProjectID:           *authCtx.ProjectID,
			OrganizationID:      authCtx.ActiveOrganizationID,
			UserSessionIssuerID: deleted.UserSessionIssuerID.UUID,
		})
		if err != nil {
			return oops.E(oops.CodeUnexpected, err, "check user session issuer ownership").LogError(ctx, logger)
		}

		if lockErr == nil && !hasActiveOwner {
			deletedIssuer, err := userSessionsRepo.DeleteUserSessionIssuer(ctx, usersessionsrepo.DeleteUserSessionIssuerParams{
				ID:             deleted.UserSessionIssuerID.UUID,
				ProjectID:      *authCtx.ProjectID,
				OrganizationID: authCtx.ActiveOrganizationID,
			})
			switch {
			case errors.Is(err, pgx.ErrNoRows):
				// A missing issuer must not block server deletion.
			case err != nil:
				return oops.E(oops.CodeUnexpected, err, "delete mcp server issuer").LogError(ctx, logger)
			default:
				orphanCreds, err = s.revoker.DetachUserSessionIssuerFromClients(ctx, dbtx, deletedIssuer.ID, *authCtx.ProjectID, authCtx.ActiveOrganizationID)
				if err != nil {
					return oops.E(oops.CodeUnexpected, err, "detach remote session clients from mcp server issuer").LogError(ctx, logger)
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

	// Post-commit, best-effort: RFC 7009 for the orphaned grants.
	s.revoker.RevokeAllDetached(ctx, orphanCreds)

	if err := s.reconcileMcpServerCustomDomains(ctx, RootDomainIDs(locked.RootEndpoints)); err != nil {
		return err
	}

	return nil
}

func (s *Service) toolsetMirror(authCtx *contextvalues.AuthContext) hostedmcp.Mirror {
	return hostedmcp.Mirror{Audit: s.audit, ActorUserID: authCtx.UserID, ActorEmail: authCtx.Email}
}

// lockServerBacking locks a server's backing toolset ahead of every domain,
// server, and endpoint lock so the write serializes with the toolset's own.
func lockServerBacking(ctx context.Context, dbtx pgx.Tx, projectID, serverID uuid.UUID) (repo.McpServer, error) {
	preread, err := repo.New(dbtx).GetMCPServerByIDAndProjectID(ctx, repo.GetMCPServerByIDAndProjectIDParams{ID: serverID, ProjectID: projectID})
	if err != nil {
		return repo.McpServer{}, fmt.Errorf("get mcp server: %w", err)
	}
	if err := hostedmcp.LockToolsets(ctx, dbtx, projectID, preread.ToolsetID); err != nil {
		return repo.McpServer{}, fmt.Errorf("lock backing toolset: %w", err)
	}
	return preread, nil
}

func lockMcpServerCustomDomains(ctx context.Context, dbtx pgx.Tx, domainIDs []uuid.UUID) error {
	slices.SortFunc(domainIDs, func(a, b uuid.UUID) int {
		return strings.Compare(a.String(), b.String())
	})
	repository := customdomainsrepo.New(dbtx)
	for _, domainID := range domainIDs {
		if _, err := repository.LockCustomDomainByID(ctx, domainID); err != nil {
			return fmt.Errorf("lock custom domain %s: %w", domainID, err)
		}
	}
	return nil
}

// RootDomainIDs lists, sorted and deduplicated, the custom domains whose root
// endpoint sat on the given rows.
func RootDomainIDs(endpoints []mcpendpointsrepo.McpEndpoint) []uuid.UUID {
	seen := make(map[uuid.UUID]struct{}, len(endpoints))
	result := make([]uuid.UUID, 0, len(endpoints))
	for _, endpoint := range endpoints {
		if !endpoint.CustomDomainID.Valid {
			continue
		}
		if _, ok := seen[endpoint.CustomDomainID.UUID]; ok {
			continue
		}
		seen[endpoint.CustomDomainID.UUID] = struct{}{}
		result = append(result, endpoint.CustomDomainID.UUID)
	}
	slices.SortFunc(result, func(a, b uuid.UUID) int {
		return strings.Compare(a.String(), b.String())
	})
	return result
}

func (s *Service) reconcileMcpServerCustomDomains(ctx context.Context, customDomainIDs []uuid.UUID) error {
	if err := background.ReconcileCustomDomains(ctx, s.logger, s.temporalEnv, customDomainIDs); err != nil {
		return fmt.Errorf("reconcile custom domains: %w", err)
	}
	return nil
}

// serverIDs bundles the optional UUID references on the mcp_servers
// create/update payloads so they can be passed around without a long
// positional argument list.
type serverIDs struct {
	EnvironmentID uuid.NullUUID
	// Set by MintServerUserSessionIssuer during create, never parsed from a payload.
	UserSessionIssuerID   uuid.NullUUID
	RemoteMcpServerID     uuid.NullUUID
	TunneledMcpServerID   uuid.NullUUID
	ToolsetID             uuid.NullUUID
	UnproxiedMcpServerID  uuid.NullUUID
	ToolVariationsGroupID uuid.NullUUID
}

// serverIDStrings bundles the optional UUID payload fields shared by the
// create/update forms, as raw strings straight off the wire, so
// parseServerIDs takes one labeled argument instead of a positional run of
// same-typed *string parameters.
type serverIDStrings struct {
	EnvironmentID         *string
	RemoteMcpServerID     *string
	TunneledMcpServerID   *string
	ToolsetID             *string
	UnproxiedMcpServerID  *string
	ToolVariationsGroupID *string
}

// parseServerIDs parses the optional UUID payload fields into a
// serverIDs struct. Any malformed UUID surfaces with a field-specific error.
func parseServerIDs(in serverIDStrings) (serverIDs, error) {
	var (
		ids serverIDs
		err error
	)

	if ids.EnvironmentID, err = conv.PtrToNullUUID(in.EnvironmentID); err != nil {
		return serverIDs{}, fmt.Errorf("invalid environment_id: %w", err)
	}
	if ids.RemoteMcpServerID, err = conv.PtrToNullUUID(in.RemoteMcpServerID); err != nil {
		return serverIDs{}, fmt.Errorf("invalid remote_mcp_server_id: %w", err)
	}
	if ids.TunneledMcpServerID, err = conv.PtrToNullUUID(in.TunneledMcpServerID); err != nil {
		return serverIDs{}, fmt.Errorf("invalid tunneled_mcp_server_id: %w", err)
	}
	if ids.ToolsetID, err = conv.PtrToNullUUID(in.ToolsetID); err != nil {
		return serverIDs{}, fmt.Errorf("invalid toolset_id: %w", err)
	}
	if ids.UnproxiedMcpServerID, err = conv.PtrToNullUUID(in.UnproxiedMcpServerID); err != nil {
		return serverIDs{}, fmt.Errorf("invalid unproxied_mcp_server_id: %w", err)
	}
	if ids.ToolVariationsGroupID, err = conv.PtrToNullUUID(in.ToolVariationsGroupID); err != nil {
		return serverIDs{}, fmt.Errorf("invalid tool_variations_group_id: %w", err)
	}

	return ids, nil
}

func validateServerBackendExclusivity(ids serverIDs) error {
	if backendFilterCount(ids.RemoteMcpServerID, ids.TunneledMcpServerID, ids.ToolsetID, ids.UnproxiedMcpServerID) != 1 {
		return fmt.Errorf("exactly one of remote_mcp_server_id, tunneled_mcp_server_id, toolset_id, or unproxied_mcp_server_id must be provided")
	}
	return nil
}

// verifyTunneledPublicConsent enforces the double opt-in for anonymous public
// serving of tunneled backends: an mcp_server may only take public visibility
// when the tunneled source's owner has set allow_public. Runs inside the
// create/update transaction against the authoritative post-write state so no
// payload permutation can slip a public tunneled server past the check.
func verifyTunneledPublicConsent(ctx context.Context, dbtx pgx.Tx, projectID uuid.UUID, tunneledMcpServerID uuid.NullUUID, visibility string) error {
	if !tunneledMcpServerID.Valid || visibility != VisibilityPublic {
		return nil
	}
	source, err := tunneledmcprepo.New(dbtx).GetServerByID(ctx, tunneledmcprepo.GetServerByIDParams{
		ID:        tunneledMcpServerID.UUID,
		ProjectID: projectID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("tunneled_mcp_server_id does not reference a resource in this project")
	}
	if err != nil {
		return fmt.Errorf("check tunneled mcp server public consent: %w", err)
	}
	if !source.AllowPublic {
		return fmt.Errorf("tunneled MCP servers cannot be public until the tunnel source enables public serving")
	}
	return nil
}

// verifyMetaMcpBackendUniqueness rejects repointing a server onto a backend a
// co-member of one of its meta MCP servers already fronts, the state
// metamcp.AddMetaMcpMember refuses at attach time. Running after the server
// lock leaves nowhere to also take the meta lock without inverting that path's
// meta-then-server order, so a simultaneous attach can still slip past.
func verifyMetaMcpBackendUniqueness(
	ctx context.Context,
	dbtx pgx.Tx,
	projectID uuid.UUID,
	serverID uuid.UUID,
	existing repo.McpServer,
	ids serverIDs,
	logger *slog.Logger,
) error {
	if ids.RemoteMcpServerID == existing.RemoteMcpServerID &&
		ids.TunneledMcpServerID == existing.TunneledMcpServerID &&
		ids.ToolsetID == existing.ToolsetID &&
		ids.UnproxiedMcpServerID == existing.UnproxiedMcpServerID {
		return nil
	}

	metaName, err := metamcprepo.New(dbtx).FindMetaMCPSiblingSharingBackend(ctx, metamcprepo.FindMetaMCPSiblingSharingBackendParams{
		McpServerID:          serverID,
		ProjectID:            projectID,
		RemoteMcpServerID:    ids.RemoteMcpServerID,
		TunneledMcpServerID:  ids.TunneledMcpServerID,
		ToolsetID:            ids.ToolsetID,
		UnproxiedMcpServerID: ids.UnproxiedMcpServerID,
	})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return nil
	case err != nil:
		return oops.E(oops.CodeUnexpected, err, "check meta mcp members sharing a backend").LogError(ctx, logger)
	default:
		return oops.E(oops.CodeConflict, nil, "another member of meta mcp server %q already fronts this backend", metaName).LogError(ctx, logger)
	}
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

// requireStaffForUnproxiedBackend rejects attaching an mcp_servers row to an
// unproxied backend unless the caller is Speakeasy staff. Unproxied MCP
// servers are a staff-curated catalog (see unproxiedmcp.CreateServer); without
// this check, any project member could wrap an existing unproxied_mcp_servers
// row in their own mcp_servers entry and distribute it, bypassing the
// staff-only restriction on that catalog.
func requireStaffForUnproxiedBackend(ctx context.Context, authCtx *contextvalues.AuthContext, unproxiedMcpServerID uuid.NullUUID, logger *slog.Logger) error {
	if !unproxiedMcpServerID.Valid {
		return nil
	}

	return access.RequireStaffForUnproxiedMcp(ctx, authCtx, "attached", logger)
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

	if ids.UnproxiedMcpServerID.Valid {
		if _, err := unproxiedmcprepo.New(dbtx).GetServerByID(ctx, unproxiedmcprepo.GetServerByIDParams{
			ID:        ids.UnproxiedMcpServerID.UUID,
			ProjectID: projectID,
		}); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return fmt.Errorf("unproxied_mcp_server_id does not reference a resource in this project")
			}
			return fmt.Errorf("check unproxied mcp server ownership: %w", err)
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
