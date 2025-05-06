package toolsets

import (
	"context"
	"maps"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	gen "github.com/speakeasy-api/gram/gen/toolsets"
	"github.com/speakeasy-api/gram/internal/conv"
	toolsRepo "github.com/speakeasy-api/gram/internal/tools/repo"
	"github.com/speakeasy-api/gram/internal/tools/security"
	"github.com/speakeasy-api/gram/internal/toolsets/repo"
)

// Shared service for aggregating toolset details
type Toolsets struct {
	repo      *repo.Queries
	toolsRepo *toolsRepo.Queries
}

func NewToolsets(db *pgxpool.Pool) *Toolsets {
	return &Toolsets{
		repo:      repo.New(db),
		toolsRepo: toolsRepo.New(db),
	}
}

func (t *Toolsets) LoadToolsetDetails(ctx context.Context, slug string, projectID uuid.UUID) (*gen.ToolsetDetails, error) {
	toolset, err := t.repo.GetToolset(ctx, repo.GetToolsetParams{
		Slug:      strings.ToLower(slug),
		ProjectID: projectID,
	})
	if err != nil {
		return nil, err
	}

	var httpTools []*gen.HTTPToolDefinition
	var relevantEnvVars []string
	if len(toolset.HttpToolNames) > 0 {
		definitions, err := t.toolsRepo.FindToolsByName(ctx, toolsRepo.FindToolsByNameParams{
			ProjectID:    projectID,
			Names:        toolset.HttpToolNames,
			DeploymentID: uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		})
		if err != nil {
			return nil, err
		}

		httpTools = make([]*gen.HTTPToolDefinition, 0, len(definitions))
		seen := make(map[string]bool, 0)
		for _, def := range definitions {
			if _, ok := seen[def.HttpToolDefinition.Name]; ok {
				continue
			}
			seen[def.HttpToolDefinition.ID.String()] = true

			httpTools = append(httpTools, &gen.HTTPToolDefinition{
				ID:                  def.HttpToolDefinition.ID.String(),
				ProjectID:           def.HttpToolDefinition.Description,
				DeploymentID:        def.HttpToolDefinition.DeploymentID.String(),
				Openapiv3DocumentID: conv.FromNullableUUID(def.HttpToolDefinition.Openapiv3DocumentID),
				Name:                def.HttpToolDefinition.Name,
				Summary:             def.HttpToolDefinition.Summary,
				Description:         def.HttpToolDefinition.Description,
				Openapiv3Operation:  conv.FromPGText[string](def.HttpToolDefinition.Openapiv3Operation),
				Tags:                def.HttpToolDefinition.Tags,
				Security:            conv.FromBytes(def.HttpToolDefinition.Security),
				HTTPMethod:          def.HttpToolDefinition.HttpMethod,
				Path:                def.HttpToolDefinition.Path,
				SchemaVersion:       &def.HttpToolDefinition.SchemaVersion,
				Schema:              string(def.HttpToolDefinition.Schema),
				CreatedAt:           def.HttpToolDefinition.CreatedAt.Time.Format(time.RFC3339),
				UpdatedAt:           def.HttpToolDefinition.UpdatedAt.Time.Format(time.RFC3339),
			})
		}
		relevantEnvVars, err = t.GetRelevantEnvironmentVariables(ctx, definitions)
		if err != nil {
			return nil, err
		}
	}

	return &gen.ToolsetDetails{
		ID:                           toolset.ID.String(),
		OrganizationID:               toolset.OrganizationID,
		ProjectID:                    toolset.ProjectID.String(),
		Name:                         toolset.Name,
		Slug:                         gen.Slug(toolset.Slug),
		DefaultEnvironmentSlug:       conv.FromPGText[gen.Slug](toolset.DefaultEnvironmentSlug),
		RelevantEnvironmentVariables: relevantEnvVars,
		Description:                  conv.FromPGText[string](toolset.Description),
		HTTPTools:                    httpTools,
		CreatedAt:                    toolset.CreatedAt.Time.Format(time.RFC3339),
		UpdatedAt:                    toolset.UpdatedAt.Time.Format(time.RFC3339),
	}, nil
}

func (t *Toolsets) GetRelevantEnvironmentVariables(ctx context.Context, tools []toolsRepo.FindToolsByNameRow) ([]string, error) {
	if len(tools) == 0 {
		return []string{}, nil
	}

	relevantSecurityKeysMap := make(map[string]bool)
	serverEnvVarsMap := make(map[string]bool)
	for _, tool := range tools {
		securityKeys, err := security.ParseHTTPToolSecurityKeys(tool.HttpToolDefinition.Security)
		if err != nil {
			return nil, err
		}

		for _, key := range securityKeys {
			relevantSecurityKeysMap[key] = true
		}

		if tool.HttpToolDefinition.ServerEnvVar != "" {
			serverEnvVarsMap[tool.HttpToolDefinition.ServerEnvVar] = true
		}
	}

	uniqueDeploymentIDs := make(map[uuid.UUID]bool)
	for _, tool := range tools {
		uniqueDeploymentIDs[tool.HttpToolDefinition.DeploymentID] = true
	}

	securityEntries, err := t.repo.GetHTTPSecurityDefinitions(ctx, repo.GetHTTPSecurityDefinitionsParams{
		SecurityKeys:  slices.Collect(maps.Keys(relevantSecurityKeysMap)),
		DeploymentIds: slices.Collect(maps.Keys(uniqueDeploymentIDs)), // all selected tools share the same deployment
	})
	if err != nil {
		return nil, err
	}

	relevantEnvVarsMap := make(map[string]bool)
	for _, entry := range securityEntries {
		for _, envVar := range entry.EnvVariables {
			relevantEnvVarsMap[envVar] = true
		}
	}

	for key := range serverEnvVarsMap {
		relevantEnvVarsMap[key] = true
	}

	return slices.Collect(maps.Keys(relevantEnvVarsMap)), nil
}

type HTTPToolExecutionInfo struct {
	Tool     toolsRepo.HttpToolDefinition
	Security []repo.HttpSecurity
}

func (t *Toolsets) GetHTTPToolExecutionInfoByID(ctx context.Context, id uuid.UUID, projectID uuid.UUID) (*HTTPToolExecutionInfo, error) {
	tool, err := t.toolsRepo.GetHTTPToolDefinitionByID(ctx, toolsRepo.GetHTTPToolDefinitionByIDParams{
		ID:        id,
		ProjectID: projectID,
	})
	if err != nil {
		return nil, err
	}

	relevantSecurityKeysMap := make(map[string]bool)
	securityKeys, err := security.ParseHTTPToolSecurityKeys(tool.Security)
	if err != nil {
		return nil, err
	}
	for _, key := range securityKeys {
		relevantSecurityKeysMap[key] = true
	}

	securityEntries, err := t.repo.GetHTTPSecurityDefinitions(ctx, repo.GetHTTPSecurityDefinitionsParams{
		SecurityKeys:  slices.Collect(maps.Keys(relevantSecurityKeysMap)),
		DeploymentIds: []uuid.UUID{tool.DeploymentID},
	})
	if err != nil {
		return nil, err
	}

	return &HTTPToolExecutionInfo{
		Tool:     tool,
		Security: securityEntries,
	}, nil
}
