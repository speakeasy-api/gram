package toolsets

import (
	"context"
	"maps"
	"slices"

	"github.com/google/uuid"
	projectsRepo "github.com/speakeasy-api/gram/internal/projects/repo"
	toolsRepo "github.com/speakeasy-api/gram/internal/tools/repo"
	"github.com/speakeasy-api/gram/internal/tools/security"
	"github.com/speakeasy-api/gram/internal/toolsets/repo"
)

// Shared service for aggregating toolset details
type Toolsets struct {
	repo      *repo.Queries
	toolsRepo *toolsRepo.Queries
	projects  *projectsRepo.Queries
}

func NewToolsets(tx repo.DBTX) *Toolsets {
	return &Toolsets{
		repo:      repo.New(tx),
		toolsRepo: toolsRepo.New(tx),
		projects:  projectsRepo.New(tx),
	}
}

type HTTPToolExecutionInfo struct {
	Tool        toolsRepo.HttpToolDefinition
	Security    []repo.HttpSecurity
	ProjectSlug *string
	OrgSlug     *string
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

	var projectSlug *string
	var orgSlug *string
	if orgData, err := t.projects.GetProjectWithOrganizationMetadata(ctx, tool.ProjectID); err == nil {
		orgSlug = &orgData.Slug
		projectSlug = &orgData.ProjectSlug
	}

	return &HTTPToolExecutionInfo{
		Tool:        tool,
		Security:    securityEntries,
		ProjectSlug: projectSlug,
		OrgSlug:     orgSlug,
	}, nil
}
