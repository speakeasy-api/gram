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

	srv "github.com/speakeasy-api/gram/dev-idp/gen/http/xaa_app_assignments/server"
	gen "github.com/speakeasy-api/gram/dev-idp/gen/xaa_app_assignments"
	"github.com/speakeasy-api/gram/dev-idp/internal/conv"
	"github.com/speakeasy-api/gram/dev-idp/internal/database/repo"
	"github.com/speakeasy-api/gram/dev-idp/internal/middleware"
	"github.com/speakeasy-api/gram/dev-idp/internal/oops"
)

// XaaAppAssignmentsService is the dev-idp /rpc/xaaAppAssignments.*
// implementation: the "assign apps to users" surface.
type XaaAppAssignmentsService struct {
	tracer trace.Tracer
	logger *slog.Logger
	db     *sql.DB
}

var _ gen.Service = (*XaaAppAssignmentsService)(nil)

func NewXaaAppAssignmentsService(logger *slog.Logger, tracerProvider trace.TracerProvider, db *sql.DB) *XaaAppAssignmentsService {
	return &XaaAppAssignmentsService{
		tracer: tracerProvider.Tracer("github.com/speakeasy-api/gram/dev-idp/internal/service/xaa_app_assignments"),
		logger: logger.With(slog.String("component", "devidp.xaaAppAssignments")),
		db:     db,
	}
}

func AttachXaaAppAssignments(mux goahttp.Muxer, service *XaaAppAssignmentsService) {
	endpoints := gen.NewEndpoints(service)
	endpoints.Use(middleware.MapErrors())
	endpoints.Use(middleware.TraceMethods(service.tracer))
	srv.Mount(
		mux,
		srv.New(endpoints, mux, goahttp.RequestDecoder, goahttp.ResponseEncoder, nil, nil),
	)
}

func (s *XaaAppAssignmentsService) Create(ctx context.Context, p *gen.CreatePayload) (*gen.XaaAppAssignment, error) {
	appID, err := uuid.Parse(p.AppID)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid app id")
	}
	userID, err := uuid.Parse(p.UserID)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid user id")
	}
	resourceID, err := uuid.Parse(p.ResourceID)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid resource id")
	}

	row, err := repo.New(s.db).CreateXaaAppAssignment(ctx, repo.CreateXaaAppAssignmentParams{
		ID:            uuid.New(),
		AppID:         appID,
		UserID:        userID,
		ResourceID:    resourceID,
		GrantedScopes: conv.PtrValOrEmpty(p.GrantedScopes),
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "create xaa app assignment").Log(ctx, s.logger)
	}

	return xaaAppAssignmentView(row), nil
}

func (s *XaaAppAssignmentsService) Update(ctx context.Context, p *gen.UpdatePayload) (*gen.XaaAppAssignment, error) {
	id, err := uuid.Parse(p.ID)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid assignment id")
	}

	row, err := repo.New(s.db).UpdateXaaAppAssignment(ctx, repo.UpdateXaaAppAssignmentParams{
		GrantedScopes: p.GrantedScopes,
		Ts:            time.Now(),
		ID:            id,
	})
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return nil, oops.E(oops.CodeNotFound, nil, "xaa app assignment not found")
	case err != nil:
		return nil, oops.E(oops.CodeUnexpected, err, "update xaa app assignment").Log(ctx, s.logger)
	}

	return xaaAppAssignmentView(row), nil
}

func (s *XaaAppAssignmentsService) List(ctx context.Context, p *gen.ListPayload) (*gen.ListXaaAppAssignmentsResult, error) {
	after, err := cursorUUID(p.Cursor)
	if err != nil {
		return nil, err
	}
	appID, err := optionalUUID(p.AppID, "app_id")
	if err != nil {
		return nil, err
	}
	userID, err := optionalUUID(p.UserID, "user_id")
	if err != nil {
		return nil, err
	}
	resourceID, err := optionalUUID(p.ResourceID, "resource_id")
	if err != nil {
		return nil, err
	}

	rows, err := repo.New(s.db).ListXaaAppAssignments(ctx, repo.ListXaaAppAssignmentsParams{
		After:      after,
		AppID:      appID,
		UserID:     userID,
		ResourceID: resourceID,
		MaxRows:    int64(p.Limit) + 1,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list xaa app assignments").Log(ctx, s.logger)
	}

	page, nextCursor := paginate(rows, p.Limit, func(a repo.XaaAppAssignment) string { return a.ID.String() })

	items := make([]*gen.XaaAppAssignment, 0, len(page))
	for _, r := range page {
		items = append(items, xaaAppAssignmentView(r))
	}

	return &gen.ListXaaAppAssignmentsResult{Items: items, NextCursor: nextCursor}, nil
}

func (s *XaaAppAssignmentsService) Delete(ctx context.Context, p *gen.DeletePayload) error {
	id, err := uuid.Parse(p.ID)
	if err != nil {
		return oops.E(oops.CodeBadRequest, err, "invalid assignment id")
	}

	if err := repo.New(s.db).DeleteXaaAppAssignment(ctx, id); err != nil {
		return oops.E(oops.CodeUnexpected, err, "delete xaa app assignment").Log(ctx, s.logger)
	}

	return nil
}

func xaaAppAssignmentView(r repo.XaaAppAssignment) *gen.XaaAppAssignment {
	return &gen.XaaAppAssignment{
		ID:            r.ID.String(),
		AppID:         r.AppID.String(),
		UserID:        r.UserID.String(),
		ResourceID:    r.ResourceID.String(),
		GrantedScopes: r.GrantedScopes,
		CreatedAt:     r.CreatedAt.UTC().Format(timeFormat),
		UpdatedAt:     r.UpdatedAt.UTC().Format(timeFormat),
	}
}
