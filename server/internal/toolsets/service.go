package toolsets

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/trace"
	goahttp "goa.design/goa/v3/http"
	"goa.design/goa/v3/security"

	srv "github.com/speakeasy-api/gram/gen/http/toolsets/server"
	gen "github.com/speakeasy-api/gram/gen/toolsets"
	"github.com/speakeasy-api/gram/internal/auth"
	"github.com/speakeasy-api/gram/internal/auth/sessions"
	"github.com/speakeasy-api/gram/internal/contextvalues"
	"github.com/speakeasy-api/gram/internal/conv"
	environments_repo "github.com/speakeasy-api/gram/internal/environments/repo"
	"github.com/speakeasy-api/gram/internal/middleware"
	"github.com/speakeasy-api/gram/internal/oops"
	"github.com/speakeasy-api/gram/internal/toolsets/repo"
)

type Service struct {
	tracer          trace.Tracer
	logger          *slog.Logger
	db              *pgxpool.Pool
	repo            *repo.Queries
	environmentRepo *environments_repo.Queries
	auth            *auth.Auth
	toolsets        *Toolsets
}

var _ gen.Service = (*Service)(nil)

func NewService(logger *slog.Logger, db *pgxpool.Pool, sessions *sessions.Manager) *Service {
	return &Service{
		tracer:          otel.Tracer("github.com/speakeasy-api/gram/internal/toolsets"),
		logger:          logger,
		db:              db,
		repo:            repo.New(db),
		auth:            auth.New(logger, db, sessions),
		environmentRepo: environments_repo.New(db),
		toolsets:        NewToolsets(db),
	}
}

func Attach(mux goahttp.Muxer, service *Service) {
	endpoints := gen.NewEndpoints(service)
	endpoints.Use(middleware.TraceMethods(service.tracer))
	srv.Mount(
		mux,
		srv.New(endpoints, mux, goahttp.RequestDecoder, goahttp.ResponseEncoder, nil, nil),
	)
}

func (s *Service) CreateToolset(ctx context.Context, payload *gen.CreateToolsetPayload) (*gen.ToolsetDetails, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return nil, errors.New("project ID not found in context")
	}

	createToolParams := repo.CreateToolsetParams{
		OrganizationID:         authCtx.ActiveOrganizationID,
		ProjectID:              *authCtx.ProjectID,
		Name:                   payload.Name,
		Slug:                   conv.ToSlug(payload.Name),
		Description:            conv.PtrToPGText(payload.Description),
		DefaultEnvironmentSlug: conv.PtrToPGText(nil),
		HttpToolNames:          payload.HTTPToolNames,
	}

	if payload.DefaultEnvironmentSlug != nil {
		_, err := s.environmentRepo.GetEnvironmentBySlug(ctx, environments_repo.GetEnvironmentBySlugParams{
			Slug:      *payload.DefaultEnvironmentSlug,
			ProjectID: *authCtx.ProjectID,
		})
		if err != nil {
			return nil, fmt.Errorf("failed to find environment: %w", err)
		}
		createToolParams.DefaultEnvironmentSlug = conv.ToPGText(*payload.DefaultEnvironmentSlug)
	} else {
		environments, err := s.environmentRepo.ListEnvironments(ctx, *authCtx.ProjectID)
		if err != nil {
			return nil, fmt.Errorf("failed to list environments: %w", err)
		}
		for _, environment := range environments {
			if environment.Slug == "default" { // We will autofill the default environment if one is available
				createToolParams.DefaultEnvironmentSlug = conv.ToPGText(environment.Slug)
				break
			}
		}
	}

	createdToolset, err := s.repo.CreateToolset(ctx, createToolParams)
	if err != nil {
		if strings.Contains(err.Error(), "unique constraint") {
			return nil, errors.New("toolset slug already exists")
		}

		return nil, err
	}

	toolsetDetails, err := s.toolsets.LoadToolsetDetails(ctx, createdToolset.Slug, *authCtx.ProjectID)
	if err != nil {
		return nil, err
	}

	return toolsetDetails, nil
}

func (s *Service) ListToolsets(ctx context.Context, payload *gen.ListToolsetsPayload) (*gen.ListToolsetsResult, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return nil, errors.New("project ID not found in context")
	}

	toolsets, err := s.repo.ListToolsetsByProject(ctx, *authCtx.ProjectID)
	if err != nil {
		return nil, err
	}

	result := make([]*gen.ToolsetDetails, len(toolsets))
	for i, toolset := range toolsets {
		toolsetDetails, err := s.toolsets.LoadToolsetDetails(ctx, toolset.Slug, *authCtx.ProjectID)
		if err != nil {
			return nil, err
		}
		result[i] = toolsetDetails
	}

	return &gen.ListToolsetsResult{
		Toolsets: result,
	}, nil
}

func (s *Service) UpdateToolset(ctx context.Context, payload *gen.UpdateToolsetPayload) (*gen.ToolsetDetails, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return nil, errors.New("project ID not found in context")
	}

	// First get the existing toolset
	existingToolset, err := s.repo.GetToolset(ctx, repo.GetToolsetParams{
		Slug:      payload.Slug,
		ProjectID: *authCtx.ProjectID,
	})
	if err != nil {
		return nil, err
	}

	// Convert update params
	updateParams := repo.UpdateToolsetParams{
		Slug:                   payload.Slug,
		Description:            existingToolset.Description,
		Name:                   existingToolset.Name,
		DefaultEnvironmentSlug: existingToolset.DefaultEnvironmentSlug,
		ProjectID:              *authCtx.ProjectID,
		HttpToolNames:          existingToolset.HttpToolNames,
	}
	if payload.Name != nil {
		updateParams.Name = *payload.Name
	}
	if payload.Description != nil {
		updateParams.Description = pgtype.Text{String: *payload.Description, Valid: true}
	}

	if payload.DefaultEnvironmentSlug != nil {
		_, err := s.environmentRepo.GetEnvironmentBySlug(ctx, environments_repo.GetEnvironmentBySlugParams{
			Slug:      *payload.DefaultEnvironmentSlug,
			ProjectID: *authCtx.ProjectID,
		})
		if err != nil {
			return nil, fmt.Errorf("failed to find environment: %w", err)
		}
		updateParams.DefaultEnvironmentSlug = conv.ToPGText(*payload.DefaultEnvironmentSlug)
	}

	// Convert set back to slice
	if len(payload.HTTPToolNames) > 0 {
		updateParams.HttpToolNames = make([]string, 0, len(payload.HTTPToolNames))
		updateParams.HttpToolNames = append(updateParams.HttpToolNames, payload.HTTPToolNames...)
	}

	updatedToolset, err := s.repo.UpdateToolset(ctx, updateParams)
	if err != nil {
		return nil, oops.E(err, "error updating toolset", "failed to update toolset in database").Log(ctx, s.logger)
	}

	toolsetDetails, err := s.toolsets.LoadToolsetDetails(ctx, updatedToolset.Slug, *authCtx.ProjectID)
	if err != nil {
		return nil, err
	}

	return toolsetDetails, nil
}

func (s *Service) DeleteToolset(ctx context.Context, payload *gen.DeleteToolsetPayload) (err error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return errors.New("project ID not found in context")
	}

	return s.repo.DeleteToolset(ctx, repo.DeleteToolsetParams{
		Slug:      payload.Slug,
		ProjectID: *authCtx.ProjectID,
	})
}

func (s *Service) GetToolsetDetails(ctx context.Context, payload *gen.GetToolsetDetailsPayload) (*gen.ToolsetDetails, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return nil, errors.New("project ID not found in context")
	}

	return s.toolsets.LoadToolsetDetails(ctx, payload.Slug, *authCtx.ProjectID)
}

func (s *Service) APIKeyAuth(ctx context.Context, key string, schema *security.APIKeyScheme) (context.Context, error) {
	return s.auth.Authorize(ctx, key, schema)
}
