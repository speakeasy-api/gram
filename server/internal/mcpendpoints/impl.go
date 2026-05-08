package mcpendpoints

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

	srv "github.com/speakeasy-api/gram/server/gen/http/mcp_endpoints/server"
	gen "github.com/speakeasy-api/gram/server/gen/mcp_endpoints"
	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/auth"
	"github.com/speakeasy-api/gram/server/internal/auth/sessions"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	customdomainsrepo "github.com/speakeasy-api/gram/server/internal/customdomains/repo"
	"github.com/speakeasy-api/gram/server/internal/mcpendpoints/repo"
	mcpserversrepo "github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
	"github.com/speakeasy-api/gram/server/internal/middleware"
	"github.com/speakeasy-api/gram/server/internal/mv"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/oops"
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
	logger = logger.With(attr.SlogComponent("mcpendpoints"))

	return &Service{
		tracer: tracerProvider.Tracer("github.com/speakeasy-api/gram/server/internal/mcpendpoints"),
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
		return nil, oops.E(oops.CodeBadRequest, err, "invalid custom_domain_id").Log(ctx, logger)
	}

	mcpServerID, err := uuid.Parse(payload.McpServerID)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid mcp_server_id").Log(ctx, logger)
	}

	slug := string(payload.Slug)
	if err := validateSlugPrefix(slug, customDomainID, authCtx.OrganizationSlug); err != nil {
		return nil, oops.E(oops.CodeInvalid, err, "invalid slug").Log(ctx, logger)
	}

	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "begin transaction").Log(ctx, logger)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })

	txRepo := repo.New(dbtx)

	if err := verifyEndpointReferenceOwnership(ctx, dbtx, *authCtx.ProjectID, authCtx.ActiveOrganizationID, mcpServerID, customDomainID); err != nil {
		return nil, oops.E(oops.CodeInvalid, err, "invalid mcp endpoint").Log(ctx, logger)
	}

	created, err := txRepo.CreateMCPEndpoint(ctx, repo.CreateMCPEndpointParams{
		ProjectID:      *authCtx.ProjectID,
		CustomDomainID: customDomainID,
		McpServerID:    mcpServerID,
		Slug:           slug,
	})
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == pgerrcode.UniqueViolation {
			return nil, oops.E(oops.CodeConflict, err, "mcp endpoint slug already exists for this domain").Log(ctx, logger)
		}
		return nil, oops.E(oops.CodeUnexpected, err, "create mcp endpoint").Log(ctx, logger)
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
		return nil, oops.E(oops.CodeUnexpected, err, "log mcp endpoint creation").Log(ctx, logger)
	}

	if err := dbtx.Commit(ctx); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "commit transaction").Log(ctx, logger)
	}

	return mv.BuildMcpEndpointView(created), nil
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
		return nil, oops.E(oops.CodeInvalid, nil, "provide exactly one of id or slug").Log(ctx, s.logger)
	}

	if hasID && payload.CustomDomainID != nil {
		return nil, oops.E(oops.CodeInvalid, nil, "custom_domain_id cannot be combined with id").Log(ctx, s.logger)
	}

	txRepo := repo.New(s.db)

	if hasID {
		endpointID, err := uuid.Parse(*payload.ID)
		if err != nil {
			return nil, oops.E(oops.CodeBadRequest, err, "invalid mcp endpoint id").Log(ctx, s.logger)
		}

		row, err := txRepo.GetMCPEndpointByID(ctx, repo.GetMCPEndpointByIDParams{
			ID:        endpointID,
			ProjectID: *authCtx.ProjectID,
		})
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return nil, oops.E(oops.CodeNotFound, err, "mcp endpoint not found").Log(ctx, s.logger)
			}
			return nil, oops.E(oops.CodeUnexpected, err, "get mcp endpoint").Log(ctx, s.logger)
		}

		return mv.BuildMcpEndpointView(row), nil
	}

	customDomainID, err := conv.PtrToNullUUID(payload.CustomDomainID)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid custom_domain_id").Log(ctx, s.logger)
	}

	row, err := txRepo.GetMCPEndpointByProjectAndCustomDomainAndSlug(ctx, repo.GetMCPEndpointByProjectAndCustomDomainAndSlugParams{
		ProjectID:      *authCtx.ProjectID,
		Slug:           string(*payload.Slug),
		CustomDomainID: customDomainID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, oops.E(oops.CodeNotFound, err, "mcp endpoint not found").Log(ctx, s.logger)
		}
		return nil, oops.E(oops.CodeUnexpected, err, "get mcp endpoint").Log(ctx, s.logger)
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

	if payload.McpServerID != nil && *payload.McpServerID != "" {
		serverID, err := uuid.Parse(*payload.McpServerID)
		if err != nil {
			return nil, oops.E(oops.CodeBadRequest, err, "invalid mcp_server_id").Log(ctx, s.logger)
		}

		rows, err := r.ListMCPEndpointsByMCPServerID(ctx, repo.ListMCPEndpointsByMCPServerIDParams{
			ProjectID:   *authCtx.ProjectID,
			McpServerID: serverID,
		})
		if err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "list mcp endpoints by server").Log(ctx, s.logger)
		}

		return &gen.ListMcpEndpointsResult{McpEndpoints: mv.BuildMcpEndpointListView(rows)}, nil
	}

	rows, err := r.ListMCPEndpointsByProject(ctx, *authCtx.ProjectID)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list mcp endpoints").Log(ctx, s.logger)
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
		return nil, oops.E(oops.CodeBadRequest, err, "invalid mcp endpoint id").Log(ctx, logger)
	}

	customDomainID, err := conv.PtrToNullUUID(payload.CustomDomainID)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid custom_domain_id").Log(ctx, logger)
	}

	mcpServerID, err := uuid.Parse(payload.McpServerID)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid mcp_server_id").Log(ctx, logger)
	}

	slug := string(payload.Slug)
	if err := validateSlugPrefix(slug, customDomainID, authCtx.OrganizationSlug); err != nil {
		return nil, oops.E(oops.CodeInvalid, err, "invalid slug").Log(ctx, logger)
	}

	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "begin transaction").Log(ctx, logger)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })

	txRepo := repo.New(dbtx)

	existing, err := txRepo.GetMCPEndpointByID(ctx, repo.GetMCPEndpointByIDParams{
		ID:        endpointID,
		ProjectID: *authCtx.ProjectID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, oops.E(oops.CodeNotFound, err, "mcp endpoint not found").Log(ctx, logger)
		}
		return nil, oops.E(oops.CodeUnexpected, err, "get mcp endpoint").Log(ctx, logger)
	}

	beforeView := mv.BuildMcpEndpointView(existing)

	if err := verifyEndpointReferenceOwnership(ctx, dbtx, *authCtx.ProjectID, authCtx.ActiveOrganizationID, mcpServerID, customDomainID); err != nil {
		return nil, oops.E(oops.CodeInvalid, err, "invalid mcp endpoint").Log(ctx, logger)
	}

	updated, err := txRepo.UpdateMCPEndpoint(ctx, repo.UpdateMCPEndpointParams{
		CustomDomainID: customDomainID,
		McpServerID:    mcpServerID,
		Slug:           slug,
		ID:             endpointID,
		ProjectID:      *authCtx.ProjectID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, oops.E(oops.CodeNotFound, err, "mcp endpoint not found").Log(ctx, logger)
		}
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == pgerrcode.UniqueViolation {
			return nil, oops.E(oops.CodeConflict, err, "mcp endpoint slug already exists for this domain").Log(ctx, logger)
		}
		return nil, oops.E(oops.CodeUnexpected, err, "update mcp endpoint").Log(ctx, logger)
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
		return nil, oops.E(oops.CodeUnexpected, err, "log mcp endpoint update").Log(ctx, logger)
	}

	if err := dbtx.Commit(ctx); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "commit transaction").Log(ctx, logger)
	}

	return afterView, nil
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
		return oops.E(oops.CodeBadRequest, err, "invalid mcp endpoint id").Log(ctx, logger)
	}

	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "begin transaction").Log(ctx, logger)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })

	txRepo := repo.New(dbtx)

	deleted, err := txRepo.DeleteMCPEndpoint(ctx, repo.DeleteMCPEndpointParams{
		ID:        endpointID,
		ProjectID: *authCtx.ProjectID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return oops.E(oops.CodeNotFound, err, "mcp endpoint not found").Log(ctx, logger)
		}
		return oops.E(oops.CodeUnexpected, err, "delete mcp endpoint").Log(ctx, logger)
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
		return oops.E(oops.CodeUnexpected, err, "log mcp endpoint deletion").Log(ctx, logger)
	}

	if err := dbtx.Commit(ctx); err != nil {
		return oops.E(oops.CodeUnexpected, err, "commit transaction").Log(ctx, logger)
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

// verifyEndpointReferenceOwnership checks that the referenced mcp_server lives
// in the caller's project and that the optional custom_domain is registered
// to the caller's organization. The raw FK constraints only enforce existence,
// not tenancy, so this closes a cross-tenant leak.
//
// Each check delegates to the owning package's scoped Get*ByID query and
// treats sql.ErrNoRows as "not in this project/organization".
func verifyEndpointReferenceOwnership(
	ctx context.Context,
	dbtx pgx.Tx,
	projectID uuid.UUID,
	organizationID string,
	mcpServerID uuid.UUID,
	customDomainID uuid.NullUUID,
) error {
	if _, err := mcpserversrepo.New(dbtx).GetMCPServerByID(ctx, mcpserversrepo.GetMCPServerByIDParams{
		ID:        mcpServerID,
		ProjectID: projectID,
	}); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("mcp_server_id does not reference a resource in this project")
		}
		return fmt.Errorf("check mcp server ownership: %w", err)
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
