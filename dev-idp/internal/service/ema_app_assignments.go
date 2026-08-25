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

	gen "github.com/speakeasy-api/gram/dev-idp/gen/ema_app_assignments"
	srv "github.com/speakeasy-api/gram/dev-idp/gen/http/ema_app_assignments/server"
	"github.com/speakeasy-api/gram/dev-idp/internal/conv"
	"github.com/speakeasy-api/gram/dev-idp/internal/database/repo"
	"github.com/speakeasy-api/gram/dev-idp/internal/middleware"
	"github.com/speakeasy-api/gram/dev-idp/internal/oops"
)

// EmaAppAssignmentsService is the dev-idp /rpc/emaAppAssignments.*
// implementation: the "assign apps to users" surface.
type EmaAppAssignmentsService struct {
	tracer trace.Tracer
	logger *slog.Logger
	db     *sql.DB
}

var _ gen.Service = (*EmaAppAssignmentsService)(nil)

func NewEmaAppAssignmentsService(logger *slog.Logger, tracerProvider trace.TracerProvider, db *sql.DB) *EmaAppAssignmentsService {
	return &EmaAppAssignmentsService{
		tracer: tracerProvider.Tracer("github.com/speakeasy-api/gram/dev-idp/internal/service/ema_app_assignments"),
		logger: logger.With(slog.String("component", "devidp.emaAppAssignments")),
		db:     db,
	}
}

func AttachEmaAppAssignments(mux goahttp.Muxer, service *EmaAppAssignmentsService) {
	endpoints := gen.NewEndpoints(service)
	endpoints.Use(middleware.MapErrors())
	endpoints.Use(middleware.TraceMethods(service.tracer))
	srv.Mount(
		mux,
		srv.New(endpoints, mux, goahttp.RequestDecoder, goahttp.ResponseEncoder, nil, nil),
	)
}

func (s *EmaAppAssignmentsService) Create(ctx context.Context, p *gen.CreatePayload) (*gen.EmaAppAssignment, error) {
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

	row, err := repo.New(s.db).CreateEmaAppAssignment(ctx, repo.CreateEmaAppAssignmentParams{
		ID:            uuid.New(),
		AppID:         appID,
		UserID:        userID,
		ResourceID:    resourceID,
		GrantedScopes: conv.PtrValOrEmpty(p.GrantedScopes),
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "create ema app assignment").Log(ctx, s.logger)
	}

	return emaAppAssignmentView(row), nil
}

func (s *EmaAppAssignmentsService) Update(ctx context.Context, p *gen.UpdatePayload) (*gen.EmaAppAssignment, error) {
	id, err := uuid.Parse(p.ID)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid assignment id")
	}

	row, err := repo.New(s.db).UpdateEmaAppAssignment(ctx, repo.UpdateEmaAppAssignmentParams{
		GrantedScopes: p.GrantedScopes,
		Ts:            time.Now(),
		ID:            id,
	})
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return nil, oops.E(oops.CodeNotFound, nil, "ema app assignment not found")
	case err != nil:
		return nil, oops.E(oops.CodeUnexpected, err, "update ema app assignment").Log(ctx, s.logger)
	}

	return emaAppAssignmentView(row), nil
}

func (s *EmaAppAssignmentsService) List(ctx context.Context, p *gen.ListPayload) (*gen.ListEmaAppAssignmentsResult, error) {
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

	rows, err := repo.New(s.db).ListEmaAppAssignments(ctx, repo.ListEmaAppAssignmentsParams{
		After:      after,
		AppID:      appID,
		UserID:     userID,
		ResourceID: resourceID,
		MaxRows:    int64(p.Limit) + 1,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list ema app assignments").Log(ctx, s.logger)
	}

	page, nextCursor := paginate(rows, p.Limit, func(a repo.EmaAppAssignment) string { return a.ID.String() })

	items := make([]*gen.EmaAppAssignment, 0, len(page))
	for _, r := range page {
		items = append(items, emaAppAssignmentView(r))
	}

	return &gen.ListEmaAppAssignmentsResult{Items: items, NextCursor: nextCursor}, nil
}

func (s *EmaAppAssignmentsService) Delete(ctx context.Context, p *gen.DeletePayload) error {
	id, err := uuid.Parse(p.ID)
	if err != nil {
		return oops.E(oops.CodeBadRequest, err, "invalid assignment id")
	}

	if err := repo.New(s.db).DeleteEmaAppAssignment(ctx, id); err != nil {
		return oops.E(oops.CodeUnexpected, err, "delete ema app assignment").Log(ctx, s.logger)
	}

	return nil
}

func emaAppAssignmentView(r repo.EmaAppAssignment) *gen.EmaAppAssignment {
	return &gen.EmaAppAssignment{
		ID:            r.ID.String(),
		AppID:         r.AppID.String(),
		UserID:        r.UserID.String(),
		ResourceID:    r.ResourceID.String(),
		GrantedScopes: r.GrantedScopes,
		CreatedAt:     r.CreatedAt.UTC().Format(timeFormat),
		UpdatedAt:     r.UpdatedAt.UTC().Format(timeFormat),
	}
}
