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

	srv "github.com/speakeasy-api/gram/dev-idp/gen/http/xaa_resources/server"
	gen "github.com/speakeasy-api/gram/dev-idp/gen/xaa_resources"
	"github.com/speakeasy-api/gram/dev-idp/internal/conv"
	"github.com/speakeasy-api/gram/dev-idp/internal/database/repo"
	"github.com/speakeasy-api/gram/dev-idp/internal/middleware"
	"github.com/speakeasy-api/gram/dev-idp/internal/oops"
	"github.com/speakeasy-api/gram/dev-idp/internal/xaa"
)

// XaaResourcesService is the dev-idp /rpc/xaaResources.* implementation.
//
// It holds the external URL because a resource's issuer identifier is derived
// from its slug, and callers need that value to know what to put in an
// ID-JAG's `aud` -- recomputing it by hand is exactly the mistake this
// profile punishes.
type XaaResourcesService struct {
	tracer      trace.Tracer
	logger      *slog.Logger
	db          *sql.DB
	externalURL string
}

var _ gen.Service = (*XaaResourcesService)(nil)

func NewXaaResourcesService(logger *slog.Logger, tracerProvider trace.TracerProvider, db *sql.DB, externalURL string) *XaaResourcesService {
	return &XaaResourcesService{
		tracer:      tracerProvider.Tracer("github.com/speakeasy-api/gram/dev-idp/internal/service/xaa_resources"),
		logger:      logger.With(slog.String("component", "devidp.xaaResources")),
		db:          db,
		externalURL: externalURL,
	}
}

func AttachXaaResources(mux goahttp.Muxer, service *XaaResourcesService) {
	endpoints := gen.NewEndpoints(service)
	endpoints.Use(middleware.MapErrors())
	endpoints.Use(middleware.TraceMethods(service.tracer))
	srv.Mount(
		mux,
		srv.New(endpoints, mux, goahttp.RequestDecoder, goahttp.ResponseEncoder, nil, nil),
	)
}

func (s *XaaResourcesService) Create(ctx context.Context, p *gen.CreatePayload) (*gen.XaaResource, error) {
	name := p.Slug
	if p.Name != nil && *p.Name != "" {
		name = *p.Name
	}

	row, err := repo.New(s.db).CreateXaaResource(ctx, repo.CreateXaaResourceParams{
		ID:                 uuid.New(),
		Slug:               p.Slug,
		Name:               name,
		ResourceIdentifier: p.ResourceIdentifier,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "create xaa resource").Log(ctx, s.logger)
	}

	return s.view(row), nil
}

func (s *XaaResourcesService) Update(ctx context.Context, p *gen.UpdatePayload) (*gen.XaaResource, error) {
	id, err := uuid.Parse(p.ID)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid xaa resource id")
	}

	row, err := repo.New(s.db).UpdateXaaResource(ctx, repo.UpdateXaaResourceParams{
		Slug:               conv.PtrToNullString(p.Slug),
		Name:               conv.PtrToNullString(p.Name),
		ResourceIdentifier: conv.PtrToNullString(p.ResourceIdentifier),
		Ts:                 time.Now(),
		ID:                 id,
	})
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return nil, oops.E(oops.CodeNotFound, nil, "xaa resource not found")
	case err != nil:
		return nil, oops.E(oops.CodeUnexpected, err, "update xaa resource").Log(ctx, s.logger)
	}

	return s.view(row), nil
}

func (s *XaaResourcesService) List(ctx context.Context, p *gen.ListPayload) (*gen.ListXaaResourcesResult, error) {
	after, err := cursorUUID(p.Cursor)
	if err != nil {
		return nil, err
	}

	rows, err := repo.New(s.db).ListXaaResources(ctx, repo.ListXaaResourcesParams{
		After:   after,
		MaxRows: int64(p.Limit) + 1,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list xaa resources").Log(ctx, s.logger)
	}

	page, nextCursor := paginate(rows, p.Limit, func(r repo.XaaResource) string { return r.ID.String() })

	items := make([]*gen.XaaResource, 0, len(page))
	for _, r := range page {
		items = append(items, s.view(r))
	}

	return &gen.ListXaaResourcesResult{Items: items, NextCursor: nextCursor}, nil
}

func (s *XaaResourcesService) Delete(ctx context.Context, p *gen.DeletePayload) error {
	id, err := uuid.Parse(p.ID)
	if err != nil {
		return oops.E(oops.CodeBadRequest, err, "invalid xaa resource id")
	}

	if err := repo.New(s.db).DeleteXaaResource(ctx, id); err != nil {
		return oops.E(oops.CodeUnexpected, err, "delete xaa resource").Log(ctx, s.logger)
	}

	return nil
}

func (s *XaaResourcesService) view(r repo.XaaResource) *gen.XaaResource {
	return &gen.XaaResource{
		ID:                 r.ID.String(),
		Slug:               r.Slug,
		Name:               r.Name,
		ResourceIdentifier: r.ResourceIdentifier,
		Issuer:             xaa.ResourceASIssuer(s.externalURL, r.Slug),
		CreatedAt:          r.CreatedAt.UTC().Format(timeFormat),
		UpdatedAt:          r.UpdatedAt.UTC().Format(timeFormat),
	}
}
