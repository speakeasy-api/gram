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

	srv "github.com/speakeasy-api/gram/dev-idp/gen/http/xaa_apps/server"
	gen "github.com/speakeasy-api/gram/dev-idp/gen/xaa_apps"
	"github.com/speakeasy-api/gram/dev-idp/internal/conv"
	"github.com/speakeasy-api/gram/dev-idp/internal/database/repo"
	"github.com/speakeasy-api/gram/dev-idp/internal/middleware"
	"github.com/speakeasy-api/gram/dev-idp/internal/oops"
)

// XaaAppsService is the dev-idp /rpc/xaaApps.* implementation.
type XaaAppsService struct {
	tracer trace.Tracer
	logger *slog.Logger
	db     *sql.DB
}

var _ gen.Service = (*XaaAppsService)(nil)

func NewXaaAppsService(logger *slog.Logger, tracerProvider trace.TracerProvider, db *sql.DB) *XaaAppsService {
	return &XaaAppsService{
		tracer: tracerProvider.Tracer("github.com/speakeasy-api/gram/dev-idp/internal/service/xaa_apps"),
		logger: logger.With(slog.String("component", "devidp.xaaApps")),
		db:     db,
	}
}

func AttachXaaApps(mux goahttp.Muxer, service *XaaAppsService) {
	endpoints := gen.NewEndpoints(service)
	endpoints.Use(middleware.MapErrors())
	endpoints.Use(middleware.TraceMethods(service.tracer))
	srv.Mount(
		mux,
		srv.New(endpoints, mux, goahttp.RequestDecoder, goahttp.ResponseEncoder, nil, nil),
	)
}

func (s *XaaAppsService) Create(ctx context.Context, p *gen.CreatePayload) (*gen.XaaApp, error) {
	name := p.ClientID
	if p.Name != nil && *p.Name != "" {
		name = *p.Name
	}

	row, err := repo.New(s.db).CreateXaaApp(ctx, repo.CreateXaaAppParams{
		ID:           uuid.New(),
		ClientID:     p.ClientID,
		ClientSecret: conv.PtrValOrEmpty(p.ClientSecret),
		Name:         name,
		Enabled:      conv.PtrBool(p.Enabled, true),
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "create xaa app").Log(ctx, s.logger)
	}

	return xaaAppView(row), nil
}

func (s *XaaAppsService) Update(ctx context.Context, p *gen.UpdatePayload) (*gen.XaaApp, error) {
	id, err := uuid.Parse(p.ID)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid xaa app id")
	}

	queries := repo.New(s.db)

	// `enabled` is rewritten unconditionally by the query, so an absent one
	// has to be resolved against the current row rather than defaulted.
	enabled := p.Enabled
	if enabled == nil {
		current, gerr := queries.GetXaaApp(ctx, id)
		if gerr != nil {
			if errors.Is(gerr, sql.ErrNoRows) {
				return nil, oops.E(oops.CodeNotFound, nil, "xaa app not found")
			}
			return nil, oops.E(oops.CodeUnexpected, gerr, "load xaa app").Log(ctx, s.logger)
		}
		enabled = &current.Enabled
	}

	row, err := queries.UpdateXaaApp(ctx, repo.UpdateXaaAppParams{
		ClientID:     conv.PtrToNullString(p.ClientID),
		ClientSecret: conv.PtrToNullString(p.ClientSecret),
		Name:         conv.PtrToNullString(p.Name),
		Enabled:      *enabled,
		Ts:           time.Now(),
		ID:           id,
	})
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return nil, oops.E(oops.CodeNotFound, nil, "xaa app not found")
	case err != nil:
		return nil, oops.E(oops.CodeUnexpected, err, "update xaa app").Log(ctx, s.logger)
	}

	return xaaAppView(row), nil
}

func (s *XaaAppsService) List(ctx context.Context, p *gen.ListPayload) (*gen.ListXaaAppsResult, error) {
	after, err := cursorUUID(p.Cursor)
	if err != nil {
		return nil, err
	}

	rows, err := repo.New(s.db).ListXaaApps(ctx, repo.ListXaaAppsParams{
		After:   after,
		MaxRows: int64(p.Limit) + 1,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list xaa apps").Log(ctx, s.logger)
	}

	page, nextCursor := paginate(rows, p.Limit, func(a repo.XaaApp) string { return a.ID.String() })

	items := make([]*gen.XaaApp, 0, len(page))
	for _, r := range page {
		items = append(items, xaaAppView(r))
	}

	return &gen.ListXaaAppsResult{Items: items, NextCursor: nextCursor}, nil
}

func (s *XaaAppsService) Delete(ctx context.Context, p *gen.DeletePayload) error {
	id, err := uuid.Parse(p.ID)
	if err != nil {
		return oops.E(oops.CodeBadRequest, err, "invalid xaa app id")
	}

	if err := repo.New(s.db).DeleteXaaApp(ctx, id); err != nil {
		return oops.E(oops.CodeUnexpected, err, "delete xaa app").Log(ctx, s.logger)
	}

	return nil
}

func xaaAppView(r repo.XaaApp) *gen.XaaApp {
	return &gen.XaaApp{
		ID:           r.ID.String(),
		ClientID:     r.ClientID,
		ClientSecret: r.ClientSecret,
		Name:         r.Name,
		Enabled:      r.Enabled,
		CreatedAt:    r.CreatedAt.UTC().Format(timeFormat),
		UpdatedAt:    r.UpdatedAt.UTC().Format(timeFormat),
	}
}
