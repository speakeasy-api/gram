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

	gen "github.com/speakeasy-api/gram/dev-idp/gen/ema_resources"
	srv "github.com/speakeasy-api/gram/dev-idp/gen/http/ema_resources/server"
	"github.com/speakeasy-api/gram/dev-idp/internal/conv"
	"github.com/speakeasy-api/gram/dev-idp/internal/database/repo"
	"github.com/speakeasy-api/gram/dev-idp/internal/ema"
	"github.com/speakeasy-api/gram/dev-idp/internal/middleware"
	"github.com/speakeasy-api/gram/dev-idp/internal/oops"
)

// EmaResourcesService is the dev-idp /rpc/emaResources.* implementation.
//
// It holds the external URL because a resource's issuer identifier is derived
// from its slug, and callers need that value to know what to put in an
// ID-JAG's `aud` -- recomputing it by hand is exactly the mistake this
// profile punishes.
type EmaResourcesService struct {
	tracer      trace.Tracer
	logger      *slog.Logger
	db          *sql.DB
	externalURL string
}

var _ gen.Service = (*EmaResourcesService)(nil)

func NewEmaResourcesService(logger *slog.Logger, tracerProvider trace.TracerProvider, db *sql.DB, externalURL string) *EmaResourcesService {
	return &EmaResourcesService{
		tracer:      tracerProvider.Tracer("github.com/speakeasy-api/gram/dev-idp/internal/service/ema_resources"),
		logger:      logger.With(slog.String("component", "devidp.emaResources")),
		db:          db,
		externalURL: externalURL,
	}
}

func AttachEmaResources(mux goahttp.Muxer, service *EmaResourcesService) {
	endpoints := gen.NewEndpoints(service)
	endpoints.Use(middleware.MapErrors())
	endpoints.Use(middleware.TraceMethods(service.tracer))
	srv.Mount(
		mux,
		srv.New(endpoints, mux, goahttp.RequestDecoder, goahttp.ResponseEncoder, nil, nil),
	)
}

func (s *EmaResourcesService) Create(ctx context.Context, p *gen.CreatePayload) (*gen.EmaResource, error) {
	name := p.Slug
	if p.Name != nil && *p.Name != "" {
		name = *p.Name
	}

	row, err := repo.New(s.db).CreateEmaResource(ctx, repo.CreateEmaResourceParams{
		ID:                 uuid.New(),
		Slug:               p.Slug,
		Name:               name,
		ResourceIdentifier: p.ResourceIdentifier,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "create ema resource").Log(ctx, s.logger)
	}

	return s.view(row), nil
}

func (s *EmaResourcesService) Update(ctx context.Context, p *gen.UpdatePayload) (*gen.EmaResource, error) {
	id, err := uuid.Parse(p.ID)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid ema resource id")
	}

	row, err := repo.New(s.db).UpdateEmaResource(ctx, repo.UpdateEmaResourceParams{
		Slug:               conv.PtrToNullString(p.Slug),
		Name:               conv.PtrToNullString(p.Name),
		ResourceIdentifier: conv.PtrToNullString(p.ResourceIdentifier),
		Ts:                 time.Now(),
		ID:                 id,
	})
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return nil, oops.E(oops.CodeNotFound, nil, "ema resource not found")
	case err != nil:
		return nil, oops.E(oops.CodeUnexpected, err, "update ema resource").Log(ctx, s.logger)
	}

	return s.view(row), nil
}

func (s *EmaResourcesService) List(ctx context.Context, p *gen.ListPayload) (*gen.ListEmaResourcesResult, error) {
	after, err := cursorUUID(p.Cursor)
	if err != nil {
		return nil, err
	}

	rows, err := repo.New(s.db).ListEmaResources(ctx, repo.ListEmaResourcesParams{
		After:   after,
		MaxRows: int64(p.Limit) + 1,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list ema resources").Log(ctx, s.logger)
	}

	page, nextCursor := paginate(rows, p.Limit, func(r repo.EmaResource) string { return r.ID.String() })

	items := make([]*gen.EmaResource, 0, len(page))
	for _, r := range page {
		items = append(items, s.view(r))
	}

	return &gen.ListEmaResourcesResult{Items: items, NextCursor: nextCursor}, nil
}

func (s *EmaResourcesService) Delete(ctx context.Context, p *gen.DeletePayload) error {
	id, err := uuid.Parse(p.ID)
	if err != nil {
		return oops.E(oops.CodeBadRequest, err, "invalid ema resource id")
	}

	if err := repo.New(s.db).DeleteEmaResource(ctx, id); err != nil {
		return oops.E(oops.CodeUnexpected, err, "delete ema resource").Log(ctx, s.logger)
	}

	return nil
}

func (s *EmaResourcesService) view(r repo.EmaResource) *gen.EmaResource {
	return &gen.EmaResource{
		ID:                 r.ID.String(),
		Slug:               r.Slug,
		Name:               r.Name,
		ResourceIdentifier: r.ResourceIdentifier,
		Issuer:             ema.ResourceASIssuer(s.externalURL, r.Slug),
		CreatedAt:          r.CreatedAt.UTC().Format(timeFormat),
		UpdatedAt:          r.UpdatedAt.UTC().Format(timeFormat),
	}
}
