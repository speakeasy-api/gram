package mcpslugs

import (
	"context"
	"database/sql"
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

	srv "github.com/speakeasy-api/gram/server/gen/http/mcp_slugs/server"
	gen "github.com/speakeasy-api/gram/server/gen/mcp_slugs"
	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/auth"
	"github.com/speakeasy-api/gram/server/internal/auth/sessions"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	customdomainsrepo "github.com/speakeasy-api/gram/server/internal/customdomains/repo"
	mcpfrontendsrepo "github.com/speakeasy-api/gram/server/internal/mcpfrontends/repo"
	"github.com/speakeasy-api/gram/server/internal/mcpslugs/repo"
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
}

var _ gen.Service = (*Service)(nil)
var _ gen.Auther = (*Service)(nil)

func NewService(logger *slog.Logger, tracerProvider trace.TracerProvider, db *pgxpool.Pool, sessions *sessions.Manager, authzEngine *authz.Engine) *Service {
	logger = logger.With(attr.SlogComponent("mcpslugs"))

	return &Service{
		tracer: tracerProvider.Tracer("github.com/speakeasy-api/gram/server/internal/mcpslugs"),
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

func (s *Service) CreateMcpSlug(ctx context.Context, payload *gen.CreateMcpSlugPayload) (*types.McpSlug, error) {
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

	mcpFrontendID, err := uuid.Parse(payload.McpFrontendID)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid mcp_frontend_id").Log(ctx, logger)
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

	if err := verifySlugReferenceOwnership(ctx, dbtx, *authCtx.ProjectID, authCtx.ActiveOrganizationID, mcpFrontendID, customDomainID); err != nil {
		return nil, oops.E(oops.CodeInvalid, err, "invalid mcp slug").Log(ctx, logger)
	}

	created, err := txRepo.CreateMCPSlug(ctx, repo.CreateMCPSlugParams{
		ProjectID:      *authCtx.ProjectID,
		CustomDomainID: customDomainID,
		McpFrontendID:  mcpFrontendID,
		Slug:           slug,
	})
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == pgerrcode.UniqueViolation {
			return nil, oops.E(oops.CodeConflict, err, "mcp slug already exists for this domain").Log(ctx, logger)
		}
		return nil, oops.E(oops.CodeUnexpected, err, "create mcp slug").Log(ctx, logger)
	}

	if err := audit.LogMcpSlugCreate(ctx, dbtx, audit.LogMcpSlugCreateEvent{
		OrganizationID:   authCtx.ActiveOrganizationID,
		ProjectID:        *authCtx.ProjectID,
		Actor:            urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID),
		ActorDisplayName: authCtx.Email,
		ActorSlug:        nil,
		McpSlugURN:       urn.NewMcpSlug(created.ID),
		Slug:             created.Slug,
	}); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "log mcp slug creation").Log(ctx, logger)
	}

	if err := dbtx.Commit(ctx); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "commit transaction").Log(ctx, logger)
	}

	return mv.BuildMcpSlugView(created), nil
}

func (s *Service) GetMcpSlug(ctx context.Context, payload *gen.GetMcpSlugPayload) (*types.McpSlug, error) {
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
		slugID, err := uuid.Parse(*payload.ID)
		if err != nil {
			return nil, oops.E(oops.CodeBadRequest, err, "invalid mcp slug id").Log(ctx, s.logger)
		}

		row, err := txRepo.GetMCPSlugByID(ctx, repo.GetMCPSlugByIDParams{
			ID:        slugID,
			ProjectID: *authCtx.ProjectID,
		})
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return nil, oops.E(oops.CodeNotFound, err, "mcp slug not found").Log(ctx, s.logger)
			}
			return nil, oops.E(oops.CodeUnexpected, err, "get mcp slug").Log(ctx, s.logger)
		}

		return mv.BuildMcpSlugView(row), nil
	}

	customDomainID, err := conv.PtrToNullUUID(payload.CustomDomainID)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid custom_domain_id").Log(ctx, s.logger)
	}

	row, err := txRepo.GetMCPSlugByCustomDomainIDAndSlug(ctx, repo.GetMCPSlugByCustomDomainIDAndSlugParams{
		ProjectID:      *authCtx.ProjectID,
		Slug:           string(*payload.Slug),
		CustomDomainID: customDomainID,
	})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, oops.E(oops.CodeNotFound, err, "mcp slug not found").Log(ctx, s.logger)
		}
		return nil, oops.E(oops.CodeUnexpected, err, "get mcp slug").Log(ctx, s.logger)
	}

	return mv.BuildMcpSlugView(row), nil
}

func (s *Service) ListMcpSlugs(ctx context.Context, payload *gen.ListMcpSlugsPayload) (*gen.ListMcpSlugsResult, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeMCPRead, ResourceKind: "", ResourceID: authCtx.ProjectID.String(), Dimensions: nil}); err != nil {
		return nil, err
	}

	r := repo.New(s.db)

	if payload.McpFrontendID != nil && *payload.McpFrontendID != "" {
		frontendID, err := uuid.Parse(*payload.McpFrontendID)
		if err != nil {
			return nil, oops.E(oops.CodeBadRequest, err, "invalid mcp_frontend_id").Log(ctx, s.logger)
		}

		rows, err := r.ListMCPSlugsByFrontendID(ctx, repo.ListMCPSlugsByFrontendIDParams{
			ProjectID:     *authCtx.ProjectID,
			McpFrontendID: frontendID,
		})
		if err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "list mcp slugs by frontend").Log(ctx, s.logger)
		}

		return &gen.ListMcpSlugsResult{McpSlugs: mv.BuildMcpSlugListView(rows)}, nil
	}

	rows, err := r.ListMCPSlugsByProject(ctx, *authCtx.ProjectID)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list mcp slugs").Log(ctx, s.logger)
	}

	return &gen.ListMcpSlugsResult{McpSlugs: mv.BuildMcpSlugListView(rows)}, nil
}

func (s *Service) UpdateMcpSlug(ctx context.Context, payload *gen.UpdateMcpSlugPayload) (*types.McpSlug, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil || authCtx.OrganizationSlug == "" {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeMCPWrite, ResourceKind: "", ResourceID: authCtx.ProjectID.String(), Dimensions: nil}); err != nil {
		return nil, err
	}

	logger := s.logger.With(attr.SlogProjectID(authCtx.ProjectID.String()))

	slugID, err := uuid.Parse(payload.ID)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid mcp slug id").Log(ctx, logger)
	}

	customDomainID, err := conv.PtrToNullUUID(payload.CustomDomainID)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid custom_domain_id").Log(ctx, logger)
	}

	mcpFrontendID, err := uuid.Parse(payload.McpFrontendID)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid mcp_frontend_id").Log(ctx, logger)
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

	existing, err := txRepo.GetMCPSlugByID(ctx, repo.GetMCPSlugByIDParams{
		ID:        slugID,
		ProjectID: *authCtx.ProjectID,
	})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, oops.E(oops.CodeNotFound, err, "mcp slug not found").Log(ctx, logger)
		}
		return nil, oops.E(oops.CodeUnexpected, err, "get mcp slug").Log(ctx, logger)
	}

	beforeView := mv.BuildMcpSlugView(existing)

	if err := verifySlugReferenceOwnership(ctx, dbtx, *authCtx.ProjectID, authCtx.ActiveOrganizationID, mcpFrontendID, customDomainID); err != nil {
		return nil, oops.E(oops.CodeInvalid, err, "invalid mcp slug").Log(ctx, logger)
	}

	updated, err := txRepo.UpdateMCPSlug(ctx, repo.UpdateMCPSlugParams{
		CustomDomainID: customDomainID,
		McpFrontendID:  mcpFrontendID,
		Slug:           slug,
		ID:             slugID,
		ProjectID:      *authCtx.ProjectID,
	})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, oops.E(oops.CodeNotFound, err, "mcp slug not found").Log(ctx, logger)
		}
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == pgerrcode.UniqueViolation {
			return nil, oops.E(oops.CodeConflict, err, "mcp slug already exists for this domain").Log(ctx, logger)
		}
		return nil, oops.E(oops.CodeUnexpected, err, "update mcp slug").Log(ctx, logger)
	}

	afterView := mv.BuildMcpSlugView(updated)

	if err := audit.LogMcpSlugUpdate(ctx, dbtx, audit.LogMcpSlugUpdateEvent{
		OrganizationID:        authCtx.ActiveOrganizationID,
		ProjectID:             *authCtx.ProjectID,
		Actor:                 urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID),
		ActorDisplayName:      authCtx.Email,
		ActorSlug:             nil,
		McpSlugURN:            urn.NewMcpSlug(updated.ID),
		Slug:                  updated.Slug,
		McpSlugSnapshotBefore: beforeView,
		McpSlugSnapshotAfter:  afterView,
	}); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "log mcp slug update").Log(ctx, logger)
	}

	if err := dbtx.Commit(ctx); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "commit transaction").Log(ctx, logger)
	}

	return afterView, nil
}

func (s *Service) DeleteMcpSlug(ctx context.Context, payload *gen.DeleteMcpSlugPayload) error {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return oops.C(oops.CodeUnauthorized)
	}

	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeMCPWrite, ResourceKind: "", ResourceID: authCtx.ProjectID.String(), Dimensions: nil}); err != nil {
		return err
	}

	logger := s.logger.With(attr.SlogProjectID(authCtx.ProjectID.String()))

	slugID, err := uuid.Parse(payload.ID)
	if err != nil {
		return oops.E(oops.CodeBadRequest, err, "invalid mcp slug id").Log(ctx, logger)
	}

	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "begin transaction").Log(ctx, logger)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })

	txRepo := repo.New(dbtx)

	deleted, err := txRepo.DeleteMCPSlug(ctx, repo.DeleteMCPSlugParams{
		ID:        slugID,
		ProjectID: *authCtx.ProjectID,
	})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return oops.E(oops.CodeNotFound, err, "mcp slug not found").Log(ctx, logger)
		}
		return oops.E(oops.CodeUnexpected, err, "delete mcp slug").Log(ctx, logger)
	}

	if err := audit.LogMcpSlugDelete(ctx, dbtx, audit.LogMcpSlugDeleteEvent{
		OrganizationID:   authCtx.ActiveOrganizationID,
		ProjectID:        *authCtx.ProjectID,
		Actor:            urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID),
		ActorDisplayName: authCtx.Email,
		ActorSlug:        nil,
		McpSlugURN:       urn.NewMcpSlug(deleted.ID),
		Slug:             deleted.Slug,
	}); err != nil {
		return oops.E(oops.CodeUnexpected, err, "log mcp slug deletion").Log(ctx, logger)
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
		return fmt.Errorf("mcp slug must be prefixed with the organization slug %q", organizationSlug)
	}

	return nil
}

// verifySlugReferenceOwnership checks that the referenced mcp_frontend lives
// in the caller's project and that the optional custom_domain is registered
// to the caller's organization. The raw FK constraints only enforce existence,
// not tenancy, so this closes a cross-tenant leak.
//
// Each check delegates to the owning package's scoped Get*ByID query and
// treats sql.ErrNoRows as "not in this project/organization".
func verifySlugReferenceOwnership(
	ctx context.Context,
	dbtx pgx.Tx,
	projectID uuid.UUID,
	organizationID string,
	mcpFrontendID uuid.UUID,
	customDomainID uuid.NullUUID,
) error {
	if _, err := mcpfrontendsrepo.New(dbtx).GetMCPFrontendByID(ctx, mcpfrontendsrepo.GetMCPFrontendByIDParams{
		ID:        mcpFrontendID,
		ProjectID: projectID,
	}); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("mcp_frontend_id does not reference a resource in this project")
		}
		return fmt.Errorf("check mcp frontend ownership: %w", err)
	}

	if !customDomainID.Valid {
		return nil
	}

	if _, err := customdomainsrepo.New(dbtx).GetCustomDomainByIDAndOrganization(ctx, customdomainsrepo.GetCustomDomainByIDAndOrganizationParams{
		ID:             customDomainID.UUID,
		OrganizationID: organizationID,
	}); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("custom_domain_id does not reference a resource in this organization")
		}
		return fmt.Errorf("check custom domain ownership: %w", err)
	}

	return nil
}
