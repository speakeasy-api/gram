package remotemcp

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgerrcode"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/otel/trace"
	goahttp "goa.design/goa/v3/http"
	"goa.design/goa/v3/security"

	srv "github.com/speakeasy-api/gram/server/gen/http/remote_mcp/server"
	gen "github.com/speakeasy-api/gram/server/gen/remote_mcp"
	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/auth"
	"github.com/speakeasy-api/gram/server/internal/auth/sessions"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/encryption"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/middleware"
	"github.com/speakeasy-api/gram/server/internal/mv"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/remotemcp/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

type Service struct {
	tracer  trace.Tracer
	logger  *slog.Logger
	db      *pgxpool.Pool
	auth    *auth.Auth
	authz   *authz.Engine
	headers *Headers
	policy  *guardian.Policy
	audit   *audit.Logger
}

var _ gen.Service = (*Service)(nil)
var _ gen.Auther = (*Service)(nil)

func NewService(
	logger *slog.Logger,
	tracerProvider trace.TracerProvider,
	db *pgxpool.Pool,
	sessions *sessions.Manager,
	enc *encryption.Client,
	authzEngine *authz.Engine,
	policy *guardian.Policy,
	auditLogger *audit.Logger,
) *Service {
	logger = logger.With(attr.SlogComponent("remotemcp"))

	return &Service{
		tracer:  tracerProvider.Tracer("github.com/speakeasy-api/gram/server/internal/remotemcp"),
		logger:  logger,
		db:      db,
		auth:    auth.New(logger, db, sessions, authzEngine),
		authz:   authzEngine,
		headers: NewHeaders(logger, db, enc),
		policy:  policy,
		audit:   auditLogger,
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

func (s *Service) CreateServer(ctx context.Context, payload *gen.CreateServerPayload) (*types.RemoteMcpServer, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeMCPWrite, ResourceKind: "", ResourceID: authCtx.ProjectID.String(), Dimensions: nil}); err != nil {
		return nil, err
	}

	logger := s.logger.With(attr.SlogProjectID(authCtx.ProjectID.String()))

	if err := validateURL(ctx, s.policy, payload.URL); err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid url").LogError(ctx, logger)
	}

	// Generate the server ID up front so the slug can include its suffix and
	// the row can be inserted in a single statement (no insert-then-update).
	serverID, err := uuid.NewV7()
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "generate server id").LogError(ctx, logger)
	}

	slug, err := computeServerSlug(payload.URL, serverID)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "compute server slug").LogError(ctx, logger)
	}

	name := pgtype.Text{String: "", Valid: false}
	if payload.Name != nil {
		if trimmed := strings.TrimSpace(*payload.Name); trimmed != "" {
			name = pgtype.Text{String: trimmed, Valid: true}
		}
	}

	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "begin transaction").LogError(ctx, logger)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })

	txRepo := repo.New(dbtx)

	server, err := txRepo.CreateServer(ctx, repo.CreateServerParams{
		ID:            serverID,
		ProjectID:     *authCtx.ProjectID,
		Name:          name,
		Slug:          conv.ToPGText(slug),
		TransportType: payload.TransportType,
		Url:           payload.URL,
	})
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == pgerrcode.UniqueViolation {
			return nil, oops.E(oops.CodeConflict, err, "remote mcp server slug already in use").LogError(ctx, logger)
		}
		return nil, oops.E(oops.CodeUnexpected, err, "create remote mcp server").LogError(ctx, logger)
	}

	if err := s.audit.LogRemoteMcpServerCreate(ctx, dbtx, audit.LogRemoteMcpServerCreateEvent{
		OrganizationID:     authCtx.ActiveOrganizationID,
		ProjectID:          *authCtx.ProjectID,
		Actor:              urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID),
		ActorDisplayName:   authCtx.Email,
		ActorSlug:          nil,
		RemoteMcpServerURN: urn.NewRemoteMcpServer(server.ID),
		RemoteMcpServerURL: server.Url,
	}); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "log remote mcp server creation").LogError(ctx, logger)
	}

	if err := dbtx.Commit(ctx); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "commit transaction").LogError(ctx, logger)
	}

	return mv.BuildRemoteMcpServerView(server), nil
}

func (s *Service) ListServers(ctx context.Context, payload *gen.ListServersPayload) (*gen.ListServersResult, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeMCPRead, ResourceKind: "", ResourceID: authCtx.ProjectID.String(), Dimensions: nil}); err != nil {
		return nil, err
	}

	servers, err := repo.New(s.db).ListServersByProjectID(ctx, *authCtx.ProjectID)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list remote mcp servers").LogError(ctx, s.logger)
	}

	result := make([]*types.RemoteMcpServer, 0, len(servers))
	for _, server := range servers {
		result = append(result, mv.BuildRemoteMcpServerView(server))
	}

	return &gen.ListServersResult{RemoteMcpServers: result}, nil
}

func (s *Service) GetServer(ctx context.Context, payload *gen.GetServerPayload) (*types.RemoteMcpServer, error) {
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

	var server repo.RemoteMcpServer
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
			return nil, oops.E(oops.CodeNotFound, err, "remote mcp server not found").LogError(ctx, s.logger)
		}

		return nil, oops.E(oops.CodeUnexpected, err, "get remote mcp server").LogError(ctx, s.logger)
	}

	return mv.BuildRemoteMcpServerView(server), nil
}

func (s *Service) UpdateServer(ctx context.Context, payload *gen.UpdateServerPayload) (*types.RemoteMcpServer, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeMCPWrite, ResourceKind: "", ResourceID: authCtx.ProjectID.String(), Dimensions: nil}); err != nil {
		return nil, err
	}

	logger := s.logger.With(attr.SlogProjectID(authCtx.ProjectID.String()))

	serverID, err := uuid.Parse(payload.ID)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid server id").LogError(ctx, logger)
	}

	if payload.URL != nil {
		if err := validateURL(ctx, s.policy, *payload.URL); err != nil {
			return nil, oops.E(oops.CodeBadRequest, err, "invalid url").LogError(ctx, logger)
		}
	}

	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "begin transaction").LogError(ctx, logger)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })

	txRepo := repo.New(dbtx)

	// Fetch current state for before-snapshot
	existingServer, err := txRepo.GetServerByID(ctx, repo.GetServerByIDParams{
		ID:        serverID,
		ProjectID: *authCtx.ProjectID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, oops.E(oops.CodeNotFound, err, "remote mcp server not found").LogError(ctx, logger)
		}

		return nil, oops.E(oops.CodeUnexpected, err, "get remote mcp server").LogError(ctx, logger)
	}

	beforeView := mv.BuildRemoteMcpServerView(existingServer)

	// Resolve name: nil = leave existing, "" = clear, value = trimmed value.
	name := existingServer.Name
	if payload.Name != nil {
		trimmed := strings.TrimSpace(*payload.Name)
		name = pgtype.Text{String: trimmed, Valid: trimmed != ""}
	}

	// Always recompute slug from the post-update URL so it tracks the URL
	// even when the URL didn't change (idempotent).
	finalURL := conv.PtrValOr(payload.URL, existingServer.Url)
	slug, err := computeServerSlug(finalURL, existingServer.ID)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "compute server slug").LogError(ctx, logger)
	}

	updatedServer, err := txRepo.UpdateServer(ctx, repo.UpdateServerParams{
		ID:            serverID,
		ProjectID:     *authCtx.ProjectID,
		Name:          name,
		Slug:          conv.ToPGText(slug),
		TransportType: conv.PtrValOr(payload.TransportType, existingServer.TransportType),
		Url:           finalURL,
	})
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == pgerrcode.UniqueViolation {
			return nil, oops.E(oops.CodeConflict, err, "remote mcp server slug already in use").LogError(ctx, logger)
		}
		return nil, oops.E(oops.CodeUnexpected, err, "update remote mcp server").LogError(ctx, logger)
	}

	afterView := mv.BuildRemoteMcpServerView(updatedServer)

	if err := s.audit.LogRemoteMcpServerUpdate(ctx, dbtx, audit.LogRemoteMcpServerUpdateEvent{
		OrganizationID:     authCtx.ActiveOrganizationID,
		ProjectID:          *authCtx.ProjectID,
		Actor:              urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID),
		ActorDisplayName:   authCtx.Email,
		ActorSlug:          nil,
		RemoteMcpServerURN: urn.NewRemoteMcpServer(updatedServer.ID),
		RemoteMcpServerURL: updatedServer.Url,
		SnapshotBefore:     beforeView,
		SnapshotAfter:      afterView,
	}); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "log remote mcp server update").LogError(ctx, logger)
	}

	if err := dbtx.Commit(ctx); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "commit transaction").LogError(ctx, logger)
	}

	return afterView, nil
}

func (s *Service) VerifyURL(ctx context.Context, payload *gen.VerifyURLPayload) (*gen.VerifyURLResult, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeMCPWrite, ResourceKind: "", ResourceID: authCtx.ProjectID.String(), Dimensions: nil}); err != nil {
		return nil, err
	}

	logger := s.logger.With(attr.SlogProjectID(authCtx.ProjectID.String()))

	if err := validateURL(ctx, s.policy, payload.URL); err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid url").LogError(ctx, logger)
	}

	probeCtx, cancel := context.WithTimeout(ctx, verifyURLTimeout)
	defer cancel()

	verified, status, message := VerifyRemoteMcpURL(probeCtx, s.policy, payload.URL)

	return &gen.VerifyURLResult{
		Verified:   verified,
		HTTPStatus: status,
		Message:    message,
	}, nil
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

	// The FK's ON DELETE CASCADE only fires for hard deletes, so soft-delete the
	// headers explicitly. This runs before the parent row is tombstoned so the
	// query's project subselect can still see it.
	if err := txRepo.DeleteHeadersByServerID(ctx, repo.DeleteHeadersByServerIDParams{
		RemoteMcpServerID: serverID,
		ProjectID:         *authCtx.ProjectID,
	}); err != nil {
		return oops.E(oops.CodeUnexpected, err, "delete remote mcp server headers").LogError(ctx, logger)
	}

	deleted, err := txRepo.DeleteServer(ctx, repo.DeleteServerParams{
		ID:        serverID,
		ProjectID: *authCtx.ProjectID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}

		return oops.E(oops.CodeUnexpected, err, "delete remote mcp server").LogError(ctx, logger)
	}

	// Deliberately no per-header audit event for the cascade. The parent's
	// remote-mcp:delete entry already accounts for the headers going away, and
	// one entry per header would bury that signal under detail nobody asked for.
	// Headers removed on their own still emit remote-mcp-server-header:delete.
	if err := s.audit.LogRemoteMcpServerDelete(ctx, dbtx, audit.LogRemoteMcpServerDeleteEvent{
		OrganizationID:     authCtx.ActiveOrganizationID,
		ProjectID:          *authCtx.ProjectID,
		Actor:              urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID),
		ActorDisplayName:   authCtx.Email,
		ActorSlug:          nil,
		RemoteMcpServerURN: urn.NewRemoteMcpServer(deleted.ID),
		RemoteMcpServerURL: deleted.Url,
	}); err != nil {
		return oops.E(oops.CodeUnexpected, err, "log remote mcp server deletion").LogError(ctx, logger)
	}

	if err := dbtx.Commit(ctx); err != nil {
		return oops.E(oops.CodeUnexpected, err, "commit transaction").LogError(ctx, logger)
	}

	return nil
}

func (s *Service) ListServerHeaders(ctx context.Context, payload *gen.ListServerHeadersPayload) (*gen.ListServerHeadersResult, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeMCPRead, ResourceKind: "", ResourceID: authCtx.ProjectID.String(), Dimensions: nil}); err != nil {
		return nil, err
	}

	serverID, err := uuid.Parse(payload.RemoteMcpServerID)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid server id").LogError(ctx, s.logger)
	}

	// Resolve the parent first so an unknown server, or one owned by another
	// project, is a 404 rather than an indistinguishable empty list.
	if _, err := repo.New(s.db).GetServerByID(ctx, repo.GetServerByIDParams{
		ID:        serverID,
		ProjectID: *authCtx.ProjectID,
	}); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, oops.E(oops.CodeNotFound, err, "remote mcp server not found").LogError(ctx, s.logger)
		}

		return nil, oops.E(oops.CodeUnexpected, err, "get remote mcp server").LogError(ctx, s.logger)
	}

	headers, err := s.headers.ListServerHeaders(ctx, serverID, *authCtx.ProjectID, true)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list remote mcp server headers").LogError(ctx, s.logger)
	}

	return &gen.ListServerHeadersResult{Headers: mv.BuildRemoteMcpServerHeaderListView(headers)}, nil
}

func (s *Service) GetServerHeader(ctx context.Context, payload *gen.GetServerHeaderPayload) (*types.RemoteMcpServerHeader, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeMCPRead, ResourceKind: "", ResourceID: authCtx.ProjectID.String(), Dimensions: nil}); err != nil {
		return nil, err
	}

	headerID, err := uuid.Parse(payload.ID)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid header id").LogError(ctx, s.logger)
	}

	header, err := s.headers.GetServerHeader(ctx, headerID, *authCtx.ProjectID, true)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, oops.E(oops.CodeNotFound, err, "remote mcp server header not found").LogError(ctx, s.logger)
		}

		return nil, oops.E(oops.CodeUnexpected, err, "get remote mcp server header").LogError(ctx, s.logger)
	}

	return mv.BuildRemoteMcpServerHeaderView(header), nil
}

func (s *Service) CreateServerHeader(ctx context.Context, payload *gen.CreateServerHeaderPayload) (*types.RemoteMcpServerHeader, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeMCPWrite, ResourceKind: "", ResourceID: authCtx.ProjectID.String(), Dimensions: nil}); err != nil {
		return nil, err
	}

	logger := s.logger.With(attr.SlogProjectID(authCtx.ProjectID.String()))

	serverID, err := uuid.Parse(payload.RemoteMcpServerID)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid server id").LogError(ctx, logger)
	}

	isSecret := conv.PtrValOr(payload.IsSecret, false)
	if err := validateHeaderValueSource(payload.Name, payload.Value, payload.ValueFromRequestHeader, isSecret); err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid header").LogError(ctx, logger)
	}

	hasValue := payload.Value != nil && *payload.Value != ""
	hasValueFromRequestHeader := payload.ValueFromRequestHeader != nil && *payload.ValueFromRequestHeader != ""
	isEnvSourced := !isSecret && !hasValue && !hasValueFromRequestHeader

	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "begin transaction").LogError(ctx, logger)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })

	txRepo := repo.New(dbtx)
	headersRepo := NewHeaders(s.logger, dbtx, s.headers.enc)

	// Resolve the parent up front so a missing server is a 404 rather than an
	// empty insert, and so the audit event can carry the server's URL.
	server, err := txRepo.GetServerByID(ctx, repo.GetServerByIDParams{
		ID:        serverID,
		ProjectID: *authCtx.ProjectID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, oops.E(oops.CodeNotFound, err, "remote mcp server not found").LogError(ctx, logger)
		}

		return nil, oops.E(oops.CodeUnexpected, err, "get remote mcp server").LogError(ctx, logger)
	}

	header, err := headersRepo.CreateServerHeader(ctx, repo.CreateServerHeaderParams{
		RemoteMcpServerID:      server.ID,
		ProjectID:              *authCtx.ProjectID,
		Name:                   payload.Name,
		Description:            conv.PtrToPGText(payload.Description),
		IsRequired:             conv.PtrValOr(payload.IsRequired, false),
		IsSecret:               isSecret,
		Value:                  headerValuePGText(payload.Value, isEnvSourced),
		ValueFromRequestHeader: conv.PtrToPGTextEmpty(payload.ValueFromRequestHeader),
	})
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == pgerrcode.UniqueViolation {
			return nil, oops.E(oops.CodeConflict, err, "header name already in use on this remote mcp server").LogError(ctx, logger)
		}

		return nil, oops.E(oops.CodeUnexpected, err, "create remote mcp server header").LogError(ctx, logger)
	}

	if err := s.audit.LogRemoteMcpServerHeaderCreate(ctx, dbtx, audit.LogRemoteMcpServerHeaderCreateEvent{
		OrganizationID:            authCtx.ActiveOrganizationID,
		ProjectID:                 *authCtx.ProjectID,
		Actor:                     urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID),
		ActorDisplayName:          authCtx.Email,
		ActorSlug:                 nil,
		RemoteMcpServerHeaderURN:  urn.NewRemoteMcpServerHeader(header.ID),
		RemoteMcpServerHeaderName: header.Name,
		RemoteMcpServerURN:        urn.NewRemoteMcpServer(server.ID),
		RemoteMcpServerURL:        server.Url,
	}); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "log remote mcp server header creation").LogError(ctx, logger)
	}

	if err := dbtx.Commit(ctx); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "commit transaction").LogError(ctx, logger)
	}

	return mv.BuildRemoteMcpServerHeaderView(header), nil
}

func (s *Service) UpdateServerHeader(ctx context.Context, payload *gen.UpdateServerHeaderPayload) (*types.RemoteMcpServerHeader, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeMCPWrite, ResourceKind: "", ResourceID: authCtx.ProjectID.String(), Dimensions: nil}); err != nil {
		return nil, err
	}

	logger := s.logger.With(attr.SlogProjectID(authCtx.ProjectID.String()))

	headerID, err := uuid.Parse(payload.ID)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid header id").LogError(ctx, logger)
	}

	isSecret := conv.PtrValOr(payload.IsSecret, false)

	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "begin transaction").LogError(ctx, logger)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })

	txRepo := repo.New(dbtx)
	headersRepo := NewHeaders(s.logger, dbtx, s.headers.enc)

	existing, err := headersRepo.GetServerHeader(ctx, headerID, *authCtx.ProjectID, true)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, oops.E(oops.CodeNotFound, err, "remote mcp server header not found").LogError(ctx, logger)
		}

		return nil, oops.E(oops.CodeUnexpected, err, "get remote mcp server header").LogError(ctx, logger)
	}

	// Omitting value on a header that is already a secret preserves the stored
	// value. Everything else is a full replace of the mutable fields.
	hasValue := payload.Value != nil && *payload.Value != ""
	hasValueFromRequestHeader := payload.ValueFromRequestHeader != nil && *payload.ValueFromRequestHeader != ""
	preserveStoredValue := isSecret && !hasValue && !hasValueFromRequestHeader && existing.IsSecret && existing.Value.Valid

	if !preserveStoredValue {
		if err := validateHeaderValueSource(payload.Name, payload.Value, payload.ValueFromRequestHeader, isSecret); err != nil {
			return nil, oops.E(oops.CodeBadRequest, err, "invalid header").LogError(ctx, logger)
		}
	}

	server, err := txRepo.GetServerByID(ctx, repo.GetServerByIDParams{
		ID:        existing.RemoteMcpServerID,
		ProjectID: *authCtx.ProjectID,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "get remote mcp server").LogError(ctx, logger)
	}

	beforeView := mv.BuildRemoteMcpServerHeaderView(existing)

	isEnvSourced := !isSecret && !hasValue && !hasValueFromRequestHeader

	header, err := headersRepo.UpdateServerHeader(ctx, repo.UpdateServerHeaderParams{
		ID:                     headerID,
		ProjectID:              *authCtx.ProjectID,
		Name:                   payload.Name,
		Description:            conv.PtrToPGText(payload.Description),
		IsRequired:             conv.PtrValOr(payload.IsRequired, false),
		IsSecret:               isSecret,
		SetValue:               !preserveStoredValue,
		Value:                  headerValuePGText(payload.Value, isEnvSourced),
		ValueFromRequestHeader: conv.PtrToPGTextEmpty(payload.ValueFromRequestHeader),
	})
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == pgerrcode.UniqueViolation {
			return nil, oops.E(oops.CodeConflict, err, "header name already in use on this remote mcp server").LogError(ctx, logger)
		}

		return nil, oops.E(oops.CodeUnexpected, err, "update remote mcp server header").LogError(ctx, logger)
	}

	afterView := mv.BuildRemoteMcpServerHeaderView(header)

	if err := s.audit.LogRemoteMcpServerHeaderUpdate(ctx, dbtx, audit.LogRemoteMcpServerHeaderUpdateEvent{
		OrganizationID:                      authCtx.ActiveOrganizationID,
		ProjectID:                           *authCtx.ProjectID,
		Actor:                               urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID),
		ActorDisplayName:                    authCtx.Email,
		ActorSlug:                           nil,
		RemoteMcpServerHeaderURN:            urn.NewRemoteMcpServerHeader(header.ID),
		RemoteMcpServerHeaderName:           header.Name,
		RemoteMcpServerURN:                  urn.NewRemoteMcpServer(server.ID),
		RemoteMcpServerURL:                  server.Url,
		RemoteMcpServerHeaderSnapshotBefore: beforeView,
		RemoteMcpServerHeaderSnapshotAfter:  afterView,
	}); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "log remote mcp server header update").LogError(ctx, logger)
	}

	if err := dbtx.Commit(ctx); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "commit transaction").LogError(ctx, logger)
	}

	return afterView, nil
}

func (s *Service) DeleteServerHeader(ctx context.Context, payload *gen.DeleteServerHeaderPayload) error {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return oops.C(oops.CodeUnauthorized)
	}

	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeMCPWrite, ResourceKind: "", ResourceID: authCtx.ProjectID.String(), Dimensions: nil}); err != nil {
		return err
	}

	logger := s.logger.With(attr.SlogProjectID(authCtx.ProjectID.String()))

	headerID, err := uuid.Parse(payload.ID)
	if err != nil {
		return oops.E(oops.CodeBadRequest, err, "invalid header id").LogError(ctx, logger)
	}

	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "begin transaction").LogError(ctx, logger)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })

	txRepo := repo.New(dbtx)

	deleted, err := txRepo.DeleteServerHeader(ctx, repo.DeleteServerHeaderParams{
		ID:        headerID,
		ProjectID: *authCtx.ProjectID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}

		return oops.E(oops.CodeUnexpected, err, "delete remote mcp server header").LogError(ctx, logger)
	}

	server, err := txRepo.GetServerByID(ctx, repo.GetServerByIDParams{
		ID:        deleted.RemoteMcpServerID,
		ProjectID: *authCtx.ProjectID,
	})
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "get remote mcp server").LogError(ctx, logger)
	}

	if err := s.audit.LogRemoteMcpServerHeaderDelete(ctx, dbtx, audit.LogRemoteMcpServerHeaderDeleteEvent{
		OrganizationID:            authCtx.ActiveOrganizationID,
		ProjectID:                 *authCtx.ProjectID,
		Actor:                     urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID),
		ActorDisplayName:          authCtx.Email,
		ActorSlug:                 nil,
		RemoteMcpServerHeaderURN:  urn.NewRemoteMcpServerHeader(deleted.ID),
		RemoteMcpServerHeaderName: deleted.Name,
		RemoteMcpServerURN:        urn.NewRemoteMcpServer(server.ID),
		RemoteMcpServerURL:        server.Url,
	}); err != nil {
		return oops.E(oops.CodeUnexpected, err, "log remote mcp server header deletion").LogError(ctx, logger)
	}

	if err := dbtx.Commit(ctx); err != nil {
		return oops.E(oops.CodeUnexpected, err, "commit transaction").LogError(ctx, logger)
	}

	return nil
}

func (s *Service) APIKeyAuth(ctx context.Context, key string, schema *security.APIKeyScheme) (context.Context, error) {
	return s.auth.Authorize(ctx, key, schema)
}

// validateURL checks that the given URL string is a valid absolute HTTP(S) URL
// whose host is permitted by the supplied [guardian.Policy].
func validateURL(ctx context.Context, policy *guardian.Policy, rawURL string) error {
	u, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("parse url: %w", err)
	}

	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("url scheme must be http or https")
	}

	if u.Host == "" {
		return fmt.Errorf("url must include a host")
	}

	if err := policy.ValidateHost(ctx, u.Hostname()); err != nil {
		return fmt.Errorf("validate host: %w", err)
	}

	return nil
}

// headerValuePGText stores the static value for a header. Env-sourced headers
// persist value=” (non-null) so the DB CHECK holds and the serve path can
// detect the opt-in (ADR-0002).
//
// ponytail: name-matched env source; add value_from_environment_variable column if decoupling is ever asked for
func headerValuePGText(value *string, isEnvSourced bool) pgtype.Text {
	if isEnvSourced {
		return pgtype.Text{String: "", Valid: true}
	}

	return conv.PtrToPGTextEmpty(value)
}

// validateHeaderValueSource checks that exactly one of value or
// value_from_request_header is provided, mirroring the
// remote_mcp_server_headers_value_source_check constraint, and that a
// pass-through header is not marked secret. A non-secret header with neither
// source opts into the fronting server's attached environment (ADR-0002).
// Callers that want to preserve an existing secret's stored value skip this
// check entirely; see UpdateServerHeader.
func validateHeaderValueSource(name string, value *string, valueFromRequestHeader *string, isSecret bool) error {
	hasValue := value != nil && *value != ""
	hasValueFromRequestHeader := valueFromRequestHeader != nil && *valueFromRequestHeader != ""

	if !hasValue && !hasValueFromRequestHeader {
		if isSecret {
			return fmt.Errorf("header %q: a secret header must specify a value", name)
		}

		return nil // env-sourced
	}

	if hasValue && hasValueFromRequestHeader {
		return fmt.Errorf("header %q must specify exactly one of value or value_from_request_header", name)
	}

	if hasValueFromRequestHeader && isSecret {
		return fmt.Errorf("header %q: pass-through headers cannot be marked as secret", name)
	}

	return nil
}
