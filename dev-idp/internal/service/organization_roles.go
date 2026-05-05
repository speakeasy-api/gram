package service

import (
	"context"
	"time"
	"database/sql"
	"errors"
	"log/slog"

	"github.com/google/uuid"
	"go.opentelemetry.io/otel/trace"
	goahttp "goa.design/goa/v3/http"

	srv "github.com/speakeasy-api/gram/dev-idp/gen/http/organization_roles/server"
	gen "github.com/speakeasy-api/gram/dev-idp/gen/organization_roles"
	"github.com/speakeasy-api/gram/dev-idp/internal/conv"
	"github.com/speakeasy-api/gram/dev-idp/internal/database/repo"
	"github.com/speakeasy-api/gram/dev-idp/internal/middleware"
	"github.com/speakeasy-api/gram/dev-idp/internal/oops"
)

// OrganizationRolesService implements /rpc/organizationRoles.* — the Goa
// surface for managing roles on an org keyed by (organization_id, slug).
type OrganizationRolesService struct {
	tracer trace.Tracer
	logger *slog.Logger
	db     *sql.DB
}

var _ gen.Service = (*OrganizationRolesService)(nil)

func NewOrganizationRolesService(logger *slog.Logger, tracerProvider trace.TracerProvider, db *sql.DB) *OrganizationRolesService {
	return &OrganizationRolesService{
		tracer: tracerProvider.Tracer("github.com/speakeasy-api/gram/dev-idp/internal/service/organization_roles"),
		logger: logger.With(slog.String("component", "devidp.organizationRoles")),
		db:     db,
	}
}

func AttachOrganizationRoles(mux goahttp.Muxer, service *OrganizationRolesService) {
	endpoints := gen.NewEndpoints(service)
	endpoints.Use(middleware.MapErrors())
	endpoints.Use(middleware.TraceMethods(service.tracer))
	srv.Mount(
		mux,
		srv.New(endpoints, mux, goahttp.RequestDecoder, goahttp.ResponseEncoder, nil, nil),
	)
}

func (s *OrganizationRolesService) Create(ctx context.Context, p *gen.CreatePayload) (*gen.OrganizationRole, error) {
	orgID, err := uuid.Parse(p.OrganizationID)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid organization_id")
	}

	row, err := repo.New(s.db).CreateOrganizationRole(ctx, repo.CreateOrganizationRoleParams{
		ID:             uuid.New(),
		OrganizationID: orgID,
		Slug:           p.Slug,
		Name:           p.Name,
		Description:    conv.PtrToNullString(p.Description),
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "create organization role").Log(ctx, s.logger)
	}

	return organizationRoleView(row), nil
}

func (s *OrganizationRolesService) Update(ctx context.Context, p *gen.UpdatePayload) (*gen.OrganizationRole, error) {
	orgID, err := uuid.Parse(p.OrganizationID)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid organization_id")
	}

	row, err := repo.New(s.db).UpdateOrganizationRole(ctx, repo.UpdateOrganizationRoleParams{
		OrganizationID: orgID,
		Slug:           p.Slug,
		Name:           conv.PtrToNullString(p.Name),
		Description:    conv.PtrToNullString(p.Description),
		Ts:             time.Now(),
	})
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return nil, oops.E(oops.CodeNotFound, nil, "role not found")
	case err != nil:
		return nil, oops.E(oops.CodeUnexpected, err, "update organization role").Log(ctx, s.logger)
	}

	return organizationRoleView(row), nil
}

func (s *OrganizationRolesService) List(ctx context.Context, p *gen.ListPayload) (*gen.ListOrganizationRolesResult, error) {
	orgID, err := uuid.Parse(p.OrganizationID)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid organization_id")
	}

	rows, err := repo.New(s.db).ListOrganizationRoles(ctx, orgID)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list organization roles").Log(ctx, s.logger)
	}

	items := make([]*gen.OrganizationRole, 0, len(rows))
	for _, r := range rows {
		items = append(items, organizationRoleView(r))
	}
	return &gen.ListOrganizationRolesResult{Items: items}, nil
}

func (s *OrganizationRolesService) Delete(ctx context.Context, p *gen.DeletePayload) error {
	orgID, err := uuid.Parse(p.OrganizationID)
	if err != nil {
		return oops.E(oops.CodeBadRequest, err, "invalid organization_id")
	}

	if err := repo.New(s.db).DeleteOrganizationRole(ctx, repo.DeleteOrganizationRoleParams{
		OrganizationID: orgID,
		Slug:           p.Slug,
	}); err != nil {
		return oops.E(oops.CodeUnexpected, err, "delete organization role").Log(ctx, s.logger)
	}
	return nil
}

func organizationRoleView(r repo.OrganizationRole) *gen.OrganizationRole {
	return &gen.OrganizationRole{
		ID:             r.ID.String(),
		OrganizationID: r.OrganizationID.String(),
		Slug:           r.Slug,
		Name:           r.Name,
		Description:    r.Description,
		CreatedAt:      r.CreatedAt.UTC().Format(timeFormat),
		UpdatedAt:      r.UpdatedAt.UTC().Format(timeFormat),
	}
}
