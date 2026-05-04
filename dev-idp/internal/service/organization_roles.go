package service

import (
	"context"
	"errors"
	"log/slog"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/otel/trace"
	goahttp "goa.design/goa/v3/http"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/devidp/database/repo"
	srv "github.com/speakeasy-api/gram/server/internal/devidp/gen/http/organization_roles/server"
	gen "github.com/speakeasy-api/gram/server/internal/devidp/gen/organization_roles"
	"github.com/speakeasy-api/gram/server/internal/middleware"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

// OrganizationRolesService implements /rpc/organizationRoles.* — the Goa
// surface for managing roles on an org keyed by (organization_id, slug).
type OrganizationRolesService struct {
	tracer trace.Tracer
	logger *slog.Logger
	db     *pgxpool.Pool
}

var _ gen.Service = (*OrganizationRolesService)(nil)

func NewOrganizationRolesService(logger *slog.Logger, tracerProvider trace.TracerProvider, db *pgxpool.Pool) *OrganizationRolesService {
	return &OrganizationRolesService{
		tracer: tracerProvider.Tracer("github.com/speakeasy-api/gram/server/internal/devidp/service/organization_roles"),
		logger: logger.With(attr.SlogComponent("devidp.organizationRoles")),
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
		OrganizationID: orgID,
		Slug:           p.Slug,
		Name:           p.Name,
		Description:    conv.PtrToPGTextEmpty(p.Description),
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
		Name:           conv.PtrToPGTextEmpty(p.Name),
		Description:    conv.PtrToPGTextEmpty(p.Description),
	})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
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
		CreatedAt:      r.CreatedAt.Time.UTC().Format(timeFormat),
		UpdatedAt:      r.UpdatedAt.Time.UTC().Format(timeFormat),
	}
}
