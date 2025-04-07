package toolsets

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/trace"
	goahttp "goa.design/goa/v3/http"
	"goa.design/goa/v3/security"

	srv "github.com/speakeasy-api/gram/gen/http/toolsets/server"
	gen "github.com/speakeasy-api/gram/gen/toolsets"
	"github.com/speakeasy-api/gram/internal/auth"
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
}

var _ gen.Service = &Service{}

func NewService(logger *slog.Logger, db *pgxpool.Pool, redisClient *redis.Client) *Service {
	return &Service{
		tracer:          otel.Tracer("github.com/speakeasy-api/gram/internal/toolsets"),
		logger:          logger,
		db:              db,
		repo:            repo.New(db),
		auth:            auth.New(logger, db, redisClient),
		environmentRepo: environments_repo.New(db),
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

func (s *Service) CreateToolset(ctx context.Context, payload *gen.CreateToolsetPayload) (*gen.Toolset, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return nil, errors.New("project ID not found in context")
	}

	createToolParams := repo.CreateToolsetParams{
		OrganizationID: authCtx.ActiveOrganizationID,
		ProjectID:      *authCtx.ProjectID,
		Name:           payload.Name,
		Slug:           conv.ToSlug(payload.Name),
	}

	if payload.DefaultEnvironmentID != nil {
		_, err := s.environmentRepo.GetEnvironmentByID(ctx, environments_repo.GetEnvironmentByIDParams{
			ID:        uuid.MustParse(*payload.DefaultEnvironmentID),
			ProjectID: *authCtx.ProjectID,
		})
		if err != nil {
			return nil, fmt.Errorf("failed to find environment: %w", err)
		}
		createToolParams.DefaultEnvironmentID = uuid.NullUUID{UUID: uuid.MustParse(*payload.DefaultEnvironmentID), Valid: true}
	}

	if payload.Description != nil {
		createToolParams.Description = pgtype.Text{String: *payload.Description, Valid: true}
	}

	if len(payload.HTTPToolNames) > 0 {
		createToolParams.HttpToolNames = make([]string, len(payload.HTTPToolNames))
		for i, name := range payload.HTTPToolNames {
			createToolParams.HttpToolNames[i] = name
		}
	}

	createdToolset, err := s.repo.CreateToolset(ctx, createToolParams)
	if err != nil {
		if strings.Contains(err.Error(), "unique constraint") {
			return nil, errors.New("toolset slug already exists")
		}

		return nil, err
	}

	httpToolNames := make([]string, len(createdToolset.HttpToolNames))
	for i, name := range createdToolset.HttpToolNames {
		httpToolNames[i] = name
	}

	return &gen.Toolset{
		ID:                   createdToolset.ID.String(),
		OrganizationID:       createdToolset.OrganizationID,
		ProjectID:            createdToolset.ProjectID.String(),
		Name:                 createdToolset.Name,
		Slug:                 createdToolset.Slug,
		DefaultEnvironmentID: conv.FromNullableUUID(createdToolset.DefaultEnvironmentID),
		Description:          conv.FromPGText(createdToolset.Description),
		HTTPToolNames:        httpToolNames,
		CreatedAt:            createdToolset.CreatedAt.Time.Format(time.RFC3339),
		UpdatedAt:            createdToolset.UpdatedAt.Time.Format(time.RFC3339),
	}, nil
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

	result := make([]*gen.Toolset, len(toolsets))
	for i, toolset := range toolsets {
		httpToolNames := make([]string, len(toolset.HttpToolNames))
		for j, name := range toolset.HttpToolNames {
			httpToolNames[j] = name
		}
		result[i] = &gen.Toolset{
			ID:                   toolset.ID.String(),
			OrganizationID:       toolset.OrganizationID,
			ProjectID:            toolset.ProjectID.String(),
			Name:                 toolset.Name,
			Slug:                 toolset.Slug,
			DefaultEnvironmentID: conv.FromNullableUUID(toolset.DefaultEnvironmentID),
			Description:          conv.FromPGText(toolset.Description),
			HTTPToolNames:        httpToolNames,
			CreatedAt:            toolset.CreatedAt.Time.Format(time.RFC3339),
			UpdatedAt:            toolset.UpdatedAt.Time.Format(time.RFC3339),
		}
	}

	return &gen.ListToolsetsResult{
		Toolsets: result,
	}, nil
}

func (s *Service) UpdateToolset(ctx context.Context, payload *gen.UpdateToolsetPayload) (*gen.Toolset, error) {
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
		Slug:                 payload.Slug,
		Description:          existingToolset.Description,
		Name:                 existingToolset.Name,
		DefaultEnvironmentID: existingToolset.DefaultEnvironmentID,
		ProjectID:            *authCtx.ProjectID,
	}
	if payload.Name != nil {
		updateParams.Name = *payload.Name
	}
	if payload.Description != nil {
		updateParams.Description = pgtype.Text{String: *payload.Description, Valid: true}
	}

	if payload.DefaultEnvironmentID != nil {
		_, err := s.environmentRepo.GetEnvironmentByID(ctx, environments_repo.GetEnvironmentByIDParams{
			ID:        uuid.MustParse(*payload.DefaultEnvironmentID),
			ProjectID: *authCtx.ProjectID,
		})
		if err != nil {
			return nil, fmt.Errorf("failed to find environment: %w", err)
		}
		updateParams.DefaultEnvironmentID = uuid.NullUUID{UUID: uuid.MustParse(*payload.DefaultEnvironmentID), Valid: true}
	}

	toolNameSet := make(map[string]bool, len(existingToolset.HttpToolNames))
	for _, name := range existingToolset.HttpToolNames {
		toolNameSet[name] = true
	}

	// Add new tools
	for _, name := range payload.HTTPToolNamesToAdd {
		toolNameSet[name] = true
	}

	// Remove tools
	for _, name := range payload.HTTPToolNamesToRemove {
		delete(toolNameSet, name)
	}

	// Convert set back to slice
	if len(toolNameSet) > 0 {
		updateParams.HttpToolNames = make([]string, 0, len(toolNameSet))
		for name := range toolNameSet {
			updateParams.HttpToolNames = append(updateParams.HttpToolNames, name)
		}
	}

	updatedToolset, err := s.repo.UpdateToolset(ctx, updateParams)
	if err != nil {
		return nil, oops.E(err, "error updating toolset", "failed to update toolset in database").Log(ctx, s.logger)
	}

	httpToolNames := make([]string, len(updatedToolset.HttpToolNames))
	for i, name := range updatedToolset.HttpToolNames {
		httpToolNames[i] = name
	}

	return &gen.Toolset{
		ID:                   updatedToolset.ID.String(),
		OrganizationID:       updatedToolset.OrganizationID,
		ProjectID:            updatedToolset.ProjectID.String(),
		Name:                 updatedToolset.Name,
		Slug:                 updatedToolset.Slug,
		DefaultEnvironmentID: conv.FromNullableUUID(updatedToolset.DefaultEnvironmentID),
		Description:          conv.FromPGText(updatedToolset.Description),
		HTTPToolNames:        httpToolNames,
		CreatedAt:            updatedToolset.CreatedAt.Time.Format(time.RFC3339),
		UpdatedAt:            updatedToolset.UpdatedAt.Time.Format(time.RFC3339),
	}, nil
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

	toolset, err := s.repo.GetToolset(ctx, repo.GetToolsetParams{
		Slug:      payload.Slug,
		ProjectID: *authCtx.ProjectID,
	})
	if err != nil {
		return nil, err
	}

	var httpTools []*gen.HTTPToolDefinition
	if len(toolset.HttpToolNames) > 0 {
		definitions, err := s.repo.GetHTTPToolDefinitionsForToolset(ctx, repo.GetHTTPToolDefinitionsForToolsetParams{
			ProjectID: *authCtx.ProjectID,
			Names:     toolset.HttpToolNames,
		})
		if err != nil {
			return nil, err
		}

		httpTools = make([]*gen.HTTPToolDefinition, len(definitions))
		for i, def := range definitions {
			httpTools[i] = &gen.HTTPToolDefinition{
				ID:             def.ID.String(),
				Name:           def.Name,
				Description:    def.Description,
				Tags:           def.Tags,
				ServerEnvVar:   conv.FromPGText(def.ServerEnvVar),
				SecurityType:   conv.FromPGText(def.SecurityType),
				BearerEnvVar:   conv.FromPGText(def.BearerEnvVar),
				ApikeyEnvVar:   conv.FromPGText(def.ApikeyEnvVar),
				UsernameEnvVar: conv.FromPGText(def.UsernameEnvVar),
				PasswordEnvVar: conv.FromPGText(def.PasswordEnvVar),
				HTTPMethod:     def.HttpMethod,
				Path:           def.Path,
				Schema:         conv.FromBytes(def.Schema),
				CreatedAt:      def.CreatedAt.Time.Format(time.RFC3339),
				UpdatedAt:      def.UpdatedAt.Time.Format(time.RFC3339),
			}
		}
	}

	return &gen.ToolsetDetails{
		ID:                   toolset.ID.String(),
		OrganizationID:       toolset.OrganizationID,
		ProjectID:            toolset.ProjectID.String(),
		Name:                 toolset.Name,
		Slug:                 toolset.Slug,
		DefaultEnvironmentID: conv.FromNullableUUID(toolset.DefaultEnvironmentID),
		Description:          conv.FromPGText(toolset.Description),
		HTTPTools:            httpTools,
		CreatedAt:            toolset.CreatedAt.Time.Format(time.RFC3339),
		UpdatedAt:            toolset.UpdatedAt.Time.Format(time.RFC3339),
	}, nil
}

func (s *Service) APIKeyAuth(ctx context.Context, key string, schema *security.APIKeyScheme) (context.Context, error) {
	return s.auth.Authorize(ctx, key, schema)
}
