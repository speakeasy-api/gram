package toolsets

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
	goahttp "goa.design/goa/v3/http"
	"goa.design/goa/v3/security"

	srv "github.com/speakeasy-api/gram/gen/http/toolsets/server"
	gen "github.com/speakeasy-api/gram/gen/toolsets"
	"github.com/speakeasy-api/gram/internal/auth"
	"github.com/speakeasy-api/gram/internal/contextvalues"
	"github.com/speakeasy-api/gram/internal/conv"
	"github.com/speakeasy-api/gram/internal/must"
	"github.com/speakeasy-api/gram/internal/toolsets/repo"
)

type Service struct {
	logger *slog.Logger
	db     *pgxpool.Pool
	repo   *repo.Queries
	auth   *auth.Auth
}

var _ gen.Service = &Service{}

func NewService(logger *slog.Logger, db *pgxpool.Pool, redisClient *redis.Client) *Service {
	return &Service{logger: logger, db: db, repo: repo.New(db), auth: auth.New(logger, db, redisClient)}
}

func Attach(mux goahttp.Muxer, service gen.Service) {
	endpoints := gen.NewEndpoints(service)
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
		createToolParams.DefaultEnvironmentID = uuid.NullUUID{UUID: uuid.MustParse(*payload.DefaultEnvironmentID), Valid: true}
	}

	if payload.Description != nil {
		createToolParams.Description = pgtype.Text{String: *payload.Description, Valid: true}
	}

	if len(payload.HTTPToolIds) > 0 {
		createToolParams.HttpToolIds = make([]uuid.UUID, len(payload.HTTPToolIds))
		for i, id := range payload.HTTPToolIds {
			toolID, err := uuid.Parse(id)
			if err != nil {
				return nil, err
			}
			createToolParams.HttpToolIds[i] = toolID
		}
	}

	createdToolset, err := s.repo.CreateToolset(ctx, createToolParams)
	if err != nil {
		if strings.Contains(err.Error(), "unique constraint") {
			return nil, errors.New("toolset slug already exists")
		}

		return nil, err
	}

	httpToolIds := make([]string, len(createdToolset.HttpToolIds))
	for i, id := range createdToolset.HttpToolIds {
		httpToolIds[i] = id.String()
	}

	return &gen.Toolset{
		ID:                   createdToolset.ID.String(),
		OrganizationID:       createdToolset.OrganizationID,
		ProjectID:            createdToolset.ProjectID.String(),
		Name:                 createdToolset.Name,
		Slug:                 createdToolset.Slug,
		DefaultEnvironmentID: conv.FromNullableUUID(createdToolset.DefaultEnvironmentID),
		Description:          conv.FromPGText(createdToolset.Description),
		HTTPToolIds:          httpToolIds,
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
		httpToolIds := make([]string, len(toolset.HttpToolIds))
		for j, id := range toolset.HttpToolIds {
			httpToolIds[j] = id.String()
		}
		result[i] = &gen.Toolset{
			ID:                   toolset.ID.String(),
			OrganizationID:       toolset.OrganizationID,
			ProjectID:            toolset.ProjectID.String(),
			Name:                 toolset.Name,
			Slug:                 toolset.Slug,
			DefaultEnvironmentID: conv.FromNullableUUID(toolset.DefaultEnvironmentID),
			Description:          conv.FromPGText(toolset.Description),
			HTTPToolIds:          httpToolIds,
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
		updateParams.DefaultEnvironmentID = uuid.NullUUID{UUID: uuid.MustParse(*payload.DefaultEnvironmentID), Valid: true}
	}

	toolIDSet := make(map[uuid.UUID]bool, len(existingToolset.HttpToolIds))
	for _, id := range existingToolset.HttpToolIds {
		toolIDSet[id] = true
	}

	// Add new tools
	for _, idStr := range payload.HTTPToolIdsToAdd {
		id := must.Value(uuid.Parse(idStr))
		toolIDSet[id] = true
	}

	// Remove tools
	for _, idStr := range payload.HTTPToolIdsToRemove {
		id := must.Value(uuid.Parse(idStr))
		delete(toolIDSet, id)
	}

	// Convert set back to slice
	if len(toolIDSet) > 0 {
		updateParams.HttpToolIds = make([]uuid.UUID, 0, len(toolIDSet))
		for id := range toolIDSet {
			updateParams.HttpToolIds = append(updateParams.HttpToolIds, id)
		}
	}

	updatedToolset, err := s.repo.UpdateToolset(ctx, updateParams)
	if err != nil {
		s.logger.ErrorContext(ctx, "failed to update toolset", "error", err)
		return nil, err
	}

	httpToolIds := make([]string, len(updatedToolset.HttpToolIds))
	for i, id := range updatedToolset.HttpToolIds {
		httpToolIds[i] = id.String()
	}

	return &gen.Toolset{
		ID:                   updatedToolset.ID.String(),
		OrganizationID:       updatedToolset.OrganizationID,
		ProjectID:            updatedToolset.ProjectID.String(),
		Name:                 updatedToolset.Name,
		Slug:                 updatedToolset.Slug,
		DefaultEnvironmentID: conv.FromNullableUUID(updatedToolset.DefaultEnvironmentID),
		Description:          conv.FromPGText(updatedToolset.Description),
		HTTPToolIds:          httpToolIds,
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
	if len(toolset.HttpToolIds) > 0 {
		definitions, err := s.repo.GetHTTPToolDefinitions(ctx, toolset.HttpToolIds)
		if err != nil {
			return nil, err
		}

		httpTools = make([]*gen.HTTPToolDefinition, len(definitions))
		for i, def := range definitions {
			httpTools[i] = &gen.HTTPToolDefinition{
				ID:               def.ID.String(),
				Name:             def.Name,
				Description:      def.Description,
				Tags:             def.Tags,
				ServerEnvVar:     def.ServerEnvVar,
				SecurityType:     def.SecurityType,
				BearerEnvVar:     conv.FromPGText(def.BearerEnvVar),
				ApikeyEnvVar:     conv.FromPGText(def.ApikeyEnvVar),
				UsernameEnvVar:   conv.FromPGText(def.UsernameEnvVar),
				PasswordEnvVar:   conv.FromPGText(def.PasswordEnvVar),
				HTTPMethod:       def.HttpMethod,
				Path:             def.Path,
				HeadersSchema:    conv.FromBytes(def.HeadersSchema),
				QueriesSchema:    conv.FromBytes(def.QueriesSchema),
				PathparamsSchema: conv.FromBytes(def.PathparamsSchema),
				BodySchema:       conv.FromBytes(def.BodySchema),
				CreatedAt:        def.CreatedAt.Time.Format(time.RFC3339),
				UpdatedAt:        def.UpdatedAt.Time.Format(time.RFC3339),
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
