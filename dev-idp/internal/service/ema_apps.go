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

	gen "github.com/speakeasy-api/gram/dev-idp/gen/ema_apps"
	srv "github.com/speakeasy-api/gram/dev-idp/gen/http/ema_apps/server"
	"github.com/speakeasy-api/gram/dev-idp/internal/conv"
	"github.com/speakeasy-api/gram/dev-idp/internal/database/repo"
	"github.com/speakeasy-api/gram/dev-idp/internal/middleware"
	"github.com/speakeasy-api/gram/dev-idp/internal/oops"
)

// EmaAppsService is the dev-idp /rpc/emaApps.* implementation.
type EmaAppsService struct {
	tracer trace.Tracer
	logger *slog.Logger
	db     *sql.DB
}

var _ gen.Service = (*EmaAppsService)(nil)

func NewEmaAppsService(logger *slog.Logger, tracerProvider trace.TracerProvider, db *sql.DB) *EmaAppsService {
	return &EmaAppsService{
		tracer: tracerProvider.Tracer("github.com/speakeasy-api/gram/dev-idp/internal/service/ema_apps"),
		logger: logger.With(slog.String("component", "devidp.emaApps")),
		db:     db,
	}
}

func AttachEmaApps(mux goahttp.Muxer, service *EmaAppsService) {
	endpoints := gen.NewEndpoints(service)
	endpoints.Use(middleware.MapErrors())
	endpoints.Use(middleware.TraceMethods(service.tracer))
	srv.Mount(
		mux,
		srv.New(endpoints, mux, goahttp.RequestDecoder, goahttp.ResponseEncoder, nil, nil),
	)
}

func (s *EmaAppsService) Create(ctx context.Context, p *gen.CreatePayload) (*gen.EmaApp, error) {
	name := p.ClientID
	if p.Name != nil && *p.Name != "" {
		name = *p.Name
	}

	row, err := repo.New(s.db).CreateEmaApp(ctx, repo.CreateEmaAppParams{
		ID:           uuid.New(),
		ClientID:     p.ClientID,
		ClientSecret: conv.PtrValOrEmpty(p.ClientSecret),
		Name:         name,
		Enabled:      conv.PtrBool(p.Enabled, true),
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "create ema app").Log(ctx, s.logger)
	}

	return emaAppView(row), nil
}

func (s *EmaAppsService) Update(ctx context.Context, p *gen.UpdatePayload) (*gen.EmaApp, error) {
	id, err := uuid.Parse(p.ID)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid ema app id")
	}

	queries := repo.New(s.db)

	// `enabled` is rewritten unconditionally by the query, so an absent one
	// has to be resolved against the current row rather than defaulted.
	enabled := p.Enabled
	if enabled == nil {
		current, gerr := queries.GetEmaApp(ctx, id)
		if gerr != nil {
			if errors.Is(gerr, sql.ErrNoRows) {
				return nil, oops.E(oops.CodeNotFound, nil, "ema app not found")
			}
			return nil, oops.E(oops.CodeUnexpected, gerr, "load ema app").Log(ctx, s.logger)
		}
		enabled = &current.Enabled
	}

	row, err := queries.UpdateEmaApp(ctx, repo.UpdateEmaAppParams{
		ClientID:     conv.PtrToNullString(p.ClientID),
		ClientSecret: conv.PtrToNullString(p.ClientSecret),
		Name:         conv.PtrToNullString(p.Name),
		Enabled:      *enabled,
		Ts:           time.Now(),
		ID:           id,
	})
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return nil, oops.E(oops.CodeNotFound, nil, "ema app not found")
	case err != nil:
		return nil, oops.E(oops.CodeUnexpected, err, "update ema app").Log(ctx, s.logger)
	}

	return emaAppView(row), nil
}

func (s *EmaAppsService) List(ctx context.Context, p *gen.ListPayload) (*gen.ListEmaAppsResult, error) {
	after, err := cursorUUID(p.Cursor)
	if err != nil {
		return nil, err
	}

	rows, err := repo.New(s.db).ListEmaApps(ctx, repo.ListEmaAppsParams{
		After:   after,
		MaxRows: int64(p.Limit) + 1,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list ema apps").Log(ctx, s.logger)
	}

	page, nextCursor := paginate(rows, p.Limit, func(a repo.EmaApp) string { return a.ID.String() })

	items := make([]*gen.EmaApp, 0, len(page))
	for _, r := range page {
		items = append(items, emaAppView(r))
	}

	return &gen.ListEmaAppsResult{Items: items, NextCursor: nextCursor}, nil
}

func (s *EmaAppsService) Delete(ctx context.Context, p *gen.DeletePayload) error {
	id, err := uuid.Parse(p.ID)
	if err != nil {
		return oops.E(oops.CodeBadRequest, err, "invalid ema app id")
	}

	if err := repo.New(s.db).DeleteEmaApp(ctx, id); err != nil {
		return oops.E(oops.CodeUnexpected, err, "delete ema app").Log(ctx, s.logger)
	}

	return nil
}

func emaAppView(r repo.EmaApp) *gen.EmaApp {
	return &gen.EmaApp{
		ID:           r.ID.String(),
		ClientID:     r.ClientID,
		ClientSecret: r.ClientSecret,
		Name:         r.Name,
		Enabled:      r.Enabled,
		CreatedAt:    r.CreatedAt.UTC().Format(timeFormat),
		UpdatedAt:    r.UpdatedAt.UTC().Format(timeFormat),
	}
}
