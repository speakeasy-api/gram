package service

import (
	"context"
	"errors"
	"log/slog"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/otel/trace"
	goahttp "goa.design/goa/v3/http"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/devidp/database/repo"
	srv "github.com/speakeasy-api/gram/server/internal/devidp/gen/http/organizations/server"
	gen "github.com/speakeasy-api/gram/server/internal/devidp/gen/organizations"
	"github.com/speakeasy-api/gram/server/internal/middleware"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

// OrganizationsService is the dev-idp /rpc/organizations.* implementation.
type OrganizationsService struct {
	tracer trace.Tracer
	logger *slog.Logger
	db     *pgxpool.Pool
}

var _ gen.Service = (*OrganizationsService)(nil)

func NewOrganizationsService(logger *slog.Logger, tracerProvider trace.TracerProvider, db *pgxpool.Pool) *OrganizationsService {
	return &OrganizationsService{
		tracer: tracerProvider.Tracer("github.com/speakeasy-api/gram/server/internal/devidp/service/organizations"),
		logger: logger.With(attr.SlogComponent("devidp.organizations")),
		db:     db,
	}
}

func AttachOrganizations(mux goahttp.Muxer, service *OrganizationsService) {
	endpoints := gen.NewEndpoints(service)
	endpoints.Use(middleware.MapErrors())
	endpoints.Use(middleware.TraceMethods(service.tracer))
	srv.Mount(
		mux,
		srv.New(endpoints, mux, goahttp.RequestDecoder, goahttp.ResponseEncoder, nil, nil),
	)
}

func (s *OrganizationsService) Create(ctx context.Context, p *gen.CreatePayload) (*gen.Organization, error) {
	queries := repo.New(s.db)

	row, err := queries.CreateOrganization(ctx, repo.CreateOrganizationParams{
		Name:        p.Name,
		Slug:        p.Slug,
		AccountType: conv.PtrToPGTextEmpty(p.AccountType),
		WorkosID:    conv.PtrToPGTextEmpty(p.WorkosID),
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "create organization").Log(ctx, s.logger)
	}

	return organizationView(row), nil
}

func (s *OrganizationsService) Update(ctx context.Context, p *gen.UpdatePayload) (*gen.Organization, error) {
	id, err := uuid.Parse(p.ID)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid organization id")
	}

	// Empty string on workos_id means "clear"; absent means "leave alone";
	// non-empty means "set". The SQL CASE/COALESCE pair handles all three.
	clearWorkos := p.WorkosID != nil && *p.WorkosID == ""
	workosNarg := pgtype.Text{Valid: false, String: ""}
	if !clearWorkos {
		workosNarg = conv.PtrToPGTextEmpty(p.WorkosID)
	}

	row, err := repo.New(s.db).UpdateOrganization(ctx, repo.UpdateOrganizationParams{
		Name:          conv.PtrToPGTextEmpty(p.Name),
		Slug:          conv.PtrToPGTextEmpty(p.Slug),
		AccountType:   conv.PtrToPGTextEmpty(p.AccountType),
		ClearWorkosID: clearWorkos,
		WorkosID:      workosNarg,
		ID:            id,
	})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return nil, oops.E(oops.CodeNotFound, nil, "organization not found")
	case err != nil:
		return nil, oops.E(oops.CodeUnexpected, err, "update organization").Log(ctx, s.logger)
	}

	return organizationView(row), nil
}

func (s *OrganizationsService) List(ctx context.Context, p *gen.ListPayload) (*gen.ListOrganizationsResult, error) {
	after := uuid.Nil
	if p.Cursor != nil && *p.Cursor != "" {
		parsed, err := uuid.Parse(*p.Cursor)
		if err != nil {
			return nil, oops.E(oops.CodeBadRequest, err, "invalid cursor")
		}
		after = parsed
	}

	queries := repo.New(s.db)
	rows, err := queries.ListOrganizations(ctx, repo.ListOrganizationsParams{
		After:   after,
		MaxRows: int32(p.Limit) + 1, //nolint:gosec // Goa validates Limit ∈ [1, 100]
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list organizations").Log(ctx, s.logger)
	}

	page, nextCursor := paginate(rows, p.Limit, func(o repo.Organization) string { return o.ID.String() })

	items := make([]*gen.Organization, 0, len(page))
	for _, r := range page {
		items = append(items, organizationView(r))
	}

	return &gen.ListOrganizationsResult{Items: items, NextCursor: nextCursor}, nil
}

func (s *OrganizationsService) Delete(ctx context.Context, p *gen.DeletePayload) error {
	id, err := uuid.Parse(p.ID)
	if err != nil {
		return oops.E(oops.CodeBadRequest, err, "invalid organization id")
	}

	if err := repo.New(s.db).DeleteOrganization(ctx, id); err != nil {
		return oops.E(oops.CodeUnexpected, err, "delete organization").Log(ctx, s.logger)
	}

	return nil
}

func organizationView(r repo.Organization) *gen.Organization {
	return &gen.Organization{
		ID:          r.ID.String(),
		Name:        r.Name,
		Slug:        r.Slug,
		AccountType: r.AccountType,
		WorkosID:    conv.FromPGText[string](r.WorkosID),
		CreatedAt:   r.CreatedAt.Time.UTC().Format(timeFormat),
		UpdatedAt:   r.UpdatedAt.Time.UTC().Format(timeFormat),
	}
}
