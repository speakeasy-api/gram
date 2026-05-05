package service

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"go.opentelemetry.io/otel/trace"
	goahttp "goa.design/goa/v3/http"

	srv "github.com/speakeasy-api/gram/dev-idp/gen/http/organizations/server"
	gen "github.com/speakeasy-api/gram/dev-idp/gen/organizations"
	"github.com/speakeasy-api/gram/dev-idp/internal/conv"
	"github.com/speakeasy-api/gram/dev-idp/internal/database/repo"
	"github.com/speakeasy-api/gram/dev-idp/internal/middleware"
	"github.com/speakeasy-api/gram/dev-idp/internal/oops"
)

// OrganizationsService is the dev-idp /rpc/organizations.* implementation.
type OrganizationsService struct {
	tracer trace.Tracer
	logger *slog.Logger
	db     *sql.DB
}

var _ gen.Service = (*OrganizationsService)(nil)

func NewOrganizationsService(logger *slog.Logger, tracerProvider trace.TracerProvider, db *sql.DB) *OrganizationsService {
	return &OrganizationsService{
		tracer: tracerProvider.Tracer("github.com/speakeasy-api/gram/dev-idp/internal/service/organizations"),
		logger: logger.With(slog.String("component", "devidp.organizations")),
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
		ID:          uuid.New(),
		Name:        p.Name,
		Slug:        p.Slug,
		AccountType: conv.PtrToNullString(p.AccountType),
		WorkosID:    conv.PtrToNullString(p.WorkosID),
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
	// non-empty means "set". Clearing routes through a dedicated query so
	// the partial-update path stays a simple COALESCE.
	clearWorkos := p.WorkosID != nil && *p.WorkosID == ""

	queries := repo.New(s.db)
	now := time.Now()

	if clearWorkos {
		if _, err := queries.ClearOrganizationWorkosID(ctx, repo.ClearOrganizationWorkosIDParams{ID: id, Ts: now}); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return nil, oops.E(oops.CodeNotFound, nil, "organization not found")
			}
			return nil, oops.E(oops.CodeUnexpected, err, "clear organization workos_id").Log(ctx, s.logger)
		}
	}

	row, err := queries.UpdateOrganization(ctx, repo.UpdateOrganizationParams{
		Name:        conv.PtrToNullString(p.Name),
		Slug:        conv.PtrToNullString(p.Slug),
		AccountType: conv.PtrToNullString(p.AccountType),
		WorkosID:    conv.PtrToNullString(p.WorkosID),
		Ts:          now,
		ID:          id,
	})
	switch {
	case errors.Is(err, sql.ErrNoRows):
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
		MaxRows: int64(p.Limit) + 1, //nolint:gosec // Goa validates Limit ∈ [1, 100]
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
		WorkosID:    conv.FromNullString(r.WorkosID),
		CreatedAt:   r.CreatedAt.UTC().Format(timeFormat),
		UpdatedAt:   r.UpdatedAt.UTC().Format(timeFormat),
	}
}
