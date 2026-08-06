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
	"github.com/speakeasy-api/gram/server/internal/mcp/toolfilter"
	mcpendpointsrepo "github.com/speakeasy-api/gram/server/internal/mcpendpoints/repo"
	mcpmetadatarepo "github.com/speakeasy-api/gram/server/internal/mcpmetadata/repo"
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

	if err := verifyTunneledPublicConsent(ctx, dbtx, *authCtx.ProjectID, ids.TunneledMcpServerID, string(payload.Visibility)); err != nil {
		return nil, oops.E(oops.CodeInvalid, err, "invalid mcp server").LogWarn(ctx, logger)
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
		UnproxiedMcpServerID:  ids.UnproxiedMcpServerID,
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

	// Best-effort: an unproxied server has no logo of its own to inherit, so
	// give it the vendor's favicon as a starting icon rather than leaving it
	// blank. Backgrounded on a detached context so a slow or unreachable
	// favicon (up to the fetch's own timeout) doesn't add latency to the
	// create response; the request's ctx would otherwise cancel this the
	// moment the handler returns. Bounded independently of the request so a
	// stuck DB call can't leave the goroutine running forever.
	if ids.UnproxiedMcpServerID.Valid {
		bgCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second) //nolint:gosec // cancel is deferred inside the detached goroutine below
		go func() {
			defer cancel()
			s.setDefaultUnproxiedIcon(bgCtx, logger, *authCtx.ProjectID, server.ID, ids.UnproxiedMcpServerID.UUID)
		}()
	}

	return mv.BuildMcpServerView(server), nil
}

// setDefaultUnproxiedIcon fetches the vendor's favicon and sets it as the
// newly-created server's icon. Best-effort: any failure is logged and
// swallowed rather than surfaced, since a missing icon is cosmetic and must
// never fail the create call it's attached to.
func (s *Service) setDefaultUnproxiedIcon(ctx context.Context, logger *slog.Logger, projectID uuid.UUID, mcpServerID uuid.UUID, unproxiedMcpServerID uuid.UUID) {
	source, err := unproxiedmcprepo.New(s.db).GetServerByID(ctx, unproxiedmcprepo.GetServerByIDParams{
		ID:        unproxiedMcpServerID,
		ProjectID: projectID,
	})
	if err != nil {
		logger.ErrorContext(ctx, "load unproxied mcp server for default icon", attr.SlogError(err))
		return
	}

	vendorURL, err := url.Parse(source.Url)
	if err != nil {
		logger.ErrorContext(ctx, "parse unproxied mcp server url for default icon", attr.SlogError(err))
		return
	}

	asset, err := s.assets.FetchImageAssetFromURL(ctx, unproxiedFaviconURL(vendorURL.Scheme, vendorURL.Host))
	if err != nil {
		// Some vendors only register a favicon against their registrable
		// domain, not the specific subdomain hosting the MCP endpoint (e.g.
		// mcp.figma.com has none, figma.com does) -- retry once against that
		// before giving up.
		if registrable, rErr := publicsuffix.EffectiveTLDPlusOne(vendorURL.Hostname()); rErr == nil && registrable != vendorURL.Host {
			asset, err = s.assets.FetchImageAssetFromURL(ctx, unproxiedFaviconURL(vendorURL.Scheme, registrable))
		}
	}
	if err != nil {
		logger.WarnContext(ctx, "fetch default favicon for unproxied mcp server", attr.SlogError(err))
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
		logger.ErrorContext(ctx, "save default favicon for unproxied mcp server", attr.SlogError(err))
	}
}

func unproxiedFaviconURL(scheme, host string) string {
	return fmt.Sprintf(
		"https://t0.gstatic.com/faviconV2?client=SOCIAL&type=FAVICON&fallback_opts=TYPE,SIZE,URL&url=%s&size=128",
		url.QueryEscape(scheme+"://"+host),
	)
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
	unproxiedMcpServerID, err := conv.PtrToNullUUID(payload.UnproxiedMcpServerID)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid unproxied_mcp_server_id").LogError(ctx, logger)
	}
	if backendFilterCount(remoteMcpServerID, tunneledMcpServerID, toolsetID, unproxiedMcpServerID) > 1 {
		return nil, oops.E(oops.CodeInvalid, nil, "at most one of remote_mcp_server_id, tunneled_mcp_server_id, toolset_id, or unproxied_mcp_server_id may be provided").LogWarn(ctx, logger)
	}

	servers, err := repo.New(s.db).ListMCPServersByProjectID(ctx, repo.ListMCPServersByProjectIDParams{
		ProjectID:            *authCtx.ProjectID,
		RemoteMcpServerID:    remoteMcpServerID,
		TunneledMcpServerID:  tunneledMcpServerID,
		ToolsetID:            toolsetID,
		UnproxiedMcpServerID: unproxiedMcpServerID,
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

	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "begin transaction").LogError(ctx, logger)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })

	txRepo := repo.New(dbtx)

	var affectedDomainIDs []uuid.UUID
	if payload.Visibility == VisibilityDisabled {
		affectedDomainIDs, err = mcpendpointsrepo.New(dbtx).ListCustomDomainIDsByMCPServerID(ctx, mcpendpointsrepo.ListCustomDomainIDsByMCPServerIDParams{
			McpServerID: serverID,
			ProjectID:   *authCtx.ProjectID,
		})
		if err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "list custom domains for mcp server").LogError(ctx, logger)
		}
	}

	if err := lockMcpServerCustomDomains(ctx, dbtx, affectedDomainIDs); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "lock custom domains").LogError(ctx, logger)
	}
	if payload.Visibility == VisibilityDisabled {
		_, err = mcpendpointsrepo.New(dbtx).LockRootMCPEndpointsByMCPServerID(ctx, mcpendpointsrepo.LockRootMCPEndpointsByMCPServerIDParams{
			McpServerID: serverID,
			ProjectID:   *authCtx.ProjectID,
		})
		if err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "lock root mcp endpoints").LogError(ctx, logger)
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

	beforeView := mv.BuildMcpServerView(existing)

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

	// NULL leaves the stored issuer untouched (the update query COALESCEs).
	// A backend switch onto remote/tunneled from a backend that never carried
	// one (toolset, unproxied) needs a fresh mint here, or the insert trips
	// mcp_servers_issuer_required_check.
	issuerID := uuid.NullUUID{UUID: uuid.Nil, Valid: false}
	if (ids.RemoteMcpServerID.Valid || ids.TunneledMcpServerID.Valid) && !existing.UserSessionIssuerID.Valid {
		issuerID, err = mintServerUserSessionIssuer(ctx, dbtx, *authCtx.ProjectID, slug)
		if err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "mint mcp server issuer").LogError(ctx, logger)
		}
	}

	updated, err := txRepo.UpdateMCPServer(ctx, repo.UpdateMCPServerParams{
		Name:                  name,
		Slug:                  conv.ToPGText(slug),
		EnvironmentID:         ids.EnvironmentID,
		UserSessionIssuerID:   issuerID,
		RemoteMcpServerID:     ids.RemoteMcpServerID,
		TunneledMcpServerID:   ids.TunneledMcpServerID,
		ToolsetID:             ids.ToolsetID,
		UnproxiedMcpServerID:  ids.UnproxiedMcpServerID,
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

	// Check against the post-update row (not the payload): the update query
	// COALESCEs unset backend references, so this is the only state that
	// reliably says whether the server is now tunneled + public.
	if err := verifyTunneledPublicConsent(ctx, dbtx, *authCtx.ProjectID, updated.TunneledMcpServerID, updated.Visibility); err != nil {
		return nil, oops.E(oops.CodeInvalid, err, "invalid mcp server").LogWarn(ctx, logger)
	}

	oldDisplayName := ServerDisplayName(existing)
	newDisplayName := ServerDisplayName(updated)
	if oldDisplayName != newDisplayName {
		if _, err := pluginsrepo.New(dbtx).SyncMcpServerDisplayName(ctx, pluginsrepo.SyncMcpServerDisplayNameParams{
			NewDisplayName: newDisplayName,
			ProjectID:      *authCtx.ProjectID,
			McpServerID:    uuid.NullUUID{UUID: updated.ID, Valid: true},
			OldDisplayName: oldDisplayName,
		}); err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "sync plugin server display name").LogError(ctx, logger)
		}
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

	var clearedRootEndpoints []mcpendpointsrepo.McpEndpoint
	if updated.Visibility == VisibilityDisabled {
		clearedRootEndpoints, err = mcpendpointsrepo.New(dbtx).ClearRootMCPEndpointsByMCPServerID(ctx, mcpendpointsrepo.ClearRootMCPEndpointsByMCPServerIDParams{
			McpServerID: updated.ID,
			ProjectID:   *authCtx.ProjectID,
		})
		if err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "clear root mcp endpoints").LogError(ctx, logger)
		}
		if err := s.logMcpServerRootAutoClears(ctx, dbtx, authCtx, clearedRootEndpoints); err != nil {
			return nil, err
		}
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
	if err := s.reconcileMcpServerCustomDomains(ctx, rootDomainIDs(clearedRootEndpoints)); err != nil {
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

	affectedDomainIDs, err := mcpendpointsrepo.New(dbtx).ListCustomDomainIDsByMCPServerID(ctx, mcpendpointsrepo.ListCustomDomainIDsByMCPServerIDParams{
		McpServerID: serverID,
		ProjectID:   *authCtx.ProjectID,
	})
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "list custom domains for mcp server").LogError(ctx, logger)
	}

	if err := lockMcpServerCustomDomains(ctx, dbtx, affectedDomainIDs); err != nil {
		return oops.E(oops.CodeUnexpected, err, "lock custom domains").LogError(ctx, logger)
	}
	if _, err := mcpendpointsrepo.New(dbtx).LockMCPEndpointsByMCPServerID(ctx, mcpendpointsrepo.LockMCPEndpointsByMCPServerIDParams{
		McpServerID: serverID,
		ProjectID:   *authCtx.ProjectID,
	}); err != nil {
		return oops.E(oops.CodeUnexpected, err, "lock mcp endpoints").LogError(ctx, logger)
	}
	if _, err := txRepo.LockMCPServerByIDAndProjectID(ctx, repo.LockMCPServerByIDAndProjectIDParams{
		ID:        serverID,
		ProjectID: *authCtx.ProjectID,
	}); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return oops.E(oops.CodeNotFound, err, "mcp server not found").LogError(ctx, logger)
		}
		return oops.E(oops.CodeUnexpected, err, "lock mcp server").LogError(ctx, logger)
	}
	// Post-server-lock read is the authoritative root set: the server FOR SHARE in root selection means no new root can commit past this point, and rows here carry pre-delete is_domain_root.
	rootEndpoints, err := mcpendpointsrepo.New(dbtx).LockMCPEndpointsByMCPServerID(ctx, mcpendpointsrepo.LockMCPEndpointsByMCPServerIDParams{
		McpServerID: serverID,
		ProjectID:   *authCtx.ProjectID,
	})
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "lock root mcp endpoints").LogError(ctx, logger)
	}
	rootEndpoints = slices.DeleteFunc(rootEndpoints, func(endpoint mcpendpointsrepo.McpEndpoint) bool {
		return !endpoint.IsDomainRoot.Valid || !endpoint.IsDomainRoot.Bool
	})

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
	if err := s.logMcpServerRootAutoClears(ctx, dbtx, authCtx, rootEndpoints); err != nil {
		return err
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

	if err := s.reconcileMcpServerCustomDomains(ctx, rootDomainIDs(rootEndpoints)); err != nil {
		return err
	}

	return nil
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

func rootDomainIDs(endpoints []mcpendpointsrepo.McpEndpoint) []uuid.UUID {
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

func (s *Service) logMcpServerRootAutoClears(
	ctx context.Context,
	dbtx pgx.Tx,
	authCtx *contextvalues.AuthContext,
	rootEndpoints []mcpendpointsrepo.McpEndpoint,
) error {
	repository := customdomainsrepo.New(dbtx)
	for _, endpoint := range rootEndpoints {
		if !endpoint.CustomDomainID.Valid {
			continue
		}
		domain, err := repository.GetCustomDomainByID(ctx, endpoint.CustomDomainID.UUID)
		if err != nil {
			return oops.E(oops.CodeUnexpected, err, "load custom domain for root cleanup audit").LogError(ctx, s.logger)
		}
		if err := s.audit.LogCustomDomainUpdate(ctx, dbtx, audit.LogCustomDomainUpdateEvent{
			OrganizationID:             authCtx.ActiveOrganizationID,
			Actor:                      urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID),
			ActorDisplayName:           authCtx.Email,
			ActorSlug:                  nil,
			CustomDomainURN:            urn.NewCustomDomain(domain.ID),
			DomainName:                 domain.Domain,
			CustomDomainSnapshotBefore: mv.BuildCustomDomainView(domain, false, endpoint.ID),
			CustomDomainSnapshotAfter:  mv.BuildCustomDomainView(domain, false, uuid.Nil),
		}); err != nil {
			return oops.E(oops.CodeUnexpected, err, "log automatic root endpoint cleanup").LogError(ctx, s.logger)
		}
	}
	return nil
}

func (s *Service) reconcileMcpServerCustomDomains(ctx context.Context, customDomainIDs []uuid.UUID) error {
	if s.temporalEnv == nil {
		return nil
	}
	var reconcileErrors []error
	for _, customDomainID := range customDomainIDs {
		_, err := (&background.CustomDomainRegistrationClient{TemporalEnv: s.temporalEnv}).ExecuteCustomDomainReconcile(ctx, customDomainID)
		if err != nil {
			reconcileErrors = append(reconcileErrors, oops.E(oops.CodeUnexpected, err, "start custom domain reconciliation").LogError(ctx, s.logger))
		}
	}
	return errors.Join(reconcileErrors...)
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
