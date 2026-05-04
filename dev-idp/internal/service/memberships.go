package service

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"

	"github.com/google/uuid"
	"go.opentelemetry.io/otel/trace"
	goahttp "goa.design/goa/v3/http"

	
	"github.com/speakeasy-api/gram/dev-idp/internal/conv"
	"github.com/speakeasy-api/gram/dev-idp/internal/database/repo"
	srv "github.com/speakeasy-api/gram/dev-idp/gen/http/memberships/server"
	gen "github.com/speakeasy-api/gram/dev-idp/gen/memberships"
	"github.com/speakeasy-api/gram/dev-idp/internal/middleware"
	"github.com/speakeasy-api/gram/dev-idp/internal/oops"
)

// MembershipsService is the dev-idp /rpc/memberships.* implementation.
type MembershipsService struct {
	tracer trace.Tracer
	logger *slog.Logger
	db     *sql.DB
}

var _ gen.Service = (*MembershipsService)(nil)

func NewMembershipsService(logger *slog.Logger, tracerProvider trace.TracerProvider, db *sql.DB) *MembershipsService {
	return &MembershipsService{
		tracer: tracerProvider.Tracer("github.com/speakeasy-api/gram/dev-idp/internal/service/memberships"),
		logger: logger.With(slog.String("component", "devidp.memberships")),
		db:     db,
	}
}

func AttachMemberships(mux goahttp.Muxer, service *MembershipsService) {
	endpoints := gen.NewEndpoints(service)
	endpoints.Use(middleware.MapErrors())
	endpoints.Use(middleware.TraceMethods(service.tracer))
	srv.Mount(
		mux,
		srv.New(endpoints, mux, goahttp.RequestDecoder, goahttp.ResponseEncoder, nil, nil),
	)
}

func (s *MembershipsService) Create(ctx context.Context, p *gen.CreatePayload) (*gen.Membership, error) {
	userID, err := uuid.Parse(p.UserID)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid user_id")
	}
	orgID, err := uuid.Parse(p.OrganizationID)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid organization_id")
	}

	row, err := repo.New(s.db).CreateMembership(ctx, repo.CreateMembershipParams{
		UserID:         userID,
		OrganizationID: orgID,
		Role:           conv.PtrToNullString(p.Role),
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "create membership").Log(ctx, s.logger)
	}

	return membershipView(row), nil
}

func (s *MembershipsService) Update(ctx context.Context, p *gen.UpdatePayload) (*gen.Membership, error) {
	id, err := uuid.Parse(p.ID)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid membership id")
	}

	row, err := repo.New(s.db).UpdateMembership(ctx, repo.UpdateMembershipParams{
		ID:   id,
		Role: p.Role,
	})
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return nil, oops.E(oops.CodeNotFound, nil, "membership not found")
	case err != nil:
		return nil, oops.E(oops.CodeUnexpected, err, "update membership").Log(ctx, s.logger)
	}

	return membershipView(row), nil
}

func (s *MembershipsService) List(ctx context.Context, p *gen.ListPayload) (*gen.ListMembershipsResult, error) {
	after := uuid.Nil
	if p.Cursor != nil && *p.Cursor != "" {
		parsed, err := uuid.Parse(*p.Cursor)
		if err != nil {
			return nil, oops.E(oops.CodeBadRequest, err, "invalid cursor")
		}
		after = parsed
	}

	userFilter, err := optionalUUID(p.UserID, "user_id")
	if err != nil {
		return nil, err
	}
	orgFilter, err := optionalUUID(p.OrganizationID, "organization_id")
	if err != nil {
		return nil, err
	}

	rows, err := repo.New(s.db).ListMemberships(ctx, repo.ListMembershipsParams{
		After:          after,
		UserID:         userFilter,
		OrganizationID: orgFilter,
		MaxRows:        int64(p.Limit) + 1, //nolint:gosec // Goa validates Limit ∈ [1, 100]
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list memberships").Log(ctx, s.logger)
	}

	page, nextCursor := paginate(rows, p.Limit, func(m repo.Membership) string { return m.ID.String() })

	items := make([]*gen.Membership, 0, len(page))
	for _, r := range page {
		items = append(items, membershipView(r))
	}

	return &gen.ListMembershipsResult{Items: items, NextCursor: nextCursor}, nil
}

func (s *MembershipsService) Delete(ctx context.Context, p *gen.DeletePayload) error {
	id, err := uuid.Parse(p.ID)
	if err != nil {
		return oops.E(oops.CodeBadRequest, err, "invalid membership id")
	}

	if err := repo.New(s.db).DeleteMembership(ctx, id); err != nil {
		return oops.E(oops.CodeUnexpected, err, "delete membership").Log(ctx, s.logger)
	}

	return nil
}

func membershipView(r repo.Membership) *gen.Membership {
	return &gen.Membership{
		ID:             r.ID.String(),
		UserID:         r.UserID.String(),
		OrganizationID: r.OrganizationID.String(),
		Role:           r.Role,
		CreatedAt:      r.CreatedAt.UTC().Format(timeFormat),
		UpdatedAt:      r.UpdatedAt.UTC().Format(timeFormat),
	}
}

