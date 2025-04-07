package toolsets

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	gen "github.com/speakeasy-api/gram/gen/toolsets"
	"github.com/speakeasy-api/gram/internal/conv"
	"github.com/speakeasy-api/gram/internal/toolsets/repo"
)

// Shared service for aggregating toolset details
type Toolsets struct {
	repo *repo.Queries
}

func NewToolsets(db *pgxpool.Pool) *Toolsets {
	return &Toolsets{
		repo: repo.New(db),
	}
}

func (t *Toolsets) LoadToolsetDetails(ctx context.Context, slug string, projectID uuid.UUID) (*gen.ToolsetDetails, error) {
	toolset, err := t.repo.GetToolset(ctx, repo.GetToolsetParams{
		Slug:      slug,
		ProjectID: projectID,
	})
	if err != nil {
		return nil, err
	}

	var httpTools []*gen.HTTPToolDefinition
	if len(toolset.HttpToolNames) > 0 {
		definitions, err := t.repo.GetHTTPToolDefinitionsForToolset(ctx, repo.GetHTTPToolDefinitionsForToolsetParams{
			ProjectID: projectID,
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
