package toolsets

import (
	"context"
	"fmt"
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
	Tool           toolsRepo.HttpToolDefinition
	Security       []repo.HttpSecurity
	SecurityScopes map[string][]string
	ProjectSlug    string
	AccountType    string
	OrgSlug        string
}

func (t *Toolsets) GetHTTPToolExecutionInfoByID(ctx context.Context, id uuid.UUID, projectID uuid.UUID) (*HTTPToolExecutionInfo, error) {
	tool, err := t.toolsRepo.GetHTTPToolDefinitionByID(ctx, toolsRepo.GetHTTPToolDefinitionByIDParams{
		ID:        id,
		ProjectID: projectID,
	})
	if err != nil {
		return nil, fmt.Errorf("get http tool definition by id: %w", err)
	}

	relevantSecurityKeysMap := make(map[string]bool)
	securityKeys, securityScopes, err := security.ParseHTTPToolSecurityKeys(tool.Security)
	if err != nil {
		return nil, fmt.Errorf("parse http tool security keys: %w", err)
	}
	for _, key := range securityKeys {
		relevantSecurityKeysMap[key] = true
	}

	securityEntries, err := t.repo.GetHTTPSecurityDefinitions(ctx, repo.GetHTTPSecurityDefinitionsParams{
		SecurityKeys:  slices.Collect(maps.Keys(relevantSecurityKeysMap)),
		DeploymentIds: []uuid.UUID{tool.DeploymentID},
	})
	if err != nil {
		return nil, fmt.Errorf("get http security definitions: %w", err)
	}

	orgData, err := t.projects.GetProjectWithOrganizationMetadata(ctx, tool.ProjectID)
	if err != nil {
		return nil, fmt.Errorf("get project with organization metadata: %w", err)
	}

	return &HTTPToolExecutionInfo{
		Tool:           tool,
		Security:       securityEntries,
		SecurityScopes: securityScopes,
		ProjectSlug:    orgData.ProjectSlug,
		OrgSlug:        orgData.Slug,
		AccountType:    orgData.GramAccountType,
	}, nil
}
