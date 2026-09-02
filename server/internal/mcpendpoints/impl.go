package mcpendpoints

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgerrcode"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/otel/trace"
	goahttp "goa.design/goa/v3/http"
	"goa.design/goa/v3/security"

	srv "github.com/speakeasy-api/gram/server/gen/http/mcp_endpoints/server"
	gen "github.com/speakeasy-api/gram/server/gen/mcp_endpoints"
	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/auth"
	"github.com/speakeasy-api/gram/server/internal/auth/sessions"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/background"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	customdomainsrepo "github.com/speakeasy-api/gram/server/internal/customdomains/repo"
	"github.com/speakeasy-api/gram/server/internal/hostedmcp"
	"github.com/speakeasy-api/gram/server/internal/mcpendpoints/repo"
	"github.com/speakeasy-api/gram/server/internal/mcpservers"
	mcpserversrepo "github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
	metamcprepo "github.com/speakeasy-api/gram/server/internal/metamcp/repo"
	"github.com/speakeasy-api/gram/server/internal/middleware"
	"github.com/speakeasy-api/gram/server/internal/mv"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/plugins"
	tenv "github.com/speakeasy-api/gram/server/internal/temporal"
	"github.com/speakeasy-api/gram/server/internal/urn"
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
	logger = logger.With(attr.SlogComponent("mcpendpoints"))

	return &Service{
		tracer:               tracerProvider.Tracer("github.com/speakeasy-api/gram/server/internal/mcpendpoints"),
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

func (s *Service) CreateMcpEndpoint(ctx context.Context, payload *gen.CreateMcpEndpointPayload) (*types.McpEndpoint, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil || authCtx.OrganizationSlug == "" {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeMCPWrite, ResourceKind: "", ResourceID: authCtx.ProjectID.String(), Dimensions: nil}); err != nil {
		return nil, err
	}

	logger := s.logger.With(attr.SlogProjectID(authCtx.ProjectID.String()))

	customDomainID, err := conv.PtrToNullUUID(payload.CustomDomainID)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid custom_domain_id").LogError(ctx, logger)
	}

	mcpServerID, metaMcpServerID, err := s.parseEndpointBackendIDs(ctx, logger, payload.McpServerID, payload.MetaMcpServerID)
	if err != nil {
		return nil, err
	}

	slug := string(payload.Slug)
	if err := validateSlugPrefix(slug, customDomainID, authCtx.OrganizationSlug); err != nil {
		return nil, oops.E(oops.CodeInvalid, err, "invalid slug").LogError(ctx, logger)
	}

	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "begin transaction").LogError(ctx, logger)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })

	txRepo := repo.New(dbtx)

	backing, err := lockBackingToolsets(ctx, dbtx, *authCtx.ProjectID, UniqueIDs(mcpServerID))
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "lock backing toolsets").LogError(ctx, logger)
	}
	if _, hosted := backing[mcpServerID.UUID]; hosted && utf8.RuneCountInString(slug) > hostedmcp.MaxToolsetSlugLength {
		return nil, oops.E(oops.CodeInvalid, nil, "hosted server slugs are at most %d characters", hostedmcp.MaxToolsetSlugLength).LogWarn(ctx, logger)
	}

	// Match the deletion and update paths' lock order — custom domains before
	// backend rows — so a create racing a backend deletion cannot deadlock:
	// the insert's FK share on the domain row must not be requested while
	// this transaction already holds the backend row a deleter is waiting on.
	if err := lockCustomDomains(ctx, dbtx, UniqueIDs(customDomainID)); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, oops.E(oops.CodeInvalid, err, "custom_domain_id does not reference a live custom domain").LogError(ctx, logger)
		}
		return nil, oops.E(oops.CodeUnexpected, err, "lock custom domain").LogError(ctx, logger)
	}

	// Lock the backend row before validating and inserting. Backend deletion
	// tombstones the backend and cascades a soft delete over its endpoints in
	// one transaction, so an unlocked create could validate a live backend,
	// then insert after that cascade commits, leaving a live endpoint pointing
	// at a tombstoned backend.
	backingToolsetID := uuid.NullUUID{UUID: uuid.Nil, Valid: false}
	if mcpServerID.Valid {
		lockedServer, err := mcpserversrepo.New(dbtx).LockMCPServerByIDAndProjectID(ctx, mcpserversrepo.LockMCPServerByIDAndProjectIDParams{
			ID:        mcpServerID.UUID,
			ProjectID: *authCtx.ProjectID,
		})
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return nil, oops.E(oops.CodeInvalid, err, "mcp_server_id does not reference a resource in this project").LogError(ctx, logger)
			}
			return nil, oops.E(oops.CodeUnexpected, err, "lock mcp server").LogError(ctx, logger)
		}
		if err := verifyBacking(ctx, logger, lockedServer, backing); err != nil {
			return nil, err
		}
		backingToolsetID = lockedServer.ToolsetID
	}
	if err := s.lockMetaMcpServers(ctx, dbtx, authCtx, UniqueIDs(metaMcpServerID), metaMcpServerID); err != nil {
		return nil, err
	}

	if err := verifyEndpointReferenceOwnership(ctx, dbtx, *authCtx.ProjectID, authCtx.ActiveOrganizationID, mcpServerID, metaMcpServerID, customDomainID); err != nil {
		return nil, oops.E(oops.CodeInvalid, err, "invalid mcp endpoint").LogError(ctx, logger)
	}

	if err := LockSlugScope(ctx, dbtx, customDomainID, slug); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "lock mcp endpoint slug scope").LogError(ctx, logger)
	}
	// Availability spans toolsets.mcp_slug too; the target server's own
	// backing toolset does not count.
	available, err := CheckSlugAvailable(ctx, dbtx, SlugAvailabilityCheck{
		Slug:                     slug,
		CustomDomainID:           customDomainID,
		OrganizationID:           authCtx.ActiveOrganizationID,
		ExcludeToolsetID:         uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		ExcludeMcpServerID:       mcpServerID,
		SkipDomainOwnershipCheck: false,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "check mcp endpoint slug availability").LogError(ctx, logger)
	}
	if !available {
		return nil, oops.E(oops.CodeConflict, nil, "mcp endpoint slug already exists for this domain").LogError(ctx, logger)
	}

	created, err := txRepo.CreateMCPEndpoint(ctx, repo.CreateMCPEndpointParams{
		ProjectID:       *authCtx.ProjectID,
		CustomDomainID:  customDomainID,
		McpServerID:     mcpServerID,
		MetaMcpServerID: metaMcpServerID,
		Slug:            slug,
	})
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == pgerrcode.UniqueViolation {
			return nil, oops.E(oops.CodeConflict, err, "mcp endpoint slug already exists for this domain").LogError(ctx, logger)
		}
		return nil, oops.E(oops.CodeUnexpected, err, "create mcp endpoint").LogError(ctx, logger)
	}

	if err := s.audit.LogMcpEndpointCreate(ctx, dbtx, audit.LogMcpEndpointCreateEvent{
		OrganizationID:   authCtx.ActiveOrganizationID,
		ProjectID:        *authCtx.ProjectID,
		Actor:            urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID),
		ActorDisplayName: authCtx.Email,
		ActorSlug:        nil,
		McpEndpointURN:   urn.NewMcpEndpoint(created.ID),
		Slug:             created.Slug,
	}); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "log mcp endpoint creation").LogError(ctx, logger)
	}

	if backingToolsetID.Valid {
		if err := s.syncBackingToolsetAddress(ctx, dbtx, authCtx, mcpServerID.UUID, backingToolsetID.UUID); err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "mirror endpoint onto toolset").LogError(ctx, logger)
		}
	}

	// Meta-MCP-backed endpoints never participate in default-plugin attachment
	// or marketplace publishing; that flow is exclusive to generic MCP servers.
	var pluginCreated bool
	if mcpServerID.Valid {
		pluginCreated, err = s.attachToDefaultPlugin(ctx, dbtx, authCtx, mcpServerID.UUID)
		if err != nil {
			return nil, err
		}
	}

	if err := dbtx.Commit(ctx); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "commit transaction").LogError(ctx, logger)
	}

	s.triggerInitialPublishIfNeeded(ctx, authCtx, pluginCreated)

	return mv.BuildMcpEndpointView(created), nil
}

// attachToDefaultPlugin adds an mcp_server to the project's Default plugin
// the first time it gets a usable endpoint (this is what AddPluginServer's
// own publishability check requires), so it's included in the
// auto-published marketplace without a human visiting the Plugins page.
// No-op if the server is disabled or it's already attached (e.g. a second
// endpoint on the same server). Returns pluginCreated=true if this call
// lazily created the Default plugin (project predates this feature) —
// callers should enqueue an initial publish for it, but only after their
// own transaction commits, since this runs pre-commit and the DB writes
// could still roll back.
func (s *Service) attachToDefaultPlugin(ctx context.Context, dbtx pgx.Tx, authCtx *contextvalues.AuthContext, mcpServerID uuid.UUID) (bool, error) {
	server, err := mcpserversrepo.New(dbtx).GetMCPServerByIDAndProjectID(ctx, mcpserversrepo.GetMCPServerByIDAndProjectIDParams{
		ID:        mcpServerID,
		ProjectID: *authCtx.ProjectID,
	})
	if err != nil {
		return false, oops.E(oops.CodeUnexpected, err, "load mcp server").LogError(ctx, s.logger)
	}
	if server.Visibility == mcpservers.VisibilityDisabled {
		return false, nil
	}

	pluginCreated, err := plugins.AttachToDefaultPluginAudited(ctx, dbtx, s.audit, authCtx, plugins.AttachToDefaultPluginParams{
		OrganizationID: authCtx.ActiveOrganizationID,
		ProjectID:      *authCtx.ProjectID,
		ToolsetID:      uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		McpServerID:    uuid.NullUUID{UUID: mcpServerID, Valid: true},
		DisplayName:    mcpservers.ServerDisplayName(server),
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

func (s *Service) GetMcpEndpoint(ctx context.Context, payload *gen.GetMcpEndpointPayload) (*types.McpEndpoint, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeMCPRead, ResourceKind: "", ResourceID: authCtx.ProjectID.String(), Dimensions: nil}); err != nil {
		return nil, err
	}

	hasID := payload.ID != nil && *payload.ID != ""
	hasSlug := payload.Slug != nil && *payload.Slug != ""

	if hasID == hasSlug {
		return nil, oops.E(oops.CodeInvalid, nil, "provide exactly one of id or slug").LogError(ctx, s.logger)
	}

	if hasID && payload.CustomDomainID != nil {
		return nil, oops.E(oops.CodeInvalid, nil, "custom_domain_id cannot be combined with id").LogError(ctx, s.logger)
	}

	txRepo := repo.New(s.db)

	if hasID {
		endpointID, err := uuid.Parse(*payload.ID)
		if err != nil {
			return nil, oops.E(oops.CodeBadRequest, err, "invalid mcp endpoint id").LogError(ctx, s.logger)
		}

		row, err := txRepo.GetMCPEndpointByID(ctx, repo.GetMCPEndpointByIDParams{
			ID:        endpointID,
			ProjectID: *authCtx.ProjectID,
		})
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return nil, oops.E(oops.CodeNotFound, err, "mcp endpoint not found").LogError(ctx, s.logger)
			}
			return nil, oops.E(oops.CodeUnexpected, err, "get mcp endpoint").LogError(ctx, s.logger)
		}

		return mv.BuildMcpEndpointView(row), nil
	}

	customDomainID, err := conv.PtrToNullUUID(payload.CustomDomainID)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid custom_domain_id").LogError(ctx, s.logger)
	}

	row, err := txRepo.GetMCPEndpointByProjectAndCustomDomainAndSlug(ctx, repo.GetMCPEndpointByProjectAndCustomDomainAndSlugParams{
		ProjectID:      *authCtx.ProjectID,
		Slug:           string(*payload.Slug),
		CustomDomainID: customDomainID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, oops.E(oops.CodeNotFound, err, "mcp endpoint not found").LogError(ctx, s.logger)
		}
		return nil, oops.E(oops.CodeUnexpected, err, "get mcp endpoint").LogError(ctx, s.logger)
	}

	return mv.BuildMcpEndpointView(row), nil
}

func (s *Service) ListMcpEndpoints(ctx context.Context, payload *gen.ListMcpEndpointsPayload) (*gen.ListMcpEndpointsResult, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeMCPRead, ResourceKind: "", ResourceID: authCtx.ProjectID.String(), Dimensions: nil}); err != nil {
		return nil, err
	}

	r := repo.New(s.db)

	hasServerFilter := payload.McpServerID != nil && *payload.McpServerID != ""
	hasMetaFilter := payload.MetaMcpServerID != nil && *payload.MetaMcpServerID != ""
	if hasServerFilter && hasMetaFilter {
		return nil, oops.E(oops.CodeInvalid, nil, "provide at most one of mcp_server_id or meta_mcp_server_id").LogError(ctx, s.logger)
	}

	if hasServerFilter {
		serverID, err := uuid.Parse(*payload.McpServerID)
		if err != nil {
			return nil, oops.E(oops.CodeBadRequest, err, "invalid mcp_server_id").LogError(ctx, s.logger)
		}

		rows, err := r.ListMCPEndpointsByMCPServerID(ctx, repo.ListMCPEndpointsByMCPServerIDParams{
			ProjectID:   *authCtx.ProjectID,
			McpServerID: serverID,
		})
		if err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "list mcp endpoints by server").LogError(ctx, s.logger)
		}

		return &gen.ListMcpEndpointsResult{McpEndpoints: mv.BuildMcpEndpointListView(rows)}, nil
	}

	if hasMetaFilter {
		metaID, err := uuid.Parse(*payload.MetaMcpServerID)
		if err != nil {
			return nil, oops.E(oops.CodeBadRequest, err, "invalid meta_mcp_server_id").LogError(ctx, s.logger)
		}

		rows, err := r.ListMCPEndpointsByMetaMCPServerID(ctx, repo.ListMCPEndpointsByMetaMCPServerIDParams{
			ProjectID:       *authCtx.ProjectID,
			MetaMcpServerID: metaID,
		})
		if err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "list mcp endpoints by meta server").LogError(ctx, s.logger)
		}

		return &gen.ListMcpEndpointsResult{McpEndpoints: mv.BuildMcpEndpointListView(rows)}, nil
	}

	rows, err := r.ListMCPEndpointsByProject(ctx, *authCtx.ProjectID)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list mcp endpoints").LogError(ctx, s.logger)
	}

	return &gen.ListMcpEndpointsResult{McpEndpoints: mv.BuildMcpEndpointListView(rows)}, nil
}

func (s *Service) UpdateMcpEndpoint(ctx context.Context, payload *gen.UpdateMcpEndpointPayload) (*types.McpEndpoint, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil || authCtx.OrganizationSlug == "" {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeMCPWrite, ResourceKind: "", ResourceID: authCtx.ProjectID.String(), Dimensions: nil}); err != nil {
		return nil, err
	}

	logger := s.logger.With(attr.SlogProjectID(authCtx.ProjectID.String()))

	endpointID, err := uuid.Parse(payload.ID)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid mcp endpoint id").LogError(ctx, logger)
	}

	customDomainID, err := conv.PtrToNullUUID(payload.CustomDomainID)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid custom_domain_id").LogError(ctx, logger)
	}

	mcpServerID, metaMcpServerID, err := s.parseEndpointBackendIDs(ctx, logger, payload.McpServerID, payload.MetaMcpServerID)
	if err != nil {
		return nil, err
	}

	slug := string(payload.Slug)
	if err := validateSlugPrefix(slug, customDomainID, authCtx.OrganizationSlug); err != nil {
		return nil, oops.E(oops.CodeInvalid, err, "invalid slug").LogError(ctx, logger)
	}

	preexisting, err := repo.New(s.db).GetMCPEndpointByID(ctx, repo.GetMCPEndpointByIDParams{
		ID:        endpointID,
		ProjectID: *authCtx.ProjectID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, oops.E(oops.CodeNotFound, err, "mcp endpoint not found").LogError(ctx, logger)
		}
		return nil, oops.E(oops.CodeUnexpected, err, "get mcp endpoint").LogError(ctx, logger)
	}

	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "begin transaction").LogError(ctx, logger)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })

	txRepo := repo.New(dbtx)

	backing, err := lockBackingToolsets(ctx, dbtx, *authCtx.ProjectID, UniqueIDs(preexisting.McpServerID, mcpServerID))
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "lock backing toolsets").LogError(ctx, logger)
	}
	if _, hosted := backing[mcpServerID.UUID]; hosted && utf8.RuneCountInString(slug) > hostedmcp.MaxToolsetSlugLength {
		return nil, oops.E(oops.CodeInvalid, nil, "hosted server slugs are at most %d characters", hostedmcp.MaxToolsetSlugLength).LogWarn(ctx, logger)
	}

	domainIDs := UniqueIDs(preexisting.CustomDomainID, customDomainID)
	if err := lockCustomDomains(ctx, dbtx, domainIDs); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, oops.E(oops.CodeInvalid, err, "custom_domain_id does not reference a live custom domain").LogError(ctx, logger)
		}
		return nil, oops.E(oops.CodeUnexpected, err, "lock custom domains").LogError(ctx, logger)
	}

	existing, err := txRepo.LockMCPEndpointByID(ctx, repo.LockMCPEndpointByIDParams{
		ID:        endpointID,
		ProjectID: *authCtx.ProjectID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, oops.E(oops.CodeNotFound, err, "mcp endpoint not found").LogError(ctx, logger)
		}
		return nil, oops.E(oops.CodeUnexpected, err, "get mcp endpoint").LogError(ctx, logger)
	}
	if existing.CustomDomainID != preexisting.CustomDomainID {
		return nil, oops.E(oops.CodeConflict, nil, "mcp endpoint changed concurrently; retry the request").LogError(ctx, logger)
	}

	beforeView := mv.BuildMcpEndpointView(existing)

	var targetServer *mcpserversrepo.McpServer
	if serverIDs := UniqueIDs(existing.McpServerID, mcpServerID); len(serverIDs) > 0 {
		lockedServers, err := mcpserversrepo.New(dbtx).LockMCPServersByIDs(ctx, mcpserversrepo.LockMCPServersByIDsParams{
			ProjectID: *authCtx.ProjectID,
			Ids:       serverIDs,
		})
		if err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "lock mcp servers").LogError(ctx, logger)
		}
		for i := range lockedServers {
			if err := verifyBacking(ctx, logger, lockedServers[i], backing); err != nil {
				return nil, err
			}
			if mcpServerID.Valid && lockedServers[i].ID == mcpServerID.UUID {
				targetServer = &lockedServers[i]
			}
		}
	}
	if mcpServerID.Valid && targetServer == nil {
		return nil, oops.E(oops.CodeInvalid, nil, "mcp_server_id does not reference a resource in this project").LogError(ctx, logger)
	}

	if err := s.lockMetaMcpServers(ctx, dbtx, authCtx, UniqueIDs(existing.MetaMcpServerID, metaMcpServerID), metaMcpServerID); err != nil {
		return nil, err
	}

	if err := verifyEndpointReferenceOwnership(ctx, dbtx, *authCtx.ProjectID, authCtx.ActiveOrganizationID, mcpServerID, metaMcpServerID, customDomainID); err != nil {
		return nil, oops.E(oops.CodeInvalid, err, "invalid mcp endpoint").LogError(ctx, logger)
	}

	// Only an actual address change is probed: an unchanged (domain, slug)
	// pair would collide with itself through a mirrored toolset slug.
	if slug != existing.Slug || customDomainID != existing.CustomDomainID {
		if err := LockSlugScope(ctx, dbtx, customDomainID, slug); err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "lock mcp endpoint slug scope").LogError(ctx, logger)
		}
		available, err := CheckSlugAvailable(ctx, dbtx, SlugAvailabilityCheck{
			Slug:                     slug,
			CustomDomainID:           customDomainID,
			OrganizationID:           authCtx.ActiveOrganizationID,
			ExcludeToolsetID:         uuid.NullUUID{UUID: uuid.Nil, Valid: false},
			ExcludeMcpServerID:       mcpServerID,
			SkipDomainOwnershipCheck: false,
		})
		if err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "check mcp endpoint slug availability").LogError(ctx, logger)
		}
		if !available {
			return nil, oops.E(oops.CodeConflict, nil, "mcp endpoint slug already exists for this domain").LogError(ctx, logger)
		}
	}

	wasRoot := existing.IsDomainRoot.Valid && existing.IsDomainRoot.Bool
	sameDomain := existing.CustomDomainID == customDomainID
	// Meta-MCP-backed endpoints have no visibility notion, so a meta target
	// keeps an existing root marker whenever the domain is unchanged.
	keepRoot := wasRoot && sameDomain && (targetServer == nil || targetServer.Visibility != mcpservers.VisibilityDisabled)
	rootMarker := pgtype.Bool{Bool: false, Valid: false}
	if keepRoot {
		rootMarker = pgtype.Bool{Bool: true, Valid: true}
	}

	updated, err := txRepo.UpdateMCPEndpoint(ctx, repo.UpdateMCPEndpointParams{
		CustomDomainID:  customDomainID,
		McpServerID:     mcpServerID,
		MetaMcpServerID: metaMcpServerID,
		Slug:            slug,
		IsDomainRoot:    rootMarker,
		ID:              endpointID,
		ProjectID:       *authCtx.ProjectID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, oops.E(oops.CodeNotFound, err, "mcp endpoint not found").LogError(ctx, logger)
		}
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == pgerrcode.UniqueViolation {
			return nil, oops.E(oops.CodeConflict, err, "mcp endpoint slug already exists for this domain").LogError(ctx, logger)
		}
		return nil, oops.E(oops.CodeUnexpected, err, "update mcp endpoint").LogError(ctx, logger)
	}

	afterView := mv.BuildMcpEndpointView(updated)

	if err := s.audit.LogMcpEndpointUpdate(ctx, dbtx, audit.LogMcpEndpointUpdateEvent{
		OrganizationID:            authCtx.ActiveOrganizationID,
		ProjectID:                 *authCtx.ProjectID,
		Actor:                     urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID),
		ActorDisplayName:          authCtx.Email,
		ActorSlug:                 nil,
		McpEndpointURN:            urn.NewMcpEndpoint(updated.ID),
		Slug:                      updated.Slug,
		McpEndpointSnapshotBefore: beforeView,
		McpEndpointSnapshotAfter:  afterView,
	}); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "log mcp endpoint update").LogError(ctx, logger)
	}

	if wasRoot && !keepRoot {
		if err := s.logRootAutoClear(ctx, dbtx, authCtx, existing.CustomDomainID.UUID, existing.ID); err != nil {
			return nil, err
		}
	}

	// The vacated server syncs first so its toolset releases the slug before the target claims it.
	syncOrder := []uuid.NullUUID{existing.McpServerID}
	if mcpServerID != existing.McpServerID {
		syncOrder = append(syncOrder, mcpServerID)
	}
	for _, serverID := range syncOrder {
		if toolsetID, ok := backing[serverID.UUID]; ok && serverID.Valid {
			if err := s.syncBackingToolsetAddress(ctx, dbtx, authCtx, serverID.UUID, toolsetID); err != nil {
				return nil, oops.E(oops.CodeUnexpected, err, "mirror endpoint onto toolset").LogError(ctx, logger)
			}
		}
	}

	if err := dbtx.Commit(ctx); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "commit transaction").LogError(ctx, logger)
	}

	if wasRoot && existing.CustomDomainID.Valid {
		if err := s.reconcileCustomDomains(ctx, []uuid.UUID{existing.CustomDomainID.UUID}); err != nil {
			return nil, err
		}
	}

	return afterView, nil
}

func (s *Service) CheckMcpEndpointSlugAvailability(ctx context.Context, payload *gen.CheckMcpEndpointSlugAvailabilityPayload) (bool, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return false, oops.C(oops.CodeUnauthorized)
	}

	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeMCPRead, ResourceKind: "", ResourceID: authCtx.ProjectID.String(), Dimensions: nil}); err != nil {
		return false, err
	}

	logger := s.logger.With(attr.SlogProjectID(authCtx.ProjectID.String()))

	customDomainID, err := conv.PtrToNullUUID(payload.CustomDomainID)
	if err != nil {
		return false, oops.E(oops.CodeBadRequest, err, "invalid custom_domain_id").LogError(ctx, logger)
	}

	// A foreign or unknown domain reads as "unavailable" so slugs can't be
	// probed under domains the caller doesn't own.
	available, err := CheckSlugAvailable(ctx, s.db, SlugAvailabilityCheck{
		Slug:                     string(payload.Slug),
		CustomDomainID:           customDomainID,
		OrganizationID:           authCtx.ActiveOrganizationID,
		ExcludeToolsetID:         uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		ExcludeMcpServerID:       uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		SkipDomainOwnershipCheck: false,
	})
	if err != nil {
		return false, oops.E(oops.CodeUnexpected, err, "check mcp endpoint slug availability").LogError(ctx, logger)
	}

	return available, nil
}

func (s *Service) DeleteMcpEndpoint(ctx context.Context, payload *gen.DeleteMcpEndpointPayload) error {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return oops.C(oops.CodeUnauthorized)
	}

	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeMCPWrite, ResourceKind: "", ResourceID: authCtx.ProjectID.String(), Dimensions: nil}); err != nil {
		return err
	}

	logger := s.logger.With(attr.SlogProjectID(authCtx.ProjectID.String()))

	endpointID, err := uuid.Parse(payload.ID)
	if err != nil {
		return oops.E(oops.CodeBadRequest, err, "invalid mcp endpoint id").LogError(ctx, logger)
	}

	preexisting, err := repo.New(s.db).GetMCPEndpointByID(ctx, repo.GetMCPEndpointByIDParams{
		ID:        endpointID,
		ProjectID: *authCtx.ProjectID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return oops.E(oops.CodeNotFound, err, "mcp endpoint not found").LogError(ctx, logger)
		}
		return oops.E(oops.CodeUnexpected, err, "get mcp endpoint").LogError(ctx, logger)
	}

	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "begin transaction").LogError(ctx, logger)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })

	txRepo := repo.New(dbtx)

	backing, err := lockBackingToolsets(ctx, dbtx, *authCtx.ProjectID, UniqueIDs(preexisting.McpServerID))
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "lock backing toolsets").LogError(ctx, logger)
	}

	if err := lockCustomDomains(ctx, dbtx, UniqueIDs(preexisting.CustomDomainID)); err != nil {
		return oops.E(oops.CodeUnexpected, err, "lock custom domain").LogError(ctx, logger)
	}
	existing, err := txRepo.LockMCPEndpointByID(ctx, repo.LockMCPEndpointByIDParams{
		ID:        endpointID,
		ProjectID: *authCtx.ProjectID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return oops.E(oops.CodeNotFound, err, "mcp endpoint not found").LogError(ctx, logger)
		}
		return oops.E(oops.CodeUnexpected, err, "lock mcp endpoint").LogError(ctx, logger)
	}
	if existing.CustomDomainID != preexisting.CustomDomainID {
		return oops.E(oops.CodeConflict, nil, "mcp endpoint changed concurrently; retry the request").LogError(ctx, logger)
	}
	if existing.McpServerID.Valid {
		lockedServer, err := mcpserversrepo.New(dbtx).LockMCPServerByIDAndProjectID(ctx, mcpserversrepo.LockMCPServerByIDAndProjectIDParams{
			ID:        existing.McpServerID.UUID,
			ProjectID: *authCtx.ProjectID,
		})
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return oops.E(oops.CodeUnexpected, err, "lock mcp server").LogError(ctx, logger)
		}
		if err == nil {
			if err := verifyBacking(ctx, logger, lockedServer, backing); err != nil {
				return err
			}
		}
	}

	deleted, err := txRepo.DeleteMCPEndpoint(ctx, repo.DeleteMCPEndpointParams{
		ID:        endpointID,
		ProjectID: *authCtx.ProjectID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return oops.E(oops.CodeNotFound, err, "mcp endpoint not found").LogError(ctx, logger)
		}
		return oops.E(oops.CodeUnexpected, err, "delete mcp endpoint").LogError(ctx, logger)
	}

	if err := s.audit.LogMcpEndpointDelete(ctx, dbtx, audit.LogMcpEndpointDeleteEvent{
		OrganizationID:   authCtx.ActiveOrganizationID,
		ProjectID:        *authCtx.ProjectID,
		Actor:            urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID),
		ActorDisplayName: authCtx.Email,
		ActorSlug:        nil,
		McpEndpointURN:   urn.NewMcpEndpoint(deleted.ID),
		Slug:             deleted.Slug,
	}); err != nil {
		return oops.E(oops.CodeUnexpected, err, "log mcp endpoint deletion").LogError(ctx, logger)
	}

	wasRoot := existing.IsDomainRoot.Valid && existing.IsDomainRoot.Bool
	if wasRoot {
		if err := s.logRootAutoClear(ctx, dbtx, authCtx, existing.CustomDomainID.UUID, existing.ID); err != nil {
			return err
		}
	}

	if existing.McpServerID.Valid {
		if toolsetID, ok := backing[existing.McpServerID.UUID]; ok {
			if err := s.syncBackingToolsetAddress(ctx, dbtx, authCtx, existing.McpServerID.UUID, toolsetID); err != nil {
				return oops.E(oops.CodeUnexpected, err, "mirror endpoint onto toolset").LogError(ctx, logger)
			}
		}
	}

	if err := dbtx.Commit(ctx); err != nil {
		return oops.E(oops.CodeUnexpected, err, "commit transaction").LogError(ctx, logger)
	}

	if wasRoot {
		if err := s.reconcileCustomDomains(ctx, []uuid.UUID{existing.CustomDomainID.UUID}); err != nil {
			return err
		}
	}

	return nil
}

// parseEndpointBackendIDs parses the two mutually exclusive backend id fields
// and enforces that exactly one is provided. Returned errors are fully formed
// oops errors, already logged; callers return them as-is.
func (s *Service) parseEndpointBackendIDs(ctx context.Context, logger *slog.Logger, mcpServerID *string, metaMcpServerID *string) (uuid.NullUUID, uuid.NullUUID, error) {
	serverID, err := conv.PtrToNullUUID(mcpServerID)
	if err != nil {
		return uuid.NullUUID{}, uuid.NullUUID{}, oops.E(oops.CodeBadRequest, err, "invalid mcp_server_id").LogError(ctx, logger)
	}

	metaID, err := conv.PtrToNullUUID(metaMcpServerID)
	if err != nil {
		return uuid.NullUUID{}, uuid.NullUUID{}, oops.E(oops.CodeBadRequest, err, "invalid meta_mcp_server_id").LogError(ctx, logger)
	}

	if serverID.Valid == metaID.Valid {
		return uuid.NullUUID{}, uuid.NullUUID{}, oops.E(oops.CodeInvalid, nil, "provide exactly one of mcp_server_id or meta_mcp_server_id").LogError(ctx, logger)
	}

	return serverID, metaID, nil
}

// lockMetaMcpServers locks the given meta MCP server rows in sorted order. A
// row that no longer exists is only an error when it is the endpoint's target
// backend; a vanished previous backend just has nothing left to lock.
func (s *Service) lockMetaMcpServers(ctx context.Context, dbtx pgx.Tx, authCtx *contextvalues.AuthContext, metaIDs []uuid.UUID, target uuid.NullUUID) error {
	metaRepo := metamcprepo.New(dbtx)
	for _, metaID := range metaIDs {
		if _, err := metaRepo.LockMetaMCPServer(ctx, metamcprepo.LockMetaMCPServerParams{
			ID:             metaID,
			OrganizationID: authCtx.ActiveOrganizationID,
			ProjectID:      *authCtx.ProjectID,
		}); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				if target.Valid && target.UUID == metaID {
					return oops.E(oops.CodeInvalid, err, "meta_mcp_server_id does not reference a resource in this project").LogError(ctx, s.logger)
				}
				continue
			}
			return oops.E(oops.CodeUnexpected, err, "lock meta mcp server").LogError(ctx, s.logger)
		}
	}
	return nil
}

func UniqueIDs(ids ...uuid.NullUUID) []uuid.UUID {
	seen := make(map[uuid.UUID]struct{}, len(ids))
	result := make([]uuid.UUID, 0, len(ids))
	for _, id := range ids {
		if !id.Valid {
			continue
		}
		if _, ok := seen[id.UUID]; ok {
			continue
		}
		seen[id.UUID] = struct{}{}
		result = append(result, id.UUID)
	}
	slices.SortFunc(result, func(a, b uuid.UUID) int {
		return strings.Compare(a.String(), b.String())
	})
	return result
}

func lockCustomDomains(ctx context.Context, dbtx pgx.Tx, domainIDs []uuid.UUID) error {
	repository := customdomainsrepo.New(dbtx)
	for _, domainID := range domainIDs {
		if _, err := repository.LockCustomDomainByID(ctx, domainID); err != nil {
			return fmt.Errorf("lock custom domain %s: %w", domainID, err)
		}
	}
	return nil
}

func (s *Service) logRootAutoClear(
	ctx context.Context,
	dbtx pgx.Tx,
	authCtx *contextvalues.AuthContext,
	customDomainID uuid.UUID,
	rootEndpointID uuid.UUID,
) error {
	domain, err := customdomainsrepo.New(dbtx).GetCustomDomainByID(ctx, customDomainID)
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
		CustomDomainSnapshotBefore: mv.BuildCustomDomainView(domain, false, rootEndpointID),
		CustomDomainSnapshotAfter:  mv.BuildCustomDomainView(domain, false, uuid.Nil),
	}); err != nil {
		return oops.E(oops.CodeUnexpected, err, "log automatic root endpoint cleanup").LogError(ctx, s.logger)
	}
	return nil
}

func (s *Service) reconcileCustomDomains(ctx context.Context, customDomainIDs []uuid.UUID) error {
	if err := background.ReconcileCustomDomains(ctx, s.logger, s.temporalEnv, customDomainIDs); err != nil {
		return fmt.Errorf("reconcile custom domains: %w", err)
	}
	return nil
}

// validateSlugPrefix enforces that slugs not bound to a custom domain must be
// prefixed with the organization slug followed by a hyphen.
func validateSlugPrefix(slug string, customDomainID uuid.NullUUID, organizationSlug string) error {
	if customDomainID.Valid {
		return nil
	}

	if !strings.HasPrefix(slug, organizationSlug+"-") {
		return fmt.Errorf("mcp endpoint slug must be prefixed with the organization slug %q", organizationSlug)
	}

	return nil
}

// verifyEndpointReferenceOwnership checks that the referenced backend (an
// mcp_server or a meta_mcp_server) lives in the caller's project and that the
// optional custom_domain is registered to the caller's organization. The raw
// FK constraints only enforce existence, not tenancy, so this closes a
// cross-tenant leak.
//
// Each check delegates to the owning package's scoped Get*ByID query and
// treats sql.ErrNoRows as "not in this project/organization".
func verifyEndpointReferenceOwnership(
	ctx context.Context,
	dbtx pgx.Tx,
	projectID uuid.UUID,
	organizationID string,
	mcpServerID uuid.NullUUID,
	metaMcpServerID uuid.NullUUID,
	customDomainID uuid.NullUUID,
) error {
	if mcpServerID.Valid {
		if _, err := mcpserversrepo.New(dbtx).GetMCPServerByIDAndProjectID(ctx, mcpserversrepo.GetMCPServerByIDAndProjectIDParams{
			ID:        mcpServerID.UUID,
			ProjectID: projectID,
		}); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return fmt.Errorf("mcp_server_id does not reference a resource in this project")
			}
			return fmt.Errorf("check mcp server ownership: %w", err)
		}
	}

	if metaMcpServerID.Valid {
		if _, err := metamcprepo.New(dbtx).GetMetaMCPServer(ctx, metamcprepo.GetMetaMCPServerParams{
			ID:             metaMcpServerID.UUID,
			OrganizationID: organizationID,
			ProjectID:      projectID,
		}); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return fmt.Errorf("meta_mcp_server_id does not reference a resource in this project")
			}
			return fmt.Errorf("check meta mcp server ownership: %w", err)
		}
	}

	if !customDomainID.Valid {
		return nil
	}

	if _, err := customdomainsrepo.New(dbtx).GetCustomDomainByIDAndOrganization(ctx, customdomainsrepo.GetCustomDomainByIDAndOrganizationParams{
		ID:             customDomainID.UUID,
		OrganizationID: organizationID,
	}); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("custom_domain_id does not reference a resource in this organization")
		}
		return fmt.Errorf("check custom domain ownership: %w", err)
	}

	return nil
}
