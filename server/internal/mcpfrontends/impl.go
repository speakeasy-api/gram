package mcpfrontends

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/otel/trace"
	goahttp "goa.design/goa/v3/http"
	"goa.design/goa/v3/security"

	srv "github.com/speakeasy-api/gram/server/gen/http/mcp_frontends/server"
	gen "github.com/speakeasy-api/gram/server/gen/mcp_frontends"
	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/auth"
	"github.com/speakeasy-api/gram/server/internal/auth/sessions"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	environmentsrepo "github.com/speakeasy-api/gram/server/internal/environments/repo"
	"github.com/speakeasy-api/gram/server/internal/mcpfrontends/repo"
	mcpslugsrepo "github.com/speakeasy-api/gram/server/internal/mcpslugs/repo"
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
}

var _ gen.Service = (*Service)(nil)
var _ gen.Auther = (*Service)(nil)

func NewService(logger *slog.Logger, tracerProvider trace.TracerProvider, db *pgxpool.Pool, sessions *sessions.Manager, authzEngine *authz.Engine) *Service {
	logger = logger.With(attr.SlogComponent("mcpfrontends"))

	return &Service{
		tracer: tracerProvider.Tracer("github.com/speakeasy-api/gram/server/internal/mcpfrontends"),
		logger: logger,
		db:     db,
		auth:   auth.New(logger, db, sessions, authzEngine),
		authz:  authzEngine,
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

func (s *Service) CreateMcpFrontend(ctx context.Context, payload *gen.CreateMcpFrontendPayload) (*types.McpFrontend, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeMCPWrite, ResourceKind: "", ResourceID: authCtx.ProjectID.String(), Dimensions: nil}); err != nil {
		return nil, err
	}

	logger := s.logger.With(attr.SlogProjectID(authCtx.ProjectID.String()))

	ids, err := parseFrontendIDs(
		payload.EnvironmentID,
		payload.ExternalOauthServerID,
		payload.OauthProxyServerID,
		payload.RemoteMcpServerID,
		payload.ToolsetID,
	)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid mcp frontend").Log(ctx, logger)
	}
	if err := validateFrontendBackendExclusivity(ids.RemoteMcpServerID, ids.ToolsetID); err != nil {
		return nil, oops.E(oops.CodeInvalid, err, "invalid mcp frontend").Log(ctx, logger)
	}
	if err := validateFrontendOAuthExclusivity(ids.ExternalOAuthServerID, ids.OAuthProxyServerID); err != nil {
		return nil, oops.E(oops.CodeInvalid, err, "invalid mcp frontend").Log(ctx, logger)
	}

	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "begin transaction").Log(ctx, logger)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })

	txRepo := repo.New(dbtx)

	if err := verifyFrontendReferenceOwnership(ctx, dbtx, *authCtx.ProjectID, ids); err != nil {
		return nil, oops.E(oops.CodeInvalid, err, "invalid mcp frontend").Log(ctx, logger)
	}

	frontend, err := txRepo.CreateMCPFrontend(ctx, repo.CreateMCPFrontendParams{
		ProjectID:             *authCtx.ProjectID,
		EnvironmentID:         ids.EnvironmentID,
		ExternalOauthServerID: ids.ExternalOAuthServerID,
		OauthProxyServerID:    ids.OAuthProxyServerID,
		RemoteMcpServerID:     ids.RemoteMcpServerID,
		ToolsetID:             ids.ToolsetID,
		Visibility:            string(payload.Visibility),
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "create mcp frontend").Log(ctx, logger)
	}

	if err := audit.LogMcpFrontendCreate(ctx, dbtx, audit.LogMcpFrontendCreateEvent{
		OrganizationID:   authCtx.ActiveOrganizationID,
		ProjectID:        *authCtx.ProjectID,
		Actor:            urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID),
		ActorDisplayName: authCtx.Email,
		ActorSlug:        nil,
		McpFrontendID:    frontend.ID,
	}); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "log mcp frontend creation").Log(ctx, logger)
	}

	if err := dbtx.Commit(ctx); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "commit transaction").Log(ctx, logger)
	}

	return mv.BuildMcpFrontendView(frontend), nil
}

func (s *Service) GetMcpFrontend(ctx context.Context, payload *gen.GetMcpFrontendPayload) (*types.McpFrontend, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeMCPRead, ResourceKind: "", ResourceID: authCtx.ProjectID.String(), Dimensions: nil}); err != nil {
		return nil, err
	}

	frontendID, err := uuid.Parse(payload.ID)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid mcp frontend id").Log(ctx, s.logger)
	}

	frontend, err := repo.New(s.db).GetMCPFrontendByID(ctx, repo.GetMCPFrontendByIDParams{
		ID:        frontendID,
		ProjectID: *authCtx.ProjectID,
	})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, oops.E(oops.CodeNotFound, err, "mcp frontend not found").Log(ctx, s.logger)
		}
		return nil, oops.E(oops.CodeUnexpected, err, "get mcp frontend").Log(ctx, s.logger)
	}

	return mv.BuildMcpFrontendView(frontend), nil
}

func (s *Service) ListMcpFrontends(ctx context.Context, payload *gen.ListMcpFrontendsPayload) (*gen.ListMcpFrontendsResult, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeMCPRead, ResourceKind: "", ResourceID: authCtx.ProjectID.String(), Dimensions: nil}); err != nil {
		return nil, err
	}

	frontends, err := repo.New(s.db).ListMCPFrontendsByProjectID(ctx, *authCtx.ProjectID)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list mcp frontends").Log(ctx, s.logger)
	}

	return &gen.ListMcpFrontendsResult{McpFrontends: mv.BuildMcpFrontendListView(frontends)}, nil
}

func (s *Service) UpdateMcpFrontend(ctx context.Context, payload *gen.UpdateMcpFrontendPayload) (*types.McpFrontend, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeMCPWrite, ResourceKind: "", ResourceID: authCtx.ProjectID.String(), Dimensions: nil}); err != nil {
		return nil, err
	}

	logger := s.logger.With(attr.SlogProjectID(authCtx.ProjectID.String()))

	frontendID, err := uuid.Parse(payload.ID)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid mcp frontend id").Log(ctx, logger)
	}

	ids, err := parseFrontendIDs(
		payload.EnvironmentID,
		payload.ExternalOauthServerID,
		payload.OauthProxyServerID,
		payload.RemoteMcpServerID,
		payload.ToolsetID,
	)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid mcp frontend").Log(ctx, logger)
	}
	if err := validateFrontendBackendExclusivity(ids.RemoteMcpServerID, ids.ToolsetID); err != nil {
		return nil, oops.E(oops.CodeInvalid, err, "invalid mcp frontend").Log(ctx, logger)
	}
	if err := validateFrontendOAuthExclusivity(ids.ExternalOAuthServerID, ids.OAuthProxyServerID); err != nil {
		return nil, oops.E(oops.CodeInvalid, err, "invalid mcp frontend").Log(ctx, logger)
	}

	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "begin transaction").Log(ctx, logger)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })

	txRepo := repo.New(dbtx)

	existing, err := txRepo.GetMCPFrontendByID(ctx, repo.GetMCPFrontendByIDParams{
		ID:        frontendID,
		ProjectID: *authCtx.ProjectID,
	})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, oops.E(oops.CodeNotFound, err, "mcp frontend not found").Log(ctx, logger)
		}
		return nil, oops.E(oops.CodeUnexpected, err, "get mcp frontend").Log(ctx, logger)
	}

	beforeView := mv.BuildMcpFrontendView(existing)

	if err := verifyFrontendReferenceOwnership(ctx, dbtx, *authCtx.ProjectID, ids); err != nil {
		return nil, oops.E(oops.CodeInvalid, err, "invalid mcp frontend").Log(ctx, logger)
	}

	updated, err := txRepo.UpdateMCPFrontend(ctx, repo.UpdateMCPFrontendParams{
		EnvironmentID:         ids.EnvironmentID,
		ExternalOauthServerID: ids.ExternalOAuthServerID,
		OauthProxyServerID:    ids.OAuthProxyServerID,
		RemoteMcpServerID:     ids.RemoteMcpServerID,
		ToolsetID:             ids.ToolsetID,
		Visibility:            string(payload.Visibility),
		ID:                    frontendID,
		ProjectID:             *authCtx.ProjectID,
	})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, oops.E(oops.CodeNotFound, err, "mcp frontend not found").Log(ctx, logger)
		}
		return nil, oops.E(oops.CodeUnexpected, err, "update mcp frontend").Log(ctx, logger)
	}

	afterView := mv.BuildMcpFrontendView(updated)

	if err := audit.LogMcpFrontendUpdate(ctx, dbtx, audit.LogMcpFrontendUpdateEvent{
		OrganizationID:   authCtx.ActiveOrganizationID,
		ProjectID:        *authCtx.ProjectID,
		Actor:            urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID),
		ActorDisplayName: authCtx.Email,
		ActorSlug:        nil,
		McpFrontendID:    updated.ID,
		SnapshotBefore:   beforeView,
		SnapshotAfter:    afterView,
	}); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "log mcp frontend update").Log(ctx, logger)
	}

	if err := dbtx.Commit(ctx); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "commit transaction").Log(ctx, logger)
	}

	return afterView, nil
}

func (s *Service) DeleteMcpFrontend(ctx context.Context, payload *gen.DeleteMcpFrontendPayload) error {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return oops.C(oops.CodeUnauthorized)
	}

	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeMCPWrite, ResourceKind: "", ResourceID: authCtx.ProjectID.String(), Dimensions: nil}); err != nil {
		return err
	}

	logger := s.logger.With(attr.SlogProjectID(authCtx.ProjectID.String()))

	frontendID, err := uuid.Parse(payload.ID)
	if err != nil {
		return oops.E(oops.CodeBadRequest, err, "invalid mcp frontend id").Log(ctx, logger)
	}

	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "begin transaction").Log(ctx, logger)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })

	txRepo := repo.New(dbtx)

	deleted, err := txRepo.DeleteMCPFrontend(ctx, repo.DeleteMCPFrontendParams{
		ID:        frontendID,
		ProjectID: *authCtx.ProjectID,
	})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return oops.E(oops.CodeNotFound, err, "mcp frontend not found").Log(ctx, logger)
		}
		return oops.E(oops.CodeUnexpected, err, "delete mcp frontend").Log(ctx, logger)
	}

	// The mcp_slugs.mcp_frontend_id FK has ON DELETE CASCADE, but that only
	// fires for hard deletes. Soft-delete slugs explicitly so callers don't
	// resolve to a tombstoned frontend after this commits.
	if err := mcpslugsrepo.New(dbtx).SoftDeleteMCPSlugsByFrontendID(ctx, mcpslugsrepo.SoftDeleteMCPSlugsByFrontendIDParams{
		McpFrontendID: deleted.ID,
		ProjectID:     *authCtx.ProjectID,
	}); err != nil {
		return oops.E(oops.CodeUnexpected, err, "delete child mcp slugs").Log(ctx, logger)
	}

	if err := audit.LogMcpFrontendDelete(ctx, dbtx, audit.LogMcpFrontendDeleteEvent{
		OrganizationID:   authCtx.ActiveOrganizationID,
		ProjectID:        *authCtx.ProjectID,
		Actor:            urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID),
		ActorDisplayName: authCtx.Email,
		ActorSlug:        nil,
		McpFrontendID:    deleted.ID,
	}); err != nil {
		return oops.E(oops.CodeUnexpected, err, "log mcp frontend deletion").Log(ctx, logger)
	}

	if err := dbtx.Commit(ctx); err != nil {
		return oops.E(oops.CodeUnexpected, err, "commit transaction").Log(ctx, logger)
	}

	return nil
}

// frontendIDs bundles the optional UUID references on the mcp_frontends
// create/update payloads so they can be passed around without a long
// positional argument list.
type frontendIDs struct {
	EnvironmentID         uuid.NullUUID
	ExternalOAuthServerID uuid.NullUUID
	OAuthProxyServerID    uuid.NullUUID
	RemoteMcpServerID     uuid.NullUUID
	ToolsetID             uuid.NullUUID
}

// parseFrontendIDs parses the five optional UUID payload fields into a
// frontendIDs struct. Any malformed UUID surfaces with a field-specific error.
func parseFrontendIDs(
	environmentIDStr *string,
	externalOAuthServerIDStr *string,
	oauthProxyServerIDStr *string,
	remoteMcpServerIDStr *string,
	toolsetIDStr *string,
) (frontendIDs, error) {
	var (
		ids frontendIDs
		err error
	)

	if ids.EnvironmentID, err = conv.PtrToNullUUID(environmentIDStr); err != nil {
		return frontendIDs{}, fmt.Errorf("invalid environment_id: %w", err)
	}
	if ids.ExternalOAuthServerID, err = conv.PtrToNullUUID(externalOAuthServerIDStr); err != nil {
		return frontendIDs{}, fmt.Errorf("invalid external_oauth_server_id: %w", err)
	}
	if ids.OAuthProxyServerID, err = conv.PtrToNullUUID(oauthProxyServerIDStr); err != nil {
		return frontendIDs{}, fmt.Errorf("invalid oauth_proxy_server_id: %w", err)
	}
	if ids.RemoteMcpServerID, err = conv.PtrToNullUUID(remoteMcpServerIDStr); err != nil {
		return frontendIDs{}, fmt.Errorf("invalid remote_mcp_server_id: %w", err)
	}
	if ids.ToolsetID, err = conv.PtrToNullUUID(toolsetIDStr); err != nil {
		return frontendIDs{}, fmt.Errorf("invalid toolset_id: %w", err)
	}

	return ids, nil
}

// validateFrontendBackendExclusivity enforces the mcp_frontends DB check
// constraint that exactly one backend (remote MCP server XOR toolset) is set.
func validateFrontendBackendExclusivity(remoteMcpServerID, toolsetID uuid.NullUUID) error {
	if remoteMcpServerID.Valid == toolsetID.Valid {
		return fmt.Errorf("exactly one of remote_mcp_server_id or toolset_id must be provided")
	}
	return nil
}

// validateFrontendOAuthExclusivity enforces that at most one OAuth source
// (external or proxy) is configured for a frontend. Both null is allowed.
func validateFrontendOAuthExclusivity(externalOAuthServerID, oauthProxyServerID uuid.NullUUID) error {
	if externalOAuthServerID.Valid && oauthProxyServerID.Valid {
		return fmt.Errorf("at most one of external_oauth_server_id or oauth_proxy_server_id may be provided")
	}
	return nil
}

// verifyFrontendReferenceOwnership checks that every non-null referenced
// resource belongs to the caller's project. The raw FK constraints only
// enforce existence, not tenancy, so this closes a cross-project leak.
//
// Each check delegates to the owning package's project-scoped Get*ByID query
// and treats sql.ErrNoRows as "not in this project", matching the pattern used
// elsewhere in the codebase (e.g. toolsets -> environments).
func verifyFrontendReferenceOwnership(
	ctx context.Context,
	dbtx pgx.Tx,
	projectID uuid.UUID,
	ids frontendIDs,
) error {
	if ids.EnvironmentID.Valid {
		if _, err := environmentsrepo.New(dbtx).GetEnvironmentByID(ctx, environmentsrepo.GetEnvironmentByIDParams{
			ID:        ids.EnvironmentID.UUID,
			ProjectID: projectID,
		}); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
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
			if errors.Is(err, sql.ErrNoRows) {
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
			if errors.Is(err, sql.ErrNoRows) {
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
			if errors.Is(err, sql.ErrNoRows) {
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
			if errors.Is(err, sql.ErrNoRows) {
				return fmt.Errorf("toolset_id does not reference a resource in this project")
			}
			return fmt.Errorf("check toolset ownership: %w", err)
		}
	}

	return nil
}
