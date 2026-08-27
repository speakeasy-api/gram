package unproxiedmcp

import (
	"context"
	"errors"
	"log/slog"
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

	srv "github.com/speakeasy-api/gram/server/gen/http/unproxied_mcp/server"
	"github.com/speakeasy-api/gram/server/gen/types"
	gen "github.com/speakeasy-api/gram/server/gen/unproxied_mcp"
	"github.com/speakeasy-api/gram/server/internal/access"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/auth"
	"github.com/speakeasy-api/gram/server/internal/auth/sessions"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/externalmcp"
	externalmcptypes "github.com/speakeasy-api/gram/server/internal/externalmcp/repo/types"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/middleware"
	"github.com/speakeasy-api/gram/server/internal/mv"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/unproxiedmcp/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

type Service struct {
	tracer trace.Tracer
	logger *slog.Logger
	db     *pgxpool.Pool
	auth   *auth.Auth
	authz  *authz.Engine
	policy *guardian.Policy
	audit  *audit.Logger
}

var _ gen.Service = (*Service)(nil)
var _ gen.Auther = (*Service)(nil)

func NewService(
	logger *slog.Logger,
	tracerProvider trace.TracerProvider,
	db *pgxpool.Pool,
	sessions *sessions.Manager,
	authzEngine *authz.Engine,
	policy *guardian.Policy,
	auditLogger *audit.Logger,
) *Service {
	logger = logger.With(attr.SlogComponent("unproxiedmcp"))

	return &Service{
		tracer: tracerProvider.Tracer("github.com/speakeasy-api/gram/server/internal/unproxiedmcp"),
		logger: logger,
		db:     db,
		auth:   auth.New(logger, db, sessions, authzEngine),
		authz:  authzEngine,
		policy: policy,
		audit:  auditLogger,
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

func (s *Service) CreateServer(ctx context.Context, payload *gen.CreateServerPayload) (*types.UnproxiedMcpServer, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeMCPWrite, ResourceKind: "", ResourceID: authCtx.ProjectID.String(), Dimensions: nil}); err != nil {
		return nil, err
	}

	logger := s.logger.With(attr.SlogProjectID(authCtx.ProjectID.String()))

	if err := access.RequireStaffForUnproxiedMcp(ctx, authCtx, "added", logger); err != nil {
		return nil, err
	}

	if _, err := s.policy.ValidateHTTPURL(ctx, payload.URL); err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid server url").LogWarn(ctx, logger)
	}

	// Generate the server ID up front so the slug can include its suffix and
	// the row can be inserted in a single statement (no insert-then-update).
	serverID, err := uuid.NewV7()
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "generate server id").LogError(ctx, logger)
	}

	slug, err := conv.URLBackedSlug(payload.URL, serverID)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "compute server slug").LogError(ctx, logger)
	}

	name := pgtype.Text{String: "", Valid: false}
	if payload.Name != nil {
		if trimmed := strings.TrimSpace(*payload.Name); trimmed != "" {
			name = pgtype.Text{String: trimmed, Valid: true}
		}
	}

	description := pgtype.Text{String: "", Valid: false}
	if payload.Description != nil {
		if trimmed := strings.TrimSpace(*payload.Description); trimmed != "" {
			description = pgtype.Text{String: trimmed, Valid: true}
		}
	}

	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "begin transaction").LogError(ctx, logger)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })

	txRepo := repo.New(dbtx)

	server, err := txRepo.CreateServer(ctx, repo.CreateServerParams{
		ID:          serverID,
		ProjectID:   *authCtx.ProjectID,
		Name:        name,
		Slug:        conv.ToPGText(slug),
		Url:         payload.URL,
		Description: description,
	})
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == pgerrcode.UniqueViolation {
			return nil, oops.E(oops.CodeConflict, err, "unproxied mcp server slug already in use").LogError(ctx, logger)
		}
		return nil, oops.E(oops.CodeUnexpected, err, "create unproxied mcp server").LogError(ctx, logger)
	}

	if err := s.audit.LogUnproxiedMcpServerCreate(ctx, dbtx, audit.LogUnproxiedMcpServerCreateEvent{
		OrganizationID:        authCtx.ActiveOrganizationID,
		ProjectID:             *authCtx.ProjectID,
		Actor:                 urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID),
		ActorDisplayName:      authCtx.Email,
		ActorSlug:             nil,
		UnproxiedMcpServerURN: urn.NewUnproxiedMcpServer(server.ID),
		UnproxiedMcpServerURL: server.Url,
	}); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "log unproxied mcp server creation").LogError(ctx, logger)
	}

	if err := dbtx.Commit(ctx); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "commit transaction").LogError(ctx, logger)
	}

	return mv.BuildUnproxiedMcpServerView(server), nil
}

func (s *Service) ListServers(ctx context.Context, payload *gen.ListServersPayload) (*gen.ListUnproxiedMcpServersResult, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeMCPRead, ResourceKind: "", ResourceID: authCtx.ProjectID.String(), Dimensions: nil}); err != nil {
		return nil, err
	}

	servers, err := repo.New(s.db).ListServersByProjectID(ctx, *authCtx.ProjectID)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list unproxied mcp servers").LogError(ctx, s.logger)
	}

	result := make([]*types.UnproxiedMcpServer, 0, len(servers))
	for _, server := range servers {
		result = append(result, mv.BuildUnproxiedMcpServerView(server))
	}

	return &gen.ListUnproxiedMcpServersResult{UnproxiedMcpServers: result}, nil
}

func (s *Service) GetServer(ctx context.Context, payload *gen.GetServerPayload) (*types.UnproxiedMcpServer, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeMCPRead, ResourceKind: "", ResourceID: authCtx.ProjectID.String(), Dimensions: nil}); err != nil {
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

	dbRepo := repo.New(s.db)

	var server repo.UnproxiedMcpServer
	var err error
	if idProvided {
		serverID, parseErr := uuid.Parse(*payload.ID)
		if parseErr != nil {
			return nil, oops.E(oops.CodeBadRequest, parseErr, "invalid server id").LogError(ctx, s.logger)
		}
		server, err = dbRepo.GetServerByID(ctx, repo.GetServerByIDParams{
			ID:        serverID,
			ProjectID: *authCtx.ProjectID,
		})
	} else {
		server, err = dbRepo.GetServerBySlug(ctx, repo.GetServerBySlugParams{
			Slug:      conv.ToPGText(*payload.Slug),
			ProjectID: *authCtx.ProjectID,
		})
	}
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, oops.E(oops.CodeNotFound, err, "unproxied mcp server not found").LogError(ctx, s.logger)
		}

		return nil, oops.E(oops.CodeUnexpected, err, "get unproxied mcp server").LogError(ctx, s.logger)
	}

	return mv.BuildUnproxiedMcpServerView(server), nil
}

const (
	// listToolsTimeout bounds the live MCP handshake + tools/list round trip so
	// a slow or unresponsive vendor server can't hang the management API request.
	listToolsTimeout = 10 * time.Second
	// listToolsMaxResponseBytes bounds each untrusted initialize or tools/list
	// response; exceeding it is reported as unavailable probe evidence.
	listToolsMaxResponseBytes = 1 << 20
)

func (s *Service) ListTools(ctx context.Context, payload *gen.ListToolsPayload) (*gen.ListUnproxiedMcpServerToolsResult, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeMCPRead, ResourceKind: "", ResourceID: authCtx.ProjectID.String(), Dimensions: nil}); err != nil {
		return nil, err
	}

	serverID, err := uuid.Parse(payload.ID)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid server id").LogError(ctx, s.logger)
	}

	server, err := repo.New(s.db).GetServerByID(ctx, repo.GetServerByIDParams{
		ID:        serverID,
		ProjectID: *authCtx.ProjectID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, oops.E(oops.CodeNotFound, err, "unproxied mcp server not found").LogError(ctx, s.logger)
		}
		return nil, oops.E(oops.CodeUnexpected, err, "get unproxied mcp server").LogError(ctx, s.logger)
	}

	// Detached from ctx and raced against its own deadline below: the MCP
	// SDK's Connect can spend well past listToolsTimeout on its own
	// synchronous cleanup after a context deadline trips (e.g. trying to
	// notify the now-unreachable server that the request was cancelled), so
	// bounding the probe's own context isn't enough to bound *this call's*
	// latency. The goroutine keeps running to let that cleanup finish
	// naturally; only the response to the caller is time-boxed.
	probeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), listToolsTimeout) //nolint:gosec // cancel is deferred inside the goroutine below
	resultCh := make(chan *gen.ListUnproxiedMcpServerToolsResult, 1)
	go func() {
		defer cancel()
		resultCh <- s.probeListTools(probeCtx, server.Url)
	}()

	select {
	case result := <-resultCh:
		return result, nil
	case <-probeCtx.Done():
		// select picks pseudo-randomly when both cases are ready, so a probe
		// that finishes right at the deadline could otherwise lose the race
		// and have its real result discarded here. Give resultCh one more
		// non-blocking check before reporting a timeout.
		select {
		case result := <-resultCh:
			return result, nil
		default:
			return &gen.ListUnproxiedMcpServerToolsResult{
				Status:  "unreachable",
				Tools:   []*gen.UnproxiedMcpServerTool{},
				Message: conv.PtrEmpty("Could not connect to the server."),
			}, nil
		}
	}
}

// probeListTools performs the live MCP handshake and tools/list round trip.
// Always returns a populated result (auth_required/unreachable/error/success)
// rather than an error, since ListTools reports live-probe failures as a
// result status, not a request error.
func (s *Service) probeListTools(probeCtx context.Context, serverURL string) *gen.ListUnproxiedMcpServerToolsResult {
	// DisableRetries: this is a one-shot bounded probe, not a resilient
	// long-lived connection — retries would let an unreachable server take
	// minutes to report as such instead of ~10s.
	client, err := externalmcp.NewClient(probeCtx, s.logger, s.policy, serverURL, externalmcptypes.TransportTypeStreamableHTTP, &externalmcp.ClientOptions{
		Authorization:    "",
		Headers:          nil,
		DisableRetries:   true,
		MaxResponseBytes: listToolsMaxResponseBytes,
	})
	if err != nil {
		if _, ok := errors.AsType[*externalmcp.AuthRejectedError](err); ok {
			return &gen.ListUnproxiedMcpServerToolsResult{
				Status:  "auth_required",
				Tools:   []*gen.UnproxiedMcpServerTool{},
				Message: conv.PtrEmpty("This server requires authentication Gram doesn't manage."),
			}
		}
		return &gen.ListUnproxiedMcpServerToolsResult{
			Status:  "unreachable",
			Tools:   []*gen.UnproxiedMcpServerTool{},
			Message: conv.PtrEmpty("Could not connect to the server."),
		}
	}
	defer o11y.NoLogDefer(client.Close)

	discovered, err := client.ListTools(probeCtx)
	if err != nil {
		if _, ok := errors.AsType[*externalmcp.AuthRejectedError](err); ok {
			return &gen.ListUnproxiedMcpServerToolsResult{
				Status:  "auth_required",
				Tools:   []*gen.UnproxiedMcpServerTool{},
				Message: conv.PtrEmpty("This server requires authentication Gram doesn't manage."),
			}
		}
		return &gen.ListUnproxiedMcpServerToolsResult{
			Status:  "error",
			Tools:   []*gen.UnproxiedMcpServerTool{},
			Message: conv.PtrEmpty("The server didn't respond with a valid tool list."),
		}
	}

	tools := make([]*gen.UnproxiedMcpServerTool, 0, len(discovered))
	for _, tool := range discovered {
		tools = append(tools, &gen.UnproxiedMcpServerTool{
			Name:        tool.Name,
			Description: conv.PtrEmpty(tool.Description),
		})
	}

	return &gen.ListUnproxiedMcpServerToolsResult{
		Status:  "success",
		Tools:   tools,
		Message: nil,
	}
}

func (s *Service) DeleteServer(ctx context.Context, payload *gen.DeleteServerPayload) error {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return oops.C(oops.CodeUnauthorized)
	}

	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeMCPWrite, ResourceKind: "", ResourceID: authCtx.ProjectID.String(), Dimensions: nil}); err != nil {
		return err
	}

	logger := s.logger.With(attr.SlogProjectID(authCtx.ProjectID.String()))

	if err := access.RequireStaffForUnproxiedMcp(ctx, authCtx, "deleted", logger); err != nil {
		return err
	}

	serverID, err := uuid.Parse(payload.ID)
	if err != nil {
		return oops.E(oops.CodeBadRequest, err, "invalid server id").LogError(ctx, logger)
	}

	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "begin transaction").LogError(ctx, logger)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })

	txRepo := repo.New(dbtx)

	deleted, err := txRepo.DeleteServer(ctx, repo.DeleteServerParams{
		ID:        serverID,
		ProjectID: *authCtx.ProjectID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}

		return oops.E(oops.CodeUnexpected, err, "delete unproxied mcp server").LogError(ctx, logger)
	}

	if err := s.audit.LogUnproxiedMcpServerDelete(ctx, dbtx, audit.LogUnproxiedMcpServerDeleteEvent{
		OrganizationID:        authCtx.ActiveOrganizationID,
		ProjectID:             *authCtx.ProjectID,
		Actor:                 urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID),
		ActorDisplayName:      authCtx.Email,
		ActorSlug:             nil,
		UnproxiedMcpServerURN: urn.NewUnproxiedMcpServer(deleted.ID),
		UnproxiedMcpServerURL: deleted.Url,
	}); err != nil {
		return oops.E(oops.CodeUnexpected, err, "log unproxied mcp server deletion").LogError(ctx, logger)
	}

	if err := dbtx.Commit(ctx); err != nil {
		return oops.E(oops.CodeUnexpected, err, "commit transaction").LogError(ctx, logger)
	}

	return nil
}
