package mcpservers

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
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
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	environmentsrepo "github.com/speakeasy-api/gram/server/internal/environments/repo"
	mcpendpointsrepo "github.com/speakeasy-api/gram/server/internal/mcpendpoints/repo"
	"github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
	"github.com/speakeasy-api/gram/server/internal/middleware"
	"github.com/speakeasy-api/gram/server/internal/mv"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	oauthrepo "github.com/speakeasy-api/gram/server/internal/oauth/repo"
	"github.com/speakeasy-api/gram/server/internal/oops"
	remotemcprepo "github.com/speakeasy-api/gram/server/internal/remotemcp/repo"
	toolsetsrepo "github.com/speakeasy-api/gram/server/internal/toolsets/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

type Service struct {
	tracer trace.Tracer
	logger *slog.Logger
	db     *pgxpool.Pool
	auth   *auth.Auth
	authz  *authz.Engine
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
	auditLogger *audit.Logger,
) *Service {
	logger = logger.With(attr.SlogComponent("mcpservers"))

	return &Service{
		tracer: tracerProvider.Tracer("github.com/speakeasy-api/gram/server/internal/mcpservers"),
		logger: logger,
		db:     db,
		auth:   auth.New(logger, db, sessions, authzEngine),
		authz:  authzEngine,
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

func (s *Service) CreateMcpServer(ctx context.Context, payload *gen.CreateMcpServerPayload) (*types.McpServer, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeMCPWrite, ResourceKind: "", ResourceID: authCtx.ProjectID.String(), Dimensions: nil}); err != nil {
		return nil, err
	}

	logger := s.logger.With(attr.SlogProjectID(authCtx.ProjectID.String()))

	ids, err := parseServerIDs(
		payload.EnvironmentID,
		payload.ExternalOauthServerID,
		payload.OauthProxyServerID,
		payload.RemoteMcpServerID,
		payload.ToolsetID,
	)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid mcp server").Log(ctx, logger)
	}
	if err := validateServerBackendExclusivity(ids.RemoteMcpServerID, ids.ToolsetID); err != nil {
		return nil, oops.E(oops.CodeInvalid, err, "invalid mcp server").Log(ctx, logger)
	}
	if err := validateServerOAuthExclusivity(ids.ExternalOAuthServerID, ids.OAuthProxyServerID); err != nil {
		return nil, oops.E(oops.CodeInvalid, err, "invalid mcp server").Log(ctx, logger)
	}

	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "begin transaction").Log(ctx, logger)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })

	txRepo := repo.New(dbtx)

	if err := verifyServerReferenceOwnership(ctx, dbtx, *authCtx.ProjectID, ids); err != nil {
		return nil, oops.E(oops.CodeInvalid, err, "invalid mcp server").Log(ctx, logger)
	}

	server, err := txRepo.CreateMCPServer(ctx, repo.CreateMCPServerParams{
		ProjectID:             *authCtx.ProjectID,
		EnvironmentID:         ids.EnvironmentID,
		ExternalOauthServerID: ids.ExternalOAuthServerID,
		OauthProxyServerID:    ids.OAuthProxyServerID,
		RemoteMcpServerID:     ids.RemoteMcpServerID,
		ToolsetID:             ids.ToolsetID,
		Visibility:            string(payload.Visibility),
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "create mcp server").Log(ctx, logger)
	}

	if err := s.audit.LogMcpServerCreate(ctx, dbtx, audit.LogMcpServerCreateEvent{
		OrganizationID:   authCtx.ActiveOrganizationID,
		ProjectID:        *authCtx.ProjectID,
		Actor:            urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID),
		ActorDisplayName: authCtx.Email,
		ActorSlug:        nil,
		McpServerURN:     urn.NewMcpServer(server.ID),
	}); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "log mcp server creation").Log(ctx, logger)
	}

	if err := dbtx.Commit(ctx); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "commit transaction").Log(ctx, logger)
	}

	return mv.BuildMcpServerView(server), nil
}

func (s *Service) GetMcpServer(ctx context.Context, payload *gen.GetMcpServerPayload) (*types.McpServer, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeMCPRead, ResourceKind: "", ResourceID: authCtx.ProjectID.String(), Dimensions: nil}); err != nil {
		return nil, err
	}

	serverID, err := uuid.Parse(payload.ID)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid mcp server id").Log(ctx, s.logger)
	}

	server, err := repo.New(s.db).GetMCPServerByID(ctx, repo.GetMCPServerByIDParams{
		ID:        serverID,
		ProjectID: *authCtx.ProjectID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, oops.E(oops.CodeNotFound, err, "mcp server not found").Log(ctx, s.logger)
		}
		return nil, oops.E(oops.CodeUnexpected, err, "get mcp server").Log(ctx, s.logger)
	}

	return mv.BuildMcpServerView(server), nil
}

func (s *Service) ListMcpServers(ctx context.Context, payload *gen.ListMcpServersPayload) (*gen.ListMcpServersResult, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeMCPRead, ResourceKind: "", ResourceID: authCtx.ProjectID.String(), Dimensions: nil}); err != nil {
		return nil, err
	}

	servers, err := repo.New(s.db).ListMCPServersByProjectID(ctx, *authCtx.ProjectID)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list mcp servers").Log(ctx, s.logger)
	}

	return &gen.ListMcpServersResult{McpServers: mv.BuildMcpServerListView(servers)}, nil
}

func (s *Service) UpdateMcpServer(ctx context.Context, payload *gen.UpdateMcpServerPayload) (*types.McpServer, error) {
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
		return nil, oops.E(oops.CodeBadRequest, err, "invalid mcp server id").Log(ctx, logger)
	}

	ids, err := parseServerIDs(
		payload.EnvironmentID,
		payload.ExternalOauthServerID,
		payload.OauthProxyServerID,
		payload.RemoteMcpServerID,
		payload.ToolsetID,
	)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid mcp server").Log(ctx, logger)
	}
	if err := validateServerBackendExclusivity(ids.RemoteMcpServerID, ids.ToolsetID); err != nil {
		return nil, oops.E(oops.CodeInvalid, err, "invalid mcp server").Log(ctx, logger)
	}
	if err := validateServerOAuthExclusivity(ids.ExternalOAuthServerID, ids.OAuthProxyServerID); err != nil {
		return nil, oops.E(oops.CodeInvalid, err, "invalid mcp server").Log(ctx, logger)
	}

	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "begin transaction").Log(ctx, logger)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })

	txRepo := repo.New(dbtx)

	existing, err := txRepo.GetMCPServerByID(ctx, repo.GetMCPServerByIDParams{
		ID:        serverID,
		ProjectID: *authCtx.ProjectID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, oops.E(oops.CodeNotFound, err, "mcp server not found").Log(ctx, logger)
		}
		return nil, oops.E(oops.CodeUnexpected, err, "get mcp server").Log(ctx, logger)
	}

	beforeView := mv.BuildMcpServerView(existing)

	if err := verifyServerReferenceOwnership(ctx, dbtx, *authCtx.ProjectID, ids); err != nil {
		return nil, oops.E(oops.CodeInvalid, err, "invalid mcp server").Log(ctx, logger)
	}

	updated, err := txRepo.UpdateMCPServer(ctx, repo.UpdateMCPServerParams{
		EnvironmentID:         ids.EnvironmentID,
		ExternalOauthServerID: ids.ExternalOAuthServerID,
		OauthProxyServerID:    ids.OAuthProxyServerID,
		RemoteMcpServerID:     ids.RemoteMcpServerID,
		ToolsetID:             ids.ToolsetID,
		Visibility:            string(payload.Visibility),
		ID:                    serverID,
		ProjectID:             *authCtx.ProjectID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, oops.E(oops.CodeNotFound, err, "mcp server not found").Log(ctx, logger)
		}
		return nil, oops.E(oops.CodeUnexpected, err, "update mcp server").Log(ctx, logger)
	}

	afterView := mv.BuildMcpServerView(updated)

	if err := s.audit.LogMcpServerUpdate(ctx, dbtx, audit.LogMcpServerUpdateEvent{
		OrganizationID:          authCtx.ActiveOrganizationID,
		ProjectID:               *authCtx.ProjectID,
		Actor:                   urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID),
		ActorDisplayName:        authCtx.Email,
		ActorSlug:               nil,
		McpServerURN:            urn.NewMcpServer(updated.ID),
		McpServerSnapshotBefore: beforeView,
		McpServerSnapshotAfter:  afterView,
	}); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "log mcp server update").Log(ctx, logger)
	}

	if err := dbtx.Commit(ctx); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "commit transaction").Log(ctx, logger)
	}

	return afterView, nil
}

func (s *Service) DeleteMcpServer(ctx context.Context, payload *gen.DeleteMcpServerPayload) error {
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
		return oops.E(oops.CodeBadRequest, err, "invalid mcp server id").Log(ctx, logger)
	}

	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "begin transaction").Log(ctx, logger)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })

	txRepo := repo.New(dbtx)

	deleted, err := txRepo.DeleteMCPServer(ctx, repo.DeleteMCPServerParams{
		ID:        serverID,
		ProjectID: *authCtx.ProjectID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return oops.E(oops.CodeNotFound, err, "mcp server not found").Log(ctx, logger)
		}
		return oops.E(oops.CodeUnexpected, err, "delete mcp server").Log(ctx, logger)
	}

	// The mcp_endpoints.mcp_server_id FK has ON DELETE CASCADE, but that only
	// fires for hard deletes. Soft-delete endpoints explicitly so callers don't
	// resolve to a tombstoned mcp server after this commits.
	deletedEndpoints, err := mcpendpointsrepo.New(dbtx).SoftDeleteMCPEndpointsByMCPServerID(ctx, mcpendpointsrepo.SoftDeleteMCPEndpointsByMCPServerIDParams{
		McpServerID: deleted.ID,
		ProjectID:   *authCtx.ProjectID,
	})
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "delete child mcp endpoints").Log(ctx, logger)
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
			return oops.E(oops.CodeUnexpected, err, "log mcp endpoint deletion").Log(ctx, logger)
		}
	}

	if err := s.audit.LogMcpServerDelete(ctx, dbtx, audit.LogMcpServerDeleteEvent{
		OrganizationID:   authCtx.ActiveOrganizationID,
		ProjectID:        *authCtx.ProjectID,
		Actor:            urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID),
		ActorDisplayName: authCtx.Email,
		ActorSlug:        nil,
		McpServerURN:     urn.NewMcpServer(deleted.ID),
	}); err != nil {
		return oops.E(oops.CodeUnexpected, err, "log mcp server deletion").Log(ctx, logger)
	}

	if err := dbtx.Commit(ctx); err != nil {
		return oops.E(oops.CodeUnexpected, err, "commit transaction").Log(ctx, logger)
	}

	return nil
}

// serverIDs bundles the optional UUID references on the mcp_servers
// create/update payloads so they can be passed around without a long
// positional argument list.
type serverIDs struct {
	EnvironmentID         uuid.NullUUID
	ExternalOAuthServerID uuid.NullUUID
	OAuthProxyServerID    uuid.NullUUID
	RemoteMcpServerID     uuid.NullUUID
	ToolsetID             uuid.NullUUID
}

// parseServerIDs parses the five optional UUID payload fields into a
// serverIDs struct. Any malformed UUID surfaces with a field-specific error.
func parseServerIDs(
	environmentIDStr *string,
	externalOAuthServerIDStr *string,
	oauthProxyServerIDStr *string,
	remoteMcpServerIDStr *string,
	toolsetIDStr *string,
) (serverIDs, error) {
	var (
		ids serverIDs
		err error
	)

	if ids.EnvironmentID, err = conv.PtrToNullUUID(environmentIDStr); err != nil {
		return serverIDs{}, fmt.Errorf("invalid environment_id: %w", err)
	}
	if ids.ExternalOAuthServerID, err = conv.PtrToNullUUID(externalOAuthServerIDStr); err != nil {
		return serverIDs{}, fmt.Errorf("invalid external_oauth_server_id: %w", err)
	}
	if ids.OAuthProxyServerID, err = conv.PtrToNullUUID(oauthProxyServerIDStr); err != nil {
		return serverIDs{}, fmt.Errorf("invalid oauth_proxy_server_id: %w", err)
	}
	if ids.RemoteMcpServerID, err = conv.PtrToNullUUID(remoteMcpServerIDStr); err != nil {
		return serverIDs{}, fmt.Errorf("invalid remote_mcp_server_id: %w", err)
	}
	if ids.ToolsetID, err = conv.PtrToNullUUID(toolsetIDStr); err != nil {
		return serverIDs{}, fmt.Errorf("invalid toolset_id: %w", err)
	}

	return ids, nil
}

// validateServerBackendExclusivity enforces the mcp_servers DB check
// constraint that exactly one backend (remote MCP server XOR toolset) is set.
func validateServerBackendExclusivity(remoteMcpServerID, toolsetID uuid.NullUUID) error {
	if remoteMcpServerID.Valid == toolsetID.Valid {
		return fmt.Errorf("exactly one of remote_mcp_server_id or toolset_id must be provided")
	}
	return nil
}

// validateServerOAuthExclusivity enforces that at most one OAuth source
// (external or proxy) is configured for an mcp server. Both null is allowed.
func validateServerOAuthExclusivity(externalOAuthServerID, oauthProxyServerID uuid.NullUUID) error {
	if externalOAuthServerID.Valid && oauthProxyServerID.Valid {
		return fmt.Errorf("at most one of external_oauth_server_id or oauth_proxy_server_id may be provided")
	}
	return nil
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

	if ids.ExternalOAuthServerID.Valid {
		if _, err := oauthrepo.New(dbtx).GetExternalOAuthServerMetadata(ctx, oauthrepo.GetExternalOAuthServerMetadataParams{
			ProjectID: projectID,
			ID:        ids.ExternalOAuthServerID.UUID,
		}); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return fmt.Errorf("external_oauth_server_id does not reference a resource in this project")
			}
			return fmt.Errorf("check external oauth server ownership: %w", err)
		}
	}

	if ids.OAuthProxyServerID.Valid {
		if _, err := oauthrepo.New(dbtx).GetOAuthProxyServer(ctx, oauthrepo.GetOAuthProxyServerParams{
			ProjectID: projectID,
			ID:        ids.OAuthProxyServerID.UUID,
		}); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return fmt.Errorf("oauth_proxy_server_id does not reference a resource in this project")
			}
			return fmt.Errorf("check oauth proxy server ownership: %w", err)
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

	return nil
}
