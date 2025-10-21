package resources

import (
	"context"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/trace"
	goahttp "goa.design/goa/v3/http"
	"goa.design/goa/v3/security"

	srv "github.com/speakeasy-api/gram/server/gen/http/resources/server"
	gen "github.com/speakeasy-api/gram/server/gen/resources"
	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/auth"
	"github.com/speakeasy-api/gram/server/internal/auth/sessions"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/middleware"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/resources/repo"
)

type Service struct {
	tracer trace.Tracer
	logger *slog.Logger
	db     *pgxpool.Pool
	repo   *repo.Queries
	auth   *auth.Auth
}

var _ gen.Service = (*Service)(nil)

func NewService(logger *slog.Logger, db *pgxpool.Pool, sessions *sessions.Manager) *Service {
	logger = logger.With(attr.SlogComponent("resources"))

	return &Service{
		tracer: otel.Tracer("github.com/speakeasy-api/gram/server/internal/resources"),
		logger: logger,
		db:     db,
		repo:   repo.New(db),
		auth:   auth.New(logger, db, sessions),
	}
}

func Attach(mux goahttp.Muxer, service *Service) {
	endpoints := gen.NewEndpoints(service)
	endpoints.Use(middleware.MapErrors())
	endpoints.Use(middleware.TraceMethods(service.tracer))
	srv.Mount(
		mux,
		srv.New(endpoints, mux, goahttp.RequestDecoder, goahttp.ResponseEncoder, nil, nil),
	)
}

func (s *Service) ListResources(ctx context.Context, payload *gen.ListResourcesPayload) (res *gen.ListResourcesResult, err error) {
	authCtx, _ := contextvalues.GetAuthContext(ctx)
	if authCtx == nil || authCtx.ProjectID == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	limit := conv.PtrValOrEmpty(payload.Limit, 100)
	if limit < 1 || limit > 1000 {
		limit = 100
	}

	params := repo.ListFunctionResourcesParams{
		ProjectID:    *authCtx.ProjectID,
		Cursor:       uuid.NullUUID{Valid: false, UUID: uuid.Nil},
		DeploymentID: uuid.NullUUID{Valid: false, UUID: uuid.Nil},
		Limit:        limit + 1, // +1 to check for next page
	}

	if payload.Cursor != nil {
		cursorUUID, err := uuid.Parse(*payload.Cursor)
		if err != nil {
			return nil, oops.E(oops.CodeBadRequest, err, "invalid cursor").Log(ctx, s.logger)
		}
		params.Cursor = uuid.NullUUID{UUID: cursorUUID, Valid: true}
	}

	if payload.DeploymentID != nil {
		deploymentUUID, err := uuid.Parse(*payload.DeploymentID)
		if err != nil {
			return nil, oops.E(oops.CodeBadRequest, err, "invalid deployment ID").Log(ctx, s.logger)
		}
		params.DeploymentID = uuid.NullUUID{UUID: deploymentUUID, Valid: true}
	}

	resources, err := s.repo.ListFunctionResources(ctx, params)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "failed to list resources").Log(ctx, s.logger)
	}

	result := &gen.ListResourcesResult{
		Resources:  make([]*types.Resource, len(resources)),
		NextCursor: nil,
	}

	for i, resource := range resources {
		result.Resources[i] = &types.Resource{
			FunctionResourceDefinition: &types.FunctionResourceDefinition{
				ID:           resource.ID.String(),
				ResourceUrn:  resource.ResourceUrn.String(),
				DeploymentID: resource.DeploymentID.String(),
				ProjectID:    authCtx.ProjectID.String(),
				FunctionID:   resource.FunctionID.String(),
				Runtime:      resource.Runtime,
				Name:         resource.Name,
				Description:  resource.Description,
				URI:          resource.Uri,
				Title:        conv.FromPGText[string](resource.Title),
				MimeType:     conv.FromPGText[string](resource.MimeType),
				Variables:    resource.Variables,
				CreatedAt:    resource.CreatedAt.Time.Format(time.RFC3339),
				UpdatedAt:    resource.UpdatedAt.Time.Format(time.RFC3339),
			},
		}
	}

	// Check if there are more results
	if len(resources) >= int(limit+1) {
		lastID := resources[len(resources)-1].ID.String()
		result.NextCursor = &lastID
		result.Resources = result.Resources[:len(result.Resources)-1]
	}

	return result, nil
}

func (s *Service) APIKeyAuth(ctx context.Context, key string, schema *security.APIKeyScheme) (context.Context, error) {
	return s.auth.Authorize(ctx, key, schema)
}
