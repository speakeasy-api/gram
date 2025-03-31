package toolsets

import (
	"context"
	"errors"
	"log/slog"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	srv "github.com/speakeasy-api/gram/gen/http/toolsets/server"
	gen "github.com/speakeasy-api/gram/gen/toolsets"
	"github.com/speakeasy-api/gram/internal/conv"
	"github.com/speakeasy-api/gram/internal/must"
	"github.com/speakeasy-api/gram/internal/projects"
	"github.com/speakeasy-api/gram/internal/sessions"
	"github.com/speakeasy-api/gram/internal/toolsets/repo"
	goahttp "goa.design/goa/v3/http"
	"goa.design/goa/v3/security"
)

type Service struct {
	logger   *slog.Logger
	db       *pgxpool.Pool
	repo     *repo.Queries
	sessions *sessions.Sessions
	projects *projects.Service
}

var _ gen.Service = &Service{}

func NewService(logger *slog.Logger, db *pgxpool.Pool) *Service {
	return &Service{logger: logger, db: db, repo: repo.New(db), sessions: sessions.New(), projects: projects.NewService(logger, db)}
}

func Attach(mux goahttp.Muxer, service gen.Service) {
	endpoints := gen.NewEndpoints(service)
	srv.Mount(
		mux,
		srv.New(endpoints, mux, goahttp.RequestDecoder, goahttp.ResponseEncoder, nil, nil),
	)
}

func (s *Service) CreateToolset(ctx context.Context, p *gen.CreateToolsetPayload) (*gen.Toolset, error) {
	session, ok := sessions.GetSessionValueFromContext(ctx)
	if !ok || session == nil {
		return nil, errors.New("session not found in context")
	}

	project, err := s.projects.GetProject(ctx, p.ProjectID)
	if project.OrganizationID.String() != session.ActiveOrganizationID {
		return nil, errors.New("project does not belong to active organization")
	}

	createToolParams := repo.CreateToolsetParams{
		OrganizationID: must.Value(uuid.Parse(session.ActiveOrganizationID)),
		ProjectID:      must.Value(uuid.Parse(p.ProjectID)),
		Name:           p.Name,
	}

	if p.Description != nil {
		createToolParams.Description = pgtype.Text{String: *p.Description, Valid: true}
	}

	if len(p.HTTPToolIds) > 0 {
		createToolParams.HttpToolIds = make([]uuid.UUID, len(p.HTTPToolIds))
		for i, id := range p.HTTPToolIds {
			toolID, err := uuid.Parse(id)
			if err != nil {
				return nil, err
			}
			createToolParams.HttpToolIds[i] = toolID
		}
	}

	createdToolset, err := s.repo.CreateToolset(ctx, createToolParams)
	if err != nil {
		return nil, err
	}

	httpToolIds := make([]string, len(createdToolset.HttpToolIds))
	for i, id := range createdToolset.HttpToolIds {
		httpToolIds[i] = id.String()
	}

	return &gen.Toolset{
		ID:             createdToolset.ID.String(),
		OrganizationID: createdToolset.OrganizationID.String(),
		ProjectID:      createdToolset.ProjectID.String(),
		Name:           createdToolset.Name,
		Description:    conv.FromPGText(createdToolset.Description),
		HTTPToolIds:    httpToolIds,
		CreatedAt:      createdToolset.CreatedAt.Time.String(),
		UpdatedAt:      createdToolset.UpdatedAt.Time.String(),
	}, nil
}

func (s *Service) ListToolsets(ctx context.Context, p *gen.ListToolsetsPayload) (*gen.ListToolsetsResult, error) {
	session, ok := sessions.GetSessionValueFromContext(ctx)
	if !ok || session == nil {
		return nil, errors.New("session not found in context")
	}

	project, err := s.projects.GetProject(ctx, p.ProjectID)
	if project.OrganizationID.String() != session.ActiveOrganizationID {
		return nil, errors.New("project does not belong to active organization")
	}

	projectID := must.Value(uuid.Parse(p.ProjectID))

	toolsets, err := s.repo.ListToolsetsByProject(ctx, projectID)
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
			ID:             toolset.ID.String(),
			OrganizationID: toolset.OrganizationID.String(),
			ProjectID:      toolset.ProjectID.String(),
			Name:           toolset.Name,
			Description:    conv.FromPGText(toolset.Description),
			HTTPToolIds:    httpToolIds,
			CreatedAt:      toolset.CreatedAt.Time.String(),
			UpdatedAt:      toolset.UpdatedAt.Time.String(),
		}
	}

	return &gen.ListToolsetsResult{
		Toolsets: result,
	}, nil
}

func (s *Service) UpdateToolset(ctx context.Context, p *gen.UpdateToolsetPayload) (*gen.Toolset, error) {
	toolsetID := must.Value(uuid.Parse(p.ID))
	session, ok := sessions.GetSessionValueFromContext(ctx)
	if !ok || session == nil {
		return nil, errors.New("session not found in context")
	}

	// First get the existing toolset
	existingToolset, err := s.repo.GetToolset(ctx, toolsetID)
	if err != nil {
		return nil, err
	}

	if existingToolset.OrganizationID.String() != session.ActiveOrganizationID {
		return nil, errors.New("toolset does not belong to active organization")
	}

	// Convert update params
	updateParams := repo.UpdateToolsetParams{
		ID:          toolsetID,
		Description: existingToolset.Description,
		Name:        existingToolset.Name,
	}
	if p.Name != nil {
		updateParams.Name = *p.Name
	}
	if p.Description != nil {
		updateParams.Description = pgtype.Text{String: *p.Description, Valid: true}
	}

	toolIDSet := make(map[uuid.UUID]bool, len(existingToolset.HttpToolIds))
	for _, id := range existingToolset.HttpToolIds {
		toolIDSet[id] = true
	}

	// Add new tools
	for _, idStr := range p.HTTPToolIdsToAdd {
		id := must.Value(uuid.Parse(idStr))
		toolIDSet[id] = true
	}

	// Remove tools
	for _, idStr := range p.HTTPToolIdsToRemove {
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
		ID:             updatedToolset.ID.String(),
		OrganizationID: updatedToolset.OrganizationID.String(),
		ProjectID:      updatedToolset.ProjectID.String(),
		Name:           updatedToolset.Name,
		Description:    conv.FromPGText(updatedToolset.Description),
		HTTPToolIds:    httpToolIds,
		CreatedAt:      updatedToolset.CreatedAt.Time.String(),
		UpdatedAt:      updatedToolset.UpdatedAt.Time.String(),
	}, nil
}

func (s *Service) GetToolsetDetails(ctx context.Context, p *gen.GetToolsetDetailsPayload) (*gen.ToolsetDetails, error) {
	toolsetID := must.Value(uuid.Parse(p.ID))
	session, ok := sessions.GetSessionValueFromContext(ctx)
	if !ok || session == nil {
		return nil, errors.New("session not found in context")
	}

	toolset, err := s.repo.GetToolset(ctx, toolsetID)
	if err != nil {
		return nil, err
	}

	if toolset.OrganizationID.String() != session.ActiveOrganizationID {
		return nil, errors.New("toolset does not belong to active organization")
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
			}
		}
	}

	return &gen.ToolsetDetails{
		ID:             toolset.ID.String(),
		OrganizationID: toolset.OrganizationID.String(),
		ProjectID:      toolset.ProjectID.String(),
		Name:           toolset.Name,
		Description:    conv.FromPGText(toolset.Description),
		HTTPTools:      httpTools,
		CreatedAt:      toolset.CreatedAt.Time.String(),
		UpdatedAt:      toolset.UpdatedAt.Time.String(),
	}, nil
}

func (s *Service) APIKeyAuth(ctx context.Context, key string, schema *security.APIKeyScheme) (context.Context, error) {
	return s.sessions.SessionAuth(ctx, key)
}
