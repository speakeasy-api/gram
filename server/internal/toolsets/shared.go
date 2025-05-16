package toolsets

import (
	"context"
	"maps"
	"slices"

	"github.com/google/uuid"
	"github.com/speakeasy-api/gram/internal/database"
	toolsRepo "github.com/speakeasy-api/gram/internal/tools/repo"
	"github.com/speakeasy-api/gram/internal/tools/security"
	"github.com/speakeasy-api/gram/internal/toolsets/repo"
)

// Shared service for aggregating toolset details
type Toolsets struct {
	repo      *repo.Queries
	toolsRepo *toolsRepo.Queries
}

func NewToolsets(tx database.DBTX) *Toolsets {
	return &Toolsets{
		repo:      repo.New(tx),
		toolsRepo: toolsRepo.New(tx),
	}
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
